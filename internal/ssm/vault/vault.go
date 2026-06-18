// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package vault

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/blocksecteam/web3-companion/internal/config"

	"github.com/blocksecteam/web3-companion/internal/storage"
	"gorm.io/gorm"
	"os"
	"path/filepath"

	"regexp"
	"strings"
	"sync"
	"time"
)

const MMDelegatorAddress = "0x63c0c19a282a1b52b07dd5a65b58948a07dae32b"

var (
	ErrWalletNotFound    = errors.New("wallet not found")
	ErrInvalidKeyFile    = errors.New("invalid key file")
	ErrNotDelegated      = errors.New("wallet is not EIP-7702 delegated")
	ErrWrongDelegator    = errors.New("wallet is delegated to an unsupported EIP-7702 delegator")
	ErrInsufficientGas   = errors.New("insufficient gas for delegation")
	ErrDelegationPending = errors.New("delegation transaction pending")
)

var DelegationReceiptWaitTimeout = 2 * time.Minute

type DelegationResult struct {
	WalletID       string `json:"wallet_id"`
	Address        string `json:"address"`
	Delegated      bool   `json:"delegated"`
	Status         string `json:"status,omitempty"`
	Delegator      string `json:"delegator,omitempty"`
	DelegationHash string `json:"delegation_hash,omitempty"`
	NeedsGas       bool   `json:"needs_gas"`
	GasEstimate    string `json:"gas_estimate,omitempty"`
	// CurrentBalance is the wallet's native-token balance (wei) at the time
	// of the precheck. Populated by CheckDelegation; empty on result paths
	// that didn't read it.
	CurrentBalance string `json:"current_balance,omitempty"`
	// GasEstimateSource records how GasEstimate was computed:
	//   - "rpc"      : eth_estimateGas returned a live limit and gas price
	//                  came from the live baseFee + tip
	//   - "fallback" : either gas-price RPC failed (offline 26 gwei) or
	//                  estimateGas failed (offline 100000 limit). The
	//                  displayed figure may be inaccurate; surface this to
	//                  the user so they don't take it as authoritative.
	GasEstimateSource string `json:"gas_estimate_source,omitempty"`
}

type keyFileContent struct {
	Version    int    `json:"version"`
	Address    string `json:"address"`
	Encrypted  []byte `json:"encrypted_private_key"`
	DataKeyEnc []byte `json:"encrypted_data_key"`
}

type Vault struct {
	kms       *KMSClient
	db        *gorm.DB
	keysDir   string
	rpcURLs   map[string]string
	chainIDs  map[string]int
	cfg       *config.Config
	delegator string
	mu        sync.Mutex
}

func New(kms *KMSClient, db *gorm.DB, keysDir string, rpcURLs map[string]string, chainIDs map[string]int) *Vault {
	if rpcURLs == nil {
		rpcURLs = make(map[string]string)
	}
	if chainIDs == nil {
		chainIDs = make(map[string]int)
	}
	if kms == nil {
		kms = NewTestKMSClient()
	}
	return &Vault{kms: kms, db: db, keysDir: keysDir, rpcURLs: rpcURLs, chainIDs: chainIDs, delegator: MMDelegatorAddress}
}

func (v *Vault) KMSClient() *KMSClient {
	return v.kms
}

func (v *Vault) resolveRPCURL(chainName string) (string, bool) {
	if url, ok := v.rpcURLs[chainName]; ok && url != "" {
		return url, true
	}
	if v.cfg != nil {
		if ch := v.cfg.ChainByName(chainName); ch != nil && strings.TrimSpace(ch.RPCURL) != "" && !strings.HasPrefix(ch.RPCURL, "${") {
			v.rpcURLs[chainName] = ch.RPCURL
			return ch.RPCURL, true
		}
	}
	return "", false
}
func NewConfigVault(ctx context.Context, cfg *config.Config, db *gorm.DB) (*Vault, error) {
	kmsClient, err := NewConfiguredKMSClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	rpcURLs := make(map[string]string)
	chainIDs := make(map[string]int)
	for _, ch := range cfg.ActiveChains() {
		rpcURLs[ch.Name] = ch.RPCURL
		chainIDs[ch.Name] = ch.ChainID
	}
	v := New(kmsClient, db, cfg.SSMKeysDir(), rpcURLs, chainIDs)
	v.cfg = cfg
	return v, nil
}

func NewConfiguredKMSClient(ctx context.Context, cfg *config.Config) (*KMSClient, error) {
	awsValues := []string{cfg.AWS.Region, cfg.AWS.KMSKeyID, cfg.AWS.AccessKeyID, cfg.AWS.SecretAccessKey}
	awsSet := 0
	for _, value := range awsValues {
		if strings.TrimSpace(value) != "" {
			awsSet++
		}
	}
	if awsSet == len(awsValues) {
		return NewKMSClientWithCredentials(ctx, cfg.AWS.Region, cfg.AWS.KMSKeyID, cfg.AWS.AccessKeyID, cfg.AWS.SecretAccessKey)
	}
	if awsSet > 0 {
		return nil, fmt.Errorf("incomplete AWS KMS configuration: set AWS_REGION, AWS_KMS_KEY_ID, AWS_ACCESS_KEY_ID, and AWS_SECRET_ACCESS_KEY, or unset all AWS KMS variables")
	}
	return NewEncryptedFileKMSClient(cfg.SSM.RootKey)
}
func (v *Vault) writeKeyFile(path string, kf *keyFileContent) error {
	os.MkdirAll(filepath.Dir(path), 0o700)
	data, _ := json.Marshal(kf)
	return os.WriteFile(path, data, 0o600)
}

func (v *Vault) readKeyFile(path string) (*keyFileContent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrWalletNotFound
		}
		return nil, err
	}
	var kf keyFileContent
	if err := json.Unmarshal(data, &kf); err != nil {
		return nil, ErrInvalidKeyFile
	}
	return &kf, nil
}

func (v *Vault) secureDelete(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	randomData := make([]byte, info.Size())
	if _, err := rand.Read(randomData); err != nil {
		return fmt.Errorf("secure random fill: %w", err)
	}
	if err := os.WriteFile(path, randomData, 0o600); err != nil {
		ZeroBytes(randomData)
		return err
	}
	ZeroBytes(randomData)
	return os.Remove(path)
}

var unsafeKeyPathChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func (v *Vault) keyFilePath(userID, walletID string) string {
	return filepath.Join(v.keysDir, safeKeyPathSegment(userID), safeKeyPathSegment(walletID)+".enc")
}

func (v *Vault) keyFilePathForGeneration(userID, walletID string, generation int) string {
	keyName := strings.TrimSpace(walletID)
	if generation > 1 {
		keyName = fmt.Sprintf("%s.g%d", keyName, generation)
	}
	return v.keyFilePath(userID, keyName)
}

func (v *Vault) metaFilePath(userID, walletID string) string {
	return filepath.Join(v.keysDir, safeKeyPathSegment(userID), safeKeyPathSegment(walletID)+".meta")
}

func (v *Vault) metaFilePathForGeneration(userID, walletID string, generation int) string {
	keyName := strings.TrimSpace(walletID)
	if generation > 1 {
		keyName = fmt.Sprintf("%s.g%d", keyName, generation)
	}
	return v.metaFilePath(userID, keyName)
}

func safeKeyPathSegment(raw string) string {
	trimmed := strings.TrimSpace(raw)
	safe := unsafeKeyPathChars.ReplaceAllString(trimmed, "_")
	safe = strings.Trim(safe, "._-")
	if safe == "" {
		safe = "wallet"
	}
	if len(safe) > 48 {
		safe = safe[:48]
	}
	sum := sha256.Sum256([]byte(trimmed))
	return safe + "-" + hex.EncodeToString(sum[:])[:12]
}

func storageWalletIDForGeneration(walletID string, generation int) string {
	walletID = strings.TrimSpace(walletID)
	if generation <= 1 {
		return walletID
	}
	return fmt.Sprintf("%s#%d", walletID, generation)
}

func walletDisplayID(wallet storage.Wallet) string {
	if strings.TrimSpace(wallet.DisplayWalletID) != "" {
		return strings.TrimSpace(wallet.DisplayWalletID)
	}
	return strings.TrimSpace(wallet.WalletID)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
