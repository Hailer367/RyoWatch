package watcher

import (
	"context"
	"fmt"
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
}

func New(client *rpc.Client, routes []config.Route) *Watcher {
	return &Watcher{client: client, safety: safety.New(), routes: routes}
}

// ScanOnce runs one pass over all routes. Returns number of trades attempted.
func (w *Watcher) ScanOnce(ctx context.Context) int {
	attempted := 0
	for _, route := range w.routes {
		// 1. Quote off-chain.
		_, profit, err := pools.EvaluateRoute(ctx, w.client, route, nil)
		if err != nil {
			fmt.Printf("[%s] quote error: %v\n", route.Name, err)
			continue
		}
		profitTokens := config.FromRaw(profit, 18) // approximate; use route asset decimals in prod
		if profitTokens < route.MinProfit {
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
		ok, reason = w.safety.CheckTradeSize(route.Amount)
		if !ok {
			fmt.Printf("[%s] skipped: %s\n", route.Name, reason)
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
		if simProfit == nil || config.FromRaw(simProfit, 18) < route.MinProfit {
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
	// Try WebSocket subscription first.
	ch, sub, err := w.client.SubscribeNewHeads(ctx)
	if err != nil {
		fmt.Printf("WebSocket unavailable (%v); falling back to HTTP polling\n", err)
		w.runPolling(ctx)
		return
	}
	defer sub.Unsubscribe()
	fmt.Println("subscribed to new blocks via WebSocket")

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
			start := time.Now()
			w.ScanOnce(ctx)
			fmt.Printf("round time: %v\n", time.Since(start))
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
			start := time.Now()
			w.ScanOnce(ctx)
			fmt.Printf("round time: %v\n", time.Since(start))
		}
	}
}
