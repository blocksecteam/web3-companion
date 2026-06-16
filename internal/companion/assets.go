// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package companion

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed assets/companion-server.js
var companionAssets embed.FS

const embeddedScriptName = "companion-server.js"

func EmbeddedScriptFS() fs.FS {
	assets, err := fs.Sub(companionAssets, "assets")
	if err != nil {
		return companionAssets
	}
	return assets
}

func EnsureEmbeddedScript(workdir string) (string, error) {
	if workdir == "" {
		workdir = defaultWorkdir()
	}
	target := filepath.Join(absPath(workdir), "companion", "server", embeddedScriptName)
	data, err := fs.ReadFile(EmbeddedScriptFS(), embeddedScriptName)
	if err != nil {
		return "", fmt.Errorf("read embedded companion script: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", fmt.Errorf("mkdir embedded companion script dir: %w", err)
	}
	if existing, err := os.ReadFile(target); err == nil && string(existing) == string(data) {
		return target, nil
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return "", fmt.Errorf("write embedded companion script: %w", err)
	}
	return target, nil
}
