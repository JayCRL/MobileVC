package gateway

import (
	"context"
	"encoding/base64"
	"os"
	"testing"

	"mobilevc/internal/data"
	"mobilevc/internal/protocol"
)

func TestPersistImageAttachmentsWritesOwnerOnlyFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	attachments, err := persistImageAttachments(context.Background(), "session/test", []protocol.ImageAttachment{
		{
			Name:     "screen.png",
			MIMEType: "image/png",
			Data:     base64.StdEncoding.EncodeToString([]byte("png-bytes")),
		},
	})
	if err != nil {
		t.Fatalf("persist image attachments: %v", err)
	}
	if len(attachments) != 1 {
		t.Fatalf("attachments count: got %d want 1", len(attachments))
	}
	attachment := attachments[0]
	if attachment.Kind != "image" || attachment.MIMEType != "image/png" || attachment.Source != "user_upload" {
		t.Fatalf("unexpected metadata: %+v", attachment)
	}
	if attachment.Path == "" || attachment.Size != int64(len("png-bytes")) {
		t.Fatalf("missing path/size metadata: %+v", attachment)
	}
	raw, err := os.ReadFile(attachment.Path)
	if err != nil {
		t.Fatalf("read persisted image: %v", err)
	}
	if string(raw) != "png-bytes" {
		t.Fatalf("persisted bytes: got %q", string(raw))
	}
	info, err := os.Stat(attachment.Path)
	if err != nil {
		t.Fatalf("stat persisted image: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode: got %o want 600", info.Mode().Perm())
	}
}

func TestPersistImageAttachmentsRejectsUnsupportedMIME(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	_, err := persistImageAttachments(context.Background(), "session-1", []protocol.ImageAttachment{
		{
			Name:     "note.txt",
			MIMEType: "text/plain",
			Data:     base64.StdEncoding.EncodeToString([]byte("nope")),
		},
	})
	if err == nil {
		t.Fatal("expected unsupported mime type to fail")
	}
}

func TestAppendAttachmentPathPrompt(t *testing.T) {
	got := appendAttachmentPathPrompt("请分析\n", []string{"/tmp/a.png", "/tmp/b.jpg"})
	want := "请分析\n\nAttached local image files:\n- /tmp/a.png\n- /tmp/b.jpg\n"
	if got != want {
		t.Fatalf("prompt:\ngot  %q\nwant %q", got, want)
	}
}

func TestAppendUserProjectionEntryAllowsAttachmentOnlyMessage(t *testing.T) {
	store, err := data.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	summary, err := store.CreateSession(context.Background(), "attachment only")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	attachment := protocol.TimelineAttachment{
		ID:            "att-1",
		Kind:          "image",
		Name:          "screen.png",
		MIMEType:      "image/png",
		Size:          9,
		Path:          "/tmp/screen.png",
		PreviewStatus: "available",
		Source:        "user_upload",
	}

	record, err := store.GetSession(context.Background(), summary.ID)
	if err != nil {
		t.Fatalf("get initial session: %v", err)
	}
	projection, ok := appendUserProjectionEntry(
		record.Projection,
		summary.ID,
		"",
		"回复",
		"conn-test",
		"remote-test",
		[]protocol.TimelineAttachment{attachment},
	)
	if !ok {
		t.Fatal("expected attachment-only projection entry to be appended")
	}
	if _, err := store.SaveProjection(context.Background(), summary.ID, projection); err != nil {
		t.Fatalf("save projection: %v", err)
	}

	record, err = store.GetSession(context.Background(), summary.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if len(record.Projection.LogEntries) != 1 {
		t.Fatalf("log entries: got %d want 1", len(record.Projection.LogEntries))
	}
	entry := record.Projection.LogEntries[0]
	if entry.Message != "" || len(entry.Attachments) != 1 {
		t.Fatalf("unexpected attachment-only entry: %+v", entry)
	}
	if entry.Attachments[0].ID != attachment.ID || entry.Attachments[0].Path != attachment.Path {
		t.Fatalf("attachment metadata not preserved: %+v", entry.Attachments[0])
	}
}
