package watcher

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Hailer367/Ryowatch/config"
	"github.com/Hailer367/Ryowatch/executor"
	"github.com/Hailer367/Ryowatch/pools"
	"github.com/Hailer367/Ryowatch/rpc"
	"github.com/Hailer367/Ryowatch/safety"
)

type Watcher struct {
	client  *rpc.Client
	safety  *safety.Net
	routes  []config.Route
	cache   *pools.BlockCache
	pools   []string
	scanMu  sync.Mutex
}

func New(client *rpc.Client, routes []config.Route) *Watcher {
	return &Watcher{client: client, safety: safety.New(), routes: routes, cache: pools.NewBlockCache()}
}

// ResolvePools discovers the on-chain pool addresses touched by the configured routes.
// Call this once a context is available (e.g. at startup) before subscribing to events.
func (w *Watcher) ResolvePools(ctx context.Context) {
	seen := make(map[string]bool)
	for _, r := range w.routes {
		for _, p := range pools.ResolveRoutePools(ctx, w.client, r) {
			if !seen[p] {
				seen[p] = true
				w.pools = append(w.pools, p)
			}
		}
	}
}

// ScanOnce runs one pass over all routes. Returns number of trades attempted.
func (w *Watcher) ScanOnce(ctx context.Context, blockNum uint64) int {
	attempted := 0

	// Block-level cache: if the block hasn't changed, skip all RPC calls and reuse cached results.
	if blockNum == 0 {
		if header, err := w.client.HeaderByNumber(ctx, nil); err == nil && header != nil {
			blockNum = header.Number.Uint64()
		}
	}
	if !w.cache.ShouldRequote(blockNum) {
		return 0
	}
	pools.ClearDirty()
	pools.PrefetchBlockData(ctx, w.client, w.routes, blockNum)

	for _, route := range w.routes {
		dec, derr := pools.TokenDecimals(ctx, w.client, route.Asset)
		if derr != nil {
			fmt.Printf("[%s] decimals error: %v\n", route.Name, derr)
			continue
		}

		// 1. Quote off-chain.
		_, profit, err := pools.EvaluateRoute(ctx, w.client, route, nil)
		if err != nil {
			fmt.Printf("[%s] quote error: %v\n", route.Name, err)
			continue
		}
		profitTokens := config.FromRaw(profit, dec)
		if profitTokens < route.MinProfit {
			continue
		}

		// Simulation gating: only simulate if off-chain profit exceeds minProfit by a margin.
		if profitTokens < route.MinProfit*config.SimMargin {
			fmt.Printf("[%s] off-chain profit too small (%.6f < %.6f * %.2f)\n", route.Name, profitTokens, route.MinProfit, config.SimMargin)
			continue
		}

		// 2. Safety nets (gas + trade size + circuit breaker). Rate limit is checked only
		//    before sending a transaction, so simulations never trip it.
		gasPrice, _ := w.client.SuggestGasPrice(ctx)
		gasGwei := gasPrice.Int64() / 1e9
		ok, reason := w.safety.CheckGas(gasGwei)
		if !ok {
			fmt.Printf("[%s] skipped: %s\n", route.Name, reason)
			continue
		}

		// Pool-depth sizing: cap the trade to a fraction of the shallowest pool.
		cap := config.MaxTradeSize
		if depthCapRaw, derr := pools.DepthCapRaw(ctx, w.client, route); derr == nil && depthCapRaw != nil {
			dec2, _ := pools.TokenDecimals(ctx, w.client, route.Asset)
			depthCapTokens := config.FromRaw(depthCapRaw, dec2)
			if depthCapTokens > 0 && depthCapTokens < cap {
				cap = depthCapTokens
			}
		}
		ok, reason = w.safety.CheckTradeSizeCap(route.Amount, cap)
		if !ok {
			fmt.Printf("[%s] skipped: %s (cap=%.6f)\n", route.Name, reason, cap)
			continue
		}
		ok, reason = w.safety.CheckCircuitBreaker()
		if !ok {
			fmt.Printf("[%s] skipped: %s\n", route.Name, reason)
			continue
		}

		// 3. Simulate on-chain.
		simProfit, err := executor.Simulate(ctx, w.client, route)
		if err != nil {
			fmt.Printf("[%s] simulate error: %v\n", route.Name, err)
			w.safety.RecordFailure()
			continue
		}
		if simProfit == nil || config.FromRaw(simProfit, dec) < route.MinProfit {
			fmt.Printf("[%s] simulated profit too low: %v\n", route.Name, simProfit)
			continue
		}

		// 4. Rate limit (only before sending a real transaction; dry runs are free).
		if !config.DryRun {
			ok, reason = w.safety.CheckRateLimit()
			if !ok {
				fmt.Printf("[%s] skipped: %s\n", route.Name, reason)
				continue
			}
		}

		// 5. Execute (or dry-run).
		attempted++
		if config.DryRun {
			fmt.Printf("[%s] DRY RUN: would execute (profit=%v)\n", route.Name, simProfit)
			w.safety.RecordSuccess()
			continue
		}
		txHash, profit2, err := executor.Execute(ctx, w.client, route)
		if err != nil {
			fmt.Printf("[%s] execute failed: %v\n", route.Name, err)
			w.safety.RecordFailure()
		} else {
			fmt.Printf("[%s] executed: tx=%s profit=%v\n", route.Name, txHash, profit2)
			w.safety.RecordSuccess()
		}
	}
	if attempted == 0 {
		fmt.Println("no profitable routes this round")
	}
	return attempted
}

// Run subscribes to new blocks via WebSocket and scans on each new block.
// Falls back to HTTP polling if the WebSocket is unavailable.
func (w *Watcher) Run(ctx context.Context) {
	w.ResolvePools(ctx)
	if len(w.pools) > 0 {
		fmt.Printf("watching %d pools for swap events\n", len(w.pools))
		watched := make([]*pools.WatchedPool, len(w.pools))
		for i, p := range w.pools {
			watched[i] = pools.NewWatchedPool(p)
		}
		go pools.WatchSwapEvents(ctx, w.client, watched)
		// Bridge WatchedPool dirty state into the global pools dirty flag.
		go func() {
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					for _, wp := range watched {
						if wp.IsDirty() {
							wp.ClearDirty()
							pools.MarkDirty(wp.Address)
						}
					}
				}
			}
		}()
	}

	// Try WebSocket subscription first.
	ch, sub, err := w.client.SubscribeNewHeads(ctx)
	if err != nil {
		fmt.Printf("WebSocket unavailable (%v); falling back to HTTP polling\n", err)
		w.runPolling(ctx)
		return
	}
	defer sub.Unsubscribe()
	fmt.Println("subscribed to new blocks via WebSocket")

	// Pre-warm + periodic stats logging.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := w.client.BlockNumber(ctx); err != nil {
					fmt.Printf("pre-warm ping failed: %v\n", err)
				}
				ws, http := w.client.Stats()
				fmt.Printf("stats: ws_calls=%d http_calls=%d\n", ws, http)
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case header, ok := <-ch:
			if !ok {
				fmt.Println("block channel closed; switching to polling")
				w.runPolling(ctx)
				return
			}
			fmt.Printf("new block %d\n", header.Number.Uint64())
			w.scanMu.Lock()
			start := time.Now()
			w.ScanOnce(ctx, header.Number.Uint64())
			fmt.Printf("round time: %v\n", time.Since(start))
			w.scanMu.Unlock()
		}
	}
}

func (w *Watcher) runPolling(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(config.PollIntervalSec * float64(time.Second)))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.scanMu.Lock()
			start := time.Now()
			w.ScanOnce(ctx, 0)
			fmt.Printf("round time: %v\n", time.Since(start))
			w.scanMu.Unlock()
		}
	}
}
