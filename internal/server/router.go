// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package server

import (
	"context"

	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/bridge"
	bridgeadapters "github.com/blocksecteam/web3-companion/internal/bridge/adapters"
	"github.com/blocksecteam/web3-companion/internal/chain"

	"github.com/blocksecteam/web3-companion/internal/companion"
	"github.com/blocksecteam/web3-companion/internal/config"
	"github.com/blocksecteam/web3-companion/internal/frontend"

	"github.com/blocksecteam/web3-companion/internal/passkey"
	"github.com/blocksecteam/web3-companion/internal/portfolio"

	"github.com/blocksecteam/web3-companion/internal/security"
	securitychecks "github.com/blocksecteam/web3-companion/internal/security/checks"
	"github.com/blocksecteam/web3-companion/internal/settings"
	"github.com/blocksecteam/web3-companion/internal/setup"

	ssmhandler "github.com/blocksecteam/web3-companion/internal/ssm/handler"
	"github.com/blocksecteam/web3-companion/internal/ssm/vault"

	"github.com/blocksecteam/web3-companion/internal/swap"
	swapadapters "github.com/blocksecteam/web3-companion/internal/swap/adapters"
	"github.com/blocksecteam/web3-companion/internal/token"
	"github.com/blocksecteam/web3-companion/internal/user"
	"gorm.io/gorm"
	"log/slog"
	"net/http"

	"os"
	"strings"
	"time"
)

func setupRouter(cfg *config.Config, db *gorm.DB, companionManager *companion.Manager, generatedSecrets ...setup.GeneratedSecrets) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(requestLogger())
	r.Use(browserCORS(cfg))
	r.Use(securityHeaders())

	RegisterHealthRoute(r)

	v, err := vault.NewConfigVault(context.Background(), cfg, db)
	if err != nil {
		slog.Error("vault init failed", "error", err)
		os.Exit(1)
	}

	passkeyServer := passkey.NewServer(db, &cfg.WebAuthn)
	tokenService := auth.ConfiguredTokenService(db, cfg.Auth.JWTSecret)
	llmService := settings.NewLLMService(db, v.KMSClient(), settings.LLMConfig{})
	settingsService := settings.NewService(db)
	runtimeRPC, err := settings.NewRuntimeRPC(cfg, settingsService)
	if err != nil {
		slog.Warn("runtime RPC settings load failed", "error", err)
	}

	if cfg.SetupMode {
		var secrets setup.GeneratedSecrets
		if len(generatedSecrets) > 0 {
			secrets = generatedSecrets[0]
		}
		setup.RegisterRoutes(r, cfg, db, secrets, runtimeRPC)
	}

	versionChecker := NewVersionChecker(version, "blocksecteam/web3-companion")
	versionChecker.Start()

	RegisterBrowserRoutes(r, cfg, db, v, passkeyServer, companionManager, tokenService, cfg.Auth.JWTSecret, llmService, versionChecker, runtimeRPC)
	RegisterCLIRoutes(r, cfg, db, v, passkeyServer, tokenService, companionManager, llmService, runtimeRPC)

	frontend.RegisterRoutes(r, frontend.EmbeddedFS())

	return r
}
func RegisterBrowserRoutes(r *gin.Engine, cfg *config.Config, db *gorm.DB, v *vault.Vault, passkeyServer *passkey.Server, companionManager *companion.Manager, tokenService *auth.TokenService, jwtSecret string, llmService *settings.LLMService, vc *VersionChecker, runtimeRPCs ...*settings.RuntimeRPC) {
	auth.RegisterRoutes(r, db, passkeyServer, auth.WithTokenService(tokenService), auth.WithCompanionEnvironment(companionManager), auth.WithVault(v), auth.WithRateLimiter(RateLimiter(10, time.Minute)))
	settingsService := settings.NewService(db)
	runtimeRPC := runtimeRPCFromArgs(cfg, settingsService, runtimeRPCs...)
	companion.RegisterRoutes(r, db, companionManager, companion.WithTokenService(tokenService), companion.WithJWTSecret(jwtSecret), companion.WithRuntimeProviderResolver(func(_ context.Context) (companion.LLMProviderConfig, error) {
		runtime, err := settingsService.ReadLLM(settings.InitialLLMSettings(cfg.LLM))
		if err != nil {
			return companion.LLMProviderConfig{}, err
		}
		return companion.LLMProviderConfig{
			Provider:  runtime.Provider,
			BaseURL:   runtime.BaseURL,
			APIKey:    runtime.APIKey,
			ModelName: runtime.ModelName,
		}, nil
	}))

	api := r.Group("/api/v1")
	api.Use(auth.Middleware(db, auth.WithJWTSecretString(jwtSecret)))
	browserAPI := api.Group("")
	browserAPI.Use(auth.RequireClient(auth.ClientBrowser), auth.RequireScope(auth.ScopeBrowser))
	portalsKeyFunc := func() string { return cfg.Providers.PortalsAPIKey }
	userOptions := []user.HandlerOption{}
	if names := chainNames(cfg); len(names) > 0 {
		userOptions = append(userOptions, user.WithWalletAssetInspector(portfolio.NewAssetInspector(names, portfolio.WithAPIKeyFunc(portalsKeyFunc))))
	}
	user.RegisterRoutesWithOptions(browserAPI, db, v, passkeyServer, companionManager, v.KMSClient(), userOptions...)
	if vc != nil {
		browserAPI.GET("/version", func(c *gin.Context) { c.JSON(200, vc.Info()) })
	}
	routeOptions := []settings.RouteOption{}
	if runtimeRPC != nil {
		routeOptions = append(routeOptions, settings.WithRPCSettingsApplier(runtimeRPC))
	}
	settings.RegisterRoutes(browserAPI, cfg, settingsService, llmService, companionManager, routeOptions...)
}
func RegisterCLIRoutes(r *gin.Engine, cfg *config.Config, db *gorm.DB, v *vault.Vault, passkeyServer *passkey.Server, tokenService *auth.TokenService, companionManager *companion.Manager, llmService *settings.LLMService, runtimeRPCs ...*settings.RuntimeRPC) {
	settingsService := settings.NewService(db)
	runtimeRPC := runtimeRPCFromArgs(cfg, settingsService, runtimeRPCs...)
	rpcByName := func() map[string]string {
		if runtimeRPC != nil {
			return runtimeRPC.ChainRPCMap()
		}
		return BuildChainRPCMap(cfg)
	}
	rpcByChainID := func() map[int]string {
		if runtimeRPC != nil {
			return runtimeRPC.ChainIDRPCMap()
		}
		return BuildChainIDRPCMap(cfg)
	}
	publicAPI := r.Group("/api/v1")
	chain.RegisterPublicRoutes(publicAPI, cfg)
	publicSwapReg := swap.NewRegistry()
	swapadapters.RegisterAllWithRPCProvider(publicSwapReg, nil, rpcByName)
	swap.NewHandler(publicSwapReg).RegisterPublicRoutes(publicAPI)
	publicBridgeReg := bridge.NewRegistry()
	bridgeadapters.RegisterAll(publicBridgeReg, nil) // public surface lists protocols only; no Quote/Build
	bridge.NewHandler(publicBridgeReg, bridge.WithConfig(cfg)).RegisterPublicRoutes(publicAPI)

	api := r.Group("/api/v1")
	api.Use(auth.Middleware(db, auth.WithJWTSecretString(cfg.Auth.JWTSecret)))
	readAPI := api.Group("")
	readAPI.Use(auth.RequireClient(auth.ClientBrowser, auth.ClientContainer), auth.RequireScope(auth.ScopeBrowser, auth.ScopeCompanion))
	companionAPI := api.Group("")
	companionAPI.Use(auth.RequireClient(auth.ClientContainer), auth.RequireScope(auth.ScopeCompanion))

	user.RegisterContextRoutes(readAPI, db, chainNames(cfg))
	ssmhandler.RegisterReadRoutesWithConfig(readAPI, db)
	ssmhandler.RegisterWebAuthnBrowserRoutes(readAPI, passkeyServer)
	ssmhandler.RegisterCompanionRoutesWithConfig(companionAPI, r, v, db, passkeyServer, cfg)

	swapResolver := NewSwapDecimalsResolverWithRPCProvider(cfg, rpcByName)
	swapReg := swap.NewRegistry()
	swapadapters.RegisterAllWithRPCProvider(swapReg, swapResolver, rpcByName)
	swapH := swap.NewHandler(swapReg, swap.WithResolver(swapResolver), swap.WithRPCURLProvider(rpcByName), swap.WithConfig(cfg), swap.WithDB(db))
	swapH.RegisterRoutes(companionAPI)

	bridgeReg := bridge.NewRegistry()
	bridgeadapters.RegisterAll(bridgeReg, swapResolver)
	bridgeH := bridge.NewHandler(bridgeReg, bridge.WithConfig(cfg), bridge.WithResolver(swapResolver), bridge.WithDB(db))
	bridgeH.RegisterRoutes(companionAPI)

	portalsKeyFn := func() string { return cfg.Providers.PortalsAPIKey }
	tokenH := token.NewHandler(token.WithAPIKeyFunc(portalsKeyFn))
	tokenH.RegisterRoutes(readAPI)

	portfolioH := portfolio.NewHandler(portfolio.WithAPIKeyFunc(portalsKeyFn))
	portfolioH.RegisterRoutes(readAPI)

	securityLabelProvider := security.NewDynamicLabelProvider(func() security.AddressLabelProvider {
		providerSettings, _ := settingsService.ReadProviders(settings.ProvidersSettings{})
		switch settings.NormalizeAddressLabelProvider(providerSettings.AddressLabelProvider) {
		case settings.AddressLabelMetaSleuth:
			if providerSettings.MetaSleuthAPIKey != "" {
				return security.NewMetaSleuthLabelProvider("", providerSettings.MetaSleuthAPIKey, nil)
			}
			return nil
		case settings.AddressLabelX402:
			return security.NewX402LabelProviderWithWallet(v)
		default:
			return nil
		}
	})
	securityReg := security.NewCheckRegistry()
	securitychecks.RegisterDefaultChecks(securityReg, securityLabelProvider)
	securityH := security.NewHandler(
		security.WithSimulator(security.NewRPCSimulatorWithProvider(rpcByChainID, nil, token.NewPortalsValuator(token.WithAPIKeyFunc(portalsKeyFn)))),
		security.WithAddressLabelProvider(securityLabelProvider),
		security.WithCheckRegistry(securityReg),
		security.WithSecurityConfig(security.SecurityConfig{
			SecurityAgentReviewMode: cfg.Security.Agent.ReviewMode,
			AllowReviewClear:        cfg.Security.Agent.AllowReviewClear,
			AllowReviewClearSet:     true,
		}),
		security.WithIntentStore(db),
		security.WithPasskeyServer(passkeyServer),
	)
	securityH.RegisterRoutes(companionAPI)

	chain.RegisterBrowserRoutesWithOptions(readAPI, cfg, chain.WithTransactionStore(db))
	chain.RegisterCompanionRoutesWithOptions(companionAPI, cfg, chain.WithTokenResolver(swapResolver), chain.WithTransactionStore(db), chain.WithKMSClient(v.KMSClient()))
}

func runtimeRPCFromArgs(cfg *config.Config, settingsService *settings.Service, runtimeRPCs ...*settings.RuntimeRPC) *settings.RuntimeRPC {
	if len(runtimeRPCs) > 0 && runtimeRPCs[0] != nil {
		return runtimeRPCs[0]
	}
	runtimeRPC, err := settings.NewRuntimeRPC(cfg, settingsService)
	if err != nil {
		slog.Warn("runtime RPC settings load failed", "error", err)
		return nil
	}
	return runtimeRPC
}
func RegisterHealthRoute(r *gin.Engine) {
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "version": version})
	})
}
func chainNames(cfg *config.Config) []string {
	active := cfg.ActiveChains()
	chains := make([]string, 0, len(active))
	for _, ch := range active {
		name := strings.TrimSpace(ch.Name)
		if name != "" {
			chains = append(chains, name)
		}
	}
	return chains
}

func BuildChainRPCMap(cfg *config.Config) map[string]string {
	active := cfg.ActiveChains()
	m := make(map[string]string, len(active))
	for _, c := range active {
		if c.Name != "" && c.RPCURL != "" {
			m[strings.ToLower(c.Name)] = c.RPCURL
		}
	}
	return m
}

func BuildChainIDRPCMap(cfg *config.Config) map[int]string {
	active := cfg.ActiveChains()
	m := make(map[int]string, len(active))
	for _, c := range active {
		if c.ChainID != 0 && c.RPCURL != "" {
			m[c.ChainID] = c.RPCURL
		}
	}
	return m
}

func NewSwapDecimalsResolver(cfg *config.Config) *swap.DecimalsResolver {
	return NewSwapDecimalsResolverWithRPCProvider(cfg, func() map[string]string {
		return BuildChainRPCMap(cfg)
	})
}

func NewSwapDecimalsResolverWithRPCProvider(cfg *config.Config, rpcProvider func() map[string]string) *swap.DecimalsResolver {
	httpClient := &http.Client{Timeout: 10 * time.Second}
	rpc := swap.NewRPCDecimalsSourceWithProvider(rpcProvider, httpClient)
	portalsSource := swap.NewPortalsDecimalsSourceWithKeyFunc("https://api.portals.fi/v2", func() string { return cfg.Providers.PortalsAPIKey }, httpClient)
	return swap.NewDecimalsResolver(rpc, portalsSource)
}
