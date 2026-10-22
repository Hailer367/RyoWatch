# Ryowatch - routes.py
# Build the Ryo contract's Swap structs from a Route, and evaluate expected profit.

from . import config
from . import chains
from eth_abi import encode

DEX_TYPE = {"v3": 6, "v2": 8, "aerodrome": 4, "slipstream": 7}


def hop_extra_data(hop):
    """Encode a hop's extraData exactly as Ryo expects it."""
    if hop.dex == "v3":
        if hop.min_out and hop.min_out > 1:
            return encode(["uint24", "uint256"], [hop.fee, hop.min_out])
        return encode(["uint24"], [hop.fee])
    if hop.dex == "v2":
        if hop.min_out and hop.min_out > 1:
            path = [hop.token_in, hop.token_out]
            return encode(["address[]", "uint256"], [path, hop.min_out])
        return b""
    if hop.dex == "aerodrome":
        path = [hop.token_in, hop.token_out]
        stables = [bool(hop.stable)]
        mo = hop.min_out if hop.min_out else 1
        return encode(["address[]", "bool[]", "uint256"], [path, stables, mo])
    if hop.dex == "slipstream":
        if hop.min_out and hop.min_out > 1:
            return encode(["int24", "uint256"], [hop.tick_spacing, hop.min_out])
        return encode(["int24"], [hop.tick_spacing])
    raise ValueError(f"unknown dex {hop.dex}")


def to_ryo_swaps(route):
    """Convert a Route into the list of tuples Ryo.execute expects."""
    out = []
    for hop in route.hops:
        out.append((
            DEX_TYPE[hop.dex],
            hop.router,
            hop.token_in,
            hop.token_out,
            hop_extra_data(hop),
        ))
    return out


def slippage_floor(amount_raw, bps):
    """Apply a slippage floor: amount * (1 - bps/10000)."""
    return amount_raw * (10000 - bps) // 10000
