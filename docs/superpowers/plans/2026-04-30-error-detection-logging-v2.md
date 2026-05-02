# Error Detection and Logging Enhancement Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 补全 store 层零日志的空白，添加 request ID 追踪，使错误日志足以自我诊断问题。

**Architecture:** 请求进入 → request ID middleware 分配唯一 ID → 各层日志携带 request_id → store 层操作失败时记录 slog.Error → requestLogMiddleware 汇总记录到 SQLite → Dashboard 可查询错误日志。

**Tech Stack:** Go 1.23, log/slog (标准库), SQLite (go-sqlite3)

**Risks:**
- Store 层日志量大（每个 CRUD 都有日志）→ 缓解：仅错误路径用 slog.Error，成功路径不记录
- Request ID 增加开销 → 缓解：用计数器而非 UUID，开销可忽略

---

### Task 1: Store 层错误日志

**Depends on:** None
**Files:**
- Modify: `pkg/store/store.go`（约 15 处错误路径添加 slog.Error）

- [ ] **Step 1: 添加 slog import 到 store.go**

文件: `pkg/store/store.go:3-16`（import 块）

在 import 中添加 `"log/slog"`：

```go
import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)
```

- [ ] **Step 2: 添加 slog.Error 到所有数据库操作错误路径**

逐一修改以下方法中的错误返回：

**AddAccount** (`pkg/store/store.go:263-287`) — 添加加密失败和 SQL 执行失败日志：

```go
func (s *Store) AddAccount(apiKey, ptKey, userID string, isDefault bool, defaultModel string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	encPtKey, err := s.encrypt(ptKey)
	if err != nil {
		slog.Error("store: encrypt pt_key failed", "api_key", apiKey, "error", err)
		return fmt.Errorf("encrypt pt_key: %w", err)
	}

	if isDefault {
		s.db.Exec("UPDATE accounts SET is_default = 0 WHERE is_default = 1")
	}

	def := 0
	if isDefault {
		def = 1
	}

	token := generateToken()
	_, err = s.db.Exec(
		"INSERT OR REPLACE INTO accounts (api_key, api_token, pt_key, user_id, is_default, default_model) VALUES (?, ?, ?, ?, ?, ?)",
		apiKey, token, encPtKey, userID, def, defaultModel,
	)
	if err != nil {
		slog.Error("store: add account failed", "api_key", apiKey, "error", err)
		return err
	}
	return nil
}
```

**ListAccounts** (`pkg/store/store.go:289-307`) — 查询和扫描失败日志：

```go
func (s *Store) ListAccounts() ([]AccountInfo, error) {
	rows, err := s.db.Query("SELECT api_key, api_token, user_id, is_default, default_model, created_at FROM accounts ORDER BY created_at")
	if err != nil {
		slog.Error("store: list accounts query failed", "error", err)
		return nil, err
	}
	defer rows.Close()

	var accounts []AccountInfo
	for rows.Next() {
		var a AccountInfo
		var isDef int
		if err := rows.Scan(&a.APIKey, &a.APIToken, &a.UserID, &isDef, &a.DefaultModel, &a.CreatedAt); err != nil {
			slog.Error("store: list accounts scan failed", "error", err)
			return nil, err
		}
		a.IsDefault = isDef == 1
		accounts = append(accounts, a)
	}
	if err := rows.Err(); err != nil {
		slog.Error("store: list accounts iteration failed", "error", err)
		return nil, err
	}
	return accounts, nil
}
```

**GetAccount** (`pkg/store/store.go:309-331`) — 查询失败日志：

```go
func (s *Store) GetAccount(apiKey string) (*Account, error) {
	var a Account
	var encPtKey string
	var isDef int
	err := s.db.QueryRow(
		"SELECT api_key, api_token, pt_key, user_id, is_default, default_model, created_at FROM accounts WHERE api_key = ?",
		apiKey,
	).Scan(&a.APIKey, &a.APIToken, &encPtKey, &a.UserID, &isDef, &a.DefaultModel, &a.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		slog.Error("store: get account query failed", "api_key", apiKey, "error", err)
		return nil, err
	}

	ptKey, err := s.decrypt(encPtKey)
	if err != nil {
		slog.Error("store: decrypt pt_key failed", "api_key", apiKey, "error", err)
		return nil, fmt.Errorf("decrypt pt_key: %w", err)
	}
	a.PtKey = ptKey
	a.IsDefault = isDef == 1
	return &a, nil
}
```

**GetAccountByToken** (`pkg/store/store.go:333-355`) — token 查询失败日志：

```go
func (s *Store) GetAccountByToken(token string) (*Account, error) {
	var a Account
	var encPtKey string
	var isDef int
	err := s.db.QueryRow(
		"SELECT api_key, api_token, pt_key, user_id, is_default, default_model, created_at FROM accounts WHERE api_token = ?",
		token,
	).Scan(&a.APIKey, &a.APIToken, &encPtKey, &a.UserID, &isDef, &a.DefaultModel, &a.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		slog.Error("store: get account by token query failed", "error", err)
		return nil, err
	}

	ptKey, err := s.decrypt(encPtKey)
	if err != nil {
		slog.Error("store: decrypt pt_key by token failed", "error", err)
		return nil, fmt.Errorf("decrypt pt_key: %w", err)
	}
	a.PtKey = ptKey
	a.IsDefault = isDef == 1
	return &a, nil
}
```

**GetDefaultAccount / RemoveAccount / SetDefault / UpdateAccountModel** — 同样模式添加 slog.Error：

GetDefaultAccount 解密失败加 `slog.Error("store: decrypt default account pt_key failed", "error", err)`

RemoveAccount: `slog.Error("store: remove account failed", "api_key", apiKey, "error", err)`

SetDefault 事务失败: `slog.Error("store: set default transaction failed", "api_key", apiKey, "error", err)`

UpdateAccountModel: `slog.Error("store: update account model failed", "api_key", apiKey, "model", model, "error", err)`

**GetSettings / SetSetting / SetSettings / LogRequest / GetStats / GetAccountStats / GetAccountLogs / GetRecentLogs** — 所有返回 err 的路径加 slog.Error，消息格式统一为 `"store: {method} failed"`。

**Open** 方法中 `s.db.SetMaxOpenConns` 等初始化无需加日志（启动时 log.Printf 已覆盖）。

- [ ] **Step 3: 验证编译通过**
Run: `go build ./...`
Expected:
  - Exit code: 0
  - No output

- [ ] **Step 4: 提交**
Run: `git add pkg/store/store.go && git commit -m "feat(store): add structured error logging to all database operations"`

---

### Task 2: Request ID 中间件 + Dashboard handleStats 修复

**Depends on:** Task 1
**Files:**
- Modify: `cmd/JoyCodeProxy/serve.go:196-250`（requestLogMiddleware 增加 request_id）
- Modify: `pkg/dashboard/handler.go:368-391`（handleStats 缺少错误日志）

- [ ] **Step 1: 添加 request ID 到 requestLogMiddleware**

文件: `cmd/JoyCodeProxy/serve.go` — 在 import 块添加 `"sync/atomic"`，修改 requestLogMiddleware 函数。

在文件级别添加计数器变量：

```go
var requestCounter uint64
```

修改 requestLogMiddleware 在日志中添加 request_id 字段。在错误日志处添加：

```go
reqID := atomic.AddUint64(&requestCounter, 1)
```

在 `slog.Error("proxy error response", ...)` 调用中添加 `"request_id", reqID` 字段。

完整的 requestLogMiddleware 函数（替换现有函数）：

```go
func requestLogMiddleware(next http.Handler, s *store.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Peek at body to extract model before handler consumes it
		var model string
		if r.Method == "POST" && r.Body != nil {
			bodyBytes, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			var body map[string]interface{}
			if json.Unmarshal(bodyBytes, &body) == nil {
				if m, ok := body["model"].(string); ok {
					model = m
				}
			}
		}

		rw := &responseWriter{ResponseWriter: w, statusCode: 200}
		next.ServeHTTP(rw, r)

		// Log /v1/ requests
		path := r.URL.Path
		if strings.HasPrefix(path, "/v1/") {
			reqID := atomic.AddUint64(&requestCounter, 1)

			apiKey := r.Header.Get("x-api-key")
			if apiKey == "" {
				apiKey = r.Header.Get("Authorization")
				if strings.HasPrefix(apiKey, "Bearer ") {
					apiKey = strings.TrimPrefix(apiKey, "Bearer ")
				}
			}
			if apiKey != "" {
				if account, _ := s.GetAccountByToken(apiKey); account != nil {
					apiKey = account.APIKey
				}
			}
			if apiKey == "" {
				if a, _ := s.GetDefaultAccount(); a != nil {
					apiKey = a.APIKey
				}
			}

			isStream := r.URL.Query().Get("stream") != "" || path == "/v1/messages"
			latency := time.Since(start).Milliseconds()

			if rw.statusCode >= 400 {
				slog.Error("proxy error response",
					"request_id", reqID,
					"status", rw.statusCode,
					"method", r.Method,
					"path", path,
					"model", model,
					"latency_ms", latency,
					"api_key", apiKey,
				)
			}

			go s.LogRequest(apiKey, model, path, isStream, rw.statusCode, latency)
		}
	})
}
```

同步添加 `"sync/atomic"` 到 import 块。

- [ ] **Step 2: 修复 handleStats 缺失的错误日志**

文件: `pkg/dashboard/handler.go:368-391`（handleStats 函数）

在 `GetStats()` 错误返回前添加 slog.Error：

```go
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
	writeJSON(w, http.StatusOK, stats)
}
```

- [ ] **Step 3: 验证编译通过**
Run: `go build ./...`
Expected:
  - Exit code: 0
  - No output

- [ ] **Step 4: 提交**
Run: `git add cmd/JoyCodeProxy/serve.go pkg/dashboard/handler.go && git commit -m "feat(middleware): add request ID tracking and fix handleStats error logging"`

---

### Task 3: Dashboard 错误查询 API

**Depends on:** Task 1
**Files:**
- Modify: `pkg/store/store.go`（添加 GetRecentErrors 方法）
- Modify: `pkg/dashboard/handler.go:31-38`（RegisterRoutes 添加错误查询路由）
- Modify: `web/src/api.ts`（添加 getRecentErrors API 调用）
- Modify: `web/src/pages/Dashboard.tsx`（显示最近错误列表）

- [ ] **Step 1: 添加 GetRecentErrors 方法到 store**

文件: `pkg/store/store.go`（在 GetRecentLogs 方法之后添加）

```go
func (s *Store) GetRecentErrors(limit int) ([]RequestLog, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(
		"SELECT id, api_key, model, endpoint, stream, status_code, latency_ms, created_at FROM request_logs WHERE status_code >= 400 ORDER BY id DESC LIMIT ?",
		limit,
	)
	if err != nil {
		slog.Error("store: get recent errors query failed", "error", err)
		return nil, err
	}
	defer rows.Close()

	var logs []RequestLog
	for rows.Next() {
		var l RequestLog
		var streamInt int
		if err := rows.Scan(&l.ID, &l.APIKey, &l.Model, &l.Endpoint, &streamInt, &l.StatusCode, &l.LatencyMs, &l.CreatedAt); err != nil {
			slog.Error("store: get recent errors scan failed", "error", err)
			return nil, err
		}
		l.Stream = streamInt == 1
		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		slog.Error("store: get recent errors iteration failed", "error", err)
		return nil, err
	}
	return logs, nil
}
```

- [ ] **Step 2: 注册错误查询路由到 Dashboard handler**

文件: `pkg/dashboard/handler.go:31-38`（RegisterRoutes 方法）

在 `mux.HandleFunc("/api/health", h.handleHealth)` 之后添加：

```go
mux.HandleFunc("/api/errors", h.handleErrors)
```

在文件末尾添加 handleErrors 方法：

```go
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
```

- [ ] **Step 3: 添加前端 API 调用**

文件: `web/src/api.ts`（在现有 API 方法之后添加）

```typescript
getRecentErrors: (limit = 50) =>
  request<{ errors: RequestLog[]; total: number }>(`/api/errors?limit=${limit}`),
```

- [ ] **Step 4: Dashboard 页面显示错误统计**

文件: `web/src/pages/Dashboard.tsx`（在现有 stats 卡片区域添加错误卡片）

在 Dashboard 组件中添加错误获取逻辑和显示：

```typescript
// 在组件内添加状态
const [errors, setErrors] = useState<RequestLog[]>([]);

// 在 fetchData 函数中添加
api.getRecentErrors(10).then(data => setErrors(data.errors)).catch(() => {});

// 在 stats 卡片区域之后添加错误列表
{errors.length > 0 && (
  <div className="bg-white rounded-lg shadow p-6 mt-6">
    <h3 className="text-lg font-semibold text-red-600 mb-4">Recent Errors</h3>
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b text-left text-gray-500">
            <th className="pb-2">Time</th>
            <th className="pb-2">Endpoint</th>
            <th className="pb-2">Model</th>
            <th className="pb-2">Status</th>
            <th className="pb-2">Latency</th>
          </tr>
        </thead>
        <tbody>
          {errors.map(e => (
            <tr key={e.id} className="border-b last:border-0">
              <td className="py-2 text-gray-600">{e.created_at}</td>
              <td className="py-2 font-mono text-xs">{e.endpoint}</td>
              <td className="py-2">{e.model || '-'}</td>
              <td className="py-2"><span className="text-red-600 font-semibold">{e.status_code}</span></td>
              <td className="py-2">{e.latency_ms}ms</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  </div>
)}
```

- [ ] **Step 5: 构建前端 + 后端并验证**
Run: `cd web && npm run build && cd .. && go build ./...`
Expected:
  - Exit code: 0
  - No errors

- [ ] **Step 6: 提交**
Run: `git add pkg/store/store.go pkg/dashboard/handler.go web/src/api.ts web/src/pages/Dashboard.tsx && git commit -m "feat(dashboard): add error query API and recent errors display"`
