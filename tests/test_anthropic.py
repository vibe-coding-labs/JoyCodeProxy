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
