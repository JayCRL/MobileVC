package store

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
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
		SkillCatalogMeta: CatalogMetadata{
			SourceOfTruth: CatalogSourceTruthClaude,
			SyncState:     CatalogSyncStateSynced,
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
	if record.Projection.SkillCatalogMeta.SyncState != CatalogSyncStateSynced {
		t.Fatalf("expected skill catalog meta persisted, got %#v", record.Projection.SkillCatalogMeta)
	}
}

func TestFileStoreSaveProjectionDerivesTitleAndPreviewFromMeaningfulUserInput(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	summary, err := fs.SaveProjection(context.Background(), created.ID, ProjectionSnapshot{
		RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
		LogEntries: []SnapshotLogEntry{
			{Kind: "user", Message: "codex -m gpt-5-codex --config model_reasoning_effort=high"},
			{Kind: "system", Message: "command started"},
			{Kind: "user", Message: "帮我查看这个项目的会话回复逻辑"},
			{Kind: "markdown", Message: "我先看下项目结构。"},
			{Kind: "user", Message: "再看下恢复逻辑"},
		},
	})
	if err != nil {
		t.Fatalf("save projection: %v", err)
	}

	if summary.Title != "帮我查看这个项目的会话回复逻辑" {
		t.Fatalf("expected derived title, got %q", summary.Title)
	}
	if summary.LastPreview != "再看下恢复逻辑" {
		t.Fatalf("expected latest user preview, got %q", summary.LastPreview)
	}
}

func TestFileStoreListSessionsRepairsLegacySummaryFromProjection(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	staleRecord := SessionRecord{
		Summary: SessionSummary{
			ID:        created.ID,
			Title:     "2026-04-01 20:15",
			CreatedAt: created.CreatedAt,
			UpdatedAt: created.UpdatedAt,
			Runtime:   SessionRuntime{Source: "mobilevc"},
		},
		Projection: ProjectionSnapshot{
			RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
			LogEntries: []SnapshotLogEntry{
				{Kind: "user", Message: "看下这个项目的会话恢复逻辑"},
				{Kind: "user", Message: "顺便检查一下 resume"},
			},
			Runtime: SessionRuntime{Source: "mobilevc"},
		},
	}
	data, err := json.MarshalIndent(staleRecord, "", "  ")
	if err != nil {
		t.Fatalf("marshal stale record: %v", err)
	}
	if err := os.WriteFile(fs.sessionPath(created.ID), data, 0o644); err != nil {
		t.Fatalf("write stale record: %v", err)
	}
	indexData, err := json.MarshalIndent(fileIndex{Sessions: []SessionSummary{staleRecord.Summary}}, "", "  ")
	if err != nil {
		t.Fatalf("marshal stale index: %v", err)
	}
	if err := os.WriteFile(fs.indexPath, indexData, 0o644); err != nil {
		t.Fatalf("write stale index: %v", err)
	}

	items, err := fs.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one session, got %#v", items)
	}
	if items[0].Title != "看下这个项目的会话恢复逻辑" {
		t.Fatalf("expected repaired title, got %q", items[0].Title)
	}
	if items[0].LastPreview != "顺便检查一下 resume" {
		t.Fatalf("expected repaired preview, got %q", items[0].LastPreview)
	}
}

func TestFileStoreReadsLegacySkillCatalogArray(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	legacy := `[
	  {
	    "name": "legacy-review",
	    "description": "legacy",
	    "prompt": "review it",
	    "resultView": "review-card",
	    "targetType": "diff"
	  }
	]`
	if err := os.WriteFile(fs.skillCatalogPath, []byte(legacy), 0o644); err != nil {
		t.Fatalf("write legacy skill catalog: %v", err)
	}
	snapshot, err := fs.GetSkillCatalogSnapshot(context.Background())
	if err != nil {
		t.Fatalf("get skill snapshot: %v", err)
	}
	if snapshot.Meta.Domain != CatalogDomainSkill {
		t.Fatalf("expected skill domain metadata, got %#v", snapshot.Meta)
	}
	if len(snapshot.Items) != 1 || snapshot.Items[0].Name != "legacy-review" {
		t.Fatalf("unexpected legacy skill catalog items: %#v", snapshot.Items)
	}
}

func TestFileStoreSkillAndMemoryCatalogRoundTrip(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	skillSyncedAt := mustTime("2026-03-25T10:00:00Z")
	memorySyncedAt := mustTime("2026-03-25T11:00:00Z")
	err = fs.SaveSkillCatalogSnapshot(context.Background(), SkillCatalogSnapshot{
		Meta: CatalogMetadata{
			Domain:        CatalogDomainSkill,
			SourceOfTruth: CatalogSourceTruthClaude,
			SyncState:     CatalogSyncStateSynced,
			DriftDetected: false,
			LastSyncedAt:  skillSyncedAt,
			VersionToken:  "skill-v1",
		},
		Items: []SkillDefinition{{
			Name:          "local-review",
			Description:   "desc",
			Prompt:        "prompt",
			ResultView:    "review-card",
			TargetType:    "diff",
			Source:        SkillSourceLocal,
			SourceOfTruth: CatalogSourceTruthClaude,
			SyncState:     CatalogSyncStateDraft,
			Editable:      true,
			DriftDetected: true,
			LastSyncedAt:  skillSyncedAt,
		}},
	})
	if err != nil {
		t.Fatalf("save skill catalog: %v", err)
	}
	err = fs.SaveMemoryCatalogSnapshot(context.Background(), MemoryCatalogSnapshot{
		Meta: CatalogMetadata{
			Domain:        CatalogDomainMemory,
			SourceOfTruth: CatalogSourceTruthClaude,
			SyncState:     CatalogSyncStateDraft,
			DriftDetected: true,
			LastSyncedAt:  memorySyncedAt,
			VersionToken:  "memory-v1",
		},
		Items: []MemoryItem{{
			ID:            "mem-1",
			Title:         "Memory 1",
			Content:       "content",
			Source:        "local",
			SourceOfTruth: CatalogSourceTruthClaude,
			SyncState:     CatalogSyncStateSynced,
			Editable:      true,
			DriftDetected: false,
			LastSyncedAt:  memorySyncedAt,
		}},
	})
	if err != nil {
		t.Fatalf("save memory catalog: %v", err)
	}
	skillSnapshot, err := fs.GetSkillCatalogSnapshot(context.Background())
	if err != nil {
		t.Fatalf("get skill snapshot: %v", err)
	}
	if skillSnapshot.Meta.SyncState != CatalogSyncStateSynced || skillSnapshot.Meta.VersionToken != "skill-v1" {
		t.Fatalf("unexpected skill snapshot meta: %#v", skillSnapshot.Meta)
	}
	if len(skillSnapshot.Items) != 1 || skillSnapshot.Items[0].Name != "local-review" || skillSnapshot.Items[0].LastSyncedAt.IsZero() {
		t.Fatalf("unexpected skill catalog: %#v", skillSnapshot.Items)
	}
	memorySnapshot, err := fs.GetMemoryCatalogSnapshot(context.Background())
	if err != nil {
		t.Fatalf("get memory snapshot: %v", err)
	}
	if memorySnapshot.Meta.SyncState != CatalogSyncStateDraft || !memorySnapshot.Meta.DriftDetected {
		t.Fatalf("unexpected memory snapshot meta: %#v", memorySnapshot.Meta)
	}
	if len(memorySnapshot.Items) != 1 || memorySnapshot.Items[0].ID != "mem-1" || memorySnapshot.Items[0].SyncState != CatalogSyncStateSynced {
		t.Fatalf("unexpected memory catalog: %#v", memorySnapshot.Items)
	}
}

func TestFileStoreMemoryCatalogUpsertReadBackIncludesNewItem(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	updatedAt := mustTime("2026-03-25T12:00:00Z")
	if err := fs.SaveMemoryCatalogSnapshot(context.Background(), MemoryCatalogSnapshot{
		Meta: CatalogMetadata{Domain: CatalogDomainMemory},
		Items: []MemoryItem{{
			ID:        "mem-new",
			Title:     "Remember",
			Content:   "remember this",
			Source:    "local",
			Editable:  true,
			UpdatedAt: updatedAt,
		}},
	}); err != nil {
		t.Fatalf("save memory catalog snapshot: %v", err)
	}
	items, err := fs.ListMemoryCatalog(context.Background())
	if err != nil {
		t.Fatalf("list memory catalog: %v", err)
	}
	if len(items) != 1 || items[0].ID != "mem-new" || items[0].Content != "remember this" {
		t.Fatalf("unexpected memory items: %#v", items)
	}
}

func TestFileStoreMemoryCatalogNormalizationDefaultsDomainAndSyncState(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	if err := fs.SaveMemoryCatalogSnapshot(context.Background(), MemoryCatalogSnapshot{
		Meta:  CatalogMetadata{},
		Items: []MemoryItem{{ID: "mem-1", Title: "Memory 1", Content: "hello"}},
	}); err != nil {
		t.Fatalf("save memory catalog snapshot: %v", err)
	}
	snapshot, err := fs.GetMemoryCatalogSnapshot(context.Background())
	if err != nil {
		t.Fatalf("get memory snapshot: %v", err)
	}
	if snapshot.Meta.Domain != CatalogDomainMemory {
		t.Fatalf("expected memory domain, got %#v", snapshot.Meta)
	}
	if snapshot.Meta.SyncState != CatalogSyncStateIdle {
		t.Fatalf("expected idle sync state, got %#v", snapshot.Meta)
	}
	if len(snapshot.Items) != 1 || snapshot.Items[0].ID != "mem-1" {
		t.Fatalf("unexpected memory snapshot items: %#v", snapshot.Items)
	}
}

func mustTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}
