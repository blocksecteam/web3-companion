// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package policy

import (
	"time"

	"github.com/blocksecteam/web3-companion/internal/money"
)

type Request struct {
	UserID       string
	WalletID     string
	WalletUID    string
	TxType       string
	AmountUSD    money.Amount
	PriceUnknown bool
	Recipient    string
	Now          time.Time
}

type Result struct {
	Decision Decision
	Policy   *Policy
	Reason   string
}

type Policy struct {
	PolicyID                  string
	WalletID                  string
	WalletUID                 string
	UserID                    string
	Name                      string
	Enabled                   bool
	Mode                      string
	MaxPerTransactionUSD      money.Amount
	DailyTransferLimitUSD     money.Amount
	DailyTradeLimitUSD        money.Amount
	AutoSignEnabled           bool
	AutoSignMaxPerTxUSD       money.Amount
	AutoSignDailyLimitUSD     money.Amount
	AutoSignMaxCountDay       int
	RecipientWhitelistEnabled bool
	RecipientWhitelistAddrs   string
	DailyTransferUsedUSD      money.Amount
	DailyTradeUsedUSD         money.Amount
	AutoSignDailyUsedUSD      money.Amount
	AutoSignCountToday        int
	LastResetDate             string
	CreatedAt                 time.Time
	UpdatedAt                 time.Time
}

type SignedOperation struct {
	UserID       string
	WalletID     string
	WalletUID    string
	PolicyID     string
	Chain        string
	TxHash       string
	TxType       string
	AmountUSD    money.Amount
	PriceUnknown bool
	Decision     Decision
	ExpiresAt    *time.Time
	Now          time.Time
}

type Usage struct {
	DailyTransferUsedUSD money.Amount `json:"daily_transfer_used_usd"`
	DailyTradeUsedUSD    money.Amount `json:"daily_trade_used_usd"`
	AutoSignDailyUsedUSD money.Amount `json:"auto_sign_daily_used_usd"`
	AutoSignCountToday   int          `json:"auto_sign_count_today"`
	LastResetDate        string       `json:"last_reset_date"`

	PendingTransferUSD     money.Amount `json:"daily_transfer_pending_usd"`
	PendingTradeUSD        money.Amount `json:"daily_trade_pending_usd"`
	AutoSignPendingUSD     money.Amount `json:"auto_sign_pending_usd"`
	AutoSignPendingCount   int          `json:"auto_sign_pending_count"`
	SubmittedTransferUSD   money.Amount `json:"daily_transfer_submitted_usd"`
	SubmittedTradeUSD      money.Amount `json:"daily_trade_submitted_usd"`
	AutoSignSubmittedUSD   money.Amount `json:"auto_sign_submitted_usd"`
	AutoSignSubmittedCount int          `json:"auto_sign_submitted_count"`
}

type DefinitionInput struct {
	Name                      string
	Mode                      string
	MaxPerTransactionUSD      money.Amount
	DailyTransferLimitUSD     money.Amount
	DailyTradeLimitUSD        money.Amount
	AutoSignEnabled           bool
	AutoSignMaxPerTxUSD       money.Amount
	AutoSignDailyLimitUSD     money.Amount
	AutoSignMaxCountDay       int
	RecipientWhitelistEnabled bool
	RecipientWhitelistAddrs   []string
}

type DefinitionResponse struct {
	PolicyDefinitionID        string       `json:"policy_definition_id"`
	CurrentVersionID          string       `json:"current_version_id"`
	Name                      string       `json:"name"`
	IsDefault                 bool         `json:"is_default"`
	Version                   int          `json:"version"`
	Mode                      string       `json:"mode"`
	MaxPerTransactionUSD      money.Amount `json:"max_per_transaction_usd"`
	DailyTransferLimitUSD     money.Amount `json:"daily_transfer_limit_usd"`
	DailyTradeLimitUSD        money.Amount `json:"daily_trade_limit_usd"`
	AutoSignEnabled           bool         `json:"auto_sign_enabled"`
	AutoSignMaxPerTxUSD       money.Amount `json:"auto_sign_max_per_tx_usd"`
	AutoSignDailyLimitUSD     money.Amount `json:"auto_sign_daily_limit_usd"`
	AutoSignMaxCountDay       int          `json:"auto_sign_max_count_day"`
	RecipientWhitelistEnabled bool         `json:"recipient_whitelist_enabled"`
	RecipientWhitelistAddrs   []string     `json:"recipient_whitelist_addrs"`
	CreatedAt                 time.Time    `json:"created_at"`
	UpdatedAt                 time.Time    `json:"updated_at"`
}

type Response struct {
	PolicyID                  string       `json:"policy_id"`
	WalletID                  string       `json:"wallet_id"`
	Name                      string       `json:"name"`
	Enabled                   bool         `json:"enabled"`
	Mode                      string       `json:"mode"`
	MaxPerTransactionUSD      money.Amount `json:"max_per_transaction_usd"`
	DailyTransferLimitUSD     money.Amount `json:"daily_transfer_limit_usd"`
	DailyTradeLimitUSD        money.Amount `json:"daily_trade_limit_usd"`
	AutoSignEnabled           bool         `json:"auto_sign_enabled"`
	AutoSignMaxPerTxUSD       money.Amount `json:"auto_sign_max_per_tx_usd"`
	AutoSignDailyLimitUSD     money.Amount `json:"auto_sign_daily_limit_usd"`
	AutoSignMaxCountDay       int          `json:"auto_sign_max_count_day"`
	RecipientWhitelistEnabled bool         `json:"recipient_whitelist_enabled"`
	RecipientWhitelistAddrs   []string     `json:"recipient_whitelist_addrs"`
	Usage                     Usage        `json:"usage"`
	CreatedAt                 time.Time    `json:"created_at"`
	UpdatedAt                 time.Time    `json:"updated_at"`
}

type DefinitionSummaryResult struct {
	Manual      int64
	Auto        int64
	DefaultName string
}
