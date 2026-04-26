import json
import uuid
from typing import Any, Dict, List, Optional

from fastapi import APIRouter, Request
from fastapi.responses import JSONResponse, StreamingResponse

from joycode_proxy.client import CHAT_ENDPOINT, Client, MODELS

# ---------------------------------------------------------------------------
# Model mapping: Claude model name -> JoyCode model ID
# ---------------------------------------------------------------------------

MODEL_MAPPING: Dict[str, str] = {
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

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _new_id() -> str:
    """Return a random 24-char hex string."""
    return uuid.uuid4().hex[:24]


def _new_message_id() -> str:
    return "msg_" + _new_id()


def resolve_model(model: str) -> str:
    """Map an Anthropic model name to a JoyCode model ID.

    Order of resolution:
    1. Exact match in MODEL_MAPPING
    2. Passthrough if *model* is already a known JoyCode model
    3. Fallback to the default JoyCode model
    """
    if model in MODEL_MAPPING:
        return MODEL_MAPPING[model]
    if model in MODELS:
        return model
    return "JoyAI-Code"


def parse_content(raw: Any) -> str:
    """Extract plain-text from an Anthropic message *content* field.

    The value may be:
    - a plain string
    - a list of content blocks (dicts with ``type`` / ``text``)
    - anything else -> fall back to ``str()``
    """
    if isinstance(raw, str):
        return raw
    if isinstance(raw, list):
        parts: List[str] = []
        for block in raw:
            if isinstance(block, dict) and block.get("type") == "text":
                parts.append(block.get("text", ""))
        return "\n".join(parts)
    if raw is None:
        return ""
    # Fallback: treat as raw JSON string
    if isinstance(raw, bytes):
        return raw.decode("utf-8", errors="replace")
    return str(raw)


# ---------------------------------------------------------------------------
# Tool conversion
# ---------------------------------------------------------------------------


def convert_tools_to_openai(tools: List[Dict[str, Any]]) -> List[Dict[str, Any]]:
    """Convert Anthropic-style tool definitions to OpenAI function-calling format."""
    result: List[Dict[str, Any]] = []
    for t in tools:
        result.append({
            "type": "function",
            "function": {
                "name": t.get("name", ""),
                "description": t.get("description", ""),
                "parameters": t.get("input_schema", {}),
            },
        })
    return result


# ---------------------------------------------------------------------------
# Request / Response translation
# ---------------------------------------------------------------------------


def translate_request(req: Dict[str, Any]) -> Dict[str, Any]:
    """Convert an Anthropic Messages API request body to a JoyCode/OpenAI body."""
    model = resolve_model(req.get("model", ""))

    # Build messages list
    messages: List[Dict[str, Any]] = []

    # Prepend system prompt if present
    system = req.get("system")
    if system is not None:
        sys_text = parse_content(system)
        if sys_text:
            messages.append({"role": "system", "content": sys_text})

    for m in req.get("messages", []):
        messages.append({
            "role": m.get("role", "user"),
            "content": parse_content(m.get("content")),
        })

    body: Dict[str, Any] = {
        "model": model,
        "messages": messages,
        "stream": req.get("stream", False),
    }

    # max_tokens (default 8192 if not set or zero)
    max_tokens = req.get("max_tokens", 0)
    if max_tokens:
        body["max_tokens"] = max_tokens
    else:
        body["max_tokens"] = 8192

    # Optional parameters
    if "temperature" in req and req["temperature"] is not None:
        body["temperature"] = req["temperature"]
    if "top_p" in req and req["top_p"] is not None:
        body["top_p"] = req["top_p"]
    if req.get("stop_sequences"):
        body["stop"] = req["stop_sequences"]
    if req.get("tools"):
        body["tools"] = convert_tools_to_openai(req["tools"])

    return body


def translate_response(jc_resp: Dict[str, Any], req_model: str) -> Dict[str, Any]:
    """Convert a JoyCode API response to Anthropic Messages format."""
    msg_id = _new_message_id()

    # Extract usage
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
            # Parse arguments string into an object for the Anthropic input field
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


# ---------------------------------------------------------------------------
# SSE helpers
# ---------------------------------------------------------------------------


def _sse_event(event: str, data: Any) -> str:
    """Format a single SSE event string."""
    return "event: {}\ndata: {}\n\n".format(event, json.dumps(data))


# ---------------------------------------------------------------------------
# Streaming handler
# ---------------------------------------------------------------------------


async def _handle_stream(client: Client, req: Dict[str, Any]) -> StreamingResponse:
    """Produce an SSE StreamingResponse that wraps JoyCode chunks in Anthropic format."""

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
        text_block_started = False
        tool_block_started: Dict[int, bool] = {}

        for raw_line in resp.iter_lines():
            if not raw_line:
                continue
            line = raw_line
            if isinstance(line, bytes):
                line = line.decode("utf-8", errors="replace")

            # Strip "data: " prefix
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
                    # Close any open text block first
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
                # Close any open text block
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

    return StreamingResponse(
        _generator(),
        media_type="text/event-stream",
        headers={
            "Cache-Control": "no-cache",
            "Connection": "keep-alive",
            "Access-Control-Allow-Origin": "*",
        },
    )


# ---------------------------------------------------------------------------
# Error response helper
# ---------------------------------------------------------------------------


def _error_response(status_code: int, message: str) -> JSONResponse:
    return JSONResponse(
        status_code=status_code,
        content={
            "type": "error",
            "error": {"type": "api_error", "message": message},
        },
    )


# ---------------------------------------------------------------------------
# FastAPI router
# ---------------------------------------------------------------------------


def create_anthropic_router(client: Client) -> APIRouter:
    """Create and return a FastAPI router that serves the Anthropic Messages API."""

    router = APIRouter()

    @router.post("/v1/messages")
    async def handle_messages(request: Request):  # type: ignore[return]
        body = await request.json()

        # Ensure max_tokens has a sensible default
        if not body.get("max_tokens"):
            body["max_tokens"] = 8192

        if body.get("stream"):
            return await _handle_stream(client, body)

        # Non-streaming path
        jc_body = translate_request(body)
        try:
            jc_resp = client.post(CHAT_ENDPOINT, jc_body)
        except Exception as exc:
            return _error_response(500, str(exc))

        resp = translate_response(jc_resp, body.get("model", ""))
        return JSONResponse(content=resp)

    return router
