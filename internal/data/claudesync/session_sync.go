package claudesync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mobilevc/internal/data"
)

// ExtractJSONLEvents extracts user and assistant events from LogEntries
// starting from the given index. Returns the events to write and the new
// entry count.
func ExtractJSONLEvents(entries []data.SnapshotLogEntry, startIndex int) ([]JSONLEvent, int) {
	if startIndex >= len(entries) {
		return nil, len(entries)
	}
	events := make([]JSONLEvent, 0)
	for i := startIndex; i < len(entries); i++ {
		e := entries[i]
		text := strings.TrimSpace(e.Message)
		if text == "" {
			text = strings.TrimSpace(e.Text)
		}
		if text == "" {
			continue
		}
		switch e.Kind {
		case "user":
			events = append(events, JSONLEvent{
				Type:      "user",
				Text:      text,
				Timestamp: e.Timestamp,
			})
		case "markdown":
			events = append(events, JSONLEvent{
				Type:      "assistant",
				Text:      text,
				Timestamp: e.Timestamp,
			})
		}
	}
	return events, len(entries)
}

// MergeJSONLToSession reads the Claude CLI JSONL for the given session UUID
// and returns any LogEntries from the JSONL that are not already present in
// the store's LogEntries. Returns nil if there is nothing new.
func MergeJSONLToSession(cwd, claudeUUID string, existingEntries []data.SnapshotLogEntry) (newEntries []data.SnapshotLogEntry, newCount int, _ error) {
	if strings.TrimSpace(cwd) == "" || strings.TrimSpace(claudeUUID) == "" {
		return nil, 0, nil
	}
	projectsDir, err := ClaudeProjectsDir()
	if err != nil {
		return nil, 0, err
	}
	encoded := EncodeCWDToProjectDir(cwd)
	if encoded == "" {
		return nil, 0, nil
	}
	filePath := filepath.Join(projectsDir, encoded, claudeUUID+".jsonl")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, 0, nil
	}
	native, err := parseSessionFromFile(filePath, claudeUUID)
	if err != nil {
		return nil, 0, fmt.Errorf("parse claude jsonl for merge: %w", err)
	}
	if len(native.LogEntries) == 0 {
		return nil, 0, nil
	}

	// Index existing entries by (kind + normalized text) for fast lookup.
	seen := make(map[string]bool, len(existingEntries))
	for _, e := range existingEntries {
		key := entryDedupKey(e)
		if key != "" {
			seen[key] = true
		}
	}

	var merged []data.SnapshotLogEntry
	for _, e := range native.LogEntries {
		key := entryDedupKey(e)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		merged = append(merged, e)
	}

	if len(merged) == 0 {
		return nil, 0, nil
	}

	allEntries := make([]data.SnapshotLogEntry, 0, len(existingEntries)+len(merged))
	allEntries = append(allEntries, existingEntries...)
	allEntries = append(allEntries, merged...)
	return merged, len(allEntries), nil
}

func entryDedupKey(e data.SnapshotLogEntry) string {
	text := strings.TrimSpace(e.Message)
	if text == "" {
		text = strings.TrimSpace(e.Text)
	}
	if text == "" {
		return ""
	}
	text = strings.Join(strings.Fields(text), " ")
	return e.Kind + ":" + text
}
