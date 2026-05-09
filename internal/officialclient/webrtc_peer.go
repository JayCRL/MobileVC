package officialclient

import (
	"encoding/json"
	"sync"

	"mobilevc/internal/logx"

	"github.com/pion/webrtc/v4"
)

type PeerManager struct {
	mu                sync.Mutex
	peers             map[string]*webrtc.PeerConnection
	dcs               map[string]*webrtc.DataChannel
	pendingCandidates map[string][]json.RawMessage
	defaultConfig     webrtc.Configuration
	signal            func(peerID string, data json.RawMessage)
	onData            func(peerID string, msg []byte)
}

type PeerManagerConfig struct {
	STUNURLs []string
	TURNURLs []string
	TURNUser string
	TURNPass string
}

func NewPeerManager(cfg PeerManagerConfig, signalFn func(peerID string, data json.RawMessage), onDataFn func(peerID string, msg []byte)) *PeerManager {
	pm := &PeerManager{
		peers:             make(map[string]*webrtc.PeerConnection),
		dcs:               make(map[string]*webrtc.DataChannel),
		pendingCandidates: make(map[string][]json.RawMessage),
		signal:            signalFn,
		onData:            onDataFn,
	}
	pm.defaultConfig = pm.buildConfig(cfg)
	return pm
}

func (pm *PeerManager) buildConfig(cfg PeerManagerConfig) webrtc.Configuration {
	var servers []webrtc.ICEServer
	if len(cfg.STUNURLs) > 0 {
		servers = append(servers, webrtc.ICEServer{URLs: cfg.STUNURLs})
	} else {
		servers = append(servers, webrtc.ICEServer{URLs: []string{"stun:stun.l.google.com:19302"}})
	}
	if len(cfg.TURNURLs) > 0 {
		servers = append(servers, webrtc.ICEServer{
			URLs:       cfg.TURNURLs,
			Username:   cfg.TURNUser,
			Credential: cfg.TURNPass,
		})
	}
	return webrtc.Configuration{ICEServers: servers}
}

func (pm *PeerManager) HandleOffer(peerID string, sdpType string, sdp string) error {
	pm.mu.Lock()
	existing, exists := pm.peers[peerID]
	pm.mu.Unlock()

	if exists {
		existing.Close()
	}

	pc, err := webrtc.NewPeerConnection(pm.defaultConfig)
	if err != nil {
		return err
	}

	pm.mu.Lock()
	pm.peers[peerID] = pc
	pm.mu.Unlock()

	// Handle DataChannel from mobile
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		logx.Info("webrtc", "DataChannel received: %s peer=%s", dc.Label(), peerID)
		pm.mu.Lock()
		pm.dcs[peerID] = dc
		pm.mu.Unlock()

		dc.OnOpen(func() {
			logx.Info("webrtc", "DataChannel open: peer=%s", peerID)
		})
		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			if pm.onData != nil {
				pm.onData(peerID, msg.Data)
			}
		})
		dc.OnClose(func() {
			logx.Info("webrtc", "DataChannel closed: peer=%s", peerID)
		})
	})

	// Handle ICE candidates
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			logx.Info("webrtc", "ICE candidate gathering complete for peer=%s", peerID)
			return
		}
		logx.Info("webrtc", "ICE candidate: type=%s address=%s port=%d peer=%s",
			c.Typ.String(), c.Address, c.Port, peerID)
		candidate := c.ToJSON()
		data, _ := json.Marshal(map[string]interface{}{
			"candidate":     candidate.Candidate,
			"sdpMid":        candidate.SDPMid,
			"sdpMLineIndex": candidate.SDPMLineIndex,
		})
		if pm.signal != nil {
			logx.Info("webrtc", "sending candidate via signaling: type=%s peer=%s mid=%s",
				c.Typ.String(), peerID, candidate.SDPMid)
			pm.signal(peerID, data)
		} else {
			logx.Error("webrtc", "signal callback is nil — candidate NOT sent! peer=%s", peerID)
		}
	})

	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		logx.Info("webrtc", "ICE state: %s peer=%s", state.String(), peerID)
		if state == webrtc.ICEConnectionStateFailed {
			pc.Close()
			pm.mu.Lock()
			delete(pm.peers, peerID)
			delete(pm.dcs, peerID)
			delete(pm.pendingCandidates, peerID)
			pm.mu.Unlock()
		}
		// Disconnected is transient — ICE agent can recover on its own.
		if state == webrtc.ICEConnectionStateDisconnected {
			logx.Info("webrtc", "ICE disconnected (transient), waiting for recovery: peer=%s", peerID)
		}
	})

	// Set remote description (offer from mobile)
	sdpTypeEnum := webrtc.SDPTypeOffer
	if sdpType == "answer" {
		sdpTypeEnum = webrtc.SDPTypeAnswer
	}
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: sdpTypeEnum,
		SDP:  sdp,
	}); err != nil {
		return err
	}

	// Replay any pending candidates that arrived before the PC was created
	pm.mu.Lock()
	pending := pm.pendingCandidates[peerID]
	delete(pm.pendingCandidates, peerID)
	pm.mu.Unlock()
	for _, data := range pending {
		var c struct {
			Candidate     string `json:"candidate"`
			SDPMid        string `json:"sdpMid"`
			SDPMLineIndex uint16 `json:"sdpMLineIndex"`
		}
		if err := json.Unmarshal(data, &c); err != nil {
			continue
		}
		if err := pc.AddICECandidate(webrtc.ICECandidateInit{
			Candidate: c.Candidate,
		}); err != nil {
			logx.Error("webrtc", "replay candidate error: %v", err)
		}
	}
	if len(pending) > 0 {
		logx.Info("webrtc", "replayed %d pending candidates for peer=%s", len(pending), peerID)
	}

	// Create answer
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return err
	}
	if err := pc.SetLocalDescription(answer); err != nil {
		return err
	}

	// Send answer back via signaling
	answerData, _ := json.Marshal(map[string]string{
		"type": answer.Type.String(),
		"sdp":  answer.SDP,
	})
	if pm.signal != nil {
		logx.Info("webrtc", "sending answer via signaling: peer=%s", peerID)
		pm.signal(peerID, answerData)
	} else {
		logx.Error("webrtc", "signal callback is nil — answer NOT sent! peer=%s", peerID)
	}

	logx.Info("webrtc", "answer sent to peer=%s", peerID)
	return nil
}

func (pm *PeerManager) HandleRemoteCandidate(peerID string, candidateData json.RawMessage) error {
	pm.mu.Lock()
	pc, ok := pm.peers[peerID]
	pm.mu.Unlock()
	if !ok {
		// PC not created yet — cache candidate for later replay
		pm.mu.Lock()
		pm.pendingCandidates[peerID] = append(pm.pendingCandidates[peerID], candidateData)
		pm.mu.Unlock()
		return nil
	}

	var c struct {
		Candidate     string `json:"candidate"`
		SDPMid        string `json:"sdpMid"`
		SDPMLineIndex uint16 `json:"sdpMLineIndex"`
	}
	if err := json.Unmarshal(candidateData, &c); err != nil {
		return err
	}

	return pc.AddICECandidate(webrtc.ICECandidateInit{
		Candidate: c.Candidate,
	})
}

func (pm *PeerManager) Send(peerID string, data []byte) error {
	pm.mu.Lock()
	dc, ok := pm.dcs[peerID]
	pm.mu.Unlock()
	if !ok || dc == nil {
		return nil
	}
	return dc.Send(data)
}

func (pm *PeerManager) ClosePeer(peerID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if pc, ok := pm.peers[peerID]; ok {
		pc.Close()
		delete(pm.peers, peerID)
	}
	if dc, ok := pm.dcs[peerID]; ok {
		dc.Close()
		delete(pm.dcs, peerID)
	}
	delete(pm.pendingCandidates, peerID)
}
