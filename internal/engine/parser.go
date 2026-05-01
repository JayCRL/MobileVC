package engine

import "mobilevc/internal/protocol"

type StackAdapter interface {
	Detect(dir string) bool
	ParseLine(line string, sessionID string, stream string) []any
	Flush(sessionID string, stream string) []any
}

type EventFactory interface {
	Log(sessionID, message, stream string) protocol.LogEvent
	Error(sessionID, message, stack string) protocol.ErrorEvent
}
