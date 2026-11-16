package relay

import (
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"mobilevc/internal/relay/e2ee"
)

type Server struct {
	cfg            Config
	upgrader       websocket.Upgrader
	trustedProxies []*net.IPNet
	stateStore     *stateStore
	mu             sync.Mutex
	sessions       map[string]*sessionState
	agentConns     int
	clientConns    int
	connCountByIP  map[string]int
}

type sessionState struct {
	id                   string
	pairingHash          string
	agentReconnectHash   string
	pairingExpiresAt     time.Time
	agentDisconnectedAt  time.Time
	agent                *peerConn
	client               *peerConn
	clientID             string
	clientReconnectHash  string
	capabilities         e2ee.CapabilitySet
	pairingConsumed      bool
	pairFailuresByRemote map[string]int
	devices              map[string]*deviceState
}

type deviceState struct {
	ClientID      string
	Name          string
	ReconnectHash string
	CreatedAt     time.Time
	LastSeenAt    time.Time
	Revoked       bool
}

func (s *sessionState) hasReconnectableDevices() bool {
	if s == nil {
		return false
	}
	for _, device := range s.devices {
		if device != nil && !device.Revoked {
			return true
		}
	}
	return false
}

func (s *sessionState) agentDisconnectedWithinGrace(grace time.Duration) bool {
	if s == nil || s.agent != nil || s.agentDisconnectedAt.IsZero() {
		return false
	}
	return time.Since(s.agentDisconnectedAt) <= grace
}

func NewServer(cfg Config) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	trustedProxies, err := parseTrustedProxyCIDRs(cfg.TrustedProxyCIDRs)
	if err != nil {
		return nil, err
	}
	store := newStateStore(cfg.StatePath)
	sessions, err := store.load()
	if err != nil {
		return nil, err
	}
	return &Server{
		cfg:            cfg,
		trustedProxies: trustedProxies,
		stateStore:     store,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin:     func(*http.Request) bool { return true },
		},
		sessions:      sessions,
		connCountByIP: make(map[string]int),
	}, nil
}

func (s *Server) Handler(version string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.healthz)
	mux.HandleFunc("/", s.dashboard)
	mux.HandleFunc("/dashboard", s.dashboard)
	mux.HandleFunc("/stats", s.stats)
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"version":%q}`, version)
	})
	mux.HandleFunc("/relay/agent", s.handleAgent)
	mux.HandleFunc("/relay/client", s.handleClient)
	return mux
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
