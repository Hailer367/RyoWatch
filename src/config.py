# Ryowatch - off-chain watcher for the Ryo flash-loan arbitrage executor.
#
# This file is the single source of truth for addresses, ABIs and default limits.
# Secrets (private keys) are read from the environment, never from this file.

from dataclasses import dataclass, field
from typing import List, Optional
import os

# Load .env if present (so the bot and tests pick up BASE_RPC_URL, RYO_ADDRESS, etc.).
# Parsed by hand so there is no extra dependency.
_env_path = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), ".env")
if os.path.exists(_env_path):
    with open(_env_path) as _f:
        for _line in _f:
            _line = _line.strip()
            if not _line or _line.startswith("#") or "=" not in _line:
                continue
            _key, _val = _line.split("=", 1)
            _key = _key.strip()
            _val = _val.strip().strip('"').strip("'")
            if _key and _key not in os.environ:
                os.environ[_key] = _val

# ---- Base mainnet addresses (verified on-chain) ----
BALANCER_VAULT = "0xBA12222222228d8Ba445958a75a0704d566BF2C8"
WETH = "0x4200000000000000000000000000000000000006"
USDC = "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"

cbBTC = "0xcbB7C0000aB88B473b1f5aFd9ef808440eed33Bf"
AERO = "0x940181a94A35A4569E4529A3CDfB74e38FD98631"
EURC = "0x60a3E35Cc302bFA44Cb288Bc5a4F316Fdb1adb42"

UNISWAP_V3_ROUTER = "0x2626664c2603336E57B271c5C0b26F421741e481"
UNISWAP_V3_QUOTER = "0x3d4e44Eb1374240CE5F1B871ab261CD16335B76a"
UNISWAP_V2_ROUTER = "0x4752ba5DBc23f44D87826276BF6Fd6b1C372aD24"

AERODROME_ROUTER = "0xcF77a3Ba9A5CA399B7c97c74d54e5b1Beb874E43"
AERODROME_FACTORY = "0x420DD381b31aEf6683db6B902084cB0FFECe40Da"

SLIPSTREAM_ROUTER = "0xBE6D8f0d05cC4be24d5167a3eF062215bE6D18a5"
SLIPSTREAM_QUOTER = "0x254cF9E1E6e233aa1AC962CB9B05b2cfeAaE15b0"
SLIPSTREAM_FACTORY = "0x5e7BB104d84c7CB9B682AaC2F3d509f5F406809A"

# ---- Ryo contract ----
RYO_ADDRESS = os.getenv("RYO_ADDRESS", "")

# ---- RPC ----
RPC_URL = os.getenv("BASE_RPC_URL", "https://mainnet.base.org")

# ---- Wallets (public addresses only here; private key comes from env) ----
OPERATOR_ADDRESS = os.getenv("OPERATOR_ADDRESS", "")

# ---- Safety (the bot is the primary safety net; these are backstops) ----
MAX_TRADE_SIZE = float(os.getenv("MAX_TRADE_SIZE", "1.0"))          # in units of the borrowed token
POOL_DEPTH_FRACTION = float(os.getenv("POOL_DEPTH_FRACTION", "0.01"))
MAX_GAS_PRICE_GWEI = int(os.getenv("MAX_GAS_PRICE_GWEI", "50"))
MIN_PROFIT = float(os.getenv("MIN_PROFIT", "0.001"))                # minimum profit to execute
SLIPPAGE_BPS = int(os.getenv("SLIPPAGE_BPS", "50"))                # 0.5% per hop
CIRCUIT_BREAKER_FAILURES = int(os.getenv("CIRCUIT_BREAKER_FAILURES", "3"))
POLL_INTERVAL_SECONDS = float(os.getenv("POLL_INTERVAL_SECONDS", "2.0"))
DRY_RUN = os.getenv("DRY_RUN", "true").lower() == "true"


@dataclass
class Hop:
    """One step of a route: tokenIn -> tokenOut on a given DEX."""
    dex: str                      # "v3", "v2", "aerodrome", "slipstream"
    router: str
    token_in: str
    token_out: str
    # v3
    fee: Optional[int] = None
    # aerodrome
    stable: Optional[bool] = None
    # slipstream
    tick_spacing: Optional[int] = None
    # min output floor for this hop (0 -> normalized to 1 by the contract)
    min_out: int = 1


@dataclass
class Route:
    name: str
    asset: str                    # borrowed token (must be first tokenIn and last tokenOut)
    amount: float                 # amount to borrow, in token units
    hops: List[Hop] = field(default_factory=list)
    min_profit: float = 0.01


# ---- Default routes (WETH <-> USDC) ----
DEFAULT_ROUTES: List[Route] = [
    Route(
        name="weth-usdc-v3-v2",
        asset=WETH,
        amount=1.0,
        hops=[
            Hop(dex="v3", router=UNISWAP_V3_ROUTER, token_in=WETH, token_out=USDC, fee=500),
            Hop(dex="v2", router=UNISWAP_V2_ROUTER, token_in=USDC, token_out=WETH, min_out=1),
        ],
    ),
    Route(
        name="usdc-weth-v3-v2",
        asset=USDC,
        amount=1000.0,
        hops=[
            Hop(dex="v3", router=UNISWAP_V3_ROUTER, token_in=USDC, token_out=WETH, fee=500),
            Hop(dex="v2", router=UNISWAP_V2_ROUTER, token_in=WETH, token_out=USDC, min_out=1),
        ],
    ),
    Route(
        name="cbbtc-weth-aero-v3",
        asset=WETH,
        amount=0.1,
        hops=[
            Hop(dex="aerodrome", router=AERODROME_ROUTER, token_in=WETH, token_out=cbBTC, stable=False, min_out=1),
            Hop(dex="v3", router=UNISWAP_V3_ROUTER, token_in=cbBTC, token_out=WETH, fee=500, min_out=1),
        ],
    ),
    Route(
        name="aero-usdc-v3-aero",
        asset=USDC,
        amount=100.0,
        hops=[
            Hop(dex="v3", router=UNISWAP_V3_ROUTER, token_in=USDC, token_out=AERO, fee=100, min_out=1),
            Hop(dex="aerodrome", router=AERODROME_ROUTER, token_in=AERO, token_out=USDC, stable=False, min_out=1),
        ],
    ),
    Route(
        name="usdc-eurc-slip-v3",
        asset=USDC,
        amount=100.0,
        hops=[
            Hop(dex="slipstream", router=SLIPSTREAM_ROUTER, token_in=USDC, token_out=EURC, tick_spacing=50, min_out=1),
            Hop(dex="v3", router=UNISWAP_V3_ROUTER, token_in=EURC, token_out=USDC, fee=10000, min_out=1),
        ],
    ),
]
