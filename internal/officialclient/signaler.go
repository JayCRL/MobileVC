package officialclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"mobilevc/internal/logx"

	"github.com/gorilla/websocket"
)

// SignalingMessage is a message on the signaling WebSocket.
type SignalingMessage struct {
	Type   string          `json:"type"`
	NodeID string          `json:"nodeId,omitempty"`
	PeerID string          `json:"peerId,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
	Accept *bool           `json:"accept,omitempty"`
	Reason string          `json:"reason,omitempty"`
}

type Signaler struct {
	serverURL   string
	token       string
	nodeID      string
	conn        *websocket.Conn
	writeMu     sync.Mutex
	peerManager *PeerManager
	onRequest   func(peerID string)
}

func NewSignaler(serverURL, token, nodeID string) *Signaler {
	return &Signaler{
		serverURL: serverURL,
		token:     token,
		nodeID:    nodeID,
	}
}

func (s *Signaler) SetPeerManager(pm *PeerManager) {
	s.peerManager = pm
}

func (s *Signaler) OnConnectRequest(cb func(peerID string)) {
	s.onRequest = cb
}

func (s *Signaler) Connect(ctx context.Context) error {
	wsURL := httpToWS(s.serverURL) + "/ws/signaling?token=" + url.QueryEscape(s.token)

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, resp, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("signaling dial failed: %v (status=%d)", err, resp.StatusCode)
		}
		return fmt.Errorf("signaling dial failed: %w", err)
	}

	s.conn = conn
	logx.Info("signaling", "connected to official server")

	// Announce node
	announce := SignalingMessage{
		Type:   "node_online",
		NodeID: s.nodeID,
	}
	if err := conn.WriteJSON(announce); err != nil {
		return fmt.Errorf("announce node: %w", err)
	}
	logx.Info("signaling", "node announced: id=%s", s.nodeID)

	// Start reader
	go s.readLoop(ctx)

	return nil
}

func (s *Signaler) readLoop(ctx context.Context) {
	defer func() {
		s.conn.Close()
		logx.Info("signaling", "disconnected")
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		var msg SignalingMessage
		if err := s.conn.ReadJSON(&msg); err != nil {
			if !websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				logx.Info("signaling", "connection closed")
			} else {
				logx.Error("signaling", "read error: %v", err)
			}
			return
		}

		logx.Info("signaling", "received: type=%s from=%s", msg.Type, msg.PeerID)

		switch msg.Type {
		case "connect_request":
			if s.onRequest != nil {
				s.onRequest(msg.PeerID)
			}
			// Auto-accept for now
			s.sendMessage(SignalingMessage{
				Type:   "connect_response",
				PeerID: msg.PeerID,
				Accept: boolPtr(true),
			})
		case "webrtc":
			if s.peerManager != nil && msg.Data != nil {
				var wr struct {
					SDP       string `json:"sdp"`
					Type      string `json:"type"`
					Candidate string `json:"candidate"`
				}
				if err := json.Unmarshal(msg.Data, &wr); err != nil {
					logx.Error("signaling", "webrtc unmarshal error: %v", err)
				} else if wr.SDP != "" {
					logx.Info("webrtc", "HandleOffer: peer=%s type=%s sdp_len=%d", msg.PeerID, wr.Type, len(wr.SDP))
					if err := s.peerManager.HandleOffer(msg.PeerID, wr.Type, wr.SDP); err != nil {
						logx.Error("webrtc", "HandleOffer error: %v", err)
					}
				} else if wr.Candidate != "" {
					if err := s.peerManager.HandleRemoteCandidate(msg.PeerID, msg.Data); err != nil {
						logx.Error("webrtc", "HandleRemoteCandidate error: %v", err)
					}
				}
			}
		}
	}
}

func (s *Signaler) SendWebRTC(peerID string, data json.RawMessage) {
	s.sendMessage(SignalingMessage{
		Type:   "webrtc",
		PeerID: peerID,
		Data:   data,
	})
}

func (s *Signaler) sendMessage(msg SignalingMessage) {
	if s.conn == nil {
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.conn.WriteJSON(msg); err != nil {
		logx.Error("signaling", "write error: %v", err)
	}
}

func (s *Signaler) Close() {
	if s.conn != nil {
		s.conn.Close()
	}
}

func httpToWS(httpURL string) string {
	u := strings.Replace(httpURL, "http://", "ws://", 1)
	u = strings.Replace(u, "https://", "wss://", 1)
	return strings.TrimRight(u, "/")
}

func boolPtr(v bool) *bool {
	return &v
}
