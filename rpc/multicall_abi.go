package rpc

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// Multicall3 ABI (only aggregate3).
const multicall3ABI = `[{"inputs":[{"components":[{"internalType":"address","name":"target","type":"address"},{"internalType":"bool","name":"allowFailure","type":"bool"},{"internalType":"bytes","name":"callData","type":"bytes"}],"internalType":"struct Multicall3.Call3[]","name":"calls","type":"tuple[]"}],"name":"aggregate3","outputs":[{"components":[{"internalType":"bool","name":"success","type":"bool"},{"internalType":"bytes","name":"returnData","type":"bytes"}],"internalType":"struct Multicall3.Result[]","name":"returnData","type":"tuple[]"}],"stateMutability":"payable","type":"function"}]`

var multicall3ParsedABI abi.ABI

func init() {
	parsed, err := abi.JSON(strings.NewReader(multicall3ABI))
	if err != nil {
		panic(err)
	}
	multicall3ParsedABI = parsed
}

// encodeAggregate3 builds the calldata for Multicall3.aggregate3.
func encodeAggregate3(calls []MulticallCall) ([]byte, error) {
	type call3 struct {
		Target       common.Address `abi:"target"`
		AllowFailure bool           `abi:"allowFailure"`
		CallData     []byte         `abi:"callData"`
	}
	arr := make([]call3, len(calls))
	for i, c := range calls {
		arr[i] = call3{Target: c.Target, AllowFailure: true, CallData: c.Calldata}
	}
	return multicall3ParsedABI.Pack("aggregate3", arr)
}

// aggregate3Output is the typed return of aggregate3.
type aggregate3Output struct {
	ReturnData []struct {
		Success    bool
		ReturnData []byte
	}
}

// decodeAggregate3 decodes the return data of aggregate3.
func decodeAggregate3(raw []byte, n int) ([]Multicall3Result, error) {
	out, err := multicall3ParsedABI.Unpack("aggregate3", raw)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty multicall output")
	}

	// go-ethereum may return the result slice as []interface{}, a typed slice of
	// anonymous structs, or a map. Use reflection to handle all cases robustly.
	v := reflect.ValueOf(out[0])
	if v.Kind() != reflect.Slice {
		return nil, fmt.Errorf("unexpected multicall return type %T", out[0])
	}
	res := make([]Multicall3Result, v.Len())
	for i := 0; i < v.Len(); i++ {
		item := v.Index(i)
		// dereference pointer if needed
		if item.Kind() == reflect.Ptr {
			item = item.Elem()
		}
		switch item.Kind() {
		case reflect.Map:
			m := item.Interface().(map[string]interface{})
			succ, _ := m["Success"].(bool)
			rd, _ := m["ReturnData"].([]byte)
			res[i] = Multicall3Result{Success: succ, ReturnData: rd}
		case reflect.Struct:
			// fields named Success and ReturnData
			succField := item.FieldByName("Success")
			rdField := item.FieldByName("ReturnData")
			succ := false
			if succField.IsValid() {
				succ = succField.Bool()
			}
			var rd []byte
			if rdField.IsValid() {
				if b, ok := rdField.Interface().([]byte); ok {
					rd = b
				} else if b, ok := rdField.Interface().([]uint8); ok {
					rd = b
				}
			}
			res[i] = Multicall3Result{Success: succ, ReturnData: rd}
		default:
			// try []interface{} element
			if iface, ok := item.Interface().(map[string]interface{}); ok {
				succ, _ := iface["Success"].(bool)
				rd, _ := iface["ReturnData"].([]byte)
				res[i] = Multicall3Result{Success: succ, ReturnData: rd}
			} else {
				return nil, fmt.Errorf("unexpected item type %T", item.Interface())
			}
		}
	}
	_ = n
	_ = aggregate3Output{}
	return res, nil
}
