package pools

import (
	"math/big"
	"testing"

	"github.com/Hailer367/Ryowatch/config"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func word(v int64) []byte {
	b := make([]byte, 32)
	if v < 0 {
		n := new(big.Int).SetInt64(v)
		n.Add(n, new(big.Int).Lsh(big.NewInt(1), 256))
		copy(b[32-len(n.Bytes()):], n.Bytes())
		return b
	}
	n := big.NewInt(v)
	copy(b[32-len(n.Bytes()):], n.Bytes())
	return b
}

func v2Log(topic common.Hash, amount0In, amount1In int64) types.Log {
	data := append(word(amount0In), word(amount1In)...)
	data = append(data, word(0)...)
	data = append(data, word(0)...)
	return types.Log{Topics: []common.Hash{topic}, Data: data}
}

func TestDecodeSwapInputsV2(t *testing.T) {
	in0, in1, ok := decodeSwapInputs(v2Log(swapV2Topic, 100, 7), PoolInfo{})
	if !ok || in0.Int64() != 100 || in1.Int64() != 7 {
		t.Fatalf("got in0=%v in1=%v ok=%v", in0, in1, ok)
	}
}

func TestDecodeSwapInputsAerodrome(t *testing.T) {
	in0, in1, ok := decodeSwapInputs(v2Log(swapAeroTopic, 50, 0), PoolInfo{})
	if !ok || in0.Int64() != 50 || in1.Sign() != 0 {
		t.Fatalf("got in0=%v in1=%v ok=%v", in0, in1, ok)
	}
}

func TestDecodeSwapInputsV3(t *testing.T) {
	data := append(word(100), word(-50)...)
	data = append(data, word(0)...)
	data = append(data, word(0)...)
	data = append(data, word(0)...)
	log := types.Log{Topics: []common.Hash{swapV3Topic}, Data: data}
	in0, in1, ok := decodeSwapInputs(log, PoolInfo{})
	if !ok || in0.Int64() != 100 || in1.Sign() != 0 {
		t.Fatalf("got in0=%v in1=%v ok=%v", in0, in1, ok)
	}
}

func TestShouldMarkDirtySizeFilter(t *testing.T) {
	old := config.MinSwapDepthFraction
	defer func() { config.MinSwapDepthFraction = old }()
	config.MinSwapDepthFraction = 0.01 // 1% of reserves

	info := PoolInfo{Address: "0xpool", Token0: "0xt0", Token1: "0xt1"}
	storeLastReserves(info.Address, info.Token0, info.Token1, big.NewInt(100_000), big.NewInt(100_000))

	// 100 / 100_000 = 0.1% < 1% -> ignore.
	if shouldMarkDirty(v2Log(swapV2Topic, 100, 0), info) {
		t.Fatal("small swap should be filtered")
	}
	// 2_000 / 100_000 = 2% >= 1% -> scan.
	if !shouldMarkDirty(v2Log(swapV2Topic, 2_000, 0), info) {
		t.Fatal("large swap should mark dirty")
	}
}

func TestShouldMarkDirtyFailsOpen(t *testing.T) {
	old := config.MinSwapDepthFraction
	defer func() { config.MinSwapDepthFraction = old }()
	config.MinSwapDepthFraction = 0.01

	// Unknown topic -> fail open.
	if !shouldMarkDirty(types.Log{Topics: []common.Hash{{0x01}}}, PoolInfo{}) {
		t.Fatal("unknown event should fail open")
	}
	// Known event but no reserves -> fail open.
	info := PoolInfo{Address: "0xmissing", Token0: "0xt0", Token1: "0xt1"}
	if !shouldMarkDirty(v2Log(swapV2Topic, 1, 0), info) {
		t.Fatal("missing reserves should fail open")
	}
}
