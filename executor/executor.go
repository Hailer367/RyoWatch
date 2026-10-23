package executor

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/Hailer367/Ryowatch/config"
	"github.com/Hailer367/Ryowatch/pools"
	"github.com/Hailer367/Ryowatch/routes"
	"github.com/Hailer367/Ryowatch/rpc"
)

const ryoABI = `[
  {"name":"simulate","type":"function","stateMutability":"nonpayable","inputs":[{"name":"asset","type":"address"},{"name":"amount","type":"uint256"},{"name":"swaps","type":"tuple[]","components":[{"name":"dexType","type":"uint8"},{"name":"router","type":"address"},{"name":"tokenIn","type":"address"},{"name":"tokenOut","type":"address"},{"name":"extraData","type":"bytes"}]}],"outputs":[]},
  {"name":"execute","type":"function","stateMutability":"nonpayable","inputs":[{"name":"asset","type":"address"},{"name":"amount","type":"uint256"},{"name":"swaps","type":"tuple[]","components":[{"name":"dexType","type":"uint8"},{"name":"router","type":"address"},{"name":"tokenIn","type":"address"},{"name":"tokenOut","type":"address"},{"name":"extraData","type":"bytes"}]},{"name":"minProfit","type":"uint256"}],"outputs":[{"name":"profit","type":"uint256"}]},
  {"name":"paused","type":"function","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"bool"}]}
]`

var ryoParsed abi.ABI

func init() {
	p, err := abi.JSON(strings.NewReader(ryoABI))
	if err != nil {
		panic(err)
	}
	ryoParsed = p
}

type swapTuple struct {
	DexType  uint8          `abi:"dexType"`
	Router   common.Address `abi:"router"`
	TokenIn  common.Address `abi:"tokenIn"`
	TokenOut common.Address `abi:"tokenOut"`
	Extra    []byte         `abi:"extraData"`
}

func toSwapTuples(swaps []routes.SwapTuple) []swapTuple {
	out := make([]swapTuple, len(swaps))
	for i, s := range swaps {
		out[i] = swapTuple{
			DexType:  s.DexType,
			Router:   s.Router,
			TokenIn:  s.TokenIn,
			TokenOut: s.TokenOut,
			Extra:    s.Extra,
		}
	}
	return out
}

// Simulate calls Ryo.simulate (always reverts with SimulationResult).
// Returns (profit, nil) on a clean SimulationResult, or (nil, err) otherwise.
func Simulate(ctx context.Context, client *rpc.Client, route config.Route) (*big.Int, error) {
	if config.RyoAddress == "" {
		return nil, fmt.Errorf("RYO_ADDRESS not set")
	}
	swaps, err := routes.ToSwaps(route)
	if err != nil {
		return nil, err
	}
	dec, err := pools.TokenDecimals(ctx, client, route.Asset)
	if err != nil {
		return nil, err
	}
	amount := config.ToRaw(route.Amount, dec)

	callData, err := ryoParsed.Pack("simulate", common.HexToAddress(route.Asset), amount, toSwapTuples(swaps))
	if err != nil {
		return nil, err
	}
	to := common.HexToAddress(config.RyoAddress)
	_, err = client.CallFrom(ctx, common.HexToAddress(config.OperatorAddress), to, callData)
	if err == nil {
		return nil, fmt.Errorf("simulate did not revert")
	}
	// The revert data lives in the RPC error's Data() method, not in the message string.
	var revertData []byte
	if de, ok := err.(ethrpc.DataError); ok {
		if d, ok := de.ErrorData().(string); ok {
			revertData, _ = hex.DecodeString(strings.TrimPrefix(d, "0x"))
		} else if b, ok := de.ErrorData().([]byte); ok {
			revertData = b
		}
	}
	if len(revertData) == 0 {
		revertData = extractRevertData(err)
	}
	if len(revertData) < 4 {
		return nil, fmt.Errorf("simulate reverted without data: %v", err)
	}
	// SimulationResult selector: 0x00cf1ea6
	if revertData[0] != 0x00 || revertData[1] != 0xcf || revertData[2] != 0x1e || revertData[3] != 0xa6 {
		return nil, fmt.Errorf("simulate reverted: %x", revertData)
	}
	if len(revertData) < 36 {
		return nil, fmt.Errorf("short revert data")
	}
	profit := new(big.Int).SetBytes(revertData[4:36])
	if revertData[4] >= 0x80 {
		profit.Sub(profit, new(big.Int).Exp(big.NewInt(2), big.NewInt(256), nil))
	}
	return profit, nil
}

func extractRevertData(err error) []byte {
	s := err.Error()
	idx := -1
	for i := 0; i+2 < len(s); i++ {
		if s[i] == '0' && s[i+1] == 'x' {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	hexstr := s[idx:]
	end := len(hexstr)
	for j := 2; j < len(hexstr); j++ {
		c := hexstr[j]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			end = j
			break
		}
	}
	hexstr = hexstr[:end]
	if len(hexstr) <= 2 {
		return nil
	}
	b, err := hex.DecodeString(hexstr[2:])
	if err != nil {
		return nil
	}
	return b
}

// Execute sends a real transaction. Returns (txHash, profit, error).
func Execute(ctx context.Context, client *rpc.Client, route config.Route) (string, *big.Int, error) {
	if config.RyoAddress == "" {
		return "", nil, fmt.Errorf("RYO_ADDRESS not set")
	}
	if config.DryRun {
		return "", nil, fmt.Errorf("dry run: not sending transaction")
	}
	swaps, err := routes.ToSwaps(route)
	if err != nil {
		return "", nil, err
	}
	dec, err := pools.TokenDecimals(ctx, client, route.Asset)
	if err != nil {
		return "", nil, err
	}
	amount := config.ToRaw(route.Amount, dec)
	minProfit := config.ToRaw(route.MinProfit, dec)

	callData, err := ryoParsed.Pack("execute", common.HexToAddress(route.Asset), amount, toSwapTuples(swaps), minProfit)
	if err != nil {
		return "", nil, err
	}
	to := common.HexToAddress(config.RyoAddress)

	nonce, err := client.PendingNonce(ctx, common.HexToAddress(config.OperatorAddress))
	if err != nil {
		return "", nil, err
	}
	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		return "", nil, err
	}
	chainID, err := client.ChainID(ctx)
	if err != nil {
		return "", nil, err
	}

	key, err := crypto.HexToECDSA(strings.TrimPrefix(config.OperatorKey, "0x"))
	if err != nil {
		return "", nil, err
	}
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: big.NewInt(1_000_000_000),
		GasFeeCap: new(big.Int).Mul(gasPrice, big.NewInt(2)),
		Gas:       1_000_000,
		To:        &to,
		Value:     big.NewInt(0),
		Data:      callData,
	})
	signedTx, err := types.SignTx(tx, types.NewLondonSigner(chainID), key)
	if err != nil {
		return "", nil, err
	}
	if err := client.SendTransaction(ctx, signedTx); err != nil {
		return "", nil, err
	}
	receipt, err := client.WaitForReceipt(ctx, signedTx.Hash())
	if err != nil {
		return "", nil, err
	}
	if receipt.Status != 1 {
		return signedTx.Hash().Hex(), nil, fmt.Errorf("tx reverted: %s", signedTx.Hash().Hex())
	}
	profit := parseProfit(receipt)
	return signedTx.Hash().Hex(), profit, nil
}

// parseProfit extracts profit from the ArbitrageExecuted event.
func parseProfit(receipt *types.Receipt) *big.Int {
	sig := "ArbitrageExecuted(address,address,uint256,uint256,uint256)"
	topic0 := crypto.Keccak256([]byte(sig))
	for _, log := range receipt.Logs {
		if len(log.Topics) == 0 {
			continue
		}
		if string(log.Topics[0].Bytes()) != string(topic0) {
			continue
		}
		// data = amountBorrowed (32) | profit (32) | swapCount (32)
		if len(log.Data) < 96 {
			continue
		}
		return new(big.Int).SetBytes(log.Data[32:64])
	}
	return nil
}
