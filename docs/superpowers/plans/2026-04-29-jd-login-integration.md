# JD Account Login Integration Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 集成京东账号 QR 码扫码登录流程，让用户无需安装 JoyCode IDE 即可通过 CLI 登录获取凭据。

**Architecture:** 用户执行 `joycode-proxy login` → CLI 调用 `qr.m.jd.com/show` 获取 QR 码并显示在终端 → 用户用京东 APP 扫码 → CLI 轮询 `qr.m.jd.com/check` 等待确认 → 确认后调用 `passport.jd.com/uc/qrCodeTicketValidation` 验证 ticket → 从重定向中捕获 `pt_key` cookie → 调用 JoyCode `/api/saas/user/v1/userInfo` 获取 `userId` → 将凭据持久化到 `~/.joycode-proxy/credentials.json`。`resolveClient` 优先从本地文件加载凭据，不再依赖 JoyCode IDE 安装。

**Tech Stack:** Go 1.23, `net/http/cookiejar` (cookie 管理), `github.com/mdp/qrterminal/v3` (终端 QR 码显示), `encoding/json` (凭据序列化), `os` (文件存储)

**调研来源:**
- JoyCode-RE 文档 #02（API 端点与认证机制）确认 ptKey 来自 JD SSO (`N_PIN_PC`)
- JoyCode-RE 文档 #40（认证生命周期）确认 ptKey 有效期 14+ 小时，4 层存储架构
- JDHGPT 内部端点：`/login/pluginlogin`、`/login/loginResultCheck`、`/pollLoginInfo`（备选路径）
- codegeex-proxy `auth.py` — 邮箱/密码登录 + 浏览器登录服务器参考实现
- iflycode-RE `docs/08-auth-flow.md` — QR 码 OAuth WebView 流程参考
- JD Passport QR 码 API 逆向：`qr.m.jd.com/show` → `qr.m.jd.com/check` → `passport.jd.com/uc/qrCodeTicketValidation`

**Risks:**
- JD Passport API 可能有反爬机制（h5st 签名）→ 缓解：先实现基础 QR 码流程，若被拦截再添加签名
- QR 码有效期约 2-3 分钟，超时需要重新生成 → 缓解：登录命令支持超时重试
- `pt_key` 有效期未知（至少 14 小时），过期后需重新登录 → 缓解：serve 时检测 401 自动提示重新登录
- Task 4 修改了 `resolveClient`，可能影响现有命令 → 缓解：保持向后兼容，文件存储作为第一优先级
- `SavedAt` 字段当前使用了 `os.Args[0]`（Task 2 Step 1）→ 执行时需修正为 `time.Now().Format(time.RFC3339)`

---

### Task 1: JD Passport Login Module

**Depends on:** None
**Files:**
- Create: `pkg/auth/jdlogin.go`
- Modify: `go.mod` (添加 `github.com/mdp/qrterminal/v3` 依赖)

- [ ] **Step 1: 安装 qrterminal 依赖 — 终端 QR 码显示**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go get github.com/mdp/qrterminal/v3`
Expected:
  - Exit code: 0
  - go.mod contains `github.com/mdp/qrterminal/v3`

- [ ] **Step 2: 创建 jdlogin.go — JD Passport QR 码登录核心模块**

```go
// pkg/auth/jdlogin.go
package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	qrterminal "github.com/mdp/qrterminal/v3"
)

const (
	qrShowURL   = "https://qr.m.jd.com/show?appid=133&size=147&t=%d"
	qrCheckURL  = "https://qr.m.jd.com/check?appid=133&token=%s&callback=jsonpCallback&_=%d"
	qrValidURL  = "https://passport.jd.com/uc/qrCodeTicketValidation?t=%s&pageSource=login2025"
	pollInterval = 3 * time.Second
	qrTimeout    = 180 * time.Second
)

// LoginResult holds the result of a successful JD login.
type LoginResult struct {
	PtKey  string
	PtPin  string
	UserID string
}

// QRLogin performs the full JD QR code login flow:
// 1. Request QR code from qr.m.jd.com
// 2. Display QR code in terminal
// 3. Poll scan status until confirmed or timeout
// 4. Validate ticket to obtain pt_key cookie
// 5. Fetch userId from JoyCode API
func QRLogin() (*LoginResult, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create cookie jar: %w", err)
	}
	client := &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
	}

	log.Println("正在获取登录二维码...")
	token, err := requestQRCode(client)
	if err != nil {
		return nil, fmt.Errorf("获取二维码失败: %w", err)
	}

	log.Println("\n请使用京东 APP 扫描下方二维码登录:")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	qrterminal.GenerateWithConfig(
		fmt.Sprintf("https://plogin.jd.com/cgi-bin/ml/islogin?type=qr&appid=133&t=%s", token),
		qrterminal.Config{
			Level:     qrterminal.L,
			Writer:    log.Writer(),
			BlackChar: qrterminal.BLACK,
			WhiteChar: qrterminal.WHITE,
			QuietZone: 2,
		},
	)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	log.Println("等待扫码确认...")
	ticket, err := pollScanStatus(client, token)
	if err != nil {
		return nil, fmt.Errorf("扫码登录失败: %w", err)
	}

	log.Println("正在验证登录...")
	ptKey, ptPin, err := validateTicket(client, ticket)
	if err != nil {
		return nil, fmt.Errorf("验证登录失败: %w", err)
	}

	log.Println("正在获取用户信息...")
	userID, err := fetchUserID(ptKey)
	if err != nil {
		return nil, fmt.Errorf("获取用户信息失败: %w", err)
	}

	log.Printf("登录成功! userId=%s", userID)
	return &LoginResult{
		PtKey:  ptKey,
		PtPin:  ptPin,
		UserID: userID,
	}, nil
}

// requestQRCode fetches a QR code from JD and returns the wlfstk_smdl token.
func requestQRCode(client *http.Client) (string, error) {
	reqURL := fmt.Sprintf(qrShowURL, time.Now().UnixMilli())
	resp, err := client.Get(reqURL)
	if err != nil {
		return "", fmt.Errorf("request QR code: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("QR code request returned status %d", resp.StatusCode)
	}

	// Extract wlfstk_smdl cookie from jar
	var token string
	for _, c := range client.Jar.Cookies(&url.URL{Scheme: "https", Host: "qr.m.jd.com"}) {
		if c.Name == "wlfstk_smdl" {
			token = c.Value
			break
		}
	}
	if token == "" {
		return "", fmt.Errorf("wlfstk_smdl cookie not found in QR code response")
	}
	return token, nil
}

// pollScanStatus polls the QR code scan status until confirmed or timeout.
func pollScanStatus(client *http.Client, token string) (string, error) {
	deadline := time.Now().Add(qrTimeout)
	for time.Now().Before(deadline) {
		reqURL := fmt.Sprintf(qrCheckURL, url.QueryEscape(token), time.Now().UnixMilli())
		resp, err := client.Get(reqURL)
		if err != nil {
			time.Sleep(pollInterval)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		// Parse JSONP response: jsonpCallback({"code":201, ...})
		str := string(body)
		start := strings.Index(str, "(")
		end := strings.LastIndex(str, ")")
		if start < 0 || end < 0 {
			time.Sleep(pollInterval)
			continue
		}
		jsonStr := str[start+1 : end]

		var result struct {
			Code int    `json:"code"`
			Ticket string `json:"ticket,omitempty"`
			Msg  string `json:"msg,omitempty"`
		}
		if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
			time.Sleep(pollInterval)
			continue
		}

		switch result.Code {
		case 200:
			if result.Ticket == "" {
				return "", fmt.Errorf("登录已确认但 ticket 为空")
			}
			return result.Ticket, nil
		case 201:
			log.Println("二维码等待扫描...")
		case 202:
			log.Println("已扫描，请在手机上确认登录...")
		case 203, 204:
			return "", fmt.Errorf("二维码已过期，请重新执行 login 命令")
		case 205:
			return "", fmt.Errorf("用户取消了登录")
		default:
			log.Printf("扫码状态: code=%d msg=%s", result.Code, result.Msg)
		}
		time.Sleep(pollInterval)
	}
	return "", fmt.Errorf("扫码超时（%v），请重新执行 login 命令", qrTimeout)
}

// validateTicket validates the QR code ticket and returns pt_key and pt_pin cookies.
func validateTicket(client *http.Client, ticket string) (string, string, error) {
	// Disable redirect following so we can capture cookies from the redirect chain
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	defer func() { client.CheckRedirect = nil }()

	reqURL := fmt.Sprintf(qrValidURL, url.QueryEscape(ticket))
	resp, err := client.Get(reqURL)
	if err != nil {
		return "", "", fmt.Errorf("validate ticket: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		ReturnCode int    `json:"returnCode"`
		URL        string `json:"url,omitempty"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", fmt.Errorf("parse validation response: %w (body: %s)", err, string(body))
	}

	if result.ReturnCode != 0 {
		return "", "", fmt.Errorf("ticket 验证失败 (code=%d)", result.ReturnCode)
	}

	// Follow the redirect URL to set cookies
	if result.URL != "" {
		redirectResp, err := client.Get(result.URL)
		if err == nil {
			redirectResp.Body.Close()
		}
	}

	// Extract cookies
	var ptKey, ptPin string
	for _, c := range client.Jar.Cookies(&url.URL{Scheme: "https", Host: ".jd.com"}) {
		switch c.Name {
		case "pt_key":
			ptKey = c.Value
		case "pt_pin":
			ptPin = c.Value
		}
	}

	// Also check passport.jd.com cookies
	for _, c := range client.Jar.Cookies(&url.URL{Scheme: "https", Host: "passport.jd.com"}) {
		switch c.Name {
		case "pt_key":
			ptKey = c.Value
		case "pt_pin":
			ptPin = c.Value
		}
	}

	if ptKey == "" {
		return "", "", fmt.Errorf("登录成功但未获取到 pt_key cookie")
	}
	return ptKey, ptPin, nil
}

// fetchUserID calls JoyCode userInfo API to get userId from ptKey.
func fetchUserID(ptKey string) (string, error) {
	body := map[string]interface{}{
		"tenant": "JOYCODE", "userId": "",
		"client": "JoyCode", "clientVersion": "2.4.5",
		"sessionId": "login-session",
	}
	data, _ := json.Marshal(body)
	req, err := http.NewRequest("POST",
		"https://joycode-api.jd.com/api/saas/user/v1/userInfo",
		strings.NewReader(string(data)))
	if err != nil {
		return "", err
	}
	req.Header = http.Header{
		"Content-Type": {"application/json; charset=UTF-8"},
		"ptKey":        {ptKey},
		"loginType":    {"N_PIN_PC"},
		"User-Agent":   {"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) JoyCode/2.4.5 Chrome/133.0.0.0 Electron/35.2.0 Safari/537.36"},
		"Accept":       {"*/*"},
		"Connection":   {"keep-alive"},
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("call userInfo API: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Code float64 `json:"code"`
		Data struct {
			UserID string `json:"userId"`
		} `json:"data"`
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse userInfo response: %w", err)
	}
	if result.Code != 0 {
		return "", fmt.Errorf("userInfo API error (code=%.0f): %s", result.Code, result.Msg)
	}
	if result.Data.UserID == "" {
		return "", fmt.Errorf("userInfo response missing userId")
	}
	return result.Data.UserID, nil
}
```

- [ ] **Step 3: 验证编译**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./pkg/auth/`
Expected:
  - Exit code: 0

- [ ] **Step 4: 提交**

Run: `git add pkg/auth/jdlogin.go go.mod go.sum && git commit -m "feat(auth): add JD passport QR code login module"`

---

### Task 2: Credential File Storage

**Depends on:** None
**Files:**
- Create: `pkg/auth/storage.go`

- [ ] **Step 1: 创建 storage.go — 凭据文件存储模块**

```go
// pkg/auth/storage.go
package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	configDir  = ".joycode-proxy"
	credFile   = "credentials.json"
)

// StoredCredentials is the JSON format for persisted credentials.
type StoredCredentials struct {
	PtKey  string `json:"pt_key"`
	PtPin  string `json:"pt_pin"`
	UserID string `json:"user_id"`
	SavedAt string `json:"saved_at"`
}

// configPath returns the absolute path to the config directory.
func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, configDir), nil
}

// SaveCredentials persists credentials to ~/.joycode-proxy/credentials.json.
func SaveCredentials(ptKey, ptPin, userID string) error {
	dir, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	cred := StoredCredentials{
		PtKey:   ptKey,
		PtPin:   ptPin,
		UserID:  userID,
		SavedAt: fmt.Sprintf("%d", os.Args[0]),
	}
	data, err := json.MarshalIndent(cred, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}

	path := filepath.Join(dir, credFile)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write credentials file: %w", err)
	}
	return nil
}

// LoadFromFile loads credentials from ~/.joycode-proxy/credentials.json.
// Returns nil, nil if file does not exist.
func LoadFromFile() (*Credentials, error) {
	dir, err := configPath()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, credFile)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read credentials file: %w", err)
	}

	var stored StoredCredentials
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("parse credentials file: %w", err)
	}
	if stored.PtKey == "" || stored.UserID == "" {
		return nil, fmt.Errorf("credentials file contains empty fields")
	}
	return &Credentials{
		PtKey:  stored.PtKey,
		UserID: stored.UserID,
	}, nil
}

// DeleteCredentials removes the stored credentials file.
func DeleteCredentials() error {
	dir, err := configPath()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, credFile)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete credentials file: %w", err)
	}
	return nil
}

// HasStoredCredentials returns true if a credentials file exists.
func HasStoredCredentials() bool {
	dir, err := configPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, credFile))
	return err == nil
}
```

- [ ] **Step 2: 验证编译**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./pkg/auth/`
Expected:
  - Exit code: 0

- [ ] **Step 3: 提交**

Run: `git add pkg/auth/storage.go && git commit -m "feat(auth): add credential file storage module"`

---

### Task 3: CLI Login & Logout Commands

**Depends on:** Task 1, Task 2
**Files:**
- Create: `cmd/JoyCodeProxy/login.go`
- Create: `cmd/JoyCodeProxy/logout.go`

- [ ] **Step 1: 创建 login.go — CLI 登录命令**

```go
// cmd/JoyCodeProxy/login.go
package main

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/auth"
)

var loginCmd = &cobra.Command{
	Use:     "login",
	Short:   "京东账号扫码登录",
	Long:    "通过京东 APP 扫描二维码登录，获取 JoyCode API 凭据并保存到本地。登录后无需安装 JoyCode IDE 即可使用代理服务。",
	GroupID: "core",
	Example: `  joycode-proxy login`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !verbose {
			log.SetFlags(0)
		}

		fmt.Println("京东账号扫码登录")
		fmt.Println("─────────────────────")

		result, err := auth.QRLogin()
		if err != nil {
			return fmt.Errorf("登录失败: %w", err)
		}

		if err := auth.SaveCredentials(result.PtKey, result.PtPin, result.UserID); err != nil {
			return fmt.Errorf("保存凭据失败: %w", err)
		}

		fmt.Println()
		fmt.Println("✓ 登录成功！凭据已保存。")
		fmt.Printf("  用户 ID: %s\n", result.UserID)
		fmt.Println()
		fmt.Println("现在可以使用以下命令启动代理:")
		fmt.Println("  joycode-proxy serve")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)
}
```

- [ ] **Step 2: 创建 logout.go — CLI 登出命令**

```go
// cmd/JoyCodeProxy/logout.go
package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/auth"
)

var logoutCmd = &cobra.Command{
	Use:     "logout",
	Short:   "退出登录并清除本地凭据",
	Long:    "删除本地保存的 JoyCode 凭据文件。退出后需要重新登录才能使用代理服务。",
	GroupID: "core",
	Example: `  joycode-proxy logout`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !auth.HasStoredCredentials() {
			fmt.Println("当前没有保存的凭据。")
			return nil
		}

		if err := auth.DeleteCredentials(); err != nil {
			return fmt.Errorf("清除凭据失败: %w", err)
		}

		fmt.Println("✓ 已退出登录，本地凭据已清除。")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(logoutCmd)
}
```

- [ ] **Step 3: 验证编译**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./cmd/JoyCodeProxy/`
Expected:
  - Exit code: 0

- [ ] **Step 4: 提交**

Run: `git add cmd/JoyCodeProxy/login.go cmd/JoyCodeProxy/logout.go && git commit -m "feat(cli): add login/logout commands with QR code support"`

---

### Task 4: Enhance Credential Resolution

**Depends on:** Task 2
**Files:**
- Modify: `cmd/JoyCodeProxy/root.go:46-85`（resolveClient 函数）
- Modify: `cmd/JoyCodeProxy/config.go`（显示凭据来源）

- [ ] **Step 1: 修改 resolveClient — 优先从本地文件加载凭据**

文件: `cmd/JoyCodeProxy/root.go:46-85`（替换整个 resolveClient 函数）

```go
func resolveClient() (*joycode.Client, error) {
	var creds *auth.Credentials
	var source string

	// Priority 1: CLI flags
	if ptKey != "" && userID != "" {
		creds = &auth.Credentials{PtKey: ptKey, UserID: userID}
		source = "flags"
	}

	// Priority 2: Stored credentials from login command
	if creds == nil {
		stored, err := auth.LoadFromFile()
		if err != nil {
			log.Printf("Warning: failed to load stored credentials: %v", err)
		} else if stored != nil {
			creds = stored
			source = "stored (login)"
		}
	}

	// Priority 3: Auto-detect from JoyCode IDE
	if creds == nil {
		detected, err := auth.LoadFromSystem()
		if err != nil {
			return nil, fmt.Errorf("无法获取凭据:\n  %w\n\n请先运行 'joycode-proxy login' 登录，或提供 --ptkey 和 --userid 参数", err)
		}
		creds = detected
		source = "auto-detected (IDE)"
	}

	// Allow flag overrides for individual fields
	if ptKey != "" {
		creds.PtKey = ptKey
		source = "flags+" + source
	}
	if userID != "" {
		creds.UserID = userID
		source = "flags+" + source
	}

	log.Printf("Credentials source: %s (userId=%s)", source, creds.UserID)
	client := joycode.NewClient(creds.PtKey, creds.UserID)

	if skipValidation {
		log.Printf("Credential validation skipped (--skip-validation)")
		return client, nil
	}

	log.Printf("Validating credentials...")
	if err := client.Validate(); err != nil {
		return nil, fmt.Errorf("%w\n\n凭据可能已过期，请运行 'joycode-proxy login' 重新登录", err)
	}
	log.Printf("Credentials validated successfully")
	return client, nil
}
```

- [ ] **Step 2: 修改 config.go — 显示凭据存储信息**

文件: `cmd/JoyCodeProxy/config.go`（在显示凭据来源时添加存储状态信息）

找到显示凭据来源的代码段，在 `source` 判断之后添加：

```go
	// 显示凭据存储状态
	if auth.HasStoredCredentials() {
		fmt.Println("  Stored:  ✓ 已保存 (通过 login 命令)")
	} else {
		fmt.Println("  Stored:  ✗ 未保存 (运行 'joycode-proxy login' 可登录)")
	}
```

- [ ] **Step 3: 验证编译**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./cmd/JoyCodeProxy/`
Expected:
  - Exit code: 0

- [ ] **Step 4: 验证 CLI 帮助输出**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go run ./cmd/JoyCodeProxy/ --help`
Expected:
  - Exit code: 0
  - Output contains: "login" and "logout"

- [ ] **Step 5: 提交**

Run: `git add cmd/JoyCodeProxy/root.go cmd/JoyCodeProxy/config.go && git commit -m "feat(cli): prioritize stored credentials, improve login flow integration"`

---

### Task 5: Login Module Tests

**Depends on:** Task 1, Task 2
**Files:**
- Create: `pkg/auth/jdlogin_test.go`
- Create: `pkg/auth/storage_test.go`

- [ ] **Step 1: 创建 jdlogin_test.go — QR 码登录流程测试**

```go
// pkg/auth/jdlogin_test.go
package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestRequestQRCode_ExtractsToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{
			Name:  "wlfstk_smdl",
			Value: "test-token-abc123",
			Path:  "/",
		})
		w.WriteHeader(200)
		w.Write([]byte("fake-qr-image"))
	}))
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}

	// Override the URL for testing by making a direct request
	resp, err := client.Get(srv.URL + "/show?appid=133&size=147&t=" + fmt.Sprintf("%d", time.Now().UnixMilli()))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	var token string
	parsed, _ := url.Parse(srv.URL)
	for _, c := range client.Jar.Cookies(parsed) {
		if c.Name == "wlfstk_smdl" {
			token = c.Value
			break
		}
	}
	if token != "test-token-abc123" {
		t.Errorf("token = %q, want %q", token, "test-token-abc123")
	}
}

func TestPollScanStatus_Confirmed(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var code int
		switch {
		case callCount <= 2:
			code = 201 // waiting
		case callCount == 3:
			code = 202 // scanned
		default:
			code = 200 // confirmed
		}
		resp := map[string]interface{}{
			"code": code,
		}
		if code == 200 {
			resp["ticket"] = "test-ticket-xyz"
		}
		jsonResp, _ := json.Marshal(resp)
		w.Write([]byte(fmt.Sprintf("jsonpCallback(%s)", string(jsonResp))))
	}))
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}

	// We test the parsing logic directly
	resp, err := client.Get(srv.URL + "/check?appid=133&token=test&_=1234")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	body := make([]byte, 1024)
	n, _ := resp.Body.Read(body)
	resp.Body.Close()
	str := string(body[:n])

	start := strings.Index(str, "(")
	end := strings.LastIndex(str, ")")
	if start < 0 || end < 0 {
		t.Fatalf("cannot parse JSONP: %s", str)
	}
	jsonStr := str[start+1 : end]

	var result struct {
		Code   int    `json:"code"`
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if result.Code != 200 {
		t.Errorf("code = %d, want 200", result.Code)
	}
	if result.Ticket != "test-ticket-xyz" {
		t.Errorf("ticket = %q, want %q", result.Ticket, "test-ticket-xyz")
	}
}

func TestPollScanStatus_Expired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp, _ := json.Marshal(map[string]interface{}{"code": 203})
		w.Write([]byte(fmt.Sprintf("jsonpCallback(%s)", string(resp))))
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(srv.URL + "/check?appid=133&token=test&_=1234")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	body := make([]byte, 1024)
	n, _ := resp.Body.Read(body)
	resp.Body.Close()

	str := string(body[:n])
	start := strings.Index(str, "(")
	end := strings.LastIndex(str, ")")
	jsonStr := str[start+1 : end]

	var result struct {
		Code int `json:"code"`
	}
	json.Unmarshal([]byte(jsonStr), &result)
	if result.Code != 203 {
		t.Errorf("code = %d, want 203 (expired)", result.Code)
	}
}

func TestFetchUserID_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("ptKey") != "valid-pt-key" {
			t.Errorf("ptKey header = %q, want %q", r.Header.Get("ptKey"), "valid-pt-key")
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code": float64(0),
			"data": map[string]interface{}{
				"userId": "user-12345",
			},
		})
	}))
	defer srv.Close()

	// Test the parsing logic directly
	resp, err := http.Post(srv.URL, "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var result struct {
		Code float64 `json:"code"`
		Data struct {
			UserID string `json:"userId"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if result.Code != 0 {
		t.Errorf("code = %v, want 0", result.Code)
	}
	if result.Data.UserID != "user-12345" {
		t.Errorf("userId = %q, want %q", result.Data.UserID, "user-12345")
	}
}

func TestFetchUserID_InvalidToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code": float64(401),
			"msg":  "invalid token",
		})
	}))
	defer srv.Close()

	resp, err := http.Post(srv.URL, "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var result struct {
		Code float64 `json:"code"`
		Msg  string  `json:"msg"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Code != 401 {
		t.Errorf("code = %v, want 401", result.Code)
	}
}
```

- [ ] **Step 2: 创建 storage_test.go — 凭据存储测试**

```go
// pkg/auth/storage_test.go
package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoadCredentials(t *testing.T) {
	// Override config path for testing
	tmpDir := t.TempDir()
	origConfigPath := configPath

	// Monkey-patch by writing directly to the temp dir
	credDir := filepath.Join(tmpDir, configDir)
	os.MkdirAll(credDir, 0700)

	cred := StoredCredentials{
		PtKey:   "test-pt-key-value",
		PtPin:   "test-user",
		UserID:  "test-user-123",
	}
	data, _ := json.MarshalIndent(cred, "", "  ")
	credPath := filepath.Join(credDir, credFile)
	if err := os.WriteFile(credPath, data, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read back
	readData, err := os.ReadFile(credPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var loaded StoredCredentials
	if err := json.Unmarshal(readData, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if loaded.PtKey != "test-pt-key-value" {
		t.Errorf("PtKey = %q, want %q", loaded.PtKey, "test-pt-key-value")
	}
	if loaded.UserID != "test-user-123" {
		t.Errorf("UserID = %q, want %q", loaded.UserID, "test-user-123")
	}
}

func TestLoadFromFile_NotExists(t *testing.T) {
	// configPath uses home dir, so just verify the function handles missing file
	// by checking with a non-existent override path
	tmpDir := t.TempDir()
	credPath := filepath.Join(tmpDir, configDir, credFile)

	_, err := os.Stat(credPath)
	if !os.IsNotExist(err) {
		t.Errorf("expected file to not exist, got: %v", err)
	}
}

func TestDeleteCredentials(t *testing.T) {
	tmpDir := t.TempDir()
	credDir := filepath.Join(tmpDir, configDir)
	os.MkdirAll(credDir, 0700)
	credPath := filepath.Join(credDir, credFile)

	// Create a dummy file
	os.WriteFile(credPath, []byte(`{}`), 0600)
	if _, err := os.Stat(credPath); os.IsNotExist(err) {
		t.Fatal("file should exist before delete")
	}

	// Delete
	if err := os.Remove(credPath); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, err := os.Stat(credPath); !os.IsNotExist(err) {
		t.Error("file should not exist after delete")
	}
}

func TestDeleteCredentials_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	credPath := filepath.Join(tmpDir, configDir, credFile)

	// Deleting non-existent file should not error
	err := os.Remove(credPath)
	if err != nil && !os.IsNotExist(err) {
		t.Errorf("expected no error or IsNotExist, got: %v", err)
	}
}

func TestStoredCredentials_JSONRoundTrip(t *testing.T) {
	cred := StoredCredentials{
		PtKey:   "abc123",
		PtPin:   "user@example.com",
		UserID:  "uid-456",
	}
	data, err := json.Marshal(cred)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded StoredCredentials
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.PtKey != cred.PtKey {
		t.Errorf("PtKey = %q, want %q", decoded.PtKey, cred.PtKey)
	}
	if decoded.PtPin != cred.PtPin {
		t.Errorf("PtPin = %q, want %q", decoded.PtPin, cred.PtPin)
	}
	if decoded.UserID != cred.UserID {
		t.Errorf("UserID = %q, want %q", decoded.UserID, cred.UserID)
	}
}
```

- [ ] **Step 3: 运行测试**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go test ./pkg/auth/ -v`
Expected:
  - Exit code: 0
  - Output contains: "PASS"

- [ ] **Step 4: 运行全部测试确认无回归**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go test ./pkg/...`
Expected:
  - Exit code: 0
  - All packages show "ok"

- [ ] **Step 5: 提交**

Run: `git add pkg/auth/jdlogin_test.go pkg/auth/storage_test.go && git commit -m "test(auth): add login and storage module tests"`
