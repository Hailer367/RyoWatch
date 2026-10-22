# Ryowatch - watcher.py
# Main loop: scan routes, simulate, and execute the profitable ones.

import time
from . import config
from . import chains
from . import pools
from . import routes as R
from . import executor
from . import safety

safety_net = safety.SafetyNet()


def scan_once():
    """One pass over all routes. Returns the number of trades attempted."""
    attempted = 0
    for route in config.DEFAULT_ROUTES:
        try:
            # 1. Quote the route off-chain.
            final_amount, profit_raw = pools.evaluate_route(route)
            if final_amount is None:
                continue

            profit_tokens = profit_raw / 1e18
            if profit_tokens < route.min_profit:
                continue

            # 2. Safety nets.
            ok, reason = safety_net.all_clear(chains.w3, route.amount, route.asset)
            if not ok:
                print(f"[{route.name}] skipped: {reason}")
                continue

            # 3. Simulate on-chain (catches things the off-chain quote missed).
            sim_profit, err = executor.simulate(route)
            if err:
                print(f"[{route.name}] simulate error: {err}")
                safety_net.record_failure()
                continue
            if sim_profit is None or sim_profit < int(route.min_profit * 1e18):
                print(f"[{route.name}] simulated profit too low: {sim_profit}")
                continue

            # 4. Execute (or dry-run).
            attempted += 1
            tx_hash, result = executor.execute(route)
            if tx_hash is None:
                print(f"[{route.name}] execute failed: {result}")
                safety_net.record_failure()
            else:
                print(f"[{route.name}] executed: tx={tx_hash.hex()} profit={result}")
                safety_net.record_success()
        except Exception as e:
            print(f"[{route.name}] unexpected error: {e}")
            safety_net.record_failure()
    return attempted


def run():
    print(f"Ryowatch starting. dry_run={config.DRY_RUN}")
    print(f"Ryo contract: {config.RYO_ADDRESS or '(not set)'}")
    print(f"Operator: {config.OPERATOR_ADDRESS or '(not set)'}")
    print(f"Poll interval: {config.POLL_INTERVAL_SECONDS}s")
    while True:
        try:
            n = scan_once()
            if n == 0:
                print("no profitable routes this round")
        except KeyboardInterrupt:
            print("stopping")
            break
        except Exception as e:
            print(f"round error: {e}")
        time.sleep(config.POLL_INTERVAL_SECONDS)
