package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"

	"mobilevc/internal/adb"
	"mobilevc/internal/logx"
	"mobilevc/internal/protocol"
)

const adbWebRTCGatherTimeout = 12 * time.Second

type adbWebRTCBridge struct {
	mu                  sync.Mutex
	peer                *webrtc.PeerConnection
	cancel              context.CancelFunc
	streamCancel        context.CancelFunc
	streamToken         int
	lastKeyframeRequest time.Time
	serial              string
	screenSize          adb.Size
	sessionIDFn         func() string
	emit                func(any)
}

type adbControlMessage struct {
	Type       string `json:"type"`
	X          int    `json:"x,omitempty"`
	Y          int    `json:"y,omitempty"`
	StartX     int    `json:"startX,omitempty"`
	StartY     int    `json:"startY,omitempty"`
	EndX       int    `json:"endX,omitempty"`
	EndY       int    `json:"endY,omitempty"`
	DurationMS int    `json:"durationMs,omitempty"`
	Keycode    string `json:"keycode,omitempty"`
}

func newADBWebRTCBridge(sessionIDFn func() string, emit func(any)) *adbWebRTCBridge {
	return &adbWebRTCBridge{
		sessionIDFn: sessionIDFn,
		emit:        emit,
	}
}

func (b *adbWebRTCBridge) Stop(message string) {
	b.mu.Lock()
	peer := b.peer
	cancel := b.cancel
	serial := b.serial
	screenSize := b.screenSize
	b.peer = nil
	b.cancel = nil
	b.streamCancel = nil
	b.streamToken = 0
	b.lastKeyframeRequest = time.Time{}
	b.serial = ""
	b.screenSize = adb.Size{}
	b.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if peer != nil {
		_ = peer.Close()
	}
	if strings.TrimSpace(message) != "" {
		b.emit(protocol.NewADBWebRTCStateEvent(b.sessionID(), false, false, serial, screenSize.Width, screenSize.Height, message))
	}
}

func (b *adbWebRTCBridge) HandleOffer(ctx context.Context, serial, sdpType, sdp string, iceServers []protocol.WebRTCIceServer) error {
	if strings.TrimSpace(sdp) == "" {
		return fmt.Errorf("缺少 WebRTC SDP offer")
	}
	if strings.TrimSpace(sdpType) == "" {
		sdpType = webrtc.SDPTypeOffer.String()
	}
	if !strings.EqualFold(strings.TrimSpace(sdpType), webrtc.SDPTypeOffer.String()) {
		return fmt.Errorf("仅支持 WebRTC offer，收到 %q", sdpType)
	}

	b.Stop("")

	resolvedSerial, err := adb.ResolveSerial(ctx, serial)
	if err != nil {
		return err
	}
	screenSize, err := adb.ResolveScreenSize(ctx, resolvedSerial)
	if err != nil {
		return err
	}

	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterDefaultCodecs(); err != nil {
		return fmt.Errorf("register webrtc codecs failed: %w", err)
	}
	forceRelay := shouldForceRelay(iceServers)
	api := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine))
	peer, err := api.NewPeerConnection(webrtc.Configuration{
		ICEServers:         buildICEServers(iceServers),
		ICETransportPolicy: relayPolicy(forceRelay),
	})
	if err != nil {
		return fmt.Errorf("create peer connection failed: %w", err)
	}
	if forceRelay {
		b.emit(protocol.NewADBWebRTCStateEvent(
			b.sessionID(),
			true,
			false,
			resolvedSerial,
			screenSize.Width,
			screenSize.Height,
			"服务端 WebRTC 使用 relay-only 模式",
		))
	}

	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeH264,
			ClockRate:   90000,
			SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f",
		},
		"adb-video",
		"mobilevc-adb",
	)
	if err != nil {
		_ = peer.Close()
		return fmt.Errorf("create h264 track failed: %w", err)
	}
	sender, err := peer.AddTrack(track)
	if err != nil {
		_ = peer.Close()
		return fmt.Errorf("add h264 track failed: %w", err)
	}
	go drainRTCP(sender, b.requestKeyframe)

	streamCtx, cancel := context.WithCancel(ctx)
	var startVideoOnce sync.Once
	b.mu.Lock()
	b.peer = peer
	b.cancel = cancel
	b.serial = resolvedSerial
	b.screenSize = screenSize
	b.mu.Unlock()

	peer.OnDataChannel(func(dataChannel *webrtc.DataChannel) {
		if dataChannel.Label() != "adb-control" {
			return
		}
		dataChannel.OnMessage(func(msg webrtc.DataChannelMessage) {
			if msg.IsString {
				if err := b.handleControlMessage(streamCtx, resolvedSerial, screenSize, msg.Data); err != nil {
					b.emit(protocol.NewErrorEvent(b.sessionID(), err.Error(), ""))
				}
			}
		})
	})

	peer.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		message := "WebRTC 状态：" + state.String()
		running := state != webrtc.PeerConnectionStateClosed &&
			state != webrtc.PeerConnectionStateFailed &&
			state != webrtc.PeerConnectionStateDisconnected
		connected := state == webrtc.PeerConnectionStateConnected
		b.emit(protocol.NewADBWebRTCStateEvent(b.sessionID(), running, connected, resolvedSerial, screenSize.Width, screenSize.Height, message))
		if connected {
			startVideoOnce.Do(func() {
				go b.streamVideo(streamCtx, resolvedSerial, screenSize, track)
			})
		}
		switch state {
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			b.Stop("")
		}
	})
	peer.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		message := "服务端 ICE 状态：" + state.String()
		b.emit(protocol.NewADBWebRTCStateEvent(b.sessionID(), true, false, resolvedSerial, screenSize.Width, screenSize.Height, message))
		logx.Info("ws", "adb webrtc ice state changed: sessionID=%s serial=%s state=%s", b.sessionID(), resolvedSerial, state.String())
	})
	peer.OnICEGatheringStateChange(func(state webrtc.ICEGatheringState) {
		message := "服务端 ICE 收集：" + state.String()
		b.emit(protocol.NewADBWebRTCStateEvent(b.sessionID(), true, false, resolvedSerial, screenSize.Width, screenSize.Height, message))
		logx.Info("ws", "adb webrtc gathering state changed: sessionID=%s serial=%s state=%s", b.sessionID(), resolvedSerial, state.String())
	})
	peer.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
		logx.Info(
			"ws",
			"adb webrtc local candidate: sessionID=%s serial=%s type=%s protocol=%s address=%s port=%d",
			b.sessionID(),
			resolvedSerial,
			candidate.Typ.String(),
			candidate.Protocol.String(),
			candidate.Address,
			candidate.Port,
		)
	})

	offerSummary := summarizeSDPCandidates(sdp)
	b.emit(protocol.NewADBWebRTCStateEvent(
		b.sessionID(),
		true,
		false,
		resolvedSerial,
		screenSize.Width,
		screenSize.Height,
		"客户端 Offer 候选: "+offerSummary.String(),
	))
	if forceRelay && offerSummary.Relay == 0 {
		b.Stop("")
		return fmt.Errorf("TURN 未返回客户端 relay 候选，请检查手机到 TURN 的 3478/UDP、3478/TCP 与凭据")
	}

	if err := peer.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  sdp,
	}); err != nil {
		b.Stop("")
		return fmt.Errorf("set remote description failed: %w", err)
	}

	answer, err := peer.CreateAnswer(nil)
	if err != nil {
		b.Stop("")
		return fmt.Errorf("create webrtc answer failed: %w", err)
	}

	gatherComplete := webrtc.GatheringCompletePromise(peer)
	if err := peer.SetLocalDescription(answer); err != nil {
		b.Stop("")
		return fmt.Errorf("set local description failed: %w", err)
	}
	select {
	case <-gatherComplete:
	case <-time.After(adbWebRTCGatherTimeout):
		logx.Warn("ws", "adb webrtc answer gathering timed out; sending partial candidates: sessionID=%s serial=%s", b.sessionID(), resolvedSerial)
	case <-ctx.Done():
		b.Stop("")
		return ctx.Err()
	}

	localDescription := peer.LocalDescription()
	if localDescription == nil {
		b.Stop("")
		return fmt.Errorf("missing local description after answer")
	}
	answerSummary := summarizeSDPCandidates(localDescription.SDP)
	b.emit(protocol.NewADBWebRTCStateEvent(
		b.sessionID(),
		true,
		false,
		resolvedSerial,
		screenSize.Width,
		screenSize.Height,
		"服务端 Answer 候选: "+answerSummary.String(),
	))
	if forceRelay && answerSummary.Relay == 0 {
		b.Stop("")
		return fmt.Errorf("TURN 未返回服务端 relay 候选，请检查 TURN 的 external-ip、3478/UDP、3478/TCP 与凭据配置")
	}

	b.emit(protocol.NewADBWebRTCAnswerEvent(b.sessionID(), resolvedSerial, localDescription.Type.String(), localDescription.SDP))
	b.emit(protocol.NewADBWebRTCStateEvent(b.sessionID(), true, false, resolvedSerial, screenSize.Width, screenSize.Height, "WebRTC 会话已建立，等待连接后启动 H264 推流…"))

	return nil
}

func buildICEServers(configs []protocol.WebRTCIceServer) []webrtc.ICEServer {
	if len(configs) == 0 {
		return nil
	}
	servers := make([]webrtc.ICEServer, 0, len(configs))
	for _, config := range configs {
		urls := make([]string, 0, len(config.URLs))
		for _, rawURL := range config.URLs {
			if trimmed := strings.TrimSpace(rawURL); trimmed != "" {
				urls = append(urls, trimmed)
			}
		}
		if len(urls) == 0 {
			continue
		}
		servers = append(servers, webrtc.ICEServer{
			URLs:       urls,
			Username:   strings.TrimSpace(config.Username),
			Credential: strings.TrimSpace(config.Credential),
		})
	}
	return servers
}

func relayPolicy(forceRelay bool) webrtc.ICETransportPolicy {
	if forceRelay {
		return webrtc.ICETransportPolicyRelay
	}
	return webrtc.ICETransportPolicyAll
}

func shouldForceRelay(configs []protocol.WebRTCIceServer) bool {
	for _, config := range configs {
		for _, rawURL := range config.URLs {
			host := iceURLHost(rawURL)
			if isLikelyPublicHost(host) && isTurnURL(rawURL) {
				return true
			}
		}
	}
	return false
}

func isTurnURL(rawURL string) bool {
	normalized := strings.ToLower(strings.TrimSpace(rawURL))
	return strings.HasPrefix(normalized, "turn:") || strings.HasPrefix(normalized, "turns:")
}

func iceURLHost(rawURL string) string {
	normalized := strings.TrimSpace(rawURL)
	lower := strings.ToLower(normalized)
	for _, prefix := range []string{"turns:", "turn:", "stuns:", "stun:"} {
		if !strings.HasPrefix(lower, prefix) {
			continue
		}
		rest := normalized[len(prefix):]
		if strings.HasPrefix(rest, "//") {
			rest = rest[2:]
		}
		if index := strings.Index(rest, "?"); index >= 0 {
			rest = rest[:index]
		}
		if strings.HasPrefix(rest, "[") {
			if index := strings.Index(rest, "]"); index > 0 {
				return rest[1:index]
			}
			return strings.Trim(rest, "[]")
		}
		if host, _, err := net.SplitHostPort(rest); err == nil {
			return host
		}
		if index := strings.LastIndex(rest, ":"); index > 0 {
			return rest[:index]
		}
		return rest
	}
	return ""
}

func isLikelyPublicHost(rawHost string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(rawHost))
	if trimmed == "" {
		return false
	}
	if trimmed == "localhost" || trimmed == "127.0.0.1" || trimmed == "::1" || strings.HasSuffix(trimmed, ".local") {
		return false
	}
	ip := net.ParseIP(trimmed)
	if ip == nil {
		return true
	}
	return !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast()
}

type sdpCandidateSummary struct {
	Host  int
	Srflx int
	Prflx int
	Relay int
	UDP   int
	TCP   int
}

func summarizeSDPCandidates(sdp string) sdpCandidateSummary {
	summary := sdpCandidateSummary{}
	for _, line := range strings.Split(sdp, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || (!strings.HasPrefix(trimmed, "a=candidate:") && !strings.HasPrefix(trimmed, "candidate:")) {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(strings.TrimPrefix(trimmed, "a="), ""))
		if len(fields) < 8 {
			continue
		}
		protocol := strings.ToLower(strings.TrimSpace(fields[2]))
		candidateType := ""
		for i := 6; i+1 < len(fields); i++ {
			if strings.EqualFold(fields[i], "typ") {
				candidateType = strings.ToLower(strings.TrimSpace(fields[i+1]))
				break
			}
		}
		switch candidateType {
		case "host":
			summary.Host++
		case "srflx":
			summary.Srflx++
		case "prflx":
			summary.Prflx++
		case "relay":
			summary.Relay++
		}
		switch protocol {
		case "udp":
			summary.UDP++
		case "tcp", "ssltcp":
			summary.TCP++
		}
	}
	return summary
}

func (s sdpCandidateSummary) String() string {
	return fmt.Sprintf(
		"host=%d srflx=%d prflx=%d relay=%d udp=%d tcp=%d",
		s.Host,
		s.Srflx,
		s.Prflx,
		s.Relay,
		s.UDP,
		s.TCP,
	)
}

func (b *adbWebRTCBridge) handleControlMessage(ctx context.Context, serial string, screenSize adb.Size, payload []byte) error {
	var message adbControlMessage
	if err := json.Unmarshal(payload, &message); err != nil {
		return fmt.Errorf("解析 ADB 控制消息失败: %w", err)
	}
	switch strings.TrimSpace(strings.ToLower(message.Type)) {
	case "tap":
		if message.X < 0 || message.Y < 0 {
			return fmt.Errorf("adb tap 坐标必须为非负整数")
		}
		if screenSize.Width > 0 && message.X >= screenSize.Width {
			message.X = screenSize.Width - 1
		}
		if screenSize.Height > 0 && message.Y >= screenSize.Height {
			message.Y = screenSize.Height - 1
		}
		return adb.Tap(ctx, serial, message.X, message.Y)
	case "swipe":
		if message.StartX < 0 || message.StartY < 0 || message.EndX < 0 || message.EndY < 0 {
			return fmt.Errorf("adb swipe 坐标必须为非负整数")
		}
		if screenSize.Width > 0 {
			if message.StartX >= screenSize.Width {
				message.StartX = screenSize.Width - 1
			}
			if message.EndX >= screenSize.Width {
				message.EndX = screenSize.Width - 1
			}
		}
		if screenSize.Height > 0 {
			if message.StartY >= screenSize.Height {
				message.StartY = screenSize.Height - 1
			}
			if message.EndY >= screenSize.Height {
				message.EndY = screenSize.Height - 1
			}
		}
		return adb.Swipe(
			ctx,
			serial,
			message.StartX,
			message.StartY,
			message.EndX,
			message.EndY,
			message.DurationMS,
		)
	case "keyevent":
		if strings.TrimSpace(message.Keycode) == "" {
			return fmt.Errorf("adb keyevent keycode 不能为空")
		}
		return adb.Keyevent(ctx, serial, message.Keycode)
	default:
		return fmt.Errorf("暂不支持的 ADB 控制消息类型: %s", message.Type)
	}
}

func (b *adbWebRTCBridge) streamVideo(ctx context.Context, serial string, screenSize adb.Size, track *webrtc.TrackLocalStaticSample) {
	config := adb.H264StreamConfig{
		BitRate:      1_500_000,
		MaxDimension: 960,
		TimeLimit:    170 * time.Second,
		FrameRate:    30,
	}

	if err := adb.WarmupScreen(ctx, serial); err != nil && ctx.Err() == nil {
		logx.Warn("ws", "adb warmup failed: sessionID=%s serial=%s err=%v", b.sessionID(), serial, err)
	}

	for {
		if ctx.Err() != nil {
			return
		}
		streamCtx, streamCancel := context.WithCancel(ctx)
		b.mu.Lock()
		b.streamToken++
		token := b.streamToken
		b.streamCancel = streamCancel
		b.mu.Unlock()
		stream, err := adb.StartH264Stream(streamCtx, serial, config)
		if err != nil {
			streamCancel()
			b.clearStreamCancel(token)
			b.emit(protocol.NewADBWebRTCStateEvent(b.sessionID(), false, false, serial, screenSize.Width, screenSize.Height, err.Error()))
			b.Stop("")
			return
		}

		pumpErr := adb.PumpH264Stream(streamCtx, stream.Reader(), config, func(frame []byte, duration time.Duration) error {
			return track.WriteSample(media.Sample{Data: frame, Duration: duration})
		})
		closeErr := stream.Close()
		streamCancel()
		b.clearStreamCancel(token)
		if ctx.Err() != nil {
			return
		}
		if errors.Is(pumpErr, context.Canceled) {
			select {
			case <-ctx.Done():
				return
			case <-time.After(120 * time.Millisecond):
				continue
			}
		}
		if pumpErr != nil && !errors.Is(pumpErr, context.Canceled) && !errors.Is(pumpErr, io.EOF) {
			b.emit(protocol.NewADBWebRTCStateEvent(b.sessionID(), false, false, serial, screenSize.Width, screenSize.Height, "H264 推流中断: "+pumpErr.Error()))
			b.Stop("")
			return
		}
		if closeErr != nil && !errors.Is(closeErr, context.Canceled) && !strings.Contains(closeErr.Error(), "signal: killed") {
			logx.Warn("ws", "adb screenrecord exited: sessionID=%s serial=%s err=%v", b.sessionID(), serial, closeErr)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func (b *adbWebRTCBridge) clearStreamCancel(token int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.streamToken == token {
		b.streamCancel = nil
	}
}

func (b *adbWebRTCBridge) sessionID() string {
	if b.sessionIDFn == nil {
		return ""
	}
	return b.sessionIDFn()
}

func (b *adbWebRTCBridge) requestKeyframe() {
	b.mu.Lock()
	cancel := b.streamCancel
	now := time.Now()
	if cancel == nil || now.Sub(b.lastKeyframeRequest) < 800*time.Millisecond {
		b.mu.Unlock()
		return
	}
	b.lastKeyframeRequest = now
	b.mu.Unlock()

	logx.Info("ws", "forcing adb keyframe refresh: sessionID=%s", b.sessionID())
	cancel()
}

func drainRTCP(sender *webrtc.RTPSender, onKeyframe func()) {
	if sender == nil {
		return
	}
	buffer := make([]byte, 1500)
	for {
		n, _, err := sender.Read(buffer)
		if err != nil {
			return
		}
		packets, packetErr := rtcp.Unmarshal(buffer[:n])
		if packetErr != nil {
			continue
		}
		for _, packet := range packets {
			switch packet.(type) {
			case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
				if onKeyframe != nil {
					onKeyframe()
				}
			}
		}
	}
}
