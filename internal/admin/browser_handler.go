package admin

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"

	"gem2api/internal/browser"
	"gem2api/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// BrowserHandler handles admin API routes for browser profile management.
type BrowserHandler struct {
	Manager *browser.BrowserManager
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for admin panel
	},
}

// RegisterBrowserRoutes registers browser management routes on the admin group.
func (bh *BrowserHandler) RegisterBrowserRoutes(adminGroup *gin.RouterGroup) {
	bg := adminGroup.Group("/browser")
	{
		bg.GET("/profiles", bh.ListProfiles)
		bg.POST("/profiles", bh.CreateProfile)
		bg.DELETE("/profiles/:id", bh.DeleteProfile)
		bg.POST("/profiles/:id/login", bh.StartLogin)
		bg.POST("/profiles/:id/finish", bh.FinishLogin)
		bg.POST("/profiles/:id/cancel", bh.CancelLogin)
		bg.POST("/profiles/:id/refresh", bh.ManualRefresh)
		bg.GET("/profiles/:id/view", bh.ViewScreencast)
	}
}

// ListProfiles returns all browser profiles.
func (bh *BrowserHandler) ListProfiles(c *gin.Context) {
	profiles, err := bh.Manager.DB().ListBrowserProfiles()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if profiles == nil {
		profiles = make([]*storage.BrowserProfile, 0)
	}
	c.JSON(200, gin.H{"profiles": profiles})
}

// CreateProfile creates a new browser profile.
func (bh *BrowserHandler) CreateProfile(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "name is required"})
		return
	}

	// Create profile dir
	dir := bh.Manager.ProfileDirFor(req.Name)
	profile, err := bh.Manager.DB().CreateBrowserProfile(req.Name, dir)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(201, gin.H{"profile": profile})
}

// DeleteProfile deletes a browser profile and its data directory.
func (bh *BrowserHandler) DeleteProfile(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid profile ID"})
		return
	}

	// Cancel any active session first
	bh.Manager.CancelLogin(id)

	// Get profile to find directory
	profile, err := bh.Manager.DB().GetBrowserProfile(id)
	if err != nil {
		c.JSON(404, gin.H{"error": "profile not found"})
		return
	}

	// Delete from DB
	if err := bh.Manager.DB().DeleteBrowserProfile(id); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// Clean up profile directory (best effort)
	if profile.ProfileDir != "" {
		if err := os.RemoveAll(profile.ProfileDir); err != nil {
			log.Printf("Warning: failed to remove profile dir %s: %v", profile.ProfileDir, err)
		}
	}

	c.JSON(200, gin.H{"message": "profile deleted"})
}

// StartLogin begins an interactive login session for a browser profile.
func (bh *BrowserHandler) StartLogin(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid profile ID"})
		return
	}

	if err := bh.Manager.StartLogin(id); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"message": "login session started",
		"view":    "/api/admin/browser/profiles/" + strconv.Itoa(id) + "/view",
	})
}

// FinishLogin extracts cookies from an active login session.
func (bh *BrowserHandler) FinishLogin(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid profile ID"})
		return
	}

	cookies, err := bh.Manager.FinishLogin(id)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"message":    "cookies extracted successfully",
		"has_psid":   cookies.Secure1PSID != "",
		"has_psidts": cookies.Secure1PSIDTS != "",
	})
}

// CancelLogin cancels an active login session.
func (bh *BrowserHandler) CancelLogin(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid profile ID"})
		return
	}

	bh.Manager.CancelLogin(id)
	c.JSON(200, gin.H{"message": "login cancelled"})
}

// ManualRefresh triggers a manual cookie refresh for a profile.
func (bh *BrowserHandler) ManualRefresh(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid profile ID"})
		return
	}

	profile, err := bh.Manager.DB().GetBrowserProfile(id)
	if err != nil {
		c.JSON(404, gin.H{"error": "profile not found"})
		return
	}

	if profile.AccountID == nil {
		c.JSON(400, gin.H{"error": "profile not linked to an account — complete login first"})
		return
	}

	cookies, err := bh.Manager.RefreshCookies(profile)
	if err != nil {
		bh.Manager.DB().UpdateBrowserProfileError(id, err.Error())
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// Update account cookies
	if err := bh.Manager.DB().UpdateAccountCookies(*profile.AccountID, cookies.Secure1PSID, cookies.Secure1PSIDTS); err != nil {
		c.JSON(500, gin.H{"error": "failed to update account: " + err.Error()})
		return
	}

	bh.Manager.DB().UpdateBrowserProfileRefresh(id)
	c.JSON(200, gin.H{"message": "cookies refreshed successfully"})
}

// ViewScreencast upgrades to WebSocket and streams the browser screencast.
func (bh *BrowserHandler) ViewScreencast(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid profile ID"})
		return
	}

	session := bh.Manager.GetSession(id)
	if session == nil {
		c.JSON(404, gin.H{"error": "no active login session"})
		return
	}

	// Upgrade to WebSocket
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	// Read input events from client in background
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var ev browser.InputEvent
			if err := json.Unmarshal(msg, &ev); err != nil {
				log.Printf("WebSocket: invalid input event: %v", err)
				continue
			}
			// Non-blocking send to session
			select {
			case session.InputCh <- ev:
			default:
			}
		}
	}()

	// Stream frames to client
	for frame := range session.FrameCh {
		msg, _ := json.Marshal(map[string]interface{}{
			"type": "frame",
			"data": frame.Data,
			"metadata": map[string]interface{}{
				"offsetTop":       frame.Metadata.OffsetTop,
				"pageScaleFactor": frame.Metadata.PageScaleFactor,
				"deviceWidth":     frame.Metadata.DeviceWidth,
				"deviceHeight":    frame.Metadata.DeviceHeight,
				"scrollOffsetX":   frame.Metadata.ScrollOffsetX,
				"scrollOffsetY":   frame.Metadata.ScrollOffsetY,
			},
		})
		if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
}
