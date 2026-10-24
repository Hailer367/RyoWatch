package watcher

import (
	"testing"
	"time"

	"github.com/Hailer367/Ryowatch/config"
	"github.com/Hailer367/Ryowatch/pools"
)

func TestFallbackDueFirstRun(t *testing.T) {
	w := New(nil, nil)
	if !w.fallbackDue() {
		t.Fatal("first scan must not be skipped")
	}
}

func TestFallbackDueRecentScan(t *testing.T) {
	w := New(nil, nil)
	w.lastScan = time.Now()
	if w.fallbackDue() {
		t.Fatal("recent scan should not trigger fallback")
	}
}

func TestFallbackDueElapsed(t *testing.T) {
	old := config.ScanFallbackSeconds
	defer func() { config.ScanFallbackSeconds = old }()
	config.ScanFallbackSeconds = 1

	w := New(nil, nil)
	w.lastScan = time.Now().Add(-2 * time.Second)
	if !w.fallbackDue() {
		t.Fatal("fallback interval elapsed must force a scan")
	}
}

func TestCollectDirtyRoutes(t *testing.T) {
	p1 := pools.NewWatchedPool(pools.PoolInfo{Address: "0xpool1"})
	p2 := pools.NewWatchedPool(pools.PoolInfo{Address: "0xpool2"})
	w := New(nil, nil)
	w.watched = []*pools.WatchedPool{p1, p2}
	w.poolRoutes = map[string][]int{
		"0xpool1": {0, 2},
		"0xpool2": {1},
	}

	p1.SetDirty()
	got := w.collectDirtyRoutes()
	if len(got) != 2 || got[0] != 0 || got[1] != 2 {
		t.Fatalf("unexpected routes: %v", got)
	}
	if p1.IsDirty() {
		t.Fatal("dirty flag should be cleared after collection")
	}
	if got := w.collectDirtyRoutes(); got != nil {
		t.Fatalf("expected nil when nothing is dirty, got %v", got)
	}
}
