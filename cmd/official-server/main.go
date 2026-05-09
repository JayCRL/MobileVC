package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"time"

	"mobilevc/internal/logx"
	"mobilevc/internal/official/api"
	"mobilevc/internal/official/auth"
	"mobilevc/internal/official/config"
	"mobilevc/internal/official/db"
	"mobilevc/internal/official/signaling"
)

const appName = "MobileVC Official Server"

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"

	//go:embed web/*
	webAssets embed.FS
)

func main() {
	startedAt := time.Now()
	defer logx.Recover("bootstrap", "official server panic")

	logx.Info("bootstrap", "========================================")
	logx.Info("bootstrap", "%s %s", appName, version)
	logx.Info("bootstrap", "build: commit=%s buildDate=%s", commit, buildDate)
	logx.Info("bootstrap", "========================================")

	// Load config
	logx.Info("bootstrap", "Loading configuration")
	cfg, err := config.Load()
	if err != nil {
		logx.Error("bootstrap", "config error: %v", err)
		os.Exit(1)
	}
	for k, v := range cfg.Summary() {
		logx.Info("config", "  %s: %v", k, v)
	}

	// Open database
	logx.Info("bootstrap", "Opening database: %s", cfg.DBPath)
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		logx.Error("bootstrap", "db open failed: %v", err)
		os.Exit(1)
	}
	defer database.Close()
	logx.Info("bootstrap", "Database ready")

	// JWT service
	jwtSvc := auth.NewJWTService(cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	logx.Info("bootstrap", "JWT service ready (access=%dmin refresh=%ddays)",
		cfg.AccessTokenTTL, cfg.RefreshTokenTTL)

	// Auth handler
	authHandler := &api.AuthHandler{
		DB:      database,
		JWT:     jwtSvc,
		BaseURL: cfg.BaseURL,
	}
	if cfg.GitHubClientID != "" {
		authHandler.GitHub = auth.NewGitHubOAuth(
			cfg.GitHubClientID, cfg.GitHubSecret,
			cfg.BaseURL+"/api/auth/oauth/github/callback",
		)
		logx.Info("bootstrap", "GitHub OAuth configured")
	}
	if cfg.GoogleClientID != "" {
		authHandler.GoogleEnabled = true
		authHandler.GoogleClientID = cfg.GoogleClientID
		authHandler.GoogleSecret = cfg.GoogleSecret
		logx.Info("bootstrap", "Google OAuth configured")
	}

	// Nodes handler
	nodesHandler := &api.NodesHandler{DB: database}

	// Signaling hub
	hub := signaling.NewHub()
	sigHandler := signaling.NewHandler(hub, jwtSvc)
	logx.Info("bootstrap", "Signaling hub ready")

	// Admin handler
	adminHandler := &api.AdminHandler{DB: database, Hub: hub, JWT: jwtSvc, RefreshTokenTTL: cfg.RefreshTokenTTL}
	adminToken := cfg.AdminToken
	if adminToken != "" {
		logx.Info("bootstrap", "Admin dashboard enabled")
	}

	// Auth middleware
	authMw := api.AuthMiddleware(jwtSvc)

	// Routes
	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("GET /healthz", api.HealthHandler())

	// OAuth
	mux.HandleFunc("GET /api/auth/oauth/github", authHandler.GitHubLogin())
	mux.HandleFunc("GET /api/auth/oauth/github/callback", authHandler.GitHubCallback())
	if authHandler.GoogleEnabled {
		mux.HandleFunc("GET /api/auth/oauth/google", authHandler.GoogleLogin())
		mux.HandleFunc("GET /api/auth/oauth/google/callback", authHandler.GoogleCallback())
	}
	mux.HandleFunc("GET /api/auth/mobile-callback", authHandler.MobileCallback())

	// Auth
	mux.HandleFunc("POST /api/auth/refresh", authHandler.RefreshToken())
	mux.Handle("GET /api/auth/me", authMw(http.HandlerFunc(authHandler.Me())))
	mux.HandleFunc("POST /api/auth/register", authHandler.EmailRegister())
	mux.HandleFunc("POST /api/auth/login", authHandler.EmailLogin())

	// Nodes (all require auth)
	mux.Handle("GET /api/nodes", authMw(http.HandlerFunc(nodesHandler.ListNodes())))
	mux.Handle("POST /api/nodes", authMw(http.HandlerFunc(nodesHandler.RegisterNode())))
	mux.Handle("DELETE /api/nodes/{id}", authMw(http.HandlerFunc(nodesHandler.DeregisterNode())))
	mux.Handle("POST /api/nodes/heartbeat", authMw(http.HandlerFunc(nodesHandler.Heartbeat())))

	// Signaling WebSocket
	mux.HandleFunc("GET /ws/signaling", sigHandler.ServeHTTP)

	// Admin API & dashboard
	if adminToken != "" {
		mux.Handle("GET /api/admin/dashboard", adminAuthMw(adminToken, http.HandlerFunc(adminHandler.Dashboard())))
		mux.Handle("POST /api/admin/generate-token", adminAuthMw(adminToken, http.HandlerFunc(adminHandler.GenerateNodeToken())))
	}
	mux.HandleFunc("GET /admin/", serveAdminPage(webAssets))
	mux.HandleFunc("GET /admin", serveAdminPage(webAssets))

	addr := ":" + cfg.Port
	logx.Info("bootstrap", "========================================")
	logx.Info("bootstrap", "Ready: addr=%s startedIn=%v", addr, time.Since(startedAt).Round(time.Millisecond))
	logx.Info("bootstrap", "  Base URL:  %s", cfg.BaseURL)
	if adminToken != "" {
		logx.Info("bootstrap", "  Admin:     %s/admin", cfg.BaseURL)
	}
	logx.Info("bootstrap", "========================================")

	// Background: mark stale nodes offline every 60s
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if n, err := database.MarkStaleNodesOffline(120); err != nil {
				logx.Error("cleanup", "stale node cleanup error: %v", err)
			} else if n > 0 {
				logx.Info("cleanup", "marked %d stale nodes offline", n)
			}
		}
	}()

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	if err := srv.ListenAndServe(); err != nil {
		logx.Error("serve", "server error: %v", err)
		fmt.Fprintf(os.Stderr, "server failed: %v\n", err)
		os.Exit(1)
	}
}

func adminAuthMw(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || authHeader != "Bearer "+token {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func serveAdminPage(assets embed.FS) http.HandlerFunc {
	sub, err := fs.Sub(assets, "web")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin" || r.URL.Path == "/admin/" {
			r.URL.Path = "/admin.html"
		}
		fileServer.ServeHTTP(w, r)
	}
}
