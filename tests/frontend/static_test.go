// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package frontend_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/frontend"
)

func TestServeFrontendAsset(t *testing.T) {
	r := testRouter(testAssets())

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%q", w.Code, http.StatusOK, w.Body.String())
	}
	if w.Body.String() != "console.log('web3 companion');" {
		t.Fatalf("body = %q", w.Body.String())
	}
}

func TestServeFrontendFallbackForClientRoute(t *testing.T) {
	r := testRouter(testAssets())

	for _, path := range []string{"/confirm/challenge_123", "/auth"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body=%q", w.Code, http.StatusOK, w.Body.String())
			}
			if w.Body.String() != "<!doctype html><div id=\"root\"></div>" {
				t.Fatalf("body = %q", w.Body.String())
			}
		})
	}
}

func TestServeFrontendCacheHeaders(t *testing.T) {
	r := testRouter(testAssets())

	tests := []struct {
		name         string
		path         string
		cacheControl string
	}{
		{
			name:         "hashed assets are immutable",
			path:         "/assets/app.js",
			cacheControl: "public, max-age=31536000, immutable",
		},
		{
			name:         "spa shell revalidates",
			path:         "/",
			cacheControl: "no-cache",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body=%q", w.Code, http.StatusOK, w.Body.String())
			}
			if got := w.Header().Get("Cache-Control"); got != tt.cacheControl {
				t.Fatalf("Cache-Control = %q, want %q", got, tt.cacheControl)
			}
		})
	}
}

func TestServeDoesNotFallbackBackendNamespaces(t *testing.T) {
	r := testRouter(testAssets())

	for _, path := range []string{"/api/v1/missing", "/auth/missing", "/ws/chat"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			r.ServeHTTP(w, req)

			if w.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d; body=%q", w.Code, http.StatusNotFound, w.Body.String())
			}
			if w.Body.String() == "<!doctype html><div id=\"root\"></div>" {
				t.Fatalf("backend namespace unexpectedly served SPA fallback")
			}
		})
	}
}

func TestServeMissingAssetReturnsNotFound(t *testing.T) {
	r := testRouter(testAssets())

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%q", w.Code, http.StatusNotFound, w.Body.String())
	}
}

func testRouter(assets fstest.MapFS) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	frontend.RegisterRoutes(r, assets)
	return r
}

func testAssets() fstest.MapFS {
	return fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><div id=\"root\"></div>")},
		"assets/app.js": {Data: []byte("console.log('web3 companion');")},
	}
}
