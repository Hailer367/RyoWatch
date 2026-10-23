package routes

import (
	"math/big"
	"testing"

	"github.com/Hailer367/Ryowatch/config"
)

func TestDexTypeMapping(t *testing.T) {
	if config.DexV3 != 6 || config.DexV2 != 8 || config.DexAerodrome != 4 || config.DexSlipstream != 7 {
		t.Fatal("dex type mismatch")
	}
}

func TestExtraDataV3FeeOnly(t *testing.T) {
	h := config.Hop{Dex: "v3", Fee: 500, MinOut: 1}
	data, err := extraData(h)
	if err != nil || len(data) != 32 {
		t.Fatalf("v3 fee-only: len=%d err=%v", len(data), err)
	}
}

func TestExtraDataV3WithFloor(t *testing.T) {
	h := config.Hop{Dex: "v3", Fee: 500, MinOut: 1000}
	data, err := extraData(h)
	if err != nil || len(data) != 64 {
		t.Fatalf("v3 floor: len=%d err=%v", len(data), err)
	}
}

func TestExtraDataV2Direct(t *testing.T) {
	h := config.Hop{Dex: "v2", MinOut: 1}
	data, err := extraData(h)
	if err != nil || len(data) != 0 {
		t.Fatalf("v2 direct: len=%d err=%v", len(data), err)
	}
}

func TestExtraDataV2WithPath(t *testing.T) {
	h := config.Hop{Dex: "v2", MinOut: 1000}
	data, err := extraData(h)
	if err != nil || len(data) == 0 {
		t.Fatalf("v2 path: len=%d err=%v", len(data), err)
	}
}

func TestExtraDataAerodrome(t *testing.T) {
	h := config.Hop{Dex: "aerodrome", Stable: true, MinOut: 1}
	data, err := extraData(h)
	if err != nil || len(data) < 256 {
		t.Fatalf("aero: len=%d err=%v", len(data), err)
	}
}

func TestExtraDataSlipstream(t *testing.T) {
	h := config.Hop{Dex: "slipstream", TickSpacing: 10, MinOut: 1}
	data, err := extraData(h)
	if err != nil || len(data) != 32 {
		t.Fatalf("slip: len=%d err=%v", len(data), err)
	}
	h2 := config.Hop{Dex: "slipstream", TickSpacing: 1, MinOut: 500}
	data2, err := extraData(h2)
	if err != nil || len(data2) != 64 {
		t.Fatalf("slip floor: len=%d err=%v", len(data2), err)
	}
}

func TestExtraDataUnknown(t *testing.T) {
	h := config.Hop{Dex: "curve"}
	if _, err := extraData(h); err == nil {
		t.Fatal("expected error")
	}
}

func TestToSwapsStructure(t *testing.T) {
	route := config.DefaultRoutes[0]
	swaps, err := ToSwaps(route)
	if err != nil || len(swaps) != 2 {
		t.Fatalf("swaps: %d err=%v", len(swaps), err)
	}
	if swaps[0].DexType != 6 || swaps[1].DexType != 8 {
		t.Fatalf("dex types: %d %d", swaps[0].DexType, swaps[1].DexType)
	}
}

func TestSlippageFloor(t *testing.T) {
	raw := new(big.Int)
	raw.SetString("1000000000000000000", 10)
	floored := SlippageFloor(raw, 50)
	if floored.Cmp(raw) >= 0 {
		t.Fatal("expected floor < raw")
	}
}
