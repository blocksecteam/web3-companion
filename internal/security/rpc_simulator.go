// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package security

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"

	"github.com/blocksecteam/web3-companion/internal/security/primitives"
)

const (
	erc20TransferTopic      = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
	erc1155TransferSingle   = "0xc3d58168c5ae7397731d063d5bbf3d017d58f9ed087d3e5f8bdbf03e00d0b8f7"
	erc1155TransferBatch    = "0x4a39dc06d4c0dbc64b70b1b88bf38f98337cce2cba8a7dbde7b7fc4e2578f5"
	nativeAssetAddress      = "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	nativeAssetNameEthereum = "ETH"
	ercTokenNameSelector    = "0x06fdde03"
	ercTokenSymbolSelector  = "0x95d89b41"
)

type TokenValuator interface {
	TokenAmount(ctx context.Context, chain, tokenAddress, rawAmount string) (amount string, usdValue string, ok bool)
}

type RPCSimulator struct {
	rpcURLs     map[int]string
	rpcProvider func() map[int]string
	client      *http.Client
	valuator    TokenValuator
}

func NewRPCSimulator(rpcURLs map[int]string, client *http.Client, valuator TokenValuator) *RPCSimulator {
	return NewRPCSimulatorWithProvider(func() map[int]string {
		return rpcURLs
	}, client, valuator)
}

func NewRPCSimulatorWithProvider(rpcProvider func() map[int]string, client *http.Client, valuator TokenValuator) *RPCSimulator {
	if client == nil {
		client = http.DefaultClient
	}
	return &RPCSimulator{
		rpcProvider: rpcProvider,
		client:      client,
		valuator:    valuator,
	}
}

func (s *RPCSimulator) Simulate(ctx context.Context, req CheckRequest) (*SimulationResult, []string, error) {
	rpcURL := strings.TrimSpace(s.currentRPCURLs()[req.ChainID])
	if rpcURL == "" {
		return nil, nil, fmt.Errorf("rpc url is not configured for chain_id=%d", req.ChainID)
	}
	call, err := s.traceCall(ctx, rpcURL, req, "callTracer", map[string]interface{}{"withLog": true})
	if err != nil {
		return nil, nil, err
	}
	prestate, err := s.traceCall(ctx, rpcURL, req, "prestateTracer", map[string]interface{}{"diffMode": true, "disableCode": true, "disableStorage": true})
	if err != nil {
		return nil, nil, err
	}

	var trace rpcCall
	if err := json.Unmarshal(call, &trace); err != nil {
		return nil, nil, fmt.Errorf("rpc parse call trace: %w", err)
	}
	var diff rpcPrestateDiff
	if err := json.Unmarshal(prestate, &diff); err != nil {
		return nil, nil, fmt.Errorf("rpc parse prestate diff: %w", err)
	}

	gasUsed := hexToInt64(trace.GasUsed)
	gasPrice, _ := primitives.ParseInteger(req.GasPrice)
	fee := big.NewInt(0)
	if gasPrice != nil && gasUsed > 0 {
		fee.Mul(gasPrice, big.NewInt(gasUsed))
	}
	changes := s.balanceChangesFromTrace(ctx, rpcURL, req, trace, diff)
	normalizedTrace := normalizeRPCCall(trace)
	fundFlow := collectFundFlowAddresses(trace, req.Owner)
	data := &SimulationData{
		Status:          trace.Error == "",
		Sender:          req.Owner,
		Receiver:        req.To,
		CallData:        primitives.FirstString(req.Data, "0x"),
		GasLimit:        int64(req.GasLimit),
		GasPrice:        primitives.FirstString(req.GasPrice, "0"),
		GasUsed:         gasUsed,
		TransactionFee:  fee.String(),
		Value:           primitives.FirstString(req.Value, "0"),
		ErrorInfo:       trace.Error,
		BalanceChanges:  changes,
		Trace:           normalizedTrace,
		SimulationLabel: "rpc",
	}
	return &SimulationResult{Code: 0, Message: "ok", Data: data, Raw: simplifiedSimulationPayloadRaw(&SimulationResult{Data: data}, nil)}, fundFlow, nil
}

func (s *RPCSimulator) currentRPCURLs() map[int]string {
	if s == nil {
		return nil
	}
	if s.rpcProvider != nil {
		return s.rpcProvider()
	}
	return s.rpcURLs
}

func (s *RPCSimulator) traceCall(ctx context.Context, rpcURL string, req CheckRequest, tracer string, tracerConfig map[string]interface{}) (json.RawMessage, error) {
	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "debug_traceCall",
		"params": []interface{}{
			map[string]interface{}{
				"from":     req.Owner,
				"to":       req.To,
				"value":    intToHexString(primitives.FirstString(req.Value, "0")),
				"data":     primitives.FirstString(req.Data, "0x"),
				"gas":      uintToHex(req.GasLimit),
				"gasPrice": intToHexString(primitives.FirstString(req.GasPrice, "0")),
			},
			blockTag(req.BlockNumber),
			map[string]interface{}{
				"tracer":       tracer,
				"tracerConfig": tracerConfig,
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("rpc encode %s request: %w", tracer, err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, rpcURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("rpc create %s request: %w", tracer, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("rpc %s request: %w", tracer, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("rpc read %s response: %w", tracer, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("rpc %s status %d: %s", tracer, resp.StatusCode, string(respBody))
	}
	var parsed struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("rpc parse %s response: %w", tracer, err)
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("rpc %s error %d: %s", tracer, parsed.Error.Code, parsed.Error.Message)
	}
	if len(parsed.Result) == 0 || string(parsed.Result) == "null" {
		return nil, fmt.Errorf("rpc %s response missing result", tracer)
	}
	return parsed.Result, nil
}

func (s *RPCSimulator) balanceChangesFromTrace(ctx context.Context, rpcURL string, req CheckRequest, trace rpcCall, diff rpcPrestateDiff) []BalanceChange {
	assets := make([]BalanceChangeAsset, 0)
	metadata := make(map[string]tokenMetadata)
	for _, transfer := range collectTokenTransfers(trace) {
		if primitives.AddrEq(transfer.From, req.Owner) {
			assets = append(assets, s.assetEffect(ctx, rpcURL, req, transfer, false, metadata))
		}
		if primitives.AddrEq(transfer.To, req.Owner) {
			assets = append(assets, s.assetEffect(ctx, rpcURL, req, transfer, true, metadata))
		}
	}
	if native := s.nativeBalanceAsset(ctx, req, diff); native != nil {
		assets = append(assets, *native)
	}
	if len(assets) == 0 {
		// Return empty slice (not nil) so JSON encodes as [] (verified no
		// changes observed) rather than null (ambiguous: missing data).
		return []BalanceChange{}
	}
	return []BalanceChange{{Account: req.Owner, Assets: assets}}
}

func (s *RPCSimulator) assetEffect(ctx context.Context, rpcURL string, req CheckRequest, transfer tokenTransfer, receiving bool, metadata map[string]tokenMetadata) BalanceChangeAsset {
	raw := signedAmount(transfer.Amount, receiving)
	asset := BalanceChangeAsset{
		Address:   transfer.Token,
		TokenID:   transfer.TokenID,
		Type:      transfer.Type,
		BaseUnits: raw,
		Amount:    raw,
	}
	meta := s.tokenMetadata(ctx, rpcURL, transfer.Token, metadata)
	asset.TokenName = meta.Name
	asset.TokenSymbol = meta.Symbol
	if asset.Type == "erc20" && s.valuator != nil {
		if amount, usd, ok := s.valuator.TokenAmount(ctx, req.Chain, transfer.Token, transfer.Amount); ok {
			asset.Amount = signedAmount(amount, receiving)
			asset.Value = usd
		}
	}
	return asset
}

func (s *RPCSimulator) tokenMetadata(ctx context.Context, rpcURL, token string, cache map[string]tokenMetadata) tokenMetadata {
	key := strings.ToLower(strings.TrimSpace(token))
	if cached, ok := cache[key]; ok {
		return cached
	}
	meta := tokenMetadata{
		Name:   s.tokenMetadataString(ctx, rpcURL, token, ercTokenNameSelector),
		Symbol: s.tokenMetadataString(ctx, rpcURL, token, ercTokenSymbolSelector),
	}
	cache[key] = meta
	return meta
}

func (s *RPCSimulator) tokenMetadataString(ctx context.Context, rpcURL, token, selector string) string {
	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "eth_call",
		"params": []interface{}{
			map[string]string{"to": token, "data": selector},
			"latest",
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rpcURL, bytes.NewReader(body))
	if err != nil {
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ""
	}
	var parsed struct {
		Result string `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return ""
	}
	return decodeABIString(parsed.Result)
}

func collectTokenTransfers(call rpcCall) []tokenTransfer {
	out := make([]tokenTransfer, 0)
	for _, log := range call.Logs {
		if len(log.Topics) == 0 {
			continue
		}
		switch strings.ToLower(log.Topics[0]) {
		case erc20TransferTopic:
			out = append(out, erc20TransferFromLog(log)...)
		case erc1155TransferSingle:
			out = append(out, erc1155SingleFromLog(log)...)
		case erc1155TransferBatch:
			out = append(out, erc1155BatchFromLog(log)...)
		}
	}
	for _, child := range call.Calls {
		out = append(out, collectTokenTransfers(child)...)
	}
	return out
}

func erc20TransferFromLog(log rpcLog) []tokenTransfer {
	if len(log.Topics) == 3 {
		amount, ok := primitives.ParseInteger(log.Data)
		if !ok || amount.Sign() == 0 {
			return nil
		}
		return []tokenTransfer{{
			Token:  log.Address,
			From:   topicAddress(log.Topics[1]),
			To:     topicAddress(log.Topics[2]),
			Amount: amount.String(),
			Type:   "erc20",
		}}
	}
	if len(log.Topics) == 4 {
		tokenID, ok := primitives.ParseInteger(log.Topics[3])
		if !ok {
			return nil
		}
		return []tokenTransfer{{
			Token:   log.Address,
			From:    topicAddress(log.Topics[1]),
			To:      topicAddress(log.Topics[2]),
			TokenID: tokenID.String(),
			Amount:  "1",
			Type:    "erc721",
		}}
	}
	return nil
}

func erc1155SingleFromLog(log rpcLog) []tokenTransfer {
	if len(log.Topics) != 4 {
		return nil
	}
	words := abiWords(log.Data)
	if len(words) < 2 {
		return nil
	}
	tokenID := new(big.Int).SetBytes(words[0])
	amount := new(big.Int).SetBytes(words[1])
	if amount.Sign() == 0 {
		return nil
	}
	return []tokenTransfer{{
		Token:   log.Address,
		From:    topicAddress(log.Topics[2]),
		To:      topicAddress(log.Topics[3]),
		TokenID: tokenID.String(),
		Amount:  amount.String(),
		Type:    "erc1155",
	}}
}

func erc1155BatchFromLog(log rpcLog) []tokenTransfer {
	if len(log.Topics) != 4 {
		return nil
	}
	words := abiWords(log.Data)
	if len(words) < 4 {
		return nil
	}
	idsOffset := int(new(big.Int).SetBytes(words[0]).Int64() / 32)
	amountsOffset := int(new(big.Int).SetBytes(words[1]).Int64() / 32)
	if idsOffset < 0 || amountsOffset < 0 || idsOffset >= len(words) || amountsOffset >= len(words) {
		return nil
	}
	idCount := int(new(big.Int).SetBytes(words[idsOffset]).Int64())
	amountCount := int(new(big.Int).SetBytes(words[amountsOffset]).Int64())
	if idCount < 0 || amountCount < 0 || idCount != amountCount {
		return nil
	}
	if idsOffset+1+idCount > len(words) || amountsOffset+1+amountCount > len(words) {
		return nil
	}
	out := make([]tokenTransfer, 0, idCount)
	for i := 0; i < idCount; i++ {
		amount := new(big.Int).SetBytes(words[amountsOffset+1+i])
		if amount.Sign() == 0 {
			continue
		}
		tokenID := new(big.Int).SetBytes(words[idsOffset+1+i])
		out = append(out, tokenTransfer{
			Token:   log.Address,
			From:    topicAddress(log.Topics[2]),
			To:      topicAddress(log.Topics[3]),
			TokenID: tokenID.String(),
			Amount:  amount.String(),
			Type:    "erc1155",
		})
	}
	return out
}

func (s *RPCSimulator) nativeBalanceAsset(ctx context.Context, req CheckRequest, diff rpcPrestateDiff) *BalanceChangeAsset {
	key := strings.ToLower(strings.TrimSpace(req.Owner))
	pre, ok := diff.Pre[key]
	if !ok {
		return nil
	}
	post, postExists := diff.Post[key]
	preBalance, ok := primitives.ParseInteger(pre.Balance)
	if !ok {
		return nil
	}
	if !postExists || strings.TrimSpace(post.Balance) == "" {
		return nil
	}
	postBalance, ok := primitives.ParseInteger(post.Balance)
	if !ok {
		return nil
	}
	delta := new(big.Int).Sub(postBalance, preBalance)
	if delta.Sign() == 0 {
		return nil
	}
	asset := &BalanceChangeAsset{
		Address:     nativeAssetAddress,
		TokenName:   nativeAssetNameEthereum,
		TokenSymbol: nativeAssetNameEthereum,
		Type:        "native",
		BaseUnits:   delta.String(),
		Amount:      delta.String(),
	}
	if s.valuator != nil {
		if amount, usd, ok := s.valuator.TokenAmount(ctx, req.Chain, nativeAssetAddress, absBigIntString(delta)); ok {
			receiving := delta.Sign() >= 0
			asset.Amount = signedAmount(amount, receiving)
			asset.Value = usd
		}
	}
	return asset
}

func absBigIntString(value *big.Int) string {
	if value == nil {
		return "0"
	}
	out := new(big.Int).Set(value)
	if out.Sign() < 0 {
		out.Neg(out)
	}
	return out.String()
}

func signedAmount(amount string, receiving bool) string {
	n, ok := primitives.ParseInteger(amount)
	if !ok {
		trimmed := strings.TrimSpace(amount)
		if receiving || strings.HasPrefix(trimmed, "-") {
			return trimmed
		}
		return "-" + trimmed
	}
	if n.Sign() < 0 {
		n.Neg(n)
	}
	if !receiving && n.Sign() != 0 {
		n.Neg(n)
	}
	return n.String()
}

func abiWords(data string) [][]byte {
	data = normalizeHex(data)
	body := strings.TrimPrefix(data, "0x")
	if len(body)%64 != 0 {
		return nil
	}
	out := make([][]byte, 0, len(body)/64)
	for i := 0; i < len(body); i += 64 {
		word, err := hexToBytes(body[i : i+64])
		if err != nil {
			return nil
		}
		out = append(out, word)
	}
	return out
}

func decodeABIString(input string) string {
	input = normalizeHex(input)
	body := strings.TrimPrefix(input, "0x")
	if len(body) == 64 {
		word, err := hexToBytes(body)
		if err != nil {
			return ""
		}
		return strings.TrimRight(string(word), "\x00")
	}
	words := abiWords(input)
	if len(words) < 2 {
		return ""
	}
	offset := int(new(big.Int).SetBytes(words[0]).Int64() / 32)
	if offset < 0 || offset >= len(words) {
		return ""
	}
	length := int(new(big.Int).SetBytes(words[offset]).Int64())
	if length < 0 {
		return ""
	}
	rawStart := (offset + 1) * 64
	rawEnd := rawStart + length*2
	if rawEnd > len(body) {
		return ""
	}
	value, err := hexToBytes(body[rawStart:rawEnd])
	if err != nil {
		return ""
	}
	return string(value)
}

func hexToBytes(s string) ([]byte, error) {
	out := make([]byte, len(s)/2)
	for i := 0; i < len(out); i++ {
		a := fromHexChar(s[i*2])
		b := fromHexChar(s[i*2+1])
		if a < 0 || b < 0 {
			return nil, fmt.Errorf("invalid hex")
		}
		out[i] = byte(a<<4 | b)
	}
	return out, nil
}

func fromHexChar(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	default:
		return -1
	}
}

func normalizeRPCCall(call rpcCall) *SimulationCall {
	out := &SimulationCall{
		Status:   call.Error == "",
		Error:    call.Error,
		Type:     call.Type,
		From:     call.From,
		To:       call.To,
		Value:    hexToDecimalString(call.Value),
		Func:     functionSelector(call.Input),
		GasLimit: hexToInt64(call.Gas),
		GasUsed:  hexToInt64(call.GasUsed),
	}
	for _, child := range call.Calls {
		if normalized := normalizeRPCCall(child); normalized != nil {
			out.Calls = append(out.Calls, *normalized)
		}
	}
	return out
}

func functionSelector(input string) string {
	input = normalizeHex(input)
	body := strings.TrimPrefix(input, "0x")
	if len(body) < 8 {
		return "0x"
	}
	return "0x" + body[:8]
}

// blockTag converts the requested block number into the JSON-RPC tag the
// simulator passes to debug_traceCall. Zero is treated as "latest" — the
// canonical case for the build-→-check flow, since verdict freshness is
// supplied by the 5-minute intent TTL rather than by pinning a block.
// CheckRequest.BlockNumber documents this intentional zero default.
func blockTag(blockNumber uint64) string {
	if blockNumber == 0 {
		return "latest"
	}
	return uintToHex(blockNumber)
}

func intToHexString(value string) string {
	n, ok := primitives.ParseInteger(value)
	if !ok {
		return "0x0"
	}
	return "0x" + n.Text(16)
}

func uintToHex(value uint64) string {
	if value == 0 {
		return "0x0"
	}
	return fmt.Sprintf("0x%x", value)
}

func hexToInt64(value string) int64 {
	n, ok := primitives.ParseInteger(value)
	if !ok {
		return 0
	}
	return n.Int64()
}

func hexToDecimalString(value string) string {
	n, ok := primitives.ParseInteger(value)
	if !ok {
		return "0"
	}
	return n.String()
}

func topicAddress(topic string) string {
	topic = normalizeHex(topic)
	body := strings.TrimPrefix(topic, "0x")
	if len(body) < 40 {
		return "0x"
	}
	return "0x" + body[len(body)-40:]
}

type rpcCall struct {
	Type    string    `json:"type"`
	From    string    `json:"from"`
	To      string    `json:"to"`
	Input   string    `json:"input"`
	Gas     string    `json:"gas"`
	GasUsed string    `json:"gasUsed"`
	Value   string    `json:"value"`
	Error   string    `json:"error"`
	Logs    []rpcLog  `json:"logs"`
	Calls   []rpcCall `json:"calls"`
}

type rpcLog struct {
	Address string   `json:"address"`
	Topics  []string `json:"topics"`
	Data    string   `json:"data"`
}

type rpcPrestateDiff struct {
	Pre  map[string]rpcAccountState `json:"pre"`
	Post map[string]rpcAccountState `json:"post"`
}

type rpcAccountState struct {
	Balance string `json:"balance"`
}

type tokenTransfer struct {
	Token   string
	From    string
	To      string
	TokenID string
	Amount  string
	Type    string
}

type tokenMetadata struct {
	Name   string
	Symbol string
}

func collectFundFlowAddresses(call rpcCall, owner string) []string {
	seen := make(map[string]bool)
	var out []string
	addAddr := func(addr string) {
		addr = strings.ToLower(strings.TrimSpace(addr))
		if addr == "" || seen[addr] {
			return
		}
		seen[addr] = true
		out = append(out, addr)
	}

	for _, t := range collectTokenTransfers(call) {
		addAddr(t.From)
		addAddr(t.To)
	}

	var walkETH func(rpcCall)
	walkETH = func(c rpcCall) {
		v := strings.ToLower(strings.TrimSpace(c.Value))
		if v != "" && v != "0x" && v != "0x0" {
			addAddr(c.From)
			addAddr(c.To)
		}
		for _, child := range c.Calls {
			walkETH(child)
		}
	}
	walkETH(call)

	ownerLower := strings.ToLower(strings.TrimSpace(owner))
	var walkApprove func(rpcCall)
	walkApprove = func(c rpcCall) {
		input := strings.ToLower(c.Input)
		if strings.ToLower(strings.TrimSpace(c.From)) == ownerLower &&
			strings.HasPrefix(input, primitives.SelectorApprove) &&
			len(input) >= 74 {
			addAddr("0x" + input[34:74])
		}
		for _, child := range c.Calls {
			walkApprove(child)
		}
	}
	walkApprove(call)

	return out
}
