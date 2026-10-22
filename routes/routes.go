package routes

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/Hailer367/Ryowatch/config"
)

// SwapTuple mirrors Ryo's Swap struct for ABI encoding.
type SwapTuple struct {
	DexType  uint8
	Router   common.Address
	TokenIn  common.Address
	TokenOut common.Address
	Extra    []byte
}

// ToSwaps converts a config.Route into the list of Swap tuples Ryo.execute expects.
func ToSwaps(route config.Route) ([]SwapTuple, error) {
	out := make([]SwapTuple, 0, len(route.Hops))
	for _, h := range route.Hops {
		extra, err := extraData(h)
		if err != nil {
			return nil, err
		}
		out = append(out, SwapTuple{
			DexType:  config.DexType(h.Dex),
			Router:   common.HexToAddress(h.Router),
			TokenIn:  common.HexToAddress(h.TokenIn),
			TokenOut: common.HexToAddress(h.TokenOut),
			Extra:    extra,
		})
	}
	return out, nil
}

func mustType(t string) abi.Type {
	ty, err := abi.NewType(t, "", nil)
	if err != nil {
		panic(err)
	}
	return ty
}

func extraData(h config.Hop) ([]byte, error) {
	switch h.Dex {
	case "v3":
		if h.MinOut > 1 {
			args := abi.Arguments{{Type: mustType("uint24")}, {Type: mustType("uint256")}}
			return args.Pack(big.NewInt(int64(h.Fee)), big.NewInt(int64(h.MinOut)))
		}
		args := abi.Arguments{{Type: mustType("uint24")}}
		return args.Pack(big.NewInt(int64(h.Fee)))
	case "v2":
		if h.MinOut > 1 {
			path := []common.Address{common.HexToAddress(h.TokenIn), common.HexToAddress(h.TokenOut)}
			args := abi.Arguments{{Type: mustType("address[]")}, {Type: mustType("uint256")}}
			return args.Pack(path, big.NewInt(int64(h.MinOut)))
		}
		return []byte{}, nil
	case "aerodrome":
		path := []common.Address{common.HexToAddress(h.TokenIn), common.HexToAddress(h.TokenOut)}
		stables := []bool{h.Stable}
		mo := h.MinOut
		if mo == 0 {
			mo = 1
		}
		args := abi.Arguments{{Type: mustType("address[]")}, {Type: mustType("bool[]")}, {Type: mustType("uint256")}}
		return args.Pack(path, stables, big.NewInt(int64(mo)))
	case "slipstream":
		if h.MinOut > 1 {
			args := abi.Arguments{{Type: mustType("int24")}, {Type: mustType("uint256")}}
			return args.Pack(big.NewInt(int64(h.TickSpacing)), big.NewInt(int64(h.MinOut)))
		}
		args := abi.Arguments{{Type: mustType("int24")}}
		return args.Pack(big.NewInt(int64(h.TickSpacing)))
	}
	return nil, fmt.Errorf("unknown dex %s", h.Dex)
}

// SlippageFloor applies a bps slippage floor to a raw amount.
func SlippageFloor(amount *big.Int, bps int64) *big.Int {
	return new(big.Int).Div(
		new(big.Int).Mul(amount, big.NewInt(10000-bps)),
		big.NewInt(10000),
	)
}
