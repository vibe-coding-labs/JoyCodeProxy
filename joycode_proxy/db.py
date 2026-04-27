import json
import logging
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
        """Build a CredentialRouter from DB accounts."""
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
