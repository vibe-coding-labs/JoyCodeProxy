package dashboard

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/auth"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/joycode"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/store"
)

type Handler struct {
	store     *store.Store
	staticFS  fs.FS
	modelList []string
}

func NewHandler(s *store.Store, staticFS fs.FS) *Handler {
	return &Handler{
		store:     s,
		staticFS:  staticFS,
		modelList: joycode.Models,
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Auth endpoints (no JWT required)
	mux.HandleFunc("/api/auth/status", h.handleAuthStatus)
	mux.HandleFunc("/api/auth/setup", h.handleAuthSetup)
	mux.HandleFunc("/api/auth/login", h.handleAuthLogin)
	mux.HandleFunc("/api/auth/change-password", h.handleChangePassword)

	// Dashboard endpoints (JWT required — enforced by middleware)
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
	mux.HandleFunc("/api/github-stars", h.handleGitHubStars)
}

// GitHub Stars cache
var (
	ghStarsCache     int
	ghStarsCacheTime time.Time
	ghStarsMu        sync.Mutex
)

const ghStarsCacheTTL = 1 * time.Hour
const ghRepo = "vibe-coding-labs/JoyCodeProxy"

func (h *Handler) handleGitHubStars(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ghStarsMu.Lock()
	if ghStarsCache > 0 && time.Since(ghStarsCacheTime) < ghStarsCacheTTL {
		stars := ghStarsCache
		ghStarsMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]interface{}{"stars": stars})
		return
	}
	ghStarsMu.Unlock()

	resp, err := http.Get("https://api.github.com/repos/" + ghRepo)
	if err != nil {
		slog.Warn("github stars fetch failed", "error", err)
		ghStarsMu.Lock()
		stars := ghStarsCache
		ghStarsMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]interface{}{"stars": stars})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		slog.Warn("github stars non-200", "status", resp.StatusCode)
		ghStarsMu.Lock()
		stars := ghStarsCache
		ghStarsMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]interface{}{"stars": stars})
		return
	}

	var result struct {
		StargazersCount int `json:"stargazers_count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.Warn("github stars decode failed", "error", err)
		ghStarsMu.Lock()
		stars := ghStarsCache
		ghStarsMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]interface{}{"stars": stars})
		return
	}

	ghStarsMu.Lock()
	ghStarsCache = result.StargazersCount
	ghStarsCacheTime = time.Now()
	stars := ghStarsCache
	ghStarsMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]interface{}{"stars": stars})
}

// --- Errors Handler ---

func (h *Handler) handleErrors(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := fmt.Sscanf(l, "%d", &limit); err == nil && n == 1 && limit > 0 && limit <= 200 {
			// ok
		} else {
			limit = 50
		}
	}
	logs, err := h.store.GetRecentErrors(limit)
	if err != nil {
		slog.Error("get recent errors", "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if logs == nil {
		logs = []store.RequestLog{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"errors": logs, "total": len(logs)})
}

// ServeStatic serves the SPA frontend for non-API routes.
func (h *Handler) ServeStatic(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" {
		path = "/index.html"
	}

	// Try exact file
	if f, err := h.staticFS.Open(strings.TrimPrefix(path, "/")); err == nil {
		defer f.Close()
		stat, _ := f.Stat()
		if !stat.IsDir() {
			http.ServeContent(w, r, filepath.Base(path), stat.ModTime(), readFileSeeker{f})
			return
		}
	}

	// SPA fallback
	f, err := h.staticFS.Open("index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	stat, _ := f.Stat()
	http.ServeContent(w, r, "index.html", stat.ModTime(), readFileSeeker{f})
}

// readFileSeeker wraps fs.File to implement io.ReadSeeker.
type readFileSeeker struct {
	fs.File
}

func (r readFileSeeker) Seek(offset int64, whence int) (int64, error) {
	if seeker, ok := r.File.(io.Seeker); ok {
		return seeker.Seek(offset, whence)
	}
	return 0, fmt.Errorf("not seekable")
}

// --- Helpers ---

// --- Auth Handlers ---

const jwtSecretKey = "auth_jwt_secret"
const passwordHashKey = "auth_password_hash"
const defaultJWTExpiry = 24 * time.Hour

func (h *Handler) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	hash := h.store.GetSetting(passwordHashKey)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"initialized": hash != "",
	})
}

func (h *Handler) handleAuthSetup(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if h.store.GetSetting(passwordHashKey) != "" {
		writeError(w, http.StatusConflict, "root password already initialized")
		return
	}

	var body struct {
		Password string `json:"password"`
	}
	if !readJSONBody(w, r, &body) {
		return
	}
	if len(body.Password) < 6 {
		writeError(w, http.StatusBadRequest, "密码长度不能少于 6 位")
		return
	}

	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		slog.Error("auth setup: hash password failed", "error", err)
		writeError(w, http.StatusInternalServerError, "密码加密失败")
		return
	}

	if err := h.store.SetSetting(passwordHashKey, hash); err != nil {
		slog.Error("auth setup: save password failed", "error", err)
		writeError(w, http.StatusInternalServerError, "保存密码失败")
		return
	}

	if h.store.GetSetting(jwtSecretKey) == "" {
		secret := generateRandomHex(32)
		h.store.SetSetting(jwtSecretKey, secret)
	}

	token, err := h.issueJWT()
	if err != nil {
		slog.Error("auth setup: issue JWT failed", "error", err)
		writeError(w, http.StatusInternalServerError, "生成 token 失败")
		return
	}

	slog.Info("auth: root password initialized")
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":    true,
		"token": token,
	})
}

func (h *Handler) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	hash := h.store.GetSetting(passwordHashKey)
	if hash == "" {
		writeError(w, http.StatusConflict, "root password not initialized")
		return
	}

	var body struct {
		Password string `json:"password"`
	}
	if !readJSONBody(w, r, &body) {
		return
	}

	if !auth.CheckPassword(body.Password, hash) {
		writeError(w, http.StatusUnauthorized, "密码错误")
		return
	}

	token, err := h.issueJWT()
	if err != nil {
		slog.Error("auth login: issue JWT failed", "error", err)
		writeError(w, http.StatusInternalServerError, "生成 token 失败")
		return
	}

	slog.Info("auth: root login success")
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":    true,
		"token": token,
	})
}

func (h *Handler) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	hash := h.store.GetSetting(passwordHashKey)
	if hash == "" {
		writeError(w, http.StatusConflict, "root password not initialized")
		return
	}

	var body struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if !readJSONBody(w, r, &body) {
		return
	}

	if !auth.CheckPassword(body.OldPassword, hash) {
		writeError(w, http.StatusUnauthorized, "原密码错误")
		return
	}

	if len(body.NewPassword) < 6 {
		writeError(w, http.StatusBadRequest, "新密码长度不能少于 6 位")
		return
	}

	newHash, err := auth.HashPassword(body.NewPassword)
	if err != nil {
		slog.Error("change password: hash failed", "error", err)
		writeError(w, http.StatusInternalServerError, "密码加密失败")
		return
	}

	if err := h.store.SetSetting(passwordHashKey, newHash); err != nil {
		slog.Error("change password: save failed", "error", err)
		writeError(w, http.StatusInternalServerError, "保存密码失败")
		return
	}

	slog.Info("auth: root password changed")
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func (h *Handler) issueJWT() (string, error) {
	secret := h.store.GetSetting(jwtSecretKey)
	if secret == "" {
		return "", fmt.Errorf("JWT secret not configured")
	}
	return auth.GenerateToken("root", secret, defaultJWTExpiry)
}

func generateRandomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func setCors(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, x-api-key")
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	setCors(w)
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"detail": msg})
}

func readJSONBody(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

// --- Account Handlers ---

func (h *Handler) handleAccounts(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.listAccounts(w, r)
	case http.MethodPost:
		h.addAccount(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) listAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.store.ListAccounts()
	if err != nil {
		slog.Error("list accounts", "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if accounts == nil {
		accounts = []store.AccountInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"accounts": accounts})
}

func (h *Handler) addAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		APIKey       string `json:"api_key"`
		PtKey        string `json:"pt_key"`
		UserID       string `json:"user_id"`
		IsDefault    *bool  `json:"is_default"`
		DefaultModel string `json:"default_model"`
	}
	if !readJSONBody(w, r, &body) {
		return
	}
	if body.APIKey == "" || body.PtKey == "" || body.UserID == "" {
		writeError(w, http.StatusBadRequest, "api_key, pt_key, and user_id are required")
		return
	}

	isDefault := false
	if body.IsDefault != nil {
		isDefault = *body.IsDefault
	}

	if err := h.store.AddAccount(body.APIKey, body.PtKey, body.UserID, isDefault, body.DefaultModel); err != nil {
		slog.Error("add account", "api_key", body.APIKey, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "api_key": body.APIKey})
}

func (h *Handler) handleAutoLogin(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	creds, err := auth.LoadFromSystem()
	if err != nil {
		slog.Error("auto-login: load from system failed", "error", err)
		writeError(w, http.StatusBadRequest, "无法从本机获取 JoyCode 凭据: "+err.Error())
		return
	}

	client := joycode.NewClient(creds.PtKey, creds.UserID)
	userInfo, err := client.UserInfo()
	if err != nil {
		slog.Error("auto-login: userInfo request failed", "user_id", creds.UserID, "error", err)
		writeError(w, http.StatusUnauthorized, "凭据验证失败，请先在 JoyCode IDE 中登录: "+err.Error())
		return
	}

	code, ok := userInfo["code"].(float64)
	if !ok || code != 0 {
		msg := "未知错误"
		if m, ok := userInfo["msg"].(string); ok && m != "" {
			msg = m
		}
		slog.Error("auto-login: credentials invalid", "user_id", creds.UserID, "code", code, "msg", msg)
		writeError(w, http.StatusUnauthorized, "凭据已过期或无效: "+msg)
		return
	}

	apiKey := creds.UserID
	realName := ""
	if data, ok := userInfo["data"].(map[string]interface{}); ok {
		if name, ok := data["realName"].(string); ok && name != "" {
			apiKey = name
			realName = name
		}
	}

	isDefault := true
	accounts, _ := h.store.ListAccounts()
	for _, a := range accounts {
		if a.IsDefault {
			isDefault = false
			break
		}
	}

	if err := h.store.AddAccount(apiKey, creds.PtKey, creds.UserID, isDefault, "GLM-5.1"); err != nil {
		slog.Error("auto-login: save account failed", "api_key", apiKey, "error", err)
		writeError(w, http.StatusInternalServerError, "保存账号失败: "+err.Error())
		return
	}

	slog.Info("auto-login: account saved", "api_key", apiKey, "user_id", creds.UserID, "real_name", realName)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":         true,
		"api_key":    apiKey,
		"user_id":    creds.UserID,
		"real_name":  realName,
		"is_default": isDefault,
	})
}

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
		slog.Debug("qr-login poll", "session", sessionID, "status", status)
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
		slog.Error("qr-login save account failed", "api_key", apiKey, "error", err)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":  "confirmed",
			"ok":      false,
			"api_key": apiKey,
			"user_id": result.UserID,
			"message": "登录成功但保存账号失败: " + err.Error(),
		})
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

func (h *Handler) handleAccountAction(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	path := r.URL.Path
	// /api/accounts/{apiKey}/...
	parts := strings.Split(strings.TrimPrefix(path, "/api/accounts/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "missing api_key")
		return
	}

	apiKey := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch {
	case action == "" && r.Method == http.MethodDelete:
		h.removeAccount(w, r, apiKey)
	case action == "default" && r.Method == http.MethodPut:
		h.setDefault(w, r, apiKey)
	case action == "validate" && r.Method == http.MethodPost:
		h.validateAccount(w, r, apiKey)
	case action == "model" && r.Method == http.MethodPut:
		h.updateModel(w, r, apiKey)
	case action == "models" && r.Method == http.MethodGet:
		h.listAccountModels(w, r, apiKey)
	case action == "stats" && r.Method == http.MethodGet:
		h.getAccountStats(w, r, apiKey)
	case action == "logs" && r.Method == http.MethodGet:
		h.getAccountLogs(w, r, apiKey)
	case action == "renew-token" && r.Method == http.MethodPost:
		h.renewToken(w, r, apiKey)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) removeAccount(w http.ResponseWriter, r *http.Request, apiKey string) {
	if err := h.store.RemoveAccount(apiKey); err != nil {
		slog.Error("remove account", "api_key", apiKey, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func (h *Handler) setDefault(w http.ResponseWriter, r *http.Request, apiKey string) {
	if err := h.store.SetDefault(apiKey); err != nil {
		slog.Error("set default account", "api_key", apiKey, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func (h *Handler) validateAccount(w http.ResponseWriter, r *http.Request, apiKey string) {
	account, err := h.store.GetAccount(apiKey)
	if err != nil {
		slog.Error("get account", "api_key", apiKey, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if account == nil {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}

	client := joycode.NewClient(account.PtKey, account.UserID)
	valid := true
	if err := client.Validate(); err != nil {
		valid = false
		slog.Error("validate account", "api_key", apiKey, "error", err)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"api_key": apiKey, "valid": valid})
}

func (h *Handler) updateModel(w http.ResponseWriter, r *http.Request, apiKey string) {
	var body struct {
		DefaultModel string `json:"default_model"`
	}
	if !readJSONBody(w, r, &body) {
		return
	}
	if err := h.store.UpdateAccountModel(apiKey, body.DefaultModel); err != nil {
		slog.Error("update account model", "api_key", apiKey, "model", body.DefaultModel, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "api_key": apiKey, "default_model": body.DefaultModel})
}

func (h *Handler) listAccountModels(w http.ResponseWriter, r *http.Request, apiKey string) {
	account, err := h.store.GetAccount(apiKey)
	if err != nil {
		slog.Error("get account", "api_key", apiKey, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if account == nil {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}

	client := joycode.NewClient(account.PtKey, account.UserID)
	models, err := client.ListModels()
	if err != nil {
		slog.Error("list account models", "api_key", apiKey, "error", err)
		// Fallback to hardcoded list
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"models": modelInfos(h.modelList),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"models": models})
}

func (h *Handler) getAccountStats(w http.ResponseWriter, r *http.Request, apiKey string) {
	stats, err := h.store.GetAccountStats(apiKey)
	if err != nil {
		slog.Error("get account stats", "api_key", apiKey, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if stats.ByModel == nil {
		stats.ByModel = []store.ModelCount{}
	}
	if stats.ByEndpoint == nil {
		stats.ByEndpoint = []store.EndpointCount{}
	}
	writeJSON(w, http.StatusOK, stats)
}

func (h *Handler) getAccountLogs(w http.ResponseWriter, r *http.Request, apiKey string) {
	limit := 200
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := fmt.Sscanf(l, "%d", &limit); err == nil && n == 1 && limit > 0 && limit <= 1000 {
			// ok
		} else {
			limit = 200
		}
	}
	logs, err := h.store.GetAccountLogs(apiKey, limit)
	if err != nil {
		slog.Error("get account logs", "api_key", apiKey, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if logs == nil {
		logs = []store.RequestLog{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"logs": logs, "total": len(logs)})
}

func (h *Handler) renewToken(w http.ResponseWriter, r *http.Request, apiKey string) {
	token, err := h.store.RenewToken(apiKey)
	if err != nil {
		slog.Error("renew token", "api_key", apiKey, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "api_token": token})
}

// --- Model Handlers ---

func (h *Handler) handleModels(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"models": modelInfos(h.modelList),
	})
}

func modelInfos(models []string) []map[string]string {
	result := make([]map[string]string, len(models))
	for i, m := range models {
		result[i] = map[string]string{"id": m, "name": m}
	}
	return result
}

// --- Stats Handler ---

func (h *Handler) handleStats(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	stats, err := h.store.GetStats()
	if err != nil {
		slog.Error("get global stats", "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if stats.ByModel == nil {
		stats.ByModel = []store.ModelCount{}
	}
	if stats.ByAccount == nil {
		stats.ByAccount = []store.AccountCount{}
	}

	totals, _ := h.store.GetAllTimeTotals()
	hourly, _ := h.store.GetHourlyStats()
	if hourly == nil {
		hourly = []store.HourlyData{}
	}

	resp := map[string]interface{}{
		"total_requests":       stats.TotalRequests,
		"total_input_tokens":   stats.TotalInputTk,
		"total_output_tokens":  stats.TotalOutputTk,
		"accounts_count":       stats.AccountsCount,
		"avg_latency_ms":       stats.AvgLatencyMs,
		"error_count":          stats.ErrorCount,
		"stream_count":         stats.StreamCount,
		"success_count":        stats.SuccessCount,
		"by_model":             stats.ByModel,
		"by_account":           stats.ByAccount,
		"all_time":             totals,
		"hourly":               hourly,
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- Settings Handler ---

func (h *Handler) handleSettings(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	switch r.Method {
	case http.MethodGet:
		settings, err := h.store.GetSettings()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if settings == nil {
			settings = map[string]string{}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"settings": settings})

	case http.MethodPut:
		var settings map[string]string
		if !readJSONBody(w, r, &settings) {
			return
		}
		if err := h.store.SetSettings(settings); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// --- Health Handler ---

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	setCors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	accounts, _ := h.store.ListAccounts()
	count := 0
	if accounts != nil {
		count = len(accounts)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":   "ok",
		"accounts": count,
		"version":  "0.3.0",
	})
}
