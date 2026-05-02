# JoyCodeProxy

把 JoyCode（京东内部 AI 编程助手）的 API 转换成 Anthropic / OpenAI 兼容格式，这样 Claude Code、Cursor 等 AI 编程工具就能直接用 JoyCode 的模型了。

**解决的问题：** Claude Code 只能连 Anthropic API 或兼容接口。JoyCode 提供了 JoyAI-Code、GLM-5.1 等模型的 API，但协议不兼容。JoyCodeProxy 在中间做协议翻译，让 Claude Code 直接调用这些模型。

## 架构

```
Claude Code / Cursor  →  JoyCodeProxy  →  JoyCode API
                         (协议翻译)
```

JoyCodeProxy 暴露 Anthropic Messages API（`/v1/messages`）和 OpenAI Chat API（`/v1/chat/completions`），收到请求后翻译成 JoyCode 格式转发，再把响应翻译回来。

## 功能

- **协议翻译** — Anthropic Messages API 和 OpenAI Chat Completions API 双协议支持
- **Tool Use** — 完整翻译 Anthropic tool_use/tool_result，Claude Code 可以正常调用工具
- **流式输出** — SSE 流式，实时返回模型输出
- **多模型** — JoyAI-Code、GLM-5.1、GLM-4.7、Kimi-K2.6、MiniMax-M2.7、Doubao-Seed-2.0-pro
- **多账号管理** — Dashboard Web UI 管理多个 JoyCode 账号，JD 扫码登录
- **自动上下文截断** — 对话超出模型上下文窗口时自动截断旧消息并重试
- **请求统计** — 记录每次请求的模型、延迟、状态码，Dashboard 可查看

## 快速开始

### 构建

需要 Go 1.22+ 和 Node.js 18+。

```bash
# 构建前端
cd web && npm install && npm run build && cd ..

# 构建后端（把前端嵌入二进制）
go build -o joycode_proxy_bin ./cmd/JoyCodeProxy/
```

或者用 Docker：

```bash
docker build -t joycode-proxy .
docker run -p 34891:34891 joycode-proxy
```

### 启动

```bash
./joycode_proxy_bin serve
```

默认监听 `0.0.0.0:34891`。首次启动会自动从本地 JoyCode 客户端读取凭据（macOS）。

### 配置 Claude Code

```bash
export ANTHROPIC_BASE_URL=http://localhost:34891
export ANTHROPIC_API_KEY=joycode
export ANTHROPIC_MODEL=GLM-5.1

claude
```

### 多账号

打开 `http://localhost:34891` 进入 Dashboard，用 JD App 扫码添加多个账号。Dashboard 会为每个账号生成独立的 API Key 和 Claude Code 启动命令。

## API 端点

| 路径 | 说明 |
|------|------|
| `POST /v1/messages` | Anthropic Messages API（Claude Code 用这个） |
| `POST /v1/chat/completions` | OpenAI Chat Completions API |
| `POST /v1/web-search` | 网页搜索 |
| `GET /v1/models` | 可用模型列表 |
| `GET /health` | 健康检查 |
| `GET /` | Dashboard Web UI |

## 项目结构

```
cmd/JoyCodeProxy/    程序入口，HTTP 服务器组装
pkg/anthropic/       Anthropic 协议翻译（请求/响应/SSE 流）
pkg/openai/          OpenAI 协议翻译
pkg/joycode/         JoyCode API 客户端
pkg/auth/            凭据读取、JD 扫码登录
pkg/store/           SQLite 存储（账号、请求日志）
pkg/dashboard/       Dashboard API
web/                 前端（React + Ant Design）
```

## 许可证

MIT
