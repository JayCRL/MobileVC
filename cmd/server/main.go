package main

import (
	"embed"
	"fmt"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"mobilevc/internal/config"
	"mobilevc/internal/logx"
	"mobilevc/internal/push"
	"mobilevc/internal/store"
	"mobilevc/internal/tts"
	"mobilevc/internal/ws"
)

const (
	appName = "MobileVC"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"

	//go:embed web/*
	webAssets embed.FS
)

func main() {
	startedAt := time.Now()
	defer logx.Recover("bootstrap", "server startup panic")

	logx.Info("bootstrap", "========================================")
	logx.Info("bootstrap", "%s backend %s", appName, version)
	logx.Info("bootstrap", "build metadata: commit=%s buildDate=%s", commit, buildDate)
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

	logx.Info("bootstrap", "Configuration summary: port=%s authToken=%s runtime.defaultCommand=%s runtime.defaultMode=%s runtime.debug=%v workspaceRoot=%s projection.enhanced=%v projection.step=%v projection.diff=%v projection.prompt=%v tts.enabled=%v tts.provider=%s tts.url=%s tts.timeout=%ds tts.maxTextLength=%d tts.format=%s",
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
		summary.TTSEnabled,
		summary.TTSProvider,
		fallback(summary.TTSPythonServiceURL, "-"),
		summary.TTSRequestTimeout,
		summary.TTSMaxTextLength,
		summary.TTSDefaultFormat,
	)

	logx.Info("bootstrap", "Initializing session store")
	sessionStore, err := store.NewFileStore("")
	if err != nil {
		logx.Error("bootstrap", "initialize session store failed: %v", err)
		panic(err)
	}
	logx.Info("bootstrap", "Session store ready: driver=file dir=%s", sessionStore.BaseDir())

	logx.Info("bootstrap", "Initializing push service")
	var pushService push.Service
	apnsAuthKeyPath := os.Getenv("APNS_AUTH_KEY_PATH")
	apnsKeyID := os.Getenv("APNS_KEY_ID")
	apnsTeamID := os.Getenv("APNS_TEAM_ID")
	apnsTopic := os.Getenv("APNS_TOPIC")
	apnsProduction := os.Getenv("APNS_PRODUCTION") == "true"

	if apnsAuthKeyPath != "" && apnsKeyID != "" && apnsTeamID != "" && apnsTopic != "" {
		apnsService, err := push.NewAPNsService(push.APNsConfig{
			AuthKeyPath: apnsAuthKeyPath,
			KeyID:       apnsKeyID,
			TeamID:      apnsTeamID,
			Topic:       apnsTopic,
			Production:  apnsProduction,
		})
		if err != nil {
			logx.Warn("bootstrap", "initialize APNs service failed: %v", err)
			pushService = &push.NoopService{}
		} else {
			pushService = apnsService
			logx.Info("bootstrap", "APNs service ready: topic=%s production=%v", apnsTopic, apnsProduction)
		}
	} else {
		logx.Info("bootstrap", "APNs not configured, push notifications disabled")
		pushService = &push.NoopService{}
	}

	logx.Info("bootstrap", "Preparing websocket handler")
	wsHandler := ws.NewHandler(cfg.AuthToken, sessionStore)
	wsHandler.PushService = pushService
	logx.Info("bootstrap", "WebSocket handler ready")

	var ttsHandler *tts.HTTPHandler
	if cfg.TTS.Enabled {
		logx.Info("bootstrap", "Preparing TTS handler")
		provider := tts.NewChatTTSHTTPProvider(cfg.TTS.PythonServiceURL, time.Duration(cfg.TTS.RequestTimeoutSeconds)*time.Second)
		service := tts.NewService(provider, cfg.TTS.MaxTextLength, cfg.TTS.DefaultFormat)
		ttsHandler = tts.NewHTTPHandler(cfg.AuthToken, true, cfg.TTS.Provider, service)
		logx.Info("bootstrap", "TTS handler ready: provider=%s url=%s", cfg.TTS.Provider, cfg.TTS.PythonServiceURL)
	} else {
		logx.Info("bootstrap", "TTS handler disabled")
	}

	staticFS, err := fs.Sub(webAssets, "web")
	if err != nil {
		logx.Error("bootstrap", "prepare embedded web assets failed: %v", err)
		panic(err)
	}

	logx.Info("bootstrap", "Registering routes")
	mux := http.NewServeMux()
	mux.Handle("/ws", wsHandler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, "{\"version\":%q,\"commit\":%q,\"buildDate\":%q}", version, commit, buildDate)
	})
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != cfg.AuthToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		target := filepath.Clean(r.URL.Query().Get("path"))
		if target == "" || target == "." {
			http.Error(w, "path is required", http.StatusBadRequest)
			return
		}
		absPath, err := filepath.Abs(target)
		if err != nil {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		info, err := os.Stat(absPath)
		if err != nil {
			http.Error(w, "file not found", http.StatusNotFound)
			return
		}
		if info.IsDir() {
			http.Error(w, "path is a directory", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(absPath)))
		if contentType := mime.TypeByExtension(filepath.Ext(absPath)); contentType != "" {
			w.Header().Set("Content-Type", contentType)
		} else {
			w.Header().Set("Content-Type", "application/octet-stream")
		}
		http.ServeFile(w, r, absPath)
	})
	if ttsHandler != nil {
		mux.HandleFunc("/api/tts/synthesize", ttsHandler.HandleSynthesize)
		mux.HandleFunc("/api/tts/healthz", ttsHandler.HandleHealthz)
		logx.Info("bootstrap", "Registered routes: /ws, /healthz, /version, /download, /api/tts/synthesize, /api/tts/healthz, /")
	} else {
		logx.Info("bootstrap", "Registered routes: /ws, /healthz, /version, /download, /")
	}
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

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
	logx.Info("bootstrap", "Ready: addr=%s health=http://localhost%s/healthz version=http://localhost%s/version ws=ws://localhost%s/ws?token=<redacted> startup=%s",
		addr,
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
