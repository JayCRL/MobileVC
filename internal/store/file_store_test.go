package store

import (
	"context"
	"os"
	"testing"
)

func TestFileStoreDeleteSessionRemovesRecordAndIndex(t *testing.T) {
	baseDir := t.TempDir()
	fs, err := NewFileStore(baseDir)
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}

	created, err := fs.CreateSession(context.Background(), "delete-me")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	if err := fs.DeleteSession(context.Background(), created.ID); err != nil {
		t.Fatalf("delete session: %v", err)
	}

	if _, err := fs.GetSession(context.Background(), created.ID); err == nil {
		t.Fatal("expected deleted session lookup to fail")
	}
	if _, err := os.Stat(fs.sessionPath(created.ID)); !os.IsNotExist(err) {
		t.Fatalf("expected session file removed, got err=%v", err)
	}

	items, err := fs.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	for _, item := range items {
		if item.ID == created.ID {
			t.Fatalf("expected deleted session absent from index, got %#v", items)
		}
	}
}

func TestFileStoreDeleteSessionRejectsMissingSession(t *testing.T) {
	baseDir := t.TempDir()
	fs, err := NewFileStore(baseDir)
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}

	created, err := fs.CreateSession(context.Background(), "delete-me")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := fs.DeleteSession(context.Background(), created.ID); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	if err := fs.DeleteSession(context.Background(), created.ID); err == nil {
		t.Fatal("expected repeated delete to fail")
	}
}
