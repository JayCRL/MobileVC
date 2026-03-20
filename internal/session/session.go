package session

import (
	"strings"
	"sync"

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

type Controller struct {
	mu             sync.Mutex
	sessionID      string
	currentState   ControllerState
	currentCommand string
	lastStep       string
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
	return c.newAgentStateEvent("空闲", false, "", "")
}

func (c *Controller) OnExecStart(command string) []any {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.currentCommand = command
	c.currentState = ControllerStateThinking
	c.lastStep = ""
	return []any{c.newAgentStateEvent("思考中", false, "", "")}
}

func (c *Controller) OnRunnerEvent(event any) []any {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch e := event.(type) {
	case protocol.PromptRequestEvent:
		c.currentState = ControllerStateWaitInput
		message := e.Message
		if message == "" {
			message = "等待输入"
		}
		return []any{c.newAgentStateEvent(message, true, c.lastStep, "")}
	case protocol.StepUpdateEvent:
		if e.Message != "" {
			c.lastStep = e.Message
		}
		c.currentState = ControllerStateRunningTool
		message := e.Message
		if message == "" {
			message = "执行工具中"
		}
		return []any{c.newAgentStateEvent(message, false, c.lastStep, "")}
	case protocol.FileDiffEvent:
		message := "执行工具中"
		if e.Title != "" {
			message = e.Title
		}
		c.currentState = ControllerStateRunningTool
		return []any{c.newAgentStateEvent(message, false, c.lastStep, "")}
	case protocol.LogEvent:
		if isClaudeCommand(c.currentCommand) && c.currentState == ControllerStateThinking && strings.TrimSpace(e.Message) != "" {
			c.currentState = ControllerStateWaitInput
			return []any{c.newAgentStateEvent("Claude 会话已就绪，可继续输入", true, c.lastStep, "")}
		}
		return nil
	default:
		return nil
	}
}

func (c *Controller) OnInputSent() []any {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.currentState = ControllerStateThinking
	return []any{c.newAgentStateEvent("思考中", false, c.lastStep, "")}
}

func (c *Controller) OnCommandFinished() []any {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.currentState = ControllerStateIdle
	c.currentCommand = ""
	c.lastStep = ""
	return []any{c.newAgentStateEvent("空闲", false, "", "")}
}

func (c *Controller) newAgentStateEvent(message string, awaitInput bool, step, tool string) protocol.AgentStateEvent {
	return protocol.NewAgentStateEvent(c.sessionID, string(c.currentState), message, awaitInput, c.currentCommand, step, tool)
}

func isClaudeCommand(command string) bool {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return false
	}
	head := strings.ToLower(fields[0])
	return head == "claude" || strings.HasSuffix(head, "/claude") || strings.HasSuffix(head, `\\claude`) || head == "claude.exe"
}
