package ws

import (
	"context"

	"mobilevc/internal/logx"
	"mobilevc/internal/protocol"
)

func (h *Handler) handleRegisterPushToken(ctx context.Context, sessionID, token, platform string, emit func(any)) {
	if sessionID == "" {
		emit(protocol.NewErrorEvent("", "sessionId is required", ""))
		return
	}

	if token == "" {
		emit(protocol.NewErrorEvent(sessionID, "token is required", ""))
		return
	}

	if platform == "" {
		platform = "ios" // 默认 iOS
	}

	if err := h.SessionStore.SavePushToken(ctx, sessionID, token, platform); err != nil {
		logx.Error("ws", "save push token failed: sessionID=%s platform=%s err=%v", sessionID, platform, err)
		emit(protocol.NewErrorEvent(sessionID, "failed to save push token", ""))
		return
	}

	logx.Info("ws", "push token registered: sessionID=%s platform=%s", sessionID, platform)
	// 发送成功响应（使用简单的 log event）
	emit(protocol.NewLogEvent(sessionID, "push token registered", "info"))
}
