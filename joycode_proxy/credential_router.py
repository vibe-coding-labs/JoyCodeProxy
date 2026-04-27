import json
import logging
import os
from pathlib import Path
from typing import Dict, List, Optional

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
