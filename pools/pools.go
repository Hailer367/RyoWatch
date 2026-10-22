package pools

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/Hailer367/Ryowatch/config"
	"github.com/Hailer367/Ryowatch/rpc"
)

// ---- ABIs ----
const (
	erc20ABI = `[{"name":"decimals","type":"function","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"uint8"}]}]`

	univ2RouterABI = `[{"name":"getAmountsOut","type":"function","stateMutability":"view","inputs":[{"name":"amountIn","type":"uint256"},{"name":"path","type":"address[]"}],"outputs":[{"name":"amounts","type":"uint256[]"}]}]`

	univ3QuoterABI = `[{"name":"quoteExactInputSingle","type":"function","stateMutability":"nonpayable","inputs":[{"name":"params","type":"tuple","components":[{"name":"tokenIn","type":"address"},{"name":"tokenOut","type":"address"},{"name":"amountIn","type":"uint256"},{"name":"fee","type":"uint24"},{"name":"sqrtPriceLimitX96","type":"uint160"}]}],"outputs":[{"name":"amountOut","type":"uint256"},{"name":"sqrtPriceX96After","type":"uint160"},{"name":"initializedTicksCrossed","type":"uint32"},{"name":"gasEstimate","type":"uint256"}]}]`

	aerodromeRouterABI = `[{"name":"getAmountsOut","type":"function","stateMutability":"view","inputs":[{"name":"amountIn","type":"uint256"},{"name":"routes","type":"tuple[]","components":[{"name":"from","type":"address"},{"name":"to","type":"address"},{"name":"stable","type":"bool"},{"name":"factory","type":"address"}]}],"outputs":[{"name":"amounts","type":"uint256[]"}]}]`

	slipstreamQuoterABI = `[{"name":"quoteExactInputSingle","type":"function","stateMutability":"nonpayable","inputs":[{"name":"params","type":"tuple","components":[{"name":"tokenIn","type":"address"},{"name":"tokenOut","type":"address"},{"name":"amountIn","type":"uint256"},{"name":"tickSpacing","type":"int24"},{"name":"sqrtPriceLimitX96","type":"uint160"}]}],"outputs":[{"name":"amountOut","type":"uint256"},{"name":"sqrtPriceX96After","type":"uint160"},{"name":"initializedTicksCrossed","type":"uint32"},{"name":"gasEstimate","type":"uint256"}]}]`
)

var (
	erc20ABIparsed     abi.ABI
	univ2RouterParsed  abi.ABI
	univ3QuoterParsed  abi.ABI
	aerodromeParsed    abi.ABI
	slipstreamParsed   abi.ABI
)

func init() {
	var err error
	erc20ABIparsed, err = abi.JSON(strings.NewReader(erc20ABI))
	if err != nil {
		panic(err)
	}
	univ2RouterParsed, err = abi.JSON(strings.NewReader(univ2RouterABI))
	if err != nil {
		panic(err)
	}
	univ3QuoterParsed, err = abi.JSON(strings.NewReader(univ3QuoterABI))
	if err != nil {
		panic(err)
	}
	aerodromeParsed, err = abi.JSON(strings.NewReader(aerodromeRouterABI))
	if err != nil {
		panic(err)
	}
	slipstreamParsed, err = abi.JSON(strings.NewReader(slipstreamQuoterABI))
	if err != nil {
		panic(err)
	}
}

// decimals cache
var decimalsCache = make(map[string]int)

func TokenDecimals(ctx context.Context, client *rpc.Client, token string) (int, error) {
	if d, ok := decimalsCache[token]; ok {
		return d, nil
	}
	addr := common.HexToAddress(token)
	data, err := erc20ABIparsed.Pack("decimals")
	if err != nil {
		return 0, err
	}
	raw, err := client.Call(ctx, addr, data)
	if err != nil {
		return 0, err
	}
	out, err := erc20ABIparsed.Unpack("decimals", raw)
	if err != nil {
		return 0, err
	}
	d := int(out[0].(uint8))
	decimalsCache[token] = d
	return d, nil
}

// QuoteHop returns the output amount (raw) for a single hop.
func QuoteHop(ctx context.Context, client *rpc.Client, hop config.Hop, amountIn *big.Int) (*big.Int, error) {
	switch hop.Dex {
	case "v3":
		data, err := univ3QuoterParsed.Pack("quoteExactInputSingle", struct {
			TokenIn           common.Address `abi:"tokenIn"`
			TokenOut          common.Address `abi:"tokenOut"`
			AmountIn          *big.Int       `abi:"amountIn"`
			Fee               *big.Int       `abi:"fee"`
			SqrtPriceLimitX96 *big.Int       `abi:"sqrtPriceLimitX96"`
		}{
			TokenIn:           common.HexToAddress(hop.TokenIn),
			TokenOut:          common.HexToAddress(hop.TokenOut),
			AmountIn:          amountIn,
			Fee:               big.NewInt(int64(hop.Fee)),
			SqrtPriceLimitX96: big.NewInt(0),
		})
		if err != nil {
			return nil, err
		}
		to := common.HexToAddress(config.UniswapV3Quoter)
		raw, err := client.Call(ctx, to, data)
		if err != nil {
			return nil, err
		}
		out, err := univ3QuoterParsed.Unpack("quoteExactInputSingle", raw)
		if err != nil {
			return nil, err
		}
		return out[0].(*big.Int), nil

	case "v2":
		path := []common.Address{common.HexToAddress(hop.TokenIn), common.HexToAddress(hop.TokenOut)}
		data, err := univ2RouterParsed.Pack("getAmountsOut", amountIn, path)
		if err != nil {
			return nil, err
		}
		to := common.HexToAddress(hop.Router)
		raw, err := client.Call(ctx, to, data)
		if err != nil {
			return nil, err
		}
		out, err := univ2RouterParsed.Unpack("getAmountsOut", raw)
		if err != nil {
			return nil, err
		}
		amounts := out[0].([]*big.Int)
		if len(amounts) == 0 {
			return nil, fmt.Errorf("empty amounts")
		}
		return amounts[len(amounts)-1], nil

	case "aerodrome":
		type route struct {
			From     common.Address `abi:"from"`
			To       common.Address `abi:"to"`
			Stable   bool           `abi:"stable"`
			Factory  common.Address `abi:"factory"`
		}
		routes := []route{{From: common.HexToAddress(hop.TokenIn), To: common.HexToAddress(hop.TokenOut), Stable: hop.Stable, Factory: common.HexToAddress("0x0000000000000000000000000000000000000000")}}
		data, err := aerodromeParsed.Pack("getAmountsOut", amountIn, routes)
		if err != nil {
			return nil, err
		}
		to := common.HexToAddress(hop.Router)
		raw, err := client.Call(ctx, to, data)
		if err != nil {
			return nil, err
		}
		out, err := aerodromeParsed.Unpack("getAmountsOut", raw)
		if err != nil {
			return nil, err
		}
		amounts := out[0].([]*big.Int)
		if len(amounts) == 0 {
			return nil, fmt.Errorf("empty amounts")
		}
		return amounts[len(amounts)-1], nil

	case "slipstream":
		data, err := slipstreamParsed.Pack("quoteExactInputSingle", struct {
			TokenIn           common.Address `abi:"tokenIn"`
			TokenOut          common.Address `abi:"tokenOut"`
			AmountIn          *big.Int       `abi:"amountIn"`
			TickSpacing       *big.Int       `abi:"tickSpacing"`
			SqrtPriceLimitX96 *big.Int       `abi:"sqrtPriceLimitX96"`
		}{
			TokenIn:           common.HexToAddress(hop.TokenIn),
			TokenOut:          common.HexToAddress(hop.TokenOut),
			AmountIn:          amountIn,
			TickSpacing:       big.NewInt(int64(hop.TickSpacing)),
			SqrtPriceLimitX96: big.NewInt(0),
		})
		if err != nil {
			return nil, err
		}
		to := common.HexToAddress(config.SlipstreamQuoter)
		raw, err := client.Call(ctx, to, data)
		if err != nil {
			return nil, err
		}
		out, err := slipstreamParsed.Unpack("quoteExactInputSingle", raw)
		if err != nil {
			return nil, err
		}
		return out[0].(*big.Int), nil
	}
	return nil, fmt.Errorf("unknown dex %s", hop.Dex)
}

// EvaluateRoute walks a route hop by hop and returns (finalAmount, profit) or error.
func EvaluateRoute(ctx context.Context, client *rpc.Client, route config.Route, amountOverride *big.Int) (*big.Int, *big.Int, error) {
	amountIn := amountOverride
	if amountIn == nil {
		dec, err := TokenDecimals(ctx, client, route.Asset)
		if err != nil {
			return nil, nil, err
		}
		amountIn = config.ToRaw(route.Amount, dec)
	}

	cur := new(big.Int).Set(amountIn)
	for _, hop := range route.Hops {
		out, err := QuoteHop(ctx, client, hop, cur)
		if err != nil {
			return nil, nil, err
		}
		if out == nil || out.Sign() <= 0 {
			return nil, nil, fmt.Errorf("zero output on hop %s->%s", hop.TokenIn, hop.TokenOut)
		}
		cur = out
	}

	profit := new(big.Int).Sub(cur, amountIn)
	return cur, profit, nil
}

// EvaluateAllRoutes evaluates all routes. Returns a slice of (route, final, profit, err).
type RouteResult struct {
	Route  config.Route
	Final  *big.Int
	Profit *big.Int
	Err    error
}

func EvaluateAllRoutes(ctx context.Context, client *rpc.Client, routes []config.Route) []RouteResult {
	res := make([]RouteResult, len(routes))
	for i, r := range routes {
		final, profit, err := EvaluateRoute(ctx, client, r, nil)
		res[i] = RouteResult{Route: r, Final: final, Profit: profit, Err: err}
	}
	return res
}
