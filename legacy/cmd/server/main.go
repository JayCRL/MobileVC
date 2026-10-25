package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mobilevc/internal/config"
	"mobilevc/internal/data"
	"mobilevc/internal/gateway"
	"mobilevc/internal/logx"
	"mobilevc/internal/push"
	"mobilevc/internal/relay"
	"mobilevc/internal/relay/e2ee"
	"mobilevc/internal/relayclient"
	"mobilevc/internal/tts"
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
	flags, err := parseServerFlags(os.Args[1:])
	if err != nil {
		panic(err)
	}
	if flags.showHelp {
		return
	}
	cfg, sessionStore := bootstrap(flags.overrides)
	summary := cfg.Summary()
	addr := cfg.ListenAddress()

	logConfigSummary(summary, cfg.AuthToken)
	pushService := initPushService()
	trustStore, err := relayDeviceTrustStore(cfg)
	if err != nil {
		logx.Error("bootstrap", "initialize relay device trust failed: %v", err)
		panic(err)
	}
	nodeIdentityStore, err := relayNodeIdentityStore(cfg)
	if err != nil {
		logx.Error("bootstrap", "initialize relay node identity failed: %v", err)
		panic(err)
	}
	wsHandler := initWebSocketHandler(cfg, sessionStore, pushService, trustStore, nodeIdentityStore)
	if err := startRelayClient(cfg, wsHandler, trustStore, nodeIdentityStore); err != nil {
		logx.Error("relay", "start relay client failed: %v", err)
		panic(err)
	}
	ttsHandler := initTTSHandler(cfg)
	mux := buildMux(cfg, wsHandler, ttsHandler)
	startHTTPServer(addr, summary, mux, startedAt)
}

func bootstrap(overrides config.Overrides) (config.Config, *data.FileStore) {
	logx.Info("bootstrap", "========================================")
	logx.Info("bootstrap", "%s backend %s", appName, version)
	logx.Info("bootstrap", "build metadata: commit=%s buildDate=%s", commit, buildDate)
	logx.Info("bootstrap", "========================================")
	logx.Info("bootstrap", "Starting %s", appName)

	logx.Info("bootstrap", "Loading configuration")
	cfg, err := config.LoadWithOverrides(overrides)
	if err != nil {
		logx.Error("bootstrap", "load configuration failed: %v", err)
		panic(err)
	}

	logx.Info("bootstrap", "Initializing session store")
	sessionStore, err := data.NewFileStore("")
	if err != nil {
		logx.Error("bootstrap", "initialize session store failed: %v", err)
		panic(err)
	}
	logx.Info("bootstrap", "Session store ready: driver=file dir=%s", sessionStore.BaseDir())
	return cfg, sessionStore
}

func logConfigSummary(summary config.Summary, authToken string) {
	logx.Info("bootstrap", "Configuration summary: port=%s listen=%s exposureMode=%s authToken=%s relayMode=%v relayURL=%s runtime.defaultCommand=%s runtime.defaultMode=%s runtime.debug=%v workspaceRoot=%s projection.enhanced=%v projection.step=%v projection.diff=%v projection.prompt=%v tts.enabled=%v tts.provider=%s tts.url=%s tts.timeout=%ds tts.maxTextLength=%d tts.format=%s",
		summary.Port,
		summary.ListenAddress,
		summary.ExposureMode,
		logx.AuthTokenSummary(authToken),
		summary.RelayMode,
		fallback(summary.RelayURL, "-"),
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
}

func initPushService() push.Service {
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
	return pushService
}

func initWebSocketHandler(cfg config.Config, sessionStore *data.FileStore, pushService push.Service, trustStore *e2ee.DeviceTrustStore, nodeIdentityStore *e2ee.NodeIdentityStore) *gateway.Handler {
	logx.Info("bootstrap", "Preparing websocket handler")
	wsHandler := gateway.NewHandler(cfg.AuthToken, sessionStore)
	wsHandler.PushService = pushService
	if cfg.Relay.Enabled {
		wsHandler.DeviceTrust = trustStore
		wsHandler.NodeIdentity = nodeIdentityStore
	}
	logx.Info("bootstrap", "WebSocket handler ready")
	return wsHandler
}

func relayDeviceTrustStore(cfg config.Config) (*e2ee.DeviceTrustStore, error) {
	if !cfg.Relay.Enabled {
		return nil, nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve relay device trust home: %w", err)
	}
	return e2ee.LoadOrCreateDeviceTrustStore(e2ee.DefaultDeviceTrustPath(homeDir))
}

func relayNodeIdentityStore(cfg config.Config) (*e2ee.NodeIdentityStore, error) {
	if !cfg.Relay.Enabled {
		return nil, nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve relay node identity home: %w", err)
	}
	return e2ee.LoadOrCreateNodeIdentityStore(e2ee.DefaultNodeIdentityPath(homeDir))
}

func startRelayClient(cfg config.Config, wsHandler *gateway.Handler, trustStore *e2ee.DeviceTrustStore, nodeIdentityStore *e2ee.NodeIdentityStore) error {
	if !cfg.Relay.Enabled {
		return nil
	}
	relayCfg, err := relayConfig(cfg, trustStore, nodeIdentityStore)
	if err != nil {
		return err
	}
	go func() {
		err := relayclient.Run(context.Background(), relayCfg, wsHandler, relayclient.EmitPairingFile)
		if err != nil {
			logx.Error("relay", "relay client stopped: %v", err)
		}
	}()
	return nil
}

func relayConfig(cfg config.Config, trustStore *e2ee.DeviceTrustStore, nodeIdentityStore *e2ee.NodeIdentityStore) (relayclient.Config, error) {
	if nodeIdentityStore == nil {
		return relayclient.Config{}, fmt.Errorf("relay node identity store is required")
	}
	identity, err := nodeIdentityStore.Current()
	if err != nil {
		return relayclient.Config{}, err
	}
	downloadRoot, err := relayclient.DefaultDownloadRoot(cfg.Runtime.WorkspaceRoot)
	if err != nil {
		return relayclient.Config{}, fmt.Errorf("resolve relay download root: %w", err)
	}
	selectedRoutes, err := localSelectedRoutePolicy(cfg)
	if err != nil {
		return relayclient.Config{}, err
	}
	return relayclient.Config{
		RelayURL:           cfg.Relay.URL,
		PairingTTL:         cfg.Relay.PairingTTL,
		AgentGracePeriod:   cfg.Relay.AgentGracePeriod,
		PairingEventPath:   cfg.Relay.PairingEventPath,
		SessionStatePath:   cfg.Relay.SessionStatePath,
		DownloadRoots:      []string{downloadRoot},
		SelectedRoutes:     selectedRoutes,
		Capabilities:       e2ee.ProductionCapabilities(),
		NodeFingerprintHex: fmt.Sprintf("%x", identity.Fingerprint),
		NodeIdentityStore:  nodeIdentityStore,
		DeviceTrust:        trustStore,
		LANHost:            relayPairingLANHost(cfg),
		LANPort:            relayPairingLANPort(cfg),
		LANToken:           relayPairingLANToken(cfg),
		LANCWD:             strings.TrimSpace(cfg.Runtime.WorkspaceRoot),
	}, nil
}

func relayPairingLANHost(cfg config.Config) string {
	if cfg.Network.ExposureMode == config.ExposureModeRelayOnly {
		return ""
	}
	return strings.TrimSpace(os.Getenv("MOBILEVC_LAN_HOST"))
}

func relayPairingLANPort(cfg config.Config) string {
	if relayPairingLANHost(cfg) == "" {
		return ""
	}
	return strings.TrimSpace(cfg.Port)
}

func relayPairingLANToken(cfg config.Config) string {
	if relayPairingLANHost(cfg) == "" {
		return ""
	}
	return strings.TrimSpace(cfg.AuthToken)
}

func localSelectedRoutePolicy(cfg config.Config) (relay.SelectedRoutePolicy, error) {
	policy, err := relay.SelectedRoutePolicyFromAllowlists(
		strings.TrimSpace(cfg.Relay.HTTPAllowlist),
		strings.TrimSpace(cfg.Relay.WSAllowlist),
	)
	if err != nil {
		return relay.SelectedRoutePolicy{}, fmt.Errorf("load relay selected route policy: %w", err)
	}
	return policy, nil
}

func initTTSHandler(cfg config.Config) *tts.HTTPHandler {
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
	return ttsHandler
}

func buildMux(cfg config.Config, wsHandler *gateway.Handler, ttsHandler *tts.HTTPHandler) *http.ServeMux {
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
		serveDownload(w, r, cfg.AuthToken)
	})
	if ttsHandler != nil {
		mux.HandleFunc("/api/tts/synthesize", ttsHandler.HandleSynthesize)
		mux.HandleFunc("/api/tts/healthz", ttsHandler.HandleHealthz)
		logx.Info("bootstrap", "Registered routes: /ws, /healthz, /version, /download, /api/tts/synthesize, /api/tts/healthz, /")
	} else {
		logx.Info("bootstrap", "Registered routes: /ws, /healthz, /version, /download, /")
	}

	// Serve static files with correct MIME types
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Set correct MIME type for JavaScript files
		if filepath.Ext(r.URL.Path) == ".js" {
			w.Header().Set("Content-Type", "application/javascript")
		}
		http.FileServer(http.FS(staticFS)).ServeHTTP(w, r)
	})
	return mux
}

func startHTTPServer(addr string, summary config.Summary, mux *http.ServeMux, startedAt time.Time) {
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
	logx.Info("bootstrap", "Ready: addr=%s exposureMode=%s health=%s version=%s ws=%s startup=%s",
		addr,
		summary.ExposureMode,
		summary.HealthURL,
		summary.VersionURL,
		summary.WebSocketURL,
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
