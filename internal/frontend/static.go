// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package frontend

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed assets
var embeddedAssets embed.FS

func EmbeddedFS() fs.FS {
	assets, err := fs.Sub(embeddedAssets, "assets")
	if err != nil {
		return emptyFS{}
	}
	return assets
}

func RegisterRoutes(r *gin.Engine, assets fs.FS) {
	r.NoRoute(func(c *gin.Context) {
		Serve(c, assets)
	})
}

func Serve(c *gin.Context, assets fs.FS) {
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	requestPath := cleanRequestPath(c.Request.URL.Path)
	if shouldReserveForBackend(requestPath) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	if requestPath == "" {
		serveFile(c, assets, "index.html")
		return
	}

	if fileExists(assets, requestPath) {
		serveFile(c, assets, requestPath)
		return
	}

	if strings.HasPrefix(requestPath, "assets/") {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	serveFile(c, assets, "index.html")
}

func cleanRequestPath(raw string) string {
	clean := path.Clean("/" + raw)
	if clean == "/" {
		return ""
	}
	return strings.TrimPrefix(clean, "/")
}

func shouldReserveForBackend(requestPath string) bool {
	switch {
	case requestPath == "api" || strings.HasPrefix(requestPath, "api/"):
		return true
	case strings.HasPrefix(requestPath, "auth/"):
		return true
	case requestPath == "ws" || strings.HasPrefix(requestPath, "ws/"):
		return true
	case requestPath == "health":
		return true
	case requestPath == "setup" || strings.HasPrefix(requestPath, "setup/"):
		return true
	default:
		return false
	}
}

func fileExists(assets fs.FS, name string) bool {
	info, err := fs.Stat(assets, name)
	return err == nil && !info.IsDir()
}

func serveFile(c *gin.Context, assets fs.FS, name string) {
	if !fileExists(assets, name) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	applyCacheHeaders(c, name)
	http.ServeFileFS(c.Writer, c.Request, assets, name)
}

func applyCacheHeaders(c *gin.Context, name string) {
	if strings.HasPrefix(name, "assets/") {
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
		return
	}
	c.Header("Cache-Control", "no-cache")
}

type emptyFS struct{}

func (emptyFS) Open(string) (fs.File, error) {
	return nil, fs.ErrNotExist
}
