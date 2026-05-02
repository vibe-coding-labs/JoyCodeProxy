# Fix Tool Call Parsing Failure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 修复 "The model's tool call could not be parsed" 错误。根因是逐 chunk `input_json_delta` 发送的参数 fragment 拼接后不是合法 JSON，导致 Anthropic SDK 解析失败。

**Architecture:** 流式路径：移除逐 chunk `input_json_delta` → 在 finish_reason 处理器中发送完整累积参数（带 JSON 有效性验证）→ content_block_stop。非流式路径：空参数时回退 `{}`。Error recovery 路径：修复 index 映射。

**Tech Stack:** Go 1.x, net/http, encoding/json

**Risks:**
- 移除逐 chunk streaming 意味着参数延迟到流结束时才发送，但保证了正确性
- 需确保 finish_reason 总会到来（否则参数丢失）→ 缓解：上游保证有 finish_reason

---

### Task 1: Fix streaming tool argument delivery in handler.go

**Depends on:** None
**Files:**
- Modify: `pkg/anthropic/handler.go:228` (fix indentation)
- Modify: `pkg/anthropic/handler.go:295-303` (remove per-chunk input_json_delta)
- Modify: `pkg/anthropic/handler.go:348-354` (send complete args before stop)
- Modify: `pkg/anthropic/handler.go:396-402` (fix error recovery index)

- [ ] **Step 1: Fix toolBlockToIdx indentation — correct code style**
文件: `pkg/anthropic/handler.go:228`

```go
	toolBlockToIdx := map[int]int{}
```

- [ ] **Step 2: Remove per-chunk input_json_delta — stop sending raw argument fragments**
文件: `pkg/anthropic/handler.go:295-303`

Remove these lines entirely:
```go
			// Stream tool arguments incrementally
				if toolBlockStarted[idx] && tc.Function.Arguments != "" {
					FormatSSE(w, "content_block_delta", sseContentBlockDelta{
						Type:  "content_block_delta",
						Index: toolBlockToIdx[idx],
						Delta: deltaText{Type: "input_json_delta", PartialJSON: tc.Function.Arguments},
					})
					flusher.Flush()
				}
```

- [ ] **Step 3: Send complete tool arguments before content_block_stop — ensure valid JSON delivery**
文件: `pkg/anthropic/handler.go:348-354`

Replace the tool block closing loop:
```go
			for i := 0; i < len(toolCalls); i++ {
				if toolBlockStarted[i] {
					FormatSSE(w, "content_block_stop", sseContentBlockStop{
						Type: "content_block_stop", Index: toolBlockToIdx[i],
					})
				}
			}
```

With:
```go
			for i := 0; i < len(toolCalls); i++ {
				if toolBlockStarted[i] {
					args := toolCalls[i].Arguments
					if args == "" || !json.Valid([]byte(args)) {
						args = "{}"
					}
					FormatSSE(w, "content_block_delta", sseContentBlockDelta{
						Type:  "content_block_delta",
						Index: toolBlockToIdx[i],
						Delta: deltaText{Type: "input_json_delta", PartialJSON: args},
					})
					FormatSSE(w, "content_block_stop", sseContentBlockStop{
						Type: "content_block_stop", Index: toolBlockToIdx[i],
					})
				}
			}
```

- [ ] **Step 4: Fix error recovery path — use toolBlockToIdx instead of currentBlockIndex**
文件: `pkg/anthropic/handler.go:396-402`

Replace:
```go
			for i := 0; i < len(toolCalls); i++ {
				if toolBlockStarted[i] {
					FormatSSE(w, "content_block_stop", sseContentBlockStop{
						Type: "content_block_stop", Index: currentBlockIndex,
					})
					currentBlockIndex++
				}
			}
```

With:
```go
			for i := 0; i < len(toolCalls); i++ {
				if toolBlockStarted[i] {
					FormatSSE(w, "content_block_stop", sseContentBlockStop{
						Type: "content_block_stop", Index: toolBlockToIdx[i],
					})
				}
			}
```

- [ ] **Step 5: Build and verify**
Run: `go build -o joycode_proxy_bin ./cmd/JoyCodeProxy`
Expected:
  - Exit code: 0
  - No compilation errors

---

### Task 2: Fix non-streaming empty tool arguments in translate.go

**Depends on:** None
**Files:**
- Modify: `pkg/anthropic/translate.go:94`

- [ ] **Step 1: Fix empty arguments producing invalid JSON — default to {}**
文件: `pkg/anthropic/translate.go:88-101`

```go
			name, _ := fn["name"].(string)
			argsStr, _ := fn["arguments"].(string)
			id, _ := tcMap["id"].(string)
			if id == "" {
				id = "toolu_" + newID()
			}
			if argsStr == "" || !json.Valid([]byte(argsStr)) {
				argsStr = "{}"
			}

			var input json.RawMessage = json.RawMessage(argsStr)
			content = append(content, ContentBlock{
				Type:  "tool_use",
				ID:    id,
				Name:  name,
				Input: input,
			})
```

- [ ] **Step 2: Build and verify**
Run: `go build -o joycode_proxy_bin ./cmd/JoyCodeProxy`
Expected:
  - Exit code: 0

---

### Task 3: Deploy and verify

**Depends on:** Task 1, Task 2
**Files:** None

- [ ] **Step 1: Kill old process and restart**
Run: `ps aux | grep joycode_proxy_bin | grep -v grep | awk '{print $2}' | xargs kill && sleep 4 && curl -s http://127.0.0.1:34891/api/health`
Expected:
  - Output contains: `"status":"ok"`

- [ ] **Step 2: Commit**
Run: `git add pkg/anthropic/handler.go pkg/anthropic/translate.go && git commit -m "fix(anthropic): fix tool call parsing by sending complete arguments instead of raw fragments"`
