package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"

	"mobilevc/internal/adb"
	"mobilevc/internal/logx"
	"mobilevc/internal/protocol"
)

type adbWebRTCBridge struct {
	mu          sync.Mutex
	peer        *webrtc.PeerConnection
	cancel      context.CancelFunc
	serial      string
	screenSize  adb.Size
	sessionIDFn func() string
	emit        func(any)
}

type adbControlMessage struct {
	Type string `json:"type"`
	X    int    `json:"x,omitempty"`
	Y    int    `json:"y,omitempty"`
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

func (b *adbWebRTCBridge) HandleOffer(ctx context.Context, serial, sdpType, sdp string) error {
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
	api := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine))
	peer, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return fmt.Errorf("create peer connection failed: %w", err)
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
	go drainRTCP(sender)

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
	case <-time.After(5 * time.Second):
	case <-ctx.Done():
		b.Stop("")
		return ctx.Err()
	}

	localDescription := peer.LocalDescription()
	if localDescription == nil {
		b.Stop("")
		return fmt.Errorf("missing local description after answer")
	}

	b.emit(protocol.NewADBWebRTCAnswerEvent(b.sessionID(), resolvedSerial, localDescription.Type.String(), localDescription.SDP))
	b.emit(protocol.NewADBWebRTCStateEvent(b.sessionID(), true, false, resolvedSerial, screenSize.Width, screenSize.Height, "WebRTC 会话已建立，等待连接后启动 H264 推流…"))

	return nil
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
	default:
		return fmt.Errorf("暂不支持的 ADB 控制消息类型: %s", message.Type)
	}
}

func (b *adbWebRTCBridge) streamVideo(ctx context.Context, serial string, screenSize adb.Size, track *webrtc.TrackLocalStaticSample) {
	config := adb.H264StreamConfig{
		BitRate:      2_000_000,
		MaxDimension: 1280,
		TimeLimit:    170 * time.Second,
		FrameRate:    15,
	}

	if err := adb.WarmupScreen(ctx, serial); err != nil && ctx.Err() == nil {
		logx.Warn("ws", "adb warmup failed: sessionID=%s serial=%s err=%v", b.sessionID(), serial, err)
	}

	for {
		if ctx.Err() != nil {
			return
		}
		stream, err := adb.StartH264Stream(ctx, serial, config)
		if err != nil {
			b.emit(protocol.NewADBWebRTCStateEvent(b.sessionID(), false, false, serial, screenSize.Width, screenSize.Height, err.Error()))
			b.Stop("")
			return
		}

		pumpErr := adb.PumpH264Stream(ctx, stream.Reader(), config, func(frame []byte, duration time.Duration) error {
			return track.WriteSample(media.Sample{Data: frame, Duration: duration})
		})
		closeErr := stream.Close()
		if ctx.Err() != nil {
			return
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

func (b *adbWebRTCBridge) sessionID() string {
	if b.sessionIDFn == nil {
		return ""
	}
	return b.sessionIDFn()
}

func drainRTCP(sender *webrtc.RTPSender) {
	if sender == nil {
		return
	}
	buffer := make([]byte, 1500)
	for {
		if _, _, err := sender.Read(buffer); err != nil {
			return
		}
	}
}
