// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package chain

import (
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

var (
	ErrRPCUnavailable = errors.New("RPC client unavailable")
)

func NewRPCClientForTest(rpcURL string) *RPCClient { return newRPCClient(rpcURL) }

type RPCClient struct {
	client *ethclient.Client
	rpcURL string
}

func newRPCClient(rpcURL string) *RPCClient {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return &RPCClient{rpcURL: rpcURL}
	}
	return &RPCClient{client: client, rpcURL: rpcURL}
}

func (r *RPCClient) ensureClient(ctx context.Context) error {
	if r.client != nil {
		return nil
	}
	client, err := ethclient.DialContext(ctx, r.rpcURL)
	if err != nil {
		return err
	}
	r.client = client
	return nil
}

func (r *RPCClient) SuggestGasPrice(ctx context.Context) (*big.Int, error) {
	if err := r.ensureClient(ctx); err != nil {
		return nil, err
	}
	return r.client.SuggestGasPrice(ctx)
}

func (r *RPCClient) EstimateGas(ctx context.Context, msg map[string]interface{}) (uint64, error) {
	if err := r.ensureClient(ctx); err != nil {
		return 0, err
	}
	callMsg := ethereum.CallMsg{}
	if from, ok := msg["from"].(string); ok {
		callMsg.From = common.HexToAddress(from)
	}
	if to, ok := msg["to"].(string); ok && to != "" {
		addr := common.HexToAddress(to)
		callMsg.To = &addr
	}
	if data, ok := msg["data"].(string); ok && data != "" {
		callMsg.Data = common.FromHex(data)
	}
	if value, ok := msg["value"].(string); ok && value != "" {
		v := new(big.Int)
		if _, ok := v.SetString(value, 0); ok {
			callMsg.Value = v
		}
	}
	return r.client.EstimateGas(ctx, callMsg)
}

func (r *RPCClient) SendRawTransaction(ctx context.Context, signedHex string) (string, error) {
	if err := r.ensureClient(ctx); err != nil {
		return "", ErrRPCUnavailable
	}
	var result string
	err := r.client.Client().CallContext(ctx, &result, "eth_sendRawTransaction", signedHex)
	if err != nil {
		return "", err
	}
	return result, nil
}

func (r *RPCClient) TransactionReceipt(ctx context.Context, txHash string) (*types.Receipt, error) {
	if err := r.ensureClient(ctx); err != nil {
		return nil, ErrRPCUnavailable
	}
	return r.client.TransactionReceipt(ctx, common.HexToHash(txHash))
}

func (r *RPCClient) BlockTimestamp(ctx context.Context, blockNumber *big.Int) (*time.Time, error) {
	if err := r.ensureClient(ctx); err != nil {
		return nil, ErrRPCUnavailable
	}
	header, err := r.client.HeaderByNumber(ctx, blockNumber)
	if err != nil {
		return nil, err
	}
	confirmedAt := time.Unix(int64(header.Time), 0).UTC()
	return &confirmedAt, nil
}

func (r *RPCClient) TransactionByHash(ctx context.Context, txHash string) (*types.Transaction, bool, error) {
	if err := r.ensureClient(ctx); err != nil {
		return nil, false, ErrRPCUnavailable
	}
	return r.client.TransactionByHash(ctx, common.HexToHash(txHash))
}
