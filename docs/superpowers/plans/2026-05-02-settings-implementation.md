# Settings Page: Wire All "规划中" Settings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 将 Settings 页面中 4 个未生效的配置项（`default_model`, `request_timeout`, `max_connections`, `log_retention_days`）全部接入后端，使每个配置项保存后立即生效。

**Architecture:** 前端保存设置到 SQLite → 后端读取 settings 表值应用到运行时。`default_model` 通过 `resolveModel()` 参数传递；`request_timeout` 通过 `joycode.Client.SetTimeout()` 注入；`max_connections` 通过共享 `http.Transport` 的 `MaxConnsPerHost` 控制；`log_retention_days` 通过后台 goroutine 定时执行 SQL DELETE 清理。

**Tech Stack:** Go 1.x, net/http, SQLite, React + Ant Design

**Risks:**
- `max_connections` 修改 Transport 影响所有上游连接 → 缓解：使用 `sync.Once` 确保安全初始化
- `log_retention_days` 后台 goroutine 需随 server 退出 → 缓解：使用 context 取消
- `request_timeout` 设过小会导致长回复截断 → 缓解：UI 提示最小值 60 秒

---

### Task 1: Wire `default_model` setting — system-level fallback for model resolution

**Depends on:** None
**Files:**
- Modify: `pkg/anthropic/translate.go:121-131` (resolveModel function)
- Modify: `pkg/anthropic/handler.go:76-78` (handleMessages reads setting)
- Modify: `pkg/openai/translate.go:106-116` (ResolveModel function)
- Modify: `pkg/openai/chat.go:23` (handleChat passes system default)
- Modify: `web/src/pages/Settings.tsx:31-47` (add "已生效" tag)

- [ ] **Step 1: Modify `resolveModel()` to accept system default — allow settings DB to override hardcoded default**

文件: `pkg/anthropic/translate.go:121-131`（替换整个 resolveModel 函数）

```go
func resolveModel(model string, accountDefault string, systemDefault string) string {
	for _, m := range joycode.Models {
		if m == model {
			return model
		}
	}
	if accountDefault != "" {
		return accountDefault
	}
	if systemDefault != "" {
		return systemDefault
	}
	return joycode.DefaultModel
}
```

- [ ] **Step 2: Update Anthropic handler to read `default_model` setting from store**

文件: `pkg/anthropic/handler.go:76-78`（替换 model resolution 区块）

```go
		accountDefault := store.GetAccountDefaultModel(r)
		systemDefault := ""
		if h.store != nil {
			systemDefault = h.store.GetSetting("default_model")
		}
		resolved := resolveModel(req.Model, accountDefault, systemDefault)
		store.SetModel(r, resolved)
```

- [ ] **Step 3: Update Anthropic TranslateRequest call sites to pass systemDefault**

文件: `pkg/anthropic/handler.go:91`（handleNonStream 中的 TranslateRequest 调用前）

```go
	systemDefault := ""
	if h.store != nil {
		systemDefault = h.store.GetSetting("default_model")
	}
	jcBody := TranslateRequest(req, store.GetAccountDefaultModel(r), systemDefault)
```

文件: `pkg/anthropic/handler.go:168`（handleStream 中的 TranslateRequest 调用前）

```go
	systemDefault := ""
	if h.store != nil {
		systemDefault = h.store.GetSetting("default_model")
	}
	jcBody := TranslateRequest(req, store.GetAccountDefaultModel(r), systemDefault)
```

文件: `pkg/anthropic/handler.go:106`（non-stream retry 中的 TranslateRequest）

```go
	jcBody = TranslateRequest(req, store.GetAccountDefaultModel(r), systemDefault)
```

文件: `pkg/anthropic/handler.go:179`（stream retry 中的 TranslateRequest）

```go
	jcBody = TranslateRequest(req, store.GetAccountDefaultModel(r), systemDefault)
```

- [ ] **Step 4: Update OpenAI ResolveModel to accept system default**

文件: `pkg/openai/translate.go:106-116`（替换整个 ResolveModel 函数）

```go
func ResolveModel(model string, accountDefault string, systemDefault string) string {
	for _, m := range joycode.Models {
		if m == model {
			return model
		}
	}
	if accountDefault != "" {
		return accountDefault
	}
	if systemDefault != "" {
		return systemDefault
	}
	return joycode.DefaultModel
}
```

- [ ] **Step 5: Update OpenAI chat handler to pass system default**

文件: `pkg/openai/chat.go:23`（替换 model resolution 行）

由于 OpenAI Server 没有 store 引用，需要在 Server struct 中添加 store 字段。

文件: `pkg/openai/handler.go:15-23`（替换 Server struct 和构造函数）

```go
type Server struct {
	Client   *joycode.Client
	Resolver ClientResolver
	store    *store.Store
}

func NewServer(c *joycode.Client, s *store.Store) *Server {
	return &Server{Client: c, store: s}
}
```

文件: `pkg/openai/chat.go:23`（替换 model resolution 行）

```go
	systemDefault := ""
	if s.store != nil {
		systemDefault = s.store.GetSetting("default_model")
	}
	model := ResolveModel(req.Model, store.GetAccountDefaultModel(r), systemDefault)
```

- [ ] **Step 6: Update serve.go to pass store to NewServer**

文件: `cmd/JoyCodeProxy/serve.go:79`（替换 NewServer 调用）

```go
			srv := openai.NewServer(client, s)
```

- [ ] **Step 7: Update frontend Settings.tsx — add "已生效" tag to default_model**

文件: `web/src/pages/Settings.tsx:31-47`（在 default_model 字段配置中添加 tag 属性）

在 `key: 'default_model'` 字段配置中添加:
```typescript
tag: '已生效',
```

- [ ] **Step 8: Build and verify**
Run: `go build -o joycode_proxy_bin ./cmd/JoyCodeProxy`
Expected:
  - Exit code: 0
  - No compilation errors

---

### Task 2: Wire `request_timeout` setting — configurable upstream request timeout

**Depends on:** None
**Files:**
- Modify: `pkg/joycode/client.go:43-49` (NewClient and add SetTimeout)
- Modify: `cmd/JoyCodeProxy/serve.go:84-104` (resolver sets timeout)

- [ ] **Step 1: Add `SetTimeout()` method to joycode.Client — allow external timeout configuration**

文件: `pkg/joycode/client.go:43-56`（替换 NewClient 和 SetHTTPClient，添加 SetTimeout）

```go
func NewClient(ptKey, userID string) *Client {
	return &Client{
		PtKey:      ptKey,
		UserID:     userID,
		SessionID:  newHexID(),
		httpClient: &http.Client{Timeout: 30 * time.Minute},
	}
}

func (c *Client) SetHTTPClient(hc *http.Client) {
	c.httpClient = hc
}

func (c *Client) SetTimeout(d time.Duration) {
	c.httpClient.Timeout = d
}
```

- [ ] **Step 2: Wire `request_timeout` in resolver — read from settings on each request**

文件: `cmd/JoyCodeProxy/serve.go:84-104`（在 resolver 中，`joycode.NewClient` 之后添加超时设置）

在 `return joycode.NewClient(account.PtKey, account.UserID)` 之前插入超时逻辑:

```go
				resolver := func(r *http.Request) *joycode.Client {
					// ... existing key resolution logic ...
					timeout := 120
					if s != nil {
						timeout = s.GetIntSetting("request_timeout", 120)
					}
					if timeout < 60 {
						timeout = 60
					}
					cl := joycode.NewClient(ptKey, userID)
					cl.SetTimeout(time.Duration(timeout) * time.Second)
					return cl
				}
```

具体修改方式：在每个 `return joycode.NewClient(...)` 调用处，改为先创建 client，设置 timeout，再 return。将 resolver 内部的 4 个 return 语句统一处理。

- [ ] **Step 3: Update frontend tooltip — remove "规划中" tag**

文件: `web/src/pages/Settings.tsx:70-77`（request_timeout 字段）

```typescript
      {
        key: 'request_timeout',
        label: '请求超时（秒）',
        tooltip: '与 JoyCode 后端通信的读取超时时间，低于 60 秒会自动调整为 60 秒',
        placeholder: '120',
        type: 'number' as const,
        suffix: '秒',
        tag: '已生效',
      },
```

- [ ] **Step 4: Build and verify**
Run: `go build -o joycode_proxy_bin ./cmd/JoyCodeProxy`
Expected:
  - Exit code: 0

---

### Task 3: Wire `max_connections` setting — limit concurrent upstream connections

**Depends on:** Task 2
**Files:**
- Modify: `cmd/JoyCodeProxy/serve.go:79-107` (shared transport, inject into clients)
- Modify: `pkg/joycode/client.go:53-55` (add SetTransport method)

- [ ] **Step 1: Add `SetTransport()` method to joycode.Client — inject shared transport**

文件: `pkg/joycode/client.go:53-55`（在 SetHTTPClient 后面添加）

```go
func (c *Client) SetTransport(transport http.RoundTripper) {
	c.httpClient.Transport = transport
}
```

- [ ] **Step 2: Create shared transport in serve.go — configure connection limits from settings**

文件: `cmd/JoyCodeProxy/serve.go`（在 `mux := http.NewServeMux()` 之前添加）

```go
			// Shared transport for connection pooling and limits
			sharedTransport := &http.Transport{
				MaxIdleConnsPerHost: 10,
				MaxConnsPerHost:     20,
				IdleConnTimeout:     90 * time.Second,
			}

			// Background goroutine to sync max_connections setting to transport
			if s != nil {
				go func() {
					ticker := time.NewTicker(10 * time.Second)
					defer ticker.Stop()
					for range ticker.C {
						maxConns := s.GetIntSetting("max_connections", 20)
						if maxConns < 1 {
							maxConns = 1
						}
						sharedTransport.MaxConnsPerHost = maxConns
						sharedTransport.MaxIdleConnsPerHost = maxConns / 2
						if sharedTransport.MaxIdleConnsPerHost < 2 {
							sharedTransport.MaxIdleConnsPerHost = 2
						}
					}
				}()
			}
```

- [ ] **Step 3: Inject shared transport into resolver clients — ensure all clients share the transport**

在 resolver 中的每个 `cl := joycode.NewClient(...)` 之后添加:

```go
					cl.SetTransport(sharedTransport)
```

- [ ] **Step 4: Also inject transport into the default client**

文件: `cmd/JoyCodeProxy/serve.go`（在 `client, err := resolveClient()` 之后添加）

```go
			client.SetTransport(sharedTransport)
```

- [ ] **Step 5: Update frontend tooltip — remove "规划中" tag**

文件: `web/src/pages/Settings.tsx:78-86`（max_connections 字段）

```typescript
      {
        key: 'max_connections',
        label: '最大连接数',
        tooltip: '与 JoyCode 后端的最大并发 HTTP 连接数，修改后 10 秒内自动生效',
        placeholder: '20',
        type: 'number' as const,
        tag: '已生效',
      },
```

- [ ] **Step 6: Build and verify**
Run: `go build -o joycode_proxy_bin ./cmd/JoyCodeProxy`
Expected:
  - Exit code: 0

---

### Task 4: Wire `log_retention_days` setting — automatic log cleanup

**Depends on:** None
**Files:**
- Modify: `pkg/store/store.go` (add CleanupOldLogs method)
- Modify: `cmd/JoyCodeProxy/serve.go` (start background cleanup goroutine)
- Modify: `web/src/pages/Settings.tsx:88-109` (update tooltip and tag)

- [ ] **Step 1: Add `CleanupOldLogs()` method to Store — delete logs older than N days**

文件: `pkg/store/store.go`（在 `GetRecentErrors` 方法后面添加）

```go
// CleanupOldLogs deletes request logs older than the specified number of days.
// If days is 0, no cleanup is performed (permanent retention).
func (s *Store) CleanupOldLogs(days int) (int64, error) {
	if days <= 0 {
		return 0, nil
	}
	result, err := s.db.Exec(
		"DELETE FROM request_logs WHERE created_at < datetime('now', 'localtime', '-' || ? || ' days')",
		days,
	)
	if err != nil {
		slog.Error("store: cleanup old logs failed", "days", days, "error", err)
		return 0, err
	}
	affected, _ := result.RowsAffected()
	if affected > 0 {
		slog.Info("store: cleaned up old logs", "days", days, "deleted", affected)
	}
	return affected, nil
}
```

- [ ] **Step 2: Start background cleanup goroutine in serve.go**

文件: `cmd/JoyCodeProxy/serve.go`（在 `mux := http.NewServeMux()` 之前添加）

```go
			// Background log cleanup goroutine
			if s != nil {
				go func() {
					ticker := time.NewTicker(1 * time.Hour)
					defer ticker.Stop()
					// Run once at startup
					if days := s.GetIntSetting("log_retention_days", 30); days > 0 {
						s.CleanupOldLogs(days)
					}
					for range ticker.C {
						if days := s.GetIntSetting("log_retention_days", 30); days > 0 {
							s.CleanupOldLogs(days)
						}
					}
				}()
			}
```

- [ ] **Step 3: Update frontend tooltip — remove "规划中" tag**

文件: `web/src/pages/Settings.tsx:100-109`（log_retention_days 字段）

```typescript
      {
        key: 'log_retention_days',
        label: '日志保留天数',
        tooltip: '请求日志的自动清理周期。超过此天数的日志将每小时自动清理，0 表示永久保留',
        placeholder: '30',
        type: 'number' as const,
        suffix: '天',
        tag: '已生效',
      },
```

- [ ] **Step 4: Build and verify**
Run: `go build -o joycode_proxy_bin ./cmd/JoyCodeProxy`
Expected:
  - Exit code: 0

- [ ] **Step 5: Deploy — rebuild frontend, rebuild binary, restart service**

Run frontend build:
```bash
cd web && npm run build
```

Copy built assets and rebuild Go binary:
```bash
rm -rf cmd/JoyCodeProxy/static/assets && cp -r web/dist/* cmd/JoyCodeProxy/static/ && go build -o joycode_proxy_bin ./cmd/JoyCodeProxy
```

Restart service:
```bash
ps aux | grep joycode_proxy_bin | grep -v grep | awk '{print $2}' | xargs kill && sleep 4 && curl -s http://127.0.0.1:34891/api/health
```

Expected:
  - Output contains: `"status":"ok"`

- [ ] **Step 6: Commit**
Run: `git add pkg/anthropic/handler.go pkg/anthropic/translate.go pkg/openai/handler.go pkg/openai/chat.go pkg/openai/translate.go pkg/joycode/client.go pkg/store/store.go cmd/JoyCodeProxy/serve.go web/src/pages/Settings.tsx && git commit -m "feat(settings): wire all settings to runtime — default_model, request_timeout, max_connections, log_retention_days"`
