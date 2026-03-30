package runtime

import (
	"testing"

	"mobilevc/internal/protocol"
)

func TestDetectModelValueCodex(t *testing.T) {
	got := detectModelValue(protocol.RuntimeMeta{
		Command: "codex --help",
		Engine:  "codex",
	})
	if got != "codex" {
		t.Fatalf("expected codex, got %q", got)
	}
}
