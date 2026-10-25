package unit

import (
	"testing"

	"mobilevc/internal/engine"
)

func TestStripANSI_NoCodes(t *testing.T) {
	input := "hello world"
	got := engine.StripANSI(input)
	if got != input {
		t.Errorf("got %q, want %q", got, input)
	}
}

func TestStripANSI_ColorCodes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"red", "\x1b[31mhello\x1b[0m", "hello"},
		{"bold", "\x1b[1mbold\x1b[0m", "bold"},
		{"green_bg", "\x1b[42mtext\x1b[0m", "text"},
		{"move_cursor", "\x1b[10;20Hcontent", "content"},
		{"clear_line", "before\x1b[2Kafter", "beforeafter"},
		{"empty", "", ""},
		{"plain_text", "just text", "just text"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.StripANSI(tt.input)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStripANSIChunk_Complete(t *testing.T) {
	cleaned, carry := engine.StripANSIChunk("\x1b[31mhello\x1b[0m", "")
	if cleaned != "hello" {
		t.Errorf("cleaned: got %q", cleaned)
	}
	if carry != "" {
		t.Errorf("carry: got %q, want empty", carry)
	}
}

func TestStripANSIChunk_IncompleteCarryOnly(t *testing.T) {
	// Pure carry with no chunk: incomplete ANSI stays in carry
	cleaned, carry := engine.StripANSIChunk("", "\x1b[31")
	if cleaned != "" {
		t.Errorf("cleaned: got %q, want empty", cleaned)
	}
	if carry != "\x1b[31" {
		t.Errorf("carry: got %q, want %q", carry, "\x1b[31")
	}
}

func TestStripANSIChunk_CompleteWithCarry(t *testing.T) {
	cleaned, carry := engine.StripANSIChunk("mhello", "\x1b[31")
	if cleaned != "hello" {
		t.Errorf("cleaned: got %q, want %q", cleaned, "hello")
	}
	if carry != "" {
		t.Errorf("carry: got %q, want empty", carry)
	}
}

func TestStripANSIChunk_IncompleteSplit(t *testing.T) {
	// incomplete CSI at the end should go to carry
	cleaned, carry := engine.StripANSIChunk("hello\x1b[31", "")
	if cleaned != "hello" {
		t.Errorf("cleaned: got %q, want %q", cleaned, "hello")
	}
	if carry != "\x1b[31" {
		t.Errorf("carry: got %q, want %q", carry, "\x1b[31")
	}
}

func TestNewExecRunner(t *testing.T) {
	r := engine.NewExecRunner()
	if r == nil {
		t.Fatal("NewExecRunner returned nil")
	}
}

func TestNewPtyRunner(t *testing.T) {
	r := engine.NewPtyRunner()
	if r == nil {
		t.Fatal("NewPtyRunner returned nil")
	}
}

func TestModeConstants(t *testing.T) {
	if engine.ModeExec != "exec" {
		t.Errorf("ModeExec: got %q", engine.ModeExec)
	}
	if engine.ModePTY != "pty" {
		t.Errorf("ModePTY: got %q", engine.ModePTY)
	}
}

func TestExecRequest(t *testing.T) {
	req := engine.ExecRequest{
		Command:        "claude --print",
		CWD:            "/tmp",
		SessionID:      "s1",
		Mode:           engine.ModePTY,
		PermissionMode: "default",
		InitialInput:   "hello",
	}
	if req.Command != "claude --print" {
		t.Errorf("Command: %q", req.Command)
	}
	if req.Mode != engine.ModePTY {
		t.Errorf("Mode: %q", req.Mode)
	}
}

func TestErrorVariables(t *testing.T) {
	if engine.ErrInputNotSupported == nil {
		t.Error("ErrInputNotSupported should not be nil")
	}
	if engine.ErrNoPendingControlRequest == nil {
		t.Error("ErrNoPendingControlRequest should not be nil")
	}
}
