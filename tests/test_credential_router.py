import json
import stat
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
        mode = path.stat().st_mode & 0o777
        assert mode == 0o600
