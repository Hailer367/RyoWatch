package config

import "math/big"

// DexType returns the Ryo dexType id for a hop's dex string.
func DexType(dex string) uint8 {
	switch dex {
	case "v3":
		return DexV3
	case "v2":
		return DexV2
	case "aerodrome":
		return DexAerodrome
	case "slipstream":
		return DexSlipstream
	}
	return 0
}

// SlippageFloor applies a bps slippage floor to a raw amount.
func SlippageFloor(amount *big.Int, bps int64) *big.Int {
	return new(big.Int).Div(
		new(big.Int).Mul(amount, big.NewInt(10000-bps)),
		big.NewInt(10000),
	)
}
