package gateway

import (
	"context"
	"strings"
	"testing"
	"time"

	"mobilevc/internal/data"
	"mobilevc/internal/protocol"
	"mobilevc/internal/push"
)

// chanPushService 把 SendNotification 投递到 channel，方便测试同步等待。
type chanPushService struct {
	calls chan push.NotificationRequest
}

func newChanPushService() *chanPushService {
	return &chanPushService{calls: make(chan push.NotificationRequest, 8)}
}

func (c *chanPushService) SendNotification(_ context.Context, req push.NotificationRequest) error {
	c.calls <- req
	return nil
}

func (c *chanPushService) wait(t *testing.T, label string) push.NotificationRequest {
	t.Helper()
	select {
	case req := <-c.calls:
		return req
	case <-time.After(2 * time.Second):
		t.Fatalf("did not receive push notification: %s", label)
		return push.NotificationRequest{}
	}
}

func (c *chanPushService) expectNoCall(t *testing.T, label string) {
	t.Helper()
	select {
	case req := <-c.calls:
		t.Fatalf("unexpected push call (%s): %+v", label, req)
	case <-time.After(120 * time.Millisecond):
	}
}

func newPushTestHandler(t *testing.T) (*Handler, *chanPushService, string) {
	t.Helper()
	store, err := data.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "push-session"
	if _, err := store.UpsertSession(context.Background(), data.SessionRecord{
		Summary: data.SessionSummary{ID: sessionID, Title: "push-test"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePushToken(context.Background(), sessionID, "tok-xyz", "ios"); err != nil {
		t.Fatal(err)
	}
	svc := newChanPushService()
	h := newTestHandler()
	h.SessionStore = store
	h.PushService = svc
	return h, svc, sessionID
}

func TestPush_PromptRequestTriggersActionNeeded(t *testing.T) {
	h, svc, sessionID := newPushTestHandler(t)

	event := protocol.PromptRequestEvent{
		Event:   protocol.Event{SessionID: sessionID},
		Message: "Need permission for x",
	}
	h.sendPushNotificationIfNeeded(context.Background(), sessionID, event)

	got := svc.wait(t, "prompt request")
	if got.Token != "tok-xyz" {
		t.Errorf("token: %q", got.Token)
	}
	if got.Platform != "ios" {
		t.Errorf("platform: %q", got.Platform)
	}
	if got.Title != "MobileVC" {
		t.Errorf("title: %q", got.Title)
	}
	if got.Body != "Need permission for x" {
		t.Errorf("body: %q", got.Body)
	}
	if got.Data["sessionId"] != sessionID {
		t.Errorf("data sessionId: %+v", got.Data)
	}
	if got.Data["type"] != "action_needed" {
		t.Errorf("data type: %+v", got.Data)
	}
	if got.Data["eventType"] != protocol.EventTypePromptRequest {
		t.Errorf("data eventType: %+v", got.Data)
	}
}

func TestPush_PromptRequestEmptyMessageUsesDefault(t *testing.T) {
	h, svc, sessionID := newPushTestHandler(t)
	h.sendPushNotificationIfNeeded(context.Background(), sessionID,
		protocol.PromptRequestEvent{Event: protocol.Event{SessionID: sessionID}, Message: ""})
	got := svc.wait(t, "default body")
	if !strings.Contains(got.Body, "Claude 需要你的授权") {
		t.Errorf("expected default body, got %q", got.Body)
	}
}

func TestPush_BlockingKindOverridesBody(t *testing.T) {
	cases := []struct {
		blockingKind string
		wantBody     string
	}{
		{"permission", "需要你确认权限"},
		{"review", "需要你处理代码审核"},
		{"plan", "需要你完成计划选择"},
		{"reply", "等待你的回复"},
	}
	for _, tc := range cases {
		t.Run(tc.blockingKind, func(t *testing.T) {
			h, svc, sessionID := newPushTestHandler(t)
			event := protocol.PromptRequestEvent{
				Event: protocol.Event{
					SessionID:   sessionID,
					RuntimeMeta: protocol.RuntimeMeta{BlockingKind: tc.blockingKind},
				},
				Message: "ignored",
			}
			h.sendPushNotificationIfNeeded(context.Background(), sessionID, event)
			got := svc.wait(t, tc.blockingKind)
			if !strings.Contains(got.Body, tc.wantBody) {
				t.Errorf("expected %q in body, got %q", tc.wantBody, got.Body)
			}
			if got.Data["blockingKind"] != tc.blockingKind {
				t.Errorf("expected blockingKind in data, got %+v", got.Data)
			}
		})
	}
}

func TestPush_BlockingKindReadySkips(t *testing.T) {
	h, svc, sessionID := newPushTestHandler(t)
	event := protocol.PromptRequestEvent{
		Event: protocol.Event{
			SessionID:   sessionID,
			RuntimeMeta: protocol.RuntimeMeta{BlockingKind: "ready"},
		},
		Message: "x",
	}
	h.sendPushNotificationIfNeeded(context.Background(), sessionID, event)
	svc.expectNoCall(t, "ready blocking kind should be skipped")
}

func TestPush_InteractionRequestTriggersActionNeeded(t *testing.T) {
	h, svc, sessionID := newPushTestHandler(t)
	event := protocol.InteractionRequestEvent{
		Event:   protocol.Event{SessionID: sessionID},
		Kind:    "review",
		Message: "需要审核",
	}
	h.sendPushNotificationIfNeeded(context.Background(), sessionID, event)
	got := svc.wait(t, "interaction request")
	if got.Data["type"] != "action_needed" {
		t.Errorf("data type: %+v", got.Data)
	}
	// Kind=review 会被映射到 BlockingKind=review 路径，body 被覆盖
	if !strings.Contains(got.Body, "代码审核") {
		t.Errorf("expected review body, got %q", got.Body)
	}
}

func TestPush_AgentStateRunningTriggersProgress(t *testing.T) {
	h, svc, sessionID := newPushTestHandler(t)
	event := protocol.AgentStateEvent{
		Event:   protocol.Event{SessionID: sessionID},
		State:   "RUNNING",
		Message: "正在执行",
	}
	h.sendPushNotificationIfNeeded(context.Background(), sessionID, event)
	got := svc.wait(t, "agent state running")
	if got.Title != "AI 助手运行中" {
		t.Errorf("title: %q", got.Title)
	}
	if got.Body != "正在执行" {
		t.Errorf("body: %q", got.Body)
	}
}

func TestPush_AgentStateIdleSkips(t *testing.T) {
	h, svc, sessionID := newPushTestHandler(t)
	for _, state := range []string{"IDLE", "WAIT_INPUT", "DONE", "DISCONNECTED", ""} {
		t.Run(state, func(t *testing.T) {
			h.sendPushNotificationIfNeeded(context.Background(), sessionID,
				protocol.AgentStateEvent{Event: protocol.Event{SessionID: sessionID}, State: state, Message: "x"})
			svc.expectNoCall(t, "state="+state)
		})
	}
}

func TestPush_StepUpdateTriggersExecution(t *testing.T) {
	h, svc, sessionID := newPushTestHandler(t)
	event := protocol.StepUpdateEvent{
		Event:  protocol.Event{SessionID: sessionID},
		Target: "main.go",
	}
	h.sendPushNotificationIfNeeded(context.Background(), sessionID, event)
	got := svc.wait(t, "step update")
	if got.Title != "执行工具" {
		t.Errorf("title: %q", got.Title)
	}
	if !strings.Contains(got.Body, "main.go") {
		t.Errorf("body: %q", got.Body)
	}
}

func TestPush_LogEventOnlyForAssistantOrMarkdown(t *testing.T) {
	h, svc, sessionID := newPushTestHandler(t)
	// stderr 应当不发
	h.sendPushNotificationIfNeeded(context.Background(), sessionID,
		protocol.LogEvent{Event: protocol.Event{SessionID: sessionID}, Stream: "stderr", Message: "boom"})
	svc.expectNoCall(t, "stderr log")

	// 但 markdown 流应当发
	h.sendPushNotificationIfNeeded(context.Background(), sessionID,
		protocol.LogEvent{Event: protocol.Event{SessionID: sessionID}, Stream: "markdown", Message: "## hi"})
	got := svc.wait(t, "markdown log")
	if got.Title != "AI 回复" {
		t.Errorf("title: %q", got.Title)
	}
}

func TestPush_LogEventEmptyBodySkips(t *testing.T) {
	h, svc, sessionID := newPushTestHandler(t)
	h.sendPushNotificationIfNeeded(context.Background(), sessionID,
		protocol.LogEvent{Event: protocol.Event{SessionID: sessionID}, Stream: "assistant_reply", Message: "  "})
	svc.expectNoCall(t, "empty assistant reply")
}

func TestPush_ErrorEventSendsErrorTitle(t *testing.T) {
	h, svc, sessionID := newPushTestHandler(t)
	h.sendPushNotificationIfNeeded(context.Background(), sessionID,
		protocol.ErrorEvent{Event: protocol.Event{SessionID: sessionID}, Message: "boom"})
	got := svc.wait(t, "error event")
	if got.Title != "错误" || got.Body != "boom" {
		t.Errorf("title/body: %q/%q", got.Title, got.Body)
	}
}

func TestPush_ErrorEventDefaultBody(t *testing.T) {
	h, svc, sessionID := newPushTestHandler(t)
	h.sendPushNotificationIfNeeded(context.Background(), sessionID,
		protocol.ErrorEvent{Event: protocol.Event{SessionID: sessionID}, Message: ""})
	got := svc.wait(t, "error default body")
	if got.Body != "发生了一个错误" {
		t.Errorf("body: %q", got.Body)
	}
}

func TestPush_UnsupportedEventTypeSkips(t *testing.T) {
	h, svc, sessionID := newPushTestHandler(t)
	type custom struct{}
	h.sendPushNotificationIfNeeded(context.Background(), sessionID, custom{})
	svc.expectNoCall(t, "unsupported event type")
}

func TestPush_NilServicesShortCircuit(t *testing.T) {
	h, svc, sessionID := newPushTestHandler(t)
	prevPush := h.PushService
	h.PushService = nil
	h.sendPushNotificationIfNeeded(context.Background(), sessionID,
		protocol.PromptRequestEvent{Event: protocol.Event{SessionID: sessionID}, Message: "x"})
	svc.expectNoCall(t, "nil push service")
	h.PushService = prevPush

	prevStore := h.SessionStore
	h.SessionStore = nil
	h.sendPushNotificationIfNeeded(context.Background(), sessionID,
		protocol.PromptRequestEvent{Event: protocol.Event{SessionID: sessionID}, Message: "x"})
	svc.expectNoCall(t, "nil session store")
	h.SessionStore = prevStore
}

func TestPush_NoTokenSkipsSend(t *testing.T) {
	h, _, sessionID := newPushTestHandler(t)
	// 换一个没有 token 的 session
	if _, err := h.SessionStore.UpsertSession(context.Background(), data.SessionRecord{
		Summary: data.SessionSummary{ID: "no-token", Title: "x"},
	}); err != nil {
		t.Fatal(err)
	}
	svc := newChanPushService()
	h.PushService = svc
	_ = sessionID
	h.sendPushNotificationIfNeeded(context.Background(), "no-token",
		protocol.PromptRequestEvent{Event: protocol.Event{SessionID: "no-token"}, Message: "x"})
	svc.expectNoCall(t, "no token registered")
}

func TestPush_ProgressDebouncedWithinWindow(t *testing.T) {
	h, svc, sessionID := newPushTestHandler(t)
	event := protocol.AgentStateEvent{
		Event:   protocol.Event{SessionID: sessionID},
		State:   "RUNNING",
		Message: "first",
	}
	h.sendPushNotificationIfNeeded(context.Background(), sessionID, event)
	first := svc.wait(t, "first progress")
	if first.Body != "first" {
		t.Errorf("first body: %q", first.Body)
	}

	// 立即再发同类 progress 应当被 debounce 掉
	event.Message = "second"
	h.sendPushNotificationIfNeeded(context.Background(), sessionID, event)
	svc.expectNoCall(t, "debounced progress")
}

func TestPush_TruncatePushBody(t *testing.T) {
	cases := []struct {
		in     string
		maxLen int
		want   string
	}{
		{"", 10, ""},
		{"  hi  ", 10, "hi"},
		{"abcdef", 5, "abcde..."},
		{"中文很长测试一下", 4, "中文很长..."},
	}
	for _, tc := range cases {
		if got := truncatePushBody(tc.in, tc.maxLen); got != tc.want {
			t.Errorf("truncatePushBody(%q,%d) = %q, want %q", tc.in, tc.maxLen, got, tc.want)
		}
	}
}

func TestPush_FirstNonEmptyPushString(t *testing.T) {
	if got := firstNonEmptyPushString("", "  ", "a", "b"); got != "  " {
		// 注意：当前实现仅判断 != ""，不 trim
		t.Errorf("got %q", got)
	}
	if got := firstNonEmptyPushString("", "", ""); got != "" {
		t.Errorf("got %q", got)
	}
	if got := firstNonEmptyPushString("first", "second"); got != "first" {
		t.Errorf("got %q", got)
	}
}

func TestPush_HandleRegisterPushToken(t *testing.T) {
	h, _, _ := newPushTestHandler(t)
	emitted := []any{}
	emit := func(e any) { emitted = append(emitted, e) }

	t.Run("nil store emits error", func(t *testing.T) {
		prev := h.SessionStore
		h.SessionStore = nil
		emitted = emitted[:0]
		h.handleRegisterPushToken(context.Background(), "s", "tok", "ios", emit)
		if len(emitted) == 0 {
			t.Fatal("expected error emit")
		}
		if _, ok := emitted[0].(protocol.ErrorEvent); !ok {
			t.Errorf("expected ErrorEvent, got %T", emitted[0])
		}
		h.SessionStore = prev
	})

	t.Run("empty session id emits error", func(t *testing.T) {
		emitted = emitted[:0]
		h.handleRegisterPushToken(context.Background(), "", "tok", "ios", emit)
		if len(emitted) == 0 || emitted[0].(protocol.ErrorEvent).Message != "sessionId is required" {
			t.Errorf("expected sessionId required, got %+v", emitted)
		}
	})

	t.Run("empty token emits error", func(t *testing.T) {
		emitted = emitted[:0]
		h.handleRegisterPushToken(context.Background(), "s", "", "ios", emit)
		if len(emitted) == 0 || emitted[0].(protocol.ErrorEvent).Message != "token is required" {
			t.Errorf("expected token required, got %+v", emitted)
		}
	})

	t.Run("default platform ios", func(t *testing.T) {
		emitted = emitted[:0]
		h.handleRegisterPushToken(context.Background(), "newsess", "tok-2", "", emit)
		// 不应当 emit 任何 error
		for _, e := range emitted {
			if _, ok := e.(protocol.ErrorEvent); ok {
				t.Errorf("unexpected error: %+v", e)
			}
		}
		// 真的存到 store 了
		token, platform, err := h.SessionStore.GetPushToken(context.Background(), "newsess")
		if err != nil {
			t.Fatal(err)
		}
		if token != "tok-2" || platform != "ios" {
			t.Errorf("token/platform: %q/%q", token, platform)
		}
	})
}
