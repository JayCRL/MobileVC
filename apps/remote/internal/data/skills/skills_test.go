package skills

import (
	"context"
	"strings"
	"testing"
	"time"

	"mobilevc/internal/data"
)

func newTestStore(t *testing.T) *data.FileStore {
	t.Helper()
	store, err := data.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store
}

// --- Builtins / Registry ---

func TestBuiltinsCoversCoreSkills(t *testing.T) {
	got := Builtins()
	must := []string{"review", "analyze", "doctor", "simplify", "debug", "security-review", "explain-step", "next-step", "flutter-context"}
	for _, name := range must {
		if _, ok := got[name]; !ok {
			t.Errorf("missing builtin: %q", name)
		}
	}
	for name, def := range got {
		if def.Source != data.SkillSourceBuiltin {
			t.Errorf("%s: expected builtin source, got %q", name, def.Source)
		}
		if def.Editable {
			t.Errorf("%s: builtin should not be editable", name)
		}
		if strings.TrimSpace(def.Prompt) == "" {
			t.Errorf("%s: empty prompt", name)
		}
	}
}

func TestRegistry_NilStoreReturnsBuiltinsOnly(t *testing.T) {
	r := NewRegistry(nil)
	items, err := r.ListSkills()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != len(Builtins()) {
		t.Errorf("expected %d items, got %d", len(Builtins()), len(items))
	}
	// 确保已排序
	for i := 1; i < len(items); i++ {
		if items[i-1].Name > items[i].Name {
			t.Errorf("not sorted at %d: %q > %q", i, items[i-1].Name, items[i].Name)
		}
	}
}

func TestRegistry_GetSkill(t *testing.T) {
	r := NewRegistry(nil)
	def, ok, err := r.GetSkill("review")
	if err != nil || !ok {
		t.Fatalf("review should exist, err=%v ok=%v", err, ok)
	}
	if def.Name != "review" {
		t.Errorf("name: %q", def.Name)
	}
	_, ok, _ = r.GetSkill("does-not-exist")
	if ok {
		t.Errorf("expected not found")
	}
	// trimming
	_, ok, _ = r.GetSkill("  review  ")
	if !ok {
		t.Errorf("expected trim-tolerant lookup")
	}
}

func TestRegistry_UpsertLocalSkillAndList(t *testing.T) {
	store := newTestStore(t)
	r := NewRegistry(store)

	def := Definition{Name: "my-skill", Description: "x", Prompt: "do x", TargetType: "diff"}
	if err := r.UpsertLocalSkill(def); err != nil {
		t.Fatal(err)
	}
	items, err := r.ListSkills()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range items {
		if item.Name == "my-skill" {
			found = true
			if item.Source != data.SkillSourceLocal {
				t.Errorf("expected local source, got %q", item.Source)
			}
			if !item.Editable {
				t.Errorf("local skill should be editable")
			}
			if item.UpdatedAt.IsZero() {
				t.Errorf("UpdatedAt should be auto-set")
			}
		}
	}
	if !found {
		t.Errorf("inserted skill not in list")
	}

	// 再 upsert 同名 — 更新而非增量
	def2 := Definition{Name: "my-skill", Description: "y", Prompt: "do y", TargetType: "diff"}
	if err := r.UpsertLocalSkill(def2); err != nil {
		t.Fatal(err)
	}
	items, _ = r.ListSkills()
	count := 0
	for _, item := range items {
		if item.Name == "my-skill" {
			count++
			if item.Description != "y" {
				t.Errorf("expected updated description, got %q", item.Description)
			}
		}
	}
	if count != 1 {
		t.Errorf("expected 1 my-skill, got %d", count)
	}
}

func TestRegistry_UpsertLocalOnNilStoreNoop(t *testing.T) {
	r := NewRegistry(nil)
	err := r.UpsertLocalSkill(Definition{Name: "x"})
	if err != nil {
		t.Errorf("expected nil-store no-op, got %v", err)
	}
}

func TestRegistry_LocalOverridesBuiltin(t *testing.T) {
	// review 是 builtin, 我们 upsert 同名 local 改 prompt, ListSkills 应当合并
	store := newTestStore(t)
	r := NewRegistry(store)
	if err := r.UpsertLocalSkill(Definition{Name: "review", Description: "覆盖描述", Prompt: "覆盖 prompt"}); err != nil {
		t.Fatal(err)
	}
	def, ok, err := r.GetSkill("review")
	if err != nil || !ok {
		t.Fatal(err)
	}
	if def.Description != "覆盖描述" {
		t.Errorf("expected overridden description, got %q", def.Description)
	}
}

func TestRegistry_SyncExternalSkillsReplacesPriorExternal(t *testing.T) {
	store := newTestStore(t)
	r := NewRegistry(store)

	// 先注入一个外部 skill
	if err := r.SyncExternalSkills([]Definition{{Name: "ext-1", Prompt: "p1"}}); err != nil {
		t.Fatal(err)
	}
	items, _ := r.ListSkills()
	hasExt1 := false
	for _, it := range items {
		if it.Name == "ext-1" {
			hasExt1 = true
		}
	}
	if !hasExt1 {
		t.Fatal("ext-1 should be registered")
	}

	// 再同步一个新的外部 skill, 旧的 ext-1 应当被替换 (filtered 只保留非 external)
	if err := r.SyncExternalSkills([]Definition{{Name: "ext-2", Prompt: "p2"}}); err != nil {
		t.Fatal(err)
	}
	items, _ = r.ListSkills()
	hasExt1, hasExt2 := false, false
	for _, it := range items {
		if it.Name == "ext-1" {
			hasExt1 = true
		}
		if it.Name == "ext-2" {
			hasExt2 = true
		}
	}
	if hasExt1 {
		t.Errorf("ext-1 should be removed by sync")
	}
	if !hasExt2 {
		t.Errorf("ext-2 should be added")
	}
}

func TestRegistry_SyncExternalKeepsLocal(t *testing.T) {
	store := newTestStore(t)
	r := NewRegistry(store)
	_ = r.UpsertLocalSkill(Definition{Name: "local-1"})
	_ = r.SyncExternalSkills([]Definition{{Name: "ext-1"}})
	items, _ := r.ListSkills()
	hasLocal, hasExt := false, false
	for _, it := range items {
		if it.Name == "local-1" {
			hasLocal = true
		}
		if it.Name == "ext-1" {
			hasExt = true
		}
	}
	if !hasLocal {
		t.Errorf("local should survive external sync")
	}
	if !hasExt {
		t.Errorf("external should be added")
	}
}

func TestMetaForSkill(t *testing.T) {
	def := Definition{Name: "review", TargetType: "diff", ResultView: "card"}
	got := MetaForSkill(def, "tgt", "/p", "ctx", "title", "text")
	if got.Source != "skill-center" {
		t.Errorf("source: %q", got.Source)
	}
	if got.SkillName != "review" {
		t.Errorf("skill name: %q", got.SkillName)
	}
	if got.TargetType != "diff" || got.TargetPath != "/p" || got.ResultView != "card" {
		t.Errorf("misc: %+v", got)
	}
}

func TestMergeDefinition(t *testing.T) {
	base := Definition{
		Name:        "x",
		Description: "old",
		Prompt:      "old",
		ResultView:  "old-view",
		TargetType:  "diff",
	}
	overlay := Definition{
		Description: "new",
		Source:      data.SkillSourceLocal,
		Editable:    true,
		UpdatedAt:   time.Now(),
	}
	got := mergeDefinition(base, overlay)
	if got.Description != "new" {
		t.Errorf("desc: %q", got.Description)
	}
	if got.Prompt != "old" {
		t.Errorf("prompt should be kept: %q", got.Prompt)
	}
	if got.Source != data.SkillSourceLocal {
		t.Errorf("source: %q", got.Source)
	}
	if !got.Editable {
		t.Errorf("editable should be true")
	}
}

func TestMergeExternalDefinitionPreservesBuiltinPrompt(t *testing.T) {
	base := Definition{Name: "review", Source: data.SkillSourceBuiltin, Prompt: "原始 prompt"}
	overlay := Definition{Description: "external desc", Prompt: "外部 prompt"}
	got := mergeExternalDefinition(base, overlay)
	if got.Prompt != "原始 prompt" {
		t.Errorf("builtin prompt should be preserved, got %q", got.Prompt)
	}
}

// --- Launcher ---

func TestLauncher_BuildInvocationUnknownSkill(t *testing.T) {
	store := newTestStore(t)
	l := NewLauncher(store)
	_, err := l.BuildInvocation("does-not-exist", "claude", "/cwd", "diff", "/p", "title", "+ a", "ctx", "title", "text", "stack",
		data.SessionContext{EnabledSkillNames: []string{"does-not-exist"}})
	if err == nil || !strings.Contains(err.Error(), "unknown skill") {
		t.Errorf("expected unknown skill error, got %v", err)
	}
}

func TestLauncher_BuildInvocationDisabled(t *testing.T) {
	store := newTestStore(t)
	l := NewLauncher(store)
	_, err := l.BuildInvocation("review", "claude", "/cwd", "diff", "/p", "", "+ a", "", "", "", "", data.SessionContext{})
	if err == nil || !strings.Contains(err.Error(), "未在本会话启用") {
		t.Errorf("expected enable error, got %v", err)
	}
}

func TestLauncher_BuildInvocationDiffMissingDiff(t *testing.T) {
	store := newTestStore(t)
	l := NewLauncher(store)
	_, err := l.BuildInvocation("review", "claude", "/cwd", "diff", "/p", "", "  ", "", "", "", "",
		data.SessionContext{EnabledSkillNames: []string{"review"}})
	if err == nil || !strings.Contains(err.Error(), "target diff is required") {
		t.Errorf("expected diff required error, got %v", err)
	}
}

func TestLauncher_BuildInvocationDiffHappyPath(t *testing.T) {
	store := newTestStore(t)
	l := NewLauncher(store)
	inv, err := l.BuildInvocation("review", "claude", "/cwd", "diff", "/p/x.go", "", "+ added\n- removed", "ctx-1", "ContextTitle", "", "",
		data.SessionContext{EnabledSkillNames: []string{"review"}})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Engine != "claude" {
		t.Errorf("engine: %q", inv.Engine)
	}
	if inv.CWD != "/cwd" {
		t.Errorf("cwd: %q", inv.CWD)
	}
	if !strings.Contains(inv.Prompt, "ContextTitle") {
		t.Errorf("prompt should contain context title: %q", inv.Prompt)
	}
	if !strings.Contains(inv.Prompt, "/p/x.go") {
		t.Errorf("prompt should contain target path: %q", inv.Prompt)
	}
	if !strings.Contains(inv.Prompt, "+ added") {
		t.Errorf("prompt should contain diff body: %q", inv.Prompt)
	}
	if inv.RuntimeMeta.SkillName != "review" {
		t.Errorf("meta skill: %q", inv.RuntimeMeta.SkillName)
	}
	if inv.RuntimeMeta.TargetPath != "/p/x.go" {
		t.Errorf("meta target path: %q", inv.RuntimeMeta.TargetPath)
	}
}

func TestLauncher_BuildInvocation_EngineSelection(t *testing.T) {
	store := newTestStore(t)
	l := NewLauncher(store)
	cases := []struct {
		engineIn, want string
	}{
		{"", "claude"},
		{"Codex", "codex"},
		{"GEMINI", "gemini"},
		{"unknown", "claude"},
	}
	for _, tc := range cases {
		inv, err := l.BuildInvocation("review", tc.engineIn, "/cwd", "diff", "/p", "", "+ a", "", "", "", "",
			data.SessionContext{EnabledSkillNames: []string{"review"}})
		if err != nil {
			t.Fatalf("[%s] %v", tc.engineIn, err)
		}
		if inv.Engine != tc.want {
			t.Errorf("[engineIn=%q] got %q, want %q", tc.engineIn, inv.Engine, tc.want)
		}
	}
}

func TestLauncher_BuildInvocationStepError(t *testing.T) {
	store := newTestStore(t)
	l := NewLauncher(store)
	_, err := l.BuildInvocation("explain-step", "claude", "/cwd", "step", "/p", "", "", "", "", "  ", "",
		data.SessionContext{EnabledSkillNames: []string{"explain-step"}})
	if err == nil || !strings.Contains(err.Error(), "target text is required") {
		t.Errorf("expected text required err, got %v", err)
	}
}

func TestLauncher_BuildInvocationContextOK(t *testing.T) {
	store := newTestStore(t)
	l := NewLauncher(store)
	inv, err := l.BuildInvocation("flutter-context", "claude", "/cwd", "context", "", "", "", "", "", "extra context", "",
		data.SessionContext{EnabledSkillNames: []string{"flutter-context"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(inv.Prompt, "extra context") {
		t.Errorf("expected target text body: %q", inv.Prompt)
	}
}

func TestLauncher_BuildInvocationErrorPromptRequiresMessageOrStack(t *testing.T) {
	store := newTestStore(t)
	l := NewLauncher(store)
	_, err := l.BuildInvocation("debug", "claude", "/cwd", "error", "/p", "", "", "", "", "", "",
		data.SessionContext{EnabledSkillNames: []string{"debug"}})
	if err == nil {
		t.Errorf("expected error when neither message nor stack provided")
	}

	inv, err := l.BuildInvocation("debug", "claude", "/cwd", "error", "/p", "", "", "", "", "msg", "stack body",
		data.SessionContext{EnabledSkillNames: []string{"debug"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(inv.Prompt, "msg") || !strings.Contains(inv.Prompt, "stack body") {
		t.Errorf("expected error/stack in prompt: %q", inv.Prompt)
	}
}

func TestNormalizeContextType(t *testing.T) {
	cases := []struct{ in, want string }{
		{"current-diff", "diff"},
		{"diff", "diff"},
		{"current-step", "step"},
		{"step", "step"},
		{"current-error", "error"},
		{"error", "error"},
		{"current-context", "context"},
		{"context", "context"},
		{"  custom  ", "custom"},
	}
	for _, tc := range cases {
		if got := normalizeContextType(tc.in); got != tc.want {
			t.Errorf("(%q) -> %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestQuotePromptAndExtractPrompt(t *testing.T) {
	cases := []string{
		"hello",
		`包含 "引号" 的文本`,
		"line1\nline2",
		`backslash \\test`,
	}
	for _, in := range cases {
		quoted := QuotePrompt(in)
		// quoted 必须以 " 开头结尾
		if !strings.HasPrefix(quoted, `"`) || !strings.HasSuffix(quoted, `"`) {
			t.Errorf("expected quoted form for %q, got %q", in, quoted)
		}
		// 模拟 shell 命令: 前缀 + quoted
		got := ExtractPrompt("claude " + quoted + " --foo")
		if got != in {
			t.Errorf("extract round-trip failed: in=%q got=%q (quoted=%q)", in, got, quoted)
		}
	}
}

func TestExtractPromptWithoutQuoteReturnsEmpty(t *testing.T) {
	if got := ExtractPrompt("no quotes here"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestLauncherExtractPromptInstanceForward(t *testing.T) {
	l := &Launcher{}
	if got := l.ExtractPrompt(`claude "hi"`); got != "hi" {
		t.Errorf("instance forward broken: %q", got)
	}
}

func TestIsSkillEnabled(t *testing.T) {
	ctx := data.SessionContext{EnabledSkillNames: []string{"review", " analyze "}}
	if !isSkillEnabled(ctx, "review") {
		t.Errorf("review should be enabled")
	}
	if !isSkillEnabled(ctx, "analyze") {
		t.Errorf("analyze (trimmed) should be enabled")
	}
	if isSkillEnabled(ctx, "unknown") {
		t.Errorf("unknown should not be enabled")
	}
}

func TestBuildMemoryPrefix(t *testing.T) {
	t.Run("empty inputs returns empty", func(t *testing.T) {
		if got := BuildMemoryPrefix(data.SessionContext{}, nil); got != "" {
			t.Errorf("expected empty, got %q", got)
		}
		if got := BuildMemoryPrefix(data.SessionContext{EnabledMemoryIDs: []string{"m"}}, nil); got != "" {
			t.Errorf("expected empty when items nil, got %q", got)
		}
	})
	t.Run("renders bullet list", func(t *testing.T) {
		ctx := data.SessionContext{EnabledMemoryIDs: []string{"m1"}}
		items := []data.MemoryItem{{ID: "m1", Title: "Title", Content: "  content  "}}
		got := BuildMemoryPrefix(ctx, items)
		if !strings.Contains(got, "Title") {
			t.Errorf("expected title in output: %q", got)
		}
		if !strings.Contains(got, "[MobileVC Memory]") {
			t.Errorf("expected header: %q", got)
		}
		if !strings.Contains(got, "content") {
			t.Errorf("expected trimmed content: %q", got)
		}
	})
}

// --- session_context.go ---

func TestBuildEnabledSkillsPrefix_NotConfiguredReturnsEmpty(t *testing.T) {
	got, err := BuildEnabledSkillsPrefix(nil, data.SessionContext{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("expected empty when not configured, got %q", got)
	}
}

func TestBuildEnabledSkillsPrefix_ConfiguredButEmpty(t *testing.T) {
	got, err := BuildEnabledSkillsPrefix(nil, data.SessionContext{Configured: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "(无)") {
		t.Errorf("expected (无) marker, got %q", got)
	}
}

func TestBuildEnabledSkillsPrefix_ListsEnabled(t *testing.T) {
	store := newTestStore(t)
	got, err := BuildEnabledSkillsPrefix(store, data.SessionContext{
		Configured:        true,
		EnabledSkillNames: []string{"review", "  analyze  ", "review"}, // 重复+空白
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "- review") {
		t.Errorf("expected review listed, got %q", got)
	}
	if !strings.Contains(got, "- analyze") {
		t.Errorf("expected analyze listed (trim), got %q", got)
	}
	// 重复名字应当只出现一次
	if strings.Count(got, "- review") != 1 {
		t.Errorf("expected single review entry, got: %q", got)
	}
}

func TestBuildEnabledMemoryPrefix_EmptyConfigured(t *testing.T) {
	got, err := BuildEnabledMemoryPrefix(nil, data.SessionContext{Configured: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "(无)") {
		t.Errorf("expected (无) marker, got %q", got)
	}
}

func TestBuildEnabledMemoryPrefix_NotConfigured(t *testing.T) {
	got, err := BuildEnabledMemoryPrefix(nil, data.SessionContext{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestBuildEnabledMemoryPrefix_RendersSyncedItem(t *testing.T) {
	store := newTestStore(t)
	if err := store.SaveMemoryCatalog(context.Background(), []data.MemoryItem{
		{ID: "m1", Title: "M1", Content: "c1", SyncState: data.CatalogSyncStateSynced},
		{ID: "m2", Title: "M2 unsynced", Content: "c2", SyncState: data.CatalogSyncStateDraft},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := BuildEnabledMemoryPrefix(store, data.SessionContext{
		Configured:       true,
		EnabledMemoryIDs: []string{"m1", "m2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "M1") {
		t.Errorf("expected synced item title in output: %q", got)
	}
	if strings.Contains(got, "M2 unsynced") {
		t.Errorf("expected unsynced item filtered out, got: %q", got)
	}
}

func TestInjectConversationPrefixes(t *testing.T) {
	t.Run("no prefixes returns input", func(t *testing.T) {
		if got := InjectConversationPrefixes("hi"); got != "hi" {
			t.Errorf("got %q", got)
		}
		if got := InjectConversationPrefixes("hi", "  ", ""); got != "hi" {
			t.Errorf("expected unchanged for blank prefixes, got %q", got)
		}
	})
	t.Run("adds prefixes", func(t *testing.T) {
		got := InjectConversationPrefixes("hi", "P1", "P2")
		if !strings.Contains(got, "P1") || !strings.Contains(got, "P2") {
			t.Errorf("expected both prefixes: %q", got)
		}
		if !strings.Contains(got, "[User Input]") {
			t.Errorf("expected [User Input] separator: %q", got)
		}
	})
	t.Run("idempotent if header already present", func(t *testing.T) {
		input := "[MobileVC Enabled Skills]\nalready prefixed"
		got := InjectConversationPrefixes(input, "ignored")
		if got != input {
			t.Errorf("expected unchanged input, got %q", got)
		}
	})
}

func TestInjectEnabledSkillsPrefixForward(t *testing.T) {
	got := InjectEnabledSkillsPrefix("hi", "P")
	if !strings.Contains(got, "P") || !strings.Contains(got, "[User Input]") {
		t.Errorf("expected merged, got %q", got)
	}
}

func TestLoadEnabledMemoryItems_NilStore(t *testing.T) {
	got, err := loadEnabledMemoryItems(nil, data.SessionContext{EnabledMemoryIDs: []string{"x"}})
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil for nil store, got %+v", got)
	}
}
