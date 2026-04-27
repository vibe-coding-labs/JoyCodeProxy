"""OpenAI-compatible API handler for the JoyCode proxy.

Translates between OpenAI-format requests/responses and the upstream
JoyCode API, providing a drop-in OpenAI-compatible server surface.
"""

import json
import time
from typing import Any, Dict, List, Optional

from fastapi import APIRouter, Request
from fastapi.responses import JSONResponse, StreamingResponse

from joycode_proxy.client import CHAT_ENDPOINT, DEFAULT_MODEL, MODELS
from joycode_proxy.credential_router import CredentialRouter

# ---------------------------------------------------------------------------
# Model capabilities and reasoning model set
# ---------------------------------------------------------------------------

MODEL_CAPABILITIES: Dict[str, Dict[str, Any]] = {
    "JoyAI-Code": {"max_tokens": 64000, "ctx": 200000},
    "MiniMax-M2.7": {"reasoning": True, "max_tokens": 16384, "ctx": 200000},
    "Kimi-K2.5": {"vision": True, "max_tokens": 16384, "ctx": 200000},
    "Kimi-K2.6": {"vision": True, "reasoning": True, "max_tokens": 16384, "ctx": 200000},
    "GLM-5.1": {"reasoning": True, "max_tokens": 16384, "ctx": 200000},
    "GLM-5": {"max_tokens": 8192, "ctx": 200000},
    "GLM-4.7": {"max_tokens": 8192, "ctx": 200000},
    "Doubao-Seed-2.0-pro": {"max_tokens": 16384, "ctx": 200000},
}

REASONING_MODELS = {"GLM-5.1", "Kimi-K2.6", "MiniMax-M2.7"}

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _short_id() -> str:
    """Return a short numeric ID derived from the current time."""
    return str(int(time.time() * 1e6) % 10**12)


def _error_response(message: str, status_code: int = 500) -> JSONResponse:
    return JSONResponse(
        status_code=status_code,
        content={"error": {"message": message, "type": "api_error"}},
    )


# ---------------------------------------------------------------------------
# Translation helpers
# ---------------------------------------------------------------------------


def translate_request(req_body: Dict[str, Any]) -> Dict[str, Any]:
    """Convert an OpenAI chat-completion request body to a JoyCode API body.

    Only fields recognised by the upstream API are forwarded; everything
    else is silently dropped so callers can send a full OpenAI payload
    without causing errors.
    """
    model: str = req_body.get("model", DEFAULT_MODEL) or DEFAULT_MODEL
    stream: bool = bool(req_body.get("stream", False))

    body: Dict[str, Any] = {
        "model": model,
        "stream": stream,
    }

    # Forward messages (already a list of dicts in JSON-parsed body)
    messages = req_body.get("messages")
    if messages:
        body["messages"] = messages

    # Optional scalar fields
    max_tokens = req_body.get("max_tokens")
    if max_tokens is not None and max_tokens > 0:
        body["max_tokens"] = max_tokens

    temperature = req_body.get("temperature")
    if temperature is not None:
        body["temperature"] = temperature

    top_p = req_body.get("top_p")
    if top_p is not None:
        body["top_p"] = top_p

    # Tool use
    tools = req_body.get("tools")
    if tools:
        body["tools"] = tools

    tool_choice = req_body.get("tool_choice")
    if tool_choice is not None:
        body["tool_choice"] = tool_choice

    # Stop sequences
    stop = req_body.get("stop")
    if stop:
        body["stop"] = stop

    # Thinking / reasoning — only for models that support it
    thinking = req_body.get("thinking")
    if thinking and model in REASONING_MODELS:
        body["thinking"] = thinking

    return body


def translate_response(
    jc_resp: Dict[str, Any], model: str
) -> Dict[str, Any]:
    """Convert a JoyCode non-streaming response to OpenAI format."""
    return {
        "id": "chatcmpl-" + _short_id(),
        "object": "chat.completion",
        "created": int(time.time()),
        "model": model,
        "choices": jc_resp.get("choices"),
        "usage": jc_resp.get("usage"),
        "system_fingerprint": "fp_" + _short_id(),
    }


def translate_models(jc_models: List[Dict[str, Any]]) -> Dict[str, Any]:
    """Convert a JoyCode model list to OpenAI ``/v1/models`` format."""
    data: List[Dict[str, Any]] = []
    for m in jc_models:
        mid: str = m.get("modelId") or m.get("label") or ""
        entry: Dict[str, Any] = {
            "id": mid,
            "object": "model",
            "created": 1700000000,
            "owned_by": "joycode",
        }
        caps = MODEL_CAPABILITIES.get(mid)
        if caps is not None:
            entry["capabilities"] = caps
        data.append(entry)
    return {"object": "list", "data": data}


# ---------------------------------------------------------------------------
# SSE streaming helper
# ---------------------------------------------------------------------------


def _stream_chat(
    client, jc_body: Dict[str, Any], model: str
) -> StreamingResponse:
    """Return a ``StreamingResponse`` that pipes JoyCode SSE chunks,
    enriching each data line with ``model``, ``object``, and ``id`` fields
    so clients receive a fully-formed OpenAI streaming payload."""

    def _generate() -> Any:
        try:
            resp = client.post_stream(CHAT_ENDPOINT, jc_body)
        except Exception as exc:
            yield "data: {}\n\n".format(
                json.dumps({"error": {"message": str(exc)}})
            )
            yield "data: [DONE]\n\n"
            return

        chat_id = "chatcmpl-" + _short_id()
        try:
            for raw_line in resp.iter_lines():
                if not raw_line:
                    continue
                # JoyCode already sends ``data: …`` lines in OpenAI SSE
                # format, but they may lack model/id/object fields.
                if raw_line.startswith("data: "):
                    payload = raw_line[6:]  # strip "data: " prefix
                else:
                    payload = raw_line

                if payload == "[DONE]":
                    yield "data: [DONE]\n\n"
                    continue

                # Try to parse and enrich the chunk
                try:
                    chunk = json.loads(payload)
                except (json.JSONDecodeError, ValueError):
                    # Not JSON — forward as-is
                    yield "data: {}\n\n".format(payload)
                    continue

                # Add required OpenAI fields if absent
                if "id" not in chunk:
                    chunk["id"] = chat_id
                chunk["model"] = model
                chunk["object"] = "chat.completion.chunk"

                yield "data: {}\n\n".format(json.dumps(chunk))
        finally:
            resp.close()

    return StreamingResponse(
        _generate(),
        media_type="text/event-stream",
        headers={
            "Cache-Control": "no-cache",
            "Connection": "close",
            "X-Accel-Buffering": "no",
        },
    )


# ---------------------------------------------------------------------------
# FastAPI router
# ---------------------------------------------------------------------------


def create_openai_router(cred_router: CredentialRouter) -> APIRouter:
    """Create and return a :class:`FastAPI.APIRouter` that exposes the
    OpenAI-compatible endpoints.

    Endpoints
    ---------
    POST /v1/chat/completions
        Chat completions (non-streaming and streaming).
    GET /v1/models
        List available models.
    POST /v1/web-search
        Proxy to JoyCode web search.
    POST /v1/rerank
        Proxy to JoyCode rerank.
    GET /health
        Health / readiness check.
    """

    router = APIRouter()

    # ---- POST /v1/chat/completions -----------------------------------------

    @router.post("/v1/chat/completions")
    async def chat_completions(request: Request) -> Any:
        api_key = request.headers.get("x-api-key", "")
        client = cred_router.get_client(api_key or None)
        account_default_model = cred_router.get_default_model(api_key or None)

        try:
            req_body = await request.json()
        except Exception:
            return _error_response("invalid JSON", 400)

        model: str = req_body.get("model") or account_default_model or DEFAULT_MODEL
        jc_body = translate_request(req_body)
        if not jc_body.get("model") or jc_body["model"] == DEFAULT_MODEL:
            if account_default_model:
                jc_body["model"] = account_default_model

        if jc_body.get("stream"):
            return _stream_chat(client, jc_body, model)

        # Non-streaming path
        try:
            jc_resp = client.post(CHAT_ENDPOINT, jc_body)
        except Exception as exc:
            return _error_response(str(exc))

        return JSONResponse(content=translate_response(jc_resp, model))

    # ---- GET /v1/models ----------------------------------------------------

    @router.get("/v1/models")
    async def list_models(request: Request) -> Any:
        api_key = request.headers.get("x-api-key", "")
        client = cred_router.get_client(api_key or None)
        try:
            jc_models = client.list_models()
        except Exception as exc:
            return _error_response(str(exc))
        return JSONResponse(content=translate_models(jc_models))

    # ---- POST /v1/web-search -----------------------------------------------

    @router.post("/v1/web-search")
    async def web_search(request: Request) -> Any:
        api_key = request.headers.get("x-api-key", "")
        client = cred_router.get_client(api_key or None)
        try:
            body = await request.json()
        except Exception:
            return _error_response("invalid JSON", 400)

        query = body.get("query", "")
        if not query:
            return _error_response("query is required", 400)

        try:
            results = client.web_search(query)
        except Exception as exc:
            return _error_response(str(exc))

        return JSONResponse(content={"search_result": results})

    # ---- POST /v1/rerank ---------------------------------------------------

    @router.post("/v1/rerank")
    async def rerank(request: Request) -> Any:
        api_key = request.headers.get("x-api-key", "")
        client = cred_router.get_client(api_key or None)
        try:
            body = await request.json()
        except Exception:
            return _error_response("invalid JSON", 400)

        query = body.get("query", "")
        documents = body.get("documents")
        if not query or not documents:
            return _error_response("query and documents are required", 400)

        top_n = body.get("top_n", 5)

        try:
            result = client.rerank(query, documents, top_n)
        except Exception as exc:
            return _error_response(str(exc))

        return JSONResponse(content=result)

    # ---- GET /health -------------------------------------------------------

    @router.get("/health")
    async def health() -> Any:
        return JSONResponse(
            content={
                "status": "ok",
                "service": "joycode-openai-proxy",
                "accounts": len(cred_router.list_accounts()),
                "endpoints": [
                    "/v1/chat/completions",
                    "/v1/models",
                    "/v1/web-search",
                    "/v1/rerank",
                ],
            }
        )

    return router
