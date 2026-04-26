# Go to Python Rewrite Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 将 JoyCodeProxy 从 Go 语言完整转换为 Python，保持所有功能不变：CLI 命令、凭证自动检测、OpenAI/Anthropic 兼容代理、tool use、SSE 流式、macOS 服务管理。

**Architecture:** Go Cobra CLI → Python Click CLI；Go net/http → FastAPI + uvicorn；Go net/http client → httpx；Go sqlite3 → Python sqlite3。功能模块 1:1 对应：auth.py / client.py / openai_handler.py / anthropic_handler.py / cli.py。CLI 入口使用 Click 组命令，复刻 serve/chat/models/whoami/service 子命令。

**Tech Stack:** Python 3.12, FastAPI 0.115, uvicorn 0.34, httpx 0.28, Click 8.2, sse-starlette 2.2, pytest 8.3

**Risks:**
- Task 4 Anthropic handler 的 tool_use SSE 转换逻辑复杂，是整个转换的难点 → 缓解：直接移植 Go 版已验证的逻辑
- Task 5 macOS plist 生成需要模板字符串 → 缓解：直接复制 Go 版的 plist 模板
- JoyCode API 的 gzip 响应需要 httpx 正确处理 → 缓解：httpx 默认处理 gzip

---

### Task 1: Project Scaffolding and Auth Module

**Depends on:** None
**Files:**
- Create: `pyproject.toml`
- Create: `joycode_proxy/__init__.py`
- Create: `joycode_proxy/auth.py`

- [ ] **Step 1: Create pyproject.toml — 定义项目依赖和入口**

```toml
# pyproject.toml
[project]
name = "joycode-proxy"
version = "0.1.0"
description = "JoyCode API proxy - OpenAI & Anthropic compatible"
requires-python = ">=3.12"
dependencies = [
    "fastapi>=0.115",
    "uvicorn>=0.34",
    "httpx>=0.28",
    "click>=8.2",
    "sse-starlette>=2.2",
]

[project.optional-dependencies]
dev = ["pytest>=8.3", "pytest-asyncio>=0.24"]

[project.scripts]
joycode-proxy = "joycode_proxy.cli:cli"

[build-system]
requires = ["hatchling"]
build-backend = "hatchling.backends"
```

- [ ] **Step 2: Create joycode_proxy/__init__.py — 包标识**

```python
# joycode_proxy/__init__.py
```

- [ ] **Step 3: Create joycode_proxy/auth.py — 凭证加载（移植 pkg/auth/credentials.go）**

```python
# joycode_proxy/auth.py
import json
import os
import platform
import sqlite3
from dataclasses import dataclass


@dataclass
class Credentials:
    pt_key: str
    user_id: str


def load_from_system() -> Credentials:
    if platform.system() != "Darwin":
        raise RuntimeError(
            "auto credential extraction only supported on macOS; "
            "on other systems, please provide --ptkey and --userid flags"
        )
    home = os.path.expanduser("~")
    db_path = os.path.join(
        home,
        "Library", "Application Support",
        "JoyCode", "User", "globalStorage", "state.vscdb",
    )
    if not os.path.exists(db_path):
        raise FileNotFoundError(
            f"JoyCode state database not found at {db_path}\n"
            "  Please install and log in to JoyCode IDE first"
        )
    conn = sqlite3.connect(f"file:{db_path}?mode=ro", uri=True)
    try:
        cursor = conn.execute(
            "SELECT value FROM ItemTable WHERE key='JoyCoder.IDE'"
        )
        row = cursor.fetchone()
        if not row:
            raise ValueError(
                "login info not found in database\n"
                "  Please log in to JoyCode IDE first"
            )
        data = json.loads(row[0])
        user = data.get("joyCoderUser", {})
        pt_key = user.get("ptKey", "")
        user_id = user.get("userId", "")
        if not pt_key:
            raise ValueError(
                "ptKey is empty in stored credentials\n"
                "  Please re-login to JoyCode IDE"
            )
        if not user_id:
            raise ValueError(
                "userId is empty in stored credentials\n"
                "  Please re-login to JoyCode IDE"
            )
        return Credentials(pt_key=pt_key, user_id=user_id)
    finally:
        conn.close()
```

- [ ] **Step 4: 验证 auth 模块**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && python3 -c "from joycode_proxy.auth import load_from_system; c = load_from_system(); print(f'userId={c.user_id}')"`
Expected:
  - Exit code: 0
  - Output contains: "userId="

- [ ] **Step 5: 提交**
Run: `git add pyproject.toml joycode_proxy/__init__.py joycode_proxy/auth.py && git commit -m "feat(python): add project scaffolding and auth module"`

---

### Task 2: JoyCode API Client

**Depends on:** Task 1
**Files:**
- Create: `joycode_proxy/client.py`

- [ ] **Step 1: Create joycode_proxy/client.py — HTTP 客户端（移植 pkg/joycode/）**

```python
# joycode_proxy/client.py
import os
import uuid
from typing import Any

import httpx

BASE_URL = "https://joycode-api.jd.com"
DEFAULT_MODEL = "JoyAI-Code"
CLIENT_VERSION = "2.4.5"
USER_AGENT = (
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) "
    "AppleWebKit/537.36 (KHTML, like Gecko) "
    "JoyCode/2.4.5 Chrome/133.0.0.0 Electron/35.2.0 Safari/537.36"
)
TIMEOUT = 120.0

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
        self._http = httpx.Client(timeout=TIMEOUT)

    def _headers(self) -> dict[str, str]:
        return {
            "Content-Type": "application/json; charset=UTF-8",
            "ptKey": self.pt_key,
            "loginType": "N_PIN_PC",
            "User-Agent": USER_AGENT,
            "Accept": "*/*",
            "Accept-Encoding": "gzip, deflate, br",
            "Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
            "Connection": "keep-alive",
        }

    def _prepare_body(self, extra: dict[str, Any] | None = None) -> dict[str, Any]:
        body: dict[str, Any] = {
            "tenant": "JOYCODE",
            "userId": self.user_id,
            "client": "JoyCode",
            "clientVersion": CLIENT_VERSION,
            "sessionId": self.session_id,
        }
        if extra:
            if "chatId" not in extra:
                body["chatId"] = _hex_id()
            if "requestId" not in extra:
                body["requestId"] = _hex_id()
            body.update(extra)
        else:
            body["chatId"] = _hex_id()
            body["requestId"] = _hex_id()
        return body

    def post(self, endpoint: str, body: dict[str, Any] | None = None) -> dict[str, Any]:
        resp = self._http.post(
            BASE_URL + endpoint,
            json=self._prepare_body(body),
            headers=self._headers(),
        )
        if resp.status_code != 200:
            raise RuntimeError(f"API error {resp.status_code}: {resp.text}")
        return resp.json()

    def post_stream(self, endpoint: str, body: dict[str, Any] | None = None):
        req = self._http.build_request(
            "POST",
            BASE_URL + endpoint,
            json=self._prepare_body(body),
            headers=self._headers(),
        )
        resp = self._http.send(req, stream=True)
        if resp.status_code != 200:
            resp.read()
            resp.close()
            raise RuntimeError(f"API error {resp.status_code}: {resp.text}")
        return resp

    def list_models(self) -> list[dict[str, Any]]:
        resp = self.post("/api/saas/models/v1/modelList")
        return resp.get("data", [])

    def web_search(self, query: str) -> list[Any]:
        body = {
            "messages": [{"role": "user", "content": query}],
            "stream": False,
            "model": "search_pro_jina",
            "language": "UNKNOWN",
        }
        resp = self.post("/api/saas/openai/v1/web-search", body)
        return resp.get("search_result", [])

    def rerank(self, query: str, documents: list[str], top_n: int) -> dict[str, Any]:
        return self.post("/api/saas/openai/v1/rerank", {
            "model": "Qwen3-Reranker-8B",
            "query": query,
            "documents": documents,
            "top_n": top_n,
        })

    def user_info(self) -> dict[str, Any]:
        return self.post("/api/saas/user/v1/userInfo")

    def validate(self) -> None:
        resp = self.user_info()
        code = resp.get("code", -1)
        if code != 0:
            msg = resp.get("msg", "unknown error")
            raise RuntimeError(
                f"credential validation failed (code={code}): {msg}"
            )
```

- [ ] **Step 2: 验证 client 模块**
Run: `python3 -c "from joycode_proxy.auth import load_from_system; from joycode_proxy.client import Client; c = load_from_system(); cl = Client(c.pt_key, c.user_id); cl.validate(); print('OK')"`
Expected:
  - Exit code: 0
  - Output contains: "OK"

- [ ] **Step 3: 提交**
Run: `git add joycode_proxy/client.py && git commit -m "feat(python): add JoyCode API client"`

---

### Task 3: OpenAI-Compatible Handler

**Depends on:** Task 2
**Files:**
- Create: `joycode_proxy/openai_handler.py`

- [ ] **Step 1: Create joycode_proxy/openai_handler.py — OpenAI 兼容端点（移植 pkg/openai/）**

```python
# joycode_proxy/openai_handler.py
import json
import time
from typing import Any

from fastapi import Request
from fastapi.responses import JSONResponse, StreamingResponse
from sse_starlette.sse import EventSourceResponse

from joycode_proxy.client import CHAT_ENDPOINT, Client, DEFAULT_MODEL, MODELS

MODEL_CAPABILITIES: dict[str, dict[str, Any]] = {
    "JoyAI-Code": {"vision": False, "reasoning": False, "max_tokens": 64000, "ctx": 200000},
    "MiniMax-M2.7": {"vision": False, "reasoning": True, "max_tokens": 16384, "ctx": 200000},
    "Kimi-K2.5": {"vision": True, "reasoning": False, "max_tokens": 16384, "ctx": 200000},
    "Kimi-K2.6": {"vision": True, "reasoning": True, "max_tokens": 16384, "ctx": 200000},
    "GLM-5.1": {"vision": False, "reasoning": True, "max_tokens": 16384, "ctx": 200000},
    "GLM-5": {"vision": False, "reasoning": False, "max_tokens": 8192, "ctx": 200000},
    "GLM-4.7": {"vision": False, "reasoning": False, "max_tokens": 8192, "ctx": 200000},
    "Doubao-Seed-2.0-pro": {"vision": False, "reasoning": False, "max_tokens": 16384, "ctx": 200000},
}

REASONING_MODELS = {"GLM-5.1", "Kimi-K2.6", "MiniMax-M2.7"}


def _short_id() -> str:
    return str(int(time.time_ns() % 10**12))


def default_model(model: str) -> str:
    return model or DEFAULT_MODEL


def translate_request(req_body: dict[str, Any]) -> dict[str, Any]:
    body: dict[str, Any] = {"model": req_body.get("model", ""), "stream": req_body.get("stream", False)}
    if "messages" in req_body:
        body["messages"] = req_body["messages"]
    if req_body.get("max_tokens"):
        body["max_tokens"] = req_body["max_tokens"]
    if "temperature" in req_body:
        body["temperature"] = req_body["temperature"]
    if "top_p" in req_body:
        body["top_p"] = req_body["top_p"]
    if "tools" in req_body:
        body["tools"] = req_body["tools"]
    if "tool_choice" in req_body:
        body["tool_choice"] = req_body["tool_choice"]
    if "stop" in req_body:
        body["stop"] = req_body["stop"]
    model = req_body.get("model", "")
    if "thinking" in req_body and model in REASONING_MODELS:
        body["thinking"] = req_body["thinking"]
    return body


def translate_response(jc_resp: dict[str, Any], model: str) -> dict[str, Any]:
    return {
        "id": f"chatcmpl-{_short_id()}",
        "object": "chat.completion",
        "created": int(time.time()),
        "model": model,
        "choices": jc_resp.get("choices"),
        "usage": jc_resp.get("usage"),
        "system_fingerprint": f"fp_{_short_id()}",
    }


def translate_models(jc_models: list[dict[str, Any]]) -> dict[str, Any]:
    data = []
    for m in jc_models:
        mid = m.get("modelId") or m.get("label", "")
        entry: dict[str, Any] = {
            "id": mid, "object": "model",
            "created": 1700000000, "owned_by": "joycode",
        }
        if mid in MODEL_CAPABILITIES:
            entry["capabilities"] = MODEL_CAPABILITIES[mid]
        data.append(entry)
    return {"object": "list", "data": data}


def create_openai_router(client: Client):
    from fastapi import APIRouter
    router = APIRouter()

    @router.post("/v1/chat/completions")
    async def chat_completions(request: Request):
        body = await request.json()
        model = default_model(body.get("model", ""))
        jc_body = translate_request(body)
        if body.get("stream"):
            return _stream_chat(client, jc_body, model)
        resp = client.post(CHAT_ENDPOINT, jc_body)
        return JSONResponse(translate_response(resp, model))

    @router.get("/v1/models")
    async def list_models():
        models = client.list_models()
        return JSONResponse(translate_models(models))

    @router.post("/v1/web-search")
    async def web_search(request: Request):
        body = await request.json()
        query = body.get("query", "")
        if not query:
            return JSONResponse({"error": {"message": "query is required", "type": "api_error"}}, 400)
        results = client.web_search(query)
        return JSONResponse({"search_result": results})

    @router.post("/v1/rerank")
    async def rerank(request: Request):
        body = await request.json()
        query = body.get("query", "")
        docs = body.get("documents", [])
        if not query or not docs:
            return JSONResponse({"error": {"message": "query and documents are required", "type": "api_error"}}, 400)
        result = client.rerank(query, docs, body.get("top_n", 5))
        return JSONResponse(result)

    @router.get("/health")
    async def health():
        return JSONResponse({
            "status": "ok",
            "service": "joycode-openai-proxy",
            "endpoints": ["/v1/chat/completions", "/v1/models", "/v1/web-search", "/v1/rerank"],
        })

    return router


def _stream_chat(client: Client, jc_body: dict[str, Any], model: str):
    def generate():
        resp = client.post_stream(CHAT_ENDPOINT, jc_body)
        try:
            for line in resp.iter_lines():
                if not line:
                    continue
                if line.startswith("data: ") and line[6:] == "[DONE]":
                    yield "data: [DONE]\n\n"
                    continue
                if line.startswith("data: "):
                    try:
                        chunk = json.loads(line[6:])
                        chunk.setdefault("id", f"chatcmpl-{_short_id()}")
                        chunk["model"] = model
                        chunk["object"] = "chat.completion.chunk"
                        yield f"data: {json.dumps(chunk)}\n\n"
                    except json.JSONDecodeError:
                        yield f"{line}\n\n"
                else:
                    yield f"{line}\n\n"
        finally:
            resp.close()

    return StreamingResponse(generate(), media_type="text/event-stream")
```

- [ ] **Step 2: 验证 OpenAI handler**
Run: `python3 -c "from joycode_proxy.openai_handler import translate_request, translate_response, translate_models; print('OK')"`
Expected:
  - Exit code: 0
  - Output contains: "OK"

- [ ] **Step 3: 提交**
Run: `git add joycode_proxy/openai_handler.py && git commit -m "feat(python): add OpenAI-compatible handler"`

---

### Task 4: Anthropic-Compatible Handler

**Depends on:** Task 2
**Files:**
- Create: `joycode_proxy/anthropic_handler.py`

- [ ] **Step 1: Create joycode_proxy/anthropic_handler.py — Anthropic 端点含 tool use（移植 pkg/anthropic/）**

```python
# joycode_proxy/anthropic_handler.py
import json
import uuid
from typing import Any

from fastapi import Request
from fastapi.responses import JSONResponse, StreamingResponse

from joycode_proxy.client import CHAT_ENDPOINT, Client, MODELS

MODEL_MAPPING: dict[str, str] = {
    "claude-sonnet-4-20250514": "JoyAI-Code",
    "claude-sonnet-4": "JoyAI-Code",
    "claude-opus-4-20250514": "JoyAI-Code",
    "claude-opus-4": "JoyAI-Code",
    "claude-haiku-4-5-20251001": "GLM-4.7",
    "claude-haiku-4-5": "GLM-4.7",
    "claude-3-5-sonnet-latest": "JoyAI-Code",
    "claude-3-5-sonnet-20241022": "JoyAI-Code",
    "claude-3-5-haiku-latest": "GLM-4.7",
    "claude-3-5-haiku-20241022": "GLM-4.7",
    "claude-3-haiku-20240307": "GLM-4.7",
}


def _msg_id() -> str:
    return f"msg_{uuid.uuid4().hex[:24]}"


def _tool_id() -> str:
    return f"toolu_{uuid.uuid4().hex[:24]}"


def resolve_model(model: str) -> str:
    if model in MODEL_MAPPING:
        return MODEL_MAPPING[model]
    if model in MODELS:
        return model
    return "JoyAI-Code"


def parse_content(raw: Any) -> str:
    if isinstance(raw, str):
        return raw
    if isinstance(raw, list):
        parts = [b.get("text", "") for b in raw if b.get("type") == "text"]
        return "\n".join(parts)
    return str(raw) if raw else ""


def convert_tools_to_openai(tools: list[dict[str, Any]]) -> list[dict[str, Any]]:
    result = []
    for t in tools:
        result.append({
            "type": "function",
            "function": {
                "name": t["name"],
                "description": t.get("description", ""),
                "parameters": t.get("input_schema", {}),
            },
        })
    return result


def translate_request(req: dict[str, Any]) -> dict[str, Any]:
    model = resolve_model(req.get("model", ""))
    messages = []
    if req.get("system"):
        sys_text = parse_content(req["system"])
        if sys_text:
            messages.append({"role": "system", "content": sys_text})
    for m in req.get("messages", []):
        messages.append({"role": m["role"], "content": parse_content(m.get("content", ""))})
    body: dict[str, Any] = {
        "model": model,
        "messages": messages,
        "stream": req.get("stream", False),
        "max_tokens": req.get("max_tokens", 8192),
    }
    if "temperature" in req:
        body["temperature"] = req["temperature"]
    if "top_p" in req:
        body["top_p"] = req["top_p"]
    if req.get("stop_sequences"):
        body["stop"] = req["stop_sequences"]
    if req.get("tools"):
        body["tools"] = convert_tools_to_openai(req["tools"])
    return body


def translate_response(jc_resp: dict[str, Any], req_model: str) -> dict[str, Any]:
    msg_id = _msg_id()
    choices = jc_resp.get("choices", [])
    content: list[dict[str, Any]] = []
    stop_reason = "end_turn"
    input_tokens = 0
    output_tokens = 0
    usage = jc_resp.get("usage", {})
    if isinstance(usage, dict):
        input_tokens = usage.get("prompt_tokens", 0) or 0
        output_tokens = usage.get("completion_tokens", 0) or 0
    if choices:
        choice = choices[0]
        msg = choice.get("message", {})
        tool_calls = msg.get("tool_calls")
        if tool_calls:
            stop_reason = "tool_use"
            for tc in tool_calls:
                fn = tc.get("function", {})
                tc_id = tc.get("id") or _tool_id()
                content.append({
                    "type": "tool_use",
                    "id": tc_id,
                    "name": fn.get("name", ""),
                    "input": json.loads(fn.get("arguments", "{}")),
                })
        else:
            text = msg.get("content", "")
            content.append({"type": "text", "text": text})
    return {
        "id": msg_id,
        "type": "message",
        "role": "assistant",
        "content": content,
        "model": req_model,
        "stop_reason": stop_reason,
        "stop_sequence": None,
        "usage": {"input_tokens": input_tokens, "output_tokens": output_tokens},
    }


def create_anthropic_router(client: Client):
    from fastapi import APIRouter
    router = APIRouter()

    @router.post("/v1/messages")
    async def messages(request: Request):
        body = await request.json()
        if body.get("stream"):
            return _handle_stream(client, body)
        jc_body = translate_request(body)
        jc_resp = client.post(CHAT_ENDPOINT, jc_body)
        return JSONResponse(translate_response(jc_resp, body.get("model", "")))

    return router


def _handle_stream(client: Client, req: dict[str, Any]):
    req_model = req.get("model", "")
    jc_body = translate_request(req)
    jc_body["stream"] = True

    def generate():
        msg_id = _msg_id()
        # message_start
        yield f"event: message_start\ndata: {json.dumps({'type': 'message_start', 'message': {'id': msg_id, 'type': 'message', 'role': 'assistant', 'model': req_model, 'content': [], 'stop_reason': '', 'stop_sequence': None, 'usage': {'input_tokens': 0, 'output_tokens': 0}}})}\n\n"
        yield f"event: ping\ndata: {json.dumps({'type': 'ping'})}\n\n"

        resp = client.post_stream(CHAT_ENDPOINT, jc_body)
        try:
            text_block_started = False
            tool_block_index = 0
            tool_calls_acc: dict[int, dict[str, str]] = {}
            tool_block_started: dict[int, bool] = {}
            current_block_index = 0
            total_output = 0

            for line in resp.iter_lines():
                if not line or not line.startswith("data: "):
                    continue
                data_str = line[6:]
                if data_str == "[DONE]":
                    break
                try:
                    chunk = json.loads(data_str)
                except json.JSONDecodeError:
                    continue
                choices = chunk.get("choices", [])
                if not choices:
                    continue
                choice = choices[0]
                delta = choice.get("delta", {})

                # Process tool_calls deltas
                for tc in delta.get("tool_calls", []):
                    idx = tc.get("index", 0)
                    if idx not in tool_calls_acc:
                        tool_calls_acc[idx] = {"id": "", "name": "", "arguments": ""}
                    if tc.get("id"):
                        tool_calls_acc[idx]["id"] = tc["id"]
                    fn = tc.get("function", {})
                    if fn.get("name"):
                        tool_calls_acc[idx]["name"] = fn["name"]
                    if fn.get("arguments"):
                        tool_calls_acc[idx]["arguments"] += fn["arguments"]
                    if idx not in tool_block_started:
                        # Close text block if open
                        if text_block_started:
                            yield f"event: content_block_stop\ndata: {json.dumps({'type': 'content_block_stop', 'index': current_block_index})}\n\n"
                            current_block_index += 1
                            text_block_started = False
                        tool_block_started[idx] = True
                        tc_id = tool_calls_acc[idx]["id"] or _tool_id()
                        yield f"event: content_block_start\ndata: {json.dumps({'type': 'content_block_start', 'index': current_block_index, 'content_block': {'type': 'tool_use', 'id': tc_id, 'name': tool_calls_acc[idx]['name']}})}\n\n"

                # Process text content
                text = delta.get("content", "")
                if text:
                    if not text_block_started:
                        text_block_started = True
                        yield f"event: content_block_start\ndata: {json.dumps({'type': 'content_block_start', 'index': current_block_index, 'content_block': {'type': 'text', 'text': ''}})}\n\n"
                    total_output += len(text)
                    yield f"event: content_block_delta\ndata: {json.dumps({'type': 'content_block_delta', 'index': current_block_index, 'delta': {'type': 'text_delta', 'text': text}})}\n\n"

                # Handle finish
                finish_reason = choice.get("finish_reason")
                if finish_reason:
                    if text_block_started:
                        yield f"event: content_block_stop\ndata: {json.dumps({'type': 'content_block_stop', 'index': current_block_index})}\n\n"
                        current_block_index += 1
                        text_block_started = False
                    for i in sorted(tool_calls_acc.keys()):
                        if tool_block_started.get(i):
                            tc = tool_calls_acc[i]
                            yield f"event: content_block_delta\ndata: {json.dumps({'type': 'content_block_delta', 'index': current_block_index, 'delta': {'type': 'input_json_delta', 'text': tc['arguments']}})}\n\n"
                            yield f"event: content_block_stop\ndata: {json.dumps({'type': 'content_block_stop', 'index': current_block_index})}\n\n"
                            current_block_index += 1
                    stop_reason = "tool_use" if finish_reason == "tool_calls" else "end_turn"
                    yield f"event: message_delta\ndata: {json.dumps({'type': 'message_delta', 'delta': {'stop_reason': stop_reason, 'stop_sequence': None}, 'usage': {'output_tokens': total_output // 4}})}\n\n"
                    yield f"event: message_stop\ndata: {json.dumps({'type': 'message_stop'})}\n\n"
        finally:
            resp.close()

    return StreamingResponse(generate(), media_type="text/event-stream")
```

- [ ] **Step 2: 验证 Anthropic handler**
Run: `python3 -c "from joycode_proxy.anthropic_handler import translate_request, translate_response, resolve_model; assert resolve_model('GLM-5.1') == 'GLM-5.1'; print('OK')"`
Expected:
  - Exit code: 0
  - Output contains: "OK"

- [ ] **Step 3: 提交**
Run: `git add joycode_proxy/anthropic_handler.py && git commit -m "feat(python): add Anthropic-compatible handler with tool use"`

---

### Task 5: CLI Commands

**Depends on:** Task 3, Task 4
**Files:**
- Create: `joycode_proxy/cli.py`
- Create: `joycode_proxy/server.py`

- [ ] **Step 1: Create joycode_proxy/server.py — FastAPI 应用组装**

```python
# joycode_proxy/server.py
from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from joycode_proxy.client import Client
from joycode_proxy.openai_handler import create_openai_router
from joycode_proxy.anthropic_handler import create_anthropic_router


def create_app(client: Client) -> FastAPI:
    app = FastAPI(title="JoyCode Proxy")
    app.add_middleware(
        CORSMiddleware,
        allow_origins=["*"],
        allow_methods=["*"],
        allow_headers=["*"],
    )
    app.include_router(create_openai_router(client))
    app.include_router(create_anthropic_router(client))
    return app
```

- [ ] **Step 2: Create joycode_proxy/cli.py — 完整 CLI（移植 cmd/）**

```python
# joycode_proxy/cli.py
import logging
import os
import subprocess
import sys
from pathlib import Path

import click

from joycode_proxy.auth import Credentials, load_from_system

log = logging.getLogger("joycode-proxy")


def _resolve_client(ptkey: str, userid: str, skip_validation: bool = False):
    from joycode_proxy.client import Client
    creds: Credentials
    source: str
    if ptkey and userid:
        creds = Credentials(pt_key=ptkey, user_id=userid)
        source = "flags"
    else:
        creds = load_from_system()
        source = "auto-detected"
        if ptkey:
            creds.pt_key = ptkey
            source = "flags+auto-detected"
        if userid:
            creds.user_id = userid
            source = "flags+auto-detected"
    log.info("Credentials source: %s (userId=%s)", source, creds.user_id)
    client = Client(creds.pt_key, creds.user_id)
    if skip_validation:
        log.info("Credential validation skipped (--skip-validation)")
        return client
    log.info("Validating credentials...")
    client.validate()
    log.info("Credentials validated successfully")
    return client


@click.group()
@click.option("-k", "--ptkey", default="", help="JoyCode ptKey (auto-detected if empty)")
@click.option("-u", "--userid", default="", help="JoyCode userID (auto-detected if empty)")
@click.option("--skip-validation", is_flag=True, help="Skip credential validation")
@click.pass_context
def cli(ctx, ptkey: str, userid: str, skip_validation: bool):
    ctx.ensure_object(dict)
    ctx.obj["ptkey"] = ptkey
    ctx.obj["userid"] = userid
    ctx.obj["skip_validation"] = skip_validation


@cli.command()
@click.option("-H", "--host", default="0.0.0.0", help="Bind host")
@click.option("-p", "--port", default=34891, help="Bind port")
@click.pass_context
def serve(ctx, host: str, port: int):
    import uvicorn
    client = _resolve_client(ctx.obj["ptkey"], ctx.obj["userid"], ctx.obj["skip_validation"])
    from joycode_proxy.server import create_app
    app = create_app(client)
    click.echo(f"  Endpoints:")
    click.echo(f"    POST /v1/chat/completions  — Chat (OpenAI format)")
    click.echo(f"    POST /v1/messages          — Chat (Anthropic/Claude Code format)")
    click.echo(f"    POST /v1/web-search        — Web Search")
    click.echo(f"    POST /v1/rerank            — Rerank documents")
    click.echo(f"    GET  /v1/models            — Model list")
    click.echo(f"    GET  /health               — Health check")
    click.echo()
    click.echo(f"  Claude Code setup:")
    click.echo(f"    export ANTHROPIC_BASE_URL=http://{host}:{port}")
    click.echo(f"    export ANTHROPIC_API_KEY=joycode")
    uvicorn.run(app, host=host, port=port, log_level="info")


@cli.command()
@click.argument("message")
@click.option("-m", "--model", default="JoyAI-Code", help="Model name")
@click.option("-s", "--stream", is_flag=True, help="Stream output")
@click.option("--max-tokens", default=64000, help="Max output tokens")
@click.pass_context
def chat(ctx, message: str, model: str, stream: bool, max_tokens: int):
    client = _resolve_client(ctx.obj["ptkey"], ctx.obj["userid"], ctx.obj["skip_validation"])
    body = {
        "model": model,
        "messages": [{"role": "user", "content": message}],
        "stream": False,
        "max_tokens": max_tokens,
    }
    if stream:
        body["stream"] = True
        resp = client.post_stream("/api/saas/openai/v1/chat/completions", body)
        try:
            for line in resp.iter_lines():
                if line:
                    click.echo(line)
        finally:
            resp.close()
        return
    resp = client.post("/api/saas/openai/v1/chat/completions", body)
    choices = resp.get("choices", [])
    if choices:
        click.echo(choices[0].get("message", {}).get("content", ""))


@cli.command()
@click.pass_context
def models(ctx):
    from joycode_proxy.client import DEFAULT_MODEL
    client = _resolve_client(ctx.obj["ptkey"], ctx.obj["userid"], ctx.obj["skip_validation"])
    model_list = client.list_models()
    for m in model_list:
        label = m.get("label", "")
        api_model = m.get("chatApiModel", "")
        ctx_max = m.get("maxTotalTokens", 0)
        out_max = m.get("respMaxTokens", 0)
        pref = " *" if api_model == DEFAULT_MODEL else ""
        click.echo(f"  {label} ({api_model}) ctx={ctx_max} out={out_max}{pref}")


@cli.command()
@click.pass_context
def whoami(ctx):
    client = _resolve_client(ctx.obj["ptkey"], ctx.obj["userid"], ctx.obj["skip_validation"])
    resp = client.user_info()
    data = resp.get("data", {})
    click.echo(f"  用户: {data.get('realName', 'N/A')}")
    click.echo(f"  ID: {data.get('userId', 'N/A')}")
    click.echo(f"  组织: {data.get('orgName', 'N/A')}")
    click.echo(f"  租户: {data.get('tenant', 'N/A')}")
    status = "有效" if resp.get("code") == 0 else "无效"
    click.echo(f"  状态: {status}")


@cli.group()
def service():
    pass


@service.command("install")
@click.option("-p", "--port", default=34891, help="Bind port")
@click.pass_context
def service_install(ctx, port: int):
    bin_path = Path(sys.executable).resolve()
    home = Path.home()
    log_dir = home / ".joycode-proxy" / "logs"
    log_dir.mkdir(parents=True, exist_ok=True)
    plist_path = home / "Library" / "LaunchAgents" / "com.joycode.proxy.plist"
    plist = f"""<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key><string>com.joycode.proxy</string>
    <key>ProgramArguments</key>
    <array>
        <string>{bin_path}</string>
        <string>-m</string>
        <string>joycode_proxy.cli</string>
        <string>serve</string>
        <string>--port</string>
        <string>{port}</string>
    </array>
    <key>RunAtLoad</key><true/>
    <key>KeepAlive</key><true/>
    <key>ThrottleInterval</key><integer>10</integer>
    <key>StandardOutPath</key><string>{log_dir / "stdout.log"}</string>
    <key>StandardErrorPath</key><string>{log_dir / "stderr.log"}</string>
    <key>EnvironmentVariables</key><dict><key>HOME</key><string>{home}</string></dict>
</dict>
</plist>"""
    plist_path.write_text(plist)
    subprocess.run(["launchctl", "load", str(plist_path)], check=True)
    click.echo(f"Service installed and started.")
    click.echo(f"  Label:   com.joycode.proxy")
    click.echo(f"  Plist:   {plist_path}")
    click.echo(f"  Port:    {port}")
    click.echo(f"  Logs:    {log_dir}/")


@service.command("uninstall")
def service_uninstall():
    home = Path.home()
    plist_path = home / "Library" / "LaunchAgents" / "com.joycode.proxy.plist"
    subprocess.run(["launchctl", "unload", str(plist_path)], capture_output=True)
    if plist_path.exists():
        plist_path.unlink()
        click.echo("Service stopped and removed.")
    else:
        click.echo("Service not installed (plist not found).")


@service.command("status")
def service_status():
    home = Path.home()
    plist_path = home / "Library" / "LaunchAgents" / "com.joycode.proxy.plist"
    if not plist_path.exists():
        click.echo("Service not installed.")
        return
    result = subprocess.run(["launchctl", "list"], capture_output=True, text=True)
    found = False
    for line in result.stdout.splitlines():
        if "com.joycode.proxy" in line:
            click.echo(f"Service status: {line}")
            found = True
            break
    if not found:
        click.echo("Service installed but not running.")
    click.echo(f"\nLogs: {home / '.joycode-proxy' / 'logs'}/")


if __name__ == "__main__":
    cli()
```

- [ ] **Step 3: 验证 CLI help**
Run: `pip install -e . 2>&1 | tail -1 && python3 -m joycode_proxy.cli --help`
Expected:
  - Exit code: 0
  - Output contains: "serve" and "chat" and "models" and "whoami" and "service"

- [ ] **Step 4: 提交**
Run: `git add joycode_proxy/cli.py joycode_proxy/server.py && git commit -m "feat(python): add CLI commands and FastAPI server assembly"`

---

### Task 6: Tests, Dockerfile, and Final Verification

**Depends on:** Task 5
**Files:**
- Create: `tests/test_auth.py`
- Create: `tests/test_anthropic.py`
- Create: `Dockerfile.python`（保留原 Dockerfile 给 Go 版）
- Create: `.gitignore` 更新

- [ ] **Step 1: Create tests/test_auth.py**

```python
# tests/test_auth.py
import os
import tempfile

import pytest

from joycode_proxy.auth import Credentials, load_from_system


def test_credentials_fields():
    c = Credentials(pt_key="test-key", user_id="test-user")
    assert c.pt_key == "test-key"
    assert c.user_id == "test-user"


def test_load_from_system_database_not_found(monkeypatch):
    with tempfile.TemporaryDirectory() as tmpdir:
        monkeypatch.setenv("HOME", tmpdir)
        with pytest.raises(FileNotFoundError, match="not found"):
            load_from_system()


def test_load_from_system_integration():
    home = os.path.expanduser("~")
    db_path = os.path.join(
        home, "Library", "Application Support",
        "JoyCode", "User", "globalStorage", "state.vscdb",
    )
    if not os.path.exists(db_path):
        pytest.skip("JoyCode database not found")
    creds = load_from_system()
    assert creds.pt_key
    assert creds.user_id
```

- [ ] **Step 2: Create tests/test_anthropic.py**

```python
# tests/test_anthropic.py
from joycode_proxy.anthropic_handler import (
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
```

- [ ] **Step 3: Create Dockerfile.python**

```dockerfile
FROM python:3.12-slim

WORKDIR /app
COPY pyproject.toml .
COPY joycode_proxy/ joycode_proxy/
RUN pip install --no-cache-dir .

EXPOSE 34891
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD python -c "import urllib.request; urllib.request.urlopen('http://localhost:34891/health')" || exit 1

ENTRYPOINT ["python", "-m", "joycode_proxy.cli"]
CMD ["serve"]
```

- [ ] **Step 4: 验证全部测试通过**
Run: `pip install -e ".[dev]" 2>&1 | tail -1 && python3 -m pytest tests/ -v`
Expected:
  - Exit code: 0
  - Output contains: "passed"
  - Output does NOT contain: "FAIL"

- [ ] **Step 5: 验证 Python 版 serve 启动**
Run: `python3 -m joycode_proxy.cli serve --skip-validation --port 34892 &
sleep 3 && curl -s http://localhost:34892/health && kill %1 2>/dev/null`
Expected:
  - Exit code: 0
  - Output contains: "ok"

- [ ] **Step 6: 提交**
Run: `git add tests/ Dockerfile.python && git commit -m "feat(python): add tests, Dockerfile, and final integration"`
