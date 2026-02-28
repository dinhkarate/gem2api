package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gem2api/internal/config"
	"gem2api/internal/gemini"
	"gem2api/internal/handler"
	"gem2api/internal/middleware"

	"github.com/gin-gonic/gin"
)

func main() {
	cfg := config.Load()

	if cfg.Secure1PSID == "" {
		log.Fatal("SECURE_1PSID is required. Set it to your __Secure-1PSID cookie value from gemini.google.com.")
	}

	// Create Gemini web client
	client, err := gemini.NewClient(cfg.Secure1PSID, cfg.Secure1PSIDTS, cfg.ProxyURL)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Bootstrap session (extract CSRF token, build label, session ID)
	// Non-fatal: if bootstrap fails, requests will trigger re-bootstrap automatically.
	log.Println("Bootstrapping Gemini session...")
	if err := client.Bootstrap(context.Background()); err != nil {
		log.Printf("WARNING: Initial bootstrap failed: %v", err)
		log.Println("Server will start anyway — requests will auto-retry bootstrap.")
	}

	// Start background cookie rotation (~9 min interval)
	client.StartCookieRotation()
	defer client.Close()

	// Setup Gin router
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(middleware.Logger())
	r.Use(middleware.CORS())
	r.Use(middleware.Auth(cfg.APIKey))

	// OpenAI-compatible routes
	chatHandler := &handler.ChatHandler{Client: client}
	r.POST("/v1/chat/completions", chatHandler.Handle)
	r.GET("/v1/models", handler.ModelsHandler)

	// Health check
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// Start server
	addr := ":" + cfg.Port
	srv := &http.Server{Addr: addr, Handler: r}

	go func() {
		log.Printf("gem2api listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Shutdown error: %v", err)
	}
}
