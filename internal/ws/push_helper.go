package ws

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

// sendPushNotificationIfNeeded 在需要时发送推送通知
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

	// 覆盖 prompt/interaction 的 body
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

	// 检查是否有活跃连接。如果有，只发需要用户介入的事件。
	hasActiveConnection := h.runtimeSessions.HasActiveConnection(sessionID)
	if hasActiveConnection && isProgress {
		return
	}

	// 进度类事件做防抖，避免推送轰炸。
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

		if err := h.PushService.SendNotification(pushCtx, push.NotificationRequest{
			Token:    token,
			Platform: platform,
			Title:    title,
			Body:     body,
			Data: map[string]string{
				"type":         firstNonEmptyPushString(eventType, "action_needed"),
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
