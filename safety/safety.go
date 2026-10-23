package safety

import (
	"fmt"
	"sync"
	"time"

	"github.com/Hailer367/Ryowatch/config"
)

type Net struct {
	mu                   sync.Mutex
	consecutiveFailures   int
	lastTradeTime        time.Time
	minTradeInterval     time.Duration
}

func New() *Net {
	return &Net{minTradeInterval: time.Second}
}

func (n *Net) CheckGas(gasPriceGwei int64) (bool, string) {
	if config.MaxGasPriceGwei > 0 && gasPriceGwei > config.MaxGasPriceGwei {
		return false, fmt.Sprintf("gas too high: %d > %d gwei", gasPriceGwei, config.MaxGasPriceGwei)
	}
	return true, "ok"
}

func (n *Net) CheckTradeSize(amount float64) (bool, string) {
	if config.MaxTradeSize > 0 && amount > config.MaxTradeSize {
		return false, fmt.Sprintf("trade too large: %f > %f", amount, config.MaxTradeSize)
	}
	return true, "ok"
}

// CheckTradeSizeCap is like CheckTradeSize but uses an explicit cap (e.g. pool-depth based)
// instead of the global config.MaxTradeSize.
func (n *Net) CheckTradeSizeCap(amount, cap float64) (bool, string) {
	if cap > 0 && amount > cap {
		return false, fmt.Sprintf("trade too large: %f > %f", amount, cap)
	}
	return true, "ok"
}

func (n *Net) CheckRateLimit() (bool, string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if time.Since(n.lastTradeTime) < n.minTradeInterval {
		return false, "rate limited"
	}
	return true, "ok"
}

func (n *Net) CheckCircuitBreaker() (bool, string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.consecutiveFailures >= config.CircuitBreakerN {
		return false, fmt.Sprintf("circuit breaker open (%d failures)", n.consecutiveFailures)
	}
	return true, "ok"
}

func (n *Net) RecordSuccess() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.consecutiveFailures = 0
	n.lastTradeTime = time.Now()
}

func (n *Net) RecordFailure() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.consecutiveFailures++
	n.lastTradeTime = time.Now()
}

func (n *Net) AllClear(gasPriceGwei int64, amount float64) (bool, string) {
	for _, c := range []func() (bool, string){
		func() (bool, string) { return n.CheckGas(gasPriceGwei) },
		func() (bool, string) { return n.CheckTradeSize(amount) },
		func() (bool, string) { return n.CheckRateLimit() },
		func() (bool, string) { return n.CheckCircuitBreaker() },
	} {
		ok, reason := c()
		if !ok {
			return false, reason
		}
	}
	return true, "ok"
}
