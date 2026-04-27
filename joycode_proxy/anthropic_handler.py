import json
import logging
import uuid
from typing import Any, Dict, Iterator, List, Optional

from fastapi import APIRouter, Request
from fastapi.responses import JSONResponse, StreamingResponse

from joycode_proxy.client import CHAT_ENDPOINT, MODELS
from joycode_proxy.credential_router import CredentialRouter

log = logging.getLogger("joycode-proxy.anthropic")

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


def _new_id() -> str:
    return uuid.uuid4().hex[:24]


def resolve_model(model: str, account_default_model: str = "") -> str:
    if model in MODEL_MAPPING:
        return MODEL_MAPPING[model]
    if model in MODELS:
        return model
    if account_default_model:
        return account_default_model
    return "JoyAI-Code"


# ---------------------------------------------------------------------------
# litellm adapter — battle-tested Anthropic ↔ OpenAI protocol conversion
# ---------------------------------------------------------------------------

from litellm.llms.anthropic.experimental_pass_through.adapters.transformation import (
    AnthropicAdapter,
)
from litellm.llms.anthropic.experimental_pass_through.adapters.streaming_iterator import (
    AnthropicStreamWrapper,
)
from litellm.types.utils import ModelResponse, StreamingChoices, Usage

_adapter = AnthropicAdapter()


def _translate_request(anthropic_body: Dict[str, Any], account_default_model: str = "") -> tuple:
    """Convert Anthropic Messages API request to OpenAI format using litellm.

    Returns (openai_kwargs, tool_name_mapping, joycode_model).
    """
    kwargs = dict(anthropic_body)
    requested_model = kwargs.get("model", "")
    joycode_model = resolve_model(requested_model, account_default_model)
    kwargs["model"] = requested_model

    openai_kwargs, tool_name_mapping = _adapter.translate_completion_input_params_with_tool_mapping(
        kwargs
    )

    # Replace the model name with JoyCode model
    openai_kwargs["model"] = joycode_model

    # JoyCode only supports tool_choice "auto", normalize others
    tc = openai_kwargs.get("tool_choice")
    if tc and tc != "auto":
        openai_kwargs["tool_choice"] = "auto"

    return openai_kwargs, tool_name_mapping, requested_model


def _clean_content_blocks(content: list) -> list:
    """Remove internal litellm fields from content blocks."""
    cleaned = []
    for block in content:
        if isinstance(block, dict):
            block = {k: v for k, v in block.items() if v is not None and k != "provider_specific_fields"}
            cleaned.append(block)
        else:
            cleaned.append(block)
    return cleaned


def _translate_response(jc_resp: Dict[str, Any], requested_model: str, tool_name_mapping: Dict[str, str]) -> Dict[str, Any]:
    """Convert JoyCode OpenAI-format response to Anthropic format using litellm."""
    model_response = ModelResponse(**jc_resp)
    anthropic_resp = _adapter.translate_completion_output_params(
        model_response, tool_name_mapping=tool_name_mapping
    )
    if anthropic_resp is not None:
        result = dict(anthropic_resp) if hasattr(anthropic_resp, "__iter__") else anthropic_resp
        if isinstance(result, dict):
            result["model"] = requested_model
            if "content" in result:
                result["content"] = _clean_content_blocks(result["content"])
            return result
    # Fallback
    return {
        "id": "msg_" + _new_id(),
        "type": "message",
        "role": "assistant",
        "model": requested_model,
        "content": [{"type": "text", "text": str(jc_resp)}],
        "stop_reason": "end_turn",
        "stop_sequence": None,
        "usage": {"input_tokens": 0, "output_tokens": 0},
    }


# ---------------------------------------------------------------------------
# Streaming handler — uses litellm's AnthropicStreamWrapper
# ---------------------------------------------------------------------------

async def _handle_stream(client, req: Dict[str, Any], account_default_model: str = "") -> StreamingResponse:
    """Produce an SSE StreamingResponse using litellm's AnthropicStreamWrapper."""

    openai_kwargs, tool_name_mapping, requested_model = _translate_request(req, account_default_model)
    openai_kwargs["stream"] = True
    joycode_model = openai_kwargs["model"]

    from litellm.types.utils import ModelResponseStream, StreamingChoices, ChatCompletionDeltaToolCall, Function

    def _raw_chunks_to_model_response_stream(resp):
        for raw_line in resp.iter_lines():
            if not raw_line:
                continue
            line = raw_line
            if isinstance(line, bytes):
                line = line.decode("utf-8", errors="replace")
            if line.startswith("data: "):
                line = line[6:]
            line = line.strip()
            if not line or line == "[DONE]":
                continue
            try:
                chunk_data = json.loads(line)
            except json.JSONDecodeError:
                continue

            choices = chunk_data.get("choices") or []
            if not choices:
                continue

            c = choices[0]
            delta = c.get("delta", {})
            msg = {}

            # Only include content when it has actual text
            # JoyCode sends content="" even during reasoning/tool_calls,
            # which confuses litellm's block type detection
            content = delta.get("content")
            if content:
                msg["content"] = content

            if delta.get("reasoning_content"):
                msg["reasoning_content"] = delta["reasoning_content"]

            tool_calls_raw = delta.get("tool_calls")
            if tool_calls_raw:
                tc_list = []
                for tc in tool_calls_raw:
                    fn = tc.get("function", {})
                    tc_obj = ChatCompletionDeltaToolCall(
                        index=tc.get("index", 0),
                        id=tc.get("id"),
                        type="function",
                        function=Function(
                            name=fn.get("name"),
                            arguments=fn.get("arguments", ""),
                        ),
                    )
                    tc_list.append(tc_obj)
                msg["tool_calls"] = tc_list

            stream_choice = StreamingChoices(
                index=c.get("index", 0),
                delta=msg,
                finish_reason=c.get("finish_reason"),
            )

            stream_resp = ModelResponseStream(
                id=chunk_data.get("id", "chatcmpl-" + _new_id()),
                model=joycode_model,
                choices=[stream_choice],
            )

            usage = chunk_data.get("usage")
            if usage:
                stream_resp.usage = Usage(
                    prompt_tokens=usage.get("prompt_tokens", 0),
                    completion_tokens=usage.get("completion_tokens", 0),
                    total_tokens=usage.get("total_tokens", 0),
                )

            yield stream_resp

    def _generate():
        try:
            resp = client.post_stream(CHAT_ENDPOINT, openai_kwargs)
        except Exception as exc:
            log.error("Stream connection failed: %s", exc)
            yield f"event: error\ndata: {json.dumps({'type': 'error', 'error': {'type': 'api_error', 'message': str(exc)}})}\n\n"
            return

        raw_stream = _raw_chunks_to_model_response_stream(resp)
        wrapper = AnthropicStreamWrapper(
            completion_stream=raw_stream,
            model=requested_model,
            tool_name_mapping=tool_name_mapping,
        )

        try:
            for sse_bytes in wrapper.anthropic_sse_wrapper():
                if isinstance(sse_bytes, bytes):
                    yield sse_bytes.decode("utf-8", errors="replace")
                else:
                    yield sse_bytes
        except Exception as exc:
            log.error("Stream wrapper error: %s", exc)
            yield f"event: error\ndata: {json.dumps({'type': 'error', 'error': {'type': 'api_error', 'message': str(exc)}})}\n\n"
        finally:
            resp.close()

    return StreamingResponse(
        _generate(),
        media_type="text/event-stream",
        headers={
            "Cache-Control": "no-cache",
            "Connection": "keep-alive",
            "Access-Control-Allow-Origin": "*",
            "X-Accel-Buffering": "no",
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


def create_anthropic_router(cred_router: CredentialRouter) -> APIRouter:
    router = APIRouter()

    @router.post("/v1/messages")
    async def handle_messages(request: Request):  # type: ignore[return]
        api_key = request.headers.get("x-api-key", "")
        client = cred_router.get_client(api_key or None)
        account_default_model = cred_router.get_default_model(api_key or None)

        body = await request.json()
        log.debug("POST /v1/messages model=%s stream=%s tools=%d key=%s account_model=%s",
                  body.get("model"), body.get("stream"), len(body.get("tools", [])),
                  api_key[:8] + "..." if api_key else "default", account_default_model)

        if not body.get("max_tokens"):
            body["max_tokens"] = 8192

        if body.get("stream"):
            return await _handle_stream(client, body, account_default_model)

        openai_kwargs, tool_name_mapping, requested_model = _translate_request(body, account_default_model)
        try:
            jc_resp = client.post(CHAT_ENDPOINT, openai_kwargs)
        except Exception as exc:
            log.error("JoyCode API error: %s", exc)
            return _error_response(500, str(exc))

        resp = _translate_response(jc_resp, requested_model, tool_name_mapping)
        log.debug("Response: stop_reason=%s content_blocks=%d",
                  resp.get("stop_reason"), len(resp.get("content", [])))
        return JSONResponse(content=resp)

    return router
