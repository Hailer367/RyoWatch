package routes

import (
	"math/big"
	"testing"
)

func FuzzSlippageFloor(f *testing.F) {
	f.Add(int64(1000), int64(50))
	f.Fuzz(func(t *testing.T, amount int64, bps int64) {
		if amount < 0 || bps < 0 || bps > 10000 {
			return
		}
		raw := new(big.Int).SetInt64(amount)
		if raw.Sign() <= 0 {
			return
		}
		floored := SlippageFloor(raw, bps)
		if floored.Cmp(raw) > 0 {
			t.Fatal("floor must not exceed raw")
		}
		if bps == 0 && floored.Cmp(raw) != 0 {
			t.Fatal("zero bps should return same")
		}
	})
}
