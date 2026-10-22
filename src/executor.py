# Ryowatch - executor.py
# Talks to the Ryo contract: simulate (eth_call) and execute (signed transaction).

import os
from eth_abi import decode as abi_decode
from web3 import Web3
from . import config
from . import chains
from . import routes as R
from . import pools

SIMULATION_RESULT_SELECTOR = "0x00cf1ea6"

# SimulationResult(int256) selector
SIM_RESULT_SEL = bytes.fromhex("00cf1ea6")


def _account():
    pk = os.getenv("OPERATOR_PRIVATE_KEY")
    if not pk:
        raise RuntimeError("OPERATOR_PRIVATE_KEY is not set")
    from . import chains
    return chains.w3.eth.account.from_key(pk)


def simulate(route, amount_override=None):
    """Simulate a route. Returns (profit_raw_int, None) on success, (None, error) on failure."""
    if chains.ryo is None:
        raise RuntimeError("RYO_ADDRESS is not set")
    amt = amount_override if amount_override is not None else route.amount
    decimals = pools.token_decimals(route.asset)
    amt_raw = int(amt * (10 ** decimals))
    swaps = R.to_ryo_swaps(route)

    try:
        chains.ryo.functions.simulate(route.asset, amt_raw, swaps).call({"from": config.OPERATOR_ADDRESS})
        # simulate always reverts, so reaching here means something is wrong
        return None, "simulate did not revert"
    except Exception as e:
        # web3.py raises with the revert data in e.args or e.data
        data = _extract_revert_data(e)
        if data is None or not data.startswith(SIM_RESULT_SEL):
            return None, f"unexpected revert: {e}"
        profit = abi_decode(["int256"], data[4:])[0]
        return profit, None


def execute(route, amount_override=None, min_profit_raw=None):
    """Execute a route. Returns (tx_hash, profit_raw) on success, (None, error) on failure."""
    if chains.ryo is None:
        raise RuntimeError("RYO_ADDRESS is not set")
    if config.DRY_RUN:
        return None, "dry run: not sending transaction"

    amt = amount_override if amount_override is not None else route.amount
    decimals = pools.token_decimals(route.asset)
    amt_raw = int(amt * (10 ** decimals))
    swaps = R.to_ryo_swaps(route)
    mp = min_profit_raw if min_profit_raw is not None else int(route.min_profit * 1e18)

    acct = _account()
    fn = chains.ryo.functions.execute(route.asset, amt_raw, swaps, mp)

    try:
        tx = fn.build_transaction({
            "from": acct.address,
            "nonce": chains.w3.eth.get_transaction_count(acct.address),
            "gas": 600_000,
            "maxFeePerGas": chains.w3.eth.gas_price * 2,
            "maxPriorityFeePerGas": chains.w3.to_wei(1, "gwei"),
            "chainId": chains.w3.eth.chain_id,
        })
        signed = acct.sign_transaction(tx)
        tx_hash = chains.w3.eth.send_raw_transaction(signed.raw_transaction)
        receipt = chains.w3.eth.wait_for_transaction_receipt(tx_hash, timeout=120)
    except Exception as e:
        return None, f"tx failed: {e}"

    if receipt.status != 1:
        return None, f"tx reverted: {tx_hash.hex()}"

    # Parse profit from the ArbitrageExecuted event.
    profit = _parse_profit(receipt)
    return tx_hash, profit


def _extract_revert_data(exc):
    """Pull the revert data bytes out of a web3.py exception."""
    # web3.py v7+: exc has .data or args[0]['data']
    data = getattr(exc, "data", None)
    if isinstance(data, str):
        return bytes.fromhex(data.removeprefix("0x"))
    if isinstance(data, (bytes, bytearray)):
        return bytes(data)
    args = getattr(exc, "args", None)
    if args:
        first = args[0]
        if isinstance(first, dict):
            d = first.get("data")
            if isinstance(d, str):
                return bytes.fromhex(d.removeprefix("0x"))
            if isinstance(d, (bytes, bytearray)):
                return bytes(d)
    # Last resort: parse from the string representation.
    s = str(exc)
    import re
    m = re.search(r"0x[0-9a-fA-F]{8,}", s)
    if m:
        return bytes.fromhex(m.group(0)[2:])
    return None


def _parse_profit(receipt):
    """Extract the profit from the ArbitrageExecuted event in a receipt."""
    # Event signature: ArbitrageExecuted(address,address,uint256,uint256,uint256)
    # topics[0] = keccak256 of the signature.
    import hashlib
    sig = "ArbitrageExecuted(address,address,uint256,uint256,uint256)"
    topic0 = "0x" + Web3.keccak(text=sig).hex()
    for log in receipt.logs:
        if log.topics and log.topics[0].hex() == topic0:
            # data = amountBorrowed (32) | profit (32) | swapCount (32)
            data = log.data
            if isinstance(data, str):
                data = bytes.fromhex(data.removeprefix("0x"))
            # profit is the second 32-byte word
            profit = int.from_bytes(data[32:64], "big")
            return profit
    return None
