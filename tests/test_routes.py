# Ryowatch - tests/test_routes.py
# Unit tests for route encoding and evaluation helpers.

import pytest
from src import config, routes


def test_dex_type_mapping():
    assert routes.DEX_TYPE["v3"] == 6
    assert routes.DEX_TYPE["v2"] == 8
    assert routes.DEX_TYPE["aerodrome"] == 4
    assert routes.DEX_TYPE["slipstream"] == 7


def test_hop_extra_data_v3_fee_only():
    from src.config import Hop
    hop = Hop(dex="v3", router="0x0", token_in="0x4200000000000000000000000000000000000006", token_out="0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", fee=500)
    data = routes.hop_extra_data(hop)
    # 32 bytes: abi.encode(uint24)
    assert len(data) == 32


def test_hop_extra_data_v3_with_floor():
    from src.config import Hop
    hop = Hop(dex="v3", router="0x0", token_in="0x4200000000000000000000000000000000000006", token_out="0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", fee=500, min_out=1000)
    data = routes.hop_extra_data(hop)
    assert len(data) == 64


def test_hop_extra_data_v2_direct():
    from src.config import Hop
    hop = Hop(dex="v2", router="0x0", token_in="0x4200000000000000000000000000000000000006", token_out="0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", min_out=1)
    data = routes.hop_extra_data(hop)
    assert data == b""


def test_hop_extra_data_v2_with_path():
    from src.config import Hop
    hop = Hop(dex="v2", router="0x0", token_in="0x4200000000000000000000000000000000000006", token_out="0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", min_out=1000)
    data = routes.hop_extra_data(hop)
    assert len(data) > 0


def test_hop_extra_data_aerodrome():
    from src.config import Hop
    hop = Hop(dex="aerodrome", router="0x0", token_in="0x4200000000000000000000000000000000000006", token_out="0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", stable=True, min_out=1)
    data = routes.hop_extra_data(hop)
    assert len(data) >= 256  # canonical encoding minimum


def test_hop_extra_data_slipstream():
    from src.config import Hop
    hop = Hop(dex="slipstream", router="0x0", token_in="0x4200000000000000000000000000000000000006", token_out="0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", tick_spacing=10)
    data = routes.hop_extra_data(hop)
    assert len(data) == 32


def test_hop_extra_data_slipstream_with_floor():
    from src.config import Hop
    hop = Hop(dex="slipstream", router="0x0", token_in="0x4200000000000000000000000000000000000006", token_out="0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", tick_spacing=1, min_out=500)
    data = routes.hop_extra_data(hop)
    assert len(data) == 64


def test_hop_extra_data_unknown_dex_raises():
    from src.config import Hop
    hop = Hop(dex="curve", router="0x0", token_in="0x4200000000000000000000000000000000000006", token_out="0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913")
    with pytest.raises(ValueError):
        routes.hop_extra_data(hop)


def test_to_ryo_swaps_structure():
    route = config.DEFAULT_ROUTES[0]
    swaps = routes.to_ryo_swaps(route)
    assert len(swaps) == 2
    assert swaps[0][0] == 6  # v3
    assert swaps[1][0] == 8  # v2
    assert swaps[0][2] == route.hops[0].token_in
    assert swaps[0][3] == route.hops[0].token_out


def test_slippage_floor():
    raw = 10**18
    floored = routes.slippage_floor(raw, 50)  # 0.5%
    assert floored == 995 * 10**15
    assert floored < raw
