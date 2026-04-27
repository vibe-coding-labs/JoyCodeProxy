# Web Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`
> Steps use checkbox (`- [ ]`) syntax.

**Goal:** 为 JoyCodeProxy 开发一个 Web 管理界面（TypeScript + React + Ant Design），支持账号管理、代理状态监控、设置配置，数据存储在 `~/.joycode-proxy/proxy.db`（SQLite）。

**Architecture:** 用户浏览器 → React SPA（Ant Design）→ FastAPI Web API (`/api/*`) → SQLite 数据层 → JoyCode 后端。前端使用 Vite 构建，开发时 Vite dev server 代理 API 到 FastAPI，部署时构建产物复制到 `joycode_proxy/static/` 由 FastAPI 直接 serve。数据层从现有 JSON 文件迁移到 SQLite，保持向后兼容。

**Tech Stack:** TypeScript 5, React 19, Ant Design 5, Vite 6, Python 3.12, FastAPI, aiosqlite 0.20, SQLite 3

**Risks:**
- Task 1 创建 SQLite 层需同时支持现有 `CredentialRouter` 的接口 → 缓解：`Database` 类提供 `get_router()` 方法返回兼容的 `CredentialRouter`
- Task 3 前端项目初始化需 Node.js 24 + npm 11 → 缓解：已验证环境可用
- Task 6 构建集成需确保静态文件路径正确 → 缓解：使用 `importlib.resources` 或 `Path(__file__).parent / "static"`

---

### Task 1: SQLite 数据层 — 创建 Database 类

**Depends on:** None
**Files:**
- Create: `joycode_proxy/db.py`
- Create: `tests/test_db.py`

- [ ] **Step 1: 创建 Database 类 — 负责账号和设置的 SQLite CRUD 操作**

```python
# joycode_proxy/db.py
import json
import logging
import os
import sqlite3
from pathlib import Path
from typing import Any, Dict, List, Optional

log = logging.getLogger("joycode-proxy.db")

DATA_DIR = Path.home() / ".joycode-proxy"
DB_PATH = DATA_DIR / "proxy.db"

SCHEMA = """
CREATE TABLE IF NOT EXISTS accounts (
    api_key TEXT PRIMARY KEY,
    pt_key TEXT NOT NULL,
    user_id TEXT NOT NULL,
    is_default INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS request_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    api_key TEXT,
    model TEXT,
    endpoint TEXT,
    stream INTEGER,
    status_code INTEGER,
    latency_ms INTEGER,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
"""


class Database:
    def __init__(self, db_path: Optional[Path] = None):
        self.db_path = db_path or DB_PATH
        self.db_path.parent.mkdir(parents=True, exist_ok=True)
        self._conn: Optional[sqlite3.Connection] = None

    def _get_conn(self) -> sqlite3.Connection:
        if self._conn is None:
            self._conn = sqlite3.connect(str(self.db_path))
            self._conn.row_factory = sqlite3.Row
            self._conn.execute("PRAGMA journal_mode=WAL")
            self._conn.execute("PRAGMA foreign_keys=ON")
            self._conn.executescript(SCHEMA)
        return self._conn

    def close(self):
        if self._conn:
            self._conn.close()
            self._conn = None

    # -- Account CRUD --

    def add_account(self, api_key: str, pt_key: str, user_id: str, is_default: bool = False):
        conn = self._get_conn()
        if is_default:
            conn.execute("UPDATE accounts SET is_default = 0")
        conn.execute(
            "INSERT OR REPLACE INTO accounts (api_key, pt_key, user_id, is_default, updated_at) "
            "VALUES (?, ?, ?, ?, datetime('now'))",
            (api_key, pt_key, user_id, 1 if is_default else 0),
        )
        conn.commit()
        log.info("Account saved: api_key=%s user_id=%s", api_key, user_id)

    def remove_account(self, api_key: str) -> bool:
        conn = self._get_conn()
        cursor = conn.execute("DELETE FROM accounts WHERE api_key = ?", (api_key,))
        conn.commit()
        return cursor.rowcount > 0

    def list_accounts(self) -> List[Dict[str, Any]]:
        conn = self._get_conn()
        rows = conn.execute(
            "SELECT api_key, user_id, is_default, created_at FROM accounts ORDER BY created_at"
        ).fetchall()
        return [
            {
                "api_key": r["api_key"],
                "user_id": r["user_id"],
                "is_default": bool(r["is_default"]),
                "created_at": r["created_at"],
            }
            for r in rows
        ]

    def get_account(self, api_key: str) -> Optional[Dict[str, Any]]:
        conn = self._get_conn()
        row = conn.execute(
            "SELECT api_key, pt_key, user_id, is_default FROM accounts WHERE api_key = ?",
            (api_key,),
        ).fetchone()
        if not row:
            return None
        return {
            "api_key": row["api_key"],
            "pt_key": row["pt_key"],
            "user_id": row["user_id"],
            "is_default": bool(row["is_default"]),
        }

    def get_default_account(self) -> Optional[Dict[str, Any]]:
        conn = self._get_conn()
        row = conn.execute(
            "SELECT api_key, pt_key, user_id FROM accounts WHERE is_default = 1"
        ).fetchone()
        if not row:
            row = conn.execute(
                "SELECT api_key, pt_key, user_id FROM accounts ORDER BY created_at LIMIT 1"
            ).fetchone()
        if not row:
            return None
        return {"api_key": row["api_key"], "pt_key": row["pt_key"], "user_id": row["user_id"]}

    def set_default(self, api_key: str) -> bool:
        conn = self._get_conn()
        row = conn.execute("SELECT 1 FROM accounts WHERE api_key = ?", (api_key,)).fetchone()
        if not row:
            return False
        conn.execute("UPDATE accounts SET is_default = 0")
        conn.execute("UPDATE accounts SET is_default = 1, updated_at = datetime('now') WHERE api_key = ?", (api_key,))
        conn.commit()
        return True

    def validate_account(self, api_key: str) -> bool:
        acc = self.get_account(api_key)
        if not acc:
            return False
        from joycode_proxy.client import Client
        try:
            client = Client(acc["pt_key"], acc["user_id"])
            client.validate()
            return True
        except Exception:
            return False

    # -- Settings --

    def get_setting(self, key: str, default: str = "") -> str:
        conn = self._get_conn()
        row = conn.execute("SELECT value FROM settings WHERE key = ?", (key,)).fetchone()
        return row["value"] if row else default

    def set_setting(self, key: str, value: str):
        conn = self._get_conn()
        conn.execute(
            "INSERT OR REPLACE INTO settings (key, value, updated_at) VALUES (?, ?, datetime('now'))",
            (key, value),
        )
        conn.commit()

    def get_all_settings(self) -> Dict[str, str]:
        conn = self._get_conn()
        rows = conn.execute("SELECT key, value FROM settings").fetchall()
        return {r["key"]: r["value"] for r in rows}

    # -- Request logs --

    def log_request(self, api_key: str, model: str, endpoint: str, stream: bool,
                    status_code: int, latency_ms: int):
        conn = self._get_conn()
        conn.execute(
            "INSERT INTO request_logs (api_key, model, endpoint, stream, status_code, latency_ms) "
            "VALUES (?, ?, ?, ?, ?, ?)",
            (api_key, model, endpoint, 1 if stream else 0, status_code, latency_ms),
        )
        conn.commit()

    def get_recent_logs(self, limit: int = 100) -> List[Dict[str, Any]]:
        conn = self._get_conn()
        rows = conn.execute(
            "SELECT * FROM request_logs ORDER BY id DESC LIMIT ?", (limit,)
        ).fetchall()
        return [dict(r) for r in rows]

    def get_stats(self) -> Dict[str, Any]:
        conn = self._get_conn()
        total = conn.execute("SELECT COUNT(*) as cnt FROM request_logs").fetchone()["cnt"]
        by_model = conn.execute(
            "SELECT model, COUNT(*) as cnt FROM request_logs GROUP BY model ORDER BY cnt DESC"
        ).fetchall()
        by_account = conn.execute(
            "SELECT api_key, COUNT(*) as cnt FROM request_logs GROUP BY api_key ORDER BY cnt DESC"
        ).fetchall()
        avg_latency = conn.execute(
            "SELECT AVG(latency_ms) as avg FROM request_logs WHERE latency_ms > 0"
        ).fetchone()["avg"]
        return {
            "total_requests": total,
            "by_model": [{"model": r["model"], "count": r["cnt"]} for r in by_model],
            "by_account": [{"api_key": r["api_key"], "count": r["cnt"]} for r in by_account],
            "avg_latency_ms": round(avg_latency or 0, 1),
            "accounts_count": conn.execute("SELECT COUNT(*) as cnt FROM accounts").fetchone()["cnt"],
        }

    def get_credential_router(self):
        """Build a CredentialRouter from DB accounts, for use by existing handlers."""
        from joycode_proxy.credential_router import CredentialRouter
        router = CredentialRouter()
        for acc in self.list_accounts():
            full = self.get_account(acc["api_key"])
            if full:
                router.add_account(full["api_key"], full["pt_key"], full["user_id"], default=full["is_default"])
        return router

    def migrate_from_json(self):
        """One-time migration from accounts.json to SQLite."""
        json_path = DATA_DIR / "accounts.json"
        if not json_path.exists():
            return 0
        data = json.loads(json_path.read_text())
        count = 0
        for acc in data:
            existing = self.get_account(acc["api_key"])
            if not existing:
                self.add_account(
                    acc["api_key"], acc["pt_key"], acc["user_id"],
                    is_default=acc.get("default", False),
                )
                count += 1
        if count > 0:
            log.info("Migrated %d accounts from JSON to SQLite", count)
        return count
```

- [ ] **Step 2: 创建 Database 单元测试**

```python
# tests/test_db.py
import tempfile
from pathlib import Path

from joycode_proxy.db import Database


def _make_db() -> Database:
    tmpdir = tempfile.mkdtemp()
    return Database(Path(tmpdir) / "test.db")


def test_add_and_list_accounts():
    db = _make_db()
    db.add_account("key-1", "pt-abc", "user-1")
    db.add_account("key-2", "pt-def", "user-2", is_default=True)
    accounts = db.list_accounts()
    assert len(accounts) == 2
    assert accounts[1]["is_default"] is True
    db.close()


def test_remove_account():
    db = _make_db()
    db.add_account("key-1", "pt-abc", "user-1")
    assert db.remove_account("key-1") is True
    assert db.remove_account("nonexistent") is False
    assert len(db.list_accounts()) == 0
    db.close()


def test_set_default():
    db = _make_db()
    db.add_account("key-1", "pt-abc", "user-1")
    db.add_account("key-2", "pt-def", "user-2", is_default=True)
    db.set_default("key-1")
    acc = db.get_account("key-1")
    assert acc["is_default"] is True
    db.close()


def test_get_default_account():
    db = _make_db()
    assert db.get_default_account() is None
    db.add_account("key-1", "pt-abc", "user-1")
    default = db.get_default_account()
    assert default["api_key"] == "key-1"
    db.close()


def test_settings():
    db = _make_db()
    assert db.get_setting("port") == ""
    db.set_setting("port", "34891")
    assert db.get_setting("port") == "34891"
    settings = db.get_all_settings()
    assert settings["port"] == "34891"
    db.close()


def test_request_logs_and_stats():
    db = _make_db()
    db.log_request("key-1", "GLM-5.1", "/v1/messages", True, 200, 1500)
    db.log_request("key-1", "GLM-5.1", "/v1/messages", False, 200, 800)
    stats = db.get_stats()
    assert stats["total_requests"] == 2
    assert stats["avg_latency_ms"] == 1150.0
    assert len(stats["by_model"]) == 1
    db.close()


def test_get_credential_router():
    db = _make_db()
    db.add_account("key-1", "pt-abc", "user-1", is_default=True)
    db.add_account("key-2", "pt-def", "user-2")
    router = db.get_credential_router()
    client = router.get_client("key-1")
    assert client.pt_key == "pt-abc"
    client2 = router.get_client("key-2")
    assert client2.user_id == "user-2"
    db.close()


def test_migrate_from_json():
    import json
    tmpdir = tempfile.mkdtemp()
    data_dir = Path(tmpdir)
    json_path = data_dir / "accounts.json"
    json_path.write_text(json.dumps([
        {"api_key": "k1", "pt_key": "p1", "user_id": "u1", "default": True},
    ]))
    db = Database(data_dir / "test.db")
    # Monkey-patch DATA_DIR for migration
    import joycode_proxy.db as db_mod
    orig = db_mod.DATA_DIR
    db_mod.DATA_DIR = data_dir
    try:
        count = db.migrate_from_json()
        assert count == 1
        assert len(db.list_accounts()) == 1
        # Second migration should not duplicate
        count2 = db.migrate_from_json()
        assert count2 == 0
        assert len(db.list_accounts()) == 1
    finally:
        db_mod.DATA_DIR = orig
    db.close()
```

- [ ] **Step 3: 添加 aiosqlite 依赖到 pyproject.toml**
文件: `pyproject.toml:7-14`

```toml
dependencies = [
    "fastapi>=0.115",
    "uvicorn>=0.34",
    "httpx>=0.28",
    "click>=8.2",
    "sse-starlette>=2.2",
    "rich>=13.7",
    "litellm>=1.0",
    "aiosqlite>=0.20",
]
```

- [ ] **Step 4: 验证 Database 模块**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && pip install aiosqlite>=0.20 -q && python3 -m pytest tests/test_db.py -v`
Expected:
  - Exit code: 0
  - Output contains: "9 passed"

- [ ] **Step 5: 提交**
Run: `git add joycode_proxy/db.py tests/test_db.py pyproject.toml && git commit -m "feat(db): add SQLite data layer for accounts, settings, and request logs"`

---

### Task 2: Web API — 创建 FastAPI 管理接口

**Depends on:** Task 1
**Files:**
- Create: `joycode_proxy/web_api.py`
- Modify: `joycode_proxy/server.py:1-19`（挂载 Web API + 静态文件服务）

- [ ] **Step 1: 创建 web_api.py — 提供 /api/* 管理端点**

```python
# joycode_proxy/web_api.py
import logging
import time
from pathlib import Path
from typing import Any, Dict, Optional

from fastapi import APIRouter, HTTPException, Request
from fastapi.responses import JSONResponse

from joycode_proxy.db import Database

log = logging.getLogger("joycode-proxy.web-api")


def create_web_api_router(db: Database) -> APIRouter:
    router = APIRouter(prefix="/api")

    # -- Accounts --

    @router.get("/accounts")
    async def list_accounts():
        accounts = db.list_accounts()
        # Mask pt_key in list view
        for acc in accounts:
            if "pt_key" in acc:
                acc["pt_key"] = acc["pt_key"][:6] + "***"
        return {"accounts": accounts}

    @router.post("/accounts")
    async def add_account(request: Request):
        body = await request.json()
        api_key = body.get("api_key", "").strip()
        pt_key = body.get("pt_key", "").strip()
        user_id = body.get("user_id", "").strip()
        is_default = body.get("is_default", False)
        if not api_key or not pt_key or not user_id:
            raise HTTPException(400, "api_key, pt_key, user_id are required")
        db.add_account(api_key, pt_key, user_id, is_default=is_default)
        return {"ok": True, "api_key": api_key}

    @router.delete("/accounts/{api_key}")
    async def remove_account(api_key: str):
        if not db.remove_account(api_key):
            raise HTTPException(404, f"Account '{api_key}' not found")
        return {"ok": True}

    @router.put("/accounts/{api_key}/default")
    async def set_default(api_key: str):
        if not db.set_default(api_key):
            raise HTTPException(404, f"Account '{api_key}' not found")
        return {"ok": True}

    @router.post("/accounts/{api_key}/validate")
    async def validate_account(api_key: str):
        valid = db.validate_account(api_key)
        return {"api_key": api_key, "valid": valid}

    # -- Settings --

    @router.get("/settings")
    async def get_settings():
        return {"settings": db.get_all_settings()}

    @router.put("/settings")
    async def update_settings(request: Request):
        body = await request.json()
        for key, value in body.items():
            db.set_setting(key, str(value))
        return {"ok": True}

    # -- Stats --

    @router.get("/stats")
    async def get_stats():
        return db.get_stats()

    @router.get("/stats/logs")
    async def get_logs(limit: int = 100):
        return {"logs": db.get_recent_logs(limit)}

    # -- Health --

    @router.get("/health")
    async def health():
        accounts = db.list_accounts()
        return {
            "status": "ok",
            "accounts": len(accounts),
            "version": "0.2.0",
        }

    return router
```

- [ ] **Step 2: 修改 server.py — 挂载 Web API + 静态文件 + 请求日志中间件**

```python
# joycode_proxy/server.py
import logging
import time
from pathlib import Path
from typing import Optional

from joycode_proxy.credential_router import CredentialRouter
from joycode_proxy.openai_handler import create_openai_router
from joycode_proxy.anthropic_handler import create_anthropic_router

log = logging.getLogger("joycode-proxy")


def create_app(router: CredentialRouter, db=None):
    from fastapi import FastAPI, Request
    from fastapi.middleware.cors import CORSMiddleware
    from fastapi.staticfiles import StaticFiles

    app = FastAPI(title="JoyCode Proxy")
    app.add_middleware(
        CORSMiddleware,
        allow_origins=["*"],
        allow_methods=["*"],
        allow_headers=["*"],
    )

    # Request logging middleware
    if db:
        @app.middleware("http")
        async def log_requests(request: Request, call_next):
            start = time.time()
            response = await call_next(request)
            latency = int((time.time() - start) * 1000)
            path = request.url.path
            if path.startswith("/v1/") or path.startswith("/api/"):
                api_key = request.headers.get("x-api-key", "")
                model = ""
                if request.method == "POST":
                    try:
                        body = await request.json()
                        model = body.get("model", "")
                    except Exception:
                        pass
                db.log_request(
                    api_key=api_key, model=model, endpoint=path,
                    stream=False, status_code=response.status_code,
                    latency_ms=latency,
                )
            return response

        from joycode_proxy.web_api import create_web_api_router
        app.include_router(create_web_api_router(db))

    app.include_router(create_openai_router(router))
    app.include_router(create_anthropic_router(router))

    # Serve static frontend files if they exist
    static_dir = Path(__file__).parent / "static"
    if static_dir.is_dir():
        app.mount("/", StaticFiles(directory=str(static_dir), html=True), name="static")

    return app
```

- [ ] **Step 3: 修改 cli.py serve 命令 — 使用 Database 替代 JSON CredentialRouter**
文件: `joycode_proxy/cli.py:73-101`（替换 serve 函数）

```python
# 替换 cli.py 中 serve 函数（第73-101行）
@cli.command()
@click.option("-H", "--host", default="0.0.0.0", help="Bind host")
@click.option("-p", "--port", default=34891, help="Bind port")
@click.pass_context
def serve(ctx, host: str, port: int):
    import uvicorn
    from joycode_proxy.db import Database
    print_banner()

    db = Database()
    db.migrate_from_json()

    # Build CredentialRouter from DB for the proxy handlers
    router = db.get_credential_router()

    # If no accounts in DB, fallback to auto-detect
    if not router.list_accounts():
        client = _resolve_client(ctx.obj["ptkey"], ctx.obj["userid"], ctx.obj["skip_validation"])
        db.add_account("default", client.pt_key, client.user_id, is_default=True)
        router = db.get_credential_router()
        log.info("No accounts configured, using auto-detected credentials as default")

    from joycode_proxy.server import create_app
    app = create_app(router, db=db)
    print_endpoint_tree(host, port)
    console.print()
    log_level = "debug" if ctx.obj.get("verbose") else "info"
    uvicorn.run(app, host=host, port=port, log_level=log_level)
```

- [ ] **Step 4: 验证 Web API 启动**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && python3 -c "from joycode_proxy.server import create_app; from joycode_proxy.db import Database; db = Database(); app = create_app(db.get_credential_router(), db=db); routes = [r.path for r in app.routes if hasattr(r, 'path')]; print('OK:', [r for r in routes if '/api/' in r or '/v1/' in r or r == '/health'])"`
Expected:
  - Exit code: 0
  - Output contains: "/api/accounts"

- [ ] **Step 5: 提交**
Run: `git add joycode_proxy/web_api.py joycode_proxy/server.py joycode_proxy/cli.py && git commit -m "feat(web-api): add management REST API for accounts, settings, and stats"`

---

### Task 3: 前端项目初始化 — Vite + React + Ant Design

**Depends on:** Task 2
**Files:**
- Create: `web/` 项目目录（package.json, vite.config.ts, tsconfig.json, src/main.tsx, src/App.tsx, src/layouts/MainLayout.tsx）
- Modify: `.gitignore`（添加 web/node_modules/, web/dist/）

- [ ] **Step 1: 初始化 Vite + React + TypeScript 项目**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && npm create vite@latest web -- --template react-ts 2>&1`
Expected:
  - Exit code: 0
  - Directory `web/` exists with `package.json`

- [ ] **Step 2: 安装依赖 — Ant Design + React Router**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npm install antd @ant-design/icons react-router-dom recharts 2>&1`
Expected:
  - Exit code: 0
  - `web/node_modules/antd` exists

- [ ] **Step 3: 配置 Vite 开发代理 — 将 /api/* 代理到 FastAPI 后端**
文件: `web/vite.config.ts`（替换整个文件）

```typescript
// web/vite.config.ts
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:34891',
        changeOrigin: true,
      },
      '/v1': {
        target: 'http://localhost:34891',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: '../joycode_proxy/static',
    emptyOutDir: true,
  },
});
```

- [ ] **Step 4: 创建 API 客户端模块 — 封装 /api/* 调用**

```typescript
// web/src/api.ts
export interface Account {
  api_key: string;
  user_id: string;
  is_default: boolean;
  created_at?: string;
}

export interface Stats {
  total_requests: number;
  accounts_count: number;
  avg_latency_ms: number;
  by_model: { model: string; count: number }[];
  by_account: { api_key: string; count: number }[];
}

export interface Settings {
  [key: string]: string;
}

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const resp = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  });
  if (!resp.ok) {
    const err = await resp.json().catch(() => ({ detail: resp.statusText }));
    throw new Error(err.detail || `HTTP ${resp.status}`);
  }
  return resp.json();
}

export const api = {
  listAccounts: () => request<{ accounts: Account[] }>('/api/accounts').then(r => r.accounts),
  addAccount: (data: { api_key: string; pt_key: string; user_id: string; is_default?: boolean }) =>
    request<{ ok: boolean }>('/api/accounts', { method: 'POST', body: JSON.stringify(data) }),
  removeAccount: (apiKey: string) =>
    request<{ ok: boolean }>(`/api/accounts/${encodeURIComponent(apiKey)}`, { method: 'DELETE' }),
  setDefault: (apiKey: string) =>
    request<{ ok: boolean }>(`/api/accounts/${encodeURIComponent(apiKey)}/default`, { method: 'PUT' }),
  validateAccount: (apiKey: string) =>
    request<{ valid: boolean }>(`/api/accounts/${encodeURIComponent(apiKey)}/validate`, { method: 'POST' }),
  getStats: () => request<Stats>('/api/stats'),
  getSettings: () => request<{ settings: Settings }>('/api/settings').then(r => r.settings),
  updateSettings: (data: Settings) =>
    request<{ ok: boolean }>('/api/settings', { method: 'PUT', body: JSON.stringify(data) }),
  getHealth: () => request<{ status: string; accounts: number }>('/api/health'),
};
```

- [ ] **Step 5: 创建主布局 — Ant Design ProLayout 风格侧边栏 + 路由**

```typescript
// web/src/layouts/MainLayout.tsx
import React, { useState } from 'react';
import { Layout, Menu, Typography, theme } from 'antd';
import {
  DashboardOutlined,
  TeamOutlined,
  SettingOutlined,
  ApiOutlined,
} from '@ant-design/icons';
import { useNavigate, useLocation, Outlet } from 'react-router-dom';

const { Header, Sider, Content } = Layout;
const { Text } = Typography;

const menuItems = [
  { key: '/', icon: <DashboardOutlined />, label: 'Dashboard' },
  { key: '/accounts', icon: <TeamOutlined />, label: 'Accounts' },
  { key: '/settings', icon: <SettingOutlined />, label: 'Settings' },
];

const MainLayout: React.FC = () => {
  const [collapsed, setCollapsed] = useState(false);
  const navigate = useNavigate();
  const location = useLocation();
  const { token } = theme.useToken();

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider
        collapsible
        collapsed={collapsed}
        onCollapse={setCollapsed}
        style={{ background: token.colorBgContainer }}
      >
        <div style={{
          height: 48,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          borderBottom: `1px solid ${token.colorBorderSecondary}`,
        }}>
          <ApiOutlined style={{ fontSize: 20, marginRight: collapsed ? 0 : 8 }} />
          {!collapsed && <Text strong>JoyCode Proxy</Text>}
        </div>
        <Menu
          mode="inline"
          selectedKeys={[location.pathname]}
          items={menuItems}
          onClick={({ key }) => navigate(key)}
        />
      </Sider>
      <Layout>
        <Header style={{
          padding: '0 24px',
          background: token.colorBgContainer,
          borderBottom: `1px solid ${token.colorBorderSecondary}`,
          display: 'flex',
          alignItems: 'center',
        }}>
          <Text type="secondary">JoyCode API Proxy Management</Text>
        </Header>
        <Content style={{ margin: 24 }}>
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
};

export default MainLayout;
```

- [ ] **Step 6: 创建 App.tsx — 配置路由和入口页面**

```typescript
// web/src/App.tsx
import React from 'react';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { ConfigProvider } from 'antd';
import MainLayout from './layouts/MainLayout';
import Dashboard from './pages/Dashboard';
import Accounts from './pages/Accounts';
import Settings from './pages/Settings';

const App: React.FC = () => (
  <ConfigProvider theme={{ token: { colorPrimary: '#1677ff' } }}>
    <BrowserRouter>
      <Routes>
        <Route element={<MainLayout />}>
          <Route path="/" element={<Dashboard />} />
          <Route path="/accounts" element={<Accounts />} />
          <Route path="/settings" element={<Settings />} />
        </Route>
      </Routes>
    </BrowserRouter>
  </ConfigProvider>
);

export default App;
```

- [ ] **Step 7: 更新 main.tsx 入口**

```typescript
// web/src/main.tsx
import React from 'react';
import ReactDOM from 'react-dom/client';
import App from './App';
import './index.css';

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
```

- [ ] **Step 8: 更新 .gitignore — 添加前端构建产物**
文件: `.gitignore`（在文件末尾追加）

```text
# Frontend
web/node_modules/
web/dist/
joycode_proxy/static/
```

- [ ] **Step 9: 验证前端项目启动**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npx tsc --noEmit 2>&1 | head -20`
Expected:
  - Exit code: 0
  - No TypeScript errors

- [ ] **Step 10: 提交**
Run: `git add web/ .gitignore && git commit -m "feat(web): initialize React + Ant Design frontend with routing and layout"`

---

### Task 4: 账号管理页 — Accounts 页面完整功能

**Depends on:** Task 3
**Files:**
- Create: `web/src/pages/Accounts.tsx`

- [ ] **Step 1: 创建 Accounts 页面 — 账号列表、添加、删除、设为默认、验证**

```tsx
// web/src/pages/Accounts.tsx
import React, { useEffect, useState } from 'react';
import {
  Table, Button, Space, Modal, Form, Input, Switch,
  message, Popconfirm, Tag, Typography,
} from 'antd';
import {
  PlusOutlined, DeleteOutlined, StarOutlined,
  SafetyCertificateOutlined, ReloadOutlined,
} from '@ant-design/icons';
import { api, Account } from '../api';

const Accounts: React.FC = () => {
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [loading, setLoading] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [form] = Form.useForm();
  const [validating, setValidating] = useState<string | null>(null);

  const fetchAccounts = async () => {
    setLoading(true);
    try {
      const data = await api.listAccounts();
      setAccounts(data);
    } catch (e: any) {
      message.error(e.message);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { fetchAccounts(); }, []);

  const handleAdd = async (values: any) => {
    try {
      await api.addAccount(values);
      message.success(`Account "${values.api_key}" added`);
      setModalOpen(false);
      form.resetFields();
      fetchAccounts();
    } catch (e: any) {
      message.error(e.message);
    }
  };

  const handleRemove = async (apiKey: string) => {
    try {
      await api.removeAccount(apiKey);
      message.success(`Account "${apiKey}" removed`);
      fetchAccounts();
    } catch (e: any) {
      message.error(e.message);
    }
  };

  const handleSetDefault = async (apiKey: string) => {
    try {
      await api.setDefault(apiKey);
      message.success(`Default account set to "${apiKey}"`);
      fetchAccounts();
    } catch (e: any) {
      message.error(e.message);
    }
  };

  const handleValidate = async (apiKey: string) => {
    setValidating(apiKey);
    try {
      const result = await api.validateAccount(apiKey);
      if (result.valid) {
        message.success(`Account "${apiKey}" is valid`);
      } else {
        message.error(`Account "${apiKey}" validation failed`);
      }
    } catch (e: any) {
      message.error(e.message);
    } finally {
      setValidating(null);
    }
  };

  const columns = [
    {
      title: 'API Key',
      dataIndex: 'api_key',
      key: 'api_key',
      render: (text: string) => <Typography.Text code>{text}</Typography.Text>,
    },
    {
      title: 'User ID',
      dataIndex: 'user_id',
      key: 'user_id',
    },
    {
      title: 'Default',
      dataIndex: 'is_default',
      key: 'is_default',
      render: (val: boolean) => val ? <Tag color="blue"><StarOutlined /> Default</Tag> : null,
    },
    {
      title: 'Actions',
      key: 'actions',
      render: (_: any, record: Account) => (
        <Space>
          {!record.is_default && (
            <Button size="small" onClick={() => handleSetDefault(record.api_key)}>
              <StarOutlined /> Set Default
            </Button>
          )}
          <Button
            size="small"
            onClick={() => handleValidate(record.api_key)}
            loading={validating === record.api_key}
          >
            <SafetyCertificateOutlined /> Validate
          </Button>
          <Popconfirm
            title={`Remove account "${record.api_key}"?`}
            onConfirm={() => handleRemove(record.api_key)}
          >
            <Button size="small" danger><DeleteOutlined /></Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between' }}>
        <Typography.Title level={4} style={{ margin: 0 }}>Account Management</Typography.Title>
        <Space>
          <Button onClick={fetchAccounts} icon={<ReloadOutlined />}>Refresh</Button>
          <Button type="primary" onClick={() => setModalOpen(true)} icon={<PlusOutlined />}>
            Add Account
          </Button>
        </Space>
      </div>

      <Table
        dataSource={accounts}
        columns={columns}
        rowKey="api_key"
        loading={loading}
        pagination={false}
      />

      <Modal
        title="Add Account"
        open={modalOpen}
        onCancel={() => { setModalOpen(false); form.resetFields(); }}
        onOk={() => form.submit()}
      >
        <Form form={form} layout="vertical" onFinish={handleAdd}>
          <Form.Item name="api_key" label="API Key" rules={[{ required: true, message: 'Required' }]}>
            <Input placeholder="e.g. my-key-1 (used by clients to route)" />
          </Form.Item>
          <Form.Item name="pt_key" label="JoyCode ptKey" rules={[{ required: true, message: 'Required' }]}>
            <Input.Password placeholder="JoyCode ptKey credential" />
          </Form.Item>
          <Form.Item name="user_id" label="JoyCode User ID" rules={[{ required: true, message: 'Required' }]}>
            <Input placeholder="e.g. user-12345" />
          </Form.Item>
          <Form.Item name="is_default" label="Set as default" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
};

export default Accounts;
```

- [ ] **Step 2: 验证 Accounts 页面编译**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npx tsc --noEmit 2>&1 | head -20`
Expected:
  - Exit code: 0
  - No TypeScript errors

- [ ] **Step 3: 提交**
Run: `git add web/src/pages/Accounts.tsx && git commit -m "feat(web): add account management page with CRUD operations"`

---

### Task 5: 仪表盘 + 设置页 — Dashboard 和 Settings 页面

**Depends on:** Task 4
**Files:**
- Create: `web/src/pages/Dashboard.tsx`
- Create: `web/src/pages/Settings.tsx`

- [ ] **Step 1: 创建 Dashboard 页面 — 统计概览 + 请求图表**

```tsx
// web/src/pages/Dashboard.tsx
import React, { useEffect, useState } from 'react';
import { Card, Col, Row, Statistic, Typography, Spin, Empty } from 'antd';
import {
  ApiOutlined, TeamOutlined, ThunderboltOutlined,
  BarChartOutlined,
} from '@ant-design/icons';
import { BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from 'recharts';
import { api, Stats } from '../api';

const Dashboard: React.FC = () => {
  const [stats, setStats] = useState<Stats | null>(null);
  const [loading, setLoading] = useState(true);

  const fetchStats = async () => {
    setLoading(true);
    try {
      const data = await api.getStats();
      setStats(data);
    } catch (e: any) {
      console.error(e);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { fetchStats(); }, []);

  if (loading) return <Spin size="large" style={{ display: 'block', margin: '100px auto' }} />;
  if (!stats) return <Empty description="Failed to load stats" />;

  return (
    <div>
      <Typography.Title level={4}>Dashboard</Typography.Title>

      <Row gutter={[16, 16]}>
        <Col xs={24} sm={12} md={6}>
          <Card>
            <Statistic
              title="Total Requests"
              value={stats.total_requests}
              prefix={<ApiOutlined />}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} md={6}>
          <Card>
            <Statistic
              title="Accounts"
              value={stats.accounts_count}
              prefix={<TeamOutlined />}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} md={6}>
          <Card>
            <Statistic
              title="Avg Latency"
              value={stats.avg_latency_ms}
              suffix="ms"
              prefix={<ThunderboltOutlined />}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} md={6}>
          <Card>
            <Statistic
              title="Models Used"
              value={stats.by_model.length}
              prefix={<BarChartOutlined />}
            />
          </Card>
        </Col>
      </Row>

      {stats.by_model.length > 0 && (
        <Card title="Requests by Model" style={{ marginTop: 24 }}>
          <ResponsiveContainer width="100%" height={300}>
            <BarChart data={stats.by_model}>
              <CartesianGrid strokeDasharray="3 3" />
              <XAxis dataKey="model" />
              <YAxis />
              <Tooltip />
              <Bar dataKey="count" fill="#1677ff" radius={[4, 4, 0, 0]} />
            </BarChart>
          </ResponsiveContainer>
        </Card>
      )}

      {stats.by_account.length > 0 && (
        <Card title="Requests by Account" style={{ marginTop: 24 }}>
          <ResponsiveContainer width="100%" height={300}>
            <BarChart data={stats.by_account}>
              <CartesianGrid strokeDasharray="3 3" />
              <XAxis dataKey="api_key" />
              <YAxis />
              <Tooltip />
              <Bar dataKey="count" fill="#52c41a" radius={[4, 4, 0, 0]} />
            </BarChart>
          </ResponsiveContainer>
        </Card>
      )}
    </div>
  );
};

export default Dashboard;
```

- [ ] **Step 2: 创建 Settings 页面 — 代理配置编辑**

```tsx
// web/src/pages/Settings.tsx
import React, { useEffect, useState } from 'react';
import { Card, Form, Input, Button, message, Spin, Typography, Space, Divider } from 'antd';
import { SaveOutlined, ReloadOutlined } from '@ant-design/icons';
import { api, Settings } from '../api';

const SettingsPage: React.FC = () => {
  const [settings, setSettings] = useState<Settings>({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [form] = Form.useForm();

  const fetchSettings = async () => {
    setLoading(true);
    try {
      const data = await api.getSettings();
      setSettings(data);
      form.setFieldsValue(data);
    } catch (e: any) {
      message.error(e.message);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { fetchSettings(); }, [form]);

  const handleSave = async (values: Settings) => {
    setSaving(true);
    try {
      await api.updateSettings(values);
      message.success('Settings saved');
      setSettings(values);
    } catch (e: any) {
      message.error(e.message);
    } finally {
      setSaving(false);
    }
  };

  if (loading) return <Spin size="large" style={{ display: 'block', margin: '100px auto' }} />;

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between' }}>
        <Typography.Title level={4} style={{ margin: 0 }}>Settings</Typography.Title>
        <Button onClick={fetchSettings} icon={<ReloadOutlined />}>Refresh</Button>
      </div>

      <Card>
        <Form form={form} layout="vertical" onFinish={handleSave}>
          <Typography.Text type="secondary">
            These settings are stored in SQLite at <Typography.Text code>~/.joycode-proxy/proxy.db</Typography.Text>
          </Typography.Text>

          <Divider />

          <Form.Item name="proxy_host" label="Proxy Host">
            <Input placeholder="0.0.0.0" />
          </Form.Item>
          <Form.Item name="proxy_port" label="Proxy Port">
            <Input placeholder="34891" />
          </Form.Item>
          <Form.Item name="default_model" label="Default Model">
            <Input placeholder="JoyAI-Code" />
          </Form.Item>
          <Form.Item name="log_level" label="Log Level">
            <Input placeholder="info" />
          </Form.Item>

          <Space>
            <Button type="primary" htmlType="submit" loading={saving} icon={<SaveOutlined />}>
              Save Settings
            </Button>
          </Space>
        </Form>
      </Card>
    </div>
  );
};

export default SettingsPage;
```

- [ ] **Step 3: 验证全部前端编译**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npx tsc --noEmit 2>&1 | head -20`
Expected:
  - Exit code: 0
  - No TypeScript errors

- [ ] **Step 4: 提交**
Run: `git add web/src/pages/Dashboard.tsx web/src/pages/Settings.tsx && git commit -m "feat(web): add dashboard stats page and settings configuration page"`

---

### Task 6: 集成与构建 — 前端构建 + 后端集成

**Depends on:** Task 5
**Files:**
- Modify: `joycode_proxy/server.py`（静态文件服务已在 Task 2 添加）
- Modify: `web/index.html`（如有需要修正标题）

- [ ] **Step 1: 构建前端产物 — 输出到 joycode_proxy/static/**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy/web && npm run build 2>&1`
Expected:
  - Exit code: 0
  - Directory `joycode_proxy/static/` contains `index.html`, `assets/`

- [ ] **Step 2: 更新 .gitignore — 确保构建产物在 gitignore 中**
确认 `joycode_proxy/static/` 已在 `.gitignore` 中（Task 3 Step 8 已添加）。

- [ ] **Step 3: 验证完整应用启动 — 后端 + 静态文件 + API**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && python3 -c "
from joycode_proxy.server import create_app
from joycode_proxy.db import Database
db = Database()
db.add_account('test-key', 'test-pt', 'test-user')
app = create_app(db.get_credential_router(), db=db)
routes = [r.path for r in app.routes if hasattr(r, 'path')]
api_routes = sorted([r for r in routes if '/api/' in r])
print('API routes:', api_routes)
static_mounts = [r for r in app.routes if not hasattr(r, 'path')]
print('Static mount:', len(static_mounts))
print('OK')
"`
Expected:
  - Exit code: 0
  - Output contains: "/api/accounts" and "OK"

- [ ] **Step 4: 运行全部 Python 测试确保无回归**
Run: `cd /Users/cc11001100/github/vibe-coding-labs/JoyCodeProxy && python3 -m pytest tests/ -v`
Expected:
  - Exit code: 0
  - All tests pass

- [ ] **Step 5: 提交**
Run: `git add -A && git commit -m "feat(web): integrate built frontend with FastAPI static file serving"`
