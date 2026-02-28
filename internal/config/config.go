package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all application configuration.
type Config struct {
	Port          string // Server port (default: 8080)
	APIKey        string // Optional: protect proxy access with an API key
	Secure1PSID   string // Fallback: __Secure-1PSID cookie from env
	Secure1PSIDTS string // Fallback: __Secure-1PSIDTS cookie from env
	ProxyURL      string // Optional: HTTP/SOCKS5 proxy URL

	// Database
	DBPath string // SQLite database path (default: data/gem2api.db)

	// Admin panel
	AdminUsername   string        // Admin login username (default: admin)
	AdminPassword   string        // Admin login password (default: admin)
	ConnectionToken string        // Token for Chrome Extension / plugin API
	SessionTTL      time.Duration // Admin session TTL (default: 24h)

	// Pool settings
	ErrorThreshold int           // Consecutive errors before auto-ban (default: 3)
	AutoUnbanAfter time.Duration // Auto-unban duration (default: 1h)
}

// Load reads configuration from environment variables.
func Load() *Config {
	return &Config{
		Port:          getEnv("PORT", "8080"),
		APIKey:        os.Getenv("API_KEY"),
		Secure1PSID:   os.Getenv("SECURE_1PSID"),
		Secure1PSIDTS: os.Getenv("SECURE_1PSIDTS"),
		ProxyURL:      os.Getenv("PROXY_URL"),

		DBPath: getEnv("DB_PATH", "data/gem2api.db"),

		AdminUsername:   getEnv("ADMIN_USERNAME", "admin"),
		AdminPassword:   getEnv("ADMIN_PASSWORD", "admin"),
		ConnectionToken: os.Getenv("CONNECTION_TOKEN"),
		SessionTTL:      parseDuration(os.Getenv("SESSION_TTL"), 24*time.Hour),

		ErrorThreshold: parseInt(os.Getenv("ERROR_THRESHOLD"), 3),
		AutoUnbanAfter: parseDuration(os.Getenv("AUTO_UNBAN_AFTER"), 1*time.Hour),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseInt(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return v
}

func parseDuration(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}
