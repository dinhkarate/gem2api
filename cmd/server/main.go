package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gem2api/internal/admin"
	"gem2api/internal/config"
	"gem2api/internal/gemini"
	"gem2api/internal/handler"
	"gem2api/internal/middleware"
	"gem2api/internal/pool"
	"gem2api/internal/storage"

	"github.com/gin-gonic/gin"
)

func main() {
	cfg := config.Load()

	// Create Gemini web client (shared HTTP transport)
	client, err := gemini.NewClient(cfg.Secure1PSID, cfg.Secure1PSIDTS, cfg.ProxyURL)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Initialize database
	db, err := storage.NewDB(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Initialize cookie pool
	cookiePool := pool.NewPool(db, cfg.ErrorThreshold, cfg.AutoUnbanAfter)
	cookiePool.StartAutoUnban()
	defer cookiePool.Stop()

	// Bootstrap session (try pool first, then env vars)
	bootstrapDone := false
	if cookiePool.HasAccounts() {
		total, active, _ := cookiePool.Stats()
		log.Printf("Cookie pool: %d total, %d active accounts", total, active)
		bootstrapDone = true // pool accounts will auto-bootstrap on first use
	}

	if cfg.Secure1PSID != "" {
		log.Println("Bootstrapping Gemini session with env var cookies...")
		if err := client.Bootstrap(context.Background()); err != nil {
			log.Printf("WARNING: Initial bootstrap failed: %v", err)
			log.Println("Server will start anyway — requests will auto-retry bootstrap.")
		} else {
			bootstrapDone = true
		}
		client.StartCookieRotation()
		defer client.Close()
	}

	if !bootstrapDone && cfg.Secure1PSID == "" {
		log.Println("No cookies configured. Add accounts via /manage admin panel or set SECURE_1PSID env var.")
	}

	// Setup Gin router
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(middleware.Logger())
	r.Use(middleware.CORS())

	// Admin panel + API (no proxy auth required)
	sessionMgr := admin.NewSessionManager(cfg.SessionTTL)
	adminHandler := &admin.Handler{
		DB:      db,
		Pool:    cookiePool,
		Config:  cfg,
		Session: sessionMgr,
	}
	adminHandler.RegisterRoutes(r)

	// OpenAI-compatible routes (with optional API key auth)
	api := r.Group("/")
	api.Use(middleware.Auth(cfg.APIKey))

	chatHandler := &handler.ChatHandler{
		Client: client,
		Pool:   cookiePool,
	}
	api.POST("/v1/chat/completions", chatHandler.Handle)
	api.GET("/v1/models", handler.ModelsHandler)

	// Health check
	r.GET("/health", func(c *gin.Context) {
		total, active, _ := cookiePool.Stats()
		c.JSON(200, gin.H{
			"status":       "ok",
			"pool_total":   total,
			"pool_active":  active,
			"env_fallback": cfg.Secure1PSID != "",
		})
	})

	// Start server
	addr := ":" + cfg.Port
	srv := &http.Server{Addr: addr, Handler: r}

	go func() {
		log.Printf("gem2api listening on %s", addr)
		log.Printf("  Admin panel: http://localhost:%s/manage/", cfg.Port)
		log.Printf("  API: http://localhost:%s/v1/chat/completions", cfg.Port)
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
