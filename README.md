# JoyCode Proxy

JoyCode API Proxy — 将 JoyCode 内部 API 转换为 OpenAI / Anthropic 兼容格式，让 Claude Code、Codex 等 AI 编程工具可以直接使用。

## 功能

- **OpenAI 兼容** — `/v1/chat/completions` 标准 Chat Completions API，支持流式输出
- **Anthropic 兼容** — `/v1/messages` Messages API，支持 tool use、thinking blocks、SSE 流式
- **多模型支持** — JoyAI-Code、GLM-5.1、Kimi-K2.6、MiniMax-M2.7、Doubao-Seed-2.0-pro 等
- **Tool Use** — 完整支持 Anthropic tool_use / tool_result 协议转换，Claude Code 可正常调用工具
- **Thinking 支持** — 自动将模型的 reasoning_content 转换为 Anthropic thinking blocks
- **凭据自动检测** — 从 macOS JoyCode 客户端数据库自动读取 ptKey/userId
- **macOS 服务** — 支持以 launchd 服务运行，开机自启、崩溃自动重启

## 快速开始

### 安装依赖

```bash
pip install -e .
```

需要 Python 3.9+。

### 启动代理

```bash
python -m joycode_proxy.cli serve
```

代理默认监听 `0.0.0.0:34891`。凭据会自动从 JoyCode 客户端检测。

### 配置 Claude Code

```bash
export ANTHROPIC_BASE_URL=http://localhost:34891
export ANTHROPIC_API_KEY=joycode
export ANTHROPIC_MODEL=GLM-5.1

claude
```

### 使用 Docker

```bash
docker build -f Dockerfile.python -t joycode-proxy .
docker run -p 34891:34891 joycode-proxy
```

## CLI 命令

| 命令 | 说明 |
|------|------|
| `serve` | 启动代理服务器 |
| `chat "消息"` | 直接对话 |
| `models` | 列出可用模型 |
| `whoami` | 查看当前用户信息 |
| `version` | 显示版本号 |
| `config` | 显示配置信息 |
| `check` | 检查代理是否运行 |
| `search "查询"` | 网页搜索 |
| `service install` | 安装为 macOS 服务 |
| `service status` | 查看服务状态 |
| `service uninstall` | 卸载服务 |

全局选项：

```bash
-k, --ptkey TEXT          指定 ptKey（默认自动检测）
-u, --userid TEXT         指定 userId（默认自动检测）
--skip-validation         跳过凭据验证
-v, --verbose             开启调试日志
```

## API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/v1/chat/completions` | OpenAI Chat Completions |
| POST | `/v1/messages` | Anthropic Messages |
| POST | `/v1/web-search` | 网页搜索 |
| POST | `/v1/rerank` | 文档重排序 |
| GET | `/v1/models` | 模型列表 |
| GET | `/health` | 健康检查 |

## 项目结构

```
joycode_proxy/
  __init__.py
  cli.py              # Click CLI 入口
  ui.py               # Rich 终端 UI
  server.py           # FastAPI 应用组装
  client.py           # JoyCode HTTP 客户端
  auth.py             # 凭据加载与验证
  anthropic_handler.py # Anthropic 协议转换
  openai_handler.py   # OpenAI 协议处理
tests/
  test_anthropic.py   # Anthropic 转换测试
  test_auth.py        # 认证测试
```

## 许可证

MIT
