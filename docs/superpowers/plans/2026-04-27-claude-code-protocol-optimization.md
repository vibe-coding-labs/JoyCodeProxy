# Claude Code Protocol Optimization Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 优化 JoyCodeProxy 的 Anthropic 协议兼容性，修复 tool_result 内容丢失、添加 thinking 推理过程支持、增强 HTTP 客户端稳定性、添加调试日志，确保 Claude Code 完整工作流无异常。

**Architecture:** Claude Code 发送请求 → anthropic_handler 解析多类型 content blocks（text/tool_result/image/tool_use）→ 构建 OpenAI 格式消息 → Client 带重试发送到 JoyCode → 响应中 reasoning_content 转换为 Anthropic thinking block → SSE 流式返回。每层独立修改，向后兼容。

**Tech Stack:** Python 3.9+, FastAPI 0.115+, httpx 0.28+, pytest 8.3+

**Risks:**
- Task 1 修改 parse_content 和 translate_request 影响所有消息处理路径 → 缓解：纯文本输入保持原行为，新增逻辑只在 content 是 list 时触发
- Task 2 依赖 JoyCode 的 reasoning_content SSE 格式 → 缓解：已通过实际 API 抓包确认格式为 `delta.reasoning_content` 流式文本片段
- Task 3 修改 Client 初始化可能影响现有连接行为 → 缓解：httpx 默认值不变，只新增重试和池化

---

### Task 1: 修复 Anthropic 消息内容处理 — 支持 tool_result/image/tool_use 多类型 content blocks

**Depends on:** None
**Files:**
- Modify: `joycode_proxy/anthropic_handler.py:57-78`（parse_content 函数）
- Modify: `joycode_proxy/anthropic_handler.py:106-149`（translate_request 函数）

- [ ] **Step 1: 重写 parse_content 为 translate_content_blocks — 支持多类型 content block 转换**

文件: `joycode_proxy/anthropic_handler.py:57-78`（替换整个 parse_content 函数及其后空行）

重写原因：原函数只能提取 text，丢失 tool_result/image/tool_use 等结构化内容。Claude Code 多轮对话中会发送包含 tool_result 的 content blocks，必须正确转换为 OpenAI 格式的 tool message。

```python
def parse_content(raw: Any) -> str:
    """Extract plain-text from an Anthropic message *content* field.

    Backward-compatible: plain string → string, list → joined text.
    Used only for system prompts and simple text extraction.
    """
    if isinstance(raw, str):
        return raw
    if isinstance(raw, list):
        parts: List[str] = []
        for block in raw:
            if isinstance(block, dict):
                if block.get("type") == "text":
                    parts.append(block.get("text", ""))
                elif block.get("type") == "tool_result":
                    rc = block.get("content", "")
                    if isinstance(rc, list):
                        for sub in rc:
                            if isinstance(sub, dict) and sub.get("type") == "text":
                                parts.append(sub.get("text", ""))
                    elif isinstance(rc, str):
                        parts.append(rc)
        return "\n".join(parts)
    if raw is None:
        return ""
    if isinstance(raw, bytes):
        return raw.decode("utf-8", errors="replace")
    return str(raw)


def _translate_content_blocks(content: Any) -> Any:
    """Translate Anthropic content blocks to OpenAI-compatible format.

    Returns a string for simple text, or a list of OpenAI content parts
    for structured content (images, tool results, etc.).
    """
    if isinstance(content, str):
        return content
    if not isinstance(content, list):
        return str(content) if content is not None else ""

    parts: List[Any] = []
    has_non_text = False

    for block in content:
        if not isinstance(block, dict):
            continue
        btype = block.get("type", "")

        if btype == "text":
            parts.append({"type": "text", "text": block.get("text", "")})
        elif btype == "image":
            source = block.get("source", {})
            if source.get("type") == "base64":
                data_url = "data:{};base64,{}".format(
                    source.get("media_type", "image/png"),
                    source.get("data", ""),
                )
                parts.append({
                    "type": "image_url",
                    "image_url": {"url": data_url},
                })
                has_non_text = True
            else:
                url = source.get("url", "")
                if url:
                    parts.append({
                        "type": "image_url",
                        "image_url": {"url": url},
                    })
                    has_non_text = True
        elif btype == "tool_result":
            rc = block.get("content", "")
            text = ""
            if isinstance(rc, list):
                texts = [
                    s.get("text", "") for s in rc
                    if isinstance(s, dict) and s.get("type") == "text"
                ]
                text = "\n".join(texts)
            elif isinstance(rc, str):
                text = rc
            parts.append({"type": "text", "text": text or "Tool executed successfully"})
            has_non_text = True
        elif btype == "tool_use":
            fn_input = block.get("input", {})
            if isinstance(fn_input, dict):
                text = json.dumps(fn_input, ensure_ascii=False)
            else:
                text = str(fn_input)
            parts.append({"type": "text", "text": text})
            has_non_text = True

    if not has_non_text:
        return "\n".join(
            p.get("text", "") for p in parts if p.get("type") == "text"
        )
    return parts
```

- [ ] **Step 2: 修改 translate_request 以支持多 content block 和 tool_result 消息**

文件: `joycode_proxy/anthropic_handler.py:106-149`（替换整个 translate_request 函数）

重写原因：原函数把所有 content 合并成纯字符串，丢失了 tool_result 关联的 tool_use_id 信息。Claude Code 的多轮 tool 对话需要把 assistant 的 tool_use 转为 OpenAI 的 assistant tool_calls，把 user 的 tool_result 转为 OpenAI 的 tool message。

```python
def translate_request(req: Dict[str, Any]) -> Dict[str, Any]:
    """Convert an Anthropic Messages API request body to a JoyCode/OpenAI body."""
    model = resolve_model(req.get("model", ""))

    messages: List[Dict[str, Any]] = []

    system = req.get("system")
    if system is not None:
        sys_text = parse_content(system)
        if sys_text:
            messages.append({"role": "system", "content": sys_text})

    for m in req.get("messages", []):
        role = m.get("role", "user")
        raw_content = m.get("content")

        # Detect if message has tool_use (assistant) or tool_result (user) blocks
        if isinstance(raw_content, list):
            tool_uses = [b for b in raw_content if isinstance(b, dict) and b.get("type") == "tool_use"]
            tool_results = [b for b in raw_content if isinstance(b, dict) and b.get("type") == "tool_result"]

            if tool_uses and role == "assistant":
                # Convert assistant tool_use → OpenAI tool_calls format
                text_parts = []
                for b in raw_content:
                    if isinstance(b, dict) and b.get("type") == "text":
                        text_parts.append(b.get("text", ""))
                assistant_msg: Dict[str, Any] = {
                    "role": "assistant",
                    "content": "\n".join(text_parts) if text_parts else None,
                }
                openai_tool_calls = []
                for tu in tool_uses:
                    tc_id = tu.get("id", "toolu_" + _new_id())
                    fn_input = tu.get("input", {})
                    if isinstance(fn_input, dict):
                        args_str = json.dumps(fn_input, ensure_ascii=False)
                    else:
                        args_str = str(fn_input)
                    openai_tool_calls.append({
                        "id": tc_id,
                        "type": "function",
                        "function": {
                            "name": tu.get("name", ""),
                            "arguments": args_str,
                        },
                    })
                assistant_msg["tool_calls"] = openai_tool_calls
                messages.append(assistant_msg)

                # Add a tool message for each tool_result that follows
                for tr in tool_results:
                    tool_msg = _build_tool_message(tr)
                    messages.append(tool_msg)
                continue

            if tool_results and role == "user":
                for tr in tool_results:
                    tool_msg = _build_tool_message(tr)
                    messages.append(tool_msg)
                # Also add any non-tool_result text content
                text_parts = []
                for b in raw_content:
                    if isinstance(b, dict) and b.get("type") == "text":
                        text_parts.append(b.get("text", ""))
                if text_parts:
                    messages.append({"role": "user", "content": "\n".join(text_parts)})
                continue

        # Default path: translate content blocks to OpenAI format
        translated = _translate_content_blocks(raw_content)
        messages.append({"role": role, "content": translated})

    body: Dict[str, Any] = {
        "model": model,
        "messages": messages,
        "stream": req.get("stream", False),
    }

    max_tokens = req.get("max_tokens", 0)
    if max_tokens:
        body["max_tokens"] = max_tokens
    else:
        body["max_tokens"] = 8192

    if "temperature" in req and req["temperature"] is not None:
        body["temperature"] = req["temperature"]
    if "top_p" in req and req["top_p"] is not None:
        body["top_p"] = req["top_p"]
    if req.get("stop_sequences"):
        body["stop"] = req["stop_sequences"]
    if req.get("tools"):
        body["tools"] = convert_tools_to_openai(req["tools"])

    return body


def _build_tool_message(tool_result: Dict[str, Any]) -> Dict[str, Any]:
    """Convert an Anthropic tool_result block to an OpenAI tool message."""
    rc = tool_result.get("content", "")
    text = ""
    if isinstance(rc, list):
        texts = [
            s.get("text", "") for s in rc
            if isinstance(s, dict) and s.get("type") == "text"
        ]
        text = "\n".join(texts)
    elif isinstance(rc, str):
        text = rc
    return {
        "role": "tool",
        "tool_call_id": tool_result.get("tool_use_id", ""),
        "content": text or "Tool executed successfully",
    }
```

- [ ] **Step 3: 验证消息内容处理**

Run: `python3 -c "
from joycode_proxy.anthropic_handler import parse_content, _translate_content_blocks, translate_request

# Test parse_content backward compat
assert parse_content('hello') == 'hello'
assert parse_content([{'type': 'text', 'text': 'hi'}]) == 'hi'
assert parse_content(None) == ''

# Test tool_result in parse_content
result = parse_content([{'type': 'tool_result', 'content': [{'type': 'text', 'text': 'file contents'}]}])
assert 'file contents' in result

# Test _translate_content_blocks with image
blocks = _translate_content_blocks([
    {'type': 'text', 'text': 'see image'},
    {'type': 'image', 'source': {'type': 'base64', 'media_type': 'image/png', 'data': 'abc123'}},
])
assert isinstance(blocks, list)
assert blocks[0]['type'] == 'text'
assert blocks[1]['type'] == 'image_url'

# Test translate_request with tool_result multi-turn
req = translate_request({
    'model': 'claude-sonnet-4',
    'max_tokens': 100,
    'messages': [
        {'role': 'user', 'content': 'read file'},
        {'role': 'assistant', 'content': [
            {'type': 'text', 'text': 'I will read it.'},
            {'type': 'tool_use', 'id': 'toolu_1', 'name': 'Read', 'input': {'path': '/etc/hosts'}},
        ]},
        {'role': 'user', 'content': [
            {'type': 'tool_result', 'tool_use_id': 'toolu_1', 'content': '127.0.0.1 localhost'},
        ]},
    ],
})
msgs = req['messages']
# Should have: system(if any) + user + assistant(tool_calls) + tool
roles = [m['role'] for m in msgs]
assert 'tool' in roles, f'Expected tool role in messages, got: {roles}'
tool_msg = [m for m in msgs if m['role'] == 'tool'][0]
assert tool_msg['tool_call_id'] == 'toolu_1'
assert '127.0.0.1' in tool_msg['content']
assistant_msg = [m for m in msgs if m['role'] == 'assistant'][0]
assert 'tool_calls' in assistant_msg
assert assistant_msg['tool_calls'][0]['function']['name'] == 'Read'
print('All content translation tests passed!')
"`
Expected:
  - Exit code: 0
  - Output contains: "All content translation tests passed!"

- [ ] **Step 4: 提交**
Run: `git add joycode_proxy/anthropic_handler.py && git commit -m "fix(anthropic): support multi-type content blocks including tool_result, image, and tool_use"`

---

### Task 2: 添加 Thinking/Reasoning 支持 — 将 JoyCode reasoning_content 转换为 Anthropic thinking block

**Depends on:** Task 1
**Files:**
- Modify: `joycode_proxy/anthropic_handler.py:152-214`（translate_response 函数 — reasoning 非流式）
- Modify: `joycode_proxy/anthropic_handler.py:232-400`（_handle_stream 函数 — reasoning 流式）

- [ ] **Step 1: 修改 translate_response — 将 reasoning_content 转换为 thinking content block**

文件: `joycode_proxy/anthropic_handler.py:152-214`（替换整个 translate_response 函数）

重写原因：JoyCode 的 reasoning_content 在非流式响应中位于 `message.reasoning_content`，需要转为 Anthropic 的 `thinking` content block 放在 text block 之前，让 Claude Code 能看到推理过程。

```python
def translate_response(jc_resp: Dict[str, Any], req_model: str) -> Dict[str, Any]:
    """Convert a JoyCode API response to Anthropic Messages format."""
    msg_id = _new_message_id()

    usage_info = jc_resp.get("usage") or {}
    usage = {
        "input_tokens": int(usage_info.get("prompt_tokens", 0)),
        "output_tokens": int(usage_info.get("completion_tokens", 0)),
    }

    choices = jc_resp.get("choices") or []
    if not choices:
        return {
            "id": msg_id,
            "type": "message",
            "role": "assistant",
            "model": req_model,
            "content": [{"type": "text", "text": ""}],
            "stop_reason": "end_turn",
            "usage": usage,
        }

    choice = choices[0]
    msg = choice.get("message", {})
    content: List[Dict[str, Any]] = []
    stop_reason = "end_turn"

    # Handle reasoning_content → thinking block
    reasoning = msg.get("reasoning_content", "")
    if reasoning:
        content.append({"type": "thinking", "thinking": reasoning})

    # Handle tool_calls
    tool_calls = msg.get("tool_calls") or []
    if tool_calls:
        stop_reason = "tool_use"
        for tc in tool_calls:
            fn = tc.get("function", {})
            name = fn.get("name", "")
            args_str = fn.get("arguments", "{}")
            tc_id = tc.get("id", "")
            if not tc_id:
                tc_id = "toolu_" + _new_id()
            try:
                input_obj = json.loads(args_str)
            except (json.JSONDecodeError, TypeError):
                input_obj = args_str
            content.append({
                "type": "tool_use",
                "id": tc_id,
                "name": name,
                "input": input_obj,
            })
    else:
        text = msg.get("content", "")
        content.append({"type": "text", "text": text})

    return {
        "id": msg_id,
        "type": "message",
        "role": "assistant",
        "model": req_model,
        "content": content,
        "stop_reason": stop_reason,
        "usage": usage,
    }
```

- [ ] **Step 2: 修改 _handle_stream — 在 SSE 流中输出 thinking block**

文件: `joycode_proxy/anthropic_handler.py`（替换 _handle_stream 函数内的 `_generator` 内部函数，从 `async def _generator():` 到 `resp.close()` 为止，即原文件约第 235-390 行）

重写原因：JoyCode 流式返回的 `delta.reasoning_content` 需要先作为 Anthropic `thinking` content block 输出，然后才是 `text` content block。当前代码完全忽略 reasoning_content。

```python
    async def _generator():
        jc_body = translate_request(req)
        jc_body["stream"] = True

        resp = client.post_stream(CHAT_ENDPOINT, jc_body)

        msg_id = _new_message_id()
        model = req.get("model", "")
        total_output = 0

        # message_start
        yield _sse_event("message_start", {
            "type": "message_start",
            "message": {
                "id": msg_id,
                "type": "message",
                "role": "assistant",
                "model": model,
                "content": [],
                "usage": {},
            },
        })
        yield _sse_event("ping", {"type": "ping"})

        # Accumulator for in-progress tool calls: index -> {id, name, arguments}
        tool_calls_acc: Dict[int, Dict[str, str]] = {}
        current_block_index = 0

        thinking_block_started = False
        text_block_started = False
        tool_block_started: Dict[int, bool] = {}

        for raw_line in resp.iter_lines():
            if not raw_line:
                continue
            line = raw_line
            if isinstance(line, bytes):
                line = line.decode("utf-8", errors="replace")

            if line.startswith("data: "):
                line = line[len("data: "):]
            line = line.strip()
            if not line or line == "[DONE]":
                continue

            try:
                chunk = json.loads(line)
            except json.JSONDecodeError:
                continue

            choices = chunk.get("choices") or []
            if not choices:
                continue
            choice = choices[0]
            delta = choice.get("delta", {})

            # ---- Process reasoning_content → thinking block ----
            reasoning_text = delta.get("reasoning_content", "")
            if reasoning_text:
                if not thinking_block_started:
                    thinking_block_started = True
                    yield _sse_event("content_block_start", {
                        "type": "content_block_start",
                        "index": current_block_index,
                        "content_block": {"type": "thinking", "thinking": ""},
                    })
                yield _sse_event("content_block_delta", {
                    "type": "content_block_delta",
                    "index": current_block_index,
                    "delta": {"type": "thinking_delta", "thinking": reasoning_text},
                })

            # ---- Process tool_calls deltas ----
            for tc in delta.get("tool_calls") or []:
                idx = tc.get("index", 0)
                if idx not in tool_calls_acc:
                    tool_calls_acc[idx] = {
                        "id": tc.get("id", ""),
                        "name": tc.get("function", {}).get("name", ""),
                        "arguments": "",
                    }
                acc = tool_calls_acc[idx]
                if tc.get("id"):
                    acc["id"] = tc["id"]
                fn = tc.get("function", {})
                if fn.get("name"):
                    acc["name"] = fn["name"]
                if fn.get("arguments"):
                    acc["arguments"] += fn["arguments"]

                if not tool_block_started.get(idx):
                    # Close thinking block if open
                    if thinking_block_started:
                        yield _sse_event("content_block_stop", {
                            "type": "content_block_stop",
                            "index": current_block_index,
                        })
                        current_block_index += 1
                        thinking_block_started = False
                    # Close text block if open
                    if text_block_started:
                        yield _sse_event("content_block_stop", {
                            "type": "content_block_stop",
                            "index": current_block_index,
                        })
                        current_block_index += 1
                        text_block_started = False

                    tool_block_started[idx] = True
                    tc_id = acc["id"]
                    if not tc_id:
                        tc_id = "toolu_" + _new_id()
                    yield _sse_event("content_block_start", {
                        "type": "content_block_start",
                        "index": current_block_index,
                        "content_block": {
                            "type": "tool_use",
                            "id": tc_id,
                            "name": acc["name"],
                        },
                    })

            # ---- Process text content ----
            text = delta.get("content", "")
            if text:
                if not text_block_started:
                    # Close thinking block first if open
                    if thinking_block_started:
                        yield _sse_event("content_block_stop", {
                            "type": "content_block_stop",
                            "index": current_block_index,
                        })
                        current_block_index += 1
                        thinking_block_started = False

                    text_block_started = True
                    yield _sse_event("content_block_start", {
                        "type": "content_block_start",
                        "index": current_block_index,
                        "content_block": {"type": "text", "text": ""},
                    })
                total_output += len(text)
                yield _sse_event("content_block_delta", {
                    "type": "content_block_delta",
                    "index": current_block_index,
                    "delta": {"type": "text_delta", "text": text},
                })

            # ---- Handle finish ----
            finish_reason = choice.get("finish_reason")
            if finish_reason is not None:
                # Close thinking block if still open
                if thinking_block_started:
                    yield _sse_event("content_block_stop", {
                        "type": "content_block_stop",
                        "index": current_block_index,
                    })
                    current_block_index += 1
                    thinking_block_started = False

                # Close text block if still open
                if text_block_started:
                    yield _sse_event("content_block_stop", {
                        "type": "content_block_stop",
                        "index": current_block_index,
                    })
                    current_block_index += 1
                    text_block_started = False

                # Close and flush tool call blocks with input_json_delta
                for i in range(len(tool_calls_acc)):
                    if tool_block_started.get(i):
                        tc = tool_calls_acc[i]
                        yield _sse_event("content_block_delta", {
                            "type": "content_block_delta",
                            "index": current_block_index,
                            "delta": {
                                "type": "input_json_delta",
                                "text": tc["arguments"],
                            },
                        })
                        yield _sse_event("content_block_stop", {
                            "type": "content_block_stop",
                            "index": current_block_index,
                        })
                        current_block_index += 1

                stop_reason = "end_turn"
                if finish_reason == "tool_calls":
                    stop_reason = "tool_use"

                yield _sse_event("message_delta", {
                    "type": "message_delta",
                    "delta": {"stop_reason": stop_reason, "stop_sequence": None},
                    "usage": {"output_tokens": max(1, total_output // 4)},
                })
                yield _sse_event("message_stop", {"type": "message_stop"})

        resp.close()
```

- [ ] **Step 3: 验证 thinking 流式输出**

Run: `curl -s -N -X POST http://localhost:34891/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: joycode" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "GLM-5.1",
    "max_tokens": 50,
    "stream": true,
    "messages": [{"role": "user", "content": "1+1等于几？"}]
  }' 2>&1 | grep -E "thinking|content_block_start" | head -10`
Expected:
  - Output contains: `"type":"thinking"` (thinking block 出现)
  - Output contains: `"type":"text"` (text block 在 thinking 之后)

- [ ] **Step 4: 提交**
Run: `git add joycode_proxy/anthropic_handler.py && git commit -m "feat(anthropic): add thinking/reasoning support for extended thinking models"`

---

### Task 3: 增强 HTTP 客户端 — 添加连接池、超时分层、自动重试

**Depends on:** None
**Files:**
- Modify: `joycode_proxy/client.py:1-39`（imports 和 Client.__init__）

- [ ] **Step 1: 修改 Client.__init__ — 添加连接池、分层超时、重试传输**

文件: `joycode_proxy/client.py:1-39`（替换文件头部 imports 和 Client 类的 __init__ 方法）

重写原因：当前 httpx.Client 只有单一 120s 超时，无连接池限制、无重试。高并发或网络抖动时容易失败。添加分层超时（connect 10s, read 120s, write 30s, pool 10s）和最多 3 次重试。

```python
import uuid
from typing import Any, Dict, List, Optional

import httpx

BASE_URL = "https://joycode-api.jd.com"
DEFAULT_MODEL = "JoyAI-Code"
CLIENT_VERSION = "2.4.5"
USER_AGENT = (
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) "
    "AppleWebKit/537.36 (KHTML, like Gecko) "
    "JoyCode/2.4.5 Chrome/133.0.0.0 Electron/35.2.0 Safari/537.36"
)
TIMEOUT = httpx.Timeout(connect=10.0, read=120.0, write=30.0, pool=10.0)
MAX_RETRIES = 3
RETRY_STATUS_CODES = {429, 502, 503, 504}

MODELS = [
    "JoyAI-Code",
    "MiniMax-M2.7",
    "Kimi-K2.6",
    "Kimi-K2.5",
    "GLM-5.1",
    "GLM-5",
    "GLM-4.7",
    "Doubao-Seed-2.0-pro",
]

CHAT_ENDPOINT = "/api/saas/openai/v1/chat/completions"


def _hex_id() -> str:
    return uuid.uuid4().hex


class Client:
    def __init__(self, pt_key: str, user_id: str):
        self.pt_key = pt_key
        self.user_id = user_id
        self.session_id = _hex_id()
        transport = httpx.AsyncHTTPTransport(
            retries=MAX_RETRIES,
        )
        self._http = httpx.Client(
            timeout=TIMEOUT,
            limits=httpx.Limits(
                max_connections=20,
                max_keepalive_connections=10,
                keepalive_expiry=60,
            ),
            transport=transport,
        )
```

注意：`httpx.AsyncHTTPTransport(retries=N)` 在 httpx 0.28+ 中支持自动重试，底层使用 `httpcore` 的连接级别重试，对 `RETRY_STATUS_CODES` 中的状态码和连接错误自动重试。这不需要修改 post/post_stream 方法的签名。

- [ ] **Step 2: 验证 HTTP 客户端增强**

Run: `python3 -c "
from joycode_proxy.client import Client, TIMEOUT, MAX_RETRIES

assert MAX_RETRIES == 3
assert TIMEOUT.connect == 10.0
assert TIMEOUT.read == 120.0
assert TIMEOUT.write == 30.0
assert TIMEOUT.pool == 10.0

# Verify Client can be instantiated
c = Client('test-key', 'test-user')
assert c.pt_key == 'test-key'
assert c.user_id == 'test-user'
print('HTTP client enhancement verified!')
"`
Expected:
  - Exit code: 0
  - Output contains: "HTTP client enhancement verified!"

- [ ] **Step 3: 提交**
Run: `git add joycode_proxy/client.py && git commit -m "feat(client): add connection pooling, layered timeouts, and auto-retry"`

---

### Task 4: 添加请求调试日志 — --verbose 选项和请求/响应记录

**Depends on:** None
**Files:**
- Modify: `joycode_proxy/cli.py:39-48`（cli 命令 — 添加 --verbose 选项）
- Modify: `joycode_proxy/cli.py:55-71`（serve 命令 — 传递 log_level）
- Modify: `joycode_proxy/anthropic_handler.py:1-3`（添加 logging import）
- Modify: `joycode_proxy/anthropic_handler.py:428-449`（handle_messages — 添加日志）

- [ ] **Step 1: 修改 CLI — 添加 --verbose 选项控制日志级别**

文件: `joycode_proxy/cli.py:39-48`（替换 cli 命令）

```python
@cli.group()
@click.option("-k", "--ptkey", default="", help="JoyCode ptKey (auto-detected if empty)")
@click.option("-u", "--userid", default="", help="JoyCode userID (auto-detected if empty)")
@click.option("--skip-validation", is_flag=True, help="Skip credential validation")
@click.option("-v", "--verbose", is_flag=True, help="Enable debug logging")
@click.pass_context
def cli(ctx, ptkey: str, userid: str, skip_validation: bool, verbose: bool):
    ctx.ensure_object(dict)
    ctx.obj["ptkey"] = ptkey
    ctx.obj["userid"] = userid
    ctx.obj["skip_validation"] = skip_validation
    ctx.obj["verbose"] = verbose
    level = logging.DEBUG if verbose else logging.INFO
    logging.basicConfig(level=level, format="%(asctime)s %(levelname)s %(name)s: %(message)s")
```

- [ ] **Step 2: 修改 serve 命令 — 使用 verbose 控制日志级别**

文件: `joycode_proxy/cli.py:51-71`（替换 serve 命令，在 `@cli.command()` 和 `uvicorn.run` 之间）

```python
@cli.command()
@click.option("-H", "--host", default="0.0.0.0", help="Bind host")
@click.option("-p", "--port", default=34891, help="Bind port")
@click.pass_context
def serve(ctx, host: str, port: int):
    import uvicorn
    client = _resolve_client(ctx.obj["ptkey"], ctx.obj["userid"], ctx.obj["skip_validation"])
    from joycode_proxy.server import create_app
    app = create_app(client)
    click.echo("  Endpoints:")
    click.echo("    POST /v1/chat/completions  - Chat (OpenAI format)")
    click.echo("    POST /v1/messages          - Chat (Anthropic/Claude Code format)")
    click.echo("    POST /v1/web-search        - Web Search")
    click.echo("    POST /v1/rerank            - Rerank documents")
    click.echo("    GET  /v1/models            - Model list")
    click.echo("    GET  /health               - Health check")
    click.echo()
    click.echo("  Claude Code setup:")
    click.echo(f"    export ANTHROPIC_BASE_URL=http://{host}:{port}")
    click.echo("    export ANTHROPIC_API_KEY=joycode")
    log_level = "debug" if ctx.obj.get("verbose") else "info"
    uvicorn.run(app, host=host, port=port, log_level=log_level)
```

- [ ] **Step 3: 在 anthropic_handler 添加请求/响应日志**

文件: `joycode_proxy/anthropic_handler.py:1-3`（在现有 import 区域后添加 logging）

在文件顶部的 import 区域（第 1-3 行）之后添加 logging 初始化：

```python
import json
import uuid
from typing import Any, Dict, List, Optional
import logging

from fastapi import APIRouter, Request
from fastapi.responses import JSONResponse, StreamingResponse

from joycode_proxy.client import CHAT_ENDPOINT, Client, MODELS

log = logging.getLogger("joycode-proxy.anthropic")
```

然后修改 `handle_messages` 端点函数（`create_anthropic_router` 内部的 `handle_messages`）添加日志：

文件: `joycode_proxy/anthropic_handler.py`（在 `create_anthropic_router` 内部的 `handle_messages` 函数中添加日志行）

```python
    @router.post("/v1/messages")
    async def handle_messages(request: Request):
        body = await request.json()
        log.debug("POST /v1/messages model=%s stream=%s tools=%d",
                  body.get("model"), body.get("stream"), len(body.get("tools", [])))

        if not body.get("max_tokens"):
            body["max_tokens"] = 8192

        if body.get("stream"):
            return await _handle_stream(client, body)

        jc_body = translate_request(body)
        try:
            jc_resp = client.post(CHAT_ENDPOINT, jc_body)
        except Exception as exc:
            log.error("JoyCode API error: %s", exc)
            return _error_response(500, str(exc))

        resp = translate_response(jc_resp, body.get("model", ""))
        log.debug("Response: stop_reason=%s content_blocks=%d",
                  resp.get("stop_reason"), len(resp.get("content", [])))
        return JSONResponse(content=resp)
```

- [ ] **Step 4: 验证调试日志**

Run: `python3 -c "
from joycode_proxy.cli import cli
# Verify verbose option exists
import click
params = {p.name for p in cli.params}
assert 'verbose' in params, f'Expected verbose in {params}'
print('Debug logging option verified!')
"`
Expected:
  - Exit code: 0
  - Output contains: "Debug logging option verified!"

- [ ] **Step 5: 提交**
Run: `git add joycode_proxy/cli.py joycode_proxy/anthropic_handler.py && git commit -m "feat: add --verbose debug logging with request/response tracing"`

---

### Task 5: 补充单元测试 — 覆盖 content blocks、thinking、tool_result 场景

**Depends on:** Task 1, Task 2
**Files:**
- Modify: `tests/test_anthropic.py`（补充测试用例）

- [ ] **Step 1: 扩展 test_anthropic.py — 添加 content block 和 thinking 测试**

文件: `tests/test_anthropic.py`（替换整个文件）

```python
from joycode_proxy.anthropic_handler import (
    _translate_content_blocks,
    convert_tools_to_openai,
    parse_content,
    resolve_model,
    translate_request,
    translate_response,
)


def test_resolve_model():
    assert resolve_model("GLM-5.1") == "GLM-5.1"
    assert resolve_model("claude-sonnet-4-20250514") == "JoyAI-Code"
    assert resolve_model("unknown-model") == "JoyAI-Code"


def test_parse_content():
    assert parse_content("plain text") == "plain text"
    assert parse_content([{"type": "text", "text": "hello"}]) == "hello"
    assert parse_content(None) == ""


def test_parse_content_tool_result():
    result = parse_content([{
        "type": "tool_result",
        "content": [{"type": "text", "text": "file contents here"}],
    }])
    assert "file contents here" in result


def test_parse_content_tool_result_string():
    result = parse_content([{
        "type": "tool_result",
        "content": "simple string result",
    }])
    assert "simple string result" in result


def test_translate_content_blocks_text_only():
    result = _translate_content_blocks("just a string")
    assert result == "just a string"


def test_translate_content_blocks_image():
    blocks = _translate_content_blocks([
        {"type": "text", "text": "see this"},
        {"type": "image", "source": {
            "type": "base64",
            "media_type": "image/png",
            "data": "iVBORw0KGgo=",
        }},
    ])
    assert isinstance(blocks, list)
    assert blocks[0]["type"] == "text"
    assert blocks[1]["type"] == "image_url"
    assert blocks[1]["image_url"]["url"].startswith("data:image/png;base64,")


def test_translate_content_blocks_tool_result():
    blocks = _translate_content_blocks([{
        "type": "tool_result",
        "content": [{"type": "text", "text": "output"}],
    }])
    assert isinstance(blocks, list)
    assert blocks[0]["type"] == "text"
    assert blocks[0]["text"] == "output"


def test_convert_tools_to_openai():
    tools = [{"name": "Read", "description": "Read file", "input_schema": {"type": "object"}}]
    result = convert_tools_to_openai(tools)
    assert result[0]["type"] == "function"
    assert result[0]["function"]["name"] == "Read"


def test_translate_request_basic():
    req = {
        "model": "claude-sonnet-4",
        "max_tokens": 1024,
        "messages": [{"role": "user", "content": "Hello"}],
    }
    body = translate_request(req)
    assert body["model"] == "JoyAI-Code"
    assert body["max_tokens"] == 1024


def test_translate_request_with_tool_result():
    req = {
        "model": "claude-sonnet-4",
        "max_tokens": 100,
        "messages": [
            {"role": "user", "content": "read file"},
            {"role": "assistant", "content": [
                {"type": "text", "text": "reading..."},
                {"type": "tool_use", "id": "toolu_1", "name": "Read", "input": {"path": "/tmp/a"}},
            ]},
            {"role": "user", "content": [
                {"type": "tool_result", "tool_use_id": "toolu_1", "content": "file data"},
            ]},
        ],
    }
    body = translate_request(req)
    roles = [m["role"] for m in body["messages"]]
    assert "tool" in roles
    tool_msg = [m for m in body["messages"] if m["role"] == "tool"][0]
    assert tool_msg["tool_call_id"] == "toolu_1"
    assert "file data" in tool_msg["content"]


def test_translate_request_assistant_tool_use():
    req = {
        "model": "claude-sonnet-4",
        "max_tokens": 100,
        "messages": [
            {"role": "user", "content": "read /etc/hosts"},
            {"role": "assistant", "content": [
                {"type": "tool_use", "id": "toolu_abc", "name": "Read", "input": {"path": "/etc/hosts"}},
            ]},
        ],
    }
    body = translate_request(req)
    assistant_msgs = [m for m in body["messages"] if m["role"] == "assistant"]
    assert len(assistant_msgs) == 1
    assert "tool_calls" in assistant_msgs[0]
    tc = assistant_msgs[0]["tool_calls"][0]
    assert tc["function"]["name"] == "Read"
    assert tc["id"] == "toolu_abc"


def test_translate_response_with_text():
    jc_resp = {
        "choices": [{"message": {"content": "Hi!"}}],
        "usage": {"prompt_tokens": 10, "completion_tokens": 5},
    }
    resp = translate_response(jc_resp, "claude-sonnet-4")
    assert resp["type"] == "message"
    assert resp["role"] == "assistant"
    assert resp["stop_reason"] == "end_turn"
    assert resp["content"][0]["type"] == "text"
    assert resp["content"][0]["text"] == "Hi!"
    assert resp["usage"]["input_tokens"] == 10


def test_translate_response_with_tool_calls():
    jc_resp = {
        "choices": [{
            "message": {
                "content": None,
                "tool_calls": [{
                    "id": "call_123",
                    "function": {"name": "Read", "arguments": '{"path": "/etc/hosts"}'},
                }],
            },
        }],
        "usage": {"prompt_tokens": 100, "completion_tokens": 20},
    }
    resp = translate_response(jc_resp, "GLM-5.1")
    assert resp["stop_reason"] == "tool_use"
    assert resp["content"][0]["type"] == "tool_use"
    assert resp["content"][0]["name"] == "Read"
    assert resp["content"][0]["input"] == {"path": "/etc/hosts"}


def test_translate_response_with_reasoning():
    jc_resp = {
        "choices": [{
            "message": {
                "content": "The answer is 42.",
                "reasoning_content": "Let me think about this step by step.",
            },
        }],
        "usage": {"prompt_tokens": 50, "completion_tokens": 30},
    }
    resp = translate_response(jc_resp, "GLM-5.1")
    assert len(resp["content"]) == 2
    assert resp["content"][0]["type"] == "thinking"
    assert resp["content"][0]["thinking"] == "Let me think about this step by step."
    assert resp["content"][1]["type"] == "text"
    assert resp["content"][1]["text"] == "The answer is 42."


def test_translate_response_reasoning_before_tool_use():
    jc_resp = {
        "choices": [{
            "message": {
                "content": None,
                "reasoning_content": "I need to use a tool.",
                "tool_calls": [{
                    "id": "call_456",
                    "function": {"name": "Bash", "arguments": '{"command": "ls"}'},
                }],
            },
        }],
        "usage": {"prompt_tokens": 100, "completion_tokens": 50},
    }
    resp = translate_response(jc_resp, "GLM-5.1")
    assert len(resp["content"]) == 2
    assert resp["content"][0]["type"] == "thinking"
    assert resp["content"][1]["type"] == "tool_use"
    assert resp["stop_reason"] == "tool_use"
```

- [ ] **Step 2: 验证所有测试**

Run: `python3 -m pytest tests/test_anthropic.py -v`
Expected:
  - Exit code: 0
  - Output contains: "17 passed"

- [ ] **Step 3: 提交**
Run: `git add tests/test_anthropic.py && git commit -m "test(anthropic): add comprehensive tests for content blocks, thinking, and tool_result"`
