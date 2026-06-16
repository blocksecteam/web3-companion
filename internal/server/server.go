// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/companion"
	"github.com/blocksecteam/web3-companion/internal/config"
	"github.com/blocksecteam/web3-companion/internal/settings"
	"github.com/blocksecteam/web3-companion/internal/setup"
	"github.com/blocksecteam/web3-companion/internal/storage"
)

var version = "0.1.0"

func SetBuildInfo(v string) {
	if strings.TrimSpace(v) == "" {
		version = "0.1.0"
	} else {
		version = v
	}
}

func Main() {
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	dbCfg := cfg.DB
	dbCfg.DSN = storage.ResolveSQLiteDSN(dbCfg, cfg.Workdir.Path)
	db, err := storage.NewDB(dbCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "database init failed: %v\n", err)
		os.Exit(1)
	}

	setup.MigrateFromFiles(db, cfg.Workdir.Path)

	generatedSecrets, err := setup.EnsureSecrets(db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "secret generation: %v\n", err)
		os.Exit(1)
	}
	cfg.Auth.JWTSecret = generatedSecrets.JWTSecret
	cfg.SSM.RootKey = generatedSecrets.SSMKey

	setupComplete := setup.IsSetupComplete(db)

	{
		settingsService := settings.NewService(db)
		providers, _ := settingsService.ReadProviders(settings.ProvidersSettings{
			PortalsAPIKey:    cfg.Providers.PortalsAPIKey,
			MetaSleuthAPIKey: cfg.Providers.MetaSleuthAPIKey,
		})
		cfg.Providers.PortalsAPIKey = providers.PortalsAPIKey
		cfg.Providers.MetaSleuthAPIKey = providers.MetaSleuthAPIKey
	}

	if setupComplete {
		settingsService := settings.NewService(db)
		rpcSettings, _ := settingsService.ReadRPC()
		settings.ApplyRPCSettings(cfg, rpcSettings)

		if err := cfg.Validate(); err != nil {
			fmt.Fprintf(os.Stderr, "config validation: %v\n", err)
			os.Exit(1)
		}
		if err := VerifyConfiguredRPCs(context.Background(), cfg); err != nil {
			fmt.Fprintf(os.Stderr, "config validation: %v\n", err)
			os.Exit(1)
		}
	} else {
		cfg.SetupMode = true
		if err := cfg.ValidateForSetup(); err != nil {
			fmt.Fprintf(os.Stderr, "config validation: %v\n", err)
			os.Exit(1)
		}
	}

	setupLogging(cfg.Log, cfg.Workdir.Path, os.Stdout)
	auth.SetExternalURL(cfg.Server.ExternalURL)

	slog.Info("starting web3 API server", "port", cfg.Server.Port, "setup_mode", cfg.SetupMode)

	backendRoot, _ := os.Getwd()
	companionManager := companion.NewManager(companion.DefaultConfig(backendRoot, cfg.Workdir.Path, cfg))
	r := setupRouter(cfg, db, companionManager, generatedSecrets)

	srv := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      r,
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeout) * time.Second,
	}

	go func() {
		slog.Info("server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
	}
	companionCtx, companionCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer companionCancel()
	if err := companionManager.Shutdown(companionCtx); err != nil {
		slog.Error("companion shutdown failed", "error", err)
	}
	slog.Info("server exited")
}

func VerifyConfiguredRPCs(ctx context.Context, cfg *config.Config) error {
	if os.Getenv("WEB3_SKIP_RPC_VERIFY") == "1" {
		return nil
	}
	verifyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	verifier := settings.NewHTTPRPCVerifier(&http.Client{Timeout: 5 * time.Second})
	return cfg.VerifyChainRPCs(verifyCtx, verifier.Verify)
}
