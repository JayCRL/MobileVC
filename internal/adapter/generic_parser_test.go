package adapter

import (
	"testing"

	"mobilevc/internal/protocol"
)

func TestGenericParserPlainLog(t *testing.T) {
	parser := NewGenericParser()
	events := parser.ParseLine("build started", "s1", "stderr")

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	logEvent, ok := events[0].(protocol.LogEvent)
	if !ok {
		t.Fatalf("expected LogEvent, got %T", events[0])
	}

	if logEvent.Type != protocol.EventTypeLog || logEvent.Message != "build started" {
		t.Fatalf("unexpected log event: %#v", logEvent)
	}
	if logEvent.Stream != "stderr" {
		t.Fatalf("expected stderr stream, got %q", logEvent.Stream)
	}
}

func TestGenericParserPythonTraceback(t *testing.T) {
	parser := NewGenericParser()
	lines := []string{
		"Traceback (most recent call last):",
		"  File \"main.py\", line 1, in <module>",
		"    run()",
		"ValueError: boom",
	}

	var events []any
	for _, line := range lines {
		events = parser.ParseLine(line, "s1", "stderr")
	}
	events = append(events, parser.Flush("s1", "stderr")...)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	errorEvent, ok := events[0].(protocol.ErrorEvent)
	if !ok {
		t.Fatalf("expected ErrorEvent, got %T", events[0])
	}

	if errorEvent.Type != protocol.EventTypeError {
		t.Fatalf("unexpected event type: %s", errorEvent.Type)
	}
	if errorEvent.Message != "ValueError: boom" {
		t.Fatalf("unexpected error message: %s", errorEvent.Message)
	}
}

func TestGenericParserJavaExceptionStack(t *testing.T) {
	parser := NewGenericParser()
	lines := []string{
		"Exception in thread \"main\" java.lang.RuntimeException: boom",
		"\tat example.Main.main(Main.java:10)",
		"Caused by: java.io.IOException: disk error",
		"\tat example.IO.read(IO.java:20)",
	}

	var events []any
	for _, line := range lines {
		events = parser.ParseLine(line, "s1", "stderr")
	}
	events = append(events, parser.Flush("s1", "stderr")...)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	errorEvent, ok := events[0].(protocol.ErrorEvent)
	if !ok {
		t.Fatalf("expected ErrorEvent, got %T", events[0])
	}

	if errorEvent.Type != protocol.EventTypeError {
		t.Fatalf("unexpected event type: %s", errorEvent.Type)
	}
	if errorEvent.Message != "at example.IO.read(IO.java:20)" && errorEvent.Message != "Caused by: java.io.IOException: disk error" {
		t.Fatalf("unexpected error message: %s", errorEvent.Message)
	}
}

func TestGenericParserFileDiffMetadata(t *testing.T) {
	parser := NewGenericParser()
	lines := []string{
		"diff --git a/internal/ws/handler.go b/internal/ws/handler.go",
		"--- a/internal/ws/handler.go",
		"+++ b/internal/ws/handler.go",
		"@@ -1,1 +1,2 @@",
		"-old",
		"+new",
	}

	var events []any
	for _, line := range lines {
		events = parser.ParseLine(line, "s1", "stdout")
	}
	events = append(events, parser.Flush("s1", "stdout")...)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	diffEvent, ok := events[0].(protocol.FileDiffEvent)
	if !ok {
		t.Fatalf("expected FileDiffEvent, got %T", events[0])
	}
	if diffEvent.Path != "internal/ws/handler.go" {
		t.Fatalf("unexpected diff path: %q", diffEvent.Path)
	}
	if diffEvent.Title != "Updating internal/ws/handler.go" {
		t.Fatalf("unexpected diff title: %q", diffEvent.Title)
	}
}

func TestGenericParserFileDiffMetadataWithLeadingCarriageReturn(t *testing.T) {
	parser := NewGenericParser()
	lines := []string{
		"\rdiff --git a/internal/ws/handler.go b/internal/ws/handler.go",
		"\r--- a/internal/ws/handler.go",
		"\r+++ b/internal/ws/handler.go",
		"\r@@ -1,1 +1,2 @@",
		"\r-old",
		"\r+new",
	}

	var events []any
	for _, line := range lines {
		events = parser.ParseLine(line, "s1", "stdout")
	}
	events = append(events, parser.Flush("s1", "stdout")...)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	diffEvent, ok := events[0].(protocol.FileDiffEvent)
	if !ok {
		t.Fatalf("expected FileDiffEvent, got %T", events[0])
	}
	if diffEvent.Path != "internal/ws/handler.go" {
		t.Fatalf("unexpected diff path: %q", diffEvent.Path)
	}
	if diffEvent.Title != "Updating internal/ws/handler.go" {
		t.Fatalf("unexpected diff title: %q", diffEvent.Title)
	}
}
