// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	auditservice "github.com/blocksecteam/web3-companion/internal/audit"
	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/config"
	"github.com/blocksecteam/web3-companion/internal/passkey"
	"github.com/blocksecteam/web3-companion/internal/ssm/vault"
	"github.com/blocksecteam/web3-companion/internal/storage"
)

func RegisterReadRoutesWithConfig(r *gin.RouterGroup, db *gorm.DB) {
	registerSSMReadRoutes(r, db)
}

func RegisterBrowserRoutes(r *gin.RouterGroup, db *gorm.DB) {
	RegisterAuditRoutes(r, db)
}

func RegisterCompanionRoutesWithConfig(r *gin.RouterGroup, rawEngine *gin.Engine, v *vault.Vault, db *gorm.DB, pk *passkey.Server, cfg *config.Config) {
	signH := NewSignHandlerWithConfig(v, db, pk, cfg)
	delegationH := NewDelegationHandlerWithConfig(v, db, pk, cfg)
	registerSSMWriteRoutes(r, rawEngine, signH, delegationH, db, pk)
}

func registerSSMWriteRoutes(r *gin.RouterGroup, rawEngine *gin.Engine, signH *SignHandler, delegationH *DelegationHandler, db *gorm.DB, pk *passkey.Server) {

	r.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Next()
	})

	ssm := r.Group("/ssm")
	{

		ssm.POST("/sign", signH.RequestSign)
		ssm.GET("/sign/:challenge_id", signH.PollSign)
		ssm.POST("/delegate", delegationH.Delegate)
		ssm.GET("/delegate/estimate", delegationH.DelegateEstimate)
	}

	RegisterWebAuthnCredentialRoutes(r, pk)
	RegisterWebAuthnConfirmRoutes(rawEngine, pk)
}

func registerSSMReadRoutes(r *gin.RouterGroup, db *gorm.DB) {

	r.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Next()
	})

	RegisterAuditRoutes(r, db)
}
func RegisterAuditRoutes(r *gin.RouterGroup, db *gorm.DB) {
	h := &AuditHandler{db: db}
	r.GET("/ssm/audit", h.QueryAudit)
}

type AuditHandler struct {
	db *gorm.DB
}

func (h *AuditHandler) QueryAudit(c *gin.Context) {
	userID := auth.GetUserID(c.Request.Context())
	base := h.db.Where("user_id = ?", userID)

	if walletID := c.Query("wallet_id"); walletID != "" {
		base = base.Where("wallet_id = ?", walletID)
	}
	if eventType := c.Query("event_type"); eventType != "" {
		base = base.Where("event_type = ?", eventType)
	}
	if severity := c.Query("severity"); severity != "" {
		base = base.Where("severity = ?", severity)
	}
	if from := c.Query("from"); from != "" {
		base = base.Where("created_at >= ?", from)
	}
	if to := c.Query("to"); to != "" {
		base = base.Where("created_at <= ?", to)
	}

	pageSize := 25
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			pageSize = n
		}
	}

	offset := 0
	if v := c.Query("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_offset", "message": "offset must be a non-negative integer"}})
			return
		}
		offset = n
	}

	var total int64
	if err := base.Model(&storage.AuditLog{}).Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "internal_error", "message": err.Error()}})
		return
	}

	var logs []storage.AuditLog
	if err := base.Order("created_at DESC").Limit(pageSize).Offset(offset).Find(&logs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "internal_error", "message": err.Error()}})
		return
	}

	if logs == nil {
		logs = []storage.AuditLog{}
	}

	resp := gin.H{"logs": logs, "total": total, "offset": offset, "page_size": pageSize}

	if offset == 0 {
		type severityRow struct {
			Severity string
			Count    int64
		}
		var rows []severityRow

		cntQ := h.db.WithContext(c.Request.Context()).Model(&storage.AuditLog{}).Where("user_id = ?", userID)
		if walletID := c.Query("wallet_id"); walletID != "" {
			cntQ = cntQ.Where("wallet_id = ?", walletID)
		}
		if eventType := c.Query("event_type"); eventType != "" {
			cntQ = cntQ.Where("event_type = ?", eventType)
		}
		if severity := c.Query("severity"); severity != "" {
			cntQ = cntQ.Where("severity = ?", severity)
		}
		if from := c.Query("from"); from != "" {
			cntQ = cntQ.Where("created_at >= ?", from)
		}
		if to := c.Query("to"); to != "" {
			cntQ = cntQ.Where("created_at <= ?", to)
		}
		if err := cntQ.Select("severity, COUNT(*) as count").Group("severity").Scan(&rows).Error; err != nil {
			slog.Warn("severity counts query failed", "error", err)
		} else {
			severityCounts := map[string]int64{}
			for _, r := range rows {
				severityCounts[r.Severity] = r.Count
			}
			resp["severity_counts"] = severityCounts
		}
	}

	c.JSON(http.StatusOK, resp)
}

func logAudit(c *gin.Context, eventType, severity, walletID, detail string) {
	userID := auth.GetUserID(c.Request.Context())
	reqID := auth.GetRequestID(c.Request.Context())

	db, ok := c.Get("db")
	if !ok {
		slog.Warn("no db in context for audit log")
		return
	}
	if gormDB, ok := db.(*gorm.DB); ok {
		if _, err := auditservice.NewService(gormDB).Record(c.Request.Context(), auditservice.Event{
			EventType: eventType,
			Severity:  severity,
			UserID:    userID,
			WalletID:  walletID,
			RequestID: reqID,
			Data:      gin.H{"detail": detail},
		}); err != nil {
			slog.Warn("audit log write failed", "event_type", eventType, "error", err)
		}
	}
}
