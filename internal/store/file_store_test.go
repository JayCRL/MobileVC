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

func TestFileStorePersistsSessionContext(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "ctx")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	_, err = fs.SaveProjection(context.Background(), created.ID, ProjectionSnapshot{
		RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
		SessionContext: SessionContext{
			EnabledSkillNames: []string{"review", "analyze"},
			EnabledMemoryIDs:  []string{"m1", "m2"},
		},
	})
	if err != nil {
		t.Fatalf("save projection: %v", err)
	}
	record, err := fs.GetSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if len(record.Projection.SessionContext.EnabledSkillNames) != 2 {
		t.Fatalf("unexpected enabled skills length: %#v", record.Projection.SessionContext)
	}
	seenSkills := map[string]bool{}
	for _, item := range record.Projection.SessionContext.EnabledSkillNames {
		seenSkills[item] = true
	}
	if !seenSkills["review"] || !seenSkills["analyze"] {
		t.Fatalf("unexpected enabled skills: %#v", record.Projection.SessionContext)
	}
	if len(record.Projection.SessionContext.EnabledMemoryIDs) != 2 || record.Projection.SessionContext.EnabledMemoryIDs[1] != "m2" {
		t.Fatalf("unexpected enabled memories: %#v", record.Projection.SessionContext)
	}
}

func TestFileStoreSkillAndMemoryCatalogRoundTrip(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	err = fs.SaveSkillCatalog(context.Background(), []SkillDefinition{{
		Name:        "local-review",
		Description: "desc",
		Prompt:      "prompt",
		ResultView:  "review-card",
		TargetType:  "diff",
		Source:      SkillSourceLocal,
		Editable:    true,
	}})
	if err != nil {
		t.Fatalf("save skill catalog: %v", err)
	}
	err = fs.SaveMemoryCatalog(context.Background(), []MemoryItem{{
		ID:      "mem-1",
		Title:   "Memory 1",
		Content: "content",
	}})
	if err != nil {
		t.Fatalf("save memory catalog: %v", err)
	}
	skills, err := fs.ListSkillCatalog(context.Background())
	if err != nil {
		t.Fatalf("list skill catalog: %v", err)
	}
	if len(skills) != 1 || skills[0].Name != "local-review" {
		t.Fatalf("unexpected skill catalog: %#v", skills)
	}
	memory, err := fs.ListMemoryCatalog(context.Background())
	if err != nil {
		t.Fatalf("list memory catalog: %v", err)
	}
	if len(memory) != 1 || memory[0].ID != "mem-1" {
		t.Fatalf("unexpected memory catalog: %#v", memory)
	}
}
