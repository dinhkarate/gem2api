package admin

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"strconv"

	"gem2api/internal/config"
	"gem2api/internal/pool"
	"gem2api/internal/storage"

	"github.com/gin-gonic/gin"
)

//go:embed static/*
var staticFiles embed.FS

// Handler holds dependencies for admin API endpoints.
type Handler struct {
	DB      *storage.DB
	Pool    *pool.Pool
	Config  *config.Config
	Session *SessionManager
}

// RegisterRoutes registers all admin routes on the given Gin engine.
func (h *Handler) RegisterRoutes(r *gin.Engine) {
	// Serve static admin panel
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("Failed to load static files: %v", err)
	}
	r.StaticFS("/manage", http.FS(staticFS))

	// Public: login
	r.POST("/api/admin/login", h.Login)

	// Plugin API: cookie update (connection token auth)
	r.POST("/api/cookies/update", ConnectionTokenAuth(h.Config.ConnectionToken), h.PluginUpdateCookies)

	// Admin API (session auth)
	admin := r.Group("/api/admin", AdminAuth(h.Session))
	{
		admin.GET("/cookies", h.ListCookies)
		admin.POST("/cookies", h.AddCookie)
		admin.DELETE("/cookies/:id", h.DeleteCookie)
		admin.POST("/cookies/:id/enable", h.EnableCookie)
		admin.POST("/cookies/:id/disable", h.DisableCookie)
		admin.GET("/stats", h.GetStats)
	}
}

// Login authenticates admin and returns a session token.
func (h *Handler) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if req.Username != h.Config.AdminUsername || req.Password != h.Config.AdminPassword {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	token, err := h.Session.CreateSession()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create session"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token})
}

// PluginUpdateCookies handles cookie updates from Chrome Extension.
func (h *Handler) PluginUpdateCookies(c *gin.Context) {
	var req struct {
		Secure1PSID   string `json:"secure_1psid"`
		Secure1PSIDTS string `json:"secure_1psidts"`
		Nickname      string `json:"nickname,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if req.Secure1PSID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "secure_1psid is required"})
		return
	}

	id, isNew, err := h.DB.UpsertByPSID(req.Secure1PSID, req.Secure1PSIDTS)
	if err != nil {
		log.Printf("Error upserting cookie: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save cookies"})
		return
	}

	action := "updated"
	if isNew {
		action = "created"
	}
	log.Printf("Cookie %s: account ID %d", action, id)
	c.JSON(http.StatusOK, gin.H{"id": id, "action": action})
}

// ListCookies returns all accounts.
func (h *Handler) ListCookies(c *gin.Context) {
	accounts, err := h.DB.ListAccounts()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list accounts"})
		return
	}
	// Mask sensitive data
	type safeAccount struct {
		ID                int    `json:"id"`
		PSIDPrefix        string `json:"psid_prefix"` // Show only first 20 chars
		Nickname          string `json:"nickname"`
		IsActive          bool   `json:"is_active"`
		UseCount          int    `json:"use_count"`
		ErrorCount        int    `json:"error_count"`
		ConsecutiveErrors int    `json:"consecutive_errors"`
		BanReason         string `json:"ban_reason,omitempty"`
		BannedAt          string `json:"banned_at,omitempty"`
		LastUsedAt        string `json:"last_used_at,omitempty"`
		LastError         string `json:"last_error,omitempty"`
		CreatedAt         string `json:"created_at"`
		UpdatedAt         string `json:"updated_at"`
	}
	safe := make([]safeAccount, len(accounts))
	for i, a := range accounts {
		prefix := a.Secure1PSID
		if len(prefix) > 20 {
			prefix = prefix[:20] + "..."
		}
		safe[i] = safeAccount{
			ID: a.ID, PSIDPrefix: prefix, Nickname: a.Nickname,
			IsActive: a.IsActive, UseCount: a.UseCount,
			ErrorCount: a.ErrorCount, ConsecutiveErrors: a.ConsecutiveErrors,
			BanReason: a.BanReason, BannedAt: a.BannedAt,
			LastUsedAt: a.LastUsedAt, LastError: a.LastError,
			CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
		}
	}
	c.JSON(http.StatusOK, safe)
}

// AddCookie adds a new account from the admin panel.
func (h *Handler) AddCookie(c *gin.Context) {
	var req struct {
		Secure1PSID   string `json:"secure_1psid"`
		Secure1PSIDTS string `json:"secure_1psidts"`
		Nickname      string `json:"nickname"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if req.Secure1PSID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "secure_1psid is required"})
		return
	}
	id, err := h.DB.AddAccount(req.Secure1PSID, req.Secure1PSIDTS, req.Nickname)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to add account"})
		return
	}
	log.Printf("Account added: ID %d, nickname %q", id, req.Nickname)
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

// DeleteCookie removes an account.
func (h *Handler) DeleteCookie(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.DB.DeleteAccount(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete account"})
		return
	}
	log.Printf("Account deleted: ID %d", id)
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// EnableCookie enables an account.
func (h *Handler) EnableCookie(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.DB.EnableAccount(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to enable account"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"enabled": true})
}

// DisableCookie disables an account.
func (h *Handler) DisableCookie(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.DB.DisableAccount(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to disable account"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"disabled": true})
}

// GetStats returns pool statistics.
func (h *Handler) GetStats(c *gin.Context) {
	total, active, err := h.Pool.Stats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get stats"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"total_accounts":  total,
		"active_accounts": active,
	})
}
