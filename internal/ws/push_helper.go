package ws

import (
	"context"
	"time"

	"mobilevc/internal/logx"
	"mobilevc/internal/protocol"
)

// shouldSendPushNotification 判断是否需要发送推送通知
func (h *Handler) shouldSendPushNotification(sessionID string, eventType string) bool {
	// 只在需要用户介入时发送推送
	switch eventType {
	case protocol.EventTypePromptRequest,
		protocol.EventTypeInteractionRequest:
		return true
	default:
		return false
	}
}

// sendPushNotificationIfNeeded 在需要时发送推送通知
func (h *Handler) sendPushNotificationIfNeeded(ctx context.Context, sessionID string, event any) {
	if h.PushService == nil || h.SessionStore == nil {
		return
	}

	eventType := ""
	title := "MobileVC"
	body := "需要你的确认"

	switch e := event.(type) {
	case protocol.PromptRequestEvent:
		eventType = protocol.EventTypePromptRequest
		if e.Message != "" {
			body = e.Message
		} else {
			body = "Claude 需要你的授权确认"
		}
	case protocol.InteractionRequestEvent:
		eventType = protocol.EventTypeInteractionRequest
		if e.Message != "" {
			body = e.Message
		} else {
			body = "Claude 需要你审核代码变更"
		}
	default:
		return
	}

	if !h.shouldSendPushNotification(sessionID, eventType) {
		return
	}

	// 异步发送推送，不阻塞主流程
	go func() {
		defer logx.Recover("push", "send push notification panic")

		token, platform, err := h.SessionStore.GetPushToken(ctx, sessionID)
		if err != nil {
			logx.Warn("push", "get push token failed: sessionID=%s err=%v", sessionID, err)
			return
		}

		if token == "" {
			logx.Info("push", "no push token registered: sessionID=%s", sessionID)
			return
		}

		pushCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := h.PushService.SendNotification(pushCtx, token, platform, title, body); err != nil {
			logx.Warn("push", "send push notification failed: sessionID=%s platform=%s err=%v", sessionID, platform, err)
		} else {
			logx.Info("push", "push notification sent: sessionID=%s platform=%s title=%q body=%q", sessionID, platform, title, body)
		}
	}()
}
