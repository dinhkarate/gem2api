package config

import "os"

// Config holds all application configuration.
type Config struct {
	Port          string // Server port (default: 8080)
	APIKey        string // Optional: protect proxy access with an API key
	Secure1PSID   string // Required: __Secure-1PSID cookie from browser
	Secure1PSIDTS string // Recommended: __Secure-1PSIDTS cookie from browser
	ProxyURL      string // Optional: HTTP/SOCKS5 proxy URL
}

// Load reads configuration from environment variables.
func Load() *Config {
	return &Config{
		Port:          getEnv("PORT", "8080"),
		APIKey:        os.Getenv("API_KEY"),
		Secure1PSID:   os.Getenv("SECURE_1PSID"),
		Secure1PSIDTS: os.Getenv("SECURE_1PSIDTS"),
		ProxyURL:      os.Getenv("PROXY_URL"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
