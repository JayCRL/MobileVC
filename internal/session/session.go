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
}

type Controller struct {
	mu             sync.Mutex
	sessionID      string
	currentState   ControllerState
	currentCommand string
	lastStep       string
	lastTool       string
	resumeSession  string
	activeMeta     protocol.RuntimeMeta
	recentDiff     DiffContext

	// dedup fields
	lastLogMsg     string
	lastLogTime    time.Time
	lastStepMsg    string
	lastStepStatus string
	lastPromptMsg  string
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
	message := "思考中"
	if meta.SkillName != "" {
		message = "执行 skill：" + meta.SkillName
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
		return []any{c.newAgentStateEvent(message, false)}
	case protocol.LogEvent:
		if e.ResumeSessionID != "" {
			c.resumeSession = e.ResumeSessionID
		}
		// dedup: skip identical log within 300ms
		if e.Message == c.lastLogMsg && now.Sub(c.lastLogTime) < 300*time.Millisecond {
			return nil
		}
		c.lastLogMsg = e.Message
		c.lastLogTime = now
		if isAICommand(c.currentCommand) && c.currentState == ControllerStateThinking && isAIPrompt(e.Message) {
			c.currentState = ControllerStateWaitInput
			return []any{c.newAgentStateEvent("AI 会话已就绪，可继续输入", true)}
		}
		return nil
	case protocol.SessionStateEvent:
		if e.ResumeSessionID != "" {
			c.resumeSession = e.ResumeSessionID
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
	if meta.Source == "review-decision" && c.recentDiff.PendingReview {
		switch strings.TrimSpace(meta.TargetText) {
		case "accept", "revert":
			c.recentDiff.PendingReview = false
		case "revise":
			c.recentDiff.PendingReview = true
		}
	}
	c.currentState = ControllerStateThinking
	return []any{c.newAgentStateEvent("思考中", false)}
}

func (c *Controller) OnCommandFinished(meta protocol.RuntimeMeta) []any {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.currentState = ControllerStateIdle
	c.currentCommand = ""
	c.lastStep = ""
	c.lastTool = ""
	c.lastLogMsg = ""
	c.lastStepMsg = ""
	c.lastStepStatus = ""
	c.lastPromptMsg = ""
	if meta.Source != "" || meta.SkillName != "" || meta.ResumeSessionID != "" || meta.ContextID != "" || meta.ContextTitle != "" || meta.TargetText != "" || meta.TargetPath != "" {
		c.activeMeta = protocol.MergeRuntimeMeta(c.activeMeta, meta)
	}
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

func (c *Controller) newAgentStateEvent(message string, awaitInput bool) protocol.AgentStateEvent {
	event := protocol.NewAgentStateEvent(c.sessionID, string(c.currentState), message, awaitInput, c.currentCommand, c.lastStep, c.lastTool)
	event.RuntimeMeta = protocol.MergeRuntimeMeta(c.activeMeta, protocol.RuntimeMeta{ResumeSessionID: c.resumeSession})
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
	return isClaude || isGemini
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
