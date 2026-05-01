package session

import (
	"context"
	"strings"
	"time"

	"mobilevc/internal/data"
	"mobilevc/internal/engine"
	"mobilevc/internal/protocol"
)

type TaskCursorSnapshot struct {
	LatestCursor int64
	LastOutputAt time.Time
}

func NormalizeProjectionSnapshot(snapshot data.ProjectionSnapshot) data.ProjectionSnapshot {
	if snapshot.RawTerminalByStream == nil {
		snapshot.RawTerminalByStream = map[string]string{"stdout": "", "stderr": ""}
	}
	if snapshot.LogEntries == nil {
		snapshot.LogEntries = []data.SnapshotLogEntry{}
	}
	if snapshot.TerminalExecutions == nil {
		snapshot.TerminalExecutions = []data.TerminalExecution{}
	}
	if snapshot.Runtime.ResumeSessionID == "" {
		snapshot.Runtime.ResumeSessionID = snapshot.Controller.ResumeSession
	}
	if snapshot.Runtime.Command == "" {
		snapshot.Runtime.Command = snapshot.Controller.CurrentCommand
	}
	if snapshot.Runtime.Engine == "" {
		snapshot.Runtime.Engine = firstNonEmptyString(snapshot.Controller.ActiveMeta.Engine, snapshot.Controller.ActiveMeta.SkillName)
	}
	if snapshot.Runtime.CWD == "" {
		snapshot.Runtime.CWD = snapshot.Controller.ActiveMeta.CWD
	}
	if snapshot.Runtime.PermissionMode == "" {
		snapshot.Runtime.PermissionMode = snapshot.Controller.ActiveMeta.PermissionMode
	}
	snapshot.Runtime.ClaudeLifecycle = NormalizeProjectionLifecycle(
		firstNonEmptyString(snapshot.Controller.ClaudeLifecycle, snapshot.Controller.ActiveMeta.ClaudeLifecycle, snapshot.Runtime.ClaudeLifecycle),
		snapshot.Runtime.ResumeSessionID,
	)
	if snapshot.Runtime.ClaudeLifecycle != "" {
		snapshot.Controller.ClaudeLifecycle = snapshot.Runtime.ClaudeLifecycle
		snapshot.Controller.ActiveMeta.ClaudeLifecycle = snapshot.Runtime.ClaudeLifecycle
	}
	if !snapshot.SessionContext.Configured &&
		len(snapshot.SessionContext.EnabledSkillNames) == 0 &&
		len(snapshot.SessionContext.EnabledMemoryIDs) == 0 {
		snapshot.SessionContext = data.SessionContext{}
	}
	if len(snapshot.Diffs) == 0 && snapshot.CurrentDiff != nil {
		snapshot.Diffs = []DiffContext{*snapshot.CurrentDiff}
	}
	if len(snapshot.ReviewGroups) == 0 && len(snapshot.Diffs) > 0 {
		snapshot.ReviewGroups = RebuildReviewGroups(snapshot.Diffs)
	}
	if activeGroup := PickActiveReviewGroup(snapshot.ReviewGroups); activeGroup != nil {
		snapshot.ActiveReviewGroup = activeGroup
	}
	activeDiff := PickActiveSnapshotDiff(snapshot.Diffs)
	if strings.TrimSpace(activeDiff.ContextID+activeDiff.Path+activeDiff.Title) != "" {
		snapshot.CurrentDiff = &activeDiff
	}
	return snapshot
}

func NormalizeProjectionLifecycle(lifecycle string, resumeSessionID string) string {
	normalized := strings.TrimSpace(lifecycle)
	if normalized == "starting" && strings.TrimSpace(resumeSessionID) != "" {
		return "resumable"
	}
	return normalized
}

func WithRuntimeSnapshot(snapshot data.ProjectionSnapshot, svc *Service) data.ProjectionSnapshot {
	snapshot = NormalizeProjectionSnapshot(snapshot)
	if svc == nil {
		return snapshot
	}
	controller := svc.ControllerSnapshot()
	runtimeSnapshot := svc.RuntimeSnapshot()
	hasLiveRuntimeState := runtimeSnapshot.Running ||
		strings.TrimSpace(runtimeSnapshot.ResumeSessionID) != "" ||
		strings.TrimSpace(runtimeSnapshot.ActiveMeta.ResumeSessionID) != "" ||
		strings.TrimSpace(runtimeSnapshot.ActiveMeta.Command) != "" ||
		strings.TrimSpace(runtimeSnapshot.ActiveMeta.Engine) != "" ||
		(strings.TrimSpace(string(controller.State)) != "" && controller.State != ControllerStateIdle) ||
		strings.TrimSpace(controller.ResumeSession) != "" ||
		strings.TrimSpace(controller.CurrentCommand) != "" ||
		strings.TrimSpace(controller.ActiveMeta.Command) != "" ||
		strings.TrimSpace(controller.ActiveMeta.Engine) != "" ||
		strings.TrimSpace(controller.ClaudeLifecycle) != ""
	if !hasLiveRuntimeState {
		return snapshot
	}
	runtimeMeta := protocol.MergeRuntimeMeta(runtimeSnapshot.ActiveMeta, controller.ActiveMeta)
	snapshot.Controller = controller
	snapshot.Runtime = data.SessionRuntime{
		ResumeSessionID: firstNonEmptyString(
			controller.ResumeSession,
			runtimeMeta.ResumeSessionID,
			runtimeSnapshot.ResumeSessionID,
			snapshot.Runtime.ResumeSessionID,
		),
		Command: firstNonEmptyString(
			controller.CurrentCommand,
			runtimeMeta.Command,
			snapshot.Runtime.Command,
		),
		Engine: firstNonEmptyString(
			runtimeMeta.Engine,
			runtimeMeta.SkillName,
			snapshot.Runtime.Engine,
		),
		PermissionMode: firstNonEmptyString(runtimeMeta.PermissionMode, snapshot.Runtime.PermissionMode),
		CWD:            firstNonEmptyString(runtimeMeta.CWD, snapshot.Runtime.CWD),
		ClaudeLifecycle: NormalizeProjectionLifecycle(
			firstNonEmptyString(controller.ClaudeLifecycle, runtimeMeta.ClaudeLifecycle, runtimeSnapshot.ClaudeLifecycle),
			firstNonEmptyString(controller.ResumeSession, runtimeMeta.ResumeSessionID, runtimeSnapshot.ResumeSessionID),
		),
		Source: firstNonEmptyString(snapshot.Runtime.Source, "mobilevc"),
	}
	return snapshot
}

func SessionRecordRuntimeAlive(record data.SessionRecord, svc *Service, allowStoredRuntime bool) bool {
	if svc != nil {
		snapshot := svc.RuntimeSnapshot()
		if snapshot.Running && strings.TrimSpace(snapshot.ActiveSession) == strings.TrimSpace(record.Summary.ID) {
			return true
		}
	}
	if !allowStoredRuntime {
		return false
	}
	projection := NormalizeProjectionSnapshot(record.Projection)
	state := strings.TrimSpace(string(projection.Controller.State))
	lifecycle := strings.TrimSpace(projection.Runtime.ClaudeLifecycle)
	return IsBusyRuntimeState(state) || lifecycle == "active" || lifecycle == "starting"
}

func (s *Service) ShouldEmitTransientResumeThinkingEvent(req ExecuteRequest) bool {
	if s == nil || s.IsRunning() {
		return false
	}
	if req.Mode != engine.ModePTY {
		return false
	}
	return s.CanResumeAISession(req) && s.HasResumeSession(req)
}

func (s *Service) BuildTaskSnapshotEvent(sessionID string, cursor TaskCursorSnapshot, reason string, syncing bool) *protocol.TaskSnapshotEvent {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || s == nil {
		return nil
	}
	snapshot := s.RuntimeSnapshot()
	controller := s.ControllerSnapshot()
	runtimeAlive := snapshot.Running && strings.TrimSpace(snapshot.ActiveSession) == sessionID
	meta := protocol.MergeRuntimeMeta(controller.ActiveMeta, snapshot.ActiveMeta)
	if strings.TrimSpace(meta.Command) == "" {
		meta.Command = strings.TrimSpace(firstNonEmptyString(controller.CurrentCommand, snapshot.ActiveMeta.Command))
	}
	if strings.TrimSpace(meta.CWD) == "" {
		meta.CWD = strings.TrimSpace(snapshot.ActiveMeta.CWD)
	}
	if strings.TrimSpace(meta.ResumeSessionID) == "" {
		meta.ResumeSessionID = strings.TrimSpace(firstNonEmptyString(controller.ResumeSession, snapshot.ResumeSessionID))
	}
	if strings.TrimSpace(meta.ClaudeLifecycle) == "" {
		meta.ClaudeLifecycle = strings.TrimSpace(firstNonEmptyString(controller.ClaudeLifecycle, snapshot.ClaudeLifecycle))
	}
	state := "IDLE"
	message := "Task idle"
	awaitInput := false
	if runtimeAlive {
		state = "RUNNING"
		message = "Task running on desktop"
		if snapshot.CanAcceptInteractiveInput || strings.EqualFold(strings.TrimSpace(snapshot.ClaudeLifecycle), "waiting_input") {
			state = "WAIT_INPUT"
			message = "Task waiting for input"
			awaitInput = true
		}
	} else if strings.TrimSpace(meta.ResumeSessionID) != "" || strings.EqualFold(strings.TrimSpace(snapshot.ClaudeLifecycle), "resumable") {
		state = "IDLE"
		message = "Task resumable"
	}
	if reason = strings.TrimSpace(reason); reason != "" {
		message += " (" + reason + ")"
	}
	event := protocol.NewTaskSnapshotEvent(
		sessionID,
		state,
		message,
		runtimeAlive,
		awaitInput,
		meta.Command,
		controller.LastStep,
		controller.LastTool,
		cursor.LatestCursor,
		cursor.LastOutputAt,
		meta,
	)
	event.Syncing = syncing
	return &event
}

func BuildResumeRecoveryStateEvent(sessionID string, svc *Service, projection data.ProjectionSnapshot, lastKnownRuntimeState string) protocol.AgentStateEvent {
	projection = NormalizeProjectionSnapshot(projection)
	controller := projection.Controller
	runtimeMeta := projection.Runtime
	if svc != nil {
		snapshot := svc.RuntimeSnapshot()
		runtimeMeta = MergeStoreSessionRuntime(runtimeMeta, data.SessionRuntime{
			ResumeSessionID: snapshot.ResumeSessionID,
			Command:         snapshot.ActiveMeta.Command,
			Engine:          snapshot.ActiveMeta.Engine,
			PermissionMode:  snapshot.ActiveMeta.PermissionMode,
			CWD:             snapshot.ActiveMeta.CWD,
			ClaudeLifecycle: snapshot.ClaudeLifecycle,
			Source:          "mobilevc",
		})
		controller = svc.ControllerSnapshot()
	}
	meta := protocol.MergeRuntimeMeta(controller.ActiveMeta, protocol.RuntimeMeta{
		Source:          firstNonEmptyString(runtimeMeta.Source, "mobilevc"),
		ResumeSessionID: runtimeMeta.ResumeSessionID,
		Command:         firstNonEmptyString(runtimeMeta.Command, controller.CurrentCommand),
		Engine:          runtimeMeta.Engine,
		CWD:             runtimeMeta.CWD,
		PermissionMode:  runtimeMeta.PermissionMode,
		ClaudeLifecycle: firstNonEmptyString(runtimeMeta.ClaudeLifecycle, "active"),
	})
	message := "恢复会话中"
	if IsBusyRuntimeState(lastKnownRuntimeState) {
		message = "恢复执行中"
	}
	event := protocol.NewAgentStateEvent(
		sessionID,
		"RECOVERING",
		message,
		false,
		meta.Command,
		controller.LastStep,
		controller.LastTool,
	)
	event.RuntimeMeta = meta
	return event
}

func ResolvedResumeRuntimeState(restoredState string, record data.SessionRecord, svc *Service) string {
	if strings.TrimSpace(restoredState) != "" {
		return restoredState
	}
	projection := NormalizeProjectionSnapshot(record.Projection)
	if svc != nil {
		projection = WithRuntimeSnapshot(projection, svc)
	}
	if state := strings.TrimSpace(string(projection.Controller.State)); state != "" {
		return state
	}
	if svc != nil && svc.IsRunning() {
		return "RECOVERING"
	}
	return ""
}

func IsBusyRuntimeState(state string) bool {
	switch strings.TrimSpace(strings.ToUpper(state)) {
	case "RUNNING", "THINKING", "RUNNING_TOOL", "RECOVERING":
		return true
	default:
		return false
	}
}

func (s *Service) WaitForInteractive(ctx context.Context, timeout time.Duration) error {
	if timeout <= 0 {
		return s.waitForInteractive(ctx)
	}
	deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if s.CanAcceptInteractiveInput() {
			return nil
		}
		if !s.IsRunning() {
			return ErrNoActiveRunner
		}
		select {
		case <-deadlineCtx.Done():
			if s.CanAcceptInteractiveInput() {
				return nil
			}
			if !s.IsRunning() {
				return ErrNoActiveRunner
			}
			return ErrRunnerNotInteractive
		case <-ticker.C:
		}
	}
}

func MergeStoreSessionRuntime(base data.SessionRuntime, overlay data.SessionRuntime) data.SessionRuntime {
	return data.SessionRuntime{
		ResumeSessionID: firstNonEmptyString(overlay.ResumeSessionID, base.ResumeSessionID),
		Command:         firstNonEmptyString(overlay.Command, base.Command),
		Engine:          firstNonEmptyString(overlay.Engine, base.Engine),
		PermissionMode:  firstNonEmptyString(overlay.PermissionMode, base.PermissionMode),
		CWD:             firstNonEmptyString(overlay.CWD, base.CWD),
		ClaudeLifecycle: firstNonEmptyString(overlay.ClaudeLifecycle, base.ClaudeLifecycle),
		Source:          firstNonEmptyString(overlay.Source, base.Source),
	}
}

func MergeControllerSnapshot(base ControllerSnapshot, overlay ControllerSnapshot) ControllerSnapshot {
	merged := base
	merged.SessionID = firstNonEmptyString(overlay.SessionID, base.SessionID)
	if overlay.State != "" {
		merged.State = overlay.State
	}
	merged.CurrentCommand = firstNonEmptyString(overlay.CurrentCommand, base.CurrentCommand)
	merged.LastStep = firstNonEmptyString(overlay.LastStep, base.LastStep)
	merged.LastTool = firstNonEmptyString(overlay.LastTool, base.LastTool)
	merged.ResumeSession = firstNonEmptyString(overlay.ResumeSession, base.ResumeSession)
	merged.ClaudeLifecycle = firstNonEmptyString(overlay.ClaudeLifecycle, base.ClaudeLifecycle)
	merged.LastUserInput = firstNonEmptyString(overlay.LastUserInput, base.LastUserInput)
	merged.ActiveMeta = protocol.MergeRuntimeMeta(base.ActiveMeta, overlay.ActiveMeta)
	if len(overlay.RecentDiffs) > 0 {
		merged.RecentDiffs = overlay.RecentDiffs
	}
	if overlay.RecentDiff.ContextID != "" || overlay.RecentDiff.Path != "" || overlay.RecentDiff.Title != "" {
		merged.RecentDiff = overlay.RecentDiff
	}
	if len(overlay.ReviewGroups) > 0 {
		merged.ReviewGroups = overlay.ReviewGroups
	}
	merged.ActiveReviewID = firstNonEmptyString(overlay.ActiveReviewID, base.ActiveReviewID)
	return merged
}

func RebuildReviewGroups(diffs []DiffContext) []ReviewGroup {
	if len(diffs) == 0 {
		return nil
	}
	groupOrder := make([]string, 0)
	byGroup := map[string][]DiffContext{}
	for _, diff := range diffs {
		groupID := firstNonEmptyString(diff.GroupID, diff.ExecutionID, diff.ContextID, diff.Path)
		if groupID == "" {
			continue
		}
		if _, ok := byGroup[groupID]; !ok {
			groupOrder = append(groupOrder, groupID)
		}
		if diff.GroupID == "" {
			diff.GroupID = groupID
		}
		if diff.GroupTitle == "" {
			diff.GroupTitle = firstNonEmptyString(diff.Title, diff.Path, groupID)
		}
		byGroup[groupID] = append(byGroup[groupID], diff)
	}
	groups := make([]ReviewGroup, 0, len(groupOrder))
	for _, groupID := range groupOrder {
		items := byGroup[groupID]
		if len(items) == 0 {
			continue
		}
		files := make([]ReviewFile, 0, len(items))
		pendingCount := 0
		acceptedCount := 0
		revertedCount := 0
		revisedCount := 0
		for _, item := range items {
			files = append(files, ReviewFile{
				ContextID:     item.ContextID,
				Title:         item.Title,
				Path:          item.Path,
				Diff:          item.Diff,
				Lang:          item.Lang,
				PendingReview: item.PendingReview,
				ExecutionID:   item.ExecutionID,
				ReviewStatus:  item.ReviewStatus,
			})
			if item.PendingReview {
				pendingCount++
			}
			switch strings.TrimSpace(item.ReviewStatus) {
			case "accepted":
				acceptedCount++
			case "reverted":
				revertedCount++
			case "revised":
				revisedCount++
			}
		}
		reviewStatus := "pending"
		switch {
		case pendingCount == len(files):
			reviewStatus = "pending"
		case acceptedCount == len(files):
			reviewStatus = "accepted"
		case revertedCount == len(files):
			reviewStatus = "reverted"
		case revisedCount == len(files):
			reviewStatus = "revised"
		default:
			reviewStatus = "mixed"
		}
		current := PickActiveReviewFile(files)
		groups = append(groups, ReviewGroup{
			ID:            groupID,
			Title:         firstNonEmptyString(items[len(items)-1].GroupTitle, items[len(items)-1].Title, items[len(items)-1].Path, groupID),
			ExecutionID:   firstNonEmptyString(items[len(items)-1].ExecutionID, groupID),
			PendingReview: pendingCount > 0,
			ReviewStatus:  reviewStatus,
			CurrentFileID: current.ContextID,
			CurrentPath:   current.Path,
			PendingCount:  pendingCount,
			AcceptedCount: acceptedCount,
			RevertedCount: revertedCount,
			RevisedCount:  revisedCount,
			Files:         files,
		})
	}
	return groups
}

func PickActiveReviewFile(files []ReviewFile) ReviewFile {
	for _, file := range files {
		if file.PendingReview {
			return file
		}
	}
	if len(files) > 0 {
		return files[len(files)-1]
	}
	return ReviewFile{}
}

func PickActiveReviewGroup(groups []ReviewGroup) *ReviewGroup {
	for i := len(groups) - 1; i >= 0; i-- {
		if groups[i].PendingReview {
			group := groups[i]
			return &group
		}
	}
	if len(groups) > 0 {
		group := groups[len(groups)-1]
		return &group
	}
	return nil
}

func PickActiveSnapshotDiff(diffs []DiffContext) DiffContext {
	for i := len(diffs) - 1; i >= 0; i-- {
		if diffs[i].PendingReview {
			return diffs[i]
		}
	}
	if len(diffs) > 0 {
		return diffs[len(diffs)-1]
	}
	return DiffContext{}
}
