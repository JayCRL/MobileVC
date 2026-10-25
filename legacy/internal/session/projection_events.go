package session

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"mobilevc/internal/data"
	"mobilevc/internal/protocol"
)

type DeltaCursorSnapshot struct {
	LatestCursor int64
}

const (
	terminalRangeDefaultLimit         = 64 * 1024
	terminalRangeMaxLimit             = 512 * 1024
	diffPageDefaultLimit              = 25
	diffPageMaxLimit                  = 100
	terminalExecutionPageDefaultLimit = 50
	terminalExecutionPageMaxLimit     = 100
)

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
			ResumeSessionID:   item.Runtime.ResumeSessionID,
			Command:           item.Runtime.Command,
			Engine:            item.Runtime.Engine,
			CodexSandboxMode:  item.Runtime.CodexSandboxMode,
			CWD:               item.Runtime.CWD,
			PermissionMode:    item.Runtime.PermissionMode,
			ClaudeSessionUUID: item.ClaudeSessionUUID,
			ClaudeLifecycle:   item.Runtime.ClaudeLifecycle,
			Source:            item.Runtime.Source,
			SourcePath:        item.Runtime.SourcePath,
			SourceSize:        item.Runtime.SourceSize,
			SourceModUnixNS:   item.Runtime.SourceModUnixNS,
			SourceEntryCount:  item.Runtime.SourceEntryCount,
		},
	}
}

func ToProtocolSessionContext(ctx data.SessionContext) protocol.SessionContext {
	return protocol.SessionContext{
		EnabledSkillNames: append([]string(nil), ctx.EnabledSkillNames...),
		EnabledMemoryIDs:  append([]string(nil), ctx.EnabledMemoryIDs...),
	}
}

func ToProtocolContextWindowUsage(usage data.ContextWindowUsage) protocol.ContextWindowUsage {
	return protocol.NormalizeContextWindowUsage(protocol.ContextWindowUsage{
		TokensUsed: usage.TokensUsed,
		TokenLimit: usage.TokenLimit,
	})
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
		Trigger:       ctx.Trigger,
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

func ApplyAutoReviewAcceptanceToProjection(snapshot data.ProjectionSnapshot) data.ProjectionSnapshot {
	snapshot = NormalizeProjectionSnapshot(snapshot)
	for i := range snapshot.Diffs {
		if !snapshot.Diffs[i].PendingReview {
			continue
		}
		snapshot.Diffs[i].PendingReview = false
		snapshot.Diffs[i].ReviewStatus = "accepted"
	}
	snapshot.ReviewGroups = RebuildReviewGroups(snapshot.Diffs)
	if active := PickActiveReviewGroup(snapshot.ReviewGroups); active != nil {
		snapshot.ActiveReviewGroup = active
	} else {
		snapshot.ActiveReviewGroup = nil
	}
	activeDiff := PickActiveSnapshotDiff(snapshot.Diffs)
	if strings.TrimSpace(activeDiff.ContextID+activeDiff.Path+activeDiff.Title) != "" {
		snapshot.CurrentDiff = &activeDiff
	} else {
		snapshot.CurrentDiff = nil
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
	return SessionHistoryWindowEventFromRecord(record, runtimeAlive, 0)
}

func SessionHistoryWindowEventFromRecord(record data.SessionRecord, runtimeAlive bool, limit int) protocol.SessionHistoryEvent {
	projection := NormalizeProjectionSnapshot(record.Projection)
	window := snapshotLogEntryWindow(projection.LogEntries, len(projection.LogEntries), limit)
	return sessionHistoryWindowEventFromProjection(record, projection, window, runtimeAlive, DeltaCursorSnapshot{})
}

func SessionHistoryWindowEventFromRecordWithPayloadLimit(record data.SessionRecord, runtimeAlive bool, limit int, maxPayloadBytes int) protocol.SessionHistoryEvent {
	return SessionHistoryWindowEventFromRecordWithCursorAndPayloadLimit(record, runtimeAlive, limit, DeltaCursorSnapshot{}, maxPayloadBytes)
}

func SessionHistoryWindowEventFromRecordWithCursorAndPayloadLimit(record data.SessionRecord, runtimeAlive bool, limit int, cursor DeltaCursorSnapshot, maxPayloadBytes int) protocol.SessionHistoryEvent {
	projection := NormalizeProjectionSnapshot(record.Projection)
	window := snapshotLogEntryWindow(projection.LogEntries, len(projection.LogEntries), limit)
	event := lightweightSessionHistoryWindowEventFromProjection(record, projection, window, runtimeAlive, cursor)
	if fitsProtocolPayloadBudget(event, maxPayloadBytes) {
		return event
	}
	return shrinkSessionHistoryEventToPayloadBudget(event, window, maxPayloadBytes)
}

func SessionHistoryWindowEventFromWindowWithCursorAndPayloadLimit(window data.SessionHistoryWindow, runtimeAlive bool, cursor DeltaCursorSnapshot, maxPayloadBytes int) protocol.SessionHistoryEvent {
	record, projection, snapshotWindow := projectionAndSnapshotWindowFromSessionHistoryWindow(window)
	event := lightweightSessionHistoryWindowEventFromProjection(record, projection, snapshotWindow, runtimeAlive, cursor)
	if fitsProtocolPayloadBudget(event, maxPayloadBytes) {
		return event
	}
	return shrinkSessionHistoryEventToPayloadBudget(event, snapshotWindow, maxPayloadBytes)
}

func sessionHistoryWindowEventFromProjection(record data.SessionRecord, projection data.ProjectionSnapshot, window snapshotLogWindow, runtimeAlive bool, cursor DeltaCursorSnapshot) protocol.SessionHistoryEvent {
	executions := protocolTerminalExecutions(projection.TerminalExecutions)
	resumeMeta := runtimeMetaFromProjection(projection)
	event := protocol.NewSessionHistoryWindowEvent(
		record.Summary.ID,
		ToProtocolSummary(record.Summary),
		protocolLogEntries(window.entries),
		window.start,
		window.total,
		ProtocolDiffContexts(projection.Diffs),
		ProtocolDiffContext(projection.CurrentDiff),
		ProtocolReviewGroups(projection.ReviewGroups),
		ProtocolReviewGroup(projection.ActiveReviewGroup),
		HistoryContextFromSnapshot(projection.CurrentStep),
		HistoryContextFromSnapshot(projection.LatestError),
		projection.RawTerminalByStream,
		executions,
		ToProtocolContextWindowUsage(projection.ContextWindowUsage),
		ToProtocolSessionContext(projection.SessionContext),
		ToProtocolCatalogMetadata(projection.SkillCatalogMeta),
		ToProtocolCatalogMetadata(projection.MemoryCatalogMeta),
		strings.TrimSpace(resumeMeta.ResumeSessionID) != "",
		runtimeAlive,
		resumeMeta,
	)
	event.Latest = sessionDeltaKnownFromProjectionWithLogCount(projection, cursor, window.total)
	return event
}

func lightweightSessionHistoryWindowEventFromProjection(record data.SessionRecord, projection data.ProjectionSnapshot, window snapshotLogWindow, runtimeAlive bool, cursor DeltaCursorSnapshot) protocol.SessionHistoryEvent {
	event := sessionHistoryWindowEventFromProjection(record, projection, window, runtimeAlive, cursor)
	event.Diffs = nil
	event.CurrentDiff = nil
	event.ReviewGroups = nil
	event.ActiveReviewGroup = nil
	event.RawTerminalByStream = nil
	event.TerminalExecutions = nil
	return event
}

func SessionHistoryPageEventFromRecord(record data.SessionRecord, before, limit int) protocol.SessionHistoryPageEvent {
	projection := NormalizeProjectionSnapshot(record.Projection)
	window := snapshotLogEntryWindow(projection.LogEntries, before, limit)
	return sessionHistoryPageEventFromProjection(record, projection, window)
}

func SessionHistoryPageEventFromRecordWithPayloadLimit(record data.SessionRecord, before, limit int, maxPayloadBytes int) protocol.SessionHistoryPageEvent {
	projection := NormalizeProjectionSnapshot(record.Projection)
	window := snapshotLogEntryWindow(projection.LogEntries, before, limit)
	event := sessionHistoryPageEventFromProjection(record, projection, window)
	if fitsProtocolPayloadBudget(event, maxPayloadBytes) {
		return event
	}
	return shrinkSessionHistoryPageEventToPayloadBudget(record, projection, window, maxPayloadBytes)
}

func SessionHistoryPageEventFromWindowWithPayloadLimit(window data.SessionHistoryWindow, maxPayloadBytes int) protocol.SessionHistoryPageEvent {
	record, projection, snapshotWindow := projectionAndSnapshotWindowFromSessionHistoryWindow(window)
	if snapshotLogWindowExceedsPayloadBudget(snapshotWindow, maxPayloadBytes) {
		return shrinkSessionHistoryPageEventToPayloadBudget(record, projection, snapshotWindow, maxPayloadBytes)
	}
	event := sessionHistoryPageEventFromProjection(record, projection, snapshotWindow)
	if fitsProtocolPayloadBudget(event, maxPayloadBytes) {
		return event
	}
	return shrinkSessionHistoryPageEventToPayloadBudget(record, projection, snapshotWindow, maxPayloadBytes)
}

func projectionAndSnapshotWindowFromSessionHistoryWindow(window data.SessionHistoryWindow) (data.SessionRecord, data.ProjectionSnapshot, snapshotLogWindow) {
	record := window.Record
	projection := NormalizeProjectionSnapshot(record.Projection)
	total := window.LogEntryTotal
	if total < 0 {
		total = 0
	}
	entries := append([]data.SnapshotLogEntry(nil), window.LogEntries...)
	start := window.LogEntryStart
	if start < 0 {
		start = 0
	}
	if total < start+len(entries) {
		total = start + len(entries)
	}
	projection.LogEntries = nil
	record.Projection = projection
	record.Summary.EntryCount = total
	return record, projection, snapshotLogWindow{
		entries: entries,
		start:   start,
		total:   total,
	}
}

func SessionTerminalRangeEventFromRecordWithPayloadLimit(record data.SessionRecord, stream string, start, limit int, cursor DeltaCursorSnapshot, maxPayloadBytes int) (protocol.SessionTerminalRangeEvent, error) {
	projection := NormalizeProjectionSnapshot(record.Projection)
	normalizedStream, err := normalizeTerminalStream(stream)
	if err != nil {
		return protocol.SessionTerminalRangeEvent{}, err
	}
	content := projection.RawTerminalByStream[normalizedStream]
	total := len(content)
	if start < 0 {
		start = 0
	}
	if start > total {
		start = total
	}
	limit = boundedPositiveLimit(limit, terminalRangeDefaultLimit, terminalRangeMaxLimit)
	end := start + limit
	if end > total {
		end = total
	}
	start, end = terminalUTF8ByteRange(content, start, end)
	event := protocol.NewSessionTerminalRangeEvent(
		record.Summary.ID,
		normalizedStream,
		start,
		end,
		total,
		content[start:end],
		sessionDeltaKnownFromProjection(projection, cursor),
	)
	if fitsProtocolPayloadBudget(event, maxPayloadBytes) {
		return event, nil
	}
	event.Content = ""
	event.End = start
	event.PayloadLimited = true
	event.PayloadLimitReason = "payload_budget_exceeded"
	event.SuggestedLimit = suggestedTerminalRangeLimit(maxPayloadBytes)
	return event, nil
}

func SessionTerminalRangeEventFromRangeWithPayloadLimit(item data.SessionTerminalRange, cursor DeltaCursorSnapshot, maxPayloadBytes int) protocol.SessionTerminalRangeEvent {
	event := protocol.NewSessionTerminalRangeEvent(
		item.Record.Summary.ID,
		item.Stream,
		item.Start,
		item.End,
		item.Total,
		item.Content,
		sessionDeltaKnownFromCounts(item.Latest, cursor),
	)
	if fitsProtocolPayloadBudget(event, maxPayloadBytes) {
		return event
	}
	event.Content = ""
	event.End = item.Start
	event.PayloadLimited = true
	event.PayloadLimitReason = "payload_budget_exceeded"
	event.SuggestedLimit = suggestedTerminalRangeLimit(maxPayloadBytes)
	return event
}

func terminalUTF8ByteRange(content string, start, end int) (int, int) {
	total := len(content)
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	if end > total {
		end = total
	}
	for start < total && !utf8.RuneStart(content[start]) {
		start++
	}
	for end > start && end < total && !utf8.RuneStart(content[end]) {
		end--
	}
	if end < start {
		end = start
	}
	if end == start && start < total {
		_, size := utf8.DecodeRuneInString(content[start:])
		end = start + size
	}
	return start, end
}

func SessionDiffPageEventFromRecordWithPayloadLimit(record data.SessionRecord, before, limit int, cursor DeltaCursorSnapshot, maxPayloadBytes int) protocol.SessionDiffPageEvent {
	projection := NormalizeProjectionSnapshot(record.Projection)
	limit = boundedPositiveLimit(limit, diffPageDefaultLimit, diffPageMaxLimit)
	window := diffWindowForProjection(projection.Diffs, before, limit)
	event := sessionDiffPageEventFromProjection(record, projection, window, cursor)
	if fitsProtocolPayloadBudget(event, maxPayloadBytes) {
		return event
	}
	markSessionDiffPagePayloadLimited(&event, "payload_budget_exceeded")
	event = shrinkSessionDiffPageToPayloadBudget(event, window, projection, cursor, maxPayloadBytes)
	if fitsProtocolPayloadBudget(event, maxPayloadBytes) {
		return event
	}
	event.Diffs = stripHistoryContextBodies(event.Diffs)
	event.CurrentDiff = stripHistoryContextBody(event.CurrentDiff)
	event.ReviewGroups = stripReviewGroupBodies(event.ReviewGroups)
	if event.ActiveReviewGroup != nil {
		stripped := stripReviewGroupBodies([]protocol.ReviewGroup{*event.ActiveReviewGroup})
		if len(stripped) > 0 {
			event.ActiveReviewGroup = &stripped[0]
		}
	}
	return event
}

func SessionDiffPageEventFromPageWithPayloadLimit(page data.SessionDiffPage, cursor DeltaCursorSnapshot, maxPayloadBytes int) protocol.SessionDiffPageEvent {
	projection := NormalizeProjectionSnapshot(page.Record.Projection)
	window := snapshotDiffWindow{
		entries: append([]DiffContext(nil), page.Diffs...),
		start:   page.DiffStart,
	}
	event := sessionDiffPageEventFromProjection(page.Record, projection, window, cursor)
	event.DiffTotal = page.DiffTotal
	event.HasMoreBefore = page.DiffStart > 0
	event.Latest = sessionDeltaKnownFromCounts(page.Latest, cursor)
	if fitsProtocolPayloadBudget(event, maxPayloadBytes) {
		return event
	}
	markSessionDiffPagePayloadLimited(&event, "payload_budget_exceeded")
	event = shrinkSessionDiffPageToPayloadBudget(event, window, projection, cursor, maxPayloadBytes)
	event.DiffTotal = page.DiffTotal
	event.Latest = sessionDeltaKnownFromCounts(page.Latest, cursor)
	if fitsProtocolPayloadBudget(event, maxPayloadBytes) {
		return event
	}
	event.Diffs = stripHistoryContextBodies(event.Diffs)
	event.CurrentDiff = stripHistoryContextBody(event.CurrentDiff)
	event.ReviewGroups = stripReviewGroupBodies(event.ReviewGroups)
	if event.ActiveReviewGroup != nil {
		stripped := stripReviewGroupBodies([]protocol.ReviewGroup{*event.ActiveReviewGroup})
		if len(stripped) > 0 {
			event.ActiveReviewGroup = &stripped[0]
		}
	}
	return event
}

func SessionTerminalExecutionPageEventFromRecordWithPayloadLimit(record data.SessionRecord, before, limit int, includeOutput bool, cursor DeltaCursorSnapshot, maxPayloadBytes int) protocol.SessionTerminalExecutionPageEvent {
	projection := NormalizeProjectionSnapshot(record.Projection)
	limit = boundedPositiveLimit(limit, terminalExecutionPageDefaultLimit, terminalExecutionPageMaxLimit)
	window := terminalExecutionWindowForProjection(projection.TerminalExecutions, before, limit)
	event := sessionTerminalExecutionPageEventFromProjection(record, projection, window, includeOutput, cursor)
	if fitsProtocolPayloadBudget(event, maxPayloadBytes) {
		return event
	}
	markSessionTerminalExecutionPagePayloadLimited(&event, "payload_budget_exceeded")
	event = shrinkSessionTerminalExecutionPageToPayloadBudget(event, window, includeOutput, projection, cursor, maxPayloadBytes)
	if fitsProtocolPayloadBudget(event, maxPayloadBytes) {
		return event
	}
	event.TerminalExecutions = stripTerminalExecutionOutput(event.TerminalExecutions)
	event.IncludeOutput = false
	return event
}

func SessionTerminalExecutionPageEventFromPageWithPayloadLimit(page data.SessionTerminalExecutionPage, cursor DeltaCursorSnapshot, maxPayloadBytes int) protocol.SessionTerminalExecutionPageEvent {
	projection := NormalizeProjectionSnapshot(page.Record.Projection)
	window := snapshotTerminalExecutionWindow{
		entries: append([]data.TerminalExecution(nil), page.TerminalExecutions...),
		start:   page.ExecutionStart,
	}
	event := sessionTerminalExecutionPageEventFromProjection(page.Record, projection, window, page.IncludeOutput, cursor)
	event.ExecutionTotal = page.ExecutionTotal
	event.HasMoreBefore = page.ExecutionStart > 0
	event.Latest = sessionDeltaKnownFromCounts(page.Latest, cursor)
	if fitsProtocolPayloadBudget(event, maxPayloadBytes) {
		return event
	}
	markSessionTerminalExecutionPagePayloadLimited(&event, "payload_budget_exceeded")
	event = shrinkSessionTerminalExecutionPageToPayloadBudget(event, window, page.IncludeOutput, projection, cursor, maxPayloadBytes)
	event.ExecutionTotal = page.ExecutionTotal
	event.Latest = sessionDeltaKnownFromCounts(page.Latest, cursor)
	if fitsProtocolPayloadBudget(event, maxPayloadBytes) {
		return event
	}
	event.TerminalExecutions = stripTerminalExecutionOutput(event.TerminalExecutions)
	event.IncludeOutput = false
	return event
}

func sessionHistoryPageEventFromProjection(record data.SessionRecord, projection data.ProjectionSnapshot, window snapshotLogWindow) protocol.SessionHistoryPageEvent {
	resumeMeta := runtimeMetaFromProjection(projection)
	return protocol.NewSessionHistoryPageEvent(
		record.Summary.ID,
		protocolLogEntries(window.entries),
		window.start,
		window.total,
		resumeMeta,
		sessionDeltaKnownFromProjectionWithLogCount(projection, DeltaCursorSnapshot{}, window.total),
	)
}

func sessionDiffPageEventFromProjection(record data.SessionRecord, projection data.ProjectionSnapshot, window snapshotDiffWindow, cursor DeltaCursorSnapshot) protocol.SessionDiffPageEvent {
	pageDiffs := protocolDiffContextsFromDiffs(window.entries)
	currentDiff := protocolCurrentDiffForPage(projection.CurrentDiff, window.entries)
	reviewGroups := protocolReviewGroupsFromDiffs(window.entries)
	activeReviewGroup := protocolActiveReviewGroupForPage(projection.ActiveReviewGroup, reviewGroups)
	return protocol.NewSessionDiffPageEvent(
		record.Summary.ID,
		pageDiffs,
		window.start,
		len(projection.Diffs),
		reviewGroups,
		activeReviewGroup,
		currentDiff,
		sessionDeltaKnownFromProjection(projection, cursor),
	)
}

func sessionTerminalExecutionPageEventFromProjection(record data.SessionRecord, projection data.ProjectionSnapshot, window snapshotTerminalExecutionWindow, includeOutput bool, cursor DeltaCursorSnapshot) protocol.SessionTerminalExecutionPageEvent {
	executions := protocolTerminalExecutions(window.entries)
	if !includeOutput {
		executions = stripTerminalExecutionOutput(executions)
	}
	return protocol.NewSessionTerminalExecutionPageEvent(
		record.Summary.ID,
		executions,
		window.start,
		len(projection.TerminalExecutions),
		includeOutput,
		sessionDeltaKnownFromProjection(projection, cursor),
	)
}

func shrinkSessionHistoryEventToPayloadBudget(event protocol.SessionHistoryEvent, window snapshotLogWindow, maxPayloadBytes int) protocol.SessionHistoryEvent {
	markSessionHistoryPayloadLimited(&event, "payload_budget_exceeded")
	event = shrinkSessionHistoryEntriesToPayloadBudget(event, window, maxPayloadBytes)
	if fitsProtocolPayloadBudget(event, maxPayloadBytes) {
		return event
	}
	event.CurrentStep = nil
	event.LatestError = nil
	event = shrinkSessionHistoryEntriesToPayloadBudget(event, window, maxPayloadBytes)
	if fitsProtocolPayloadBudget(event, maxPayloadBytes) {
		return event
	}
	event.ContextWindowUsage = protocol.ContextWindowUsage{}
	event.SessionContext = protocol.SessionContext{}
	event.SkillCatalogMeta = protocol.CatalogMetadata{}
	event.MemoryCatalogMeta = protocol.CatalogMetadata{}
	event = shrinkSessionHistoryEntriesToPayloadBudget(event, window, maxPayloadBytes)
	if fitsProtocolPayloadBudget(event, maxPayloadBytes) {
		return event
	}
	event.LogEntries = nil
	event.LogEntryStart = window.start + len(window.entries)
	event.HasMoreBefore = event.LogEntryStart > 0
	event.RawTerminalByStream = nil
	event.TerminalExecutions = nil
	event.Diffs = nil
	event.CurrentDiff = nil
	event.ReviewGroups = nil
	event.ActiveReviewGroup = nil
	event.CurrentStep = nil
	event.LatestError = nil
	event.ContextWindowUsage = protocol.ContextWindowUsage{}
	event.SessionContext = protocol.SessionContext{}
	event.SkillCatalogMeta = protocol.CatalogMetadata{}
	event.MemoryCatalogMeta = protocol.CatalogMetadata{}
	return event
}

func shrinkSessionHistoryPageEventToPayloadBudget(record data.SessionRecord, projection data.ProjectionSnapshot, window snapshotLogWindow, maxPayloadBytes int) protocol.SessionHistoryPageEvent {
	event := sessionHistoryPageEventFromProjection(record, projection, window)
	markSessionHistoryPagePayloadLimited(&event, "payload_budget_exceeded")
	event = shrinkSessionHistoryPageEntriesToPayloadBudget(event, window, maxPayloadBytes)
	if fitsProtocolPayloadBudget(event, maxPayloadBytes) {
		return event
	}
	event.ResumeRuntimeMeta = protocol.RuntimeMeta{}
	return shrinkSessionHistoryPageEntriesToPayloadBudget(event, window, maxPayloadBytes)
}

func shrinkSessionHistoryEntriesToPayloadBudget(event protocol.SessionHistoryEvent, window snapshotLogWindow, maxPayloadBytes int) protocol.SessionHistoryEvent {
	entries, start := shrinkSnapshotWindowEntries(window, func(candidate snapshotLogWindow) bool {
		candidateEvent := event
		candidateEvent.LogEntries = protocolLogEntries(candidate.entries)
		candidateEvent.LogEntryStart = candidate.start
		candidateEvent.HasMoreBefore = candidate.start > 0
		return fitsProtocolPayloadBudget(candidateEvent, maxPayloadBytes)
	})
	event.LogEntries = protocolLogEntries(entries)
	event.LogEntryStart = start
	event.HasMoreBefore = start > 0
	return event
}

func shrinkSessionHistoryPageEntriesToPayloadBudget(event protocol.SessionHistoryPageEvent, window snapshotLogWindow, maxPayloadBytes int) protocol.SessionHistoryPageEvent {
	entries, start := shrinkSnapshotWindowEntries(window, func(candidate snapshotLogWindow) bool {
		candidateEvent := event
		candidateEvent.LogEntries = protocolLogEntries(candidate.entries)
		candidateEvent.LogEntryStart = candidate.start
		candidateEvent.HasMoreBefore = candidate.start > 0
		return fitsProtocolPayloadBudget(candidateEvent, maxPayloadBytes)
	})
	event.LogEntries = protocolLogEntries(entries)
	event.LogEntryStart = start
	event.HasMoreBefore = start > 0
	return event
}

func shrinkSnapshotWindowEntries(window snapshotLogWindow, fits func(snapshotLogWindow) bool) ([]data.SnapshotLogEntry, int) {
	if len(window.entries) == 0 {
		return nil, window.start
	}
	before := window.start + len(window.entries)
	low, high := 0, len(window.entries)
	best := 0
	for low <= high {
		count := (low + high) / 2
		start := before - count
		candidate := snapshotLogWindow{
			entries: append([]data.SnapshotLogEntry(nil), window.entries[len(window.entries)-count:]...),
			start:   start,
			total:   window.total,
		}
		if fits(candidate) {
			best = count
			low = count + 1
			continue
		}
		high = count - 1
	}
	start := before - best
	if best == 0 {
		return nil, start
	}
	return append([]data.SnapshotLogEntry(nil), window.entries[len(window.entries)-best:]...), start
}

func snapshotLogWindowExceedsPayloadBudget(window snapshotLogWindow, maxPayloadBytes int) bool {
	if maxPayloadBytes <= 0 {
		return false
	}
	total := 0
	for _, entry := range window.entries {
		total += len(entry.Kind) + len(entry.Message) + len(entry.Label) + len(entry.Timestamp)
		total += len(entry.Stream) + len(entry.Text) + len(entry.ExecutionID) + len(entry.Phase)
		if entry.Context != nil {
			total += snapshotContextPayloadBytes(*entry.Context)
		}
		for _, attachment := range entry.Attachments {
			total += len(attachment.ID) + len(attachment.Kind) + len(attachment.Name) + len(attachment.MIMEType)
			total += len(attachment.Path) + len(attachment.PreviewStatus) + len(attachment.Source)
		}
		if total > maxPayloadBytes {
			return true
		}
	}
	return false
}

func snapshotContextPayloadBytes(ctx data.SnapshotContext) int {
	return len(ctx.ID) + len(ctx.Type) + len(ctx.Message) + len(ctx.Status) + len(ctx.Trigger) +
		len(ctx.Target) + len(ctx.TargetPath) + len(ctx.Tool) + len(ctx.Command) +
		len(ctx.Timestamp) + len(ctx.Title) + len(ctx.Stack) + len(ctx.Code) +
		len(ctx.RelatedStep) + len(ctx.Path) + len(ctx.Diff) + len(ctx.Lang) +
		len(ctx.Source) + len(ctx.SkillName) + len(ctx.ExecutionID) + len(ctx.GroupID) +
		len(ctx.GroupTitle) + len(ctx.ReviewStatus)
}

func fitsProtocolPayloadBudget(event any, maxPayloadBytes int) bool {
	if maxPayloadBytes <= 0 {
		return true
	}
	encoded, err := json.Marshal(event)
	return err == nil && len(encoded) <= maxPayloadBytes
}

func markSessionHistoryPayloadLimited(event *protocol.SessionHistoryEvent, reason string) {
	event.PayloadLimited = true
	event.PayloadLimitReason = strings.TrimSpace(reason)
}

func markSessionHistoryPagePayloadLimited(event *protocol.SessionHistoryPageEvent, reason string) {
	event.PayloadLimited = true
	event.PayloadLimitReason = strings.TrimSpace(reason)
}

func markSessionDiffPagePayloadLimited(event *protocol.SessionDiffPageEvent, reason string) {
	event.PayloadLimited = true
	event.PayloadLimitReason = strings.TrimSpace(reason)
}

func markSessionTerminalExecutionPagePayloadLimited(event *protocol.SessionTerminalExecutionPageEvent, reason string) {
	event.PayloadLimited = true
	event.PayloadLimitReason = strings.TrimSpace(reason)
}

type snapshotLogWindow struct {
	entries []data.SnapshotLogEntry
	start   int
	total   int
}

type snapshotDiffWindow struct {
	entries []DiffContext
	start   int
}

type snapshotTerminalExecutionWindow struct {
	entries []data.TerminalExecution
	start   int
}

func snapshotLogEntryWindow(entries []data.SnapshotLogEntry, before, limit int) snapshotLogWindow {
	total := len(entries)
	if before <= 0 || before > total {
		before = total
	}
	if limit <= 0 || limit >= before {
		return snapshotLogWindow{
			entries: append([]data.SnapshotLogEntry(nil), entries[:before]...),
			start:   0,
			total:   total,
		}
	}
	start := before - limit
	return snapshotLogWindow{
		entries: append([]data.SnapshotLogEntry(nil), entries[start:before]...),
		start:   start,
		total:   total,
	}
}

func diffWindowForProjection(entries []DiffContext, before, limit int) snapshotDiffWindow {
	total := len(entries)
	if before <= 0 || before > total {
		before = total
	}
	if limit <= 0 || limit >= before {
		return snapshotDiffWindow{
			entries: append([]DiffContext(nil), entries[:before]...),
			start:   0,
		}
	}
	start := before - limit
	return snapshotDiffWindow{
		entries: append([]DiffContext(nil), entries[start:before]...),
		start:   start,
	}
}

func terminalExecutionWindowForProjection(entries []data.TerminalExecution, before, limit int) snapshotTerminalExecutionWindow {
	total := len(entries)
	if before <= 0 || before > total {
		before = total
	}
	if limit <= 0 || limit >= before {
		return snapshotTerminalExecutionWindow{
			entries: append([]data.TerminalExecution(nil), entries[:before]...),
			start:   0,
		}
	}
	start := before - limit
	return snapshotTerminalExecutionWindow{
		entries: append([]data.TerminalExecution(nil), entries[start:before]...),
		start:   start,
	}
}

func shrinkSessionDiffPageToPayloadBudget(event protocol.SessionDiffPageEvent, window snapshotDiffWindow, projection data.ProjectionSnapshot, cursor DeltaCursorSnapshot, maxPayloadBytes int) protocol.SessionDiffPageEvent {
	entries, start := shrinkDiffWindowEntries(window, func(candidate snapshotDiffWindow) bool {
		candidateEvent := sessionDiffPageEventFromProjection(
			data.SessionRecord{Summary: data.SessionSummary{ID: event.SessionID}, Projection: projection},
			projection,
			candidate,
			cursor,
		)
		candidateEvent.PayloadLimited = event.PayloadLimited
		candidateEvent.PayloadLimitReason = event.PayloadLimitReason
		return fitsProtocolPayloadBudget(candidateEvent, maxPayloadBytes)
	})
	event.Diffs = protocolDiffContextsFromDiffs(entries)
	event.DiffStart = start
	event.HasMoreBefore = start > 0
	event.ReviewGroups = protocolReviewGroupsFromDiffs(entries)
	event.ActiveReviewGroup = protocolActiveReviewGroupForPage(projection.ActiveReviewGroup, event.ReviewGroups)
	event.CurrentDiff = protocolCurrentDiffForPage(projection.CurrentDiff, entries)
	return event
}

func shrinkSessionTerminalExecutionPageToPayloadBudget(event protocol.SessionTerminalExecutionPageEvent, window snapshotTerminalExecutionWindow, includeOutput bool, projection data.ProjectionSnapshot, cursor DeltaCursorSnapshot, maxPayloadBytes int) protocol.SessionTerminalExecutionPageEvent {
	entries, start := shrinkTerminalExecutionWindowEntries(window, func(candidate snapshotTerminalExecutionWindow) bool {
		candidateEvent := sessionTerminalExecutionPageEventFromProjection(
			data.SessionRecord{Summary: data.SessionSummary{ID: event.SessionID}, Projection: projection},
			projection,
			candidate,
			includeOutput,
			cursor,
		)
		candidateEvent.PayloadLimited = event.PayloadLimited
		candidateEvent.PayloadLimitReason = event.PayloadLimitReason
		return fitsProtocolPayloadBudget(candidateEvent, maxPayloadBytes)
	})
	event.TerminalExecutions = protocolTerminalExecutions(entries)
	if !includeOutput {
		event.TerminalExecutions = stripTerminalExecutionOutput(event.TerminalExecutions)
	}
	event.ExecutionStart = start
	event.HasMoreBefore = start > 0
	return event
}

func shrinkDiffWindowEntries(window snapshotDiffWindow, fits func(snapshotDiffWindow) bool) ([]DiffContext, int) {
	if len(window.entries) == 0 {
		return nil, window.start
	}
	before := window.start + len(window.entries)
	low, high := 0, len(window.entries)
	best := 0
	for low <= high {
		count := (low + high) / 2
		start := before - count
		candidate := snapshotDiffWindow{
			entries: append([]DiffContext(nil), window.entries[len(window.entries)-count:]...),
			start:   start,
		}
		if fits(candidate) {
			best = count
			low = count + 1
			continue
		}
		high = count - 1
	}
	start := before - best
	if best == 0 {
		return nil, start
	}
	return append([]DiffContext(nil), window.entries[len(window.entries)-best:]...), start
}

func shrinkTerminalExecutionWindowEntries(window snapshotTerminalExecutionWindow, fits func(snapshotTerminalExecutionWindow) bool) ([]data.TerminalExecution, int) {
	if len(window.entries) == 0 {
		return nil, window.start
	}
	before := window.start + len(window.entries)
	low, high := 0, len(window.entries)
	best := 0
	for low <= high {
		count := (low + high) / 2
		start := before - count
		candidate := snapshotTerminalExecutionWindow{
			entries: append([]data.TerminalExecution(nil), window.entries[len(window.entries)-count:]...),
			start:   start,
		}
		if fits(candidate) {
			best = count
			low = count + 1
			continue
		}
		high = count - 1
	}
	start := before - best
	if best == 0 {
		return nil, start
	}
	return append([]data.TerminalExecution(nil), window.entries[len(window.entries)-best:]...), start
}

func SessionDeltaEventFromRecord(record data.SessionRecord, known protocol.SessionDeltaKnown, cursor DeltaCursorSnapshot, runtimeAlive bool) protocol.SessionDeltaEvent {
	return SessionDeltaEventFromRecordWithPayloadLimit(record, known, cursor, runtimeAlive, 0, 0)
}

func SessionDeltaEventFromWindowMetadata(window data.SessionHistoryWindow, known protocol.SessionDeltaKnown, cursor DeltaCursorSnapshot, runtimeAlive bool, requiresFullSync bool) protocol.SessionDeltaEvent {
	record, projection, _ := projectionAndSnapshotWindowFromSessionHistoryWindow(window)
	latest := sessionDeltaKnownFromProjectionWithLogCount(projection, cursor, window.LogEntryTotal)
	resumeMeta := runtimeMetaFromProjection(projection)
	return protocol.NewSessionDeltaEvent(
		record.Summary.ID,
		ToProtocolSummary(record.Summary),
		known,
		latest,
		nil,
		nil,
		nil,
		nil,
		nil,
		HistoryContextFromSnapshot(projection.CurrentStep),
		HistoryContextFromSnapshot(projection.LatestError),
		nil,
		nil,
		ToProtocolContextWindowUsage(projection.ContextWindowUsage),
		ToProtocolSessionContext(projection.SessionContext),
		ToProtocolCatalogMetadata(projection.SkillCatalogMeta),
		ToProtocolCatalogMetadata(projection.MemoryCatalogMeta),
		strings.TrimSpace(resumeMeta.ResumeSessionID) != "",
		runtimeAlive,
		resumeMeta,
		requiresFullSync,
	)
}

func SessionDeltaEventFromHistoryWindow(window data.SessionHistoryWindow, known protocol.SessionDeltaKnown, cursor DeltaCursorSnapshot, runtimeAlive bool, maxPayloadBytes int) protocol.SessionDeltaEvent {
	record, projection, snapshotWindow := projectionAndSnapshotWindowFromSessionHistoryWindow(window)
	latest := sessionDeltaKnownFromProjectionWithLogCount(projection, cursor, window.LogEntryTotal)
	return sessionDeltaEventFromHistoryWindow(record, projection, snapshotWindow, known, latest, runtimeAlive, maxPayloadBytes)
}

func SessionDeltaEventFromHistoryWindowWithCounts(window data.SessionHistoryWindow, latestCounts data.SessionProjectionCounts, known protocol.SessionDeltaKnown, cursor DeltaCursorSnapshot, runtimeAlive bool, maxPayloadBytes int) protocol.SessionDeltaEvent {
	record, projection, snapshotWindow := projectionAndSnapshotWindowFromSessionHistoryWindow(window)
	latest := sessionDeltaKnownFromCounts(latestCounts, cursor)
	return sessionDeltaEventFromHistoryWindow(record, projection, snapshotWindow, known, latest, runtimeAlive, maxPayloadBytes)
}

func sessionDeltaEventFromHistoryWindow(record data.SessionRecord, projection data.ProjectionSnapshot, snapshotWindow snapshotLogWindow, known, latest protocol.SessionDeltaKnown, runtimeAlive bool, maxPayloadBytes int) protocol.SessionDeltaEvent {
	resumeMeta := runtimeMetaFromProjection(projection)
	requiresFullSync := false
	appendEntries := []protocol.HistoryLogEntry(nil)
	if known.LogEntryCount < 0 || known.LogEntryCount > snapshotWindow.total {
		requiresFullSync = known.LogEntryCount != 0
	} else if known.LogEntryCount < snapshotWindow.start {
		requiresFullSync = true
	} else {
		offset := known.LogEntryCount - snapshotWindow.start
		if offset < 0 {
			offset = 0
		}
		if offset > len(snapshotWindow.entries) {
			offset = len(snapshotWindow.entries)
		}
		appendEntries = protocolLogEntries(snapshotWindow.entries[offset:])
	}
	event := protocol.NewSessionDeltaEvent(
		record.Summary.ID,
		ToProtocolSummary(record.Summary),
		known,
		latest,
		appendEntries,
		nil,
		nil,
		nil,
		nil,
		HistoryContextFromSnapshot(projection.CurrentStep),
		HistoryContextFromSnapshot(projection.LatestError),
		nil,
		nil,
		ToProtocolContextWindowUsage(projection.ContextWindowUsage),
		ToProtocolSessionContext(projection.SessionContext),
		ToProtocolCatalogMetadata(projection.SkillCatalogMeta),
		ToProtocolCatalogMetadata(projection.MemoryCatalogMeta),
		strings.TrimSpace(resumeMeta.ResumeSessionID) != "",
		runtimeAlive,
		resumeMeta,
		requiresFullSync,
	)
	if requiresFullSync {
		event.PayloadLimited = true
		event.PayloadLimitReason = "history_window_out_of_range"
		return event
	}
	if fitsProtocolPayloadBudget(event, maxPayloadBytes) {
		return event
	}
	event.AppendLogEntries = nil
	event.RequiresFullSync = true
	event.PayloadLimited = true
	event.PayloadLimitReason = "payload_budget_exceeded"
	return event
}

func SessionDeltaEventFromRuntimeMetadata(meta data.SessionRuntimeMetadata, known protocol.SessionDeltaKnown, cursor DeltaCursorSnapshot, runtimeAlive bool, requiresFullSync bool) protocol.SessionDeltaEvent {
	record := meta.Record
	projection := NormalizeProjectionSnapshot(record.Projection)
	latest := sessionDeltaKnownFromCounts(meta.Latest, cursor)
	resumeMeta := runtimeMetaFromProjection(projection)
	return protocol.NewSessionDeltaEvent(
		record.Summary.ID,
		ToProtocolSummary(record.Summary),
		known,
		latest,
		nil,
		nil,
		nil,
		nil,
		nil,
		HistoryContextFromSnapshot(projection.CurrentStep),
		HistoryContextFromSnapshot(projection.LatestError),
		nil,
		nil,
		ToProtocolContextWindowUsage(projection.ContextWindowUsage),
		ToProtocolSessionContext(projection.SessionContext),
		ToProtocolCatalogMetadata(projection.SkillCatalogMeta),
		ToProtocolCatalogMetadata(projection.MemoryCatalogMeta),
		strings.TrimSpace(resumeMeta.ResumeSessionID) != "",
		runtimeAlive,
		resumeMeta,
		requiresFullSync,
	)
}

func SessionDeltaEventFromRecordWithPayloadLimit(record data.SessionRecord, known protocol.SessionDeltaKnown, cursor DeltaCursorSnapshot, runtimeAlive bool, maxPayloadBytes int, maxAppendLogEntries int) protocol.SessionDeltaEvent {
	projection := NormalizeProjectionSnapshot(record.Projection)
	allEntries := protocolLogEntries(projection.LogEntries)
	latestCursor := cursor.LatestCursor
	latest := sessionDeltaKnownFromProjection(projection, cursor)
	resumeMeta := runtimeMetaFromProjection(projection)
	if deltaKnownEmpty(known) && maxAppendLogEntries > 0 && len(allEntries) > maxAppendLogEntries {
		return sessionDeltaFullSyncEvent(record, known, latest, resumeMeta, runtimeAlive, "payload_log_entry_limit_exceeded")
	}
	startLog := known.LogEntryCount
	logReset := false
	if latestCursor > 0 && known.EventCursor >= latestCursor {
		startLog = len(allEntries)
	}
	if startLog < 0 || startLog > len(allEntries) {
		startLog = 0
		logReset = known.LogEntryCount != 0
	}
	appendEntries := append([]protocol.HistoryLogEntry(nil), allEntries[startLog:]...)

	stdout := projection.RawTerminalByStream["stdout"]
	stderr := projection.RawTerminalByStream["stderr"]
	allExecutions := protocolTerminalExecutions(projection.TerminalExecutions)
	allDiffs := ProtocolDiffContexts(projection.Diffs)
	latest.DiffCount = len(allDiffs)
	latest.TerminalExecutionCount = len(allExecutions)
	latest.TerminalStdoutLength = len(stdout)
	latest.TerminalStderrLength = len(stderr)
	requiresFullSync := logReset
	event := protocol.NewSessionDeltaEvent(
		record.Summary.ID,
		ToProtocolSummary(record.Summary),
		known,
		latest,
		appendEntries,
		nil,
		nil,
		nil,
		nil,
		HistoryContextFromSnapshot(projection.CurrentStep),
		HistoryContextFromSnapshot(projection.LatestError),
		nil,
		nil,
		ToProtocolContextWindowUsage(projection.ContextWindowUsage),
		ToProtocolSessionContext(projection.SessionContext),
		ToProtocolCatalogMetadata(projection.SkillCatalogMeta),
		ToProtocolCatalogMetadata(projection.MemoryCatalogMeta),
		strings.TrimSpace(resumeMeta.ResumeSessionID) != "",
		runtimeAlive,
		resumeMeta,
		requiresFullSync,
	)
	if requiresFullSync {
		return sessionDeltaFullSyncEvent(record, known, latest, resumeMeta, runtimeAlive, "")
	}
	if deltaPayloadExceedsBudget(event, maxPayloadBytes, maxAppendLogEntries) {
		return sessionDeltaFullSyncEvent(record, known, latest, resumeMeta, runtimeAlive, "payload_budget_exceeded")
	}
	return event
}

func runtimeMetaFromProjection(projection data.ProjectionSnapshot) protocol.RuntimeMeta {
	return protocol.RuntimeMeta{
		ResumeSessionID:  projection.Runtime.ResumeSessionID,
		Command:          projection.Runtime.Command,
		Engine:           projection.Runtime.Engine,
		CodexSandboxMode: projection.Runtime.CodexSandboxMode,
		CWD:              projection.Runtime.CWD,
		PermissionMode:   projection.Runtime.PermissionMode,
		ClaudeLifecycle:  NormalizeProjectionLifecycle(projection.Runtime.ClaudeLifecycle, projection.Runtime.ResumeSessionID),
		SourcePath:       projection.Runtime.SourcePath,
		SourceSize:       projection.Runtime.SourceSize,
		SourceModUnixNS:  projection.Runtime.SourceModUnixNS,
		SourceEntryCount: projection.Runtime.SourceEntryCount,
	}
}

func sessionDeltaKnownFromProjection(projection data.ProjectionSnapshot, cursor DeltaCursorSnapshot) protocol.SessionDeltaKnown {
	return sessionDeltaKnownFromProjectionWithLogCount(projection, cursor, len(projection.LogEntries))
}

func sessionDeltaKnownFromProjectionWithLogCount(projection data.ProjectionSnapshot, cursor DeltaCursorSnapshot, logEntryCount int) protocol.SessionDeltaKnown {
	if logEntryCount < 0 {
		logEntryCount = 0
	}
	return protocol.SessionDeltaKnown{
		EventCursor:            cursor.LatestCursor,
		LogEntryCount:          logEntryCount,
		DiffCount:              len(projection.Diffs),
		TerminalExecutionCount: len(projection.TerminalExecutions),
		TerminalStdoutLength:   len(projection.RawTerminalByStream["stdout"]),
		TerminalStderrLength:   len(projection.RawTerminalByStream["stderr"]),
	}
}

func sessionDeltaKnownFromCounts(counts data.SessionProjectionCounts, cursor DeltaCursorSnapshot) protocol.SessionDeltaKnown {
	logEntryCount := counts.LogEntryCount
	if logEntryCount < 0 {
		logEntryCount = 0
	}
	return protocol.SessionDeltaKnown{
		EventCursor:            cursor.LatestCursor,
		LogEntryCount:          logEntryCount,
		DiffCount:              counts.DiffCount,
		TerminalExecutionCount: counts.TerminalExecutionCount,
		TerminalStdoutLength:   counts.TerminalStdoutLength,
		TerminalStderrLength:   counts.TerminalStderrLength,
	}
}

func deltaKnownEmpty(known protocol.SessionDeltaKnown) bool {
	return known.EventCursor == 0 &&
		known.LogEntryCount == 0 &&
		known.DiffCount == 0 &&
		known.TerminalExecutionCount == 0 &&
		known.TerminalStdoutLength == 0 &&
		known.TerminalStderrLength == 0
}

func sessionDeltaFullSyncEvent(record data.SessionRecord, known, latest protocol.SessionDeltaKnown, resumeMeta protocol.RuntimeMeta, runtimeAlive bool, payloadLimitReason string) protocol.SessionDeltaEvent {
	event := protocol.NewSessionDeltaEvent(
		record.Summary.ID,
		ToProtocolSummary(record.Summary),
		known,
		latest,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		protocol.ContextWindowUsage{},
		protocol.SessionContext{},
		protocol.CatalogMetadata{},
		protocol.CatalogMetadata{},
		strings.TrimSpace(resumeMeta.ResumeSessionID) != "",
		runtimeAlive,
		resumeMeta,
		true,
	)
	if strings.TrimSpace(payloadLimitReason) != "" {
		event.PayloadLimited = true
		event.PayloadLimitReason = strings.TrimSpace(payloadLimitReason)
	}
	return event
}

func deltaPayloadExceedsBudget(event protocol.SessionDeltaEvent, maxPayloadBytes int, maxAppendLogEntries int) bool {
	if maxAppendLogEntries > 0 && len(event.AppendLogEntries) > maxAppendLogEntries {
		return true
	}
	if maxPayloadBytes <= 0 {
		return false
	}
	return !fitsProtocolPayloadBudget(event, maxPayloadBytes)
}

type terminalExecutionDeltaInput struct {
	all                   []protocol.TerminalExecution
	knownCount            int
	cursorCaughtUp        bool
	entries               []protocol.HistoryLogEntry
	terminalOutputChanged bool
}

func terminalExecutionsForDelta(input terminalExecutionDeltaInput) ([]protocol.TerminalExecution, bool) {
	all := input.all
	start := input.knownCount
	if input.cursorCaughtUp {
		start = len(all)
	}
	reset := false
	if start < 0 || start > len(all) {
		start = 0
		reset = input.knownCount != 0
	}

	indexByID := make(map[string]int, len(all))
	for i, item := range all {
		if id := strings.TrimSpace(item.ExecutionID); id != "" {
			indexByID[id] = i
		}
	}

	out := make([]protocol.TerminalExecution, 0, len(all)-start+1)
	included := make(map[string]struct{}, len(all)-start+1)
	addAt := func(index int) {
		if index < 0 || index >= len(all) {
			return
		}
		item := all[index]
		id := strings.TrimSpace(item.ExecutionID)
		if id != "" {
			if _, ok := included[id]; ok {
				return
			}
			included[id] = struct{}{}
		}
		out = append(out, item)
	}

	for i := start; i < len(all); i++ {
		addAt(i)
	}
	for _, entry := range input.entries {
		if index, ok := indexByID[strings.TrimSpace(entry.ExecutionID)]; ok {
			addAt(index)
		}
	}
	if input.terminalOutputChanged {
		for i, item := range all {
			if terminalExecutionStillRunning(item) {
				addAt(i)
			}
		}
	}
	return out, reset
}

func terminalExecutionStillRunning(item protocol.TerminalExecution) bool {
	return strings.TrimSpace(item.ExecutionID) != "" &&
		strings.TrimSpace(item.FinishedAt) == "" &&
		item.ExitCode == nil
}

func normalizeTerminalStream(stream string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(stream)) {
	case "", "stdout":
		return "stdout", nil
	case "stderr":
		return "stderr", nil
	default:
		return "", fmt.Errorf("unsupported terminal stream %q", stream)
	}
}

func boundedPositiveLimit(limit, defaultLimit, maxLimit int) int {
	if limit <= 0 {
		limit = defaultLimit
	}
	if maxLimit > 0 && limit > maxLimit {
		return maxLimit
	}
	return limit
}

func suggestedTerminalRangeLimit(maxPayloadBytes int) int {
	if maxPayloadBytes <= 0 {
		return terminalRangeDefaultLimit
	}
	limit := maxPayloadBytes / 2
	if limit <= 0 {
		return 1
	}
	if limit > terminalRangeMaxLimit {
		return terminalRangeMaxLimit
	}
	return limit
}

func protocolDiffContextsFromDiffs(diffs []DiffContext) []protocol.HistoryContext {
	if len(diffs) == 0 {
		return nil
	}
	result := make([]protocol.HistoryContext, 0, len(diffs))
	for _, diff := range diffs {
		if ctx := ProtocolDiffContext(&diff); ctx != nil {
			result = append(result, *ctx)
		}
	}
	return result
}

func protocolReviewGroupsFromDiffs(diffs []DiffContext) []protocol.ReviewGroup {
	return ProtocolReviewGroups(RebuildReviewGroups(diffs))
}

func protocolCurrentDiffForPage(current *DiffContext, page []DiffContext) *protocol.HistoryContext {
	if current == nil {
		return nil
	}
	for _, item := range page {
		if snapshotDiffMatches(item, current.ContextID, current.Path) {
			return ProtocolDiffContext(&item)
		}
	}
	return nil
}

func protocolActiveReviewGroupForPage(active *ReviewGroup, groups []protocol.ReviewGroup) *protocol.ReviewGroup {
	if len(groups) == 0 {
		return nil
	}
	activeID := ""
	if active != nil {
		activeID = strings.TrimSpace(active.ID)
	}
	if activeID != "" {
		for i := range groups {
			if strings.TrimSpace(groups[i].ID) == activeID {
				return &groups[i]
			}
		}
	}
	return &groups[0]
}

func stripTerminalExecutionOutput(items []protocol.TerminalExecution) []protocol.TerminalExecution {
	if len(items) == 0 {
		return nil
	}
	result := make([]protocol.TerminalExecution, 0, len(items))
	for _, item := range items {
		item.Stdout = ""
		item.Stderr = ""
		result = append(result, item)
	}
	return result
}

func stripHistoryContextBodies(items []protocol.HistoryContext) []protocol.HistoryContext {
	if len(items) == 0 {
		return nil
	}
	result := make([]protocol.HistoryContext, 0, len(items))
	for i := range items {
		item := items[i]
		item.Diff = ""
		item.Stack = ""
		item.Code = ""
		result = append(result, item)
	}
	return result
}

func stripHistoryContextBody(item *protocol.HistoryContext) *protocol.HistoryContext {
	if item == nil {
		return nil
	}
	copy := *item
	copy.Diff = ""
	copy.Stack = ""
	copy.Code = ""
	return &copy
}

func stripReviewGroupBodies(items []protocol.ReviewGroup) []protocol.ReviewGroup {
	if len(items) == 0 {
		return nil
	}
	result := make([]protocol.ReviewGroup, 0, len(items))
	for _, group := range items {
		for i := range group.Files {
			group.Files[i].Diff = ""
		}
		result = append(result, group)
	}
	return result
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
		CodexSandboxMode: firstNonEmptyString(
			projection.Controller.ActiveMeta.CodexSandboxMode,
			projection.Runtime.CodexSandboxMode,
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
			Attachments: append([]protocol.TimelineAttachment(nil), entry.Attachments...),
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
