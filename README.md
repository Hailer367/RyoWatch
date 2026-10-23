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

### 1. Prerequisites
- Go 1.24 or newer (`go version`)
- A Base RPC endpoint — **Alchemy WebSocket recommended** (e.g. `wss://base-mainnet.g.alchemy.com/v2/YOUR_KEY`)
- A deployed Ryo contract address (`RYO_ADDRESS`) and an operator keypair

### 2. Configure
```bash
cp .env.example .env
```
Edit `.env` and fill in at minimum:
- `BASE_RPC_URL` — your Alchemy WebSocket or HTTP URL
- `RYO_ADDRESS` — deployed Ryo contract address
- `OPERATOR_ADDRESS` / `OPERATOR_PRIVATE_KEY` — the bot's hot wallet (must be the operator on the contract)
- `DRY_RUN=true` (default) — keep this true until you're ready to trade real funds

Optional safety knobs (defaults are conservative):
- `MAX_TRADE_SIZE` (default 1.0 token)
- `MAX_GAS_PRICE_GWEI` (default 50)
- `MIN_PROFIT` (default 0.001)
- `POOL_DEPTH_FRACTION` (default 0.01 = 1% of shallowest pool)
- `CIRCUIT_BREAKER_FAILURES` (default 3)

### 3. Build
```bash
make build
```

### 4. Run (dry run by default)
```bash
make run
# or directly:
./ryowatch
```

### 5. Single scan (useful for testing)
```bash
./ryowatch --once
```

### 6. Verify it's working
You should see:
```
Ryowatch starting. dry_run=true
Ryo contract: 0x...
Operator: 0x...
watching 2 pools for swap events
subscribed to new blocks via WebSocket
new block 12345678
no profitable routes this round
round time: 70ms
```

### Troubleshooting
- **"WebSocket connect timed out"** — your RPC URL may not support WebSocket; the bot falls back to HTTP polling automatically.
- **"RYO_ADDRESS not set"** — set `RYO_ADDRESS` in `.env`.
- **"OPERATOR_ADDRESS required when OPERATOR_PRIVATE_KEY is set"** — fill in both.
- **No profitable routes** — normal on a calm market; the bot only trades when a price gap exists.
- **Pool event re-scans** — the bot reacts to pool trades within ~250ms; this is expected.


## Setup (Python, reference)

### 1. Prerequisites
- Python 3.10+ and pip
- A Base RPC endpoint

### 2. Configure
```bash
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
cp .env.example .env
# Edit .env with your RYO_ADDRESS, OPERATOR_ADDRESS, OPERATOR_PRIVATE_KEY, etc.
```

### 3. Run
```bash
python run.py
```

### 4. Run tests
```bash
# Pure unit tests (no RPC needed)
pytest tests/test_routes.py tests/test_safety.py

# Live RPC tests (requires BASE_RPC_URL)
BASE_RPC_URL=https://mainnet.base.org pytest tests/test_pools.py
```

> The Python version is the readable reference implementation. The Go version is the production bot.


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