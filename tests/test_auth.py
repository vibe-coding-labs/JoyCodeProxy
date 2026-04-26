import os
import tempfile

import pytest

from joycode_proxy.auth import Credentials, load_from_system


def test_credentials_fields():
    c = Credentials(pt_key="test-key", user_id="test-user")
    assert c.pt_key == "test-key"
    assert c.user_id == "test-user"


def test_load_from_system_database_not_found(monkeypatch):
    with tempfile.TemporaryDirectory() as tmpdir:
        monkeypatch.setenv("HOME", tmpdir)
        with pytest.raises(FileNotFoundError, match="not found"):
            load_from_system()


def test_load_from_system_integration():
    home = os.path.expanduser("~")
    db_path = os.path.join(
        home, "Library", "Application Support",
        "JoyCode", "User", "globalStorage", "state.vscdb",
    )
    if not os.path.exists(db_path):
        pytest.skip("JoyCode database not found")
    creds = load_from_system()
    assert creds.pt_key
    assert creds.user_id
