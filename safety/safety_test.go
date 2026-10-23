package safety

import (
	"testing"
	"time"

	"github.com/Hailer367/Ryowatch/config"
)

func TestGasCheck(t *testing.T) {
	sn := New()
	old := config.MaxGasPriceGwei
	defer func() { config.MaxGasPriceGwei = old }()

	config.MaxGasPriceGwei = 50
	if ok, _ := sn.CheckGas(10); !ok {
		t.Fatal("expected pass")
	}
	config.MaxGasPriceGwei = 5
	if ok, _ := sn.CheckGas(10); ok {
		t.Fatal("expected fail")
	}
	config.MaxGasPriceGwei = 0
	if ok, _ := sn.CheckGas(999); !ok {
		t.Fatal("expected pass when disabled")
	}
}

func TestTradeSizeCap(t *testing.T) {
	sn := New()
	if ok, _ := sn.CheckTradeSizeCap(0.5, 1.0); !ok {
		t.Fatal("expected pass")
	}
	if ok, _ := sn.CheckTradeSizeCap(2.0, 1.0); ok {
		t.Fatal("expected fail")
	}
	if ok, _ := sn.CheckTradeSizeCap(999.0, 0); !ok {
		t.Fatal("expected pass when cap disabled")
	}
}

func TestTradeSize(t *testing.T) {
	sn := New()
	old := config.MaxTradeSize
	defer func() { config.MaxTradeSize = old }()

	config.MaxTradeSize = 0.5
	if ok, _ := sn.CheckTradeSize(0.1); !ok {
		t.Fatal("expected pass")
	}
	if ok, _ := sn.CheckTradeSize(1.0); ok {
		t.Fatal("expected fail")
	}
	config.MaxTradeSize = 0
	if ok, _ := sn.CheckTradeSize(999); !ok {
		t.Fatal("expected pass when disabled")
	}
}

func TestCircuitBreaker(t *testing.T) {
	sn := New()
	old := config.CircuitBreakerN
	defer func() { config.CircuitBreakerN = old }()
	config.CircuitBreakerN = 2

	sn.RecordFailure()
	sn.RecordFailure()
	if ok, _ := sn.CheckCircuitBreaker(); ok {
		t.Fatal("expected open")
	}
	sn.RecordSuccess()
	if ok, _ := sn.CheckCircuitBreaker(); !ok {
		t.Fatal("expected closed")
	}
	if sn.consecutiveFailures != 0 {
		t.Fatal("expected reset")
	}
}

func TestRateLimit(t *testing.T) {
	sn := New()
	if ok, _ := sn.CheckRateLimit(); !ok {
		t.Fatal("expected ok initially")
	}
	sn.lastTradeTime = time.Now()
	if ok, _ := sn.CheckRateLimit(); ok {
		t.Fatal("expected blocked")
	}
}

func TestRecordTimestamp(t *testing.T) {
	sn := New()
	before := sn.lastTradeTime
	time.Sleep(10 * time.Millisecond)
	sn.RecordSuccess()
	if !sn.lastTradeTime.After(before) {
		t.Fatal("expected timestamp update")
	}
}
