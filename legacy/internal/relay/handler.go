package relay

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"mobilevc/internal/logx"
)

var errFrameTooLarge = errors.New("frame too large")

func (s *Server) handleAgent(w http.ResponseWriter, r *http.Request) {
	remote := s.clientIP(r)
	if err := s.acquireConn(roleAgent, remote); err != nil {
		http.Error(w, NewErrorFrame(CodeCapacityReached).Message, http.StatusTooManyRequests)
		return
	}
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.releaseConn(roleAgent, remote)
		logx.Warn("relay", "agent upgrade failed: remote=%s err=%v", r.RemoteAddr, err)
		return
	}
	go s.agentLoop(newPeerConn(conn, roleAgent, remote, s.cfg.ForwardQueueSize, r.UserAgent()))
}

func (s *Server) handleClient(w http.ResponseWriter, r *http.Request) {
	remote := s.clientIP(r)
	if err := s.acquireConn(roleClient, remote); err != nil {
		http.Error(w, NewErrorFrame(CodeCapacityReached).Message, http.StatusTooManyRequests)
		return
	}
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.releaseConn(roleClient, remote)
		logx.Warn("relay", "client upgrade failed: remote=%s err=%v", r.RemoteAddr, err)
		return
	}
	go s.clientLoop(newPeerConn(conn, roleClient, remote, s.cfg.ForwardQueueSize, r.UserAgent()))
}

func (s *Server) agentLoop(peer *peerConn) {
	defer s.releaseConn(peer.role, peer.remote)
	defer peer.Stop()
	peer.ConfigurePong(s.cfg.PingInterval + s.cfg.PongTimeout)
	go peer.StartWriter(s.cfg.PingInterval)
	sessionID, err := s.authenticateAgent(peer)
	if err != nil {
		writeError(peer, errorCode(err, CodeUnauthorized))
		return
	}
	logx.Info("relay", "agent connected: sessionID=%s remote=%s", sessionID, peer.remote)
	s.forwardLoop(peer, sessionID, DirectionAgentToClient)
	s.markAgentDisconnected(sessionID, peer)
}

func (s *Server) clientLoop(peer *peerConn) {
	defer s.releaseConn(peer.role, peer.remote)
	defer peer.Stop()
	peer.ConfigurePong(s.cfg.PingInterval + s.cfg.PongTimeout)
	go peer.StartWriter(s.cfg.PingInterval)
	sessionID, clientID, err := s.authenticateClient(peer, peer.remote)
	if err != nil {
		writeError(peer, errorCode(err, CodePairingRejected))
		return
	}
	logx.Info("relay", "client paired: sessionID=%s clientID=%s remote=%s", sessionID, clientID, peer.remote)
	s.forwardLoop(peer, sessionID, DirectionClientToAgent)
	s.markClientDisconnected(sessionID, peer)
}

func (s *Server) authenticateAgent(peer *peerConn) (string, error) {
	_ = peer.conn.SetReadDeadline(time.Now().Add(s.cfg.AgentRegisterTimeout))
	frame, raw, err := readControl(peer.conn, s.cfg.MaxControlFrameBytes)
	if err != nil {
		return "", err
	}
	sessionID, err := s.applyAgentAuthFrame(peer, frame, raw)
	if err != nil {
		return "", err
	}
	_ = peer.conn.SetReadDeadline(time.Now().Add(s.cfg.PingInterval + s.cfg.PongTimeout))
	return sessionID, nil
}

func (s *Server) applyAgentAuthFrame(peer *peerConn, frame ControlFrame, raw []byte) (string, error) {
	if frame.Type == TypeAgentRegister {
		return s.registerAgent(peer, raw)
	}
	if frame.Type == TypeAgentReconnect {
		return s.reconnectAgent(peer, raw)
	}
	return "", errors.New("first agent frame must register or reconnect")
}

func (s *Server) authenticateClient(peer *peerConn, remote string) (string, string, error) {
	_ = peer.conn.SetReadDeadline(time.Now().Add(s.cfg.PairingHandshakeTimeout))
	frame, raw, err := readControl(peer.conn, s.cfg.MaxControlFrameBytes)
	if err != nil {
		return "", "", err
	}
	sessionID, clientID, err := s.applyClientAuthFrame(peer, frame, raw, remote)
	if err != nil {
		return "", "", err
	}
	_ = peer.conn.SetReadDeadline(time.Now().Add(s.cfg.PingInterval + s.cfg.PongTimeout))
	return sessionID, clientID, nil
}

func (s *Server) applyClientAuthFrame(peer *peerConn, frame ControlFrame, raw []byte, remote string) (string, string, error) {
	if frame.Type == TypeClientPair {
		return s.pairClient(peer, raw, remote)
	}
	if frame.Type == TypeClientReconnect {
		return s.reconnectClient(peer, raw)
	}
	return "", "", errors.New("first client frame must pair or reconnect")
}

func readControl(conn *websocket.Conn, limit int64) (ControlFrame, []byte, error) {
	_, reader, err := conn.NextReader()
	if err != nil {
		return ControlFrame{}, nil, err
	}
	raw, err := readLimitedFrame(reader, limit)
	if err != nil {
		return ControlFrame{}, nil, err
	}
	return decodeControlFrame(raw)
}

func readLimitedFrame(reader io.Reader, limit int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, errFrameTooLarge
	}
	return raw, nil
}

func decodeControlFrame(raw []byte) (ControlFrame, []byte, error) {
	var frame ControlFrame
	if err := json.Unmarshal(raw, &frame); err != nil {
		return ControlFrame{}, nil, err
	}
	if frame.Version != Version || strings.TrimSpace(frame.Type) == "" {
		return ControlFrame{}, nil, errors.New("invalid control frame")
	}
	return frame, raw, nil
}

func remoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
