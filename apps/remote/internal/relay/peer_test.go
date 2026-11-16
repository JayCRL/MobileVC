package relay

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestPeerStopDoesNotWaitForPingInterval(t *testing.T) {
	serverConn, clientConn := newPeerTestConns(t)
	defer clientConn.Close()

	peer := newPeerConn(serverConn, roleAgent, "127.0.0.1", 1, "")
	go peer.StartWriter(time.Hour)

	started := time.Now()
	peer.Stop()
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("peer stop took %s", elapsed)
	}
}

func TestPeerConfigurePongAllowsFirstPingInterval(t *testing.T) {
	serverConn, clientConn := newPeerTestConns(t)
	defer clientConn.Close()
	peer := newPeerConn(serverConn, roleAgent, "127.0.0.1", 1, "")

	peer.ConfigurePong(40 * time.Millisecond)
	if err := peer.ReadJSON(&ControlFrame{}); err == nil {
		t.Fatal("expected read timeout")
	} else if !isTimeoutError(err) {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func newPeerTestConns(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		accepted <- conn
	}))
	t.Cleanup(server.Close)

	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial peer test: %v", err)
	}
	select {
	case serverConn := <-accepted:
		return serverConn, client
	case <-time.After(time.Second):
		t.Fatal("server websocket was not accepted")
	}
	return nil, nil
}

func isTimeoutError(err error) bool {
	netErr, ok := err.(net.Error)
	return ok && netErr.Timeout()
}
