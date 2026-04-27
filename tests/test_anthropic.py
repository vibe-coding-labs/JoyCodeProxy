from joycode_proxy.anthropic_handler import (
    resolve_model,
    _translate_request,
    _translate_response,
)


def test_resolve_model():
    assert resolve_model("GLM-5.1") == "GLM-5.1"
    assert resolve_model("claude-sonnet-4-20250514") == "JoyAI-Code"
    assert resolve_model("unknown-model") == "JoyAI-Code"


def test_translate_request_basic():
    req = {
        "model": "claude-sonnet-4",
        "max_tokens": 1024,
        "messages": [{"role": "user", "content": "Hello"}],
    }
    openai_kwargs, tool_name_mapping, requested_model = _translate_request(req)
    assert openai_kwargs["model"] == "JoyAI-Code"
    assert requested_model == "claude-sonnet-4"
    assert len(openai_kwargs["messages"]) == 1
    assert openai_kwargs["messages"][0]["role"] == "user"


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
    openai_kwargs, tool_name_mapping, requested_model = _translate_request(req)
    roles = [m["role"] for m in openai_kwargs["messages"]]
    assert "tool" in roles
    tool_msg = [m for m in openai_kwargs["messages"] if m["role"] == "tool"][0]
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
    openai_kwargs, tool_name_mapping, requested_model = _translate_request(req)
    assistant_msgs = [m for m in openai_kwargs["messages"] if m["role"] == "assistant"]
    assert len(assistant_msgs) == 1
    assert "tool_calls" in assistant_msgs[0]
    tc = assistant_msgs[0]["tool_calls"][0]
    assert tc["function"]["name"] == "Read"
    assert tc["id"] == "toolu_abc"


def test_translate_response_with_text():
    jc_resp = {
        "id": "chatcmpl-123",
        "choices": [{"message": {"role": "assistant", "content": "Hi!"}, "finish_reason": "stop", "index": 0}],
        "usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
    }
    resp = _translate_response(jc_resp, "claude-sonnet-4", {})
    assert resp["type"] == "message"
    assert resp["role"] == "assistant"
    assert resp["stop_reason"] == "end_turn"
    assert resp["content"][0]["type"] == "text"
    assert resp["content"][0]["text"] == "Hi!"
    assert resp["usage"]["input_tokens"] == 10
    assert resp["model"] == "claude-sonnet-4"
    assert resp["stop_sequence"] is None


def test_translate_response_with_tool_calls():
    jc_resp = {
        "id": "chatcmpl-456",
        "choices": [{
            "message": {
                "role": "assistant",
                "content": None,
                "tool_calls": [{
                    "id": "call_123",
                    "type": "function",
                    "function": {"name": "Read", "arguments": '{"path": "/etc/hosts"}'},
                }],
            },
            "finish_reason": "tool_calls",
            "index": 0,
        }],
        "usage": {"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120},
    }
    resp = _translate_response(jc_resp, "GLM-5.1", {})
    assert resp["stop_reason"] == "tool_use"
    assert resp["content"][0]["type"] == "tool_use"
    assert resp["content"][0]["name"] == "Read"
    assert resp["content"][0]["input"] == {"path": "/etc/hosts"}


def test_translate_response_stop_reason_mapping():
    """Verify all stop reasons are correctly mapped."""
    for openai_reason, anthropic_reason in [
        ("stop", "end_turn"),
        ("tool_calls", "tool_use"),
        ("length", "max_tokens"),
    ]:
        jc_resp = {
            "id": "chatcmpl-test",
            "choices": [{"message": {"role": "assistant", "content": "ok"}, "finish_reason": openai_reason, "index": 0}],
            "usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
        }
        resp = _translate_response(jc_resp, "claude-sonnet-4", {})
        assert resp["stop_reason"] == anthropic_reason, f"Expected {anthropic_reason} for {openai_reason}"
