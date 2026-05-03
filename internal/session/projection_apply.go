package session

import (
	"fmt"
	"strings"
	"time"

	"mobilevc/internal/data"
	"mobilevc/internal/protocol"
)

func MarkSystemBootstrapEvent(event any) any {
	logEvent, ok := event.(protocol.LogEvent)
	if !ok {
		return event
	}
	if !isSystemBootstrapLog(logEvent) {
		return event
	}
	logEvent.RuntimeMeta = protocol.MergeRuntimeMeta(logEvent.RuntimeMeta, protocol.RuntimeMeta{
		Source: "system/bootstrap",
	})
	return logEvent
}

func isSystemBootstrapLog(event protocol.LogEvent) bool {
	if runtimeEngineFromMeta(event.RuntimeMeta) != "codex" {
		return false
	}
	message := strings.TrimSpace(strings.ToLower(event.Message))
	if message == "" {
		return false
	}
	if strings.HasPrefix(message, "using ") && strings.Contains(message, " mode") {
		return true
	}
	if strings.Contains(message, "reasoning effort") ||
		strings.Contains(message, "model set to") ||
		strings.Contains(message, "how can i help you") ||
		strings.Contains(message, "what would you like to work on next") {
		return true
	}
	raw := strings.TrimSpace(event.Message)
	if strings.Contains(raw, "设置模型") ||
		strings.Contains(raw, "推理强度") ||
		strings.Contains(raw, "模型强度") ||
		strings.Contains(raw, "模型已设") ||
		strings.Contains(raw, "Codex 会话") {
		return true
	}
	return false
}

func IsVisibleAssistantReplyLog(event protocol.LogEvent) bool {
	if strings.EqualFold(strings.TrimSpace(event.Stream), "stderr") {
		return false
	}
	source := strings.TrimSpace(event.Source)
	if source == "system/bootstrap" {
		return false
	}
	if source == "claude/assistant" || source == "codex/assistant" {
		return strings.TrimSpace(event.Message) != ""
	}
	message := strings.TrimSpace(event.Message)
	if message == "" {
		return false
	}
	engine := runtimeEngineFromMeta(event.RuntimeMeta)
	if engine != "claude" && engine != "codex" {
		return false
	}
	lower := strings.ToLower(message)
	if strings.HasPrefix(lower, "output") || strings.HasPrefix(lower, "wall time:") {
		return false
	}
	if looksLikeMarkdownMessage(message) {
		return true
	}
	if strings.Contains(message, "\n") {
		return true
	}
	return !looksLikeTerminalLikeLogLine(message)
}

func looksLikeTerminalLikeLogLine(message string) bool {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	return strings.HasPrefix(lower, "$ ") ||
		strings.HasPrefix(lower, "# ") ||
		strings.HasPrefix(lower, "> ") ||
		strings.HasPrefix(lower, "at ") ||
		strings.Contains(lower, "fatal:") ||
		strings.Contains(lower, "error:") ||
		strings.Contains(lower, "traceback") ||
		strings.Contains(lower, "[info]") ||
		strings.Contains(lower, "[error]") ||
		strings.Contains(lower, "task :")
}

func runtimeEngineFromMeta(meta protocol.RuntimeMeta) string {
	engine := strings.TrimSpace(strings.ToLower(meta.Engine))
	if engine == "claude" || engine == "codex" {
		return engine
	}
	command := strings.TrimSpace(strings.ToLower(meta.Command))
	switch {
	case command == "claude", strings.HasPrefix(command, "claude "):
		return "claude"
	case command == "codex", strings.HasPrefix(command, "codex "):
		return "codex"
	default:
		if meta.ResumeSessionID != "" {
			return "claude"
		}
		return ""
	}
}

func ApplyEventToProjection(snapshot data.ProjectionSnapshot, event any) (data.ProjectionSnapshot, bool) {
	snapshot = NormalizeProjectionSnapshot(snapshot)
	switch e := event.(type) {
	case protocol.SessionStateEvent:
		if e.Message != "" {
			snapshot.LogEntries = append(snapshot.LogEntries, data.SnapshotLogEntry{Kind: "system", Message: e.Message, Timestamp: e.Timestamp.Format(time.RFC3339)})
		}
		return snapshot, true
	case protocol.LogEvent:
		phase := strings.TrimSpace(e.Phase)
		msg := strings.TrimSpace(e.Message)
		context := logSnapshotContextFromEvent(e)
		entry := data.SnapshotLogEntry{
			Kind:        "terminal",
			Message:     e.Message,
			Timestamp:   e.Timestamp.Format(time.RFC3339),
			Stream:      e.Stream,
			Text:        strings.TrimLeft(e.Message, "\r"),
			ExecutionID: e.ExecutionID,
			Phase:       phase,
			ExitCode:    e.ExitCode,
			Context:     context,
		}
		if phase == "started" {
			snapshot.TerminalExecutions = upsertTerminalExecution(snapshot.TerminalExecutions, data.TerminalExecution{
				ExecutionID: e.ExecutionID,
				Command:     firstNonEmptyString(e.Command, e.Message),
				CWD:         e.CWD,
				StartedAt:   e.Timestamp.Format(time.RFC3339),
			})
			snapshot.LogEntries = append(snapshot.LogEntries, entry)
			return snapshot, true
		}
		if phase == "finished" {
			snapshot.TerminalExecutions = updateTerminalExecution(snapshot.TerminalExecutions, e.ExecutionID, func(item *data.TerminalExecution) {
				if item.StartedAt == "" {
					item.StartedAt = e.Timestamp.Format(time.RFC3339)
				}
				item.FinishedAt = e.Timestamp.Format(time.RFC3339)
				item.ExitCode = e.ExitCode
			})
			snapshot.LogEntries = append(snapshot.LogEntries, entry)
			return snapshot, true
		}
		if msg == "" {
			return snapshot, false
		}
		if IsVisibleAssistantReplyLog(e) {
			snapshot.Controller.State = ControllerStateIdle
			snapshot.LogEntries = removeSupersededAssistantLogEntry(
				snapshot.LogEntries,
				e,
			)
			snapshot.LogEntries = append(snapshot.LogEntries, data.SnapshotLogEntry{Kind: "markdown", Message: e.Message, Timestamp: e.Timestamp.Format(time.RFC3339), Stream: e.Stream, ExecutionID: e.ExecutionID, Phase: phase, ExitCode: e.ExitCode, Context: context})
		} else {
			previousIndex := len(snapshot.LogEntries) - 1
			if previousIndex >= 0 && snapshot.LogEntries[previousIndex].Kind == "terminal" && snapshot.LogEntries[previousIndex].Stream == e.Stream && snapshot.LogEntries[previousIndex].ExecutionID == e.ExecutionID && snapshot.LogEntries[previousIndex].Phase == phase {
				prev := snapshot.LogEntries[previousIndex]
				if prev.Text != "" {
					prev.Text += "\n"
				}
				prev.Text += strings.TrimLeft(e.Message, "\r")
				prev.Timestamp = e.Timestamp.Format(time.RFC3339)
				if prev.Context == nil && context != nil {
					prev.Context = context
				}
				snapshot.LogEntries[previousIndex] = prev
			} else {
				snapshot.LogEntries = append(snapshot.LogEntries, entry)
			}
			stream := fallbackString(e.Stream, "stdout")
			if snapshot.RawTerminalByStream[stream] != "" {
				snapshot.RawTerminalByStream[stream] += "\n"
			}
			snapshot.RawTerminalByStream[stream] += strings.TrimLeft(e.Message, "\r")
			snapshot.TerminalExecutions = updateTerminalExecution(snapshot.TerminalExecutions, e.ExecutionID, func(item *data.TerminalExecution) {
				if item.ExecutionID == "" {
					item.ExecutionID = e.ExecutionID
				}
				if item.Command == "" {
					item.Command = e.Command
				}
				if item.CWD == "" {
					item.CWD = e.CWD
				}
				if item.StartedAt == "" {
					item.StartedAt = e.Timestamp.Format(time.RFC3339)
				}
				appendExecutionStream(item, stream, strings.TrimLeft(e.Message, "\r"))
			})
		}
		return snapshot, true
	case protocol.ErrorEvent:
		ctx := &data.SnapshotContext{ID: firstNonEmptyString(e.ContextID, fmt.Sprintf("error:%s", e.Timestamp.Format(time.RFC3339Nano))), Message: e.Message, Stack: e.Stack, Code: e.Code, TargetPath: firstNonEmptyString(e.TargetPath, e.RuntimeMeta.TargetPath), RelatedStep: e.Step, Command: e.Command, Timestamp: e.Timestamp.Format(time.RFC3339), Title: firstNonEmptyString(e.ContextTitle, e.Message)}
		snapshot.LatestError = ctx
		snapshot.LogEntries = append(snapshot.LogEntries, data.SnapshotLogEntry{Kind: "error", Context: ctx})
		return snapshot, true
	case protocol.StepUpdateEvent:
		ctx := &data.SnapshotContext{ID: firstNonEmptyString(e.ContextID, fmt.Sprintf("step:%s", e.Timestamp.Format(time.RFC3339Nano))), Type: "step", Message: e.Message, Status: e.Status, Target: e.Target, TargetPath: firstNonEmptyString(e.TargetPath, e.Target), Tool: e.Tool, Command: e.Command, Timestamp: e.Timestamp.Format(time.RFC3339), Title: firstNonEmptyString(e.ContextTitle, e.Message, "当前步骤")}
		if !isTerminalStepStatus(e.Status) && !isTerminalStepMessage(e.Message) {
			snapshot.CurrentStep = ctx
		}
		snapshot.LogEntries = append(snapshot.LogEntries, data.SnapshotLogEntry{Kind: "step", Context: ctx})
		return snapshot, true
	case protocol.FileDiffEvent:
		diff := DiffContext{ContextID: firstNonEmptyString(e.ContextID, e.Path, e.Title), Title: firstNonEmptyString(e.Title, e.ContextTitle, "最近改动"), Path: firstNonEmptyString(e.Path, e.TargetPath), Diff: e.Diff, Lang: e.Lang, PendingReview: true, ExecutionID: e.ExecutionID, GroupID: firstNonEmptyString(e.GroupID, e.ExecutionID, e.ContextID, e.Path), GroupTitle: firstNonEmptyString(e.GroupTitle, e.ContextTitle, e.Title), ReviewStatus: "pending"}
		snapshot.Diffs = upsertSnapshotDiff(snapshot.Diffs, diff)
		snapshot.ReviewGroups = RebuildReviewGroups(snapshot.Diffs)
		activeGroup := PickActiveReviewGroup(snapshot.ReviewGroups)
		snapshot.ActiveReviewGroup = activeGroup
		active := PickActiveSnapshotDiff(snapshot.Diffs)
		if strings.TrimSpace(active.ContextID+active.Path+active.Title) != "" {
			snapshot.CurrentDiff = &active
		}
		snapshot.LogEntries = append(snapshot.LogEntries, data.SnapshotLogEntry{Kind: "diff", Context: &data.SnapshotContext{ID: diff.ContextID, Path: diff.Path, Title: diff.Title, Diff: diff.Diff, Lang: diff.Lang, PendingReview: diff.PendingReview, Timestamp: e.Timestamp.Format(time.RFC3339), Source: e.Source, SkillName: e.SkillName, ExecutionID: diff.ExecutionID, GroupID: diff.GroupID, GroupTitle: diff.GroupTitle, ReviewStatus: diff.ReviewStatus}})
		return snapshot, true
	case protocol.AgentStateEvent:
		snapshot.Controller.State = ControllerState(e.State)
		snapshot.Controller.CurrentCommand = firstNonEmptyString(e.Command, snapshot.Controller.CurrentCommand)
		snapshot.Controller.LastStep = firstNonEmptyString(e.Step, snapshot.Controller.LastStep)
		snapshot.Controller.LastTool = firstNonEmptyString(e.Tool, snapshot.Controller.LastTool)
		snapshot.Controller.ActiveMeta = protocol.MergeRuntimeMeta(snapshot.Controller.ActiveMeta, e.RuntimeMeta)
		snapshot.Runtime.ResumeSessionID = firstNonEmptyString(e.RuntimeMeta.ResumeSessionID, snapshot.Runtime.ResumeSessionID)
		snapshot.Runtime.Command = firstNonEmptyString(e.RuntimeMeta.Command, snapshot.Runtime.Command, snapshot.Controller.CurrentCommand)
		snapshot.Runtime.Engine = firstNonEmptyString(e.RuntimeMeta.Engine, snapshot.Runtime.Engine)
		snapshot.Runtime.CWD = firstNonEmptyString(e.RuntimeMeta.CWD, snapshot.Runtime.CWD)
		snapshot.Runtime.PermissionMode = firstNonEmptyString(e.RuntimeMeta.PermissionMode, snapshot.Runtime.PermissionMode)
		snapshot.Runtime.ClaudeLifecycle = NormalizeProjectionLifecycle(firstNonEmptyString(e.RuntimeMeta.ClaudeLifecycle, snapshot.Runtime.ClaudeLifecycle), firstNonEmptyString(e.RuntimeMeta.ResumeSessionID, snapshot.Runtime.ResumeSessionID))
		return snapshot, true
	case protocol.PromptRequestEvent:
		snapshot.Controller.State = ControllerStateWaitInput
		snapshot.Controller.ActiveMeta = protocol.MergeRuntimeMeta(snapshot.Controller.ActiveMeta, e.RuntimeMeta)
		snapshot.Runtime.ResumeSessionID = firstNonEmptyString(e.RuntimeMeta.ResumeSessionID, snapshot.Runtime.ResumeSessionID)
		snapshot.Runtime.Command = firstNonEmptyString(e.RuntimeMeta.Command, snapshot.Runtime.Command, snapshot.Controller.CurrentCommand)
		snapshot.Runtime.Engine = firstNonEmptyString(e.RuntimeMeta.Engine, snapshot.Runtime.Engine)
		snapshot.Runtime.CWD = firstNonEmptyString(e.RuntimeMeta.CWD, snapshot.Runtime.CWD)
		snapshot.Runtime.PermissionMode = firstNonEmptyString(e.RuntimeMeta.PermissionMode, snapshot.Runtime.PermissionMode)
		snapshot.Runtime.ClaudeLifecycle = NormalizeProjectionLifecycle(firstNonEmptyString(e.RuntimeMeta.ClaudeLifecycle, "waiting_input", snapshot.Runtime.ClaudeLifecycle), firstNonEmptyString(e.RuntimeMeta.ResumeSessionID, snapshot.Runtime.ResumeSessionID))
		return snapshot, true
	default:
		return snapshot, false
	}
}

func AIStatusEventForBackendEvent(sessionID string, svc *Service, projection data.ProjectionSnapshot, event any) (protocol.AIStatusEvent, bool) {
	switch e := event.(type) {
	case protocol.AgentStateEvent:
		if !isAIStatusContext(e.Command, e.RuntimeMeta, projection) {
			return protocol.AIStatusEvent{}, false
		}
		return buildAIStatusEvent(sessionID, e.State, e.AwaitInput, e.Step, e.Tool, e.Command, e.RuntimeMeta, projection)
	case protocol.TaskSnapshotEvent:
		if !isAIStatusContext(e.Command, e.RuntimeMeta, projection) {
			return protocol.AIStatusEvent{}, false
		}
		return buildAIStatusEventFromSnapshot(sessionID, e, projection)
	case protocol.PromptRequestEvent, protocol.InteractionRequestEvent:
		return protocol.NewAIStatusEvent(sessionID, false, "", "waiting_input", protocol.RuntimeMeta{}), true
	case protocol.LogEvent:
		if !isAIStatusContext(e.Command, e.RuntimeMeta, projection) {
			return protocol.AIStatusEvent{}, false
		}
		if IsVisibleAssistantReplyLog(e) {
			// 流式输出期间不能让单条 LogEvent 把状态球打灭：
			// ApplyEventToProjection 在可见回复时会就地把 projection.Controller.State
			// 改写成 Idle（仅是值副本，不影响 svc 中的真实运行时状态）。
			// 因此这里同时检查值副本与真实运行时控制器，只要任意一处仍在忙碌，
			// 就抑制 settled 事件——状态球的关闭交给 AgentStateEvent 切到 IDLE/WAIT_INPUT 时统一管理。
			projectionState := strings.TrimSpace(strings.ToUpper(string(projection.Controller.State)))
			if IsBusyRuntimeState(projectionState) {
				return protocol.AIStatusEvent{}, false
			}
			if svc != nil {
				runtimeState := strings.TrimSpace(strings.ToUpper(string(svc.ControllerSnapshot().State)))
				if IsBusyRuntimeState(runtimeState) {
					return protocol.AIStatusEvent{}, false
				}
			}
			return protocol.NewAIStatusEvent(sessionID, false, "", "settled", e.RuntimeMeta), true
		}
	case protocol.SessionStateEvent:
		projection = NormalizeProjectionSnapshot(projection)
		meta := projection.Controller.ActiveMeta
		state := strings.TrimSpace(strings.ToUpper(string(projection.Controller.State)))
		step := projection.Controller.LastStep
		tool := projection.Controller.LastTool
		command := projection.Controller.CurrentCommand
		if svc != nil {
			controller := svc.ControllerSnapshot()
			meta = protocol.MergeRuntimeMeta(meta, controller.ActiveMeta)
			if runtimeState := strings.TrimSpace(strings.ToUpper(string(controller.State))); IsBusyRuntimeState(runtimeState) {
				state = runtimeState
				step = firstNonEmptyString(controller.LastStep, step)
				tool = firstNonEmptyString(controller.LastTool, tool)
				command = firstNonEmptyString(controller.CurrentCommand, command)
			}
		}
		if state == "" || !IsBusyRuntimeState(state) {
			state = e.State
		}
		if !isAIStatusContext(command, meta, projection) {
			return protocol.AIStatusEvent{}, false
		}
		return buildAIStatusEvent(sessionID, state, false, step, tool, command, meta, projection)
	}
	return protocol.AIStatusEvent{}, false
}

func isAIStatusContext(command string, meta protocol.RuntimeMeta, projection data.ProjectionSnapshot) bool {
	if isAICommand(command) ||
		isAICommand(meta.Command) ||
		isAICommand(projection.Controller.CurrentCommand) ||
		isAICommand(projection.Runtime.Command) {
		return true
	}
	switch strings.TrimSpace(strings.ToLower(firstNonEmptyString(
		meta.Engine,
		projection.Controller.ActiveMeta.Engine,
		projection.Runtime.Engine,
	))) {
	case "claude", "codex", "gemini":
		return true
	default:
		return false
	}
}

func buildAIStatusEventFromSnapshot(sessionID string, snapshot protocol.TaskSnapshotEvent, projection data.ProjectionSnapshot) (protocol.AIStatusEvent, bool) {
	projection = NormalizeProjectionSnapshot(projection)
	state := strings.TrimSpace(strings.ToUpper(snapshot.State))
	step := snapshot.Step
	tool := snapshot.Tool
	command := snapshot.Command
	meta := snapshot.RuntimeMeta
	if !snapshot.AwaitInput && !IsBusyRuntimeState(state) {
		projectedState := strings.TrimSpace(strings.ToUpper(string(projection.Controller.State)))
		if IsBusyRuntimeState(projectedState) {
			state = projectedState
			step = firstNonEmptyString(projection.Controller.LastStep, step)
			tool = firstNonEmptyString(projection.Controller.LastTool, tool)
			command = firstNonEmptyString(projection.Controller.CurrentCommand, command)
			meta = protocol.MergeRuntimeMeta(meta, projection.Controller.ActiveMeta)
		}
	}
	return buildAIStatusEvent(sessionID, state, snapshot.AwaitInput, step, tool, command, meta, projection)
}

func buildAIStatusEvent(sessionID, state string, awaitInput bool, step, tool, command string, meta protocol.RuntimeMeta, projection data.ProjectionSnapshot) (protocol.AIStatusEvent, bool) {
	state = strings.TrimSpace(strings.ToUpper(state))
	if awaitInput || state == string(ControllerStateWaitInput) {
		return protocol.NewAIStatusEvent(sessionID, false, "", "waiting_input", meta), true
	}
	phase := strings.ToLower(state)
	if phase == "" {
		phase = "idle"
	}
	if !IsBusyRuntimeState(state) && state != "RUNNING" {
		return protocol.NewAIStatusEvent(sessionID, false, "", phase, meta), true
	}
	label := aiStatusLabelFromState(state, step, tool, command, meta, projection)
	return protocol.NewAIStatusEvent(sessionID, true, label, phase, meta), true
}

func aiStatusLabelFromState(state, step, tool, command string, meta protocol.RuntimeMeta, projection data.ProjectionSnapshot) string {
	if step = strings.TrimSpace(step); step != "" {
		if !isTerminalStepMessage(step) {
			return step
		}
	}
	verb := aiStatusVerbForTool(tool)
	target := pathBase(firstNonEmptyString(meta.TargetPath, meta.Target, projection.Controller.ActiveMeta.TargetPath))
	if verb != "" && target != "" {
		return verb + " · " + target
	}
	if verb != "" {
		return verb
	}
	toolLabel := normalizeToolLabel(tool)
	if toolLabel != "" && state == string(ControllerStateRunningTool) {
		return "执行中 · " + toolLabel
	}
	if head := commandHead(command); head != "" && state == string(ControllerStateRunningTool) {
		return "执行中 · " + head
	}
	switch state {
	case "RECOVERING":
		return "恢复中"
	case string(ControllerStateRunningTool):
		return "执行中"
	case "RUNNING":
		return "运行中"
	default:
		return "思考中"
	}
}

func aiStatusVerbForTool(tool string) string {
	switch strings.ToLower(strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return r
		}
		return -1
	}, strings.TrimSpace(tool))) {
	case "read":
		return "正在读取"
	case "write":
		return "正在写入"
	case "edit":
		return "正在修改"
	case "bash":
		return "正在执行命令"
	case "grep":
		return "正在搜索"
	case "glob":
		return "正在查找文件"
	case "webfetch":
		return "正在抓取网页"
	case "websearch":
		return "正在联网搜索"
	case "agent":
		return "正在派发子代理"
	case "skill":
		return "正在调用 skill"
	default:
		return ""
	}
}

func normalizeToolLabel(tool string) string {
	trimmed := strings.TrimSpace(tool)
	if trimmed == "" {
		return ""
	}
	switch strings.ToLower(trimmed) {
	case "read":
		return "Read"
	case "write":
		return "Write"
	case "edit":
		return "Edit"
	case "bash":
		return "Bash"
	default:
		return trimmed
	}
}

func pathBase(path string) string {
	normalized := strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	if normalized == "" {
		return ""
	}
	parts := strings.Split(normalized, "/")
	return parts[len(parts)-1]
}

func logSnapshotContextFromEvent(event protocol.LogEvent) *data.SnapshotContext {
	command := firstNonEmptyString(event.Command, event.RuntimeMeta.Command)
	source := strings.TrimSpace(event.Source)
	skillName := strings.TrimSpace(event.SkillName)
	if command == "" && source == "" && skillName == "" && strings.TrimSpace(event.ExecutionID) == "" {
		return nil
	}
	return &data.SnapshotContext{
		ID:          firstNonEmptyString(event.ContextID, fmt.Sprintf("log:%s", event.Timestamp.Format(time.RFC3339Nano))),
		Command:     command,
		Title:       firstNonEmptyString(event.ContextTitle, event.Message),
		Timestamp:   event.Timestamp.Format(time.RFC3339),
		Source:      source,
		SkillName:   skillName,
		ExecutionID: event.ExecutionID,
	}
}

func removeSupersededAssistantLogEntry(entries []data.SnapshotLogEntry, event protocol.LogEvent) []data.SnapshotLogEntry {
	message := strings.TrimSpace(event.Message)
	if message == "" || len(entries) == 0 {
		return entries
	}
	normalizedMessage := normalizeAssistantReplyForDedupe(message)
	eventStream := strings.TrimSpace(event.Stream)
	eventExecutionID := strings.TrimSpace(event.ExecutionID)
	remove := make(map[int]bool)
	found := false
	for index := len(entries) - 1; index >= 0; index-- {
		entry := entries[index]
		if eventExecutionID != "" && strings.TrimSpace(entry.ExecutionID) != "" && strings.TrimSpace(entry.ExecutionID) != eventExecutionID {
			if found {
				break
			}
			continue
		}
		if eventStream != "" && strings.TrimSpace(entry.Stream) != "" && strings.TrimSpace(entry.Stream) != eventStream {
			if found {
				break
			}
			continue
		}
		if entry.Kind != "terminal" && entry.Kind != "markdown" {
			if found {
				break
			}
			continue
		}
		previous := strings.TrimSpace(firstNonEmptyString(entry.Message, entry.Text))
		if previous == "" {
			continue
		}
		normalizedPrevious := normalizeAssistantReplyForDedupe(previous)
		if normalizedPrevious == normalizedMessage ||
			strings.HasPrefix(normalizedMessage, normalizedPrevious) ||
			strings.Contains(normalizedMessage, normalizedPrevious) {
			remove[index] = true
			found = true
			continue
		}
		break
	}
	if len(remove) == 0 {
		return entries
	}
	next := entries[:0]
	for index, entry := range entries {
		if remove[index] {
			continue
		}
		next = append(next, entry)
	}
	return next
}

func normalizeAssistantReplyForDedupe(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

func upsertTerminalExecution(items []data.TerminalExecution, next data.TerminalExecution) []data.TerminalExecution {
	if strings.TrimSpace(next.ExecutionID) == "" {
		return items
	}
	for i := range items {
		if items[i].ExecutionID == next.ExecutionID {
			if next.Command != "" {
				items[i].Command = next.Command
			}
			if next.CWD != "" {
				items[i].CWD = next.CWD
			}
			if next.StartedAt != "" {
				items[i].StartedAt = next.StartedAt
			}
			if next.FinishedAt != "" {
				items[i].FinishedAt = next.FinishedAt
			}
			if next.ExitCode != nil {
				items[i].ExitCode = next.ExitCode
			}
			if next.Stdout != "" {
				appendExecutionStream(&items[i], "stdout", next.Stdout)
			}
			if next.Stderr != "" {
				appendExecutionStream(&items[i], "stderr", next.Stderr)
			}
			return items
		}
	}
	return append(items, next)
}

func updateTerminalExecution(items []data.TerminalExecution, executionID string, mutate func(item *data.TerminalExecution)) []data.TerminalExecution {
	if strings.TrimSpace(executionID) == "" {
		return items
	}
	for i := range items {
		if items[i].ExecutionID == executionID {
			mutate(&items[i])
			return items
		}
	}
	item := data.TerminalExecution{ExecutionID: executionID}
	mutate(&item)
	return append(items, item)
}

func appendExecutionStream(item *data.TerminalExecution, stream string, text string) {
	if item == nil || text == "" {
		return
	}
	switch stream {
	case "stderr":
		if item.Stderr != "" {
			item.Stderr += "\n"
		}
		item.Stderr += text
	default:
		if item.Stdout != "" {
			item.Stdout += "\n"
		}
		item.Stdout += text
	}
}

func upsertSnapshotDiff(diffs []DiffContext, diff DiffContext) []DiffContext {
	for i := range diffs {
		item := diffs[i]
		if (strings.TrimSpace(diff.ContextID) != "" && strings.TrimSpace(item.ContextID) == strings.TrimSpace(diff.ContextID)) ||
			(strings.TrimSpace(diff.Path) != "" && strings.TrimSpace(item.Path) == strings.TrimSpace(diff.Path)) {
			diffs[i] = diff
			return diffs
		}
	}
	return append(diffs, diff)
}

func looksLikeMarkdownMessage(message string) bool {
	if strings.TrimSpace(message) == "" {
		return false
	}
	return strings.Contains(message, "```") || strings.Contains(message, "# ") || strings.Contains(message, "## ") || strings.Contains(message, "- ") || len(message) > 180
}

func fallbackString(value, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return value
}
