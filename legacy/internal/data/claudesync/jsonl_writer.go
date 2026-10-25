package claudesync

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const syntheticVersion = "2.1.119"

// JSONLEvent describes a single event to write to a Claude CLI jsonl file.
type JSONLEvent struct {
	Type      string
	Text      string
	Timestamp string
}

// --- Exact Claude CLI JSONL event types (field order matches CLI output) ---

type claudeUserEvent struct {
	ParentUuid     any           `json:"parentUuid"`
	IsSidechain    bool          `json:"isSidechain"`
	PromptID       string        `json:"promptId"`
	Type           string        `json:"type"`
	Message        claudeUserMsg `json:"message"`
	UUID           string        `json:"uuid"`
	Timestamp      string        `json:"timestamp"`
	PermissionMode string        `json:"permissionMode"`
	UserType       string        `json:"userType"`
	Entrypoint     string        `json:"entrypoint"`
	CWD            string        `json:"cwd"`
	SessionID      string        `json:"sessionId"`
	Version        string        `json:"version"`
	GitBranch      string        `json:"gitBranch"`
}

type claudeUserMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type claudeAssistantEvent struct {
	ParentUuid  any                `json:"parentUuid"`
	IsSidechain bool               `json:"isSidechain"`
	Type        string             `json:"type"`
	Message     claudeAssistantMsg `json:"message"`
	UUID        string             `json:"uuid"`
	Timestamp   string             `json:"timestamp"`
	UserType    string             `json:"userType"`
	Entrypoint  string             `json:"entrypoint"`
	CWD         string             `json:"cwd"`
	SessionID   string             `json:"sessionId"`
	Version     string             `json:"version"`
	GitBranch   string             `json:"gitBranch"`
}

type claudeAssistantMsg struct {
	ID           string             `json:"id"`
	Type         string             `json:"type"`
	Role         string             `json:"role"`
	Model        string             `json:"model"`
	Content      []claudeContentBlk `json:"content"`
	StopReason   string             `json:"stop_reason"`
	StopSequence any                `json:"stop_sequence"`
	Usage        claudeUsage        `json:"usage"`
}

type claudeContentBlk struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type claudeUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// WriteSessionToJSONL appends events to the Claude CLI jsonl file.
func WriteSessionToJSONL(cwd, claudeUUID string, events []JSONLEvent) error {
	if strings.TrimSpace(cwd) == "" || strings.TrimSpace(claudeUUID) == "" || len(events) == 0 {
		return nil
	}
	projectsDir, err := ClaudeProjectsDir()
	if err != nil {
		return err
	}
	encoded := EncodeCWDToProjectDir(cwd)
	if encoded == "" {
		return fmt.Errorf("encode cwd failed: %s", cwd)
	}
	dir := filepath.Join(projectsDir, encoded)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create claude project dir: %w", err)
	}
	filePath := filepath.Join(dir, claudeUUID+".jsonl")

	lastUUID := readLastUUID(filePath)

	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open claude jsonl: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	for _, ev := range events {
		ts := strings.TrimSpace(ev.Timestamp)
		if ts == "" {
			ts = time.Now().UTC().Format(time.RFC3339Nano)
		}
		evUUID := uuid.NewString()
		parentUuidVal := any(lastUUID)
		if lastUUID == "" {
			parentUuidVal = nil
		}
		switch strings.TrimSpace(ev.Type) {
		case "user":
			line := claudeUserEvent{
				ParentUuid:     parentUuidVal,
				IsSidechain:    false,
				PromptID:       uuid.NewString(),
				Type:           "user",
				Message:        claudeUserMsg{Role: "user", Content: ev.Text},
				UUID:           evUUID,
				Timestamp:      ts,
				PermissionMode: "default",
				UserType:       "external",
				Entrypoint:     "cli",
				CWD:            cwd,
				SessionID:      claudeUUID,
				Version:        syntheticVersion,
				GitBranch:      "main",
			}
			if err := enc.Encode(line); err != nil {
				return fmt.Errorf("write user event: %w", err)
			}
			lastUUID = evUUID
		case "assistant":
			line := claudeAssistantEvent{
				ParentUuid:  parentUuidVal,
				IsSidechain: false,
				Type:        "assistant",
				Message: claudeAssistantMsg{
					ID:           "msg_" + randSeq(20),
					Type:         "message",
					Role:         "assistant",
					Model:        "claude-sonnet-4-6",
					Content:      []claudeContentBlk{{Type: "text", Text: ev.Text}},
					StopReason:   "end_turn",
					StopSequence: nil,
					Usage:        claudeUsage{},
				},
				UUID:       evUUID,
				Timestamp:  ts,
				UserType:   "external",
				Entrypoint: "cli",
				CWD:        cwd,
				SessionID:  claudeUUID,
				Version:    syntheticVersion,
				GitBranch:  "main",
			}
			if err := enc.Encode(line); err != nil {
				return fmt.Errorf("write assistant event: %w", err)
			}
			lastUUID = evUUID
		case "ai-title":
			line := map[string]any{
				"type":       "ai-title",
				"title":      ev.Text,
				"uuid":       evUUID,
				"timestamp":  ts,
				"cwd":        cwd,
				"sessionId":  claudeUUID,
				"entrypoint": "cli",
				"version":    syntheticVersion,
			}
			if err := enc.Encode(line); err != nil {
				return fmt.Errorf("write ai-title event: %w", err)
			}
		case "last-prompt":
			line := map[string]any{
				"type":       "last-prompt",
				"lastPrompt": ev.Text,
				"sessionId":  claudeUUID,
				"uuid":       evUUID,
				"parentUuid": parentUuidVal,
			}
			lastUUID = evUUID
			if err := enc.Encode(line); err != nil {
				return fmt.Errorf("write last-prompt event: %w", err)
			}
		}
	}
	return nil
}

func readLastUUID(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return ""
	}
	bufSize := int64(4096)
	if info.Size() < bufSize {
		bufSize = info.Size()
	}
	buf := make([]byte, bufSize)
	f.ReadAt(buf, info.Size()-bufSize)
	scanner := bufio.NewScanner(strings.NewReader(string(buf)))
	var lastLine string
	for scanner.Scan() {
		lastLine = scanner.Text()
	}
	if lastLine == "" {
		return ""
	}
	var payload struct {
		UUID string `json:"uuid"`
	}
	if json.Unmarshal([]byte(lastLine), &payload) != nil {
		return ""
	}
	return strings.TrimSpace(payload.UUID)
}

func randSeq(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}
