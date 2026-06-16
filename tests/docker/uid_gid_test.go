// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package docker_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComposePassesHostUIDAndGID(t *testing.T) {
	root := repositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "docker", "compose.yml"))
	if err != nil {
		t.Fatalf("read compose.yml: %v", err)
	}
	compose := string(data)
	for _, want := range []string{
		"WEB3_UID: ${WEB3_UID",
		"WEB3_GID: ${WEB3_GID",
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("compose.yml must pass %q to the container", want)
		}
	}
}

func TestStartScriptDefaultsUIDAndGIDFromHostUser(t *testing.T) {
	root := repositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "docker", "start.sh"))
	if err != nil {
		t.Fatalf("read start.sh: %v", err)
	}
	start := string(data)
	for _, want := range []string{
		": \"${WEB3_UID:=$(id -u)}\"",
		": \"${WEB3_GID:=$(id -g)}\"",
		"export WEB3_PORT WEB3_DATA_DIR COMPOSE_PROJECT_NAME WEB3_UID WEB3_GID",
	} {
		if !strings.Contains(start, want) {
			t.Fatalf("start.sh must contain %q", want)
		}
	}
}

func TestEntrypointPreservesHomeWhenDroppingPrivileges(t *testing.T) {
	root := repositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "docker", "entrypoint.sh"))
	if err != nil {
		t.Fatalf("read entrypoint.sh: %v", err)
	}
	entrypoint := string(data)
	want := `exec gosu "$WEB3_UID:$WEB3_GID" env HOME="$HOME" WEB3_WORKDIR="$WEB3_WORKDIR" USER=web3 LOGNAME=web3 "$@"`
	if !strings.Contains(entrypoint, want) {
		t.Fatalf("entrypoint must preserve HOME and WEB3_WORKDIR after gosu")
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatal("could not find repository root")
		}
		dir = next
	}
}
