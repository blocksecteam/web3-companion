// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package security

import (
	"encoding/json"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/blocksecteam/web3-companion/internal/storage"
)

type CheckIntentRequest struct {
	TransactionID string `json:"transaction_id" binding:"required"`
}

type CheckRequest struct {
	ChainID  int    `json:"chain_id" binding:"required"`
	Chain    string `json:"chain" binding:"required"`
	Owner    string `json:"owner" binding:"required"`
	To       string `json:"to" binding:"required"`
	Value    string `json:"value"`
	Data     string `json:"data"`
	GasLimit uint64 `json:"gas_limit"`
	GasPrice string `json:"gas_price"`
	// GasPriceSource records how GasPrice was obtained at build time:
	// "rpc" (live node), "fallback" (RPC unreachable), or "" (legacy /
	// not populated). gas_cost_sanity downgrades pass→warn when source
	// is "fallback" so the verdict surfaces an unverified estimate.
	GasPriceSource string `json:"gas_price_source,omitempty"`
	// BlockNumber pins the simulation to a specific block. Zero means
	// "latest" — see blockTag in rpc_simulator.go. CheckRequestFromIntent
	// intentionally does NOT populate this from the intent: build → check
	// is wall-clock time during which chain state changes, and verdict
	// must reflect the state the broadcast will land on, not the state at
	// build time. Direct callers may pass a specific block for replay or
	// debugging; the intent path always evaluates "latest".
	BlockNumber uint64 `json:"block_number"`
	TxType      string `json:"tx_type,omitempty"`
	SummaryJSON string `json:"summary_json,omitempty"`
}

type CheckResponse struct {
	TransactionID string          `json:"transaction_id,omitempty"`
	Status        string          `json:"status,omitempty"`
	TxType        string          `json:"tx_type,omitempty"`
	TxSummary     json.RawMessage `json:"tx_summary,omitempty"`
	Verdict       string          `json:"verdict"`
	Summary       string          `json:"summary"`
	Checks        []CheckResult   `json:"checks"`
	Effect        *TxEffect       `json:"effect,omitempty"`
	Simulation    json.RawMessage `json:"simulation,omitempty"`
}

func CheckRequestFromIntent(intent storage.TransactionIntent) CheckRequest {
	return CheckRequest{
		ChainID:        int(intent.ChainID),
		Chain:          intent.Chain,
		Owner:          intent.Owner,
		To:             intent.To,
		Value:          intent.Value,
		Data:           intent.Data,
		GasLimit:       intent.GasLimit,
		GasPrice:       intent.GasPrice,
		GasPriceSource: intent.GasPriceSource,
		TxType:         intent.TxType,
		SummaryJSON:    intent.SummaryJSON,
	}
}

type CheckResult struct {
	Rule string `json:"rule"`
	// Name, Description, StatusMeaning are filled by the orchestrator from
	// the check's RuleDescription after Run() returns. They use omitempty
	// so a future emit source that bypasses the registry would degrade
	// gracefully instead of emitting empty strings; every built-in rule is
	// a registered Check and therefore populates all three.
	Name          string      `json:"name,omitempty"`
	Description   string      `json:"description,omitempty"`
	Status        string      `json:"status"`
	StatusMeaning string      `json:"status_meaning,omitempty"`
	Detail        string      `json:"detail"`
	Observed      interface{} `json:"observed,omitempty"`
}

type Operation struct {
	To    common.Address `json:"to"`
	Value *big.Int       `json:"-"`
	Data  []byte         `json:"-"`
}

type OperationView struct {
	To     string `json:"to"`
	Value  string `json:"value"`
	Method string `json:"method,omitempty"`
}

type AssetEffect struct {
	TokenAddress string `json:"token_address"`
	TokenID      string `json:"token_id,omitempty"`
	Amount       string `json:"amount"`
	USDValue     string `json:"usd_value,omitempty"`
	Receiving    bool   `json:"receiving"`
}

type TxEffect struct {
	UserAddress       string        `json:"user_address"`
	ChainID           int           `json:"chain_id"`
	Contract          string        `json:"contract"`
	Spending          []AssetEffect `json:"spending"`
	Receiving         []AssetEffect `json:"receiving"`
	TotalSpendingUSD  *string       `json:"total_spending_usd"`
	TotalReceivingUSD *string       `json:"total_receiving_usd"`
	GasUSD            *string       `json:"gas_usd"`
	NetUSD            *string       `json:"net_usd"`
}

type BalanceChangeAsset struct {
	Address     string `json:"address"`
	TokenID     string `json:"tokenId,omitempty"`
	TokenName   string `json:"tokenName,omitempty"`
	TokenSymbol string `json:"tokenSymbol,omitempty"`
	Type        string `json:"type"`
	BaseUnits   string `json:"rawAmount"`
	Amount      string `json:"amount"`
	Value       string `json:"value,omitempty"`
}

type BalanceChange struct {
	Account string               `json:"account"`
	Assets  []BalanceChangeAsset `json:"assets"`
}

type SimulationCall struct {
	ID        int              `json:"id"`
	Status    bool             `json:"status"`
	Error     string           `json:"error,omitempty"`
	Type      string           `json:"type"`
	From      string           `json:"from"`
	To        string           `json:"to"`
	FromLabel *TraceLabel      `json:"from_label,omitempty"`
	ToLabel   *TraceLabel      `json:"to_label,omitempty"`
	Value     string           `json:"value"`
	Func      string           `json:"func"`
	GasLimit  int64            `json:"gasLimit"`
	GasUsed   int64            `json:"gasUsed"`
	Calls     []SimulationCall `json:"calls,omitempty"`
}

type SimulationCallView struct {
	Type      string               `json:"type,omitempty"`
	From      string               `json:"from,omitempty"`
	To        string               `json:"to,omitempty"`
	FromLabel *TraceLabel          `json:"from_label,omitempty"`
	ToLabel   *TraceLabel          `json:"to_label,omitempty"`
	Value     *float64             `json:"value,omitempty"`
	Func      string               `json:"func,omitempty"`
	Error     string               `json:"error,omitempty"`
	Calls     []SimulationCallView `json:"calls,omitempty"`
}

type TraceLabel struct {
	Label      string `json:"label,omitempty"`
	Attributes string `json:"attributes,omitempty"`
}

func (c SimulationCall) Iter() []SimulationCall {
	out := []SimulationCall{c}
	for _, child := range c.Calls {
		out = append(out, child.Iter()...)
	}
	return out
}

type SimulationData struct {
	Status          bool            `json:"status"`
	BlockNumber     int64           `json:"blockNumber"`
	TxnHash         string          `json:"txnHash"`
	Sender          string          `json:"sender"`
	Receiver        string          `json:"receiver"`
	CallData        string          `json:"callData"`
	GasLimit        int64           `json:"gasLimit"`
	GasPrice        string          `json:"gasPrice"`
	GasUsed         int64           `json:"gasUsed"`
	BaseFee         string          `json:"baseFee"`
	TransactionFee  string          `json:"transactionFee"`
	Nonce           int64           `json:"nonce"`
	Value           string          `json:"value"`
	ErrorInfo       string          `json:"errorInfo"`
	BalanceChanges  []BalanceChange `json:"balanceChanges"`
	ViewLink        string          `json:"viewLink"`
	Trace           *SimulationCall `json:"trace"`
	SimulationLabel string          `json:"simulation_label,omitempty"`
}

type SimulationResult struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    *SimulationData `json:"data,omitempty"`
	Raw     json.RawMessage `json:"-"`
}

type SecurityConfig struct {
	MaxGasUSD          float64
	MaxUSDValueLossBPS int64
	// KnownRoutersByChainID is the static allow-list the erc20_approve_sanity
	// check uses to suppress the `unknown_spender` advisory finding for
	// trusted spenders. Currently hardcoded in defaultSecurityConfig();
	// future versions may replace this with a trusted whitelist source.
	KnownRoutersByChainID   map[int]map[string]bool
	SecurityAgentReviewMode string
	AllowReviewClear        bool
	AllowReviewClearSet     bool
}

func defaultSecurityConfig() SecurityConfig {
	return SecurityConfig{
		MaxGasUSD:               10.0,
		MaxUSDValueLossBPS:      200,
		SecurityAgentReviewMode: "always",
		AllowReviewClear:        false,
		KnownRoutersByChainID: map[int]map[string]bool{

			1: {
				"0x3fc91a3afd70395cd496c647d5a6cc9d4b2b7fad": true,
				"0x000000000022d473030f116ddee9f6b43ac78ba3": true,
				"0xe592427a0aece92de3edee1f18e0157c05861564": true,
				"0x68b3465833fb72a70ecdf485e0e4c7bd8665fc45": true,
				"0x66a9893cc07d91d95644aedd05d03f95e1dba8af": true,
				"0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae": true,
				"0x8b742329ca21f0bce32a2f95fd43ef2c6afb52f4": true,
				"0xc36442b4a4522e871399cd717abdd847ab11fe88": true,
				"0x2a0c0dbecc7e4d658f48e01e3fa353f44050c208": true,
				"0x6131b5fae19ea4f9d964eac0408e4408b66337b5": true,
				"0xcf5540fffcdc3d510b18bfca6d2b9987b0772559": true,
				"0x6352a56caadc4f1e25cd6c75970fa768a3304e64": true,
				"0x216b4b4ba9f3e719726886d34a177484278bfcae": true,
				"0x6a000f20005980200259b80c5102003040001068": true,
				"0xeff92a263d31888d860bd50809a8d171709b7b1c": true,
				"0x13f4ea83d0bd40e75c8222255bc855a974568dd4": true,
			},

			56: {
				"0x3fc91a3afd70395cd496c647d5a6cc9d4b2b7fad": true,
				"0x000000000022d473030f116ddee9f6b43ac78ba3": true,
				"0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae": true,
				"0x8b742329ca21f0bce32a2f95fd43ef2c6afb52f4": true,
				"0x6131b5fae19ea4f9d964eac0408e4408b66337b5": true,
				"0x89b8aa89fdd0507a99d334cbe3c808fafc7d850e": true,
				"0x6352a56caadc4f1e25cd6c75970fa768a3304e64": true,
				"0x6a000f20005980200259b80c5102003040001068": true,
				"0x10ed43c718714eb63d5aa57b78b54704e256024e": true,
				"0x13f4ea83d0bd40e75c8222255bc855a974568dd4": true,
			},

			137: {
				"0x3fc91a3afd70395cd496c647d5a6cc9d4b2b7fad": true,
				"0x000000000022d473030f116ddee9f6b43ac78ba3": true,
				"0xe592427a0aece92de3edee1f18e0157c05861564": true,
				"0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae": true,
				"0x8b742329ca21f0bce32a2f95fd43ef2c6afb52f4": true,
				"0x6131b5fae19ea4f9d964eac0408e4408b66337b5": true,
				"0x4e3288c9ca110bcc82bf38f09a7b425c095d92bf": true,
				"0x6352a56caadc4f1e25cd6c75970fa768a3304e64": true,
				"0x6a000f20005980200259b80c5102003040001068": true,
			},

			8453: {
				"0x3fc91a3afd70395cd496c647d5a6cc9d4b2b7fad": true,
				"0x000000000022d473030f116ddee9f6b43ac78ba3": true,
				"0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae": true,
				"0x8b742329ca21f0bce32a2f95fd43ef2c6afb52f4": true,
				"0x2a0c0dbecc7e4d658f48e01e3fa353f44050c208": true,
				"0x6131b5fae19ea4f9d964eac0408e4408b66337b5": true,
				"0x19ceead7105607cd444f5ad10dd51356436095a1": true,
				"0x6352a56caadc4f1e25cd6c75970fa768a3304e64": true,
				"0x6a000f20005980200259b80c5102003040001068": true,
				"0x8cfe327cec66d1c090dd72bd0ff11d690c33a2eb": true,
				"0x678aa4bf4e210cf2166753e054d5b7c31cc7fa86": true,
			},
		},
	}
}
