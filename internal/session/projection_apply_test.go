package session

import (
	"strings"
	"testing"
	"time"

	"mobilevc/internal/data"
	"mobilevc/internal/protocol"
)

func TestRuntimeEngineFromMeta(t *testing.T) {
	cases := []struct {
		name string
		meta protocol.RuntimeMeta
		want string
	}{
		{"explicit claude engine", protocol.RuntimeMeta{Engine: "claude"}, "claude"},
		{"upper case codex", protocol.RuntimeMeta{Engine: "Codex"}, "codex"},
		{"command head claude", protocol.RuntimeMeta{Command: "claude --resume x"}, "claude"},
		{"command head codex", protocol.RuntimeMeta{Command: "codex run"}, "codex"},
		{"resume id implies claude", protocol.RuntimeMeta{ResumeSessionID: "abc"}, "claude"},
		{"unknown returns empty", protocol.RuntimeMeta{Command: "bash"}, ""},
		{"empty returns empty", protocol.RuntimeMeta{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runtimeEngineFromMeta(tc.meta); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPathBase(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"file.go", "file.go"},
		{"/abs/path/file.go", "file.go"},
		{`C:\Win\Sub\file.go`, "file.go"},
		{"a/b/c", "c"},
		{"  /trim/  ", ""}, // 末尾 / 之后是空
	}
	for _, tc := range cases {
		if got := pathBase(tc.in); got != tc.want {
			t.Errorf("pathBase(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeToolLabel(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"  ", ""},
		{"Read", "Read"},
		{"read", "Read"},
		{"WRITE", "Write"},
		{"edit", "Edit"},
		{"bash", "Bash"},
		{"CustomTool", "CustomTool"}, // unknown 保持原样
	}
	for _, tc := range cases {
		if got := normalizeToolLabel(tc.in); got != tc.want {
			t.Errorf("normalizeToolLabel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestAIStatusVerbForTool(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Read", "正在读取"},
		{"read", "正在读取"},
		{"  Read  ", "正在读取"},
		{"Write", "正在写入"},
		{"Edit", "正在修改"},
		{"Bash", "正在执行命令"},
		{"Grep", "正在搜索"},
		{"Glob", "正在查找文件"},
		{"WebFetch", "正在抓取网页"},
		{"WebSearch", "正在联网搜索"},
		{"Agent", "正在派发子代理"},
		{"Skill", "正在调用 skill"},
		{"Web_Search", "正在联网搜索"}, // 非字母被去掉后 = "websearch"
		{"unknown", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := aiStatusVerbForTool(tc.in); got != tc.want {
			t.Errorf("aiStatusVerbForTool(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLooksLikeMarkdownMessage(t *testing.T) {
	yes := []string{"```code", "# heading", "## sub", "- item", strings.Repeat("a", 200)}
	no := []string{"", "  ", "plain short text"}
	for _, m := range yes {
		if !looksLikeMarkdownMessage(m) {
			t.Errorf("expected markdown for %q", m)
		}
	}
	for _, m := range no {
		if looksLikeMarkdownMessage(m) {
			t.Errorf("expected NOT markdown for %q", m)
		}
	}
}

func TestLooksLikeTerminalLikeLogLine(t *testing.T) {
	yes := []string{"$ ls", "# rm", "> echo", "at line 5", "fatal: oops", "error: bad", "Traceback xxx", "[INFO] hi", "[ERROR] x", "task : foo"}
	no := []string{"", "regular line", "Hello world"}
	for _, m := range yes {
		if !looksLikeTerminalLikeLogLine(m) {
			t.Errorf("expected terminal-like for %q", m)
		}
	}
	for _, m := range no {
		if looksLikeTerminalLikeLogLine(m) {
			t.Errorf("expected NOT terminal-like for %q", m)
		}
	}
}

func TestFallbackString(t *testing.T) {
	if got := fallbackString("", "def"); got != "def" {
		t.Errorf("got %q", got)
	}
	if got := fallbackString("  ", "def"); got != "def" {
		t.Errorf("got %q for whitespace", got)
	}
	if got := fallbackString("v", "def"); got != "v" {
		t.Errorf("got %q", got)
	}
}

func TestNormalizeAssistantReplyForDedupe(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"  hello   world  ", "hello world"},
		{"a\n\tb\nc", "a b c"},
	}
	for _, tc := range cases {
		if got := normalizeAssistantReplyForDedupe(tc.in); got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}
}

func TestAppendExecutionStream(t *testing.T) {
	t.Run("nil item is no-op", func(t *testing.T) {
		appendExecutionStream(nil, "stdout", "x") // 不能 panic
	})
	t.Run("empty text is no-op", func(t *testing.T) {
		item := &data.TerminalExecution{}
		appendExecutionStream(item, "stdout", "")
		if item.Stdout != "" || item.Stderr != "" {
			t.Errorf("unexpected mutation: %+v", item)
		}
	})
	t.Run("stdout append", func(t *testing.T) {
		item := &data.TerminalExecution{Stdout: "first"}
		appendExecutionStream(item, "stdout", "second")
		if item.Stdout != "first\nsecond" {
			t.Errorf("got %q", item.Stdout)
		}
	})
	t.Run("stderr default", func(t *testing.T) {
		item := &data.TerminalExecution{}
		appendExecutionStream(item, "stderr", "err")
		if item.Stderr != "err" {
			t.Errorf("got %q", item.Stderr)
		}
	})
	t.Run("unknown stream defaults to stdout", func(t *testing.T) {
		item := &data.TerminalExecution{}
		appendExecutionStream(item, "weird", "x")
		if item.Stdout != "x" {
			t.Errorf("got %q", item.Stdout)
		}
	})
}

func TestUpsertTerminalExecution(t *testing.T) {
	t.Run("empty execution id no-op", func(t *testing.T) {
		got := upsertTerminalExecution(nil, data.TerminalExecution{})
		if got != nil {
			t.Errorf("expected nil, got %+v", got)
		}
	})
	t.Run("inserts new", func(t *testing.T) {
		got := upsertTerminalExecution(nil, data.TerminalExecution{ExecutionID: "e1", Command: "ls"})
		if len(got) != 1 || got[0].ExecutionID != "e1" {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("updates existing", func(t *testing.T) {
		base := []data.TerminalExecution{{ExecutionID: "e1", Command: "old"}}
		exit := 0
		got := upsertTerminalExecution(base, data.TerminalExecution{
			ExecutionID: "e1",
			Command:     "new",
			CWD:         "/tmp",
			StartedAt:   "t1",
			FinishedAt:  "t2",
			ExitCode:    &exit,
			Stdout:      "x",
			Stderr:      "y",
		})
		if len(got) != 1 {
			t.Fatalf("expected 1 element, got %d", len(got))
		}
		if got[0].Command != "new" || got[0].CWD != "/tmp" || got[0].StartedAt != "t1" || got[0].FinishedAt != "t2" {
			t.Errorf("unexpected: %+v", got[0])
		}
		if got[0].ExitCode == nil || *got[0].ExitCode != 0 {
			t.Errorf("exit code: %v", got[0].ExitCode)
		}
		if got[0].Stdout != "x" || got[0].Stderr != "y" {
			t.Errorf("streams: %+v", got[0])
		}
	})
}

func TestUpdateTerminalExecution(t *testing.T) {
	t.Run("empty id no-op", func(t *testing.T) {
		got := updateTerminalExecution(nil, "", func(*data.TerminalExecution) {})
		if got != nil {
			t.Errorf("expected nil")
		}
	})
	t.Run("creates if not found", func(t *testing.T) {
		got := updateTerminalExecution(nil, "e1", func(item *data.TerminalExecution) {
			item.Command = "ls"
		})
		if len(got) != 1 || got[0].Command != "ls" {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("updates if found", func(t *testing.T) {
		base := []data.TerminalExecution{{ExecutionID: "e1", Command: "old"}}
		got := updateTerminalExecution(base, "e1", func(item *data.TerminalExecution) {
			item.Command = "new"
		})
		if got[0].Command != "new" {
			t.Errorf("got %+v", got[0])
		}
	})
}

func TestUpsertSnapshotDiff(t *testing.T) {
	t.Run("insert by ContextID", func(t *testing.T) {
		got := upsertSnapshotDiff(nil, DiffContext{ContextID: "c1", Path: "p1"})
		if len(got) != 1 {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("update by ContextID", func(t *testing.T) {
		base := []DiffContext{{ContextID: "c1", Path: "p1", Title: "old"}}
		got := upsertSnapshotDiff(base, DiffContext{ContextID: "c1", Title: "new"})
		if len(got) != 1 || got[0].Title != "new" {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("update by Path", func(t *testing.T) {
		base := []DiffContext{{ContextID: "c1", Path: "p1", Title: "old"}}
		got := upsertSnapshotDiff(base, DiffContext{Path: "p1", Title: "new"})
		if len(got) != 1 || got[0].Title != "new" {
			t.Errorf("got %+v", got)
		}
	})
}

func TestIsAIStatusContext(t *testing.T) {
	cases := []struct {
		name       string
		command    string
		meta       protocol.RuntimeMeta
		projection data.ProjectionSnapshot
		want       bool
	}{
		{"command claude direct", "claude", protocol.RuntimeMeta{}, data.ProjectionSnapshot{}, true},
		{"meta command codex", "", protocol.RuntimeMeta{Command: "codex run"}, data.ProjectionSnapshot{}, true},
		{"projection runtime engine claude", "", protocol.RuntimeMeta{}, data.ProjectionSnapshot{Runtime: data.SessionRuntime{Engine: "claude"}}, true},
		{"meta engine gemini", "", protocol.RuntimeMeta{Engine: "gemini"}, data.ProjectionSnapshot{}, true},
		{"none -> false", "", protocol.RuntimeMeta{}, data.ProjectionSnapshot{}, false},
		{"bash command -> false", "bash", protocol.RuntimeMeta{}, data.ProjectionSnapshot{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isAIStatusContext(tc.command, tc.meta, tc.projection); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAIStatusLabelFromState(t *testing.T) {
	t.Run("step explicit wins", func(t *testing.T) {
		got := aiStatusLabelFromState("RUNNING", "loading", "Read", "claude", protocol.RuntimeMeta{}, data.ProjectionSnapshot{})
		if got != "loading" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("verb + target", func(t *testing.T) {
		got := aiStatusLabelFromState("RUNNING", "", "Read", "claude",
			protocol.RuntimeMeta{TargetPath: "/abs/foo.go"}, data.ProjectionSnapshot{})
		if got != "正在读取 · foo.go" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("verb only without target", func(t *testing.T) {
		got := aiStatusLabelFromState("RUNNING", "", "Edit", "", protocol.RuntimeMeta{}, data.ProjectionSnapshot{})
		if got != "正在修改" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("running tool with tool label fallback", func(t *testing.T) {
		// 工具不在 verbForTool map 但在 normalizeToolLabel map
		got := aiStatusLabelFromState("RUNNING_TOOL", "", "CustomTool", "", protocol.RuntimeMeta{}, data.ProjectionSnapshot{})
		if got != "执行中 · CustomTool" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("recovering", func(t *testing.T) {
		if got := aiStatusLabelFromState("RECOVERING", "", "", "", protocol.RuntimeMeta{}, data.ProjectionSnapshot{}); got != "恢复中" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("running default", func(t *testing.T) {
		if got := aiStatusLabelFromState("RUNNING", "", "", "", protocol.RuntimeMeta{}, data.ProjectionSnapshot{}); got != "运行中" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("unknown defaults to thinking", func(t *testing.T) {
		if got := aiStatusLabelFromState("THINKING", "", "", "", protocol.RuntimeMeta{}, data.ProjectionSnapshot{}); got != "思考中" {
			t.Errorf("got %q", got)
		}
	})
}

func TestIsVisibleAssistantReplyLog(t *testing.T) {
	cases := []struct {
		name string
		in   protocol.LogEvent
		want bool
	}{
		{
			"stderr never visible",
			protocol.LogEvent{Stream: "stderr", Message: "x", Event: protocol.Event{RuntimeMeta: protocol.RuntimeMeta{Source: "claude/assistant"}}},
			false,
		},
		{
			"explicit claude assistant source",
			protocol.LogEvent{Message: "hi", Event: protocol.Event{RuntimeMeta: protocol.RuntimeMeta{Source: "claude/assistant"}}},
			true,
		},
		{
			"system bootstrap source not visible",
			protocol.LogEvent{Message: "hi", Event: protocol.Event{RuntimeMeta: protocol.RuntimeMeta{Source: "system/bootstrap"}}},
			false,
		},
		{
			"empty message",
			protocol.LogEvent{Message: "  ", Event: protocol.Event{RuntimeMeta: protocol.RuntimeMeta{Engine: "claude"}}},
			false,
		},
		{
			"claude engine plain text",
			protocol.LogEvent{Message: "hello world", Event: protocol.Event{RuntimeMeta: protocol.RuntimeMeta{Engine: "claude"}}},
			true,
		},
		{
			"output prefix",
			protocol.LogEvent{Message: "output: foo", Event: protocol.Event{RuntimeMeta: protocol.RuntimeMeta{Engine: "claude"}}},
			false,
		},
		{
			"non-AI engine excluded",
			protocol.LogEvent{Message: "hi", Event: protocol.Event{RuntimeMeta: protocol.RuntimeMeta{Engine: "bash"}}},
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsVisibleAssistantReplyLog(tc.in); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsSystemBootstrapLog(t *testing.T) {
	cases := []struct {
		name string
		in   protocol.LogEvent
		want bool
	}{
		{"non-codex never bootstrap", protocol.LogEvent{Message: "Using foo mode", Event: protocol.Event{RuntimeMeta: protocol.RuntimeMeta{Engine: "claude"}}}, false},
		{"codex Using mode", protocol.LogEvent{Message: "Using fast mode", Event: protocol.Event{RuntimeMeta: protocol.RuntimeMeta{Engine: "codex"}}}, true},
		{"codex reasoning effort", protocol.LogEvent{Message: "reasoning effort low", Event: protocol.Event{RuntimeMeta: protocol.RuntimeMeta{Engine: "codex"}}}, true},
		{"codex how can I help", protocol.LogEvent{Message: "How can I help you", Event: protocol.Event{RuntimeMeta: protocol.RuntimeMeta{Engine: "codex"}}}, true},
		{"codex chinese 设置模型", protocol.LogEvent{Message: "设置模型 done", Event: protocol.Event{RuntimeMeta: protocol.RuntimeMeta{Engine: "codex"}}}, true},
		{"codex empty msg", protocol.LogEvent{Message: " ", Event: protocol.Event{RuntimeMeta: protocol.RuntimeMeta{Engine: "codex"}}}, false},
		{"codex unknown", protocol.LogEvent{Message: "regular output", Event: protocol.Event{RuntimeMeta: protocol.RuntimeMeta{Engine: "codex"}}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSystemBootstrapLog(tc.in); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMarkSystemBootstrapEvent(t *testing.T) {
	t.Run("non-log passthrough", func(t *testing.T) {
		ev := protocol.AgentStateEvent{}
		out := MarkSystemBootstrapEvent(ev)
		if _, ok := out.(protocol.AgentStateEvent); !ok {
			t.Errorf("expected passthrough type")
		}
	})
	t.Run("non-bootstrap unchanged", func(t *testing.T) {
		ev := protocol.LogEvent{Message: "hi", Event: protocol.Event{RuntimeMeta: protocol.RuntimeMeta{Engine: "claude"}}}
		out := MarkSystemBootstrapEvent(ev).(protocol.LogEvent)
		if out.RuntimeMeta.Source != "" {
			t.Errorf("expected source untouched, got %q", out.RuntimeMeta.Source)
		}
	})
	t.Run("bootstrap stamps source", func(t *testing.T) {
		ev := protocol.LogEvent{Message: "Using fast mode", Event: protocol.Event{RuntimeMeta: protocol.RuntimeMeta{Engine: "codex"}}}
		out := MarkSystemBootstrapEvent(ev).(protocol.LogEvent)
		if out.RuntimeMeta.Source != "system/bootstrap" {
			t.Errorf("expected stamped source, got %q", out.RuntimeMeta.Source)
		}
	})
}

func TestApplyEventToProjection_SessionStateEvent(t *testing.T) {
	out, applied := ApplyEventToProjection(data.ProjectionSnapshot{},
		protocol.SessionStateEvent{Event: protocol.Event{Type: "session_state", SessionID: "s1", Timestamp: time.Now()}, State: "stopped", Message: "已停止"})
	if !applied {
		t.Fatal("expected applied")
	}
	if len(out.LogEntries) == 0 || out.LogEntries[0].Kind != "system" {
		t.Errorf("expected system entry, got %+v", out.LogEntries)
	}
}

func TestApplyEventToProjection_AgentStateEvent(t *testing.T) {
	out, applied := ApplyEventToProjection(data.ProjectionSnapshot{},
		protocol.AgentStateEvent{
			Event: protocol.Event{
				Type:        "agent_state",
				SessionID:   "s1",
				RuntimeMeta: protocol.RuntimeMeta{ResumeSessionID: "r1", Command: "claude", CWD: "/tmp"},
			},
			State:   "THINKING",
			Command: "claude",
			Step:    "step-1",
			Tool:    "Read",
		})
	if !applied {
		t.Fatal("expected applied")
	}
	if out.Controller.State != "THINKING" {
		t.Errorf("state: %q", out.Controller.State)
	}
	if out.Controller.LastStep != "step-1" || out.Controller.LastTool != "Read" {
		t.Errorf("last step/tool: %q/%q", out.Controller.LastStep, out.Controller.LastTool)
	}
	if out.Runtime.ResumeSessionID != "r1" {
		t.Errorf("resume id: %q", out.Runtime.ResumeSessionID)
	}
	if out.Runtime.Command != "claude" {
		t.Errorf("runtime command: %q", out.Runtime.Command)
	}
}

func TestApplyEventToProjection_PromptRequestSetsWaitInput(t *testing.T) {
	out, applied := ApplyEventToProjection(data.ProjectionSnapshot{},
		protocol.PromptRequestEvent{
			Event:   protocol.Event{Type: "prompt_request", SessionID: "s1"},
			Message: "ok?",
		})
	if !applied {
		t.Fatal("expected applied")
	}
	if out.Controller.State != ControllerStateWaitInput {
		t.Errorf("state: %q", out.Controller.State)
	}
	// 由于没有 ResumeSessionID, lifecycle 是 "waiting_input"
	if out.Runtime.ClaudeLifecycle != "waiting_input" {
		t.Errorf("lifecycle: %q", out.Runtime.ClaudeLifecycle)
	}
}

func TestApplyEventToProjection_FileDiff(t *testing.T) {
	out, applied := ApplyEventToProjection(data.ProjectionSnapshot{},
		protocol.FileDiffEvent{
			Event: protocol.Event{Type: "file_diff", SessionID: "s1"},
			Path:  "main.go",
			Title: "edit",
			Diff:  "+ a",
		})
	if !applied {
		t.Fatal("expected applied")
	}
	if len(out.Diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(out.Diffs))
	}
	if !out.Diffs[0].PendingReview {
		t.Errorf("expected pending review")
	}
	if out.CurrentDiff == nil || out.CurrentDiff.Path != "main.go" {
		t.Errorf("current diff: %+v", out.CurrentDiff)
	}
}

func TestApplyEventToProjection_Error(t *testing.T) {
	out, applied := ApplyEventToProjection(data.ProjectionSnapshot{},
		protocol.ErrorEvent{
			Event:   protocol.Event{Type: "error", SessionID: "s1", Timestamp: time.Now()},
			Message: "oops",
			Stack:   "stack",
			Code:    "E1",
		})
	if !applied {
		t.Fatal("expected applied")
	}
	if out.LatestError == nil || out.LatestError.Message != "oops" {
		t.Errorf("latest error: %+v", out.LatestError)
	}
	if len(out.LogEntries) == 0 || out.LogEntries[0].Kind != "error" {
		t.Errorf("log entries: %+v", out.LogEntries)
	}
}

func TestApplyEventToProjection_StepUpdate(t *testing.T) {
	out, applied := ApplyEventToProjection(data.ProjectionSnapshot{},
		protocol.StepUpdateEvent{
			Event:   protocol.Event{Type: "step_update", SessionID: "s1", Timestamp: time.Now()},
			Message: "step",
			Tool:    "Read",
		})
	if !applied {
		t.Fatal("expected applied")
	}
	if out.CurrentStep == nil || out.CurrentStep.Tool != "Read" {
		t.Errorf("current step: %+v", out.CurrentStep)
	}
}

func TestApplyEventToProjection_TerminalStepDoesNotReplaceCurrentStep(t *testing.T) {
	previous := &data.SnapshotContext{Type: "step", Message: "Running command", Status: "running"}
	out, applied := ApplyEventToProjection(data.ProjectionSnapshot{CurrentStep: previous},
		protocol.StepUpdateEvent{
			Event:   protocol.Event{Type: "step_update", SessionID: "s1", Timestamp: time.Now()},
			Message: "Completed command",
			Status:  "done",
			Tool:    "commandExecution",
		})
	if !applied {
		t.Fatal("expected applied")
	}
	if out.CurrentStep == nil || out.CurrentStep.Message != "Running command" {
		t.Fatalf("terminal step should not replace current step, got %+v", out.CurrentStep)
	}
	if len(out.LogEntries) == 0 || out.LogEntries[len(out.LogEntries)-1].Kind != "step" {
		t.Fatalf("expected terminal step to remain in log entries, got %+v", out.LogEntries)
	}
}

func TestApplyEventToProjection_LogTerminalStartedAndFinished(t *testing.T) {
	exit := 0
	startedEv := protocol.LogEvent{
		Event: protocol.Event{
			Type:        "log",
			SessionID:   "s1",
			Timestamp:   time.Now(),
			RuntimeMeta: protocol.RuntimeMeta{ExecutionID: "e1"},
		},
		Message: "ls",
		Stream:  "stdout",
		Phase:   "started",
	}
	out, applied := ApplyEventToProjection(data.ProjectionSnapshot{}, startedEv)
	if !applied {
		t.Fatal("expected applied for started")
	}
	if len(out.TerminalExecutions) != 1 || out.TerminalExecutions[0].ExecutionID != "e1" {
		t.Errorf("terminal executions: %+v", out.TerminalExecutions)
	}

	finishedEv := protocol.LogEvent{
		Event: protocol.Event{
			Type:        "log",
			SessionID:   "s1",
			Timestamp:   time.Now(),
			RuntimeMeta: protocol.RuntimeMeta{ExecutionID: "e1"},
		},
		Message:  "done",
		Stream:   "stdout",
		Phase:    "finished",
		ExitCode: &exit,
	}
	out2, applied2 := ApplyEventToProjection(out, finishedEv)
	if !applied2 {
		t.Fatal("expected applied for finished")
	}
	if out2.TerminalExecutions[0].ExitCode == nil || *out2.TerminalExecutions[0].ExitCode != 0 {
		t.Errorf("expected exit code, got %+v", out2.TerminalExecutions[0])
	}
	if out2.TerminalExecutions[0].FinishedAt == "" {
		t.Errorf("expected finishedAt set")
	}
}

func TestApplyEventToProjection_LogVisibleAssistantReply(t *testing.T) {
	in := data.ProjectionSnapshot{}
	out, applied := ApplyEventToProjection(in, protocol.LogEvent{
		Event: protocol.Event{
			Type:        "log",
			SessionID:   "s1",
			Timestamp:   time.Now(),
			RuntimeMeta: protocol.RuntimeMeta{Engine: "claude"},
		},
		Message: "Hello world",
	})
	if !applied {
		t.Fatal("expected applied")
	}
	// markdown kind, controller idle
	if out.Controller.State != ControllerStateIdle {
		t.Errorf("state: %q", out.Controller.State)
	}
	if len(out.LogEntries) == 0 || out.LogEntries[len(out.LogEntries)-1].Kind != "markdown" {
		t.Errorf("expected markdown entry, got %+v", out.LogEntries)
	}
}

func TestApplyEventToProjection_UnknownEvent(t *testing.T) {
	type customEvent struct{}
	_, applied := ApplyEventToProjection(data.ProjectionSnapshot{}, customEvent{})
	if applied {
		t.Errorf("expected NOT applied for unknown event type")
	}
}

func TestAIStatusEventForBackendEvent_PromptRequestSetsWaiting(t *testing.T) {
	got, ok := AIStatusEventForBackendEvent("s1", nil, data.ProjectionSnapshot{},
		protocol.PromptRequestEvent{Event: protocol.Event{SessionID: "s1"}, Message: "ok?"})
	if !ok {
		t.Fatal("expected ok")
	}
	if got.Phase != "waiting_input" {
		t.Errorf("phase: %q", got.Phase)
	}
	if got.Visible {
		t.Errorf("expected not visible")
	}
}

func TestAIStatusEventForBackendEvent_AgentStateNonAIIgnored(t *testing.T) {
	_, ok := AIStatusEventForBackendEvent("s1", nil, data.ProjectionSnapshot{},
		protocol.AgentStateEvent{Event: protocol.Event{SessionID: "s1"}, State: "RUNNING", Command: "bash"})
	if ok {
		t.Error("expected no AI status event for non-AI command")
	}
}

func TestAIStatusEventForBackendEvent_LogVisibleSetsSettled(t *testing.T) {
	got, ok := AIStatusEventForBackendEvent("s1", nil,
		data.ProjectionSnapshot{Runtime: data.SessionRuntime{Engine: "claude"}},
		protocol.LogEvent{
			Event:   protocol.Event{SessionID: "s1", RuntimeMeta: protocol.RuntimeMeta{Engine: "claude"}},
			Message: "Hello world",
		})
	if !ok {
		t.Fatal("expected ok")
	}
	if got.Phase != "settled" {
		t.Errorf("phase: %q", got.Phase)
	}
}

func TestLogSnapshotContextFromEvent(t *testing.T) {
	t.Run("empty event returns nil", func(t *testing.T) {
		got := logSnapshotContextFromEvent(protocol.LogEvent{})
		if got != nil {
			t.Errorf("expected nil, got %+v", got)
		}
	})
	t.Run("with command builds ctx", func(t *testing.T) {
		got := logSnapshotContextFromEvent(protocol.LogEvent{
			Event: protocol.Event{
				Timestamp:   time.Now(),
				RuntimeMeta: protocol.RuntimeMeta{Command: "claude", Source: "claude/assistant"},
			},
			Message: "hi",
		})
		if got == nil {
			t.Fatal("expected non-nil")
		}
		if got.Command != "claude" || got.Source != "claude/assistant" {
			t.Errorf("got %+v", got)
		}
	})
}

func TestRemoveSupersededAssistantLogEntry(t *testing.T) {
	entries := []data.SnapshotLogEntry{
		{Kind: "markdown", Message: "你好世界", Stream: "stdout", ExecutionID: "e1"},
	}
	ev := protocol.LogEvent{
		Event:   protocol.Event{RuntimeMeta: protocol.RuntimeMeta{ExecutionID: "e1"}},
		Message: "你好世界 加更多",
		Stream:  "stdout",
	}
	got := removeSupersededAssistantLogEntry(entries, ev)
	if len(got) != 0 {
		t.Errorf("expected superseded prev removed, got %+v", got)
	}
}

func TestRemoveSupersededAssistantLogEntry_NoMatchKept(t *testing.T) {
	entries := []data.SnapshotLogEntry{
		{Kind: "markdown", Message: "完全不同", Stream: "stdout", ExecutionID: "e1"},
	}
	ev := protocol.LogEvent{
		Event:   protocol.Event{RuntimeMeta: protocol.RuntimeMeta{ExecutionID: "e1"}},
		Message: "你好世界",
		Stream:  "stdout",
	}
	got := removeSupersededAssistantLogEntry(entries, ev)
	if len(got) != 1 {
		t.Errorf("expected entry kept, got %+v", got)
	}
}
