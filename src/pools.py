# Ryowatch - pools.py
# On-chain quote helpers. Every quote is a view call, so it costs no gas and is always fresh.

from . import config
from . import chains
from web3 import Web3


def token_decimals(token):
    """Return the decimals of an ERC-20 token."""
    return chains.erc20(token).functions.decimals().call()


def _units(amount, token):
    """Convert a human amount (e.g. 1.0 WETH) to raw units using the token's decimals."""
    return int(amount * (10 ** token_decimals(token)))


def quote_v3(token_in, token_out, fee, amount_in_raw):
    """Quote a Uniswap V3 swap via the on-chain QuoterV2."""
    try:
        out = chains.univ3_quoter(config.UNISWAP_V3_QUOTER).functions.quoteExactInputSingle(
            (token_in, token_out, amount_in_raw, fee, 0)
        ).call()
        return out[0]
    except Exception as e:
        return None


def quote_v2(token_in, token_out, amount_in_raw):
    """Quote a Uniswap V2 swap via the router's getAmountsOut."""
    try:
        amounts = chains.univ2_router(config.UNISWAP_V2_ROUTER).functions.getAmountsOut(
            amount_in_raw, [token_in, token_out]
        ).call()
        return amounts[-1]
    except Exception:
        return None


def quote_aerodrome(token_in, token_out, stable, amount_in_raw, factory=None):
    """Quote an Aerodrome classic swap via the router's getAmountsOut."""
    factory = factory or config.AERODROME_FACTORY
    route = (token_in, token_out, stable, factory)
    try:
        amounts = chains.aerodrome_router(config.AERODROME_ROUTER).functions.getAmountsOut(
            amount_in_raw, [route]
        ).call()
        return amounts[-1]
    except Exception:
        return None


def quote_slipstream(token_in, token_out, tick_spacing, amount_in_raw):
    """Quote an Aerodrome Slipstream swap via the on-chain quoter."""
    try:
        out = chains.slipstream_quoter(config.SLIPSTREAM_QUOTER).functions.quoteExactInputSingle(
            (token_in, token_out, amount_in_raw, tick_spacing, 0)
        ).call()
        return out[0]
    except Exception:
        return None


def quote_hop(hop, amount_in_raw):
    """Quote a single hop based on its dex type."""
    if hop.dex == "v3":
        return quote_v3(hop.token_in, hop.token_out, hop.fee, amount_in_raw)
    if hop.dex == "v2":
        return quote_v2(hop.token_in, hop.token_out, amount_in_raw)
    if hop.dex == "aerodrome":
        return quote_aerodrome(hop.token_in, hop.token_out, hop.stable, amount_in_raw)
    if hop.dex == "slipstream":
        return quote_slipstream(hop.token_in, hop.token_out, hop.tick_spacing, amount_in_raw)
    return None


def evaluate_route(route, amount_override=None):
    """Walk a route hop by hop and return (final_amount_raw, profit_raw) or (None, None)."""
    amount = amount_override if amount_override is not None else route.amount
    amount_raw = _units(amount, route.asset)

    cur = amount_raw
    for hop in route.hops:
        out = quote_hop(hop, cur)
        if out is None or out <= 0:
            return None, None
        cur = out

    profit_raw = cur - amount_raw
    return cur, profit_raw


def pool_depth_v2(token_a, token_b):
    """Best-effort liquidity read for a Uniswap V2 pool (reserve of token_a)."""
    try:
        # Find the pair via the factory is out of scope; return None and fall back to the cap.
        return None
    except Exception:
        return None
