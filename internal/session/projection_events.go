package session

import (
	"strings"
	"time"

	"mobilevc/internal/data"
	"mobilevc/internal/protocol"
)

type DeltaCursorSnapshot struct {
	LatestCursor int64
}

func ToProtocolSummary(item data.SessionSummary) protocol.SessionSummary {
	return protocol.SessionSummary{
		ID:              item.ID,
		Title:           item.Title,
		CreatedAt:       item.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       item.UpdatedAt.Format(time.RFC3339),
		LastPreview:     item.LastPreview,
		EntryCount:      item.EntryCount,
		Source:          item.Source,
		External:        item.External,
		Ownership:       item.Ownership,
		ExecutionActive: item.ExecutionActive,
		Runtime: protocol.RuntimeMeta{
			ResumeSessionID: item.Runtime.ResumeSessionID,
			Command:         item.Runtime.Command,
			Engine:          item.Runtime.Engine,
			CWD:             item.Runtime.CWD,
			PermissionMode:  item.Runtime.PermissionMode,
			ClaudeLifecycle: item.Runtime.ClaudeLifecycle,
			Source:          item.Runtime.Source,
		},
	}
}

func ToProtocolSessionContext(ctx data.SessionContext) protocol.SessionContext {
	return protocol.SessionContext{
		EnabledSkillNames: append([]string(nil), ctx.EnabledSkillNames...),
		EnabledMemoryIDs:  append([]string(nil), ctx.EnabledMemoryIDs...),
	}
}

func ToProtocolCatalogMetadata(meta data.CatalogMetadata) protocol.CatalogMetadata {
	lastSyncedAt := ""
	if !meta.LastSyncedAt.IsZero() {
		lastSyncedAt = meta.LastSyncedAt.Format(time.RFC3339)
	}
	return protocol.CatalogMetadata{
		Domain:        string(meta.Domain),
		SourceOfTruth: string(meta.SourceOfTruth),
		SyncState:     string(meta.SyncState),
		DriftDetected: meta.DriftDetected,
		LastSyncedAt:  lastSyncedAt,
		VersionToken:  meta.VersionToken,
		LastError:     meta.LastError,
	}
}

func HistoryContextFromSnapshot(ctx *data.SnapshotContext) *protocol.HistoryContext {
	if ctx == nil {
		return nil
	}
	return &protocol.HistoryContext{
		ID:            ctx.ID,
		Type:          ctx.Type,
		Message:       ctx.Message,
		Status:        ctx.Status,
		Target:        ctx.Target,
		TargetPath:    ctx.TargetPath,
		Tool:          ctx.Tool,
		Command:       ctx.Command,
		Timestamp:     ctx.Timestamp,
		Title:         ctx.Title,
		Stack:         ctx.Stack,
		Code:          ctx.Code,
		RelatedStep:   ctx.RelatedStep,
		Path:          ctx.Path,
		Diff:          ctx.Diff,
		Lang:          ctx.Lang,
		PendingReview: ctx.PendingReview,
		Source:        ctx.Source,
		SkillName:     ctx.SkillName,
		ExecutionID:   ctx.ExecutionID,
		GroupID:       ctx.GroupID,
		GroupTitle:    ctx.GroupTitle,
		ReviewStatus:  ctx.ReviewStatus,
	}
}

func ProtocolReviewFile(file ReviewFile) protocol.ReviewFile {
	return protocol.ReviewFile{
		ID:            file.ContextID,
		Path:          file.Path,
		Title:         file.Title,
		Diff:          file.Diff,
		Lang:          file.Lang,
		PendingReview: file.PendingReview,
		ReviewStatus:  file.ReviewStatus,
		ExecutionID:   file.ExecutionID,
	}
}

func ProtocolReviewGroup(group *ReviewGroup) *protocol.ReviewGroup {
	if group == nil {
		return nil
	}
	files := make([]protocol.ReviewFile, 0, len(group.Files))
	for _, file := range group.Files {
		files = append(files, ProtocolReviewFile(file))
	}
	return &protocol.ReviewGroup{
		ID:            group.ID,
		Title:         group.Title,
		ExecutionID:   group.ExecutionID,
		PendingReview: group.PendingReview,
		ReviewStatus:  group.ReviewStatus,
		CurrentFileID: group.CurrentFileID,
		CurrentPath:   group.CurrentPath,
		PendingCount:  group.PendingCount,
		AcceptedCount: group.AcceptedCount,
		RevertedCount: group.RevertedCount,
		RevisedCount:  group.RevisedCount,
		Files:         files,
	}
}

func ProtocolReviewGroups(groups []ReviewGroup) []protocol.ReviewGroup {
	if len(groups) == 0 {
		return nil
	}
	result := make([]protocol.ReviewGroup, 0, len(groups))
	for _, group := range groups {
		item := ProtocolReviewGroup(&group)
		if item != nil {
			result = append(result, *item)
		}
	}
	return result
}

func ProtocolDiffContext(diff *DiffContext) *protocol.HistoryContext {
	if diff == nil {
		return nil
	}
	return &protocol.HistoryContext{
		ID:            diff.ContextID,
		Type:          "diff",
		Path:          diff.Path,
		Title:         diff.Title,
		Diff:          diff.Diff,
		Lang:          diff.Lang,
		PendingReview: diff.PendingReview,
		ExecutionID:   diff.ExecutionID,
		GroupID:       diff.GroupID,
		GroupTitle:    diff.GroupTitle,
		ReviewStatus:  diff.ReviewStatus,
	}
}

func ProtocolDiffContexts(diffs []DiffContext) []protocol.HistoryContext {
	if len(diffs) == 0 {
		return nil
	}
	result := make([]protocol.HistoryContext, 0, len(diffs))
	for _, diff := range diffs {
		ctx := ProtocolDiffContext(&diff)
		if ctx != nil {
			result = append(result, *ctx)
		}
	}
	return result
}

func ReviewStateEventFromProjection(sessionID string, projection data.ProjectionSnapshot) protocol.ReviewStateEvent {
	projection = NormalizeProjectionSnapshot(projection)
	return protocol.NewReviewStateEvent(
		sessionID,
		ProtocolReviewGroups(projection.ReviewGroups),
		ProtocolReviewGroup(projection.ActiveReviewGroup),
	)
}

func ApplyReviewDecisionToProjection(snapshot data.ProjectionSnapshot, reviewEvent protocol.ReviewDecisionRequestEvent, decision string, currentDiff DiffContext) data.ProjectionSnapshot {
	snapshot = NormalizeProjectionSnapshot(snapshot)
	targetContextID := firstNonEmptyString(reviewEvent.ContextID, currentDiff.ContextID)
	targetPath := firstNonEmptyString(reviewEvent.TargetPath, currentDiff.Path)
	targetExecutionID := firstNonEmptyString(reviewEvent.ExecutionID, currentDiff.ExecutionID)
	targetGroupID := firstNonEmptyString(reviewEvent.GroupID, reviewEvent.ExecutionID, currentDiff.GroupID, targetContextID, targetPath)
	targetGroupTitle := firstNonEmptyString(reviewEvent.GroupTitle, currentDiff.GroupTitle, currentDiff.Title)
	reviewStatus := reviewStatusFromDecision(decision)
	pending := decision == "revise"

	for i := range snapshot.Diffs {
		item := &snapshot.Diffs[i]
		if !snapshotDiffMatches(*item, targetContextID, targetPath) {
			continue
		}
		item.PendingReview = pending
		item.ReviewStatus = reviewStatus
		if item.GroupID == "" {
			item.GroupID = targetGroupID
		}
		if item.GroupTitle == "" {
			item.GroupTitle = targetGroupTitle
		}
		if item.ExecutionID == "" {
			item.ExecutionID = targetExecutionID
		}
	}

	snapshot.ReviewGroups = RebuildReviewGroups(snapshot.Diffs)
	if active := PickActiveReviewGroup(snapshot.ReviewGroups); active != nil {
		snapshot.ActiveReviewGroup = active
	}
	activeDiff := PickActiveSnapshotDiff(snapshot.Diffs)
	if strings.TrimSpace(activeDiff.ContextID+activeDiff.Path+activeDiff.Title) != "" {
		snapshot.CurrentDiff = &activeDiff
	}
	return snapshot
}

func reviewStatusFromDecision(decision string) string {
	switch strings.TrimSpace(strings.ToLower(decision)) {
	case "accept":
		return "accepted"
	case "revert":
		return "reverted"
	case "revise":
		return "revised"
	default:
		return "pending"
	}
}

func snapshotDiffMatches(item DiffContext, contextID, targetPath string) bool {
	if strings.TrimSpace(contextID) != "" && strings.TrimSpace(item.ContextID) == strings.TrimSpace(contextID) {
		return true
	}
	if strings.TrimSpace(targetPath) != "" && strings.TrimSpace(item.Path) == strings.TrimSpace(targetPath) {
		return true
	}
	return false
}

func SessionHistoryEventFromRecord(record data.SessionRecord, runtimeAlive bool) protocol.SessionHistoryEvent {
	projection := NormalizeProjectionSnapshot(record.Projection)
	entries := protocolLogEntries(projection.LogEntries)
	executions := protocolTerminalExecutions(projection.TerminalExecutions)
	resumeMeta := protocol.RuntimeMeta{
		ResumeSessionID: projection.Runtime.ResumeSessionID,
		Command:         projection.Runtime.Command,
		Engine:          projection.Runtime.Engine,
		CWD:             projection.Runtime.CWD,
		PermissionMode:  projection.Runtime.PermissionMode,
		ClaudeLifecycle: NormalizeProjectionLifecycle(projection.Runtime.ClaudeLifecycle, projection.Runtime.ResumeSessionID),
	}
	return protocol.NewSessionHistoryEvent(
		record.Summary.ID,
		ToProtocolSummary(record.Summary),
		entries,
		ProtocolDiffContexts(projection.Diffs),
		ProtocolDiffContext(projection.CurrentDiff),
		ProtocolReviewGroups(projection.ReviewGroups),
		ProtocolReviewGroup(projection.ActiveReviewGroup),
		HistoryContextFromSnapshot(projection.CurrentStep),
		HistoryContextFromSnapshot(projection.LatestError),
		projection.RawTerminalByStream,
		executions,
		ToProtocolSessionContext(projection.SessionContext),
		ToProtocolCatalogMetadata(projection.SkillCatalogMeta),
		ToProtocolCatalogMetadata(projection.MemoryCatalogMeta),
		strings.TrimSpace(resumeMeta.ResumeSessionID) != "",
		runtimeAlive,
		resumeMeta,
	)
}

func SessionDeltaEventFromRecord(record data.SessionRecord, known protocol.SessionDeltaKnown, cursor DeltaCursorSnapshot, runtimeAlive bool) protocol.SessionDeltaEvent {
	projection := NormalizeProjectionSnapshot(record.Projection)
	latestCursor := cursor.LatestCursor
	allEntries := protocolLogEntries(projection.LogEntries)
	startLog := known.LogEntryCount
	if latestCursor > 0 && known.EventCursor >= latestCursor {
		startLog = len(allEntries)
	}
	if startLog < 0 || startLog > len(allEntries) {
		startLog = 0
	}
	appendEntries := append([]protocol.HistoryLogEntry(nil), allEntries[startLog:]...)

	allDiffs := ProtocolDiffContexts(projection.Diffs)
	startDiff := known.DiffCount
	if latestCursor > 0 && known.EventCursor >= latestCursor {
		startDiff = len(allDiffs)
	}
	if startDiff < 0 || startDiff > len(allDiffs) {
		startDiff = 0
	}
	upsertDiffs := append([]protocol.HistoryContext(nil), allDiffs[startDiff:]...)
	if startDiff == len(allDiffs) && (projection.CurrentDiff != nil || len(projection.ReviewGroups) > 0 || projection.ActiveReviewGroup != nil) {
		upsertDiffs = append([]protocol.HistoryContext(nil), allDiffs...)
	}

	rawTerminalByStream := make(map[string]string)
	stdout := projection.RawTerminalByStream["stdout"]
	stderr := projection.RawTerminalByStream["stderr"]
	stdoutStart := known.TerminalStdoutLength
	if latestCursor > 0 && known.EventCursor >= latestCursor {
		stdoutStart = len(stdout)
	}
	if stdoutStart < 0 || stdoutStart > len(stdout) {
		stdoutStart = 0
	}
	stderrStart := known.TerminalStderrLength
	if latestCursor > 0 && known.EventCursor >= latestCursor {
		stderrStart = len(stderr)
	}
	if stderrStart < 0 || stderrStart > len(stderr) {
		stderrStart = 0
	}
	if stdoutStart < len(stdout) {
		rawTerminalByStream["stdout"] = stdout[stdoutStart:]
	}
	if stderrStart < len(stderr) {
		rawTerminalByStream["stderr"] = stderr[stderrStart:]
	}

	executions := protocolTerminalExecutions(projection.TerminalExecutions)
	resumeMeta := protocol.RuntimeMeta{
		ResumeSessionID: projection.Runtime.ResumeSessionID,
		Command:         projection.Runtime.Command,
		Engine:          projection.Runtime.Engine,
		CWD:             projection.Runtime.CWD,
		PermissionMode:  projection.Runtime.PermissionMode,
		ClaudeLifecycle: NormalizeProjectionLifecycle(projection.Runtime.ClaudeLifecycle, projection.Runtime.ResumeSessionID),
	}
	latest := protocol.SessionDeltaKnown{
		EventCursor:            latestCursor,
		LogEntryCount:          len(allEntries),
		DiffCount:              len(allDiffs),
		TerminalExecutionCount: len(executions),
		TerminalStdoutLength:   len(stdout),
		TerminalStderrLength:   len(stderr),
	}
	return protocol.NewSessionDeltaEvent(
		record.Summary.ID,
		ToProtocolSummary(record.Summary),
		known,
		latest,
		appendEntries,
		upsertDiffs,
		ProtocolDiffContext(projection.CurrentDiff),
		ProtocolReviewGroups(projection.ReviewGroups),
		ProtocolReviewGroup(projection.ActiveReviewGroup),
		HistoryContextFromSnapshot(projection.CurrentStep),
		HistoryContextFromSnapshot(projection.LatestError),
		rawTerminalByStream,
		executions,
		ToProtocolSessionContext(projection.SessionContext),
		ToProtocolCatalogMetadata(projection.SkillCatalogMeta),
		ToProtocolCatalogMetadata(projection.MemoryCatalogMeta),
		strings.TrimSpace(resumeMeta.ResumeSessionID) != "",
		runtimeAlive,
		resumeMeta,
		(startLog == 0 && known.LogEntryCount > 0) || (stdoutStart == 0 && known.TerminalStdoutLength > 0) || (stderrStart == 0 && known.TerminalStderrLength > 0),
	)
}

func RestoredAgentStateEventFromRecord(record data.SessionRecord, hasActiveRunner bool, externalNativeActive bool) *protocol.AgentStateEvent {
	projection := NormalizeProjectionSnapshot(record.Projection)
	runtimeMeta := protocol.MergeRuntimeMeta(projection.Controller.ActiveMeta, protocol.RuntimeMeta{
		ResumeSessionID: firstNonEmptyString(
			projection.Controller.ResumeSession,
			projection.Controller.ActiveMeta.ResumeSessionID,
			projection.Runtime.ResumeSessionID,
		),
		Command: firstNonEmptyString(
			projection.Controller.CurrentCommand,
			projection.Controller.ActiveMeta.Command,
			projection.Runtime.Command,
		),
		Engine: firstNonEmptyString(
			projection.Controller.ActiveMeta.Engine,
			projection.Runtime.Engine,
		),
		CWD: firstNonEmptyString(
			projection.Controller.ActiveMeta.CWD,
			projection.Runtime.CWD,
		),
		PermissionMode: firstNonEmptyString(
			projection.Controller.ActiveMeta.PermissionMode,
			projection.Runtime.PermissionMode,
			"auto",
		),
		ClaudeLifecycle: NormalizeProjectionLifecycle(
			firstNonEmptyString(
				projection.Controller.ClaudeLifecycle,
				projection.Controller.ActiveMeta.ClaudeLifecycle,
				projection.Runtime.ClaudeLifecycle,
			),
			firstNonEmptyString(
				projection.Controller.ResumeSession,
				projection.Controller.ActiveMeta.ResumeSessionID,
				projection.Runtime.ResumeSessionID,
			),
		),
	})
	state := projection.Controller.State
	downgradedStaleRunning := false
	if state == "" {
		switch strings.TrimSpace(runtimeMeta.ClaudeLifecycle) {
		case "waiting_input":
			state = ControllerStateWaitInput
		case "starting", "active":
			state = ControllerStateThinking
		case "resumable":
			state = ControllerStateIdle
		}
	}
	if !hasActiveRunner && !externalNativeActive {
		switch state {
		case ControllerStateThinking, ControllerStateRunningTool:
			if strings.TrimSpace(runtimeMeta.ResumeSessionID) != "" {
				state = ControllerStateWaitInput
				runtimeMeta.ClaudeLifecycle = "waiting_input"
			} else {
				state = ControllerStateIdle
				runtimeMeta.ClaudeLifecycle = ""
				downgradedStaleRunning = true
			}
		}
	}
	if state == "" {
		return nil
	}
	awaitInput := state == ControllerStateWaitInput
	currentStepMessage := ""
	if projection.CurrentStep != nil && !isTerminalStepStatus(projection.CurrentStep.Status) && !isTerminalStepMessage(projection.CurrentStep.Message) {
		currentStepMessage = projection.CurrentStep.Message
	}
	message := firstNonEmptyString(projection.Controller.LastStep, currentStepMessage)
	if downgradedStaleRunning {
		message = ""
	}
	switch state {
	case ControllerStateWaitInput:
		message = firstNonEmptyString(message, "等待输入")
	case ControllerStateThinking:
		message = firstNonEmptyString(message, "思考中")
	case ControllerStateRunningTool:
		message = firstNonEmptyString(message, "执行工具中")
	case ControllerStateIdle:
		if strings.TrimSpace(runtimeMeta.ResumeSessionID) == "" &&
			strings.TrimSpace(runtimeMeta.ClaudeLifecycle) == "" {
			return nil
		}
		if strings.TrimSpace(runtimeMeta.ClaudeLifecycle) == "" &&
			strings.TrimSpace(runtimeMeta.ResumeSessionID) != "" {
			runtimeMeta.ClaudeLifecycle = "waiting_input"
		}
		message = firstNonEmptyString(message, "会话已暂停，可继续对话")
	default:
		if strings.TrimSpace(message) == "" {
			return nil
		}
	}
	event := protocol.NewAgentStateEvent(
		record.Summary.ID,
		string(state),
		message,
		awaitInput,
		runtimeMeta.Command,
		projection.Controller.LastStep,
		projection.Controller.LastTool,
	)
	event.RuntimeMeta = runtimeMeta
	return &event
}

func protocolLogEntries(items []data.SnapshotLogEntry) []protocol.HistoryLogEntry {
	entries := make([]protocol.HistoryLogEntry, 0, len(items))
	for _, entry := range items {
		entries = append(entries, protocol.HistoryLogEntry{
			Kind:        entry.Kind,
			Message:     entry.Message,
			Label:       entry.Label,
			Timestamp:   entry.Timestamp,
			Stream:      entry.Stream,
			Text:        entry.Text,
			ExecutionID: entry.ExecutionID,
			Phase:       entry.Phase,
			ExitCode:    entry.ExitCode,
			Context:     HistoryContextFromSnapshot(entry.Context),
		})
	}
	return entries
}

func protocolTerminalExecutions(items []data.TerminalExecution) []protocol.TerminalExecution {
	executions := make([]protocol.TerminalExecution, 0, len(items))
	for _, item := range items {
		executions = append(executions, protocol.TerminalExecution{
			ExecutionID: item.ExecutionID,
			Command:     item.Command,
			CWD:         item.CWD,
			StartedAt:   item.StartedAt,
			FinishedAt:  item.FinishedAt,
			ExitCode:    item.ExitCode,
			Stdout:      item.Stdout,
			Stderr:      item.Stderr,
		})
	}
	return executions
}
