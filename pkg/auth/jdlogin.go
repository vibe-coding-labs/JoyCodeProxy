package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"
)

const (
	qrShowURL   = "https://qr.m.jd.com/show?appid=133&size=147&t=%d"
	qrCheckURL  = "https://qr.m.jd.com/check?appid=133&token=%s&callback=jsonpCallback&_=%d"
	qrValidURL  = "https://passport.jd.com/uc/qrCodeTicketValidation?t=%s&pageSource=login2025"
	jdUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"
)

// QRSession holds the state for an in-progress QR login session.
type QRSession struct {
	ID        string
	Token     string
	CreatedAt time.Time
	client    *http.Client
}

// QRLoginResult holds the result of a successful QR login.
type QRLoginResult struct {
	PtKey    string
	PtPin    string
	UserID   string
	RealName string
}

var (
	qrSessions   = make(map[string]*QRSession)
	qrSessionsMu sync.Mutex
)

// QRInit starts a new QR code login session. Returns session ID and QR code PNG base64.
func QRInit() (sessionID, qrImageBase64 string, err error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return "", "", fmt.Errorf("create cookie jar: %w", err)
	}
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}

	reqURL := fmt.Sprintf(qrShowURL, time.Now().UnixMilli())
	req, _ := http.NewRequest("GET", reqURL, nil)
	req.Header.Set("User-Agent", jdUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("request QR code: %w", err)
	}
	resp.Body.Close()

	var token string
	for _, c := range client.Jar.Cookies(&url.URL{Scheme: "https", Host: "qr.m.jd.com"}) {
		if c.Name == "wlfstk_smdl" {
			token = c.Value
			break
		}
	}
	if token == "" {
		return "", "", fmt.Errorf("wlfstk_smdl cookie not found")
	}

	sessionID = fmt.Sprintf("qr_%d", time.Now().UnixNano())
	qrURL := fmt.Sprintf("https://plogin.jd.com/cgi-bin/ml/islogin?type=qr&appid=133&t=%s", token)
	png, err := qrcode.Encode(qrURL, qrcode.Medium, 256)
	if err != nil {
		return "", "", fmt.Errorf("generate QR code: %w", err)
	}

	qrSessionsMu.Lock()
	qrSessions[sessionID] = &QRSession{
		ID: sessionID, Token: token,
		CreatedAt: time.Now(), client: client,
	}
	qrSessionsMu.Unlock()

	return sessionID, base64.StdEncoding.EncodeToString(png), nil
}

// QRPollStatus checks the scan status of a QR login session.
// Returns: status ("waiting"|"scanned"|"confirmed"|"expired"|"error"), result on success.
func QRPollStatus(sessionID string) (status string, result *QRLoginResult, err error) {
	qrSessionsMu.Lock()
	session, ok := qrSessions[sessionID]
	qrSessionsMu.Unlock()
	if !ok {
		return "expired", nil, fmt.Errorf("session not found")
	}
	if time.Since(session.CreatedAt) > 3*time.Minute {
		QRCleanup(sessionID)
		return "expired", nil, nil
	}

	reqURL := fmt.Sprintf(qrCheckURL, url.QueryEscape(session.Token), time.Now().UnixMilli())
	req, _ := http.NewRequest("GET", reqURL, nil)
	req.Header.Set("User-Agent", jdUserAgent)
	resp, err := session.client.Do(req)
	if err != nil {
		return "error", nil, err
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	str := string(body)
	start := strings.Index(str, "(")
	end := strings.LastIndex(str, ")")
	if start < 0 || end < 0 {
		return "waiting", nil, nil
	}

	var check struct {
		Code   int    `json:"code"`
		Ticket string `json:"ticket,omitempty"`
	}
	if err := json.Unmarshal([]byte(str[start+1:end]), &check); err != nil {
		return "waiting", nil, nil
	}

	switch check.Code {
	case 200:
		if check.Ticket == "" {
			return "error", nil, fmt.Errorf("ticket is empty")
		}
		loginResult, err := validateAndFetchInfo(session.client, check.Ticket)
		if err != nil {
			return "error", nil, err
		}
		QRCleanup(sessionID)
		return "confirmed", loginResult, nil
	case 201:
		return "waiting", nil, nil
	case 202:
		return "scanned", nil, nil
	case 203, 204:
		QRCleanup(sessionID)
		return "expired", nil, nil
	default:
		return "waiting", nil, nil
	}
}

func validateAndFetchInfo(client *http.Client, ticket string) (*QRLoginResult, error) {
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	defer func() { client.CheckRedirect = nil }()

	reqURL := fmt.Sprintf(qrValidURL, url.QueryEscape(ticket))
	req, _ := http.NewRequest("GET", reqURL, nil)
	req.Header.Set("User-Agent", jdUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("validate ticket: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var vResult struct {
		ReturnCode int    `json:"returnCode"`
		URL        string `json:"url,omitempty"`
	}
	if err := json.Unmarshal(body, &vResult); err != nil {
		return nil, fmt.Errorf("parse validation: %w", err)
	}
	if vResult.ReturnCode != 0 {
		return nil, fmt.Errorf("ticket validation failed (code=%d)", vResult.ReturnCode)
	}

	if vResult.URL != "" {
		rReq, _ := http.NewRequest("GET", vResult.URL, nil)
		rReq.Header.Set("User-Agent", jdUserAgent)
		if rResp, err := client.Do(rReq); err == nil {
			rResp.Body.Close()
		}
	}

	var ptKey, ptPin string
	for _, host := range []string{".jd.com", "passport.jd.com"} {
		for _, c := range client.Jar.Cookies(&url.URL{Scheme: "https", Host: host}) {
			switch c.Name {
			case "pt_key":
				ptKey = c.Value
			case "pt_pin":
				ptPin = c.Value
			}
		}
	}
	if ptKey == "" {
		return nil, fmt.Errorf("pt_key cookie not found after validation")
	}

	userInfo, err := fetchUserInfoWithPtKey(ptKey)
	if err != nil {
		return nil, err
	}

	userID, _ := userInfo["userId"].(string)
	realName := ""
	if data, ok := userInfo["data"].(map[string]interface{}); ok {
		if name, ok := data["realName"].(string); ok && name != "" {
			realName = name
		}
	}

	return &QRLoginResult{PtKey: ptKey, PtPin: ptPin, UserID: userID, RealName: realName}, nil
}

func fetchUserInfoWithPtKey(ptKey string) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"tenant": "JOYCODE", "userId": "",
		"client": "JoyCode", "clientVersion": "2.4.5",
		"sessionId": "qr-login-session",
	}
	data, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", "https://joycode-api.jd.com/api/saas/user/v1/userInfo", strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	req.Header = http.Header{
		"Content-Type": {"application/json; charset=UTF-8"},
		"ptKey":        {ptKey},
		"loginType":    {"N_PIN_PC"},
		"User-Agent":   {"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) JoyCode/2.4.5 Chrome/133.0.0.0 Electron/35.2.0 Safari/537.36"},
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("userInfo request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse userInfo: %w", err)
	}
	code, _ := result["code"].(float64)
	if code != 0 {
		msg, _ := result["msg"].(string)
		return nil, fmt.Errorf("userInfo error (code=%.0f): %s", code, msg)
	}
	return result, nil
}

// QRCleanup removes a QR login session.
func QRCleanup(sessionID string) {
	qrSessionsMu.Lock()
	delete(qrSessions, sessionID)
	qrSessionsMu.Unlock()
	slog.Debug("qr session cleaned up", "session_id", sessionID)
}
