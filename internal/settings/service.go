// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package settings

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/blocksecteam/web3-companion/internal/config"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	keyLLM       = "llm"
	keyRPC       = "rpc"
	keyProviders = "providers"
)

const (
	AddressLabelDisabled   = "disabled"
	AddressLabelMetaSleuth = "metasleuth"
	AddressLabelX402       = "x402"
)

type ProvidersSettings struct {
	PortalsAPIKey        string `json:"portals_api_key,omitempty"`
	MetaSleuthAPIKey     string `json:"metasleuth_api_key,omitempty"`
	AddressLabelProvider string `json:"address_label_provider,omitempty"`
}

type ProvidersResponse struct {
	PortalsAPIKeyConfigured    bool   `json:"portals_api_key_configured"`
	PortalsAPIKeyHint          string `json:"portals_api_key_hint,omitempty"`
	MetaSleuthAPIKeyConfigured bool   `json:"metasleuth_api_key_configured"`
	MetaSleuthAPIKeyHint       string `json:"metasleuth_api_key_hint,omitempty"`
	AddressLabelProvider       string `json:"address_label_provider"`
}

func NormalizeAddressLabelProvider(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case AddressLabelMetaSleuth, AddressLabelX402:
		return v
	default:
		return AddressLabelDisabled
	}
}

type Service struct {
	db *gorm.DB
}

type LLMSettings struct {
	Provider   string     `json:"provider"`
	BaseURL    string     `json:"base_url"`
	ModelName  string     `json:"model_name"`
	APIKey     string     `json:"api_key,omitempty"`
	VerifiedAt *time.Time `json:"verified_at,omitempty"`
}

type LLMResponse struct {
	Provider         string     `json:"provider"`
	BaseURL          string     `json:"base_url"`
	ModelName        string     `json:"model_name"`
	APIKeyConfigured bool       `json:"api_key_configured"`
	APIKeyHint       string     `json:"api_key_hint,omitempty"`
	VerifiedAt       *time.Time `json:"verified_at,omitempty"`
}

type RPCSettings struct {
	Chains map[string]RPCChainSettings `json:"chains"`
}

type RPCChainSettings struct {
	URL        string     `json:"url"`
	VerifiedAt *time.Time `json:"verified_at,omitempty"`
}

type RPCChainResponse struct {
	Name         string     `json:"name"`
	ChainID      int        `json:"chain_id"`
	NativeSymbol string     `json:"native_symbol"`
	URL          string     `json:"url,omitempty"`
	RPCURLHint   string     `json:"rpc_url_hint,omitempty"`
	VerifiedAt   *time.Time `json:"verified_at,omitempty"`
}

type RPCResponse struct {
	Chains []RPCChainResponse `json:"chains"`
}

type RuntimeRPC struct {
	mu       sync.RWMutex
	cfg      *config.Config
	settings RPCSettings
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

func NewRuntimeRPC(cfg *config.Config, service *Service) (*RuntimeRPC, error) {
	runtime := &RuntimeRPC{cfg: cfg}
	next := InitialRPCSettings(cfg)
	if service != nil {
		loaded, err := service.ReadRPCWithInitial(next)
		if err != nil {
			return nil, err
		}
		next = loaded
	}
	runtime.Apply(next)
	return runtime, nil
}

func (r *RuntimeRPC) Apply(next RPCSettings) {
	if r == nil {
		return
	}
	next = NormalizeRPC(next)
	r.mu.Lock()
	defer r.mu.Unlock()
	ApplyRPCSettings(r.cfg, next)
	r.settings = next
}

func (r *RuntimeRPC) ChainRPCMap() map[string]string {
	if r == nil {
		return map[string]string{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return chainRPCMapFromSettings(r.settings)
}

func (r *RuntimeRPC) ChainIDRPCMap() map[int]string {
	if r == nil {
		return map[int]string{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[int]string)
	if r.cfg == nil {
		return out
	}
	for _, ch := range r.cfg.ActiveChains() {
		name := strings.ToLower(strings.TrimSpace(ch.Name))
		if name == "" || ch.ChainID == 0 {
			continue
		}
		if rpc := strings.TrimSpace(r.settings.Chains[name].URL); rpc != "" {
			out[ch.ChainID] = rpc
		}
	}
	return out
}

func DefaultLLMSettings() LLMSettings {
	return LLMSettings{
		Provider:  DefaultProvider,
		BaseURL:   DefaultBaseURL,
		ModelName: DefaultModel,
	}
}

func InitialLLMSettings(cfg config.LLMConfig) LLMSettings {
	settings := DefaultLLMSettings()
	if strings.TrimSpace(cfg.Provider) != "" {
		settings.Provider = strings.TrimSpace(cfg.Provider)
	}
	if strings.TrimSpace(cfg.BaseURL) != "" {
		settings.BaseURL = strings.TrimSpace(cfg.BaseURL)
	}
	if strings.TrimSpace(cfg.ModelName) != "" {
		settings.ModelName = strings.TrimSpace(cfg.ModelName)
	}
	if strings.TrimSpace(cfg.APIKey) != "" {
		settings.APIKey = strings.TrimSpace(cfg.APIKey)
	}
	return NormalizeLLM(settings)
}

func NormalizeLLM(settings LLMSettings) LLMSettings {
	settings.Provider = NormalizeProvider(settings.Provider)
	settings.BaseURL = strings.TrimRight(strings.TrimSpace(settings.BaseURL), "/")
	if settings.BaseURL == "" {
		settings.BaseURL = DefaultBaseURL
	}
	settings.ModelName = strings.TrimSpace(settings.ModelName)
	if settings.ModelName == "" {
		settings.ModelName = DefaultModel
	}
	settings.APIKey = strings.TrimSpace(settings.APIKey)
	return settings
}

func (s *Service) ReadLLM(initial LLMSettings) (LLMSettings, error) {
	initial = NormalizeLLM(initial)
	data, err := s.readSetting(keyLLM)
	if err != nil {
		return initial, nil
	}
	var loaded LLMSettings
	if err := json.Unmarshal([]byte(data), &loaded); err != nil {
		return initial, nil
	}
	loaded = NormalizeLLM(loaded)
	if loaded.APIKey == "" {
		loaded.APIKey = initial.APIKey
	}
	return loaded, nil
}

func (s *Service) WriteLLM(settings LLMSettings) error {
	return s.writeSetting(keyLLM, NormalizeLLM(settings))
}

func (s *Service) ReadRPC() (RPCSettings, error) {
	empty := RPCSettings{Chains: map[string]RPCChainSettings{}}
	data, err := s.readSetting(keyRPC)
	if err != nil {
		return empty, nil
	}
	var loaded RPCSettings
	if err := json.Unmarshal([]byte(data), &loaded); err != nil {
		return empty, nil
	}
	if loaded.Chains == nil {
		loaded.Chains = map[string]RPCChainSettings{}
	}
	return NormalizeRPC(loaded), nil
}

func (s *Service) WriteRPC(settings RPCSettings) error {
	return s.writeSetting(keyRPC, NormalizeRPC(settings))
}

func (s *Service) ReadProviders(initial ProvidersSettings) (ProvidersSettings, error) {
	data, err := s.readSetting(keyProviders)
	if err != nil {
		return initial, nil
	}
	var loaded ProvidersSettings
	if err := json.Unmarshal([]byte(data), &loaded); err != nil {
		return initial, nil
	}
	loaded.PortalsAPIKey = stripBearerPrefix(loaded.PortalsAPIKey)
	if loaded.PortalsAPIKey == "" {
		loaded.PortalsAPIKey = initial.PortalsAPIKey
	}
	if loaded.MetaSleuthAPIKey == "" {
		loaded.MetaSleuthAPIKey = initial.MetaSleuthAPIKey
	}
	return loaded, nil
}

func stripBearerPrefix(key string) string {
	key = strings.TrimSpace(key)
	lower := strings.ToLower(key)
	if strings.HasPrefix(lower, "bearer ") {
		return strings.TrimSpace(key[7:])
	}
	return key
}

func (s *Service) WriteProviders(settings ProvidersSettings) error {
	return s.writeSetting(keyProviders, settings)
}

func ProvidersResponseFromSettings(settings ProvidersSettings) ProvidersResponse {
	return ProvidersResponse{
		PortalsAPIKeyConfigured:    strings.TrimSpace(settings.PortalsAPIKey) != "",
		PortalsAPIKeyHint:          KeyHint(settings.PortalsAPIKey),
		MetaSleuthAPIKeyConfigured: strings.TrimSpace(settings.MetaSleuthAPIKey) != "",
		MetaSleuthAPIKeyHint:       KeyHint(settings.MetaSleuthAPIKey),
		AddressLabelProvider:       NormalizeAddressLabelProvider(settings.AddressLabelProvider),
	}
}

func (s *Service) readSetting(key string) (string, error) {
	var row storage.AppSetting
	if err := s.db.Where("key = ?", key).First(&row).Error; err != nil {
		return "", err
	}
	return row.Value, nil
}

func (s *Service) writeSetting(key string, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal setting %s: %w", key, err)
	}
	row := storage.AppSetting{
		Key:       key,
		Value:     string(data),
		UpdatedAt: time.Now().UTC(),
	}
	return s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&row).Error
}

func (s *Service) ReadRPCWithInitial(initial RPCSettings) (RPCSettings, error) {
	initial = NormalizeRPC(initial)
	loaded, err := s.ReadRPC()
	if err != nil {
		return RPCSettings{}, err
	}
	for name, chain := range initial.Chains {
		if _, ok := loaded.Chains[name]; !ok {
			loaded.Chains[name] = chain
		}
	}
	return NormalizeRPC(loaded), nil
}

func LLMResponseFromSettings(settings LLMSettings) LLMResponse {
	settings = NormalizeLLM(settings)
	return LLMResponse{
		Provider:         settings.Provider,
		BaseURL:          settings.BaseURL,
		ModelName:        settings.ModelName,
		APIKeyConfigured: strings.TrimSpace(settings.APIKey) != "",
		APIKeyHint:       KeyHint(settings.APIKey),
		VerifiedAt:       settings.VerifiedAt,
	}
}

func NormalizeRPC(settings RPCSettings) RPCSettings {
	normalized := RPCSettings{Chains: map[string]RPCChainSettings{}}
	for name, chain := range settings.Chains {
		key := strings.ToLower(strings.TrimSpace(name))
		url := strings.TrimSpace(chain.URL)
		if key == "" || url == "" {
			continue
		}
		normalized.Chains[key] = RPCChainSettings{URL: url, VerifiedAt: chain.VerifiedAt}
	}
	return normalized
}

func InitialRPCSettings(cfg *config.Config) RPCSettings {
	settings := RPCSettings{Chains: map[string]RPCChainSettings{}}
	if cfg == nil {
		return settings
	}
	for _, ch := range cfg.Chains {
		name := strings.ToLower(strings.TrimSpace(ch.Name))
		url := strings.TrimSpace(ch.RPCURL)
		if name == "" || url == "" {
			continue
		}
		settings.Chains[name] = RPCChainSettings{URL: url}
	}
	return settings
}

func RPCResponseFromSettings(cfg *config.Config, settings RPCSettings) RPCResponse {
	settings = NormalizeRPC(settings)
	if cfg == nil {
		return RPCResponse{}
	}
	chains := make([]RPCChainResponse, 0, len(cfg.Chains))
	for _, ch := range cfg.Chains {
		name := strings.ToLower(strings.TrimSpace(ch.Name))
		if name == "" {
			continue
		}
		rpc := settings.Chains[name]
		chains = append(chains, RPCChainResponse{
			Name:         name,
			ChainID:      ch.ChainID,
			NativeSymbol: ch.NativeSymbol,
			URL:          rpc.URL,
			RPCURLHint:   urlHint(rpc.URL),
			VerifiedAt:   rpc.VerifiedAt,
		})
	}
	return RPCResponse{Chains: chains}
}

func ApplyRPCSettings(cfg *config.Config, settings RPCSettings) {
	if cfg == nil {
		return
	}
	settings = NormalizeRPC(settings)
	for i := range cfg.Chains {
		name := strings.ToLower(strings.TrimSpace(cfg.Chains[i].Name))
		if rpc, ok := settings.Chains[name]; ok && strings.TrimSpace(rpc.URL) != "" {
			cfg.Chains[i].RPCURL = strings.TrimSpace(rpc.URL)
		}
	}
}

func chainRPCMapFromSettings(settings RPCSettings) map[string]string {
	settings = NormalizeRPC(settings)
	out := make(map[string]string, len(settings.Chains))
	for name, chain := range settings.Chains {
		if rpc := strings.TrimSpace(chain.URL); rpc != "" {
			out[name] = rpc
		}
	}
	return out
}

func urlHint(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if len(raw) <= 16 {
		return raw
	}
	return raw[:8] + "..." + raw[len(raw)-4:]
}
