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
