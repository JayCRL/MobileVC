package gateway

import (
	"strings"
	"testing"
)

func TestWsDebugPreviewRedactsSecrets(t *testing.T) {
	preview := wsDebugPreview("AUTH_TOKEN=test-token --api-key sk-test\nnext")

	if strings.Contains(preview, "test-token") || strings.Contains(preview, "sk-test") {
		t.Fatalf("expected secrets to be redacted, got %q", preview)
	}
	if !strings.Contains(preview, "<redacted>") {
		t.Fatalf("expected redaction marker, got %q", preview)
	}
	if strings.Contains(preview, "\n") {
		t.Fatalf("expected newline to be escaped, got %q", preview)
	}
}
