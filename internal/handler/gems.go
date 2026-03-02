package handler

import (
	"log"
	"net/http"

	"gem2api/internal/gemini"
	"gem2api/internal/pool"

	"github.com/gin-gonic/gin"
)

// GemsHandler handles GET /v1/gems requests.
type GemsHandler struct {
	Client *gemini.Client
	Pool   *pool.Pool
}

// Handle returns available Gemini Gems.
func (h *GemsHandler) Handle(c *gin.Context) {
	ctx := c.Request.Context()

	var gems []gemini.Gem
	var err error

	// Try pool account first
	if h.Pool != nil {
		cookiePair, pickErr := h.Pool.Pick()
		if pickErr == nil && cookiePair != nil {
			gems, err = h.Client.FetchGemsAs(ctx, cookiePair.Secure1PSID, cookiePair.Secure1PSIDTS)
			if err != nil {
				log.Printf("FetchGemsAs failed for pool account %d: %v, trying fallback", cookiePair.AccountID, err)
			}
		}
	}

	// Fallback to env var cookies
	if gems == nil {
		gems, err = h.Client.FetchGems(ctx)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{
				"message": "Failed to fetch gems: " + err.Error(),
				"type":    "server_error",
			}})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   gems,
	})
}
