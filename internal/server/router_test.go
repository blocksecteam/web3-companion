// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package server

import (
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/companion"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

func TestSetupRouterForcesGinReleaseMode(t *testing.T) {
	previousMode := gin.Mode()
	gin.SetMode(gin.DebugMode)
	t.Cleanup(func() { gin.SetMode(previousMode) })

	cfg := testutil.NewTestConfig()
	cfg.Workdir.Path = t.TempDir()
	cfg.SSM.RootKey = "test-router-ssm-root-key-that-is-long-enough"
	db, err := storage.NewDB(cfg.DB)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	setupRouter(cfg, db, companion.NewManager(companion.Config{HomeRoot: t.TempDir()}))

	if gin.Mode() != gin.ReleaseMode {
		t.Fatalf("gin mode = %s, want %s", gin.Mode(), gin.ReleaseMode)
	}
}
