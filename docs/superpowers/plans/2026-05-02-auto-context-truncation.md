# 代理层自动上下文截断 — 解决上下文超限死循环

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 当上游 JoyCode API 返回上下文长度超限错误时，代理自动截断旧消息后重试，让请求成功通过。解决"上下文满了 → compact 也超限 → 永远压不了"的死循环。

**Architecture:** 请求到达代理 → 转发到上游 → 上游返回上下文超限错误 → `isContextLimitError` 检测 → `truncateMessages` 移除最旧的 60% 消息（保留首条+最近 40%+插入截断通知）→ 用截断后的消息重新翻译并重试 → 成功返回。同时修改已有的 `connectStreamWithRetry` 中对上下文错误的处理：不只返回错误，而是返回特殊标记让调用方做截断重试。

**Tech Stack:** Go 1.22, Anthropic Messages API

**Risks:**
- 消息截断后 user/assistant 交替可能被破坏 → 缓解：只在偶数索引处切割，确保交替正确
- 截断后模型可能丢失关键上下文 → 缓解：保留首条消息（原始任务）+ 插入截断通知让模型知道
- 代理无状态，Claude Code 下次请求会发原始未截断消息 → 缓解：每次请求独立检测截断，直到用户主动 `/compact` 或开启新对话
- 截断比例不够导致仍超限 → 缓解：如果第一次截断后仍超限，返回 400 让 Claude Code 知道

---

### Task 1: 创建自动截断模块 `truncate.go`

**Depends on:** None
**Files:**
- Create: `pkg/anthropic/truncate.go`

- [ ] **Step 1: 创建 truncate.go — 上下文超限时自动截断旧消息**

```go
package anthropic

import (
	"encoding/json"
	"log/slog"
)

const (
	truncationKeepRatio = 0.4
	truncationMinKeep   = 4
)

// truncateMessages removes the oldest messages from the middle of the
// conversation, keeping the first message + a truncation notice + the last
// 40% of messages. Returns true if truncation was performed.
func truncateMessages(req *MessageRequest) bool {
	n := len(req.Messages)
	if n <= truncationMinKeep {
		return false
	}

	keepFirst := 1
	keepLast := truncationMinKeep
	if ratio := int(float64(n) * truncationKeepRatio); ratio > keepLast {
		keepLast = ratio
	}
	cutEnd := n - keepLast
	if cutEnd <= keepFirst {
		return false
	}

	// message[0] is user, so even indices are user, odd are assistant.
	// Ensure cutEnd lands on an even index (user) so the sequence
	// user(0) → assistant(notice) → user(cutEnd) is valid.
	if cutEnd%2 != 0 {
		cutEnd++
	}
	if cutEnd >= n {
		return false
	}

	removed := cutEnd - keepFirst
	notice := "[System: Earlier conversation messages have been auto-truncated to fit within the model's context window. Some earlier context is now missing. Continue with the remaining conversation.]"
	noticeBytes, _ := json.Marshal(notice)

	var truncated []MessageParam
	truncated = append(truncated, req.Messages[:keepFirst]...)
	truncated = append(truncated, MessageParam{
		Role:    "assistant",
		Content: json.RawMessage(noticeBytes),
	})
	truncated = append(truncated, req.Messages[cutEnd:]...)

	slog.Warn("auto-truncated messages for context limit",
		"original_count", n,
		"truncated_count", len(truncated),
		"removed", removed,
		"kept_first", keepFirst,
		"kept_last", len(req.Messages)-cutEnd,
	)

	req.Messages = truncated
	return true
}
```

- [ ] **Step 2: 验证编译**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./pkg/anthropic/`
Expected:
  - Exit code: 0

- [ ] **Step 3: 提交**
Run: `git add pkg/anthropic/truncate.go && git commit -m "feat(anthropic): add auto-truncation module for context limit handling"`

---

### Task 2: 集成截断到 handler — 上下文超限时自动截断重试

**Depends on:** Task 1
**Files:**
- Modify: `pkg/anthropic/handler.go:87-112`（handleNonStream 错误处理）
- Modify: `pkg/anthropic/handler.go:140-158`（handleStream 错误处理）

- [ ] **Step 1: 修改 handleNonStream — 上下文超限时截断消息并重试**

文件: `pkg/anthropic/handler.go`（替换 handleNonStream 函数体中的重试逻辑）

将现有的 `handleNonStream` 函数中，`lastErr != nil` 的处理改为：如果是上下文超限错误，先尝试截断消息再重试一次，截断后仍失败才返回 400。

替换 `handleNonStream` 函数整体（从 `func (h *Handler) handleNonStream` 到其函数结束）：

```go
func (h *Handler) handleNonStream(w http.ResponseWriter, req *MessageRequest, client *joycode.Client) {
	jcBody := TranslateRequest(req)
	const maxRetries = 3
	var jcResp map[string]interface{}
	var lastErr error
	truncated := false

	for attempt := 1; attempt <= maxRetries; attempt++ {
		jcResp, lastErr = client.Post(chatEndpoint, jcBody)
		if lastErr != nil {
			if isContextLimitError(lastErr.Error()) && !truncated {
				if truncateMessages(req) {
					truncated = true
					jcBody = TranslateRequest(req)
					slog.Warn("retrying with truncated messages (non-stream)")
					continue
				}
				slog.Warn("context limit exceeded, cannot truncate further")
				writeAnthropicRequestError(w, "上下文长度超出模型限制，且无法进一步截断。请压缩对话历史或开启新对话。")
				return
			}
			slog.Error("non-stream retry error", "attempt", attempt, "max", maxRetries, "error", lastErr)
			if attempt < maxRetries {
				time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
			}
			continue
		}
		break
	}

	if lastErr != nil {
		if isContextLimitError(lastErr.Error()) {
			writeAnthropicRequestError(w, "上下文长度超出模型限制。请压缩对话历史或开启新对话。原始错误: "+lastErr.Error())
			return
		}
		writeAnthropicError(w, 500, lastErr.Error())
		return
	}
	resp := TranslateResponse(jcResp, req.Model)
	writeAnthropicJSON(w, 200, resp)
}
```

- [ ] **Step 2: 修改 handleStream — 上下文超限时截断消息并重试**

文件: `pkg/anthropic/handler.go`（替换 handleStream 中连接上游后的错误处理）

在 `handleStream` 函数中，将 `connectStreamWithRetry` 返回错误后的处理改为：如果是上下文超限，截断消息后重试一次。

替换 `handleStream` 函数中从 `jcBody := TranslateRequest(req)` 开始到 `defer resp.Body.Close()` 的部分（即流连接和错误处理部分）：

```go
	jcBody := TranslateRequest(req)
	jcBody["stream"] = true
	slog.Debug("stream starting", "model", jcBody["model"], "max_tokens", jcBody["max_tokens"])

	// Connect with retry, auto-truncate on context limit
	resp, err := h.connectStreamWithRetry(jcBody, client)
	if err != nil {
		errMsg := err.Error()
		if isContextLimitError(errMsg) {
			if truncateMessages(req) {
				slog.Warn("retrying stream with truncated messages")
				jcBody = TranslateRequest(req)
				jcBody["stream"] = true
				resp, err = h.connectStreamWithRetry(jcBody, client)
			}
		}
		if err != nil {
			errMsg = err.Error()
			if isContextLimitError(errMsg) {
				slog.Warn("context limit exceeded (stream), cannot proceed")
				writeAnthropicRequestError(w, "上下文长度超出模型限制。请压缩对话历史或开启新对话。原始错误: "+errMsg)
				return
			}
			slog.Error("stream failed after retries", "error", errMsg)
			writeAnthropicError(w, 500, errMsg)
			return
		}
	}
	defer resp.Body.Close()
```

- [ ] **Step 3: 验证编译**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && go build ./cmd/JoyCodeProxy/`
Expected:
  - Exit code: 0

- [ ] **Step 4: 提交**
Run: `git add pkg/anthropic/handler.go && git commit -m "feat(anthropic): auto-truncate messages on context limit error and retry"`

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
  - Output contains: `"status": "ok"`
