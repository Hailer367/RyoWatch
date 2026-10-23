# Ryowatch - chains.py
# Web3 connection and contract instances.

from web3 import Web3
from web3.middleware import ExtraDataToPOAMiddleware
from . import config

# Connect to Base.
w3 = Web3(Web3.HTTPProvider(config.RPC_URL))
# Base is a standard EVM chain; no POA middleware needed, but harmless to keep for safety.

RYO_ABI = [
    {
        "name": "simulate",
        "type": "function",
        "stateMutability": "nonpayable",
        "inputs": [
            {"name": "asset", "type": "address"},
            {"name": "amount", "type": "uint256"},
            {"name": "swaps", "type": "tuple[]", "components": [
                {"name": "dexType", "type": "uint8"},
                {"name": "router", "type": "address"},
                {"name": "tokenIn", "type": "address"},
                {"name": "tokenOut", "type": "address"},
                {"name": "extraData", "type": "bytes"},
            ]},
        ],
        "outputs": [],
    },
    {
        "name": "execute",
        "type": "function",
        "stateMutability": "nonpayable",
        "inputs": [
            {"name": "asset", "type": "address"},
            {"name": "amount", "type": "uint256"},
            {"name": "swaps", "type": "tuple[]", "components": [
                {"name": "dexType", "type": "uint8"},
                {"name": "router", "type": "address"},
                {"name": "tokenIn", "type": "address"},
                {"name": "tokenOut", "type": "address"},
                {"name": "extraData", "type": "bytes"},
            ]},
            {"name": "minProfit", "type": "uint256"},
        ],
        "outputs": [{"name": "profit", "type": "uint256"}],
    },
    {
        "name": "executePrimary",
        "type": "function",
        "stateMutability": "nonpayable",
        "inputs": [
            {"name": "routeId", "type": "uint8"},
            {"name": "amount", "type": "uint256"},
            {"name": "minProfit", "type": "uint256"},
        ],
        "outputs": [{"name": "profit", "type": "uint256"}],
    },
    {
        "name": "paused",
        "type": "function",
        "stateMutability": "view",
        "inputs": [],
        "outputs": [{"name": "", "type": "bool"}],
    },
    {
        "name": "maxTradeSize",
        "type": "function",
        "stateMutability": "view",
        "inputs": [{"name": "asset", "type": "address"}],
        "outputs": [{"name": "", "type": "uint256"}],
    },
    {
        "name": "isRouterWhitelisted",
        "type": "function",
        "stateMutability": "view",
        "inputs": [{"name": "router", "type": "address"}],
        "outputs": [{"name": "", "type": "bool"}],
    },
]

# Contract instances (created lazily so config can be validated first).
ryo = None
if config.RYO_ADDRESS:
    ryo = w3.eth.contract(address=Web3.to_checksum_address(config.RYO_ADDRESS), abi=RYO_ABI)

ERC20_ABI = [
    {"name": "decimals", "type": "function", "stateMutability": "view", "inputs": [], "outputs": [{"name": "", "type": "uint8"}]},
    {"name": "balanceOf", "type": "function", "stateMutability": "view", "inputs": [{"name": "a", "type": "address"}], "outputs": [{"name": "", "type": "uint256"}]},
    {"name": "allowance", "type": "function", "stateMutability": "view", "inputs": [{"name": "o", "type": "address"}, {"name": "s", "type": "address"}], "outputs": [{"name": "", "type": "uint256"}]},
]

UNIV2_ROUTER_ABI = [
    {"name": "getAmountsOut", "type": "function", "stateMutability": "view", "inputs": [{"name": "amountIn", "type": "uint256"}, {"name": "path", "type": "address[]"}], "outputs": [{"name": "amounts", "type": "uint256[]"}]},
    {"name": "getReserves", "type": "function", "stateMutability": "view", "inputs": [], "outputs": [{"name": "reserve0", "type": "uint112"}, {"name": "reserve1", "type": "uint112"}, {"name": "blockTimestampLast", "type": "uint32"}]},
]

UNIV3_QUOTER_ABI = [
    {"name": "quoteExactInputSingle", "type": "function", "stateMutability": "nonpayable", "inputs": [{"name": "params", "type": "tuple", "components": [
        {"name": "tokenIn", "type": "address"}, {"name": "tokenOut", "type": "address"},
        {"name": "amountIn", "type": "uint256"}, {"name": "fee", "type": "uint24"},
        {"name": "sqrtPriceLimitX96", "type": "uint160"}]}],
     "outputs": [{"name": "amountOut", "type": "uint256"}, {"name": "sqrtPriceX96After", "type": "uint160"}, {"name": "initializedTicksCrossed", "type": "uint32"}, {"name": "gasEstimate", "type": "uint256"}]},
]

AERODROME_ROUTER_ABI = [
    {"name": "getAmountsOut", "type": "function", "stateMutability": "view", "inputs": [{"name": "amountIn", "type": "uint256"}, {"name": "routes", "type": "tuple[]", "components": [
        {"name": "from", "type": "address"}, {"name": "to", "type": "address"},
        {"name": "stable", "type": "bool"}, {"name": "factory", "type": "address"}]}],
     "outputs": [{"name": "amounts", "type": "uint256[]"}]},
]

SLIPSTREAM_QUOTER_ABI = [
    {"name": "quoteExactInputSingle", "type": "function", "stateMutability": "nonpayable", "inputs": [{"name": "params", "type": "tuple", "components": [
        {"name": "tokenIn", "type": "address"}, {"name": "tokenOut", "type": "address"},
        {"name": "amountIn", "type": "uint256"}, {"name": "tickSpacing", "type": "int24"},
        {"name": "sqrtPriceLimitX96", "type": "uint160"}]}],
     "outputs": [{"name": "amountOut", "type": "uint256"}, {"name": "sqrtPriceX96After", "type": "uint160"}, {"name": "initializedTicksCrossed", "type": "uint32"}, {"name": "gasEstimate", "type": "uint256"}]},
]

def checksum(addr):
    return Web3.to_checksum_address(addr)

def erc20(addr):
    return w3.eth.contract(address=checksum(addr), abi=ERC20_ABI)

def univ2_router(addr):
    return w3.eth.contract(address=checksum(addr), abi=UNIV2_ROUTER_ABI)

def univ3_quoter(addr):
    return w3.eth.contract(address=checksum(addr), abi=UNIV3_QUOTER_ABI)

def aerodrome_router(addr):
    return w3.eth.contract(address=checksum(addr), abi=AERODROME_ROUTER_ABI)

def slipstream_quoter(addr):
    return w3.eth.contract(address=checksum(addr), abi=SLIPSTREAM_QUOTER_ABI)


# ---- Factory helpers for pool resolution ----
UNIV2_FACTORY_ABI = [{"name":"getPair","type":"function","stateMutability":"view","inputs":[{"name":"tokenA","type":"address"},{"name":"tokenB","type":"address"}],"outputs":[{"name":"","type":"address"}]}]
UNIV3_FACTORY_ABI = [{"name":"getPool","type":"function","stateMutability":"view","inputs":[{"name":"tokenA","type":"address"},{"name":"tokenB","type":"address"},{"name":"fee","type":"uint24"}],"outputs":[{"name":"","type":"address"}]}]

def univ2_factory():
    return w3.eth.contract(address=Web3.to_checksum_address("0x8909Dc15e40173Ff4699343b6eB8132c65e18eC6"), abi=UNIV2_FACTORY_ABI)

def univ3_factory():
    return w3.eth.contract(address=Web3.to_checksum_address("0x33128a8fC17869897dcE68Ed026d694621f6FDfD"), abi=UNIV3_FACTORY_ABI)
