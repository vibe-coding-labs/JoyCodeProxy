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
