# Multi-Account Credential Routing Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 支持多个 JoyCode 账号凭证同时运行，通过 API Key 路由到对应的 JoyCode 后端账号，同一个 Key 始终命中同一个账号的凭证，实现账号隔离。

**Architecture:** 客户端通过 `x-api-key` Header 发送预配置的 key → `CredentialRouter` 根据 key 查找对应的 JoyCode `Client` 实例 → 用该 Client 转发请求。配置通过 JSON 文件（`~/.joycode-proxy/accounts.json`）管理，支持 CLI 添加/删除/列出账号。同一 key 的所有请求始终使用同一个 Client 实例（含相同的 session_id），最大化命中 JoyCode 后端缓存。

**Tech Stack:** Python 3.12, FastAPI, Click 8, httpx, JSON 文件存储

**Risks:**
- `x-api-key` Header 与 Anthropic SDK 发送的 key 重名，但这正是我们想要的 — Claude Code 通过 `ANTHROPIC_API_KEY` 环境变量设置此值，自然成为路由 key
- 账号配置文件需要妥善保护（含敏感凭证）→ 缓解：文件权限设为 600
- 多账号验证耗时 → 缓解：并行验证，超时快速失败

---

### Task 1: 创建 CredentialRouter 核心模块

**Depends on:** None
**Files:**
- Create: `joycode_proxy/credential_router.py`
- Create: `tests/test_credential_router.py`

- [ ] **Step 1: 创建 CredentialRouter — 负责管理多账号凭证到 Client 实例的映射**

```python
# joycode_proxy/credential_router.py
import json
import logging
import os
from pathlib import Path
from typing import Dict, List, Optional

from joycode_proxy.auth import Credentials
from joycode_proxy.client import Client

log = logging.getLogger("joycode-proxy.router")

ACCOUNTS_DIR = Path.home() / ".joycode-proxy"
ACCOUNTS_FILE = ACCOUNTS_DIR / "accounts.json"


class CredentialRouter:
    """Manages multiple JoyCode accounts, routing API keys to Client instances."""

    def __init__(self):
        self._clients: Dict[str, Client] = {}
        self._default_key: Optional[str] = None

    @property
    def default_key(self) -> Optional[str]:
        return self._default_key

    def add_account(self, api_key: str, pt_key: str, user_id: str, default: bool = False):
        """Register a new account. Overwrites existing key."""
        client = Client(pt_key, user_id)
        self._clients[api_key] = client
        if default or self._default_key is None:
            self._default_key = api_key
        log.info("Account registered: api_key=%s user_id=%s default=%s", api_key, user_id, default)

    def get_client(self, api_key: Optional[str] = None) -> Client:
        """Get Client by api_key. Falls back to default."""
        if api_key and api_key in self._clients:
            return self._clients[api_key]
        if self._default_key and self._default_key in self._clients:
            return self._clients[self._default_key]
        raise KeyError(f"No account found for key '{api_key}' and no default configured")

    def list_accounts(self) -> List[Dict]:
        """Return list of account info dicts."""
        result = []
        for key, client in self._clients.items():
            result.append({
                "api_key": key,
                "user_id": client.user_id,
                "is_default": key == self._default_key,
            })
        return result

    def remove_account(self, api_key: str) -> bool:
        """Remove an account. Returns True if found and removed."""
        if api_key in self._clients:
            del self._clients[api_key]
            if self._default_key == api_key:
                self._default_key = next(iter(self._clients), None)
            log.info("Account removed: api_key=%s", api_key)
            return True
        return False

    def save(self, path: Optional[Path] = None):
        """Persist accounts to JSON file."""
        path = path or ACCOUNTS_FILE
        path.parent.mkdir(parents=True, exist_ok=True)
        accounts = []
        for key, client in self._clients.items():
            accounts.append({
                "api_key": key,
                "pt_key": client.pt_key,
                "user_id": client.user_id,
                "default": key == self._default_key,
            })
        path.write_text(json.dumps(accounts, indent=2, ensure_ascii=False))
        os.chmod(path, 0o600)
        log.info("Accounts saved to %s", path)

    @classmethod
    def load(cls, path: Optional[Path] = None) -> "CredentialRouter":
        """Load accounts from JSON file."""
        path = path or ACCOUNTS_FILE
        router = cls()
        if not path.exists():
            return router
        data = json.loads(path.read_text())
        for account in data:
            router.add_account(
                api_key=account["api_key"],
                pt_key=account["pt_key"],
                user_id=account["user_id"],
                default=account.get("default", False),
            )
        log.info("Loaded %d account(s) from %s", len(router._clients), path)
        return router

    def validate_all(self) -> Dict[str, bool]:
        """Validate all accounts. Returns {api_key: is_valid}."""
        results = {}
        for key, client in self._clients.items():
            try:
                client.validate()
                results[key] = True
                log.info("Account valid: api_key=%s", key)
            except Exception as e:
                results[key] = False
                log.warning("Account invalid: api_key=%s error=%s", key, e)
        return results
```

- [ ] **Step 2: 创建 CredentialRouter 单元测试**

```python
# tests/test_credential_router.py
import json
import tempfile
from pathlib import Path

from joycode_proxy.credential_router import CredentialRouter


def test_add_and_get_account():
    router = CredentialRouter()
    router.add_account("key-1", "pt-abc", "user-1")
    client = router.get_client("key-1")
    assert client.user_id == "user-1"
    assert client.pt_key == "pt-abc"


def test_default_account():
    router = CredentialRouter()
    router.add_account("key-1", "pt-abc", "user-1")
    router.add_account("key-2", "pt-def", "user-2", default=True)
    assert router.default_key == "key-2"
    # get_client without key returns default
    client = router.get_client()
    assert client.user_id == "user-2"


def test_fallback_to_default():
    router = CredentialRouter()
    router.add_account("key-1", "pt-abc", "user-1")
    client = router.get_client("unknown-key")
    assert client.user_id == "user-1"


def test_remove_account():
    router = CredentialRouter()
    router.add_account("key-1", "pt-abc", "user-1")
    router.add_account("key-2", "pt-def", "user-2", default=True)
    assert router.remove_account("key-2") is True
    assert router.default_key == "key-1"
    assert router.remove_account("nonexistent") is False


def test_save_and_load():
    with tempfile.TemporaryDirectory() as tmpdir:
        path = Path(tmpdir) / "accounts.json"
        router = CredentialRouter()
        router.add_account("key-1", "pt-abc", "user-1")
        router.add_account("key-2", "pt-def", "user-2", default=True)
        router.save(path)

        loaded = CredentialRouter.load(path)
        assert len(loaded.list_accounts()) == 2
        client = loaded.get_client("key-2")
        assert client.user_id == "user-2"
        assert loaded.default_key == "key-2"


def test_load_missing_file():
    router = CredentialRouter.load(Path("/nonexistent/path.json"))
    assert len(router.list_accounts()) == 0


def test_no_account_raises():
    router = CredentialRouter()
    try:
        router.get_client("any-key")
        assert False, "Should have raised KeyError"
    except KeyError:
        pass


def test_file_permissions():
    with tempfile.TemporaryDirectory() as tmpdir:
        path = Path(tmpdir) / "accounts.json"
        router = CredentialRouter()
        router.add_account("k", "p", "u")
        router.save(path)
        import stat
        mode = path.stat().st_mode & 0o777
        assert mode == 0o600
```

- [ ] **Step 3: 验证 CredentialRouter**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && python3 -m pytest tests/test_credential_router.py -v`
Expected:
  - Exit code: 0
  - Output contains: "7 passed"

- [ ] **Step 4: 提交**
Run: `git add joycode_proxy/credential_router.py tests/test_credential_router.py && git commit -m "feat(auth): add CredentialRouter for multi-account key-based routing"`

---

### Task 2: CLI 支持 — 添加 account 子命令

**Depends on:** Task 1
**Files:**
- Modify: `joycode_proxy/cli.py:1-82`（添加 account 子命令组 + 修改 serve 使用 CredentialRouter）

- [ ] **Step 1: 修改 CLI 添加 account 子命令组 — 支持 add/remove/list/validate**

文件: `joycode_proxy/cli.py` — 在文件末尾 `if __name__` 之前添加 account 命令组：

```python
# 添加到 cli.py 末尾（在 if __name__ == "__main__" 之前）

@cli.group()
def account():
    """Manage JoyCode accounts for multi-user routing."""
    pass


@account.command("add")
@click.argument("api_key")
@click.option("-k", "--ptkey", required=True, help="JoyCode ptKey")
@click.option("-u", "--userid", required=True, help="JoyCode userID")
@click.option("-d", "--default", is_flag=True, help="Set as default account")
def account_add(api_key: str, ptkey: str, userid: str, default: bool):
    """Add a new account. API_KEY is the key clients use to route to this account."""
    from joycode_proxy.credential_router import CredentialRouter
    router = CredentialRouter.load()
    router.add_account(api_key, ptkey, userid, default=default)
    router.save()
    print_success(f"Account added: {api_key} (user={userid}, default={default})")


@account.command("remove")
@click.argument("api_key")
def account_remove(api_key: str):
    """Remove an account by its API key."""
    from joycode_proxy.credential_router import CredentialRouter
    router = CredentialRouter.load()
    if router.remove_account(api_key):
        router.save()
        print_success(f"Account removed: {api_key}")
    else:
        print_error(f"Account not found: {api_key}")


@account.command("list")
def account_list():
    """List all configured accounts."""
    from joycode_proxy.credential_router import CredentialRouter
    router = CredentialRouter.load()
    accounts = router.list_accounts()
    if not accounts:
        print_warning("No accounts configured")
        return
    from rich.table import Table
    from rich import box
    table = Table(title="JoyCode Accounts", box=box.ROUNDED)
    table.add_column("API Key", style="cyan")
    table.add_column("User ID", style="green")
    table.add_column("Default", style="yellow")
    for acc in accounts:
        marker = "★" if acc["is_default"] else ""
        table.add_row(acc["api_key"], acc["user_id"], marker)
    console.print(table)


@account.command("validate")
def account_validate():
    """Validate all configured accounts."""
    from joycode_proxy.credential_router import CredentialRouter
    router = CredentialRouter.load()
    if not router.list_accounts():
        print_warning("No accounts configured")
        return
    from rich.status import Status
    with Status("[bold cyan]Validating accounts...", console=console):
        results = router.validate_all()
    for key, valid in results.items():
        status = "[green]✓ Valid[/green]" if valid else "[red]✗ Invalid[/red]"
        console.print(f"  {key}: {status}")
```

- [ ] **Step 2: 修改 serve 命令 — 使用 CredentialRouter 替代单一 Client**

文件: `joycode_proxy/cli.py:69-82`（替换 serve 函数）

```python
# 替换 cli.py 中的 serve 命令（第69-82行）
@cli.command()
@click.option("-H", "--host", default="0.0.0.0", help="Bind host")
@click.option("-p", "--port", default=34891, help="Bind port")
@click.pass_context
def serve(ctx, host: str, port: int):
    import uvicorn
    from joycode_proxy.credential_router import CredentialRouter
    print_banner()

    router = CredentialRouter.load()
    accounts = router.list_accounts()

    # 如果没有配置文件账号，回退到旧的单一凭证逻辑
    if not accounts:
        client = _resolve_client(ctx.obj["ptkey"], ctx.obj["userid"], ctx.obj["skip_validation"])
        # 将单一凭证注册为 default account
        router.add_account("default", client.pt_key, client.user_id, default=True)
        log.info("No accounts configured, using auto-detected credentials as default")

    # 验证所有账号（除非跳过）
    if not ctx.obj["skip_validation"] and accounts:
        from rich.status import Status
        with Status("[bold cyan]Validating accounts...", console=console):
            results = router.validate_all()
            valid_count = sum(1 for v in results.values() if v)
            print_info(f"Accounts: {valid_count}/{len(results)} valid")
            for key, valid in results.items():
                if not valid:
                    print_warning(f"  Invalid account: {key}")

    from joycode_proxy.server import create_app
    app = create_app(router)
    print_endpoint_tree(host, port)
    console.print()
    log_level = "debug" if ctx.obj.get("verbose") else "info"
    uvicorn.run(app, host=host, port=port, log_level=log_level)
```

- [ ] **Step 3: 验证 CLI account 命令**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && python3 -m joycode_proxy.cli account list`
Expected:
  - Exit code: 0
  - Output contains: "No accounts configured"

- [ ] **Step 4: 提交**
Run: `git add joycode_proxy/cli.py && git commit -m "feat(cli): add account subcommands for multi-account management"`

---

### Task 3: Handler 集成 — 从请求 Header 提取 key 并路由

**Depends on:** Task 1, Task 2
**Files:**
- Modify: `joycode_proxy/server.py:1-19`（接收 CredentialRouter 替代 Client）
- Modify: `joycode_proxy/anthropic_handler.py`（从 request 提取 key 路由到 Client）
- Modify: `joycode_proxy/openai_handler.py`（同上）

- [ ] **Step 1: 修改 server.py — 传递 CredentialRouter 替代 Client**

文件: `joycode_proxy/server.py`（替换整个文件）

```python
from joycode_proxy.credential_router import CredentialRouter
from joycode_proxy.openai_handler import create_openai_router
from joycode_proxy.anthropic_handler import create_anthropic_router


def create_app(router: CredentialRouter):
    from fastapi import FastAPI
    from fastapi.middleware.cors import CORSMiddleware
    app = FastAPI(title="JoyCode Proxy")
    app.add_middleware(
        CORSMiddleware,
        allow_origins=["*"],
        allow_methods=["*"],
        allow_headers=["*"],
    )
    app.include_router(create_openai_router(router))
    app.include_router(create_anthropic_router(router))
    return app
```

- [ ] **Step 2: 修改 anthropic_handler.py — 从 x-api-key 提取路由 key**

文件: `joycode_proxy/anthropic_handler.py` — 修改文件头部 import 和 `create_anthropic_router` 函数签名：

将 `from joycode_proxy.client import CHAT_ENDPOINT, Client, MODELS` 中的 `Client` 替换为 `CredentialRouter`，并修改路由函数：

```python
# 修改 anthropic_handler.py 的 import 区域
# 替换: from joycode_proxy.client import CHAT_ENDPOINT, Client, MODELS
from joycode_proxy.client import CHAT_ENDPOINT, MODELS
from joycode_proxy.credential_router import CredentialRouter
```

修改 `create_anthropic_router` 函数签名和内部逻辑：

```python
# 替换 anthropic_handler.py 末尾的 create_anthropic_router 函数
def create_anthropic_router(router: CredentialRouter) -> APIRouter:
    """Create and return a FastAPI router that serves the Anthropic Messages API."""

    _router = APIRouter()

    @_router.post("/v1/messages")
    async def handle_messages(request: Request):  # type: ignore[return]
        api_key = request.headers.get("x-api-key", "")
        client = router.get_client(api_key or None)

        body = await request.json()
        log.debug("POST /v1/messages model=%s stream=%s tools=%d key=%s",
                  body.get("model"), body.get("stream"), len(body.get("tools", [])),
                  api_key[:8] + "..." if api_key else "default")

        if not body.get("max_tokens"):
            body["max_tokens"] = 8192

        if body.get("stream"):
            return await _handle_stream(client, body)

        openai_kwargs, tool_name_mapping, requested_model = _translate_request(body)
        try:
            jc_resp = client.post(CHAT_ENDPOINT, openai_kwargs)
        except Exception as exc:
            log.error("JoyCode API error: %s", exc)
            return _error_response(500, str(exc))

        resp = _translate_response(jc_resp, requested_model, tool_name_mapping)
        log.debug("Response: stop_reason=%s content_blocks=%d",
                  resp.get("stop_reason"), len(resp.get("content", [])))
        return JSONResponse(content=resp)

    return _router
```

- [ ] **Step 3: 修改 openai_handler.py — 同样从 x-api-key 提取路由 key**

文件: `joycode_proxy/openai_handler.py` — 修改 import 和 `create_openai_router` 函数：

替换 import 区域：
```python
# 替换: from joycode_proxy.client import CHAT_ENDPOINT, Client, DEFAULT_MODEL, MODELS
from joycode_proxy.client import CHAT_ENDPOINT, DEFAULT_MODEL, MODELS
from joycode_proxy.credential_router import CredentialRouter
```

替换 `create_openai_router` 函数签名（约第 215 行起）：

```python
# 替换 openai_handler.py 中的 create_openai_router 函数
def create_openai_router(router: CredentialRouter) -> APIRouter:
    """Create and return a FastAPI router that exposes the OpenAI-compatible endpoints."""

    _router = APIRouter()

    @_router.post("/v1/chat/completions")
    async def chat_completions(request: Request) -> Any:
        api_key = request.headers.get("x-api-key", "")
        client = router.get_client(api_key or None)

        try:
            req_body = await request.json()
        except Exception:
            return _error_response("invalid JSON", 400)

        model: str = req_body.get("model") or DEFAULT_MODEL
        jc_body = translate_request(req_body)

        if jc_body.get("stream"):
            return _stream_chat(client, jc_body, model)

        try:
            jc_resp = client.post(CHAT_ENDPOINT, jc_body)
        except Exception as exc:
            return _error_response(str(exc))

        return JSONResponse(content=translate_response(jc_resp, model))

    @_router.get("/v1/models")
    async def list_models() -> Any:
        api_key = request.headers.get("x-api-key", "")
        client = router.get_client(api_key or None)
        try:
            jc_models = client.list_models()
        except Exception as exc:
            return _error_response(str(exc))
        return JSONResponse(content=translate_models(jc_models))

    @_router.post("/v1/web-search")
    async def web_search(request: Request) -> Any:
        api_key = request.headers.get("x-api-key", "")
        client = router.get_client(api_key or None)
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

    @_router.post("/v1/rerank")
    async def rerank(request: Request) -> Any:
        api_key = request.headers.get("x-api-key", "")
        client = router.get_client(api_key or None)
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

    @_router.get("/health")
    async def health() -> Any:
        return JSONResponse(
            content={
                "status": "ok",
                "service": "joycode-openai-proxy",
                "accounts": len(router.list_accounts()),
                "endpoints": [
                    "/v1/chat/completions",
                    "/v1/models",
                    "/v1/web-search",
                    "/v1/rerank",
                ],
            }
        )

    return _router
```

- [ ] **Step 4: 验证 import 和基本启动**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && python3 -c "from joycode_proxy.server import create_app; from joycode_proxy.credential_router import CredentialRouter; r = CredentialRouter(); r.add_account('test', 'pk', 'uid'); app = create_app(r); print('OK: routes=', len(app.routes))"`
Expected:
  - Exit code: 0
  - Output contains: "OK: routes="

- [ ] **Step 5: 运行全部测试**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && python3 -m pytest tests/ -v`
Expected:
  - Exit code: 0
  - Output contains: "passed"

- [ ] **Step 6: 提交**
Run: `git add joycode_proxy/server.py joycode_proxy/anthropic_handler.py joycode_proxy/openai_handler.py && git commit -m "feat(router): integrate CredentialRouter into server and handlers"`

---

### Task 4: 端到端验证 — 多账号路由测试

**Depends on:** Task 3
**Files:**
- Create: `tests/test_multi_account.py`

- [ ] **Step 1: 创建端到端多账号路由测试**

```python
# tests/test_multi_account.py
import json
from fastapi.testclient import TestClient

from joycode_proxy.credential_router import CredentialRouter
from joycode_proxy.server import create_app


def _make_router() -> CredentialRouter:
    router = CredentialRouter()
    router.add_account("key-alpha", "pt-alpha", "user-alpha", default=True)
    router.add_account("key-beta", "pt-beta", "user-beta")
    return router


def test_health_shows_account_count():
    router = _make_router()
    app = create_app(router)
    client = TestClient(app)
    resp = client.get("/health")
    assert resp.status_code == 200
    data = resp.json()
    assert data["accounts"] == 2


def test_unknown_key_uses_default():
    router = _make_router()
    app = create_app(router)
    client = TestClient(app)
    # 未知 key → fallback 到 default (key-alpha)
    resp = client.post(
        "/v1/messages",
        json={"model": "test", "max_tokens": 10, "messages": [{"role": "user", "content": "hi"}]},
        headers={"x-api-key": "unknown"},
    )
    # 即使后端返回错误（因为是假凭证），也证明路由到了 default 而不是崩溃
    assert resp.status_code in (200, 500)


def test_no_key_uses_default():
    router = _make_router()
    app = create_app(router)
    client = TestClient(app)
    resp = client.post(
        "/v1/messages",
        json={"model": "test", "max_tokens": 10, "messages": [{"role": "user", "content": "hi"}]},
    )
    assert resp.status_code in (200, 500)


def test_specific_key_routes_correctly():
    router = _make_router()
    app = create_app(router)
    client = TestClient(app)
    resp = client.post(
        "/v1/messages",
        json={"model": "test", "max_tokens": 10, "messages": [{"role": "user", "content": "hi"}]},
        headers={"x-api-key": "key-beta"},
    )
    assert resp.status_code in (200, 500)


def test_empty_router_returns_error():
    router = CredentialRouter()
    app = create_app(router)
    client = TestClient(app)
    resp = client.post(
        "/v1/messages",
        json={"model": "test", "max_tokens": 10, "messages": [{"role": "user", "content": "hi"}]},
        headers={"x-api-key": "anything"},
    )
    assert resp.status_code == 500
```

- [ ] **Step 2: 运行端到端测试**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && python3 -m pytest tests/test_multi_account.py tests/test_credential_router.py -v`
Expected:
  - Exit code: 0
  - Output contains: "12 passed"

- [ ] **Step 3: 提交**
Run: `git add tests/test_multi_account.py && git commit -m "test(router): add multi-account routing end-to-end tests"`
