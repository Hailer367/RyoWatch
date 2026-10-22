package pools

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum"
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

	univ2PairABI = `[{"name":"getReserves","type":"function","stateMutability":"view","inputs":[],"outputs":[{"name":"reserve0","type":"uint112"},{"name":"reserve1","type":"uint112"},{"name":"blockTimestampLast","type":"uint32"}]},{"name":"token0","type":"function","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"address"}]}]`

	univ2FactoryABI = `[{"name":"getPair","type":"function","stateMutability":"view","inputs":[{"name":"tokenA","type":"address"},{"name":"tokenB","type":"address"}],"outputs":[{"name":"","type":"address"}]}]`
)

var (
	erc20ABIparsed    abi.ABI
	univ2RouterParsed abi.ABI
	univ3QuoterParsed abi.ABI
	aerodromeParsed   abi.ABI
	slipstreamParsed  abi.ABI
	univ2PairParsed   abi.ABI
	univ2FactoryParsed abi.ABI
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
	cacheMu    sync.Mutex
	blockCache = make(map[string]*cacheEntry)
	lastBlock  uint64
	dirtyMu    sync.Mutex
	dirtyPools = make(map[string]bool)
)

func MarkDirty(poolAddr string) {
	dirtyMu.Lock()
	dirtyPools[poolAddr] = true
	dirtyMu.Unlock()
}

func IsDirty(poolAddr string) bool {
	dirtyMu.Lock()
	defer dirtyMu.Unlock()
	return dirtyPools[poolAddr]
}

func ClearDirty() {
	dirtyMu.Lock()
	dirtyPools = make(map[string]bool)
	dirtyMu.Unlock()
}

func getCached(key string, block uint64) (*big.Int, bool) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	e, ok := blockCache[key]
	if !ok || e.block != block {
		return nil, false
	}
	// If the pool is dirty, don't use cache.
	if dirtyPools[key] {
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

// quoteV2Local computes the V2 output locally from pool reserves.
func quoteV2Local(ctx context.Context, client *rpc.Client, tokenIn, tokenOut string, amountIn *big.Int) (*big.Int, error) {
	pairAddr, err := getV2Pair(ctx, client, tokenIn, tokenOut)
	if err != nil {
		return nil, err
	}
	reserves, err := getReserves(ctx, client, pairAddr)
	if err != nil {
		return nil, err
	}
	token0, err := getToken0(ctx, client, pairAddr)
	if err != nil {
		return nil, err
	}
	var reserveIn, reserveOut *big.Int
	if token0 == tokenIn {
		reserveIn = reserves[0]
		reserveOut = reserves[1]
	} else {
		reserveIn = reserves[1]
		reserveOut = reserves[0]
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
	return out[0].(common.Address).Hex(), nil
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
	if block == bc.block && !bc.dirty {
		return false
	}
	bc.block = block
	bc.dirty = false
	return true
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
