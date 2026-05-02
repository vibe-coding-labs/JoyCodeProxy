# Dashboard QR 码扫码登录实现 Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 在 Dashboard 账号管理页面添加"扫码登录"功能，用户使用京东 APP 扫描二维码登录，每次扫码获得不同用户的凭据，支持配置多个 JoyCode 账号。保留现有"一键登录"（从本机 IDE 读取）和"手动添加"方式。

**Architecture:** 用户点击"扫码登录" → 前端 POST `/api/accounts/qr-login/init` → 后端调用 JD Passport `qr.m.jd.com/show` 获取 QR 码 token，生成 QR 码 PNG 图片（base64）返回前端 → 前端在 Modal 中显示 QR 码 → 前端每 3 秒轮询 `/api/accounts/qr-login/status?session=xxx` → 后端轮询 `qr.m.jd.com/check` 等待扫码确认 → 确认后调用 `passport.jd.com/uc/qrCodeTicketValidation` 验证 ticket → 从 cookie 中提取 `pt_key` → 用 ptKey 调用 JoyCode UserInfo API 获取 userId/realName → 保存为新账号 → 返回成功给前端。

**Tech Stack:** Go 1.23, `github.com/skip2/go-qrcode` (QR 码图片生成), React 18, Ant Design 5 Modal

**Risks:**
- JD Passport API 可能有反爬机制（h5st 签名）→ 缓解：使用标准浏览器 headers，如被拦截再添加签名
- QR 码有效期约 2-3 分钟 → 缓解：前端显示倒计时，超时后显示"刷新二维码"按钮
- `pt_key` cookie 提取位置可能变化 → 缓解：同时检查 `.jd.com` 和 `passport.jd.com` 两个域的 cookies

---

### Task 1: 后端 JD Passport QR 码登录模块

**Depends on:** None
**Files:**
- Create: `pkg/auth/jdlogin.go`
- Modify: `go.mod` (添加 `github.com/skip2/go-qrcode` 依赖)

- [ ] **Step 1: 安装 go-qrcode 依赖 — 生成 QR 码 PNG 图片**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go get github.com/skip2/go-qrcode`
Expected:
  - Exit code: 0
  - go.mod contains `github.com/skip2/go-qrcode`

- [ ] **Step 2: 创建 jdlogin.go — JD Passport QR 码登录核心模块，支持 Dashboard 调用**

```go
// pkg/auth/jdlogin.go
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
	qrShowURL    = "https://qr.m.jd.com/show?appid=133&size=147&t=%d"
	qrCheckURL   = "https://qr.m.jd.com/check?appid=133&token=%s&callback=jsonpCallback&_=%d"
	qrValidURL   = "https://passport.jd.com/uc/qrCodeTicketValidation?t=%s&pageSource=login2025"
	jdUserAgent  = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"
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
}
```

- [ ] **Step 3: 验证编译**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./pkg/auth/`
Expected:
  - Exit code: 0

- [ ] **Step 4: 提交**
Run: `git add pkg/auth/jdlogin.go go.mod go.sum && git commit -m "feat(auth): add JD passport QR code login module for dashboard"`

---

### Task 2: Dashboard QR 登录 API 端点

**Depends on:** Task 1
**Files:**
- Modify: `pkg/dashboard/handler.go:32-41`（注册 QR 登录路由）
- Modify: `pkg/dashboard/handler.go`（添加 QR 登录处理方法）

- [ ] **Step 1: 注册 QR 登录 API 路由 — 在 RegisterRoutes 中添加两个端点**

文件: `pkg/dashboard/handler.go:32-41`（替换 RegisterRoutes 方法）

```go
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/accounts", h.handleAccounts)
	mux.HandleFunc("/api/accounts/", h.handleAccountAction)
	mux.HandleFunc("/api/accounts-auto-login", h.handleAutoLogin)
	mux.HandleFunc("/api/accounts/qr-login/init", h.handleQRLoginInit)
	mux.HandleFunc("/api/accounts/qr-login/status", h.handleQRLoginStatus)
	mux.HandleFunc("/api/models", h.handleModels)
	mux.HandleFunc("/api/stats", h.handleStats)
	mux.HandleFunc("/api/settings", h.handleSettings)
	mux.HandleFunc("/api/health", h.handleHealth)
	mux.HandleFunc("/api/errors", h.handleErrors)
}
```

注意：`/api/accounts/qr-login/init` 和 `/api/accounts/qr-login/status` 会被 `/api/accounts/` 前缀匹配到 `handleAccountAction`。为避免冲突，将这两个路由注册到 `/api/qr-login/init` 和 `/api/qr-login/status`（不带 accounts 前缀）。

修正后的注册：

```go
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/accounts", h.handleAccounts)
	mux.HandleFunc("/api/accounts/", h.handleAccountAction)
	mux.HandleFunc("/api/accounts-auto-login", h.handleAutoLogin)
	mux.HandleFunc("/api/qr-login/init", h.handleQRLoginInit)
	mux.HandleFunc("/api/qr-login/status", h.handleQRLoginStatus)
	mux.HandleFunc("/api/models", h.handleModels)
	mux.HandleFunc("/api/stats", h.handleStats)
	mux.HandleFunc("/api/settings", h.handleSettings)
	mux.HandleFunc("/api/health", h.handleHealth)
	mux.HandleFunc("/api/errors", h.handleErrors)
}
```

- [ ] **Step 2: 添加 handleQRLoginInit 方法 — 初始化 QR 登录会话并返回 QR 码图片**

在 `handleAutoLogin` 方法之后添加：

```go
func (h *Handler) handleQRLoginInit(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	sessionID, qrImage, err := auth.QRInit()
	if err != nil {
		slog.Error("qr-login init", "error", err)
		writeError(w, http.StatusInternalServerError, "生成二维码失败: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":         true,
		"session_id": sessionID,
		"qr_image":   "data:image/png;base64," + qrImage,
	})
}
```

- [ ] **Step 3: 添加 handleQRLoginStatus 方法 — 轮询扫码状态，确认后保存账号**

在 `handleQRLoginInit` 方法之后添加：

```go
func (h *Handler) handleQRLoginStatus(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "missing session parameter")
		return
	}

	status, result, err := auth.QRPollStatus(sessionID)
	if err != nil {
		slog.Error("qr-login poll", "session", sessionID, "error", err)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status": "error", "message": err.Error(),
		})
		return
	}

	if status != "confirmed" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status": status,
		})
		return
	}

	apiKey := result.RealName
	if apiKey == "" {
		apiKey = result.UserID
	}

	isDefault := true
	accounts, _ := h.store.ListAccounts()
	for _, a := range accounts {
		if a.IsDefault {
			isDefault = false
			break
		}
	}

	if err := h.store.AddAccount(apiKey, result.PtKey, result.UserID, isDefault, "GLM-5.1"); err != nil {
		slog.Error("qr-login save account", "api_key", apiKey, "error", err)
		writeError(w, http.StatusInternalServerError, "保存账号失败: "+err.Error())
		return
	}

	slog.Info("qr-login: account saved", "api_key", apiKey, "user_id", result.UserID)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":    "confirmed",
		"ok":        true,
		"api_key":   apiKey,
		"user_id":   result.UserID,
		"real_name": result.RealName,
	})
}
```

需要在 handler.go 的 import 中添加 `"github.com/vibe-coding-labs/JoyCodeProxy/pkg/auth"`（已存在则不需要重复添加）。

- [ ] **Step 4: 验证编译**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./pkg/dashboard/`
Expected:
  - Exit code: 0

- [ ] **Step 5: 提交**
Run: `git add pkg/dashboard/handler.go && git commit -m "feat(dashboard): add QR code login API endpoints for multi-account support"`

---

### Task 3: 前端 QR 码登录弹窗组件

**Depends on:** Task 2
**Files:**
- Create: `web/src/components/QRLoginModal.tsx`
- Modify: `web/src/api.ts:60-93`（添加 QR 登录 API 方法）
- Modify: `web/src/pages/Accounts.tsx:257-270`（添加"扫码登录"按钮）

- [ ] **Step 1: 添加 QR 登录 API 方法到 api.ts**

文件: `web/src/api.ts:60-93`（在 `autoLogin` 之后、`getRecentErrors` 之前添加）

```typescript
  qrLoginInit: () =>
    request<{ ok: boolean; session_id: string; qr_image: string }>('/api/qr-login/init', { method: 'POST' }),
  qrLoginStatus: (sessionId: string) =>
    request<{ status: string; ok?: boolean; api_key?: string; user_id?: string; real_name?: string; message?: string }>(`/api/qr-login/status?session=${encodeURIComponent(sessionId)}`),
```

- [ ] **Step 2: 创建 QRLoginModal 组件 — 显示 QR 码并自动轮询扫码状态**

```typescript
// web/src/components/QRLoginModal.tsx
import React, { useEffect, useState, useRef, useCallback } from 'react';
import { Modal, Typography, Button, Space, Alert, Spin } from 'antd';
import { ReloadOutlined, CheckCircleOutlined, CloseCircleOutlined } from '@ant-design/icons';
import { api } from '../api';

interface QRLoginModalProps {
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
}

const QRLoginModal: React.FC<QRLoginModalProps> = ({ open, onClose, onSuccess }) => {
  const [qrImage, setQrImage] = useState('');
  const [sessionId, setSessionId] = useState('');
  const [status, setStatus] = useState<'loading' | 'waiting' | 'scanned' | 'confirmed' | 'expired' | 'error'>('loading');
  const [countdown, setCountdown] = useState(180);
  const [errorMsg, setErrorMsg] = useState('');
  const pollRef = useRef<ReturnType<typeof setTimeout>>();

  const initQR = useCallback(async () => {
    setStatus('loading');
    setCountdown(180);
    setErrorMsg('');
    try {
      const result = await api.qrLoginInit();
      setQrImage(result.qr_image);
      setSessionId(result.session_id);
      setStatus('waiting');
    } catch (e: unknown) {
      setStatus('error');
      setErrorMsg(e instanceof Error ? e.message : '生成二维码失败');
    }
  }, []);

  useEffect(() => {
    if (open) {
      initQR();
    } else {
      setQrImage('');
      setSessionId('');
      setStatus('loading');
      if (pollRef.current) clearTimeout(pollRef.current);
    }
  }, [open, initQR]);

  useEffect(() => {
    if (!open || !sessionId || status === 'confirmed' || status === 'expired' || status === 'error' || status === 'loading') {
      return;
    }

    const poll = async () => {
      try {
        const result = await api.qrLoginStatus(sessionId);
        if (result.status === 'confirmed') {
          setStatus('confirmed');
          setTimeout(() => { onSuccess(); onClose(); }, 1500);
          return;
        }
        if (result.status === 'expired') {
          setStatus('expired');
          return;
        }
        if (result.status === 'error') {
          setStatus('error');
          setErrorMsg(result.message || '登录失败');
          return;
        }
        if (result.status === 'scanned') {
          setStatus('scanned');
        }
      } catch {
        // Continue polling on network error
      }
      pollRef.current = setTimeout(poll, 3000);
    };

    pollRef.current = setTimeout(poll, 2000);
    return () => { if (pollRef.current) clearTimeout(pollRef.current); };
  }, [open, sessionId, status, onSuccess, onClose]);

  useEffect(() => {
    if (!open || status === 'confirmed' || status === 'expired' || status === 'loading') return;
    const timer = setInterval(() => {
      setCountdown((prev) => {
        if (prev <= 1) {
          setStatus('expired');
          return 0;
        }
        return prev - 1;
      });
    }, 1000);
    return () => clearInterval(timer);
  }, [open, status]);

  const statusDisplay = () => {
    switch (status) {
      case 'loading':
        return <div style={{ textAlign: 'center', padding: 40 }}><Spin size="large" /><div style={{ marginTop: 12, color: '#666' }}>正在生成二维码...</div></div>;
      case 'waiting':
        return <Alert type="info" message="请使用京东 APP 扫描上方二维码" description={`二维码有效期剩余 ${Math.floor(countdown / 60)}:${String(countdown % 60).padStart(2, '0')}`} showIcon />;
      case 'scanned':
        return <Alert type="success" message="已扫描，请在手机上确认登录..." showIcon />;
      case 'confirmed':
        return <Alert type="success" message="登录成功！账号已添加" showIcon icon={<CheckCircleOutlined />} />;
      case 'expired':
        return <Space direction="vertical" align="center" style={{ width: '100%' }}>
          <Alert type="warning" message="二维码已过期" showIcon icon={<CloseCircleOutlined />} />
          <Button icon={<ReloadOutlined />} onClick={initQR}>刷新二维码</Button>
        </Space>;
      case 'error':
        return <Space direction="vertical" align="center" style={{ width: '100%' }}>
          <Alert type="error" message={errorMsg || "登录失败"} showIcon />
          <Button icon={<ReloadOutlined />} onClick={initQR}>重试</Button>
        </Space>;
    }
  };

  return (
    <Modal
      title="扫码登录"
      open={open}
      onCancel={onClose}
      footer={null}
      width={400}
      centered
    >
      <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 16 }}>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          使用京东 APP 扫描二维码登录，每个京东账号对应一个 JoyCode 账号
        </Typography.Text>
        {qrImage && status !== 'confirmed' && (
          <div style={{
            padding: 12, background: '#fff', borderRadius: 8,
            border: '1px solid #f0f0f0', boxShadow: '0 2px 8px rgba(0,0,0,0.06)',
          }}>
            <img src={qrImage} alt="QR Code" style={{ width: 200, height: 200 }} />
          </div>
        )}
        {statusDisplay()}
      </div>
    </Modal>
  );
};

export default QRLoginModal;
```

- [ ] **Step 3: 在 Accounts 页面添加"扫码登录"按钮**

文件: `web/src/pages/Accounts.tsx`

3a. 添加 import（在 CommandTooltip import 之后）:

```typescript
import QRLoginModal from '../components/QRLoginModal';
```

3b. 添加 state 变量（在 `autoLogging` 之后）:

```typescript
  const [qrModalOpen, setQrModalOpen] = useState(false);
```

3c. 修改按钮区域，在"一键登录"之后添加"扫码登录"按钮:

找到 Space 区域中的按钮列表，在"一键登录" Button 和"手动添加" Button 之间添加：

```tsx
          <Button
            onClick={() => setQrModalOpen(true)}
            icon={<SafetyCertificateOutlined />}
          >
            扫码登录
          </Button>
```

3d. 在 `</Modal>` 之后、`</div>` 之前添加 QRLoginModal 组件:

```tsx
      <QRLoginModal
        open={qrModalOpen}
        onClose={() => setQrModalOpen(false)}
        onSuccess={fetchAccounts}
      />
```

- [ ] **Step 4: 验证前端构建**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npm run build 2>&1 | tail -5`
Expected:
  - Exit code: 0
  - Output contains: "built in"

- [ ] **Step 5: 提交**
Run: `git add web/src/components/QRLoginModal.tsx web/src/api.ts web/src/pages/Accounts.tsx && git commit -m "feat(web): add QR code login modal for multi-account dashboard login"`

---

### Task 4: 构建部署和端到端验证

**Depends on:** Task 3
**Files:**
- Modify: `cmd/JoyCodeProxy/static/`（前端产物）

- [ ] **Step 1: 构建 Go 二进制**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build -o joycode_proxy_bin ./cmd/JoyCodeProxy/`
Expected:
  - Exit code: 0

- [ ] **Step 2: 部署到本地服务**
Run: `launchctl unload ~/Library/LaunchAgents/com.joycode.proxy.plist 2>/dev/null; sleep 1; launchctl load ~/Library/LaunchAgents/com.joycode.proxy.plist && sleep 2 && curl -s http://localhost:34891/api/health | python3 -m json.tool`
Expected:
  - Returns JSON with `status: "ok"`

- [ ] **Step 3: 验证 QR 登录初始化端点**
Run: `curl -s -X POST http://localhost:34891/api/qr-login/init | python3 -m json.tool | head -10`
Expected:
  - Returns JSON with `ok: true`, `session_id`, `qr_image` (base64 PNG)

- [ ] **Step 4: 验证账号页面可访问**
Run: `curl -s -o /dev/null -w "%{http_code}" http://localhost:34891/accounts`
Expected:
  - HTTP 200

- [ ] **Step 5: 提交**
Run: `git add cmd/JoyCodeProxy/static/ && git commit -m "build: deploy with QR code login for multi-account support"`
