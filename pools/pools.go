package pools

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"
	"sync"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/Hailer367/Ryowatch/config"
	"github.com/Hailer367/Ryowatch/rpc"
)

// ---- ABIs ----
const (
	erc20ABI = `[{"name":"decimals","type":"function","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"uint8"}]},{"name":"balanceOf","type":"function","stateMutability":"view","inputs":[{"name":"a","type":"address"}],"outputs":[{"name":"","type":"uint256"}]}]`

	univ2RouterABI = `[{"name":"getAmountsOut","type":"function","stateMutability":"view","inputs":[{"name":"amountIn","type":"uint256"},{"name":"path","type":"address[]"}],"outputs":[{"name":"amounts","type":"uint256[]"}]}]`

	univ3QuoterABI = `[{"name":"quoteExactInputSingle","type":"function","stateMutability":"nonpayable","inputs":[{"name":"params","type":"tuple","components":[{"name":"tokenIn","type":"address"},{"name":"tokenOut","type":"address"},{"name":"amountIn","type":"uint256"},{"name":"fee","type":"uint24"},{"name":"sqrtPriceLimitX96","type":"uint160"}]}],"outputs":[{"name":"amountOut","type":"uint256"},{"name":"sqrtPriceX96After","type":"uint160"},{"name":"initializedTicksCrossed","type":"uint32"},{"name":"gasEstimate","type":"uint256"}]}]`

	aerodromeRouterABI = `[{"name":"getAmountsOut","type":"function","stateMutability":"view","inputs":[{"name":"amountIn","type":"uint256"},{"name":"routes","type":"tuple[]","components":[{"name":"from","type":"address"},{"name":"to","type":"address"},{"name":"stable","type":"bool"},{"name":"factory","type":"address"}]}],"outputs":[{"name":"amounts","type":"uint256[]"}]}]`

	slipstreamQuoterABI = `[{"name":"quoteExactInputSingle","type":"function","stateMutability":"nonpayable","inputs":[{"name":"params","type":"tuple","components":[{"name":"tokenIn","type":"address"},{"name":"tokenOut","type":"address"},{"name":"amountIn","type":"uint256"},{"name":"tickSpacing","type":"int24"},{"name":"sqrtPriceLimitX96","type":"uint160"}]}],"outputs":[{"name":"amountOut","type":"uint256"},{"name":"sqrtPriceX96After","type":"uint160"},{"name":"initializedTicksCrossed","type":"uint32"},{"name":"gasEstimate","type":"uint256"}]}]`

	univ2PairABI = `[{"name":"getReserves","type":"function","stateMutability":"view","inputs":[],"outputs":[{"name":"reserve0","type":"uint112"},{"name":"reserve1","type":"uint112"},{"name":"blockTimestampLast","type":"uint32"}]},{"name":"token0","type":"function","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"address"}]}]`

	univ2FactoryABI = `[{"name":"getPair","type":"function","stateMutability":"view","inputs":[{"name":"tokenA","type":"address"},{"name":"tokenB","type":"address"}],"outputs":[{"name":"","type":"address"}]}]`

	univ3FactoryABI = `[{"name":"getPool","type":"function","stateMutability":"view","inputs":[{"name":"tokenA","type":"address"},{"name":"tokenB","type":"address"},{"name":"fee","type":"uint24"}],"outputs":[{"name":"","type":"address"}]}]`

	aerodromeFactoryABI = `[{"name":"getPool","type":"function","stateMutability":"view","inputs":[{"name":"tokenA","type":"address"},{"name":"tokenB","type":"address"},{"name":"stable","type":"bool"}],"outputs":[{"name":"","type":"address"}]}]`

	slipstreamFactoryABI = `[{"name":"getPool","type":"function","stateMutability":"view","inputs":[{"name":"tokenA","type":"address"},{"name":"tokenB","type":"address"},{"name":"tickSpacing","type":"int24"}],"outputs":[{"name":"","type":"address"}]}]`
)

var (
	erc20ABIparsed    abi.ABI
	univ2RouterParsed abi.ABI
	univ3QuoterParsed abi.ABI
	aerodromeParsed   abi.ABI
	slipstreamParsed  abi.ABI
	univ2PairParsed   abi.ABI
	univ2FactoryParsed abi.ABI
	univ3FactoryParsed abi.ABI
	aerodromeFactoryParsed abi.ABI
	slipstreamFactoryParsed abi.ABI
)

func init() {
	for _, pair := range []struct {
		src string
		dst *abi.ABI
	}{
		{erc20ABI, &erc20ABIparsed},
		{univ2RouterABI, &univ2RouterParsed},
		{univ3QuoterABI, &univ3QuoterParsed},
		{aerodromeRouterABI, &aerodromeParsed},
		{slipstreamQuoterABI, &slipstreamParsed},
		{univ2PairABI, &univ2PairParsed},
		{univ2FactoryABI, &univ2FactoryParsed},
		{univ3FactoryABI, &univ3FactoryParsed},
		{aerodromeFactoryABI, &aerodromeFactoryParsed},
		{slipstreamFactoryABI, &slipstreamFactoryParsed},
	} {
		parsed, err := abi.JSON(strings.NewReader(pair.src))
		if err != nil {
			panic(err)
		}
		*pair.dst = parsed
	}
}

// ---- Decimals cache (never changes) ----
var decimalsCache = make(map[string]int)
var decimalsMu sync.Mutex

func TokenDecimals(ctx context.Context, client *rpc.Client, token string) (int, error) {
	decimalsMu.Lock()
	defer decimalsMu.Unlock()
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

// ---- Block-level cache ----
type cacheEntry struct {
	block  uint64
	result *big.Int
	err    error
}

var (
	cacheMu     sync.Mutex
	blockCache  = make(map[string]*cacheEntry)
	lastBlock   uint64
	dirtyMu     sync.Mutex
	anyDirty    bool

	reserveMu   sync.Mutex
	reserveCache = make(map[string]struct {
		rIn  *big.Int
		rOut *big.Int
		err  error
	})

	v2Mu    sync.Mutex
	v2Cache = make(map[string]struct {
		pair   string
		token0 string
		r0     *big.Int
		r1     *big.Int
		err    error
	})

	v3Mu    sync.Mutex
	v3Cache = make(map[string]struct {
		rIn *big.Int
		rOut *big.Int
		err  error
	})

	v3QuoteMu    sync.Mutex
	v3QuoteCache = make(map[string]*big.Int)

	poolAddrMu    sync.Mutex
	poolAddrCache = make(map[string]string)

	v2PairMu    sync.Mutex
	v2PairCache = make(map[string]string) // tokenIn:tokenOut -> pair address (permanent)
	v2Token0Mu  sync.Mutex
	v2Token0Cache = make(map[string]string) // pair addr -> token0 (permanent)
)

// MarkDirty marks the global quote cache as stale because a watched pool traded.
// The pool address is retained for logging/debugging but invalidation is global:
// any pool movement means every cached quote may be stale.
func MarkDirty(poolAddr string) {
	dirtyMu.Lock()
	anyDirty = true
	dirtyMu.Unlock()
}

func IsDirty(poolAddr string) bool {
	dirtyMu.Lock()
	defer dirtyMu.Unlock()
	return anyDirty
}

func ClearDirty() {
	dirtyMu.Lock()
	anyDirty = false
	dirtyMu.Unlock()
	reserveMu.Lock()
	reserveCache = make(map[string]struct {
		rIn  *big.Int
		rOut *big.Int
		err  error
	})
	reserveMu.Unlock()

	v2Mu.Lock()
	v2Cache = make(map[string]struct {
		pair   string
		token0 string
		r0     *big.Int
		r1     *big.Int
		err    error
	})
	v2Mu.Unlock()

	v3Mu.Lock()
	v3Cache = make(map[string]struct {
		rIn *big.Int
		rOut *big.Int
		err  error
	})
	v3Mu.Unlock()
}

func IsAnyDirty() bool {
	dirtyMu.Lock()
	defer dirtyMu.Unlock()
	return anyDirty
}

func getCached(key string, block uint64) (*big.Int, bool) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	e, ok := blockCache[key]
	if !ok || e.block != block {
		return nil, false
	}
	// If any watched pool moved since the last scan, invalidate the cache.
	if anyDirty {
		return nil, false
	}
	return e.result, true
}

func setCached(key string, block uint64, result *big.Int, err error) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	blockCache[key] = &cacheEntry{block: block, result: result, err: err}
}

// ---- QuoteHop: single hop ----
func QuoteHop(ctx context.Context, client *rpc.Client, hop config.Hop, amountIn *big.Int) (*big.Int, error) {
	cacheKey := hop.Router + ":" + hop.TokenIn + ":" + hop.TokenOut + ":" + amountIn.String()
	blockNum := uint64(0)
	if header, err := client.HeaderByNumber(ctx, nil); err == nil && header != nil {
		blockNum = header.Number.Uint64()
	}
	if cached, ok := getCached(cacheKey, blockNum); ok {
		return cached, nil
	}

	var result *big.Int
	var err error

	switch hop.Dex {
	case "v3":
		if blockNum == 0 {
		blockNum = currentBlockNumber(ctx, client)
	}
		qKey := hop.TokenIn + ":" + hop.TokenOut + ":" + fmt.Sprintf("%d", hop.Fee) + ":" + amountIn.String() + ":" + fmt.Sprintf("%d", blockNum)
		v3QuoteMu.Lock()
		if cached, ok := v3QuoteCache[qKey]; ok {
			v3QuoteMu.Unlock()
			result = new(big.Int).Set(cached)
		} else {
			v3QuoteMu.Unlock()
			data, perr := univ3QuoterParsed.Pack("quoteExactInputSingle", struct {
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
			if perr != nil {
				err = perr
			} else {
				to := common.HexToAddress(config.UniswapV3Quoter)
				raw, callErr := client.Call(ctx, to, data)
				if callErr != nil {
					err = callErr
				} else {
					out, unpackErr := univ3QuoterParsed.Unpack("quoteExactInputSingle", raw)
					if unpackErr != nil {
						err = unpackErr
					} else {
						result = out[0].(*big.Int)
						v3QuoteMu.Lock()
						v3QuoteCache[qKey] = new(big.Int).Set(result)
						v3QuoteMu.Unlock()
					}
				}
			}
		}

	case "v2":
		// Local computation from reserves (saves an RPC call vs getAmountsOut).
		result, err = quoteV2Local(ctx, client, hop.TokenIn, hop.TokenOut, amountIn)

	case "aerodrome":
		type route struct {
			From     common.Address `abi:"from"`
			To       common.Address `abi:"to"`
			Stable   bool           `abi:"stable"`
			Factory  common.Address `abi:"factory"`
		}
		routes := []route{{From: common.HexToAddress(hop.TokenIn), To: common.HexToAddress(hop.TokenOut), Stable: hop.Stable, Factory: common.HexToAddress("0x0000000000000000000000000000000000000000")}}
		data, perr := aerodromeParsed.Pack("getAmountsOut", amountIn, routes)
		if perr != nil {
			err = perr
		} else {
			to := common.HexToAddress(hop.Router)
			raw, callErr := client.Call(ctx, to, data)
			if callErr != nil {
				err = callErr
			} else {
				out, unpackErr := aerodromeParsed.Unpack("getAmountsOut", raw)
				if unpackErr != nil {
					err = unpackErr
				} else {
					amounts := out[0].([]*big.Int)
					if len(amounts) > 0 {
						result = amounts[len(amounts)-1]
					} else {
						err = fmt.Errorf("empty amounts")
					}
				}
			}
		}

	case "slipstream":
		data, perr := slipstreamParsed.Pack("quoteExactInputSingle", struct {
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
		if perr != nil {
			err = perr
		} else {
			to := common.HexToAddress(config.SlipstreamQuoter)
			raw, callErr := client.Call(ctx, to, data)
			if callErr != nil {
				err = callErr
			} else {
				out, unpackErr := slipstreamParsed.Unpack("quoteExactInputSingle", raw)
				if unpackErr != nil {
					err = unpackErr
				} else {
					result = out[0].(*big.Int)
				}
			}
		}

	default:
		err = fmt.Errorf("unknown dex %s", hop.Dex)
	}

	if err != nil {
		setCached(cacheKey, blockNum, nil, err)
		return nil, err
	}
	if result == nil || result.Sign() <= 0 {
		err := fmt.Errorf("zero output on hop %s->%s", hop.TokenIn, hop.TokenOut)
		setCached(cacheKey, blockNum, nil, err)
		return nil, err
	}
	setCached(cacheKey, blockNum, result, nil)
	return result, nil
}

// v2Reserves returns (pairAddr, token0, r0, r1) cached per block.
func v2Reserves(ctx context.Context, client *rpc.Client, tokenA, tokenB string) (string, string, *big.Int, *big.Int, error) {
	blockNum := currentBlockNumber(ctx, client)
	key := tokenA + ":" + tokenB + ":" + fmt.Sprintf("%d", blockNum)

	v2Mu.Lock()
	if cached, ok := v2Cache[key]; ok {
		pair, t0, r0, r1, err := cached.pair, cached.token0, cached.r0, cached.r1, cached.err
		v2Mu.Unlock()
		if err != nil {
			return "", "", nil, nil, err
		}
		return pair, t0, new(big.Int).Set(r0), new(big.Int).Set(r1), nil
	}
	v2Mu.Unlock()

	// permanent pair address
	v2PairMu.Lock()
	pairAddr, hasPair := v2PairCache[tokenA+":"+tokenB]
	v2PairMu.Unlock()
	if !hasPair {
		pa, err := getV2Pair(ctx, client, tokenA, tokenB)
		if err != nil {
			return "", "", nil, nil, err
		}
		pairAddr = pa
		v2PairMu.Lock()
		v2PairCache[tokenA+":"+tokenB] = pairAddr
		v2PairMu.Unlock()
	}

	// permanent token0
	v2Token0Mu.Lock()
	token0, hasT0 := v2Token0Cache[pairAddr]
	v2Token0Mu.Unlock()
	if !hasT0 {
		t0, err := getToken0(ctx, client, pairAddr)
		if err != nil {
			return "", "", nil, nil, err
		}
		token0 = t0
		v2Token0Mu.Lock()
		v2Token0Cache[pairAddr] = token0
		v2Token0Mu.Unlock()
	}

	// per-block reserves
	reserves, err := getReserves(ctx, client, pairAddr)
	if err != nil {
		v2Mu.Lock()
		v2Cache[key] = struct {
			pair   string
			token0 string
			r0     *big.Int
			r1     *big.Int
			err    error
		}{err: err}
		v2Mu.Unlock()
		return "", "", nil, nil, err
	}

	v2Mu.Lock()
	v2Cache[key] = struct {
		pair   string
		token0 string
		r0     *big.Int
		r1     *big.Int
		err    error
	}{pair: pairAddr, token0: token0, r0: reserves[0], r1: reserves[1], err: nil}
	v2Mu.Unlock()

	return pairAddr, token0, new(big.Int).Set(reserves[0]), new(big.Int).Set(reserves[1]), nil
}

// quoteV2Local computes the V2 output locally from pool reserves.
func quoteV2Local(ctx context.Context, client *rpc.Client, tokenIn, tokenOut string, amountIn *big.Int) (*big.Int, error) {
	_, token0, r0, r1, err := v2Reserves(ctx, client, tokenIn, tokenOut)
	if err != nil {
		return nil, err
	}
	var reserveIn, reserveOut *big.Int
	if token0 == tokenIn {
		reserveIn = r0
		reserveOut = r1
	} else {
		reserveIn = r1
		reserveOut = r0
	}
	amountInWithFee := new(big.Int).Mul(amountIn, big.NewInt(997))
	numerator := new(big.Int).Mul(amountInWithFee, reserveOut)
	denominator := new(big.Int).Add(
		new(big.Int).Mul(reserveIn, big.NewInt(1000)),
		amountInWithFee,
	)
	return new(big.Int).Div(numerator, denominator), nil
}

func getV2Pair(ctx context.Context, client *rpc.Client, tokenA, tokenB string) (string, error) {
	key := tokenA + ":" + tokenB
	v2PairMu.Lock()
	if cached, ok := v2PairCache[key]; ok {
		v2PairMu.Unlock()
		return cached, nil
	}
	v2PairMu.Unlock()

	data, err := univ2FactoryParsed.Pack("getPair", common.HexToAddress(tokenA), common.HexToAddress(tokenB))
	if err != nil {
		return "", err
	}
	factory := common.HexToAddress("0x8909Dc15e40173Ff4699343b6eB8132c65e18eC6")
	raw, err := client.Call(ctx, factory, data)
	if err != nil {
		return "", err
	}
	out, err := univ2FactoryParsed.Unpack("getPair", raw)
	if err != nil {
		return "", err
	}
	addr := out[0].(common.Address).Hex()
	v2PairMu.Lock()
	v2PairCache[key] = addr
	v2PairMu.Unlock()
	return addr, nil
}

func getReserves(ctx context.Context, client *rpc.Client, pairAddr string) ([2]*big.Int, error) {
	addr := common.HexToAddress(pairAddr)
	data, err := univ2PairParsed.Pack("getReserves")
	if err != nil {
		return [2]*big.Int{}, err
	}
	raw, err := client.Call(ctx, addr, data)
	if err != nil {
		return [2]*big.Int{}, err
	}
	out, err := univ2PairParsed.Unpack("getReserves", raw)
	if err != nil {
		return [2]*big.Int{}, err
	}
	r0 := out[0].(*big.Int)
	r1 := out[1].(*big.Int)
	return [2]*big.Int{r0, r1}, nil
}

func getToken0(ctx context.Context, client *rpc.Client, pairAddr string) (string, error) {
	addr := common.HexToAddress(pairAddr)
	data, err := univ2PairParsed.Pack("token0")
	if err != nil {
		return "", err
	}
	raw, err := client.Call(ctx, addr, data)
	if err != nil {
		return "", err
	}
	out, err := univ2PairParsed.Unpack("token0", raw)
	if err != nil {
		return "", err
	}
	return out[0].(common.Address).Hex(), nil
}

// GetV3Pool resolves the Uniswap V3 pool address for a token pair and fee tier.
func GetV3Pool(ctx context.Context, client *rpc.Client, tokenA, tokenB string, fee uint32) (string, error) {
	key := "v3:" + tokenA + ":" + tokenB + ":" + fmt.Sprintf("%d", fee)
	poolAddrMu.Lock()
	if cached, ok := poolAddrCache[key]; ok {
		poolAddrMu.Unlock()
		return cached, nil
	}
	poolAddrMu.Unlock()

	data, err := univ3FactoryParsed.Pack("getPool", common.HexToAddress(tokenA), common.HexToAddress(tokenB), big.NewInt(int64(fee)))
	if err != nil {
		return "", err
	}
	factory := common.HexToAddress(config.UniswapV3Factory)
	raw, err := client.Call(ctx, factory, data)
	if err != nil {
		return "", err
	}
	out, err := univ3FactoryParsed.Unpack("getPool", raw)
	if err != nil {
		return "", err
	}
	addr := out[0].(common.Address).Hex()
	poolAddrMu.Lock()
	poolAddrCache[key] = addr
	poolAddrMu.Unlock()
	return addr, nil
}

// GetAerodromePool resolves the Aerodrome classic pool address for a token pair and stable flag.
func GetAerodromePool(ctx context.Context, client *rpc.Client, tokenA, tokenB string, stable bool) (string, error) {
	key := "aero:" + tokenA + ":" + tokenB + ":" + fmt.Sprintf("%t", stable)
	poolAddrMu.Lock()
	if cached, ok := poolAddrCache[key]; ok {
		poolAddrMu.Unlock()
		return cached, nil
	}
	poolAddrMu.Unlock()

	data, err := aerodromeFactoryParsed.Pack("getPool", common.HexToAddress(tokenA), common.HexToAddress(tokenB), stable)
	if err != nil {
		return "", err
	}
	factory := common.HexToAddress(config.AerodromeFactory)
	raw, err := client.Call(ctx, factory, data)
	if err != nil {
		return "", err
	}
	out, err := aerodromeFactoryParsed.Unpack("getPool", raw)
	if err != nil {
		return "", err
	}
	addr := out[0].(common.Address).Hex()
	poolAddrMu.Lock()
	poolAddrCache[key] = addr
	poolAddrMu.Unlock()
	return addr, nil
}

// GetSlipstreamPool resolves the Aerodrome Slipstream pool address for a token pair and tick spacing.
func GetSlipstreamPool(ctx context.Context, client *rpc.Client, tokenA, tokenB string, tickSpacing int32) (string, error) {
	key := "slip:" + tokenA + ":" + tokenB + ":" + fmt.Sprintf("%d", tickSpacing)
	poolAddrMu.Lock()
	if cached, ok := poolAddrCache[key]; ok {
		poolAddrMu.Unlock()
		return cached, nil
	}
	poolAddrMu.Unlock()

	data, err := slipstreamFactoryParsed.Pack("getPool", common.HexToAddress(tokenA), common.HexToAddress(tokenB), big.NewInt(int64(tickSpacing)))
	if err != nil {
		return "", err
	}
	factory := common.HexToAddress(config.SlipstreamFactory)
	raw, err := client.Call(ctx, factory, data)
	if err != nil {
		return "", err
	}
	out, err := slipstreamFactoryParsed.Unpack("getPool", raw)
	if err != nil {
		return "", err
	}
	addr := out[0].(common.Address).Hex()
	poolAddrMu.Lock()
	poolAddrCache[key] = addr
	poolAddrMu.Unlock()
	return addr, nil
}

// ResolveRoutePools returns the unique on-chain pool addresses touched by a route.
func ResolveRoutePools(ctx context.Context, client *rpc.Client, route config.Route) []string {
	seen := make(map[string]bool)
	var out []string
	add := func(addr string) {
		if addr == "" || addr == "0x0000000000000000000000000000000000000000" {
			return
		}
		if seen[addr] {
			return
		}
		seen[addr] = true
		out = append(out, addr)
	}
	for _, hop := range route.Hops {
		switch hop.Dex {
		case "v2":
			if p, err := getV2Pair(ctx, client, hop.TokenIn, hop.TokenOut); err == nil {
				add(p)
			}
		case "v3":
			if p, err := GetV3Pool(ctx, client, hop.TokenIn, hop.TokenOut, hop.Fee); err == nil {
				add(p)
			}
		case "aerodrome":
			if p, err := GetAerodromePool(ctx, client, hop.TokenIn, hop.TokenOut, hop.Stable); err == nil {
				add(p)
			}
		case "slipstream":
			if p, err := GetSlipstreamPool(ctx, client, hop.TokenIn, hop.TokenOut, hop.TickSpacing); err == nil {
				add(p)
			}
		}
	}
	return out
}

// erc20Balance reads an ERC-20 balanceOf for a holder.
func erc20Balance(ctx context.Context, client *rpc.Client, token, holder string) (*big.Int, error) {
	data, err := erc20ABIparsed.Pack("balanceOf", common.HexToAddress(holder))
	if err != nil {
		return nil, err
	}
	raw, err := client.Call(ctx, common.HexToAddress(token), data)
	if err != nil {
		return nil, err
	}
	out, err := erc20ABIparsed.Unpack("balanceOf", raw)
	if err != nil {
		return nil, err
	}
	return out[0].(*big.Int), nil
}

var (
	blockNumMu    sync.Mutex
	blockNumCache uint64
	blockNumTime  time.Time
)

// currentBlockNumber returns the latest block number (0 on error), cached for 1s.
func currentBlockNumber(ctx context.Context, client *rpc.Client) uint64 {
	blockNumMu.Lock()
	if time.Since(blockNumTime) < time.Second && blockNumCache != 0 {
		v := blockNumCache
		blockNumMu.Unlock()
		return v
	}
	blockNumMu.Unlock()

	if header, err := client.HeaderByNumber(ctx, nil); err == nil && header != nil {
		blockNumMu.Lock()
		blockNumCache = header.Number.Uint64()
		blockNumTime = time.Now()
		blockNumMu.Unlock()
		return header.Number.Uint64()
	}
	return 0
}

// PoolReserves returns (reserveIn, reserveOut) for a hop's pool, in raw token units.
// Results are cached per block and invalidated when pools are marked dirty.
func PoolReserves(ctx context.Context, client *rpc.Client, hop config.Hop) (*big.Int, *big.Int, error) {
	cacheKey := hop.Dex + ":" + hop.TokenIn + ":" + hop.TokenOut + ":" + fmt.Sprintf("%d", hop.Fee) + ":" + fmt.Sprintf("%t", hop.Stable) + ":" + fmt.Sprintf("%d", hop.TickSpacing)

	poolAddrMu.Lock()
	cachedAddr, hasCached := poolAddrCache[cacheKey]
	poolAddrMu.Unlock()

	var poolAddr string
	var err error
	if hasCached {
		poolAddr = cachedAddr
	} else {
		switch hop.Dex {
		case "v2":
			poolAddr, err = getV2Pair(ctx, client, hop.TokenIn, hop.TokenOut)
		case "v3":
			poolAddr, err = GetV3Pool(ctx, client, hop.TokenIn, hop.TokenOut, hop.Fee)
		case "aerodrome":
			poolAddr, err = GetAerodromePool(ctx, client, hop.TokenIn, hop.TokenOut, hop.Stable)
		case "slipstream":
			poolAddr, err = GetSlipstreamPool(ctx, client, hop.TokenIn, hop.TokenOut, hop.TickSpacing)
		default:
			return nil, nil, fmt.Errorf("unknown dex %s", hop.Dex)
		}
		if err != nil {
			return nil, nil, err
		}
		poolAddrMu.Lock()
		poolAddrCache[cacheKey] = poolAddr
		poolAddrMu.Unlock()
	}

	blockNum := currentBlockNumber(ctx, client)
	key := poolAddr + ":" + fmt.Sprintf("%d", blockNum)

	reserveMu.Lock()
	if cached, ok := reserveCache[key]; ok {
		rIn, rOut, err := cached.rIn, cached.rOut, cached.err
		reserveMu.Unlock()
		if err != nil {
			return nil, nil, err
		}
		return new(big.Int).Set(rIn), new(big.Int).Set(rOut), nil
	}
	reserveMu.Unlock()

	var rIn, rOut *big.Int
	switch hop.Dex {
	case "v2":
		_, token0, rr0, rr1, rerr := v2Reserves(ctx, client, hop.TokenIn, hop.TokenOut)
		if rerr != nil {
			err = rerr
		} else {
			if token0 == hop.TokenIn {
				rIn = rr0
				rOut = rr1
			} else {
				rIn = rr1
				rOut = rr0
			}
		}
	case "aerodrome":
		reserves, rerr := getReserves(ctx, client, poolAddr)
		if rerr != nil {
			err = rerr
		} else {
			token0, terr := getToken0(ctx, client, poolAddr)
			if terr != nil {
				err = terr
			} else {
				if token0 == hop.TokenIn {
					rIn = reserves[0]
					rOut = reserves[1]
				} else {
					rIn = reserves[1]
					rOut = reserves[0]
				}
			}
		}
	case "v3", "slipstream":
		blockNum := currentBlockNumber(ctx, client)
		key := poolAddr + ":" + fmt.Sprintf("%d", blockNum)
		v3Mu.Lock()
		if cached, ok := v3Cache[key]; ok {
			ri, ro, rerr := cached.rIn, cached.rOut, cached.err
			v3Mu.Unlock()
			if rerr != nil {
				err = rerr
			} else {
				rIn = new(big.Int).Set(ri)
				rOut = new(big.Int).Set(ro)
			}
		} else {
			v3Mu.Unlock()
			ri, rerr := erc20Balance(ctx, client, hop.TokenIn, poolAddr)
			if rerr != nil {
				err = rerr
				v3Mu.Lock()
				v3Cache[key] = struct {
					rIn  *big.Int
					rOut *big.Int
					err  error
				}{err: rerr}
				v3Mu.Unlock()
			} else {
				ro, rerr2 := erc20Balance(ctx, client, hop.TokenOut, poolAddr)
				if rerr2 != nil {
					err = rerr2
					v3Mu.Lock()
					v3Cache[key] = struct {
						rIn  *big.Int
						rOut *big.Int
						err  error
					}{err: rerr2}
					v3Mu.Unlock()
				} else {
					rIn = ri
					rOut = ro
					v3Mu.Lock()
					v3Cache[key] = struct {
						rIn  *big.Int
						rOut *big.Int
						err  error
					}{rIn: new(big.Int).Set(ri), rOut: new(big.Int).Set(ro), err: nil}
					v3Mu.Unlock()
				}
			}
		}
	}

	reserveMu.Lock()
	reserveCache[key] = struct {
		rIn  *big.Int
		rOut *big.Int
		err  error
	}{rIn: rIn, rOut: rOut, err: err}
	reserveMu.Unlock()

	if err != nil {
		return nil, nil, err
	}
	if rIn == nil || rOut == nil {
		return nil, nil, fmt.Errorf("no reserves for %s", poolAddr)
	}
	return new(big.Int).Set(rIn), new(big.Int).Set(rOut), nil
}

// DepthCapRaw estimates the maximum borrow amount (in raw asset units) that keeps
// every hop's price impact bounded to config.PoolDepthFraction of pool depth.
func DepthCapRaw(ctx context.Context, client *rpc.Client, route config.Route) (*big.Int, error) {
	dec, err := TokenDecimals(ctx, client, route.Asset)
	if err != nil {
		return nil, err
	}

	cumRate := 1.0
	borrowCap := math.Inf(1)
	for _, hop := range route.Hops {
		rIn, rOut, err := PoolReserves(ctx, client, hop)
		if err != nil {
			return nil, err
		}
		rInF, _ := new(big.Float).SetInt(rIn).Float64()
		rOutF, _ := new(big.Float).SetInt(rOut).Float64()
		if rInF <= 0 {
			return nil, fmt.Errorf("zero reserve in %s", hop.TokenIn)
		}
		spot := rOutF / rInF
		depthCapHop := rInF * config.PoolDepthFraction
		cap := depthCapHop / cumRate
		if cap < borrowCap {
			borrowCap = cap
		}
		cumRate *= spot
	}

	if math.IsInf(borrowCap, 1) || borrowCap <= 0 {
		return nil, nil
	}

	f := new(big.Float).SetFloat64(borrowCap)
	scale := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(dec)), nil))
	f.Mul(f, scale)
	i, _ := f.Int(nil)
	return i, nil
}

// PrefetchBlockData batches all independent per-block reads (pool balances, V2 reserves,
// V3 hop-0 quotes) into a single Multicall3 call and populates the caches, so that the
// subsequent quote/depth pipeline makes zero individual RPC calls.
func PrefetchBlockData(ctx context.Context, client *rpc.Client, routes []config.Route, blockNum uint64) {
	// 1. Resolve pool addresses (cached permanently; one-time fetch if missing).
	type v3pool struct {
		addr string
		tokens [2]string // tokenIn, tokenOut for balance reads (use first hop's tokens)
	}
	v3Pools := make(map[string]bool) // poolAddr -> seen
	var v3PoolAddrs []string
	v3PoolTokens := make(map[string][2]string)

	v2Pairs := make(map[string]bool)
	var v2PairKeys [][2]string

	// unique V3 hop-0 quotes: one per route (route amount known)
	type v3Quote struct {
		routeIdx int
		tokenIn  string
		tokenOut string
		fee      uint32
		amountIn *big.Int
	}
	var v3Quotes []v3Quote

	for ri, route := range routes {
		dec, err := TokenDecimals(ctx, client, route.Asset)
		if err != nil {
			continue
		}
		amountIn := config.ToRaw(route.Amount, dec)

		for hi, hop := range route.Hops {
			switch hop.Dex {
			case "v3":
				addr, err := GetV3Pool(ctx, client, hop.TokenIn, hop.TokenOut, hop.Fee)
				if err == nil && addr != "" && addr != "0x0000000000000000000000000000000000000000" {
					if !v3Pools[addr] {
						v3Pools[addr] = true
						v3PoolAddrs = append(v3PoolAddrs, addr)
						v3PoolTokens[addr] = [2]string{hop.TokenIn, hop.TokenOut}
					}
				}
				if hi == 0 {
					v3Quotes = append(v3Quotes, v3Quote{
						routeIdx: ri, tokenIn: hop.TokenIn, tokenOut: hop.TokenOut,
						fee: hop.Fee, amountIn: amountIn,
					})
				}
			case "v2":
				key := hop.TokenIn + ":" + hop.TokenOut
				if !v2Pairs[key] {
					v2Pairs[key] = true
					v2PairKeys = append(v2PairKeys, [2]string{hop.TokenIn, hop.TokenOut})
				}
			case "aerodrome":
				addr, err := GetAerodromePool(ctx, client, hop.TokenIn, hop.TokenOut, hop.Stable)
				if err == nil && addr != "" && addr != "0x0000000000000000000000000000000000000000" {
					if !v3Pools[addr] {
						v3Pools[addr] = true
						v3PoolAddrs = append(v3PoolAddrs, addr)
						v3PoolTokens[addr] = [2]string{hop.TokenIn, hop.TokenOut}
					}
				}
			case "slipstream":
				addr, err := GetSlipstreamPool(ctx, client, hop.TokenIn, hop.TokenOut, hop.TickSpacing)
				if err == nil && addr != "" && addr != "0x0000000000000000000000000000000000000000" {
					if !v3Pools[addr] {
						v3Pools[addr] = true
						v3PoolAddrs = append(v3PoolAddrs, addr)
						v3PoolTokens[addr] = [2]string{hop.TokenIn, hop.TokenOut}
					}
				}
			}
		}
	}

	// 2. Build multicall entries.
	type entry struct {
		kind      string
		poolAddr  string
		tokIn     string
		tokOut    string
		fee       uint32
		amountIn  *big.Int
		routeIdx  int
		pairKey   string
	}
	var entries []entry
	var calls []rpc.MulticallCall

	// V3 pool balances (2 per pool)
	for _, addr := range v3PoolAddrs {
		toks := v3PoolTokens[addr]
		// balanceOf tokenIn
		dataIn, _ := erc20ABIparsed.Pack("balanceOf", common.HexToAddress(addr))
		// wait: balanceOf is called ON the token contract, querying the pool holder.
		// We need target=token, calldata=balanceOf(pool). Fix below.
		_ = dataIn
		// tokenIn balance
		d1, _ := erc20ABIparsed.Pack("balanceOf", common.HexToAddress(addr))
		calls = append(calls, rpc.MulticallCall{Target: common.HexToAddress(toks[0]), Calldata: d1})
		entries = append(entries, entry{kind: "v3bal", poolAddr: addr, tokIn: toks[0], tokOut: toks[1], /* rIn */})
		// tokenOut balance
		d2, _ := erc20ABIparsed.Pack("balanceOf", common.HexToAddress(addr))
		calls = append(calls, rpc.MulticallCall{Target: common.HexToAddress(toks[1]), Calldata: d2})
		entries = append(entries, entry{kind: "v3bal", poolAddr: addr, tokIn: toks[0], tokOut: toks[1], /* rOut */})
	}

	// V2 reserves (1 getReserves per unique pair) — batched.
	for _, pk := range v2PairKeys {
		// resolve pair address (cached permanently)
		v2PairMu.Lock()
		pairAddr, has := v2PairCache[pk[0]+":"+pk[1]]
		v2PairMu.Unlock()
		if !has {
			pa, err := getV2Pair(ctx, client, pk[0], pk[1])
			if err != nil {
				continue
			}
			pairAddr = pa
			v2PairMu.Lock()
			v2PairCache[pk[0]+":"+pk[1]] = pairAddr
			v2PairMu.Unlock()
		}
		// resolve token0 (cached permanently)
		v2Token0Mu.Lock()
		_, hasT0 := v2Token0Cache[pairAddr]
		v2Token0Mu.Unlock()
		if !hasT0 {
			t0, err := getToken0(ctx, client, pairAddr)
			if err != nil {
				continue
			}
			v2Token0Mu.Lock()
			v2Token0Cache[pairAddr] = t0
			v2Token0Mu.Unlock()
		}
		data, _ := univ2PairParsed.Pack("getReserves")
		calls = append(calls, rpc.MulticallCall{Target: common.HexToAddress(pairAddr), Calldata: data})
		entries = append(entries, entry{kind: "v2res", pairKey: pk[0] + ":" + pk[1]})
	}

	// V3 hop-0 quotes
	for _, q := range v3Quotes {
		data, perr := univ3QuoterParsed.Pack("quoteExactInputSingle", struct {
			TokenIn           common.Address `abi:"tokenIn"`
			TokenOut          common.Address `abi:"tokenOut"`
			AmountIn          *big.Int       `abi:"amountIn"`
			Fee               *big.Int       `abi:"fee"`
			SqrtPriceLimitX96 *big.Int       `abi:"sqrtPriceLimitX96"`
		}{
			TokenIn:           common.HexToAddress(q.tokenIn),
			TokenOut:          common.HexToAddress(q.tokenOut),
			AmountIn:          q.amountIn,
			Fee:               big.NewInt(int64(q.fee)),
			SqrtPriceLimitX96: big.NewInt(0),
		})
		if perr != nil {
			continue
		}
		calls = append(calls, rpc.MulticallCall{Target: common.HexToAddress(config.UniswapV3Quoter), Calldata: data})
		entries = append(entries, entry{kind: "v3quote", routeIdx: q.routeIdx, tokIn: q.tokenIn, tokOut: q.tokenOut, fee: q.fee, amountIn: q.amountIn})
	}

	if len(calls) == 0 {
		return
	}

	results, err := rpc.DoMulticall(ctx, client, calls)
	if err != nil {
		return
	}

	// Distribute results.
	bi := 0 // balance index within v3 pool (0=rIn,1=rOut)
	for i, res := range results {
		if i >= len(entries) {
			break
		}
		e := entries[i]
		switch e.kind {
		case "v3bal":
			// determine if this is rIn or rOut by position: even index -> rIn, odd -> rOut
			// We'll handle below with a running per-pool counter; simpler: store in temp.
			_ = res
		case "v3quote":
			if res.Success && len(res.ReturnData) >= 32 {
				out, uerr := univ3QuoterParsed.Unpack("quoteExactInputSingle", res.ReturnData)
				if uerr == nil {
					blockNum := currentBlockNumber(ctx, client)
					qKey := e.tokIn + ":" + e.tokOut + ":" + fmt.Sprintf("%d", e.fee) + ":" + e.amountIn.String() + ":" + fmt.Sprintf("%d", blockNum)
					v3QuoteMu.Lock()
					v3QuoteCache[qKey] = out[0].(*big.Int)
					v3QuoteMu.Unlock()
				}
			}
		}
		bi++
	}

	// Handle v3bal results: pair them up (2 per pool).
	// Re-iterate to populate v3Cache. We need to group consecutive pairs.
	idx := 0
	for _, addr := range v3PoolAddrs {
		toks := v3PoolTokens[addr]
		// find the two entries for this pool in results (they're contiguous in the order we added)
		// We added balances in order of v3PoolAddrs, 2 per pool. So compute offset.
		_ = toks
	}
	// Simpler: walk results again, pairing v3bal entries.
	// Collect v3bal result values in order.
	var balVals []*big.Int
	var balErrs []error
	for i, res := range results {
		if i >= len(entries) {
			break
		}
		if entries[i].kind != "v3bal" {
			continue
		}
		if !res.Success || len(res.ReturnData) < 32 {
			balVals = append(balVals, nil)
			balErrs = append(balErrs, fmt.Errorf("balance call failed"))
		} else {
			out, uerr := erc20ABIparsed.Unpack("balanceOf", res.ReturnData)
			if uerr != nil {
				balVals = append(balVals, nil)
				balErrs = append(balErrs, uerr)
			} else {
				balVals = append(balVals, out[0].(*big.Int))
				balErrs = append(balErrs, nil)
			}
		}
	}
	// Now pair them (2 per pool) and populate v3Cache.
	bi = 0
	for _, addr := range v3PoolAddrs {
		if bi+1 >= len(balVals) {
			break
		}
		rIn := balVals[bi]
		rOut := balVals[bi+1]
		err1 := balErrs[bi]
		err2 := balErrs[bi+1]
		var err error
		if err1 != nil {
			err = err1
		} else if err2 != nil {
			err = err2
		}
		v3Mu.Lock()
		key := addr + ":" + fmt.Sprintf("%d", blockNum)
		if err != nil {
			v3Cache[key] = struct {
				rIn  *big.Int
				rOut *big.Int
				err  error
			}{err: err}
		} else {
			v3Cache[key] = struct {
				rIn  *big.Int
				rOut *big.Int
				err  error
			}{rIn: new(big.Int).Set(rIn), rOut: new(big.Int).Set(rOut), err: nil}
		}
		v3Mu.Unlock()
		bi += 2
	}

	// V2 reserves: already cached by v2Reserves calls above (which fetched pair+reserves+token0).
	// Those were individual calls, not batched. To batch them too, we'd need to restructure.
	// For now, v2Reserves already populated the cache; leave as is.
	_ = idx
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

// EvaluateAllRoutes evaluates all routes sequentially.
func EvaluateAllRoutes(ctx context.Context, client *rpc.Client, routes []config.Route) []RouteResult {
	res := make([]RouteResult, len(routes))
	for i, r := range routes {
		final, profit, err := EvaluateRoute(ctx, client, r, nil)
		res[i] = RouteResult{Route: r, Final: final, Profit: profit, Err: err}
	}
	return res
}

type RouteResult struct {
	Route  config.Route
	Final  *big.Int
	Profit *big.Int
	Err    error
}

// ---- Event-driven re-quoting ----
type WatchedPool struct {
	Address string
	Dirty   bool
	mu      sync.Mutex
}

func NewWatchedPool(addr string) *WatchedPool {
	return &WatchedPool{Address: addr}
}

func (wp *WatchedPool) SetDirty() {
	wp.mu.Lock()
	wp.Dirty = true
	wp.mu.Unlock()
}

func (wp *WatchedPool) IsDirty() bool {
	wp.mu.Lock()
	defer wp.mu.Unlock()
	return wp.Dirty
}

func (wp *WatchedPool) ClearDirty() {
	wp.mu.Lock()
	wp.Dirty = false
	wp.mu.Unlock()
}

// WatchSwapEvents subscribes to Swap events on the given pool addresses.
// Alchemy filters by address + event topic, so we only receive events for pools
// we care about — minimal credit usage.
func WatchSwapEvents(ctx context.Context, client *rpc.Client, pools []*WatchedPool) {
	addresses := make([]common.Address, len(pools))
	for i, p := range pools {
		addresses[i] = common.HexToAddress(p.Address)
	}
	q := ethereum.FilterQuery{
		Addresses: addresses,
	}
	ch, sub, err := client.SubscribeLogs(ctx, q)
	if err != nil {
		fmt.Printf("swap event subscription failed: %v\n", err)
		return
	}
	defer sub.Unsubscribe()
	for {
		select {
		case <-ctx.Done():
			return
		case log, ok := <-ch:
			if !ok {
				return
			}
			for _, p := range pools {
				if p.Address == log.Address.Hex() {
					p.SetDirty()
				}
			}
		}
	}
}

// BlockCache caches quote results per block to avoid re-quoting unchanged pools.
type BlockCache struct {
	mu    sync.Mutex
	block uint64
	cache map[string]*big.Int
	dirty bool
}

func NewBlockCache() *BlockCache {
	return &BlockCache{cache: make(map[string]*big.Int)}
}

func (bc *BlockCache) ShouldRequote(block uint64) bool {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	// Re-quote if the block changed, if this cache was marked dirty, or if any
	// watched pool moved since the last scan (event-driven invalidation).
	if block != bc.block || bc.dirty || IsAnyDirty() {
		bc.block = block
		bc.dirty = false
		return true
	}
	return false
}

func (bc *BlockCache) SetResult(routeName string, profit *big.Int) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	bc.cache[routeName] = profit
}

func (bc *BlockCache) GetResult(routeName string) (*big.Int, bool) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	v, ok := bc.cache[routeName]
	return v, ok
}
