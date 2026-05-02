# 修复: 上下文超限导致客户端无限重试

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 当上游 JoyCode API 返回上下文长度超限错误时，代理返回不可重试的 Anthropic API 错误（`invalid_request_error`），让 Claude Code 客户端停止重试并告知用户。同时降低复制命令中的 `CLAUDE_CODE_MAX_RETRIES`。

**Root Cause:** 两个问题叠加导致无限重试：1) 复制命令中 `CLAUDE_CODE_MAX_RETRIES=1000000` 允许无限重试；2) 代理将所有上游错误统一返回 `api_error`（500），Claude Code 认为这是可重试的临时错误。

**Architecture:** 上游错误 → `isUpstreamError()` 检测 → `classifyUpstreamError()` 分类 → 上下文超限返回 `invalid_request_error`（400）/ 其他错误保持 `api_error`（500）。Claude Code 收到 400 类错误不会重试。

**Tech Stack:** Go 1.22, Anthropic Messages API error format

**Risks:**
- 修改错误分类可能影响其他类型的错误处理 → 缓解：仅对已知模式匹配返回 400，其余保持原样
- Claude Code 对 `invalid_request_error` 的行为需要验证 → 缓解：根据 Anthropic API 文档，`invalid_request_error` (400) 不会被重试

---

### Task 1: 后端 — 上下文超限错误分类与正确返回

**Depends on:** None
**Files:**
- Modify: `pkg/anthropic/handler.go`（错误分类函数 + 错误返回修改）

- [ ] **Step 1: 添加上下文超限错误检测函数 `isContextLimitError`**

文件: `pkg/anthropic/handler.go`（在 `isUpstreamError` 函数之后添加）

```go
// isContextLimitError checks if the upstream error indicates context length exceeded.
func isContextLimitError(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "context length") ||
		strings.Contains(lower, "context window") ||
		strings.Contains(lower, "token limit") ||
		strings.Contains(lower, "tokens exceeded") ||
		strings.Contains(lower, "input length") ||
		strings.Contains(lower, "model_context_window_exceeded") ||
		strings.Contains(lower, "prompt length") ||
		strings.Contains(lower, "max_input_tokens")
}
```

- [ ] **Step 2: 添加 `writeAnthropicRequestError` 函数 — 返回不可重试的 400 错误**

文件: `pkg/anthropic/handler.go`（在 `writeAnthropicError` 函数之后添加）

```go
func writeAnthropicRequestError(w http.ResponseWriter, msg string) {
	writeAnthropicJSON(w, 400, map[string]interface{}{
		"type":  "error",
		"error": map[string]string{"type": "invalid_request_error", "message": msg},
	})
}
```

- [ ] **Step 3: 修改 `handleNonStream` — 对上下文超限错误返回 400**

文件: `pkg/anthropic/handler.go:99-101`（替换错误处理块）

```go
	if lastErr != nil {
		errMsg := lastErr.Error()
		if isContextLimitError(errMsg) {
			slog.Warn("context limit exceeded", "error", errMsg)
			writeAnthropicRequestError(w, "上下文长度超出模型限制。请压缩对话历史或开启新对话。原始错误: "+errMsg)
			return
		}
		writeAnthropicError(w, 500, errMsg)
		return
	}
```

- [ ] **Step 4: 修改 `handleStream` 的错误返回 — 对上下文超限返回 400**

文件: `pkg/anthropic/handler.go:141-145`（替换 stream 错误处理块）

```go
	resp, err := h.connectStreamWithRetry(jcBody, client)
	if err != nil {
		errMsg := err.Error()
		if isContextLimitError(errMsg) {
			slog.Warn("context limit exceeded (stream)", "error", errMsg)
			writeAnthropicRequestError(w, "上下文长度超出模型限制。请压缩对话历史或开启新对话。原始错误: "+errMsg)
			return
		}
		slog.Error("stream failed after retries", "error", errMsg)
		writeAnthropicError(w, 500, errMsg)
		return
	}
```

- [ ] **Step 5: 修改 `connectStreamWithRetry` — 上下文超限错误不重试**

文件: `pkg/anthropic/handler.go:355-363`（替换 upstream error 检测块）

```go
		if isUpstreamError(dataContent) {
			resp.Body.Close()
			lastErr = fmt.Errorf("upstream error: %s", truncate(dataContent, 500))
			slog.Error("stream upstream error", "attempt", attempt, "max", maxRetries, "body", truncate(dataContent, 500))
			// Context limit errors are not retryable — return immediately
			if isContextLimitError(dataContent) {
				return nil, lastErr
			}
			if attempt < maxRetries {
				time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
			}
			continue
		}
```

- [ ] **Step 6: 验证编译**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./cmd/JoyCodeProxy/`
Expected:
  - Exit code: 0

- [ ] **Step 7: 提交**
Run: `git add pkg/anthropic/handler.go && git commit -m "fix(anthropic): return 400 for context limit errors to prevent infinite retries"`

---

### Task 2: 前端 — 降低复制命令中的 MAX_RETRIES

**Depends on:** None
**Files:**
- Modify: `web/src/pages/AccountDetail.tsx:55-63`

- [ ] **Step 1: 修改 `buildClaudeCodeCmd` — 移除无限重试，设置合理值**

文件: `web/src/pages/AccountDetail.tsx:55-63`（替换 `buildClaudeCodeCmd` 函数）

```typescript
const buildClaudeCodeCmd = (apiKey: string, model = 'GLM-5.1') => [
  `API_TIMEOUT_MS=6000000 \\`,
  `CLAUDE_CODE_MAX_RETRIES=3 \\`,
  `ANTHROPIC_BASE_URL=${getBaseURL()} \\`,
  `ANTHROPIC_API_KEY="${apiKey}" \\`,
  `CLAUDE_CODE_MAX_OUTPUT_TOKENS=6553655 \\`,
  `ANTHROPIC_MODEL=${model} \\`,
  `claude --dangerously-skip-permissions`,
].join('\n');
```

- [ ] **Step 2: 构建前端**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npm run build`
Expected:
  - Exit code: 0

- [ ] **Step 3: 复制构建产物**
Run: `cp -r /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web/dist/* /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/cmd/JoyCodeProxy/static/`
Expected:
  - Exit code: 0

- [ ] **Step 4: 提交**
Run: `git add web/src/pages/AccountDetail.tsx cmd/JoyCodeProxy/static/ && git commit -m "fix(web): lower MAX_RETRIES from 1000000 to 3 in copy command"`

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

- [ ] **Step 3: 验证服务正常**
Run: `sleep 2 && curl -s 'http://localhost:34891/api/health' | python3 -m json.tool`
Expected:
  - Output contains: `"status"` and `"accounts"`
