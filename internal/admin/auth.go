package admin

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// SessionManager manages admin login sessions using simple random tokens.
type SessionManager struct {
	sessions map[string]time.Time // token → expiry
	mu       sync.RWMutex
	ttl      time.Duration
}

// NewSessionManager creates a session manager with the given TTL.
func NewSessionManager(ttl time.Duration) *SessionManager {
	sm := &SessionManager{
		sessions: make(map[string]time.Time),
		ttl:      ttl,
	}
	// Background cleanup every 5 minutes
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			sm.cleanup()
		}
	}()
	return sm
}

// CreateSession generates a new session token.
func (sm *SessionManager) CreateSession() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)

	sm.mu.Lock()
	sm.sessions[token] = time.Now().Add(sm.ttl)
	sm.mu.Unlock()
	return token, nil
}

// ValidateSession checks if a session token is valid.
func (sm *SessionManager) ValidateSession(token string) bool {
	sm.mu.RLock()
	expiry, ok := sm.sessions[token]
	sm.mu.RUnlock()
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		sm.mu.Lock()
		delete(sm.sessions, token)
		sm.mu.Unlock()
		return false
	}
	return true
}

func (sm *SessionManager) cleanup() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	now := time.Now()
	for token, expiry := range sm.sessions {
		if now.After(expiry) {
			delete(sm.sessions, token)
		}
	}
}

// AdminAuth returns a Gin middleware that checks for valid admin session token.
func AdminAuth(sm *SessionManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		var token string
		if auth != "" {
			token = strings.TrimPrefix(auth, "Bearer ")
		} else if t := c.Query("token"); t != "" {
			// Fallback: query param for WebSocket connections (can't set headers)
			token = t
		} else {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authorization required"})
			return
		}

		if !sm.ValidateSession(token) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired session"})
			return
		}
		c.Next()
	}
}

// ConnectionTokenAuth returns a Gin middleware that checks for valid connection token (for Chrome Extension).
func ConnectionTokenAuth(connectionToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if connectionToken == "" {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "connection token not configured"})
			return
		}

		auth := c.GetHeader("Authorization")
		token := strings.TrimPrefix(auth, "Bearer ")
		if token != connectionToken {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid connection token"})
			return
		}
		c.Next()
	}
}
