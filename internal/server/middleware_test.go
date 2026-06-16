// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/config"
)

func TestRequestLoggerSkipsHealthEndpoint(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	r := gin.New()
	r.Use(requestLogger())
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/ready", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	if logs.Len() != 0 {
		t.Fatalf("health request wrote log entry: %s", logs.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if !strings.Contains(logs.String(), `"path":"/ready"`) {
		t.Fatalf("non-health request was not logged: %s", logs.String())
	}
}

func TestRequestLoggerRedactsSensitiveQueryValues(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	r := gin.New()
	r.Use(requestLogger())
	r.GET("/confirm", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/confirm?challenge_id=ch_secret&access_token=jwt_secret&api_key=provider_secret&safe=value", nil))

	logText := logs.String()
	for _, secret := range []string{"ch_secret", "jwt_secret", "provider_secret"} {
		if strings.Contains(logText, secret) {
			t.Fatalf("request log leaked sensitive query value %q: %s", secret, logText)
		}
	}
	for _, want := range []string{"challenge_id=%3Credacted%3E", "access_token=%3Credacted%3E", "api_key=%3Credacted%3E", "safe=value"} {
		if !strings.Contains(logText, want) {
			t.Fatalf("request log missing %q: %s", want, logText)
		}
	}
}

func TestSetupLoggingWritesConsoleAndRotatingJSONFile(t *testing.T) {
	workdir := t.TempDir()
	var console bytes.Buffer
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })

	setupLogging(config.LogConfig{Level: "info"}, workdir, &console)
	slog.Info("file and console", "kind", "test")

	consoleLine := strings.TrimSpace(console.String())
	var consoleEntry map[string]interface{}
	if err := json.Unmarshal([]byte(consoleLine), &consoleEntry); err != nil {
		t.Fatalf("console log entry is not JSONL: %v; entry=%q", err, consoleLine)
	}
	if consoleEntry["msg"] != "file and console" || consoleEntry["kind"] != "test" {
		t.Fatalf("unexpected console JSON log entry: %#v", consoleEntry)
	}

	logPath := filepath.Join(workdir, "logs", "web3.log.jsonl")
	file, err := os.Open(logPath)
	if err != nil {
		t.Fatalf("open JSON log file %s: %v", logPath, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		t.Fatalf("JSON log file %s had no entries", logPath)
	}
	var entry map[string]interface{}
	if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
		t.Fatalf("log file entry is not JSON: %v; entry=%q", err, scanner.Text())
	}
	if entry["msg"] != "file and console" || entry["kind"] != "test" {
		t.Fatalf("unexpected JSON log entry: %#v", entry)
	}
}
