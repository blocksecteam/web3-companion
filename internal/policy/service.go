// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package policy

import (
	"crypto/rand"
	"encoding/json"
	"errors"

	"github.com/blocksecteam/web3-companion/internal/money"
	"gorm.io/gorm"
	"math/big"
	"strings"
	"time"
)

var (
	ErrNoPolicy                   = errors.New("no active signing policy")
	ErrPolicyBlocked              = errors.New("policy blocked signing request")
	ErrSignedOperationNotFound    = errors.New("signed operation not found")
	ErrDefaultPolicyDeleteBlocked = errors.New("default policy cannot be deleted")
	ErrPolicyDefinitionInUse      = errors.New("policy definition is bound to one or more wallets")
)

type Decision string

const (
	DecisionAuto     Decision = "auto"
	DecisionWebAuthn Decision = "webauthn"
	DecisionReject   Decision = "reject"
)

const (
	StatusSigned    = "signed"
	StatusSubmitted = "submitted"
	StatusConfirmed = "confirmed"
	// StatusFailed is reserved — no current producer writes "failed" to
	// PolicyUsageEvent.Status. Kept as a sentinel for a future terminal
	// failure state so producers can reference this constant rather than
	// inventing a new literal.
	StatusFailed  = "failed"
	StatusExpired = "expired"
)

type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

func NewEngine(db *gorm.DB) *Service {
	return NewService(db)
}
func (s *Service) Evaluate(req Request) (*Result, error) {
	if req.Now.IsZero() {
		req.Now = time.Now()
	}
	p, err := s.CurrentPolicy(req.UserID, req.WalletID, req.Now)
	if err != nil {
		return nil, err
	}
	usage, err := s.UsageForPolicy(p.PolicyID, req.Now)
	if err != nil {
		return nil, err
	}
	if req.PriceUnknown {
		return &Result{Decision: DecisionWebAuthn, Policy: p, Reason: "token price unknown"}, nil
	}
	if limitExceeded(req.AmountUSD, p.MaxPerTransactionUSD) {
		return &Result{Decision: DecisionReject, Policy: p, Reason: "per-transaction limit exceeded"}, ErrPolicyBlocked
	}
	if transferBucket(req.TxType) && sumLimitExceeded(p.DailyTransferLimitUSD, usage.DailyTransferUsedUSD, usage.PendingTransferUSD, req.AmountUSD) {
		return &Result{Decision: DecisionReject, Policy: p, Reason: "daily transfer limit exceeded"}, ErrPolicyBlocked
	}
	if tradeBucket(req.TxType) && sumLimitExceeded(p.DailyTradeLimitUSD, usage.DailyTradeUsedUSD, usage.PendingTradeUSD, req.AmountUSD) {
		return &Result{Decision: DecisionReject, Policy: p, Reason: "daily trade limit exceeded"}, ErrPolicyBlocked
	}
	// Recipient whitelist gates only chain transfer. swap routes through a
	// DEX router (not a meaningful "recipient" for whitelist semantics);
	// bridge always sends back to the source wallet on the destination chain
	// (see internal/bridge/adapters/common.go:receiveAddress). Other policy
	// rules (per-tx limit, daily limits) still apply to swap and bridge.
	if transferBucket(req.TxType) && p.RecipientWhitelistEnabled && !recipientAllowed(req.Recipient, p.RecipientWhitelistAddrs) {
		return &Result{Decision: DecisionWebAuthn, Policy: p, Reason: "recipient not whitelisted"}, nil
	}
	if p.Mode != "auto" || !p.AutoSignEnabled {
		return &Result{Decision: DecisionWebAuthn, Policy: p, Reason: "manual confirmation required"}, nil
	}
	if limitExceeded(req.AmountUSD, p.AutoSignMaxPerTxUSD) {
		return &Result{Decision: DecisionWebAuthn, Policy: p, Reason: "auto-sign per-transaction limit exceeded"}, nil
	}
	if sumLimitExceeded(p.AutoSignDailyLimitUSD, usage.AutoSignDailyUsedUSD, usage.AutoSignPendingUSD, req.AmountUSD) {
		return &Result{Decision: DecisionWebAuthn, Policy: p, Reason: "auto-sign daily limit exceeded"}, nil
	}
	if p.AutoSignMaxCountDay > 0 && usage.AutoSignCountToday+usage.AutoSignPendingCount >= p.AutoSignMaxCountDay {
		return &Result{Decision: DecisionWebAuthn, Policy: p, Reason: "auto-sign count limit exceeded"}, nil
	}
	return &Result{Decision: DecisionAuto, Policy: p, Reason: "within auto-sign policy"}, nil
}
func DecodeAddressList(raw string) []string {
	var addrs []string
	if err := json.Unmarshal([]byte(raw), &addrs); err != nil {
		addrs = strings.Split(raw, ",")
	}
	return NormalizeAddressList(addrs)
}

func normalizeChain(chain string) string {
	return strings.ToLower(strings.TrimSpace(chain))
}

func NormalizeAddressList(addrs []string) []string {
	out := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		addr = strings.TrimSpace(addr)
		if addr != "" {
			out = append(out, addr)
		}
	}
	return out
}

func limitExceeded(value, limit money.Amount) bool {
	return limit.Sign() > 0 && value.GreaterThan(limit)
}

func sumLimitExceeded(limit money.Amount, values ...money.Amount) bool {
	if limit.Sign() <= 0 {
		return false
	}
	return money.Sum(values...).GreaterThan(limit)
}

func transferBucket(txType string) bool {
	return txType == "transfer"
}

func tradeBucket(txType string) bool {
	return txType != "transfer" && txType != "delegate"
}

func recipientAllowed(recipient, whitelist string) bool {
	recipient = strings.ToLower(strings.TrimSpace(recipient))
	if recipient == "" {
		return false
	}
	for _, addr := range DecodeAddressList(whitelist) {
		if strings.EqualFold(strings.TrimSpace(addr), recipient) {
			return true
		}
	}
	return false
}

func randomID(n int) string {
	const hexChars = "0123456789abcdef"
	b := make([]byte, n)
	for i := range b {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(hexChars))))
		if err != nil {
			b[i] = hexChars[i%len(hexChars)]
			continue
		}
		b[i] = hexChars[idx.Int64()]
	}
	return string(b)
}
