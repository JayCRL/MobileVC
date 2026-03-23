package main

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"mobilevc/internal/config"
	"mobilevc/internal/logx"
	"mobilevc/internal/store"
	"mobilevc/internal/ws"
)

const (
	appName = "MobileVC"
	version = "dev"
)

func main() {
	startedAt := time.Now()
	defer logx.Recover("bootstrap", "server startup panic")

	logx.Info("bootstrap", "========================================")
	logx.Info("bootstrap", "%s backend %s", appName, version)
	logx.Info("bootstrap", "========================================")
	logx.Info("bootstrap", "Starting %s", appName)

	logx.Info("bootstrap", "Loading configuration")
	cfg, err := config.Load()
	if err != nil {
		logx.Error("bootstrap", "load configuration failed: %v", err)
		panic(err)
	}

	summary := cfg.Summary()
	addr := ":" + cfg.Port

	logx.Info("bootstrap", "Configuration summary: port=%s authToken=%s runtime.defaultCommand=%s runtime.defaultMode=%s runtime.debug=%v workspaceRoot=%s projection.enhanced=%v projection.step=%v projection.diff=%v projection.prompt=%v",
		summary.Port,
		logx.AuthTokenSummary(cfg.AuthToken),
		summary.DefaultCommand,
		summary.DefaultMode,
		summary.Debug,
		fallback(summary.WorkspaceRoot, "."),
		summary.EnhancedProjection,
		summary.EnableStepProjection,
		summary.EnableDiffProjection,
		summary.EnablePromptProjection,
	)

	logx.Info("bootstrap", "Initializing session store")
	sessionStore, err := store.NewFileStore("")
	if err != nil {
		logx.Error("bootstrap", "initialize session store failed: %v", err)
		panic(err)
	}
	logx.Info("bootstrap", "Session store ready: driver=file dir=%s", sessionStore.BaseDir())

	logx.Info("bootstrap", "Preparing websocket handler")
	wsHandler := ws.NewHandler(cfg.AuthToken, sessionStore)
	logx.Info("bootstrap", "WebSocket handler ready")

	logx.Info("bootstrap", "Registering routes")
	mux := http.NewServeMux()
	mux.Handle("/ws", wsHandler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/", http.FileServer(http.Dir("./web")))
	logx.Info("bootstrap", "Registered routes: /ws, /healthz, /")

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	logx.Info("bootstrap", "Starting HTTP server")
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		logx.Error("bootstrap", "HTTP listen failed: %v", err)
		panic(fmt.Errorf("listen tcp %s: %w", addr, err))
	}
	logx.Info("bootstrap", "Ready: addr=%s health=http://localhost%s/healthz ws=ws://localhost%s/ws?token=<redacted> startup=%s",
		addr,
		addr,
		addr,
		time.Since(startedAt).Round(time.Millisecond),
	)

	if err := server.Serve(listener); err != nil {
		if err == http.ErrServerClosed {
			logx.Info("bootstrap", "HTTP server stopped")
			return
		}
		logx.Error("bootstrap", "HTTP server stopped unexpectedly: %v", err)
		panic(fmt.Errorf("serve http: %w", err))
	}
}

func fallback(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}
