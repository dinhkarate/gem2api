package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// Auth optionally protects the proxy with an API key.
// If requiredKey is empty, all requests pass through (no auth required).
// If set, requests must include Authorization: Bearer <key> matching requiredKey.
func Auth(requiredKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if requiredKey == "" {
			c.Next()
			return
		}

		auth := c.GetHeader("Authorization")
		token := strings.TrimPrefix(auth, "Bearer ")

		if token == "" || token != requiredKey {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"message": "Invalid API key. Provide via Authorization: Bearer <key> header.",
					"type":    "authentication_error",
				},
			})
			return
		}

		c.Next()
	}
}
