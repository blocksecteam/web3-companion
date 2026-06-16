// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package setup

import (
	"net"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var setupDone atomic.Bool

func SetComplete() {
	setupDone.Store(true)
}

func IsRuntimeComplete() bool {
	return setupDone.Load()
}

func ResetForTest() {
	setupDone.Store(false)
}

func Guard(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		if setupDone.Load() || IsSetupComplete(db) {
			setupDone.Store(true)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": gin.H{
					"code":    "setup_complete",
					"message": "Setup has already been completed",
				},
			})
			return
		}

		if !isLocalhost(c.Request) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": gin.H{
					"code":    "setup_localhost_only",
					"message": "Setup wizard is only accessible from localhost",
				},
			})
			return
		}

		c.Next()
	}
}

func isLocalhost(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}
