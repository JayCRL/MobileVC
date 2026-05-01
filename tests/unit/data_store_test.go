package unit

import (
	"context"
	"os"
	"testing"

	"mobilevc/internal/data"
)

func newTestStore(t *testing.T) *data.FileStore {
	t.Helper()
	dir := t.TempDir()
	store, err := data.NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	return store
}

func TestFileStore_CreateSession(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	summary, err := store.CreateSession(ctx, "test session")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if summary.ID == "" {
		t.Fatal("session ID should not be empty")
	}
	if summary.Title != "test session" {
		t.Errorf("Title: got %q, want %q", summary.Title, "test session")
	}
	if summary.Source != "mobilevc" {
		t.Errorf("Source: got %q", summary.Source)
	}
}

func TestFileStore_CreateSession_EmptyTitle(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	summary, err := store.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if summary.Title == "" {
		t.Fatal("empty title should get a fallback")
	}
}

func TestFileStore_GetSession(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	created, _ := store.CreateSession(ctx, "get-test")
	record, err := store.GetSession(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.Summary.ID != created.ID {
		t.Errorf("ID mismatch: %q vs %q", record.Summary.ID, created.ID)
	}
}

func TestFileStore_GetSession_NotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, err := store.GetSession(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestFileStore_ListSessions(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	store.CreateSession(ctx, "first")
	store.CreateSession(ctx, "second")

	items, err := store.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(items))
	}
}

func TestFileStore_UpsertSession(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	created, _ := store.CreateSession(ctx, "original")
	record, _ := store.GetSession(ctx, created.ID)
	record.Summary.Title = "updated"

	upserted, err := store.UpsertSession(ctx, record)
	if err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	if upserted.Title != "updated" {
		t.Errorf("Title not updated: %q", upserted.Title)
	}
}

func TestFileStore_UpsertSession_New(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	record := data.SessionRecord{
		Summary: data.SessionSummary{ID: "custom-id", Title: "custom session"},
	}
	upserted, err := store.UpsertSession(ctx, record)
	if err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	if upserted.ID != "custom-id" {
		t.Errorf("ID: got %q", upserted.ID)
	}
}

func TestFileStore_DeleteSession(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	created, _ := store.CreateSession(ctx, "to-delete")
	if err := store.DeleteSession(ctx, created.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	_, err := store.GetSession(ctx, created.ID)
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestFileStore_DeleteSession_NotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	err := store.DeleteSession(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestFileStore_PushToken_RoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.SavePushToken(ctx, "s1", "token-abc", "ios"); err != nil {
		t.Fatalf("SavePushToken: %v", err)
	}
	tok, platform, err := store.GetPushToken(ctx, "s1")
	if err != nil {
		t.Fatalf("GetPushToken: %v", err)
	}
	if tok != "token-abc" {
		t.Errorf("token: got %q", tok)
	}
	if platform != "ios" {
		t.Errorf("platform: got %q", platform)
	}
}

func TestFileStore_PushToken_Missing(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tok, platform, err := store.GetPushToken(ctx, "no-such-session")
	if err != nil {
		t.Fatalf("GetPushToken unexpected error: %v", err)
	}
	if tok != "" || platform != "" {
		t.Errorf("expected empty, got %q/%q", tok, platform)
	}
}

func TestFileStore_SkillCatalog_RoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	items := []data.SkillDefinition{
		{Name: "test-skill", Description: "a test skill", Prompt: "do something"},
	}
	if err := store.SaveSkillCatalog(ctx, items); err != nil {
		t.Fatalf("SaveSkillCatalog: %v", err)
	}
	got, err := store.ListSkillCatalog(ctx)
	if err != nil {
		t.Fatalf("ListSkillCatalog: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(got))
	}
	if got[0].Name != "test-skill" {
		t.Errorf("Name: got %q", got[0].Name)
	}
}

func TestFileStore_MemoryCatalog_RoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	items := []data.MemoryItem{
		{ID: "mem-1", Title: "test memory", Content: "remember this"},
	}
	if err := store.SaveMemoryCatalog(ctx, items); err != nil {
		t.Fatalf("SaveMemoryCatalog: %v", err)
	}
	got, err := store.ListMemoryCatalog(ctx)
	if err != nil {
		t.Fatalf("ListMemoryCatalog: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(got))
	}
	if got[0].ID != "mem-1" {
		t.Errorf("ID: got %q", got[0].ID)
	}
}

func TestFileStore_PermissionRuleSnapshot(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	snap := data.PermissionRuleSnapshot{
		Enabled: true,
		Items: []data.PermissionRule{
			{ID: "rule-1", Kind: data.PermissionKindWrite, Enabled: true},
		},
	}
	if err := store.SavePermissionRuleSnapshot(ctx, snap); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.GetPermissionRuleSnapshot(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.Enabled {
		t.Error("Enabled should be true")
	}
	if len(got.Items) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(got.Items))
	}
}

func TestFileStore_BaseDir(t *testing.T) {
	store := newTestStore(t)
	if store.BaseDir() == "" {
		t.Error("BaseDir should not be empty")
	}
}

func TestFileStore_DefaultDir(t *testing.T) {
	store, err := data.NewFileStore("")
	if err != nil {
		t.Fatalf("NewFileStore with empty dir: %v", err)
	}
	if store.BaseDir() == "" {
		t.Error("default BaseDir should not be empty")
	}
	// Cleanup
	os.RemoveAll(store.BaseDir())
}
