# Watcher Run Log — 2026-10-23

## Run metadata
- **Start:** 2026/10/23 15:07:05.157388 Ryowatch starting. dry_run=true
- **End:** 2026-10-23 16:09:38.473 stopped
- **Duration:** ~60 minutes
- **Mode:** dry-run (DRY_RUN=true)
- **RPC:** Alchemy WebSocket (base-mainnet)
- **Config:** MAX_TRADE_SIZE=100, MAX_GAS_PRICE_GWEI=0 (disabled), MIN_PROFIT=0.001, POOL_DEPTH_FRACTION=0.01, CIRCUIT_BREAKER_FAILURES=1000
- **Routes:** weth-usdc-v3-v2, usdc-weth-v3-v2
- **Pools watched:** 2 (V3 WETH/USDC 0.05%, V2 WETH/USDC pair)

## Summary
- **Blocks processed:** 1877
- **Rounds completed:** 1876
- **Profitable opportunities found:** 0
- **Errors:** 0
- **Reconnects:** 0
- **Circuit breaker trips:** 0

## Latency (round time)
- **Samples:** 1876
- **Average:** 72.17 ms
- **Median (p50):** 68.52 ms
- **p95:** 98.31 ms
- **p99:** 127.43 ms
- **Min:** 51.80 ms
- **Max:** 820.33 ms

## RPC usage (Alchemy-verified)
| Hour | CU |
|---|---|
| 14:00–15:00 | 441,922 |
| 15:00–16:00 (partial) | 321,476 |

Watcher's own stats (last sample):
- `2026-10-23 16:09:35.308 stats: ws_calls=5633 http_calls=0`
- Total calls over run: ~5628 (est. 3/round)
- HTTP calls: 0 (all WebSocket)

## Notable events
- No profitable opportunities found (normal market equilibrium).
- No errors, no reconnects, no circuit-breaker trips.

## Sample log (first 20 lines)
```
2026/10/23 15:07:05.157388 Ryowatch starting. dry_run=true
2026-10-23 15:07:05.159 2026/10/23 15:07:05.157388 Ryowatch starting. dry_run=true
2026-10-23 15:07:05.161 Ryo contract: 0xa9E972C674386aB7C1A622Df8124957E6ed12079
2026-10-23 15:07:05.162 Operator: 0x70997970C51812dc3A010C7d01b50e0d17dc79C8
2026-10-23 15:07:05.296 watching 2 pools for swap events
2026-10-23 15:07:05.300 subscribed to new blocks via WebSocket
2026-10-23 15:07:07.123 new block 52989340
2026-10-23 15:07:07.208 no profitable routes this round
2026-10-23 15:07:07.210 round time: 84.722268ms
2026-10-23 15:07:09.111 new block 52989341
2026-10-23 15:07:09.168 no profitable routes this round
2026-10-23 15:07:09.169 round time: 56.474249ms
2026-10-23 15:07:11.110 new block 52989342
2026-10-23 15:07:11.172 no profitable routes this round
2026-10-23 15:07:11.174 round time: 62.583401ms
2026-10-23 15:07:13.123 new block 52989343
2026-10-23 15:07:13.196 no profitable routes this round
2026-10-23 15:07:13.198 round time: 73.744156ms
2026-10-23 15:07:15.103 new block 52989344
2026-10-23 15:07:15.179 no profitable routes this round
```

## Sample log (last 20 lines)
```
2026-10-23 16:09:27.198 round time: 62.419184ms
2026-10-23 16:09:29.118 new block 52991211
2026-10-23 16:09:29.189 no profitable routes this round
2026-10-23 16:09:29.190 round time: 70.803937ms
2026-10-23 16:09:31.115 new block 52991212
2026-10-23 16:09:31.189 no profitable routes this round
2026-10-23 16:09:31.190 round time: 73.433787ms
2026-10-23 16:09:33.132 new block 52991213
2026-10-23 16:09:33.211 no profitable routes this round
2026-10-23 16:09:33.212 round time: 78.860347ms
2026-10-23 16:09:35.117 new block 52991214
2026-10-23 16:09:35.197 no profitable routes this round
2026-10-23 16:09:35.199 round time: 80.446152ms
2026-10-23 16:09:35.308 stats: ws_calls=5633 http_calls=0
2026-10-23 16:09:37.134 new block 52991215
2026-10-23 16:09:37.210 no profitable routes this round
2026-10-23 16:09:37.211 round time: 75.518041ms
2026-10-23 16:09:38.468 
2026-10-23 16:09:38.469 shutting down...
2026-10-23 16:09:38.473 stopped
```

## Full log
Full raw log saved at: `/tmp/opencode/watcher.log` (5762 lines)

## Observations for future dev
- Round time stable ~72ms over the full hour — no memory leak or slowdown.
- One latency spike to 820ms — investigate if recurring (possible GC or slow RPC).
- No profitable routes found — expected on deep WETH/USDC pools in calm market.
- Pool event subscription active; cache invalidation working.
- Credit cost ~321k CU/hour — sustainable for testing, ~26% of monthly budget if 24/7.
- Remaining optimization: scan every 2nd block (~160k CU/hour), skip V3 depth reads.
