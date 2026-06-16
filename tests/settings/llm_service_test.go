// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package settings_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/settings"
)

func TestNormalizeRuntimeDefaultsToDeepSeek(t *testing.T) {
	runtime := settings.NormalizeRuntime(settings.RuntimeProvider{})

	if runtime.Provider != settings.DefaultProvider {
		t.Fatalf("provider = %q, want %q", runtime.Provider, settings.DefaultProvider)
	}
	if runtime.BaseURL != settings.DefaultBaseURL {
		t.Fatalf("base url = %q, want %q", runtime.BaseURL, settings.DefaultBaseURL)
	}
	if runtime.ModelName != settings.DefaultModel {
		t.Fatalf("model = %q, want %q", runtime.ModelName, settings.DefaultModel)
	}
}

func TestVerifyRuntimeProviderSendsAnthropicHelloRequestToConfiguredBaseURL(t *testing.T) {
	var got struct {
		Path          string
		Authorization string
		Body          map[string]interface{}
	}
	anthropicServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Path = r.URL.Path
		got.Authorization = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&got.Body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":          "msg_test_verify",
			"type":        "message",
			"role":        "assistant",
			"model":       "custom-model",
			"stop_reason": "end_turn",
			"usage": map[string]interface{}{
				"input_tokens":  1,
				"output_tokens": 1,
			},
			"content": []map[string]interface{}{
				{"type": "text", "text": "hi"},
			},
		})
	}))
	defer anthropicServer.Close()

	svc := settings.NewLLMService(nil, nil, settings.LLMConfig{})
	err := svc.VerifyRuntimeProvider(t.Context(), settings.RuntimeProvider{
		Provider:  "custom",
		BaseURL:   anthropicServer.URL + "/",
		APIKey:    "sk-runtime-verify",
		ModelName: "custom-model",
	})
	if err != nil {
		t.Fatalf("VerifyRuntimeProvider: %v", err)
	}

	if got.Path != "/v1/messages" {
		t.Fatalf("request path = %q, want /v1/messages", got.Path)
	}
	if got.Authorization != "Bearer sk-runtime-verify" {
		t.Fatalf("Authorization = %q", got.Authorization)
	}
	if got.Body["model"] != "custom-model" {
		t.Fatalf("model = %#v, want custom-model", got.Body["model"])
	}
	if got.Body["max_tokens"] != float64(10) {
		t.Fatalf("max_tokens = %#v, want 10", got.Body["max_tokens"])
	}
}

func TestVerifyRuntimeProviderRejectsEmptyAPIKey(t *testing.T) {
	svc := settings.NewLLMService(nil, nil, settings.LLMConfig{})

	err := svc.VerifyRuntimeProvider(t.Context(), settings.RuntimeProvider{APIKey: " "})
	if err == nil {
		t.Fatal("VerifyRuntimeProvider accepted empty API key")
	}
}

func TestKeyHint(t *testing.T) {
	if got := settings.KeyHint("sk-1234567890"); got != "sk-1...7890" {
		t.Fatalf("KeyHint = %q", got)
	}
	if got := settings.KeyHint("short"); got != "short" {
		t.Fatalf("short KeyHint = %q", got)
	}
}
