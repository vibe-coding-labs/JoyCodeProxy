import json
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
    tmpdir = tempfile.mkdtemp()
    data_dir = Path(tmpdir)
    json_path = data_dir / "accounts.json"
    json_path.write_text(json.dumps([
        {"api_key": "k1", "pt_key": "p1", "user_id": "u1", "default": True},
    ]))
    db = Database(data_dir / "test.db")
    import joycode_proxy.db as db_mod
    orig = db_mod.DATA_DIR
    db_mod.DATA_DIR = data_dir
    try:
        count = db.migrate_from_json()
        assert count == 1
        assert len(db.list_accounts()) == 1
        count2 = db.migrate_from_json()
        assert count2 == 0
        assert len(db.list_accounts()) == 1
    finally:
        db_mod.DATA_DIR = orig
    db.close()
