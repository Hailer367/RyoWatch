# Ryowatch - tests/test_pools.py
# Integration tests against a live Base RPC. Skipped if no network.

import os
import pytest

# Skip the whole module if no RPC is available.
pytestmark = pytest.mark.skipif(
    os.getenv("BASE_RPC_URL", "") == "",
    reason="BASE_RPC_URL not set",
)

from src import config, chains, pools


def test_token_decimals():
    assert pools.token_decimals(config.WETH) == 18
    assert pools.token_decimals(config.USDC) == 6


def test_quote_v3_returns_positive():
    out = pools.quote_v3(config.WETH, config.USDC, 500, 10**18)
    assert out is not None
    assert out > 0
    # sanity: ~2000-3000 USDC per WETH
    assert 1000 * 10**6 < out < 5000 * 10**6


def test_quote_v3_different_fees():
    out500 = pools.quote_v3(config.WETH, config.USDC, 500, 10**18)
    out3000 = pools.quote_v3(config.WETH, config.USDC, 3000, 10**18)
    assert out500 is not None
    assert out3000 is not None
    # both should be in the same ballpark
    assert abs(out500 - out3000) < out500 * 0.05


def test_quote_v2_returns_positive():
    out = pools.quote_v2(config.WETH, config.USDC, 10**18)
    assert out is not None
    assert out > 0


def test_quote_aerodrome_volatile():
    out = pools.quote_aerodrome(config.WETH, config.USDC, False, 10**18)
    assert out is not None
    assert out > 0


def test_quote_aerodrome_stable():
    out = pools.quote_aerodrome(config.WETH, config.USDC, True, 10**17)
    assert out is not None
    assert out > 0


def test_quote_slipstream():
    out = pools.quote_slipstream(config.WETH, config.USDC, 1, 10**17)
    assert out is not None
    assert out > 0


def test_evaluate_route_returns_profit_or_loss():
    route = config.DEFAULT_ROUTES[0]
    final, profit = pools.evaluate_route(route)
    assert final is not None
    assert profit is not None
    # at current prices this route loses a little to fees
    assert isinstance(profit, int)


def test_evaluate_route_bad_token_returns_none():
    from src.config import Hop, Route
    bad_route = Route(
        name="bad",
        asset=config.WETH,
        amount=1.0,
        hops=[Hop(dex="v3", router=config.UNISWAP_V3_ROUTER, token_in=config.WETH, token_out="0x0000000000000000000000000000000000000001", fee=500)],
    )
    final, profit = pools.evaluate_route(bad_route)
    assert final is None
    assert profit is None
