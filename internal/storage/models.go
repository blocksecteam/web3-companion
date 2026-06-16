// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package storage

import (
	"time"

	"github.com/blocksecteam/web3-companion/internal/money"
)

type User struct {
	ID                 string     `gorm:"primaryKey;size:32" json:"id"`
	Email              string     `gorm:"uniqueIndex;size:128;not null" json:"email"`
	TempToken          string     `gorm:"size:64" json:"-"`
	TempTokenExpiresAt *time.Time `json:"-"`
	PasskeySetup       bool       `gorm:"default:false" json:"passkey_setup"`
	Name               string     `gorm:"size:128" json:"name"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type Wallet struct {
	UserID          string     `gorm:"primaryKey;size:32" json:"user_id"`
	WalletID        string     `gorm:"primaryKey;size:64" json:"wallet_id"`
	DisplayWalletID string     `gorm:"index;size:64" json:"display_wallet_id,omitempty"`
	WalletUID       string     `gorm:"uniqueIndex;size:32" json:"wallet_uid"`
	Generation      int        `gorm:"not null;default:1" json:"generation"`
	Address         string     `gorm:"not null;size:42" json:"address"`
	KeyFile         string     `gorm:"not null;size:512" json:"key_file"`
	KeyStatus       string     `gorm:"not null;default:active;size:32" json:"key_status"`
	IsDefault       bool       `gorm:"index;default:false" json:"is_default"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	DeletedAt       *time.Time `json:"deleted_at,omitempty"`
}

type WalletDelegation struct {
	WalletUID      string     `gorm:"primaryKey;size:32" json:"wallet_uid"`
	Chain          string     `gorm:"primaryKey;size:32" json:"chain"`
	UserID         string     `gorm:"index;size:32;not null" json:"user_id"`
	WalletID       string     `gorm:"index;size:64;not null" json:"wallet_id"`
	Delegated      bool       `gorm:"index;default:false" json:"delegated"`
	Delegator      string     `gorm:"size:42" json:"delegator,omitempty"`
	Status         string     `gorm:"size:32;not null;default:not_delegated" json:"status"`
	DelegationHash string     `gorm:"size:66" json:"delegation_hash,omitempty"`
	DelegatedAt    *time.Time `json:"delegated_at,omitempty"`
	LastCheckedAt  *time.Time `gorm:"index" json:"last_checked_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type PolicyDefinition struct {
	PolicyDefinitionID string     `gorm:"primaryKey;size:32" json:"policy_definition_id"`
	UserID             string     `gorm:"index;not null;size:32" json:"user_id"`
	Name               string     `gorm:"not null;size:128" json:"name"`
	IsDefault          bool       `gorm:"index;default:false" json:"is_default"`
	CurrentVersionID   string     `gorm:"size:32" json:"current_version_id"`
	DeletedAt          *time.Time `gorm:"index" json:"deleted_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type PolicyDefinitionVersion struct {
	PolicyDefinitionVersionID string       `gorm:"primaryKey;size:32" json:"policy_definition_version_id"`
	PolicyDefinitionID        string       `gorm:"index;not null;size:32" json:"policy_definition_id"`
	UserID                    string       `gorm:"index;not null;size:32" json:"user_id"`
	Version                   int          `gorm:"not null" json:"version"`
	Mode                      string       `gorm:"default:manual;size:16" json:"mode"`
	MaxPerTransactionUSD      money.Amount `gorm:"type:text;not null;default:'0'" json:"max_per_transaction_usd"`
	DailyTransferLimitUSD     money.Amount `gorm:"type:text;not null;default:'0'" json:"daily_transfer_limit_usd"`
	DailyTradeLimitUSD        money.Amount `gorm:"type:text;not null;default:'0'" json:"daily_trade_limit_usd"`
	AutoSignEnabled           bool         `gorm:"default:false" json:"auto_sign_enabled"`
	AutoSignMaxPerTxUSD       money.Amount `gorm:"type:text;not null;default:'0'" json:"auto_sign_max_per_tx_usd"`
	AutoSignDailyLimitUSD     money.Amount `gorm:"type:text;not null;default:'0'" json:"auto_sign_daily_limit_usd"`
	AutoSignMaxCountDay       int          `gorm:"default:0" json:"auto_sign_max_count_day"`
	RecipientWhitelistEnabled bool         `gorm:"default:false" json:"recipient_whitelist_enabled"`
	RecipientWhitelistAddrs   string       `gorm:"type:text" json:"recipient_whitelist_addrs"`
	CreatedAt                 time.Time    `json:"created_at"`
}

type PolicyInstance struct {
	PolicyInstanceID          string       `gorm:"primaryKey;size:32" json:"policy_instance_id"`
	UserID                    string       `gorm:"uniqueIndex:idx_policy_instance_user_wallet;not null;size:32" json:"user_id"`
	WalletID                  string       `gorm:"uniqueIndex:idx_policy_instance_user_wallet;not null;size:64" json:"wallet_id"`
	DisplayWalletID           string       `gorm:"index;size:64" json:"display_wallet_id,omitempty"`
	WalletUID                 string       `gorm:"index;size:32" json:"wallet_uid,omitempty"`
	WalletGeneration          int          `gorm:"default:0" json:"wallet_generation,omitempty"`
	PolicyDefinitionID        string       `gorm:"index;not null;size:32" json:"policy_definition_id"`
	PolicyDefinitionVersionID string       `gorm:"index;not null;size:32" json:"policy_definition_version_id"`
	DailyTransferUsedUSD      money.Amount `gorm:"type:text;not null;default:'0'" json:"daily_transfer_used_usd"`
	DailyTradeUsedUSD         money.Amount `gorm:"type:text;not null;default:'0'" json:"daily_trade_used_usd"`
	AutoSignDailyUsedUSD      money.Amount `gorm:"type:text;not null;default:'0'" json:"auto_sign_daily_used_usd"`
	AutoSignCountToday        int          `gorm:"default:0" json:"auto_sign_count_today"`
	LastResetDate             string       `gorm:"size:10" json:"last_reset_date"`
	CreatedAt                 time.Time    `json:"created_at"`
	UpdatedAt                 time.Time    `json:"updated_at"`
}

type WebAuthnCredential struct {
	ID           uint      `gorm:"primaryKey;autoIncrement" json:"-"`
	CredentialID string    `gorm:"uniqueIndex;size:255;not null" json:"credential_id"`
	UserID       string    `gorm:"index;not null;size:32" json:"user_id"`
	PublicKey    []byte    `gorm:"not null" json:"-"`
	Name         string    `gorm:"size:128" json:"name"`
	DeviceName   string    `gorm:"size:128" json:"device_name"`
	AAGUID       string    `gorm:"size:36" json:"aaguid"`
	SignCount    uint32    `gorm:"default:0" json:"sign_count"`
	CreatedAt    time.Time `json:"created_at"`
	LastUsedAt   time.Time `json:"last_used_at"`
}

type Challenge struct {
	ChallengeID string    `gorm:"primaryKey;size:32" json:"challenge_id"`
	UserID      string    `gorm:"index;not null;size:32" json:"user_id"`
	Type        string    `gorm:"not null;size:32" json:"type"`
	Status      string    `gorm:"default:pending;size:16" json:"status"`
	PayloadJSON string    `gorm:"column:payload;type:text" json:"payload"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type AuditLog struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"-"`
	EventID     string    `gorm:"uniqueIndex;size:36;not null" json:"event_id"`
	EventType   string    `gorm:"index;not null;size:32" json:"event_type"`
	Severity    string    `gorm:"not null;size:16" json:"severity"`
	UserID      string    `gorm:"index;size:32" json:"user_id"`
	WalletID    string    `gorm:"index;size:64" json:"wallet_id"`
	WalletUID   string    `gorm:"index;size:32" json:"wallet_uid"`
	ChallengeID string    `gorm:"size:32" json:"challenge_id"`
	RequestID   string    `gorm:"size:36" json:"request_id"`
	DataJSON    string    `gorm:"column:data;type:text;not null" json:"data"`
	CreatedAt   time.Time `gorm:"index" json:"created_at"`
}

type Transaction struct {
	Hash              string       `gorm:"primaryKey;size:66" json:"hash"`
	Chain             string       `gorm:"primaryKey;index;size:32" json:"chain"`
	UserID            string       `gorm:"index;not null;size:32" json:"user_id"`
	WalletID          string       `gorm:"index;size:64" json:"wallet_id"`
	WalletUID         string       `gorm:"index;size:32" json:"wallet_uid"`
	Status            string       `gorm:"not null;size:16" json:"status"`
	TxType            string       `gorm:"size:32" json:"tx_type"`
	AmountUSD         money.Amount `gorm:"type:text;not null;default:'0'" json:"amount_usd"`
	PolicyID          string       `gorm:"index;size:32" json:"policy_id"`
	PolicyDecision    string       `gorm:"size:16" json:"policy_decision"`
	PolicyUsageStatus string       `gorm:"size:16" json:"policy_usage_status"`
	TxInfo            string       `gorm:"column:tx_info;type:text" json:"tx_info,omitempty"`
	SubmittedAt       *time.Time   `json:"submitted_at"`
	ConfirmedAt       *time.Time   `json:"confirmed_at"`
}

type TransactionIntent struct {
	TransactionID string `gorm:"primaryKey;size:32" json:"transaction_id"`
	UserID        string `gorm:"index;not null;size:32" json:"user_id"`
	WalletID      string `gorm:"index;not null;size:64" json:"wallet_id"`
	WalletUID     string `gorm:"index;size:32" json:"wallet_uid"`
	Chain         string `gorm:"index;not null;size:32" json:"chain"`
	ChainID       int64  `gorm:"not null" json:"chain_id"`
	TxType        string `gorm:"index;not null;size:32" json:"tx_type"`
	Owner         string `gorm:"size:42" json:"owner"`
	To            string `gorm:"not null;size:42" json:"to"`
	Value         string `gorm:"not null;size:128" json:"value"`
	Data          string `gorm:"type:text" json:"data"`
	GasLimit      uint64 `gorm:"not null" json:"gas_limit"`
	// GasPrice is the effective gas price (wei) the build_handler observed
	// from the chain at build time. It feeds the security simulator so
	// gas_cost_sanity can evaluate the fee against MaxGasUSD. The signer
	// fetches its own live fee at sign time and does NOT read this field.
	GasPrice string `gorm:"size:64" json:"gas_price,omitempty"`
	// GasPriceSource records how GasPrice was obtained: "rpc" (live node),
	// "fallback" (RPC unreachable, used package fallback constants), or
	// "" (legacy / not populated). Surfaced through CheckRequest so the
	// security check can downgrade pass→warn when the fee is fallback-
	// estimated even within the USD cap.
	GasPriceSource      string     `gorm:"size:16" json:"gas_price_source,omitempty"`
	Recipient           string     `gorm:"size:42" json:"recipient"`
	SummaryJSON         string     `gorm:"column:summary;type:text" json:"summary,omitempty"`
	PayloadHash         string     `gorm:"index;not null;size:64" json:"payload_hash"`
	SecurityVerdict     string     `gorm:"size:16" json:"security_verdict,omitempty"`
	SecurityResultJSON  string     `gorm:"column:security_result;type:text" json:"security_result,omitempty"`
	SecurityConfirmedAt *time.Time `json:"security_confirmed_at,omitempty"`
	PolicyID            string     `gorm:"index;size:32" json:"policy_id,omitempty"`
	PolicyDecision      string     `gorm:"size:16" json:"policy_decision,omitempty"`
	ChallengeID         string     `gorm:"index;size:32" json:"challenge_id,omitempty"`
	TxHash              string     `gorm:"index;size:66" json:"tx_hash,omitempty"`
	SignedTxEncrypted   []byte     `gorm:"column:signed_tx_encrypted" json:"-"`
	SignedTxDataKey     []byte     `gorm:"column:signed_tx_data_key" json:"-"`
	Status              string     `gorm:"index;not null;size:32" json:"status"`
	ExpiresAt           time.Time  `gorm:"index;not null" json:"expires_at"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	SubmittedAt         *time.Time `json:"submitted_at,omitempty"`
}

type PolicyUsageEvent struct {
	EventID      string       `gorm:"primaryKey;size:32" json:"event_id"`
	UserID       string       `gorm:"index;not null;size:32" json:"user_id"`
	WalletID     string       `gorm:"index;not null;size:64" json:"wallet_id"`
	WalletUID    string       `gorm:"index;size:32" json:"wallet_uid"`
	PolicyID     string       `gorm:"index;not null;size:32" json:"policy_id"`
	Chain        string       `gorm:"index:idx_policy_usage_tx,unique;size:32;not null" json:"chain"`
	TxHash       string       `gorm:"index:idx_policy_usage_tx,unique;size:66;not null" json:"tx_hash"`
	TxType       string       `gorm:"size:32;not null" json:"tx_type"`
	AmountUSD    money.Amount `gorm:"type:text;not null;default:'0'" json:"amount_usd"`
	PriceUnknown bool         `gorm:"default:false" json:"price_unknown"`
	Decision     string       `gorm:"size:16;not null" json:"decision"`
	Status       string       `gorm:"index;size:16;not null" json:"status"`
	UsageDate    string       `gorm:"index;size:10;not null" json:"usage_date"`
	ExpiresAt    *time.Time   `json:"expires_at,omitempty"`
	SubmittedAt  *time.Time   `json:"submitted_at,omitempty"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

type AuthSession struct {
	SessionID        string     `gorm:"primaryKey;size:32" json:"session_id"`
	UserID           string     `gorm:"index;not null;size:32" json:"user_id"`
	ClientType       string     `gorm:"index;not null;size:16" json:"client_type"`
	Scope            string     `gorm:"size:64" json:"scope"`
	CredentialID     string     `gorm:"size:255" json:"credential_id"`
	RefreshTokenHash string     `gorm:"uniqueIndex;size:64;not null" json:"-"`
	CreatedAt        time.Time  `json:"created_at"`
	ExpiresAt        time.Time  `gorm:"index;not null" json:"expires_at"`
	LastUsedAt       *time.Time `json:"last_used_at,omitempty"`
	RevokedAt        *time.Time `gorm:"index" json:"revoked_at,omitempty"`
}

type ContainerCredential struct {
	CredentialID string     `gorm:"primaryKey;size:32" json:"credential_id"`
	UserID       string     `gorm:"index;not null;size:32" json:"user_id"`
	SecretHash   string     `gorm:"uniqueIndex;size:64;not null" json:"-"`
	Scope        string     `gorm:"size:64;not null" json:"scope"`
	CreatedAt    time.Time  `json:"created_at"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
	RevokedAt    *time.Time `gorm:"index" json:"revoked_at,omitempty"`
}

type WebSocketTicket struct {
	TicketHash    string     `gorm:"primaryKey;size:64" json:"-"`
	UserID        string     `gorm:"index;not null;size:32" json:"user_id"`
	AuthSessionID string     `gorm:"index;not null;size:32" json:"auth_session_id"`
	ChatSessionID string     `gorm:"index;not null;size:128" json:"chat_session_id"`
	CreatedAt     time.Time  `json:"created_at"`
	ExpiresAt     time.Time  `gorm:"index;not null" json:"expires_at"`
	ConsumedAt    *time.Time `gorm:"index" json:"consumed_at,omitempty"`
	RevokedAt     *time.Time `gorm:"index" json:"revoked_at,omitempty"`
}

type WalletAssetSnapshot struct {
	ID         uint         `gorm:"primaryKey;autoIncrement" json:"-"`
	UserID     string       `gorm:"index;not null;size:32" json:"user_id"`
	WalletID   string       `gorm:"index;not null;size:64" json:"wallet_id"`
	WalletUID  string       `gorm:"index;size:32" json:"wallet_uid"`
	TotalUSD   money.Amount `gorm:"type:text;not null;default:'0'" json:"total_usd"`
	ObservedAt time.Time    `json:"observed_at"`
	CreatedAt  time.Time    `json:"created_at"`
}

func (User) TableName() string             { return "users" }
func (Wallet) TableName() string           { return "wallets" }
func (WalletDelegation) TableName() string { return "wallet_delegations" }
func (PolicyDefinition) TableName() string { return "policy_definitions" }
func (PolicyDefinitionVersion) TableName() string {
	return "policy_definition_versions"
}
func (PolicyInstance) TableName() string      { return "policy_instances" }
func (WebAuthnCredential) TableName() string  { return "webauthn_credentials" }
func (Challenge) TableName() string           { return "challenges" }
func (AuditLog) TableName() string            { return "audit_log" }
func (Transaction) TableName() string         { return "transactions" }
func (TransactionIntent) TableName() string   { return "transaction_intents" }
func (PolicyUsageEvent) TableName() string    { return "policy_usage_events" }
func (AuthSession) TableName() string         { return "auth_sessions" }
func (ContainerCredential) TableName() string { return "container_credentials" }
func (WebSocketTicket) TableName() string     { return "websocket_tickets" }
func (WalletAssetSnapshot) TableName() string { return "wallet_asset_snapshots" }

type AppSetting struct {
	Key       string `gorm:"primaryKey;size:64"`
	Value     string `gorm:"type:text;not null"`
	UpdatedAt time.Time
}

func (AppSetting) TableName() string { return "app_settings" }
