# Bug Fix: 错误日志详情 + 日志增强

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 1) 给 request_logs 增加 error_message 字段存储错误详情；2) AccountDetail 页面日志行可展开查看详细信息；3) 代理错误时保存错误消息到日志。

**Root Cause:** request_logs 表缺少 error_message 字段，500 错误只记录了状态码没有错误原因。用户看到的全是 500 但无法知道具体原因。

**Architecture:** SQLite migration 添加 error_message 列 → Go LogRequest 增加 errMsg 参数 → requestLogMiddleware 在错误时提取错误消息 → 前端 RequestLog 类型添加 error_message → AccountDetail 日志 Table 支持行展开显示详情。

**Tech Stack:** Go 1.22, SQLite, React 18, Ant Design 5

**Risks:**
- SQLite ALTER TABLE 添加列是安全的，不影响现有数据 → 缓解：已是标准操作
- 前端改动仅影响日志展示，不影响代理功能 → 缓解：低风险

---

### Task 1: 后端 — request_logs 增加 error_message 字段

**Depends on:** None
**Files:**
- Modify: `pkg/store/store.go`（RequestLog struct, migration, LogRequest, GetRecentErrors）

- [ ] **Step 1: 添加 error_message 到 RequestLog 结构体**

文件: `pkg/store/store.go:80-89`（替换 RequestLog struct）

```go
type RequestLog struct {
	ID           int64  `json:"id"`
	APIKey       string `json:"api_key"`
	Model        string `json:"model"`
	Endpoint     string `json:"endpoint"`
	Stream       bool   `json:"stream"`
	StatusCode   int    `json:"status_code"`
	LatencyMs    int64  `json:"latency_ms"`
	ErrorMessage string `json:"error_message"`
	CreatedAt    string `json:"created_at"`
}
```

- [ ] **Step 2: 添加 migration 为 request_logs 表增加 error_message 列**

文件: `pkg/store/store.go`（在 `CREATE TABLE IF NOT EXISTS request_logs` 语句之后，`})` 之前添加）

在 `s.db.Exec(...)` 的 multi-statement SQL 末尾添加一条 migration：

```sql
ALTER TABLE request_logs ADD COLUMN error_message TEXT DEFAULT '';
```

具体位置：在现有的 `CREATE TABLE IF NOT EXISTS request_logs` 的 `);` 之后加一行。

- [ ] **Step 3: 修改 LogRequest 方法 — 增加 errMsg 参数**

文件: `pkg/store/store.go:502-515`（替换 LogRequest 函数）

```go
func (s *Store) LogRequest(apiKey, model, endpoint string, stream bool, statusCode int, latencyMs int64, errMsg string) error {
	sInt := 0
	if stream {
		sInt = 1
	}
	_, err := s.db.Exec(
		"INSERT INTO request_logs (api_key, model, endpoint, stream, status_code, latency_ms, error_message) VALUES (?, ?, ?, ?, ?, ?, ?)",
		apiKey, model, endpoint, sInt, statusCode, latencyMs, errMsg,
	)
	if err != nil {
		slog.Error("store: log request failed", "api_key", apiKey, "endpoint", endpoint, "error", err)
	}
	return err
}
```

- [ ] **Step 4: 更新所有读取 RequestLog 的查询 — 添加 error_message 列**

找到所有 `rows.Scan(&l.ID, &l.APIKey, ...)` 调用，在末尾添加 `&l.ErrorMessage`。

文件: `pkg/store/store.go` 中 GetAccountLogs, GetRecentLogs, GetRecentErrors 三个方法的 Scan 调用。

- [ ] **Step 5: 修改 requestLogMiddleware 传递错误消息**

文件: `cmd/JoyCodeProxy/serve.go:256`

将 `go s.LogRequest(apiKey, model, path, isStream, rw.statusCode, latency)` 改为在错误时捕获错误信息。需要在 middleware 中增加 errMsg 变量并在 status >= 400 时设值。

- [ ] **Step 6: 验证编译**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./cmd/JoyCodeProxy/`
Expected:
  - Exit code: 0

- [ ] **Step 7: 提交**
Run: `git add pkg/store/store.go cmd/JoyCodeProxy/serve.go && git commit -m "feat(store): add error_message field to request logs for detailed error tracking"`

---

### Task 2: 前端 — 日志行可展开查看错误详情

**Depends on:** Task 1
**Files:**
- Modify: `web/src/api.ts`（RequestLog interface）
- Modify: `web/src/pages/AccountDetail.tsx`（日志 Table 增加展开行）

- [ ] **Step 1: 更新 RequestLog 类型 — 添加 error_message 字段**

文件: `web/src/api.ts:37-46`

```typescript
export interface RequestLog {
  id: number;
  api_key: string;
  model: string;
  endpoint: string;
  stream: boolean;
  status_code: number;
  latency_ms: number;
  error_message: string;
  created_at: string;
}
```

- [ ] **Step 2: 修改 AccountDetail 日志 Table — 添加 expandableRows 支持错误详情查看**

文件: `web/src/pages/AccountDetail.tsx`（在 Table 组件上添加 expandable 属性）

在 `<Table>` 组件中添加 `expandable` 配置，使错误行（status >= 400）可以展开查看 error_message。

- [ ] **Step 3: 构建前端**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npm run build`
Expected:
  - Exit code: 0

- [ ] **Step 4: 提交**
Run: `git add web/src/api.ts web/src/pages/AccountDetail.tsx cmd/JoyCodeProxy/static/ && git commit -m "feat(web): add expandable error details to request log table"`

---

### Task 3: 构建部署

**Depends on:** Task 1, Task 2
**Files:** None (build only)

- [ ] **Step 1: 构建 Go 二进制**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build -o joycode_proxy_bin ./cmd/JoyCodeProxy/`
Expected:
  - Exit code: 0

- [ ] **Step 2: 重启服务**
Run: `launchctl unload ~/Library/LaunchAgents/com.joycode.proxy.plist && launchctl load ~/Library/LaunchAgents/com.joycode.proxy.plist`
Expected:
  - Exit code: 0

- [ ] **Step 3: 验证新字段存在**
Run: `sleep 2 && curl -s 'http://localhost:34891/api/errors?limit=1' | python3 -m json.tool`
Expected:
  - Output contains: "error_message"
