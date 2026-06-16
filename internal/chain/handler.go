// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package chain

import (
	"context"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/core/types"

	"github.com/blocksecteam/web3-companion/internal/config"
	"github.com/blocksecteam/web3-companion/internal/ssm/vault"
	"gorm.io/gorm"
)

var (
	clients   = map[string]*RPCClient{}
	clientsMu sync.Mutex
)

func getRPCClient(rpcURL string) *RPCClient {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	if c, ok := clients[rpcURL]; ok {
		return c
	}
	c := newRPCClient(rpcURL)
	clients[rpcURL] = c
	return c
}

type TransactionRPC interface {
	SendRawTransaction(context.Context, string) (string, error)
	TransactionReceipt(context.Context, string) (*types.Receipt, error)
	BlockTimestamp(context.Context, *big.Int) (*time.Time, error)
}

type rpcClientFactory func(*config.ChainConfig) TransactionRPC

func defaultRPCClientFactory(ch *config.ChainConfig) TransactionRPC {
	return getRPCClient(ch.RPCURL)
}

type TokenResolver interface {
	Resolve(ctx context.Context, chain, asset string) (uint8, error)
}

type Handler struct {
	cfg        *config.Config
	db         *gorm.DB
	kms        *vault.KMSClient
	resolver   TokenResolver
	rpcFactory rpcClientFactory
}

func WithTokenResolver(r TokenResolver) func(*Handler) {
	return func(h *Handler) { h.resolver = r }
}

func WithTransactionStore(db *gorm.DB) func(*Handler) {
	return func(h *Handler) { h.db = db }
}

func WithKMSClient(kms *vault.KMSClient) func(*Handler) {
	return func(h *Handler) { h.kms = kms }
}

func WithRPCClientFactory(factory func(*config.ChainConfig) TransactionRPC) func(*Handler) {
	return func(h *Handler) { h.rpcFactory = factory }
}

func (h *Handler) transactionRPC(ch *config.ChainConfig) TransactionRPC {
	if h.rpcFactory != nil {
		return h.rpcFactory(ch)
	}
	return defaultRPCClientFactory(ch)
}
