package session

import (
	"path/filepath"
	"testing"
)

func TestTimelineAttachmentsFromTextNormalizesTrailingPunctuation(t *testing.T) {
	dir := t.TempDir()
	imagePath := filepath.Join(dir, "screen.png")
	text := "已生成图片：" + imagePath + ".\n" +
		"Markdown 同一路径 ![screen](" + imagePath + ",)"

	attachments := TimelineAttachmentsFromText(text, "assistant_path")
	if len(attachments) != 1 {
		t.Fatalf("expected one normalized attachment, got %+v", attachments)
	}
	if attachments[0].Path != imagePath {
		t.Fatalf("attachment path: got %q want %q", attachments[0].Path, imagePath)
	}
	if attachments[0].Name != "screen.png" {
		t.Fatalf("attachment name: got %q", attachments[0].Name)
	}
}
