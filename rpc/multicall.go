package rpc

import (
	"context"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

// Multicall3 address (same on every EVM chain).
const Multicall3Addr = "0xcA11bde05977b3631167028862bE2a173976CA11"

// MulticallCall is one (target, calldata) pair.
type MulticallCall struct {
	Target common.Address
	Calldata []byte
}

// Multicall3Result is the (success, returnData) pair returned by aggregate3.
type Multicall3Result struct {
	Success    bool
	ReturnData []byte
}

// DoMulticall executes a batch of calls through Multicall3 in a single eth_call.
func DoMulticall(ctx context.Context, client *Client, calls []MulticallCall) ([]Multicall3Result, error) {
	if len(calls) == 0 {
		return nil, nil
	}
	// Build the aggregate3 calldata using go-ethereum's abi encoder.
	data, err := encodeAggregate3(calls)
	if err != nil {
		return nil, err
	}
	to := common.HexToAddress(Multicall3Addr)
	raw, err := client.Call(ctx, to, data)
	if err != nil {
		return nil, err
	}
	return decodeAggregate3(raw, len(calls))
}

// encodeAggregate3 and decodeAggregate3 are implemented in multicall_abi.go
var _ = ethereum.CallMsg{}
