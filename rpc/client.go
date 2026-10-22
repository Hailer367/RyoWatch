package rpc

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Client wraps a primary WebSocket RPC and an HTTP fallback.
type Client struct {
	wsURL  string
	httpURL string

	mu   sync.RWMutex
	ws   *ethclient.Client
	http *ethclient.Client

	wsAlive atomic.Bool

	callsWS   atomic.Int64
	callsHTTP atomic.Int64
}

func New(wsURL, httpURL string) *Client {
	c := &Client{wsURL: wsURL, httpURL: httpURL}
	go c.connectWS()
	// dial http eagerly
	if hc, err := ethclient.Dial(httpURL); err == nil {
		c.http = hc
	}
	return c
}

func (c *Client) connectWS() {
	backoff := time.Second
	for {
		cli, err := ethclient.Dial(c.wsURL)
		if err != nil {
			time.Sleep(backoff)
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		c.mu.Lock()
		c.ws = cli
		c.mu.Unlock()
		c.wsAlive.Store(true)
		return
	}
}

func (c *Client) wsClient() *ethclient.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ws
}

func (c *Client) httpClient() *ethclient.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.http != nil {
		return c.http
	}
	// lazy dial
	hc, err := ethclient.Dial(c.httpURL)
	if err != nil {
		return nil
	}
	c.mu.Lock()
	c.http = hc
	c.mu.Unlock()
	return hc
}

// Call executes an eth_call from the zero address. Prefers WebSocket; falls back to HTTP.
func (c *Client) Call(ctx context.Context, to common.Address, data []byte) ([]byte, error) {
	return c.CallFrom(ctx, common.Address{}, to, data)
}

// CallFrom executes an eth_call from a specific sender. Used for simulate/execute paths
// where the contract checks msg.sender.
func (c *Client) CallFrom(ctx context.Context, from, to common.Address, data []byte) ([]byte, error) {
	msg := ethereum.CallMsg{From: from, To: &to, Data: data}
	if cli := c.wsClient(); cli != nil && c.wsAlive.Load() {
		c.callsWS.Add(1)
		res, err := cli.CallContract(ctx, msg, nil)
		if err == nil {
			return res, nil
		}
	}
	c.callsHTTP.Add(1)
	hc := c.httpClient()
	if hc == nil {
		return nil, fmt.Errorf("no http client")
	}
	return hc.CallContract(ctx, msg, nil)
}

func (c *Client) SubscribeNewHeads(ctx context.Context) (chan *types.Header, ethereum.Subscription, error) {
	cli := c.wsClient()
	if cli == nil {
		return nil, nil, fmt.Errorf("ws not connected")
	}
	ch := make(chan *types.Header, 16)
	sub, err := cli.SubscribeNewHead(ctx, ch)
	if err != nil {
		return nil, nil, err
	}
	return ch, sub, nil
}

func (c *Client) PendingNonce(ctx context.Context, addr common.Address) (uint64, error) {
	hc := c.httpClient()
	if hc == nil {
		return 0, fmt.Errorf("no http client")
	}
	return hc.PendingNonceAt(ctx, addr)
}

func (c *Client) SuggestGasPrice(ctx context.Context) (*big.Int, error) {
	hc := c.httpClient()
	if hc == nil {
		return nil, fmt.Errorf("no http client")
	}
	return hc.SuggestGasPrice(ctx)
}

func (c *Client) ChainID(ctx context.Context) (*big.Int, error) {
	hc := c.httpClient()
	if hc == nil {
		return nil, fmt.Errorf("no http client")
	}
	return hc.ChainID(ctx)
}

func (c *Client) SendTransaction(ctx context.Context, tx *types.Transaction) error {
	hc := c.httpClient()
	if hc == nil {
		return fmt.Errorf("no http client")
	}
	return hc.SendTransaction(ctx, tx)
}

func (c *Client) WaitForReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	hc := c.httpClient()
	if hc == nil {
		return nil, fmt.Errorf("no http client")
	}
	for {
		r, err := hc.TransactionReceipt(ctx, hash)
		if err == nil {
			return r, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (c *Client) Stats() (ws, http int64) {
	return c.callsWS.Load(), c.callsHTTP.Load()
}

func (c *Client) Close() {
	if c.ws != nil {
		c.ws.Close()
	}
	if c.http != nil {
		c.http.Close()
	}
}
