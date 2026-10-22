# Ryowatch - tests/test_executor.py
# Integration tests against a Base fork (anvil). Skipped if anvil is not running.

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

from src import config, chains, executor, pools


def test_simulate_returns_profit():
    route = config.DEFAULT_ROUTES[0]
    profit, err = executor.simulate(route)
    assert err is None, err
    assert profit is not None
    # simulate always returns a number (profit or loss)
    assert isinstance(profit, int)


def test_simulate_matches_off_chain_quote():
    route = config.DEFAULT_ROUTES[0]
    final, offchain_profit = pools.evaluate_route(route)
    sim_profit, err = executor.simulate(route)
    assert err is None, err
    # on-chain and off-chain should agree within 0.1%
    assert abs(sim_profit - offchain_profit) < abs(offchain_profit) * 0.001 + 1


def test_execute_reverts_on_unprofitable_route():
    route = config.DEFAULT_ROUTES[0]
    tx_hash, result = executor.execute(route)
    # route is unprofitable at current prices, so execute should fail
    assert tx_hash is None
    assert result is not None


def test_execute_dry_run_does_not_send():
    import os
    os.environ["DRY_RUN"] = "true"
    import importlib
    import src.config as cfg
    importlib.reload(cfg)
    route = cfg.DEFAULT_ROUTES[0]
    tx_hash, result = executor.execute(route)
    assert tx_hash is None
    assert "dry run" in (result or "")
