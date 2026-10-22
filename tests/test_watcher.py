# Ryowatch - tests/test_watcher.py
# End-to-end smoke test of the watcher loop.

import os
import pytest

def _local_fork_available():
    """True if a local anvil fork is configured (via env or .env file)."""
    import os as _os
    url = _os.getenv("BASE_RPC_URL", "")
    if not url:
        env_path = _os.path.join(_os.path.dirname(_os.path.dirname(_os.path.abspath(__file__))), ".env")
        if _os.path.exists(env_path):
            for line in open(env_path):
                line = line.strip()
                if line and not line.startswith("#") and "=" in line:
                    k, v = line.split("=", 1)
                    if k.strip() == "BASE_RPC_URL":
                        url = v.strip()
    return "127.0.0.1" in url or "localhost" in url

pytestmark = pytest.mark.skipif(
    not _local_fork_available(),
    reason="needs a local Base fork (anvil)",
)

from src import config, watcher


def test_scan_once_returns_int():
    n = watcher.scan_once()
    assert isinstance(n, int)
    assert n >= 0


def test_scan_once_does_not_crash_on_empty_routes(monkeypatch):
    import src.config as cfg
    monkeypatch.setattr(cfg, "DEFAULT_ROUTES", [])
    import importlib
    import src.watcher as w
    importlib.reload(w)
    w.config = cfg
    n = w.scan_once()
    assert n == 0
