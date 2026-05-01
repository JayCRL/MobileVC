package gateway

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mobilevc/internal/logx"
	"mobilevc/internal/protocol"
	"mobilevc/internal/push"
)

const progressPushDebounce = 30 * time.Second

// sendPushNotificationIfNeeded sends a push notification when appropriate.
func (h *Handler) sendPushNotificationIfNeeded(ctx context.Context, sessionID string, event any) {
	if h.PushService == nil || h.SessionStore == nil {
		return
	}

	eventType := ""
	title := "MobileVC"
	body := ""
	blockingKind := ""
	isProgress := false

	switch e := event.(type) {
	case protocol.PromptRequestEvent:
		eventType = protocol.EventTypePromptRequest
		blockingKind = e.RuntimeMeta.BlockingKind
		if e.Message != "" {
			body = e.Message
		} else {
			body = "Claude 需要你的授权确认"
		}
	case protocol.InteractionRequestEvent:
		eventType = protocol.EventTypeInteractionRequest
		blockingKind = firstNonEmptyPushString(e.Kind, e.RuntimeMeta.BlockingKind)
		if e.Message != "" {
			body = e.Message
		} else {
			body = "Claude 需要你审核代码变更"
		}
	case protocol.AgentStateEvent:
		state := strings.TrimSpace(strings.ToUpper(e.State))
		if state == "IDLE" || state == "WAIT_INPUT" || state == "DONE" || state == "DISCONNECTED" || state == "" {
			return
		}
		eventType = protocol.EventTypeAgentState
		title = "AI 助手运行中"
		if e.Message != "" {
			body = e.Message
		} else if e.Step != "" {
			body = e.Step
		} else {
			body = "正在处理中..."
		}
		isProgress = true
	case protocol.StepUpdateEvent:
		eventType = protocol.EventTypeStepUpdate
		title = "执行工具"
		if e.Message != "" {
			body = e.Message
		} else if e.Target != "" {
			body = fmt.Sprintf("正在执行: %s", e.Target)
		} else {
			body = "正在执行工具..."
		}
		isProgress = true
	case protocol.LogEvent:
		if e.Stream != "assistant_reply" && e.Stream != "markdown" {
			return
		}
		eventType = protocol.EventTypeLog
		title = "AI 回复"
		body = truncatePushBody(e.Message, 200)
		if body == "" {
			return
		}
		isProgress = true
	case protocol.ErrorEvent:
		eventType = protocol.EventTypeError
		title = "错误"
		body = e.Message
		if body == "" {
			body = "发生了一个错误"
		}
	default:
		return
	}

	switch blockingKind {
	case "permission":
		body = "AI 助手需要你确认权限"
	case "review":
		body = "AI 助手需要你处理代码审核"
	case "plan":
		body = "AI 助手需要你完成计划选择"
	case "reply":
		body = "AI 助手正在等待你的回复"
	case "ready":
		return
	}

	hasActiveConnection := h.runtimeSessions.HasActiveConnection(sessionID)
	if hasActiveConnection && isProgress {
		return
	}

	if isProgress {
		h.muProgressPush.Lock()
		last, ok := h.lastProgressPush[sessionID]
		if ok && time.Since(last) < progressPushDebounce {
			h.muProgressPush.Unlock()
			return
		}
		h.lastProgressPush[sessionID] = time.Now()
		h.muProgressPush.Unlock()
	}

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

		dataType := eventType
		if eventType == protocol.EventTypePromptRequest || eventType == protocol.EventTypeInteractionRequest {
			dataType = "action_needed"
		}
		if err := h.PushService.SendNotification(pushCtx, push.NotificationRequest{
			Token:    token,
			Platform: platform,
			Title:    title,
			Body:     body,
			Data: map[string]string{
				"type":         firstNonEmptyPushString(dataType, "action_needed"),
				"sessionId":    sessionID,
				"eventType":    eventType,
				"blockingKind": blockingKind,
			},
		}); err != nil {
			logx.Warn("push", "send push notification failed: sessionID=%s platform=%s err=%v", sessionID, platform, err)
		} else {
			logx.Info("push", "push notification sent: sessionID=%s platform=%s title=%q body=%q", sessionID, platform, title, body)
		}
	}()
}

func (h *Handler) handleRegisterPushToken(ctx context.Context, sessionID, token, platform string, emit func(any)) {
	if h.SessionStore == nil {
		emit(protocol.NewErrorEvent(sessionID, "session store unavailable", ""))
		return
	}

	if sessionID == "" {
		emit(protocol.NewErrorEvent("", "sessionId is required", ""))
		return
	}

	if token == "" {
		emit(protocol.NewErrorEvent(sessionID, "token is required", ""))
		return
	}

	if platform == "" {
		platform = "ios"
	}

	if err := h.SessionStore.SavePushToken(ctx, sessionID, token, platform); err != nil {
		logx.Error("ws", "save push token failed: sessionID=%s platform=%s err=%v", sessionID, platform, err)
		emit(protocol.NewErrorEvent(sessionID, "failed to save push token", ""))
		return
	}

	logx.Info("ws", "push token registered: sessionID=%s platform=%s", sessionID, platform)
}

func firstNonEmptyPushString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func truncatePushBody(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) <= maxLen {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxLen]) + "..."
}
