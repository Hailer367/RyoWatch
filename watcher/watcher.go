package watcher

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Hailer367/Ryowatch/config"
	"github.com/Hailer367/Ryowatch/executor"
	"github.com/Hailer367/Ryowatch/pools"
	"github.com/Hailer367/Ryowatch/rpc"
	"github.com/Hailer367/Ryowatch/safety"
)

type Watcher struct {
	client     *rpc.Client
	safety     *safety.Net
	routes     []config.Route
	cache      *pools.BlockCache
	pools      []string
	watched    []*pools.WatchedPool
	poolRoutes map[string][]int
	scanMu     sync.Mutex
	lastScan   time.Time
}

func New(client *rpc.Client, routes []config.Route) *Watcher {
	return &Watcher{client: client, safety: safety.New(), routes: routes, cache: pools.NewBlockCache()}
}

// ResolvePools discovers the on-chain pools touched by the configured routes and
// builds a pool -> route-index map for route-scoped scans.
// Call this once a context is available (e.g. at startup) before subscribing to events.
func (w *Watcher) ResolvePools(ctx context.Context) {
	seen := make(map[string]bool)
	routeSets := make(map[string]map[int]bool)
	for ri, r := range w.routes {
		for _, info := range pools.ResolveRoutePoolInfos(ctx, w.client, r) {
			if !seen[info.Address] {
				seen[info.Address] = true
				w.pools = append(w.pools, info.Address)
				w.watched = append(w.watched, pools.NewWatchedPool(info))
			}
			if routeSets[info.Address] == nil {
				routeSets[info.Address] = make(map[int]bool)
			}
			routeSets[info.Address][ri] = true
		}
	}
	w.poolRoutes = make(map[string][]int, len(routeSets))
	for addr, set := range routeSets {
		for ri := range set {
			w.poolRoutes[addr] = append(w.poolRoutes[addr], ri)
		}
	}
}

// ScanOnce runs one pass over all routes. Returns number of trades attempted.
func (w *Watcher) ScanOnce(ctx context.Context, blockNum uint64) int {
	return w.scanRoutes(ctx, blockNum, nil)
}

// scanRoutes runs one pass over the selected routes. A nil routeIndices means
// "all routes". Non-nil indices are used for route-scoped event-driven scans.
func (w *Watcher) scanRoutes(ctx context.Context, blockNum uint64, routeIndices []int) int {
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

	selected := w.routes
	if routeIndices != nil {
		selected = make([]config.Route, 0, len(routeIndices))
		for _, i := range routeIndices {
			if i >= 0 && i < len(w.routes) {
				selected = append(selected, w.routes[i])
			}
		}
	}

	pools.ClearDirty()
	pools.PrefetchBlockData(ctx, w.client, selected, blockNum)
	w.warmReserves(ctx, selected)

	for _, route := range selected {
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

// warmReserves populates the last-known-reserve map used by the event size
// filter. It normally hits the caches filled by PrefetchBlockData, so it adds
// little or no RPC cost.
func (w *Watcher) warmReserves(ctx context.Context, routes []config.Route) {
	for _, route := range routes {
		for _, hop := range route.Hops {
			_, _, _ = pools.PoolReserves(ctx, w.client, hop)
		}
	}
}

// Run subscribes to new blocks via WebSocket and scans on each new block.
// Falls back to HTTP polling if the WebSocket is unavailable.
func (w *Watcher) Run(ctx context.Context) {
	w.ResolvePools(ctx)
	if len(w.watched) > 0 {
		fmt.Printf("watching %d pools for swap events\n", len(w.watched))
		go pools.WatchSwapEvents(ctx, w.client, w.watched)
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
				seen, ignored := pools.EventStats()
				fmt.Printf("stats: ws_calls=%d http_calls=%d swaps_seen=%d swaps_ignored=%d\n", ws, http, seen, ignored)
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
			block := header.Number.Uint64()
			// Only scan when a watched pool actually traded, or when the fallback
			// interval has elapsed. This avoids paying for a full quote/simulate
			// pass on every quiet block.
			dirtyRoutes := w.collectDirtyRoutes()
			fallback := w.fallbackDue()
			if len(dirtyRoutes) == 0 && !fallback {
				fmt.Printf("new block %d (quiet; skipped)\n", block)
				continue
			}
			if fallback {
				dirtyRoutes = nil // nil = all routes
				fmt.Printf("new block %d (fallback full scan)\n", block)
			} else {
				fmt.Printf("new block %d (scanning %d affected route(s))\n", block, len(dirtyRoutes))
			}
			w.scanMu.Lock()
			start := time.Now()
			w.scanRoutes(ctx, block, dirtyRoutes)
			w.lastScan = time.Now()
			fmt.Printf("round time: %v\n", time.Since(start))
			w.scanMu.Unlock()
		}
	}
}

// fallbackDue reports whether a full scan should run because the fallback
// interval has elapsed (or no scan has happened yet).
func (w *Watcher) fallbackDue() bool {
	if w.lastScan.IsZero() {
		return true
	}
	fallback := time.Duration(config.ScanFallbackSeconds * float64(time.Second))
	return time.Since(w.lastScan) >= fallback
}

// collectDirtyRoutes snapshots the dirty pools, clears their flags, and returns
// the sorted route indices affected by those pools. Events that arrive during
// the subsequent scan set the flags again for the next block.
func (w *Watcher) collectDirtyRoutes() []int {
	set := make(map[int]bool)
	any := false
	for _, wp := range w.watched {
		if !wp.IsDirty() {
			continue
		}
		any = true
		wp.ClearDirty()
		for _, ri := range w.poolRoutes[wp.Address] {
			set[ri] = true
		}
	}
	if !any {
		return nil
	}
	out := make([]int, 0, len(set))
	for ri := range set {
		out = append(out, ri)
	}
	sort.Ints(out)
	return out
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
			w.lastScan = time.Now()
			fmt.Printf("round time: %v\n", time.Since(start))
			w.scanMu.Unlock()
		}
	}
}
