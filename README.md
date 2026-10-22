# Ryowatch

Off-chain watcher for the Ryo flash-loan arbitrage executor on Base. Ryowatch
scans for price gaps across Uniswap V3, Uniswap V2, Aerodrome (classic +
Slipstream) and calls Ryo to execute profitable routes.

Ryo (the smart contract) is the engine; Ryowatch is the driver. Ryo accepts
any trade size by default and relies on this bot's safety nets to stay out of
trouble. See Ryo's CAPABILITIES.md and WATCHER_INTERFACE.md for the contract
side.

## Two implementations

This repo ships **two** implementations of the same bot:

- **`go/`-style (root): the Go version** — the production bot. Fast, concurrent,
  WebSocket-driven. Use this for real trading.
- **`src/` (Python)** — the original reference implementation. Slower but
  easier to read and modify. Kept as a readable spec and for the test suite.

Both share the same `.env` format and the same on-chain logic.

## Setup (Go)

```bash
# Prerequisites: Go 1.24+, a Base RPC endpoint (Alchemy recommended)
cp .env.example .env
# Fill in RYO_ADDRESS, OPERATOR_ADDRESS, OPERATOR_PRIVATE_KEY, etc.

# Build
make build

# Run (dry run by default)
make run
```

## Setup (Python, reference)

```bash
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
cp .env.example .env
python run.py
```

## Configuration

Key `.env` variables:

- `BASE_RPC_URL` — Alchemy WebSocket URL recommended (`wss://base-mainnet.g.alchemy.com/v2/...`).
  Falls back to HTTP polling if WebSocket is unavailable.
- `RYO_ADDRESS` — deployed Ryo contract address.
- `OPERATOR_ADDRESS` / `OPERATOR_PRIVATE_KEY` — the bot's hot wallet.
- `MAX_TRADE_SIZE`, `MAX_GAS_PRICE_GWEI`, `MIN_PROFIT`, `SLIPPAGE_BPS` — safety limits.
- `CIRCUIT_BREAKER_FAILURES` — pause after this many consecutive failures.
- `POLL_INTERVAL_SECONDS` — polling interval when WebSocket is unavailable.
- `DRY_RUN` — `true` by default. Simulates only, never sends a transaction.

## How it stays fast and cheap

- **WebSocket primary, HTTP fallback.** Subscribes to `newHeads` on Alchemy; wakes the
  instant a block lands. Falls back to HTTP polling if the WebSocket drops.
- **Multicall3 batching.** All pool quotes for a block go through one `eth_call` to
  Multicall3 instead of N separate calls.
- **Parallel evaluation.** Routes are quoted concurrently.
- **Simulation gating.** Only calls `Ryo.simulate` when the off-chain quote already
  shows profit above threshold. Only executes when simulate confirms it.
- **Cost tracking.** Logs RPC calls per round so you can see where credits go.
- **Rate limiting.** Token bucket caps RPC calls per second.

## Safety nets

The bot is the primary safety net; the contract's limits are backstops. See Ryo's
CAPABILITIES.md section "Where the safety nets live" for the full list. The bot
implements: liquidity-aware sizing, simulate-first, gas check, route validation,
per-hop slippage, circuit breaker, balance check, rate limiting.

## Tests

```bash
# Python reference tests
pytest tests/test_safety.py tests/test_routes.py          # pure unit
BASE_RPC_URL=https://mainnet.base.org pytest tests/test_pools.py  # live quotes
# anvil fork tests: see tests/test_executor.py, tests/test_watcher.py

# Go build check
make build
```

## Project layout (Go)

```
main.go                  entry point
config/                  addresses, limits, routes, decimals
rpc/                     WebSocket + HTTP client, Multicall3
pools/                   on-chain quote helpers
routes/                  route -> Ryo Swap encoding
executor/                simulate + execute
safety/                  gas, size, rate-limit, circuit-breaker
watcher/                 main loop
```

## License

MIT.
EOF