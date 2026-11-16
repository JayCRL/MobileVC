package relay

import (
	"strings"

	"mobilevc/internal/logx"
)

type AuditEvent struct {
	Action    string
	Result    string
	SessionID string
	ClientID  string
	DeviceID  string
	StreamID  uint64
	Path      string
	ErrorCode string
}

func LogAuditEvent(event AuditEvent) {
	action := auditValue(event.Action, "unknown")
	result := auditValue(event.Result, "unknown")
	logx.Info(
		"relay.audit",
		"action=%s result=%s sessionID=%s clientID=%s deviceID=%s streamID=%d path=%q errorCode=%s",
		action,
		result,
		strings.TrimSpace(event.SessionID),
		strings.TrimSpace(event.ClientID),
		strings.TrimSpace(event.DeviceID),
		event.StreamID,
		strings.TrimSpace(event.Path),
		strings.TrimSpace(event.ErrorCode),
	)
}

func auditValue(value string, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}
