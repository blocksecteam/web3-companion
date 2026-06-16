// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package setup

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/setup"
	"github.com/blocksecteam/web3-companion/tests/testutil"
	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.ReleaseMode)
}

func newGuardedRouter(t *testing.T) *gin.Engine {
	t.Helper()
	db := testutil.NewTestDB(t)
	r := gin.New()
	r.Use(setup.Guard(db))
	r.GET("/setup/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r
}

func doLocalRequest(r http.Handler, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func doRemoteRequest(r http.Handler, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = "203.0.113.50:54321"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestGuard_AllowsLocalhost(t *testing.T) {
	setup.ResetForTest()
	r := newGuardedRouter(t)
	w := doLocalRequest(r, "GET", "/setup/test")
	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
}

func TestGuard_AllowsIPv6Localhost(t *testing.T) {
	setup.ResetForTest()
	r := newGuardedRouter(t)
	req := httptest.NewRequest("GET", "/setup/test", nil)
	req.RemoteAddr = "[::1]:12345"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
}

func TestGuard_BlocksRemoteIP(t *testing.T) {
	setup.ResetForTest()
	r := newGuardedRouter(t)
	w := doRemoteRequest(r, "GET", "/setup/test")
	if w.Code != http.StatusForbidden {
		t.Errorf("got %d, want 403", w.Code)
	}
}

func TestGuard_BlocksWhenComplete(t *testing.T) {
	setup.ResetForTest()
	setup.SetComplete()
	r := newGuardedRouter(t)
	w := doLocalRequest(r, "GET", "/setup/test")
	if w.Code != http.StatusForbidden {
		t.Errorf("got %d, want 403", w.Code)
	}
}

func TestGuard_BlocksWhenUserExists(t *testing.T) {
	setup.ResetForTest()
	db := testutil.NewTestDB(t)
	testutil.CreateTestUser(t, db)

	r := gin.New()
	r.Use(setup.Guard(db))
	r.GET("/setup/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := doLocalRequest(r, "GET", "/setup/test")
	if w.Code != http.StatusForbidden {
		t.Errorf("got %d, want 403 when user exists in DB", w.Code)
	}
}

func TestGuard_BlocksRemoteEvenWhenNotComplete(t *testing.T) {
	setup.ResetForTest()
	r := newGuardedRouter(t)
	w := doRemoteRequest(r, "GET", "/setup/test")
	if w.Code != http.StatusForbidden {
		t.Errorf("got %d, want 403", w.Code)
	}
}

func TestGuard_AllowsDockerBridgeIP(t *testing.T) {
	setup.ResetForTest()
	r := newGuardedRouter(t)
	req := httptest.NewRequest("GET", "/setup/test", nil)
	req.RemoteAddr = "172.17.0.1:54321"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200 for Docker bridge IP", w.Code)
	}
}

func TestGuard_AllowsPrivateNetwork(t *testing.T) {
	setup.ResetForTest()
	r := newGuardedRouter(t)
	for _, addr := range []string{"10.0.0.1:1234", "192.168.1.100:1234", "172.20.0.5:1234"} {
		req := httptest.NewRequest("GET", "/setup/test", nil)
		req.RemoteAddr = addr
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("addr %s: got %d, want 200", addr, w.Code)
		}
	}
}

func TestGuard_InvalidRemoteAddr(t *testing.T) {
	setup.ResetForTest()
	r := newGuardedRouter(t)
	req := httptest.NewRequest("GET", "/setup/test", nil)
	req.RemoteAddr = "not-an-ip"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("got %d, want 403 for invalid remote addr", w.Code)
	}
}

func TestSetComplete_IsRuntimeComplete(t *testing.T) {
	setup.ResetForTest()
	if setup.IsRuntimeComplete() {
		t.Error("expected false before SetComplete")
	}
	setup.SetComplete()
	if !setup.IsRuntimeComplete() {
		t.Error("expected true after SetComplete")
	}
}
