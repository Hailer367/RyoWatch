# Ryowatch

Off-chain watcher for the Ryo flash-loan arbitrage executor on Base. Ryowatch
scans for price gaps across Uniswap V3, Uniswap V2, Aerodrome (classic +
Slipstream) and calls Ryo to execute profitable routes.

Ryo (the smart contract) is the engine; Ryowatch is the driver. Ryo accepts
any trade size by default and relies on this bot's safety nets (liquidity-aware
sizing, simulate-first, gas checks, circuit breaker, ...) to stay out of
trouble. See Ryo's CAPABILITIES.md and WATCHER_INTERFACE.md for the contract
side.

## Setup

```bash
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt

cp .env.example .env
# Fill in RYO_ADDRESS, OPERATOR_ADDRESS, OPERATOR_PRIVATE_KEY, etc.
```

## Run

```bash
# Dry run (default): simulates only, never sends a transaction
python run.py

# Live: set DRY_RUN=false in .env first
python run.py
```

## Tests

```bash
# Pure unit tests (no network)
pytest tests/test_safety.py tests/test_routes.py

# Integration tests against a live Base RPC
BASE_RPC_URL=https://mainnet.base.org pytest tests/test_pools.py

# End-to-end tests against a local Base fork (anvil)
# Terminal 1: anvil --fork-url https://mainnet.base.org --chain-id 8453
# Terminal 2: deploy Ryo, then:
BASE_RPC_URL=http://127.0.0.1:8545 pytest tests/test_executor.py tests/test_watcher.py
```

40 tests, all green.

## Safety

- `DRY_RUN=true` by default. Flip to `false` only after you've watched it
  simulate for a while.
- `MAX_TRADE_SIZE` is a hard ceiling on top of the bot's own liquidity-aware
  sizing.
- `CIRCUIT_BREAKER_FAILURES` pauses the bot after that many consecutive
  failures.
- The operator private key is read from the environment, never committed.

## Project layout

```
src/
  config.py     addresses, limits, default routes
  chains.py     web3 connection + contract instances
  pools.py      on-chain quote helpers (Uniswap V3/V2, Aerodrome, Slipstream)
  routes.py     route -> Ryo Swap encoding
  executor.py   simulate (eth_call) and execute (signed tx)
  safety.py     gas, size, rate-limit, circuit-breaker checks
  watcher.py    main loop
run.py          entry point
```

## License

MIT.
