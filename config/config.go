package config

import (
	"fmt"
	"math/big"
	"os"
	"strconv"
)

// ---- Base mainnet addresses (verified on-chain) ----
const (
	BalancerVault  = "0xBA12222222228d8Ba445958a75a0704d566BF2C8"
	WETH           = "0x4200000000000000000000000000000000000006"
	USDC           = "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"

	UniswapV3Router = "0x2626664c2603336E57B271c5C0b26F421741e481"
	UniswapV3Quoter = "0x3d4e44Eb1374240CE5F1B871ab261CD16335B76a"
	UniswapV2Router = "0x4752ba5DBc23f44D87826276BF6Fd6b1C372aD24"
	UniswapV2Factory = "0x8909Dc15e40173Ff4699343b6eB8132c65e18eC6"

	AerodromeRouter  = "0xcF77a3Ba9A5CA399B7c97c74d54e5b1Beb874E43"
	AerodromeFactory = "0x420DD381b31aEf6683db6B902084cB0FFECe40Da"

	SlipstreamRouter = "0xBE6D8f0d05cC4be24d5167a3eF062215bE6D18a5"
	SlipstreamQuoter = "0x254cF9E1E6e233aa1AC962CB9B05b2cfeAaE15b0"
	SlipstreamFactory = "0x5e7BB104d84c7CB9B682AaC2F3d509f5F406809A"

	UniswapV3Factory = "0x33128a8fC17869897dcE68Ed026d694621f6FDfD"

	// Multicall3 — deployed at the same address on every EVM chain.
	Multicall3 = "0xcA11bde05977b3631167028862bE2a173976CA11"
)

// ---- DEX type ids (must match Ryo.sol) ----
const (
	DexV3         = 6
	DexV2         = 8
	DexAerodrome  = 4
	DexSlipstream = 7
)

// ---- Safety defaults ----
var (
	MaxTradeSize     float64 = 1.0
	MaxGasPriceGwei  int64   = 50
	MinProfit        float64 = 0.001
	SlippageBps      int64   = 50
	CircuitBreakerN  int     = 3
	PollIntervalSec  float64 = 2.0
	DryRun           bool    = true
	// SimMargin: only simulate if off-chain profit exceeds minProfit by this factor.
	SimMargin        float64 = 1.5
	PoolDepthFraction float64 = 0.01
	// SimRPCURL: separate RPC for simulate calls (use a local anvil fork for ~1ms latency).
	SimRPCURL string
)

// ---- RPC ----
var (
	RPCURL           string
	RyoAddress       string
	OperatorAddress  string
	OperatorKey      string
)

// ---- Route definition ----
type Hop struct {
	Dex        string
	Router     string
	TokenIn    string
	TokenOut   string
	Fee        uint32
	Stable     bool
	TickSpacing int32
	MinOut     uint64
}

type Route struct {
	Name      string
	Asset     string
	Amount    float64
	Hops      []Hop
	MinProfit float64
}

var DefaultRoutes = []Route{
	{
		Name:   "weth-usdc-v3-v2",
		Asset:  WETH,
		Amount: 1.0,
		Hops: []Hop{
			{Dex: "v3", Router: UniswapV3Router, TokenIn: WETH, TokenOut: USDC, Fee: 500, MinOut: 1},
			{Dex: "v2", Router: UniswapV2Router, TokenIn: USDC, TokenOut: WETH, MinOut: 1},
		},
		MinProfit: 0.001,
	},
	{
		Name:   "usdc-weth-v3-v2",
		Asset:  USDC,
		Amount: 1000.0,
		Hops: []Hop{
			{Dex: "v3", Router: UniswapV3Router, TokenIn: USDC, TokenOut: WETH, Fee: 500, MinOut: 1},
			{Dex: "v2", Router: UniswapV2Router, TokenIn: WETH, TokenOut: USDC, MinOut: 1},
		},
		MinProfit: 0.001,
	},
}

func Load() error {
	// godotenv not available; .env is loaded manually in main.go
	RPCURL = getenv("BASE_RPC_URL", "https://mainnet.base.org")
	SimRPCURL = getenv("SIM_RPC_URL", "") // empty = use RPCURL
	RyoAddress = getenv("RYO_ADDRESS", "")
	OperatorAddress = getenv("OPERATOR_ADDRESS", "")
	OperatorKey = getenv("OPERATOR_PRIVATE_KEY", "")

	MaxTradeSize, _ = strconv.ParseFloat(getenv("MAX_TRADE_SIZE", "1.0"), 64)
	if v, err := strconv.ParseInt(getenv("MAX_GAS_PRICE_GWEI", "50"), 10, 64); err == nil {
		MaxGasPriceGwei = v
	}
	if v, err := strconv.ParseFloat(getenv("MIN_PROFIT", "0.001"), 64); err == nil {
		MinProfit = v
	}
	if v, err := strconv.ParseInt(getenv("SLIPPAGE_BPS", "50"), 10, 64); err == nil {
		SlippageBps = v
	}
	if v, err := strconv.Atoi(getenv("CIRCUIT_BREAKER_FAILURES", "3")); err == nil {
		CircuitBreakerN = v
	}
	if v, err := strconv.ParseFloat(getenv("POLL_INTERVAL_SECONDS", "2.0"), 64); err == nil {
		PollIntervalSec = v
	}
	if v, err := strconv.ParseFloat(getenv("SIM_MARGIN", "1.5"), 64); err == nil {
		SimMargin = v
	}
	if v, err := strconv.ParseFloat(getenv("POOL_DEPTH_FRACTION", "0.01"), 64); err == nil {
		PoolDepthFraction = v
	}
	DryRun = getenv("DRY_RUN", "true") == "true"

	if RPCURL == "" {
		return fmt.Errorf("BASE_RPC_URL is required")
	}
	if RyoAddress != "" {
		if len(RyoAddress) != 42 || RyoAddress[:2] != "0x" {
			return fmt.Errorf("RYO_ADDRESS must be a 0x-prefixed 20-byte hex address")
		}
	}
	if OperatorKey != "" && OperatorAddress == "" {
		return fmt.Errorf("OPERATOR_ADDRESS required when OPERATOR_PRIVATE_KEY is set")
	}
	if OperatorAddress != "" && OperatorKey == "" && RyoAddress != "" {
		// allowed for dry-run / read-only, but warn
	}
	return nil
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func SimRPC() string {
	if SimRPCURL != "" {
		return SimRPCURL
	}
	return RPCURL
}

func ToRaw(amount float64, decimals int) *big.Int {
	f := new(big.Float).SetPrec(256)
	f.SetFloat64(amount)
	scale := new(big.Float).SetPrec(256)
	scale.SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
	f.Mul(f, scale)
	i, _ := f.Int(nil)
	return i
}

func FromRaw(raw *big.Int, decimals int) float64 {
	f := new(big.Float).SetInt(raw)
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	div := new(big.Float).SetInt(scale)
	f.Quo(f, div)
	v, _ := f.Float64()
	return v
}
