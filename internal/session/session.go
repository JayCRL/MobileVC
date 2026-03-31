package session

import (
	"strings"
	"sync"
	"time"

	"mobilevc/internal/protocol"
)

type State string

type ControllerState string

const (
	StateActive      State = "active"
	StateHibernating State = "hibernating"
	StateClosed      State = "closed"
)

const (
	ControllerStateIdle        ControllerState = "IDLE"
	ControllerStateThinking    ControllerState = "THINKING"
	ControllerStateWaitInput   ControllerState = "WAIT_INPUT"
	ControllerStateRunningTool ControllerState = "RUNNING_TOOL"
)

type Session struct {
	ID    string
	State State
}

type DiffContext struct {
	ContextID     string `json:"contextId,omitempty"`
	Title         string `json:"title,omitempty"`
	Path          string `json:"path,omitempty"`
	Diff          string `json:"diff,omitempty"`
	Lang          string `json:"lang,omitempty"`
	PendingReview bool   `json:"pendingReview,omitempty"`
	ExecutionID   string `json:"executionId,omitempty"`
	GroupID       string `json:"groupId,omitempty"`
	GroupTitle    string `json:"groupTitle,omitempty"`
	ReviewStatus  string `json:"reviewStatus,omitempty"`
}

type ReviewFile struct {
	ContextID     string `json:"contextId,omitempty"`
	Title         string `json:"title,omitempty"`
	Path          string `json:"path,omitempty"`
	Diff          string `json:"diff,omitempty"`
	Lang          string `json:"lang,omitempty"`
	PendingReview bool   `json:"pendingReview,omitempty"`
	ExecutionID   string `json:"executionId,omitempty"`
	ReviewStatus  string `json:"reviewStatus,omitempty"`
}

type ReviewGroup struct {
	ID            string       `json:"id,omitempty"`
	Title         string       `json:"title,omitempty"`
	ExecutionID   string       `json:"executionId,omitempty"`
	PendingReview bool         `json:"pendingReview,omitempty"`
	ReviewStatus  string       `json:"reviewStatus,omitempty"`
	CurrentFileID string       `json:"currentFileId,omitempty"`
	CurrentPath   string       `json:"currentPath,omitempty"`
	PendingCount  int          `json:"pendingCount,omitempty"`
	AcceptedCount int          `json:"acceptedCount,omitempty"`
	RevertedCount int          `json:"revertedCount,omitempty"`
	RevisedCount  int          `json:"revisedCount,omitempty"`
	Files         []ReviewFile `json:"files,omitempty"`
}

type ControllerSnapshot struct {
	SessionID       string               `json:"sessionId"`
	State           ControllerState      `json:"state"`
	CurrentCommand  string               `json:"currentCommand,omitempty"`
	LastStep        string               `json:"lastStep,omitempty"`
	LastTool        string               `json:"lastTool,omitempty"`
	ResumeSession   string               `json:"resumeSession,omitempty"`
	ClaudeLifecycle string               `json:"claudeLifecycle,omitempty"`
	LastUserInput   string               `json:"lastUserInput,omitempty"`
	ActiveMeta      protocol.RuntimeMeta `json:"activeMeta,omitempty"`
	RecentDiffs     []DiffContext        `json:"recentDiffs,omitempty"`
	RecentDiff      DiffContext          `json:"recentDiff,omitempty"`
	ReviewGroups    []ReviewGroup        `json:"reviewGroups,omitempty"`
	ActiveReviewID  string               `json:"activeReviewId,omitempty"`
}

type Controller struct {
	mu              sync.Mutex
	sessionID       string
	currentState    ControllerState
	currentCommand  string
	claudeLifecycle string
	lastStep        string
	lastTool        string
	resumeSession   string
	lastUserInput   string
	activeMeta      protocol.RuntimeMeta
	recentDiffs     []DiffContext
	recentDiff      DiffContext

	// dedup fields
	lastLogMsg     string
	lastLogTime    time.Time
	lastStepMsg    string
	lastStepStatus string
	lastPromptMsg  string
}

func (c *Controller) RecordUserInput(input string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return
	}
	c.lastUserInput = input
}

func (c *Controller) UpdatePermissionMode(mode string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.activeMeta.PermissionMode = strings.TrimSpace(mode)
}

func normalizeClaudeLifecycle(value string) string {
	switch strings.TrimSpace(value) {
	case "inactive", "starting", "active", "waiting_input", "resumable", "unknown":
		return strings.TrimSpace(value)
	default:
		return ""
	}
}

func isClaudeCommand(command string) bool {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return false
	}
	head := strings.ToLower(fields[0])
	return head == "claude" ||
		strings.HasSuffix(head, "/claude") ||
		strings.HasSuffix(head, `\\claude`) ||
		head == "claude.exe" ||
		head == "codex" ||
		strings.HasSuffix(head, "/codex") ||
		strings.HasSuffix(head, `\\codex`) ||
		head == "codex.exe"
}

func (c *Controller) deriveClaudeLifecycleLocked() string {
	if lifecycle := normalizeClaudeLifecycle(c.activeMeta.ClaudeLifecycle); lifecycle != "" {
		return lifecycle
	}
	if c.currentState == ControllerStateWaitInput && (isClaudeCommand(c.currentCommand) || strings.TrimSpace(c.resumeSession) != "") {
		return "waiting_input"
	}
	if c.currentState == ControllerStateThinking || c.currentState == ControllerStateRunningTool {
		if isClaudeCommand(c.currentCommand) {
			return "active"
		}
	}
	if strings.TrimSpace(c.resumeSession) != "" {
		return "resumable"
	}
	if isClaudeCommand(c.currentCommand) {
		return "unknown"
	}
	return "inactive"
}

func (c *Controller) refreshClaudeLifecycleLocked() {
	c.claudeLifecycle = c.deriveClaudeLifecycleLocked()
	if c.claudeLifecycle != "" {
		c.activeMeta.ClaudeLifecycle = c.claudeLifecycle
	}
}

func NewController(sessionID string) *Controller {
	return &Controller{
		sessionID:    sessionID,
		currentState: ControllerStateIdle,
	}
}

func (c *Controller) InitialEvent() protocol.AgentStateEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.newAgentStateEvent("空闲", false)
}

func (c *Controller) OnExecStart(command string, meta protocol.RuntimeMeta) []any {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.currentCommand = command
	c.currentState = ControllerStateThinking
	c.lastStep = ""
	c.lastTool = ""
	c.activeMeta = meta
	c.resumeSession = extractResumeSessionID(command, meta.ResumeSessionID)
	c.claudeLifecycle = firstNonEmpty(normalizeClaudeLifecycle(meta.ClaudeLifecycle), func() string {
		if isClaudeCommand(command) {
			return "starting"
		}
		if strings.TrimSpace(c.resumeSession) != "" {
			return "resumable"
		}
		return "inactive"
	}())
	c.activeMeta.ClaudeLifecycle = c.claudeLifecycle
	c.lastUserInput = ""
	message := "思考中"
	if meta.SkillName != "" {
		message = "执行 skill：" + meta.SkillName
	}
	if isClaudeCommand(command) && normalizeClaudeLifecycle(meta.ClaudeLifecycle) == "starting" {
		c.currentState = ControllerStateIdle
		message = "等待输入"
		return []any{c.newAgentStateEvent(message, true)}
	}
	return []any{c.newAgentStateEvent(message, false)}
}

func (c *Controller) OnRunnerEvent(event any) []any {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	switch e := event.(type) {
	case protocol.PromptRequestEvent:
		if e.Message == c.lastPromptMsg && c.currentState == ControllerStateWaitInput {
			return nil
		}
		c.lastPromptMsg = e.Message
		c.currentState = ControllerStateWaitInput
		message := e.Message
		if message == "" {
			message = "等待输入"
		}
		if e.ResumeSessionID != "" {
			c.resumeSession = e.ResumeSessionID
		}
		c.claudeLifecycle = "waiting_input"
		c.activeMeta.ClaudeLifecycle = c.claudeLifecycle
		return []any{c.newAgentStateEvent(message, true)}
	case protocol.StepUpdateEvent:
		if e.Message == c.lastStepMsg && (e.Status == c.lastStepStatus || e.Status == "") {
			return nil
		}
		c.lastStepMsg = e.Message
		c.lastStepStatus = e.Status
		if e.Message != "" {
			c.lastStep = e.Message
		}
		c.currentState = ControllerStateRunningTool
		c.claudeLifecycle = firstNonEmpty(normalizeClaudeLifecycle(e.RuntimeMeta.ClaudeLifecycle), func() string {
			if isClaudeCommand(c.currentCommand) {
				return "active"
			}
			return c.claudeLifecycle
		}())
		c.activeMeta.ClaudeLifecycle = c.claudeLifecycle
		c.lastTool = e.Target
		message := e.Message
		if message == "" {
			message = "执行工具中"
		}
		return []any{c.newAgentStateEvent(message, false)}
	case protocol.FileDiffEvent:
		message := "查看代码改动"
		if e.Title != "" {
			message = e.Title
		}
		c.currentState = ControllerStateRunningTool
		c.claudeLifecycle = firstNonEmpty(normalizeClaudeLifecycle(e.RuntimeMeta.ClaudeLifecycle), func() string {
			if isClaudeCommand(c.currentCommand) {
				return "active"
			}
			return c.claudeLifecycle
		}())
		c.activeMeta.ClaudeLifecycle = c.claudeLifecycle
		if e.Path != "" {
			c.lastTool = e.Path
		}
		c.recentDiff = DiffContext{
			ContextID:     firstNonEmpty(e.ContextID, e.Path, e.Title),
			Title:         firstNonEmpty(e.Title, e.ContextTitle, "Diff 预览"),
			Path:          firstNonEmpty(e.Path, e.TargetPath),
			Diff:          e.Diff,
			Lang:          e.Lang,
			PendingReview: true,
		}
		c.upsertRecentDiffLocked(c.recentDiff)
		c.recentDiff = c.pickActiveRecentDiffLocked()
		return []any{c.newAgentStateEvent(message, false)}
	case protocol.LogEvent:
		if e.ResumeSessionID != "" {
			c.resumeSession = e.ResumeSessionID
		}
		if lifecycle := normalizeClaudeLifecycle(e.RuntimeMeta.ClaudeLifecycle); lifecycle != "" {
			c.claudeLifecycle = lifecycle
			c.activeMeta.ClaudeLifecycle = lifecycle
		}
		if e.Message == c.lastLogMsg && now.Sub(c.lastLogTime) < 300*time.Millisecond {
			return nil
		}
		c.lastLogMsg = e.Message
		c.lastLogTime = now
		if isAICommand(c.currentCommand) && c.currentState == ControllerStateThinking && isAIPrompt(e.Message) {
			c.currentState = ControllerStateWaitInput
			if isClaudeCommand(c.currentCommand) {
				c.claudeLifecycle = "waiting_input"
				c.activeMeta.ClaudeLifecycle = c.claudeLifecycle
			}
			message := e.Message
			if strings.TrimSpace(message) == "" {
				message = "等待输入"
			}
			return []any{c.newAgentStateEvent(message, true)}
		}
		return nil
	case protocol.SessionStateEvent:
		if e.ResumeSessionID != "" {
			c.resumeSession = e.ResumeSessionID
		}
		if lifecycle := normalizeClaudeLifecycle(e.RuntimeMeta.ClaudeLifecycle); lifecycle != "" {
			c.claudeLifecycle = lifecycle
			c.activeMeta.ClaudeLifecycle = lifecycle
		}
		return nil
	default:
		return nil
	}
}

func (c *Controller) OnInputSent(meta protocol.RuntimeMeta) []any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if meta.Source != "" || meta.SkillName != "" || meta.ResumeSessionID != "" || meta.ContextID != "" || meta.ContextTitle != "" || meta.TargetText != "" || meta.TargetPath != "" {
		c.activeMeta = protocol.MergeRuntimeMeta(c.activeMeta, meta)
	}
	if meta.Source == "review-decision" {
		targetID := strings.TrimSpace(meta.ContextID)
		targetPath := strings.TrimSpace(meta.TargetPath)
		switch strings.TrimSpace(meta.TargetText) {
		case "accept", "revert":
			c.markRecentDiffPendingLocked(targetID, targetPath, false)
		case "revise":
			c.markRecentDiffPendingLocked(targetID, targetPath, true)
		}
		c.recentDiff = c.pickActiveRecentDiffLocked()
	}
	if meta.PermissionMode != "" {
		c.activeMeta.PermissionMode = meta.PermissionMode
	}
	if lifecycle := normalizeClaudeLifecycle(meta.ClaudeLifecycle); lifecycle != "" {
		c.claudeLifecycle = lifecycle
	} else if isClaudeCommand(c.currentCommand) {
		c.claudeLifecycle = "active"
	}
	c.activeMeta.ClaudeLifecycle = c.claudeLifecycle
	c.currentState = ControllerStateThinking
	message := "思考中"
	if meta.Source == "permission-decision" {
		message = "根据权限决策继续处理中"
	}
	return []any{c.newAgentStateEvent(message, false)}
}

func (c *Controller) OnCommandFinished(meta protocol.RuntimeMeta) []any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if meta.Source != "" || meta.SkillName != "" || meta.ResumeSessionID != "" || meta.ContextID != "" || meta.ContextTitle != "" || meta.TargetText != "" || meta.TargetPath != "" {
		c.activeMeta = protocol.MergeRuntimeMeta(c.activeMeta, meta)
	}
	if c.currentState == ControllerStateWaitInput {
		message := c.lastPromptMsg
		if strings.TrimSpace(message) == "" {
			message = "等待输入"
		}
		c.refreshClaudeLifecycleLocked()
		return []any{c.newAgentStateEvent(message, true)}
	}
	c.currentState = ControllerStateIdle
	c.currentCommand = ""
	c.lastStep = ""
	c.lastTool = ""
	c.lastLogMsg = ""
	c.lastStepMsg = ""
	c.lastStepStatus = ""
	c.lastPromptMsg = ""
	if lifecycle := normalizeClaudeLifecycle(meta.ClaudeLifecycle); lifecycle != "" {
		c.claudeLifecycle = lifecycle
	} else if c.resumeSession != "" {
		c.claudeLifecycle = "resumable"
	} else {
		c.claudeLifecycle = "inactive"
	}
	c.activeMeta.ClaudeLifecycle = c.claudeLifecycle
	message := "空闲"
	if c.resumeSession != "" {
		message = "会话已暂停，可继续对话"
	}
	return []any{c.newAgentStateEvent(message, false)}
}

func (c *Controller) RecentDiff() DiffContext {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.recentDiff
}

func (c *Controller) RecentDiffs() []DiffContext {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]DiffContext(nil), c.recentDiffs...)
}

func (c *Controller) Snapshot() ControllerSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return ControllerSnapshot{
		SessionID:       c.sessionID,
		State:           c.currentState,
		CurrentCommand:  c.currentCommand,
		LastStep:        c.lastStep,
		LastTool:        c.lastTool,
		ResumeSession:   c.resumeSession,
		ClaudeLifecycle: c.claudeLifecycle,
		LastUserInput:   c.lastUserInput,
		ActiveMeta:      c.activeMeta,
		RecentDiffs:     append([]DiffContext(nil), c.recentDiffs...),
		RecentDiff:      c.recentDiff,
	}
}

func (c *Controller) Restore(snapshot ControllerSnapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if snapshot.SessionID != "" {
		c.sessionID = snapshot.SessionID
	}
	c.currentState = snapshot.State
	c.currentCommand = snapshot.CurrentCommand
	c.lastStep = snapshot.LastStep
	c.lastTool = snapshot.LastTool
	c.resumeSession = snapshot.ResumeSession
	c.claudeLifecycle = firstNonEmpty(normalizeClaudeLifecycle(snapshot.ClaudeLifecycle), normalizeClaudeLifecycle(snapshot.ActiveMeta.ClaudeLifecycle))
	c.lastUserInput = snapshot.LastUserInput
	c.activeMeta = snapshot.ActiveMeta
	if c.claudeLifecycle != "" {
		c.activeMeta.ClaudeLifecycle = c.claudeLifecycle
	}
	c.recentDiffs = append([]DiffContext(nil), snapshot.RecentDiffs...)
	c.recentDiff = snapshot.RecentDiff
	if len(c.recentDiffs) == 0 && strings.TrimSpace(c.recentDiff.ContextID+c.recentDiff.Path+c.recentDiff.Title) != "" {
		c.recentDiffs = []DiffContext{c.recentDiff}
	}
	c.recentDiff = c.pickActiveRecentDiffLocked()
}

func (c *Controller) newAgentStateEvent(message string, awaitInput bool) protocol.AgentStateEvent {
	event := protocol.NewAgentStateEvent(c.sessionID, string(c.currentState), message, awaitInput, c.currentCommand, c.lastStep, c.lastTool)
	c.refreshClaudeLifecycleLocked()
	event.RuntimeMeta = protocol.MergeRuntimeMeta(c.activeMeta, protocol.RuntimeMeta{
		ResumeSessionID: c.resumeSession,
		ClaudeLifecycle: c.claudeLifecycle,
	})
	return event
}

func extractResumeSessionID(command string, fallback string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	for i := 0; i < len(fields); i++ {
		if fields[i] == "--resume" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return fallback
}

func isAIPrompt(message string) bool {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return false
	}
	// 匹配 Gemini 的多种提示符状态
	return strings.Contains(trimmed, ">   Type your message") ||
		trimmed == ">" ||
		strings.HasSuffix(trimmed, " >") ||
		strings.HasSuffix(trimmed, "\n>")
}

func isAICommand(command string) bool {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return false
	}
	head := strings.ToLower(fields[0])
	isClaude := head == "claude" || strings.HasSuffix(head, "/claude") || strings.HasSuffix(head, `\\claude`) || head == "claude.exe"
	isGemini := head == "gemini" || strings.HasSuffix(head, "/gemini") || strings.HasSuffix(head, `\\gemini`) || head == "gemini.exe"
	isCodex := head == "codex" || strings.HasSuffix(head, "/codex") || strings.HasSuffix(head, `\\codex`) || head == "codex.exe"
	return isClaude || isGemini || isCodex
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (c *Controller) upsertRecentDiffLocked(diff DiffContext) {
	keyID := strings.TrimSpace(diff.ContextID)
	keyPath := strings.TrimSpace(diff.Path)
	for i := range c.recentDiffs {
		item := c.recentDiffs[i]
		if (keyID != "" && strings.TrimSpace(item.ContextID) == keyID) || (keyPath != "" && strings.TrimSpace(item.Path) == keyPath) {
			c.recentDiffs[i] = diff
			return
		}
	}
	c.recentDiffs = append(c.recentDiffs, diff)
}

func (c *Controller) markRecentDiffPendingLocked(contextID, targetPath string, pending bool) {
	matched := false
	for i := range c.recentDiffs {
		item := &c.recentDiffs[i]
		if (contextID != "" && strings.TrimSpace(item.ContextID) == contextID) || (targetPath != "" && strings.TrimSpace(item.Path) == targetPath) {
			item.PendingReview = pending
			matched = true
		}
	}
	if !matched && len(c.recentDiffs) == 1 {
		c.recentDiffs[0].PendingReview = pending
		matched = true
	}
	if matched {
		for _, item := range c.recentDiffs {
			if (contextID != "" && strings.TrimSpace(item.ContextID) == contextID) || (targetPath != "" && strings.TrimSpace(item.Path) == targetPath) || (len(c.recentDiffs) == 1) {
				c.recentDiff = item
				break
			}
		}
	}
	if (contextID != "" && strings.TrimSpace(c.recentDiff.ContextID) == contextID) || (targetPath != "" && strings.TrimSpace(c.recentDiff.Path) == targetPath) || (len(c.recentDiffs) == 1) {
		c.recentDiff.PendingReview = pending
	}
}

func (c *Controller) pickActiveRecentDiffLocked() DiffContext {
	for i := len(c.recentDiffs) - 1; i >= 0; i-- {
		if c.recentDiffs[i].PendingReview {
			return c.recentDiffs[i]
		}
	}
	if len(c.recentDiffs) > 0 {
		return c.recentDiffs[len(c.recentDiffs)-1]
	}
	return DiffContext{}
}
