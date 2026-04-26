# CLI UI Enhancement Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 使用 Rich 库重写 CLI 界面，添加炫酷的彩色输出（Banner、Table、Panel、Spinner），并扩展子命令（version、config、check、search）。

**Architecture:** 用户输入 → Click 解析子命令 → Rich Console 渲染彩色输出（Panel/Table/Tree/Spinner）→ Client 调用 API。Rich Console 作为全局单例贯穿所有命令，替代 click.echo。

**Tech Stack:** Python 3.9+, Click 8.1, Rich 13.7+

**Risks:**
- Rich console 与 uvicorn 日志冲突 → 缓解：serve 命令中 Rich 输出仅在启动前使用，不干扰 uvicorn 日志
- Rich 表格在窄终端下可能折行 → 缓解：使用 `max_width` 和 `no_wrap` 控制

---

### Task 1: 创建 Rich UI 基础框架 — Console、Banner、通用输出工具

**Depends on:** None
**Files:**
- Create: `joycode_proxy/ui.py`（Rich UI 工具模块）
- Modify: `pyproject.toml:6-11`（添加 rich 依赖）

- [ ] **Step 1: 修改 pyproject.toml — 添加 rich 依赖**

文件: `pyproject.toml:6-11`（替换 dependencies 块）

```python
dependencies = [
    "fastapi>=0.115",
    "uvicorn>=0.34",
    "httpx>=0.28",
    "click>=8.2",
    "sse-starlette>=2.2",
    "rich>=13.7",
]
```

- [ ] **Step 2: 创建 joycode_proxy/ui.py — Rich UI 工具模块**

提供全局 Console、Banner 渲染、Table/Panel 工具函数，供所有 CLI 命令使用。

```python
from rich.console import Console
from rich.panel import Panel
from rich.table import Table
from rich.text import Text
from rich.tree import Tree
from rich import box

console = Console()

BANNER = r"""
[bold cyan]
     __  ____  __  __  ___  __    ____
    / / / / /_/ / / / / / / /   /  _/
   / /_/ / __/ / /_/ / / / /   / /
  / __  / /_/ / __  / / / /___/ /___
 /_/ /_/\__/_/_/ /_/ /_/ /_____/_____/
[/bold cyan]
[dim]  JoyCode API Proxy — OpenAI & Anthropic Compatible[/dim]
"""

VERSION = "0.2.0"


def print_banner():
    console.print(BANNER)
    console.print()


def print_error(message: str):
    console.print(f"[bold red]Error:[/bold red] {message}")


def print_success(message: str):
    console.print(f"[bold green]✓[/bold green] {message}")


def print_warning(message: str):
    console.print(f"[bold yellow]![/bold yellow] {message}")


def print_info(message: str):
    console.print(f"[bold blue]i[/bold blue] {message}")


def print_kv_table(title: str, data: dict, style: str = "cyan"):
    table = Table(title=title, box=box.ROUNDED, show_header=False)
    table.add_column("Key", style="bold")
    table.add_column("Value")
    for k, v in data.items():
        table.add_row(str(k), str(v))
    console.print(table)


def print_model_table(models: list, default_model: str):
    table = Table(
        title="Available Models",
        box=box.ROUNDED,
        show_lines=True,
    )
    table.add_column("Model", style="bold cyan")
    table.add_column("Label")
    table.add_column("Context", justify="right")
    table.add_column("Max Output", justify="right")
    table.add_column("Default", justify="center")

    for m in models:
        api_model = m.get("chatApiModel", "")
        label = m.get("label", "")
        ctx = m.get("maxTotalTokens", 0)
        out = m.get("respMaxTokens", 0)
        is_default = "[bold green]★[/bold green]" if api_model == default_model else ""
        model_style = "bold green" if api_model == default_model else ""
        table.add_row(
            f"[{model_style}]{api_model}[/{model_style}]" if model_style else api_model,
            label,
            f"{ctx:,}" if ctx else "N/A",
            f"{out:,}" if out else "N/A",
            is_default,
        )

    console.print(table)


def print_endpoint_tree(host: str, port: int):
    tree = Tree("[bold]Endpoints[/bold]")
    tree.add("[cyan]POST[/cyan] /v1/chat/completions  [dim]Chat (OpenAI format)[/dim]")
    tree.add("[cyan]POST[/cyan] /v1/messages          [dim]Chat (Anthropic/Claude Code)[/dim]")
    tree.add("[cyan]POST[/cyan] /v1/web-search        [dim]Web Search[/dim]")
    tree.add("[cyan]POST[/cyan] /v1/rerank            [dim]Rerank documents[/dim]")
    tree.add("[cyan]GET [/cyan] /v1/models            [dim]Model list[/dim]")
    tree.add("[cyan]GET [/cyan] /health               [dim]Health check[/dim]")
    console.print(tree)
    console.print()
    setup_panel = Panel(
        f"[bold]export ANTHROPIC_BASE_URL=http://{host}:{port}[/bold]\n"
        f"[bold]export ANTHROPIC_API_KEY=joycode[/bold]",
        title="[bold]Claude Code Setup[/bold]",
        border_style="green",
    )
    console.print(setup_panel)
```

- [ ] **Step 3: 验证 Rich UI 模块**

Run: `python3 -c "
from joycode_proxy.ui import console, print_banner, print_kv_table, print_model_table
print_banner()
print_kv_table('Test', {'Key1': 'Value1', 'Key2': 'Value2'})
print('Rich UI module OK!')
"`
Expected:
  - Exit code: 0
  - Output contains: "JoyCode" and "Rich UI module OK!"

- [ ] **Step 4: 提交**
Run: `git add joycode_proxy/ui.py pyproject.toml && git commit -m "feat(ui): add Rich-based UI module with banner, tables, and panels"`

---

### Task 2: 重写 CLI 命令 — 使用 Rich 替代 click.echo，添加炫酷输出

**Depends on:** Task 1
**Files:**
- Modify: `joycode_proxy/cli.py`（整体重写所有命令的输出）

- [ ] **Step 1: 重写 cli.py — 导入 Rich UI，替换所有 click.echo 为 Rich 输出**

文件: `joycode_proxy/cli.py`（替换整个文件）

保留所有现有功能和命令逻辑，仅替换输出层：click.echo → Rich console.print，添加 Spinner、Panel、Table 等。

```python
import logging
import os
import subprocess
import sys
from pathlib import Path

import click
from rich.console import Console

from joycode_proxy.auth import Credentials, load_from_system
from joycode_proxy.ui import (
    VERSION,
    console,
    print_banner,
    print_endpoint_tree,
    print_error,
    print_info,
    print_kv_table,
    print_model_table,
    print_success,
    print_warning,
)

log = logging.getLogger("joycode-proxy")


def _resolve_client(ptkey: str, userid: str, skip_validation: bool = False):
    from joycode_proxy.client import Client
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
    from rich.status import Status
    with Status("[bold cyan]Validating credentials...", console=console):
        client.validate()
    print_success("Credentials validated")
    return client


@click.group()
@click.option("-k", "--ptkey", default="", help="JoyCode ptKey (auto-detected if empty)")
@click.option("-u", "--userid", default="", help="JoyCode userID (auto-detected if empty)")
@click.option("--skip-validation", is_flag=True, help="Skip credential validation")
@click.option("-v", "--verbose", is_flag=True, help="Enable debug logging")
@click.pass_context
def cli(ctx, ptkey: str, userid: str, skip_validation: bool, verbose: bool):
    ctx.ensure_object(dict)
    ctx.obj["ptkey"] = ptkey
    ctx.obj["userid"] = userid
    ctx.obj["skip_validation"] = skip_validation
    ctx.obj["verbose"] = verbose
    level = logging.DEBUG if verbose else logging.INFO
    logging.basicConfig(level=level, format="%(asctime)s %(levelname)s %(name)s: %(message)s")


@cli.command()
@click.option("-H", "--host", default="0.0.0.0", help="Bind host")
@click.option("-p", "--port", default=34891, help="Bind port")
@click.pass_context
def serve(ctx, host: str, port: int):
    import uvicorn
    print_banner()
    client = _resolve_client(ctx.obj["ptkey"], ctx.obj["userid"], ctx.obj["skip_validation"])
    from joycode_proxy.server import create_app
    app = create_app(client)
    print_endpoint_tree(host, port)
    console.print()
    log_level = "debug" if ctx.obj.get("verbose") else "info"
    uvicorn.run(app, host=host, port=port, log_level=log_level)


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
    console.print(Panel(f"[dim]{message}[/dim]", title=f"[bold cyan]{model}[/bold cyan]", border_style="cyan"))
    if stream:
        body["stream"] = True
        resp = client.post_stream("/api/saas/openai/v1/chat/completions", body)
        try:
            for line in resp.iter_lines():
                if line:
                    decoded = line.decode("utf-8", errors="replace") if isinstance(line, bytes) else line
                    if decoded.startswith("data: "):
                        import json
                        payload = decoded[6:]
                        if payload == "[DONE]":
                            break
                        try:
                            chunk = json.loads(payload)
                            delta = chunk.get("choices", [{}])[0].get("delta", {})
                            text = delta.get("content", "")
                            if text:
                                console.print(text, end="")
                        except Exception:
                            pass
            console.print()
        finally:
            resp.close()
        return
    from rich.status import Status
    with Status("[bold cyan]Thinking...", console=console):
        resp = client.post("/api/saas/openai/v1/chat/completions", body)
    choices = resp.get("choices", [])
    if choices:
        content = choices[0].get("message", {}).get("content", "")
        console.print()
        console.print(content)
        console.print()


@cli.command()
@click.pass_context
def models(ctx):
    from joycode_proxy.client import DEFAULT_MODEL
    client = _resolve_client(ctx.obj["ptkey"], ctx.obj["userid"], ctx.obj["skip_validation"])
    from rich.status import Status
    with Status("[bold cyan]Fetching models...", console=console):
        model_list = client.list_models()
    print_model_table(model_list, DEFAULT_MODEL)


@cli.command()
@click.pass_context
def whoami(ctx):
    client = _resolve_client(ctx.obj["ptkey"], ctx.obj["userid"], ctx.obj["skip_validation"])
    from rich.status import Status
    with Status("[bold cyan]Fetching user info...", console=console):
        resp = client.user_info()
    data = resp.get("data", {})
    status = "[bold green]Active[/bold green]" if resp.get("code") == 0 else "[bold red]Invalid[/bold red]"
    print_kv_table("User Info", {
        "Name": data.get("realName", "N/A"),
        "ID": data.get("userId", "N/A"),
        "Organization": data.get("orgName", "N/A"),
        "Tenant": data.get("tenant", "N/A"),
        "Status": status,
    })


@cli.command()
def version():
    print_banner()
    print_kv_table("Version Info", {
        "JoyCode Proxy": VERSION,
        "Python": sys.version.split()[0],
    })


@cli.command()
@click.pass_context
def config(ctx):
    print_banner()
    print_kv_table("Configuration", {
        "API Base URL": "https://joycode-api.jd.com",
        "Default Model": "JoyAI-Code",
        "Default Port": "34891",
        "Verbose": str(ctx.obj.get("verbose", False)),
        "Skip Validation": str(ctx.obj.get("skip_validation", False)),
        "Config Dir": str(Path.home() / ".joycode-proxy"),
        "Log Dir": str(Path.home() / ".joycode-proxy" / "logs"),
    })


@cli.command()
@click.option("-H", "--host", default="localhost", help="Proxy host")
@click.option("-p", "--port", default=34891, help="Proxy port")
def check(host: str, port: int):
    import httpx
    from rich.status import Status
    url = f"http://{host}:{port}/health"
    with Status(f"[bold cyan]Checking {url}...", console=console):
        try:
            resp = httpx.get(url, timeout=5.0)
            if resp.status_code == 200:
                print_success(f"Proxy is running at {host}:{port}")
                data = resp.json()
                if "endpoints" in data:
                    for ep in data["endpoints"]:
                        console.print(f"  [green]•[/green] {ep}")
            else:
                print_error(f"Proxy returned status {resp.status_code}")
        except httpx.ConnectError:
            print_error(f"Cannot connect to proxy at {host}:{port}")
        except Exception as exc:
            print_error(str(exc))


@cli.command()
@click.argument("query")
@click.option("-n", "--max-results", default=5, help="Max results")
@click.pass_context
def search(ctx, query: str, max_results: int):
    client = _resolve_client(ctx.obj["ptkey"], ctx.obj["userid"], ctx.obj["skip_validation"])
    from rich.status import Status
    from rich.table import Table
    from rich import box
    with Status(f'[bold cyan]Searching: "{query}"...', console=console):
        results = client.web_search(query)
    if not results:
        print_warning("No results found.")
        return
    table = Table(title=f"Search Results: {query}", box=box.ROUNDED, show_lines=True)
    table.add_column("#", style="bold", width=3)
    table.add_column("Title", style="cyan")
    table.add_column("URL", style="blue")
    table.add_column("Snippet", max_width=60)
    for i, r in enumerate(results[:max_results], 1):
        title = r.get("title", "N/A")
        url = r.get("url", r.get("link", "N/A"))
        snippet = r.get("snippet", r.get("content", "N/A"))
        if isinstance(snippet, str) and len(snippet) > 120:
            snippet = snippet[:117] + "..."
        table.add_row(str(i), title, url, snippet)
    console.print(table)


@cli.group()
def service():
    pass


@service.command("install")
@click.option("-p", "--port", default=34891, help="Bind port")
def service_install(port: int):
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
    print_success("Service installed and started")
    print_kv_table("Service", {
        "Label": "com.joycode.proxy",
        "Plist": str(plist_path),
        "Port": str(port),
        "Logs": str(log_dir) + "/",
    })


@service.command("uninstall")
def service_uninstall():
    home = Path.home()
    plist_path = home / "Library" / "LaunchAgents" / "com.joycode.proxy.plist"
    subprocess.run(["launchctl", "unload", str(plist_path)], capture_output=True)
    if plist_path.exists():
        plist_path.unlink()
        print_success("Service stopped and removed")
    else:
        print_warning("Service not installed (plist not found)")


@service.command("status")
def service_status():
    home = Path.home()
    plist_path = home / "Library" / "LaunchAgents" / "com.joycode.proxy.plist"
    if not plist_path.exists():
        print_warning("Service not installed")
        return
    result = subprocess.run(["launchctl", "list"], capture_output=True, text=True)
    found = False
    for line in result.stdout.splitlines():
        if "com.joycode.proxy" in line:
            print_success(f"Service is running")
            console.print(f"  [dim]{line}[/dim]")
            found = True
            break
    if not found:
        print_warning("Service installed but not running")
    console.print(f"\n  Logs: [cyan]{home / '.joycode-proxy' / 'logs'}/[/cyan]")


if __name__ == "__main__":
    cli()
```

- [ ] **Step 2: 验证 CLI 命令列表**

Run: `python3 -m joycode_proxy.cli --help`
Expected:
  - Exit code: 0
  - Output contains: "chat", "check", "config", "models", "search", "serve", "service", "version", "whoami"

- [ ] **Step 3: 验证 version 命令**

Run: `python3 -m joycode_proxy.cli version`
Expected:
  - Exit code: 0
  - Output contains: "JoyCode" and "Version Info"

- [ ] **Step 4: 验证 check 命令**

Run: `python3 -m joycode_proxy.cli check --port 34891`
Expected:
  - Exit code: 0
  - Output contains: "Proxy is running" (if proxy is up) OR "Cannot connect" (if proxy is down)

- [ ] **Step 5: 提交**
Run: `git add joycode_proxy/cli.py && git commit -m "feat(cli): rewrite CLI with Rich UI — banner, tables, spinners, and 4 new subcommands"`
