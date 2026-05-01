# Dashboard 一键登录实现 Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 在 JoyCodeProxy Dashboard 账号管理页面添加"一键登录"按钮，自动从本机 JoyCode IDE 提取凭据、验证、保存为账号。保留现有手动添加账号方式不变。

**Architecture:** 用户点击"一键登录" → 前端 POST `/api/accounts/auto-login` → 后端调用 `auth.LoadFromSystem()` 从本地 JoyCode state.vscdb 提取 ptKey/userID → 调用 `joycode.UserInfo()` 验证凭据并获取用户真实姓名 → 用 realName 作为 api_key 调用 `store.AddAccount()` 保存 → 返回成功/失败给前端。复用已有的 `pkg/auth/credentials.go` 和 `pkg/joycode/client.go`，不引入新依赖。

**Tech Stack:** Go 1.23, React 18, Ant Design 5, TypeScript 5

**Risks:**
- `auth.LoadFromSystem()` 仅支持 macOS → 缓解：非 macOS 返回明确错误信息，前端友好展示
- ptKey 可能已过期 → 缓解：保存前先调 UserInfo API 验证，失败则提示用户先在 JoyCode IDE 中重新登录
- 重复点击一键登录可能创建重复账号 → 缓解：用 realName 作为 api_key，`INSERT OR REPLACE` 自动覆盖更新

---

### Task 1: Backend — 一键登录 API 端点

**Depends on:** None
**Files:**
- Modify: `pkg/dashboard/handler.go:1-15`（添加 import）
- Modify: `pkg/dashboard/handler.go:31-39`（RegisterRoutes 添加路由）
- Modify: `pkg/dashboard/handler.go:200-201`（添加 handleAutoLogin 方法）

- [ ] **Step 1: 添加 auth 包 import — 一键登录需要调用凭据提取**

文件: `pkg/dashboard/handler.go:1-15`（替换 import 块）

```go
package dashboard

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/auth"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/joycode"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/store"
)
```

- [ ] **Step 2: 注册 auto-login 路由 — 添加 POST /api/accounts/auto-login**

文件: `pkg/dashboard/handler.go:31-39`（替换 RegisterRoutes 方法）

```go
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/accounts", h.handleAccounts)
	mux.HandleFunc("/api/accounts/", h.handleAccountAction)
	mux.HandleFunc("/api/accounts-auto-login", h.handleAutoLogin)
	mux.HandleFunc("/api/models", h.handleModels)
	mux.HandleFunc("/api/stats", h.handleStats)
	mux.HandleFunc("/api/settings", h.handleSettings)
	mux.HandleFunc("/api/health", h.handleHealth)
	mux.HandleFunc("/api/errors", h.handleErrors)
}
```

注意：使用 `/api/accounts-auto-login` 而非 `/api/accounts/auto-login`，避免被 `handleAccountAction` 的 `/api/accounts/` 前缀匹配截获。

- [ ] **Step 3: 实现 handleAutoLogin — 从本机提取凭据、验证、保存账号**

文件: `pkg/dashboard/handler.go`（在 `addAccount` 方法之后添加，约第 201 行之后）

```go
// handleAutoLogin reads credentials from the local JoyCode IDE installation,
// validates them, and saves as a proxy account.
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

	// 1. Extract credentials from local JoyCode IDE
	creds, err := auth.LoadFromSystem()
	if err != nil {
		slog.Error("auto-login: load from system failed", "error", err)
		writeError(w, http.StatusBadRequest, "无法从本机获取 JoyCode 凭据: "+err.Error())
		return
	}

	// 2. Validate credentials with JoyCode API and fetch user info
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

	// 3. Generate api_key from realName (fallback to userId)
	apiKey := creds.UserID
	realName := ""
	if data, ok := userInfo["data"].(map[string]interface{}); ok {
		if name, ok := data["realName"].(string); ok && name != "" {
			apiKey = name
			realName = name
		}
	}

	// 4. Determine if this should be the default account
	isDefault := true
	accounts, _ := h.store.ListAccounts()
	for _, a := range accounts {
		if a.IsDefault {
			isDefault = false
			break
		}
	}

	// 5. Save account (INSERT OR REPLACE handles re-login)
	if err := h.store.AddAccount(apiKey, creds.PtKey, creds.UserID, isDefault, "GLM-5.1"); err != nil {
		slog.Error("auto-login: save account failed", "api_key", apiKey, "error", err)
		writeError(w, http.StatusInternalServerError, "保存账号失败: "+err.Error())
		return
	}

	slog.Info("auto-login: account saved", "api_key", apiKey, "user_id", creds.UserID, "real_name", realName)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":        true,
		"api_key":   apiKey,
		"user_id":   creds.UserID,
		"real_name": realName,
		"is_default": isDefault,
	})
}
```

- [ ] **Step 4: 验证编译**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./pkg/dashboard/`
Expected:
  - Exit code: 0

- [ ] **Step 5: 提交**

Run: `git add pkg/dashboard/handler.go && git commit -m "feat(dashboard): add auto-login endpoint for one-click JoyCode credential import"`

---

### Task 2: Frontend — 一键登录 UI

**Depends on:** Task 1
**Files:**
- Modify: `web/src/api.ts:60-91`（添加 autoLogin 方法）
- Modify: `web/src/pages/Accounts.tsx:232-241`（添加一键登录按钮和逻辑）

- [ ] **Step 1: 添加 autoLogin API 方法 — 调用后端一键登录端点**

文件: `web/src/api.ts:60-91`（在 `renewToken` 之后、`getRecentErrors` 之前添加）

在 `renewToken` 行之后添加:

```typescript
  autoLogin: () =>
    request<{ ok: boolean; api_key: string; user_id: string; real_name: string; is_default: boolean }>('/api/accounts-auto-login', { method: 'POST' }),
```

- [ ] **Step 2: 添加一键登录按钮和逻辑 — Accounts 页面**

文件: `web/src/pages/Accounts.tsx`

2a. 添加 state 变量（第 62 行 `const [validating, setValidating]` 之后）:

```typescript
  const [autoLogging, setAutoLogging] = useState(false);
```

2b. 添加 handleAutoLogin 函数（第 88 行 `handleAdd` 之后）:

```typescript
  const handleAutoLogin = async () => {
    setAutoLogging(true);
    try {
      const result = await api.autoLogin();
      message.success(`一键登录成功！账号「${result.api_key}」已添加`);
      fetchAccounts();
    } catch (e: unknown) {
      message.error(e instanceof Error ? e.message : '一键登录失败');
    } finally {
      setAutoLogging(false);
    }
  };
```

2c. 修改按钮区域（第 234-240 行，替换 Space 中的按钮部分）:

将原来的:
```tsx
        <Space>
          <Button onClick={fetchAccounts} icon={<ReloadOutlined />}>刷新</Button>
          <Button type="primary" onClick={() => setModalOpen(true)} icon={<PlusOutlined />}>
            添加账号
          </Button>
        </Space>
```

替换为:
```tsx
        <Space>
          <Button onClick={fetchAccounts} icon={<ReloadOutlined />}>刷新</Button>
          <Button
            type="primary"
            onClick={handleAutoLogin}
            loading={autoLogging}
            icon={<SafetyCertificateOutlined />}
          >
            一键登录
          </Button>
          <Button onClick={() => setModalOpen(true)} icon={<PlusOutlined />}>
            手动添加
          </Button>
        </Space>
```

2d. 修改添加账号 Modal 标题说明（第 273-279 行的 Alert）:

将原来的:
```tsx
        <Alert
          type="info"
          showIcon
          message="添加账号说明"
          description="将 JoyCode 客户端的凭证信息填入下方表单。添加后，Claude Code 使用对应的路由密钥即可通过此账号访问 JoyCode 后端。"
          style={{ marginBottom: 16 }}
        />
```

替换为:
```tsx
        <Alert
          type="info"
          showIcon
          message="手动添加账号"
          description="填写 JoyCode 客户端凭证信息。推荐使用「一键登录」自动导入，此处适合手动配置多个账号。"
          style={{ marginBottom: 16 }}
        />
```

2e. 修改 Modal 标题（第 265 行）:

将 `title="添加 JoyCode 账号"` 改为 `title="手动添加 JoyCode 账号"`

- [ ] **Step 3: 验证前端构建**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npm run build`
Expected:
  - Exit code: 0
  - Output contains: "built in"

- [ ] **Step 4: 提交**

Run: `git add web/src/api.ts web/src/pages/Accounts.tsx && git commit -m "feat(web): add one-click login button to accounts page"`

---

### Task 3: 构建部署和验证

**Depends on:** Task 2
**Files:**
- Modify: `cmd/JoyCodeProxy/static/`（前端产物）

- [ ] **Step 1: 复制前端产物到 Go 静态目录**

Run: `cp -r /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web/dist/* /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/cmd/JoyCodeProxy/static/`
Expected:
  - Exit code: 0
  - `cmd/JoyCodeProxy/static/index.html` 更新时间戳变化

- [ ] **Step 2: 构建 Go 二进制**

Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build -o joycode_proxy_bin ./cmd/JoyCodeProxy/`
Expected:
  - Exit code: 0

- [ ] **Step 3: 部署到本地服务**

Run: `launchctl unload ~/Library/LaunchAgents/com.joycode.proxy.plist 2>/dev/null; sleep 1; cp /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/joycode_proxy_bin /usr/local/bin/joycode_proxy_bin && launchctl load ~/Library/LaunchAgents/com.joycode.proxy.plist`
Expected:
  - Exit code: 0
  - No error output

- [ ] **Step 4: 验证 auto-login 端点可用**

Run: `sleep 2 && curl -s -X POST http://localhost:34891/api/accounts-auto-login | python3 -m json.tool`
Expected:
  - Returns JSON with `ok: true` or error with clear message about JoyCode not being installed/logged in

- [ ] **Step 5: 提交**

Run: `git add cmd/JoyCodeProxy/static/ && git commit -m "build: deploy frontend with auto-login feature"`
