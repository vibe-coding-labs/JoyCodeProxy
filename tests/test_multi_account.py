"""Multi-account routing integration tests.

Tests the routing logic directly (handler → CredentialRouter) without
HTTP-level round-trips, avoiding starlette/httpx version compatibility
issues in the test environment.
"""

from joycode_proxy.credential_router import CredentialRouter


def test_router_returns_correct_client_for_key():
    router = CredentialRouter()
    router.add_account("key-alpha", "pt-alpha", "user-alpha", default=True)
    router.add_account("key-beta", "pt-beta", "user-beta")

    client_alpha = router.get_client("key-alpha")
    assert client_alpha.pt_key == "pt-alpha"
    assert client_alpha.user_id == "user-alpha"

    client_beta = router.get_client("key-beta")
    assert client_beta.pt_key == "pt-beta"
    assert client_beta.user_id == "user-beta"

    assert client_alpha is not client_beta


def test_router_falls_back_to_default():
    router = CredentialRouter()
    router.add_account("key-alpha", "pt-alpha", "user-alpha", default=True)
    router.add_account("key-beta", "pt-beta", "user-beta")

    # Unknown key → default
    client = router.get_client("unknown-key")
    assert client.user_id == "user-alpha"

    # No key → default
    client = router.get_client()
    assert client.user_id == "user-alpha"


def test_router_no_account_raises():
    router = CredentialRouter()
    raised = False
    try:
        router.get_client("anything")
    except KeyError:
        raised = True
    assert raised


def test_router_session_persistence():
    """Same key always returns the same Client (same session_id for cache)."""
    router = CredentialRouter()
    router.add_account("key-1", "pt-abc", "user-1")

    client_a = router.get_client("key-1")
    client_b = router.get_client("key-1")
    assert client_a is client_b
    assert client_a.session_id == client_b.session_id


def test_router_list_accounts():
    router = CredentialRouter()
    router.add_account("key-alpha", "pt-alpha", "user-alpha", default=True)
    router.add_account("key-beta", "pt-beta", "user-beta")

    accounts = router.list_accounts()
    assert len(accounts) == 2

    alpha = next(a for a in accounts if a["api_key"] == "key-alpha")
    assert alpha["is_default"] is True
    assert alpha["user_id"] == "user-alpha"

    beta = next(a for a in accounts if a["api_key"] == "key-beta")
    assert beta["is_default"] is False


def test_router_save_load_roundtrip():
    import tempfile
    from pathlib import Path

    with tempfile.TemporaryDirectory() as tmpdir:
        path = Path(tmpdir) / "accounts.json"

        router = CredentialRouter()
        router.add_account("key-a", "pt-a", "user-a", default=True)
        router.add_account("key-b", "pt-b", "user-b")
        router.save(path)

        loaded = CredentialRouter.load(path)
        assert len(loaded.list_accounts()) == 2

        # Routing still works after reload
        client_a = loaded.get_client("key-a")
        assert client_a.pt_key == "pt-a"

        client_b = loaded.get_client("key-b")
        assert client_b.pt_key == "pt-b"

        # Default preserved
        assert loaded.default_key == "key-a"
        fallback = loaded.get_client("unknown")
        assert fallback.user_id == "user-a"


def test_router_remove_updates_default():
    router = CredentialRouter()
    router.add_account("key-1", "pt-1", "user-1")
    router.add_account("key-2", "pt-2", "user-2", default=True)

    assert router.default_key == "key-2"
    router.remove_account("key-2")
    assert router.default_key == "key-1"

    # key-2 no longer routes
    client = router.get_client("key-2")
    assert client.user_id == "user-1"  # falls back to new default


def test_router_overwrite_account():
    router = CredentialRouter()
    router.add_account("key-1", "pt-old", "user-old")
    router.add_account("key-1", "pt-new", "user-new")

    client = router.get_client("key-1")
    assert client.pt_key == "pt-new"
    assert client.user_id == "user-new"
