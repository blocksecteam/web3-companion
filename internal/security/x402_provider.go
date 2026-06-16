// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package security

import (
	"context"
	"crypto/ecdsa"
	crand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

const (
	x402BaseURL      = "https://x402.blocksec.ai"
	baseChainID      = 8453
	baseUSDCAddress  = "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"
	baseUSDCName     = "USD Coin"
	baseUSDCVersion  = "2"
	baseUSDCDecimals = 6
)

var chainSlugMap = map[int]string{
	1:    "eth",
	8453: "base",
	56:   "bsc",
	137:  "polygon",
}

type X402WalletProvider interface {
	DefaultWalletKey(ctx context.Context, userID string) (*ecdsa.PrivateKey, string, error)
}

type X402LabelProvider struct {
	wallet X402WalletProvider
	client *http.Client
}

func NewX402LabelProviderWithWallet(wallet X402WalletProvider) *X402LabelProvider {
	return &X402LabelProvider{
		wallet: wallet,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *X402LabelProvider) Labels(ctx context.Context, chainID int, addresses []string) ([]AddressLabel, error) {
	if p == nil || p.wallet == nil {
		return nil, fmt.Errorf("x402 provider requires wallet access")
	}
	userID := auth.GetUserID(ctx)
	if userID == "" {
		return nil, fmt.Errorf("x402: no user context available")
	}

	slug, ok := chainSlugMap[chainID]
	if !ok {
		return nil, nil
	}

	var allLabels []AddressLabel
	for _, addr := range addresses {
		label, err := p.fetchLabel(ctx, slug, addr, userID)
		if err != nil {
			slog.Error("x402 label fetch failed", "address", addr, "chain", slug, "error", err)
			if errors.Is(err, ErrLabelPaymentFailed) {
				return nil, err
			}
			return nil, fmt.Errorf("%w: %v", ErrLabelPaymentFailed, err)
		}
		if label != nil {
			allLabels = append(allLabels, *label)
		}
	}
	return allLabels, nil
}

func (p *X402LabelProvider) fetchLabel(ctx context.Context, chain, address, userID string) (*AddressLabel, error) {
	url := fmt.Sprintf("%s/label/%s/%s", x402BaseURL, chain, strings.ToLower(address))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusPaymentRequired {
		return p.handlePayment(ctx, url, resp, userID)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("x402 label API error (HTTP %d)", resp.StatusCode)
	}
	return p.parseResponse(resp)
}

type x402PaymentRequired struct {
	X402Version int           `json:"x402Version"`
	Accepts     []x402Accepts `json:"accepts"`
	Resource    x402Resource  `json:"resource"`
}

type x402Resource struct {
	URL string `json:"url"`
}

type x402Accepts struct {
	Scheme            string    `json:"scheme"`
	Network           string    `json:"network"`
	Asset             string    `json:"asset"`
	Amount            string    `json:"amount"`
	PayTo             string    `json:"payTo"`
	MaxTimeoutSeconds int       `json:"maxTimeoutSeconds"`
	Extra             x402Extra `json:"extra"`
}

type x402Extra struct {
	Name                string `json:"name"`
	Version             string `json:"version"`
	AssetTransferMethod string `json:"assetTransferMethod"`
}

func (p *X402LabelProvider) handlePayment(ctx context.Context, url string, resp *http.Response, userID string) (*AddressLabel, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read 402 body: %w", err)
	}

	var payReq x402PaymentRequired
	if err := json.Unmarshal(body, &payReq); err != nil {
		headerVal := resp.Header.Get("Payment-Required")
		if headerVal == "" {
			headerVal = resp.Header.Get("X-Payment")
		}
		if headerVal != "" {
			decoded, decErr := base64.StdEncoding.DecodeString(headerVal)
			if decErr == nil {
				json.Unmarshal(decoded, &payReq)
			}
		}
	}

	if len(payReq.Accepts) == 0 {
		return nil, fmt.Errorf("%w: no payment options available", ErrLabelPaymentFailed)
	}

	accept := payReq.Accepts[0]
	if accept.Scheme != "exact" || !isBaseNetwork(accept.Network) {
		return nil, fmt.Errorf("%w: unsupported payment scheme %q on %q", ErrLabelPaymentFailed, accept.Scheme, accept.Network)
	}

	privateKey, walletAddr, err := p.wallet.DefaultWalletKey(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("%w: wallet access failed: %v", ErrLabelPaymentFailed, err)
	}
	defer WipeX402Key(privateKey)

	paymentPayload, err := buildEIP3009Payment(privateKey, walletAddr, accept)
	if err != nil {
		return nil, fmt.Errorf("%w: build payment signature: %v", ErrLabelPaymentFailed, err)
	}

	paymentJSON, _ := json.Marshal(paymentPayload)
	paymentHeader := base64.StdEncoding.EncodeToString(paymentJSON)

	retryReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLabelPaymentFailed, err)
	}
	if payReq.X402Version >= 2 {
		retryReq.Header.Set("Payment-Signature", paymentHeader)
	} else {
		retryReq.Header.Set("X-Payment", paymentHeader)
	}

	slog.Info("x402 payment sent", "url", url, "wallet", walletAddr, "amount", accept.Amount, "asset", accept.Asset)
	retryResp, err := p.client.Do(retryReq)
	if err != nil {
		return nil, fmt.Errorf("%w: paid request failed: %v", ErrLabelPaymentFailed, err)
	}
	defer retryResp.Body.Close()
	slog.Info("x402 payment response", "url", url, "status", retryResp.StatusCode)

	if retryResp.StatusCode == http.StatusPaymentRequired || retryResp.StatusCode == http.StatusForbidden {
		retryBody, _ := io.ReadAll(retryResp.Body)
		slog.Error("x402 payment rejected", "status", retryResp.StatusCode, "body", string(retryBody))
		return nil, fmt.Errorf("%w: insufficient USDC on Base or payment rejected (HTTP %d): %s", ErrLabelPaymentFailed, retryResp.StatusCode, string(retryBody))
	}
	if retryResp.StatusCode != http.StatusOK {
		retryBody, _ := io.ReadAll(retryResp.Body)
		slog.Error("x402 service error", "status", retryResp.StatusCode, "body", string(retryBody))
		return nil, fmt.Errorf("%w: service error (HTTP %d): %s", ErrLabelPaymentFailed, retryResp.StatusCode, string(retryBody))
	}
	respBody, err := io.ReadAll(retryResp.Body)
	if err != nil {
		return nil, fmt.Errorf("x402: read response: %w", err)
	}
	slog.Info("x402 label response", "url", url, "body", string(respBody[:min(len(respBody), 500)]))
	return p.parseResponseBody(respBody)
}

type x402PaymentPayload struct {
	X402Version int               `json:"x402Version"`
	Accepted    x402Accepts       `json:"accepted"`
	Payload     x402SignedPayload `json:"payload"`
}

type x402SignedPayload struct {
	Signature     string            `json:"signature"`
	Authorization x402Authorization `json:"authorization"`
}

type x402Authorization struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Value       string `json:"value"`
	ValidAfter  string `json:"validAfter"`
	ValidBefore string `json:"validBefore"`
	Nonce       string `json:"nonce"`
}

func buildEIP3009Payment(key *ecdsa.PrivateKey, walletAddr string, accept x402Accepts) (*x402PaymentPayload, error) {
	now := time.Now().Unix()
	validAfter := fmt.Sprintf("%d", now-60)
	validBefore := fmt.Sprintf("%d", now+int64(accept.MaxTimeoutSeconds))
	randomBytes := make([]byte, 32)
	crand.Read(randomBytes)
	nonce := crypto.Keccak256Hash(append([]byte(fmt.Sprintf("%s:%d:%s:", walletAddr, now, accept.PayTo)), randomBytes...))

	amountAtomic := strings.TrimSpace(accept.Amount)

	chainIDBig := (*math.HexOrDecimal256)(big.NewInt(baseChainID))
	domain := apitypes.TypedDataDomain{
		Name:              baseUSDCName,
		Version:           baseUSDCVersion,
		ChainId:           chainIDBig,
		VerifyingContract: baseUSDCAddress,
	}

	typedData := apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": {
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"TransferWithAuthorization": {
				{Name: "from", Type: "address"},
				{Name: "to", Type: "address"},
				{Name: "value", Type: "uint256"},
				{Name: "validAfter", Type: "uint256"},
				{Name: "validBefore", Type: "uint256"},
				{Name: "nonce", Type: "bytes32"},
			},
		},
		PrimaryType: "TransferWithAuthorization",
		Domain:      domain,
		Message: apitypes.TypedDataMessage{
			"from":        walletAddr,
			"to":          accept.PayTo,
			"value":       amountAtomic,
			"validAfter":  validAfter,
			"validBefore": validBefore,
			"nonce":       nonce.Hex(),
		},
	}

	hash, _, err := apitypes.TypedDataAndHash(typedData)
	if err != nil {
		return nil, fmt.Errorf("EIP-712 hash: %w", err)
	}

	sig, err := crypto.Sign(hash, key)
	if err != nil {
		return nil, fmt.Errorf("sign: %w", err)
	}
	if sig[64] < 27 {
		sig[64] += 27
	}

	return &x402PaymentPayload{
		X402Version: 2,
		Accepted:    accept,
		Payload: x402SignedPayload{
			Signature: fmt.Sprintf("0x%x", sig),
			Authorization: x402Authorization{
				From:        walletAddr,
				To:          accept.PayTo,
				Value:       amountAtomic,
				ValidAfter:  validAfter,
				ValidBefore: validBefore,
				Nonce:       nonce.Hex(),
			},
		},
	}, nil
}

func (p *X402LabelProvider) parseResponse(resp *http.Response) (*AddressLabel, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return p.parseResponseBody(body)
}

func (p *X402LabelProvider) parseResponseBody(body []byte) (*AddressLabel, error) {
	type labelData struct {
		Address        string       `json:"address"`
		ChainID        int          `json:"chain_id"`
		NameTag        string       `json:"name_tag"`
		MainEntity     string       `json:"main_entity"`
		CompEntity     string       `json:"comp_entity"`
		Attributes     []LabelValue `json:"attributes"`
		MainEntityInfo *struct {
			Entity     string       `json:"entity"`
			Categories []LabelValue `json:"categories"`
			Attributes []LabelValue `json:"attributes"`
		} `json:"main_entity_info"`
		CompEntityInfo *struct {
			Entity     string       `json:"entity"`
			Categories []LabelValue `json:"categories"`
			Attributes []LabelValue `json:"attributes"`
		} `json:"comp_entity_info"`
		CompEntities []struct {
			Entity     string       `json:"entity"`
			Categories []LabelValue `json:"categories"`
			Attributes []LabelValue `json:"attributes"`
		} `json:"comp_entities"`
	}

	var envelope struct {
		Code    int       `json:"code"`
		Message string    `json:"message"`
		Data    labelData `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("parse label response: %w", err)
	}
	raw := envelope.Data
	if envelope.Code != 0 && envelope.Code != 200000 {
		return nil, fmt.Errorf("x402 label API error %d: %s", envelope.Code, envelope.Message)
	}

	if raw.Address == "" {
		return nil, nil
	}

	mainEntity := raw.MainEntity
	if raw.MainEntityInfo != nil && mainEntity == "" {
		mainEntity = raw.MainEntityInfo.Entity
	}
	compEntity := raw.CompEntity
	if raw.CompEntityInfo != nil && compEntity == "" {
		compEntity = raw.CompEntityInfo.Entity
	}
	label := AddressLabel{
		Address:    raw.Address,
		ChainID:    raw.ChainID,
		NameTag:    raw.NameTag,
		MainEntity: mainEntity,
		CompEntity: compEntity,
	}
	if raw.MainEntityInfo != nil {
		label.Categories = append(label.Categories, raw.MainEntityInfo.Categories...)
		label.Attributes = append(label.Attributes, raw.MainEntityInfo.Attributes...)
	}
	if raw.CompEntityInfo != nil {
		label.Categories = append(label.Categories, raw.CompEntityInfo.Categories...)
		label.Attributes = append(label.Attributes, raw.CompEntityInfo.Attributes...)
	}
	for _, ce := range raw.CompEntities {
		label.Categories = append(label.Categories, ce.Categories...)
		label.Attributes = append(label.Attributes, ce.Attributes...)
	}
	label.Attributes = append(label.Attributes, raw.Attributes...)
	label.RiskReasons = HighRiskLabelReasons(label)
	if len(label.RiskReasons) > 0 {
		label.RiskLevel = "high"
	}
	return &label, nil
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func isBaseNetwork(network string) bool {
	n := strings.ToLower(strings.TrimSpace(network))
	return n == "base" || n == "eip155:8453"
}

func WipeX402Key(key *ecdsa.PrivateKey) {
	if key == nil || key.D == nil {
		return
	}
	words := key.D.Bits()
	for i := range words {
		words[i] = 0
	}
	key.D.SetInt64(0)
}
