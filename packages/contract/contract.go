// Package contract 是 MobileVC 两条产品线共用的移动端交互契约。
//
// 权威来源：apps/dsh/plugin/index.mjs 的事件名。
// 本文件对应 packages/contract/contract.ts，两边字段必须一一对应。
// 只增不改：加字段可以，改语义不行；接收方遇到未知 Type 必须忽略。
package contract

// SessionStatus 会话状态
type SessionStatus string

const (
	SessionIdle            SessionStatus = "idle"
	SessionRunning         SessionStatus = "running"
	SessionWaitingApproval SessionStatus = "waiting-approval"
	SessionError           SessionStatus = "error"
)

// PromptMode 发送模式：排队 or 立即插话
type PromptMode string

const (
	PromptQueue PromptMode = "queue"
	PromptSteer PromptMode = "steer"
)

// ApprovalDecision 审批决定
type ApprovalDecision string

const (
	ApprovalAllowOnce ApprovalDecision = "allow-once"
	ApprovalAlways    ApprovalDecision = "always"
	ApprovalDeny      ApprovalDecision = "deny"
)

// SessionRef 会话引用
type SessionRef struct {
	SessionID string        `json:"sessionId"`
	Title     string        `json:"title"`
	Status    SessionStatus `json:"status"`
	UpdatedAt string        `json:"updatedAt"`
	Live      bool          `json:"live,omitempty"`
}

// ToolCall 工具调用
type ToolCall struct {
	CallID string `json:"callId"`
	Name   string `json:"name"`
	Args   string `json:"args,omitempty"`
	Status string `json:"status"` // pending | ok | error
}

// DiffFile 单个文件的差异
type DiffFile struct {
	Path   string `json:"path"`
	Status string `json:"status"` // added | modified | deleted
	Hunks  string `json:"hunks"`
}

// ApprovalRequest 审批请求
type ApprovalRequest struct {
	CallID    string             `json:"callId"`
	Kind      string             `json:"kind"`
	Title     string             `json:"title"`
	Detail    string             `json:"detail,omitempty"`
	Actions   []ApprovalDecision `json:"actions,omitempty"`
	CreatedAt string             `json:"createdAt"`
}

// Event 服务端 → 客户端的事件信封。
// Type 取值见 packages/contract/events.md。
type Event struct {
	Type      string       `json:"type"`
	SessionID string       `json:"sessionId,omitempty"`
	TurnID    string       `json:"turnId,omitempty"`
	Text      string       `json:"text,omitempty"`
	Kind      string       `json:"kind,omitempty"`
	Reason    string       `json:"reason,omitempty"`
	Session   *SessionRef  `json:"session,omitempty"`
	Call      *ToolCall    `json:"call,omitempty"`
	Output    string       `json:"output,omitempty"`
	Request   *ApprovalRequest `json:"request,omitempty"`
	CallID    string       `json:"callId,omitempty"`
	Decision  ApprovalDecision `json:"decision,omitempty"`
	Summary   string       `json:"summary,omitempty"`
}

// Command 客户端 → 服务端的指令信封
type Command struct {
	Type        string           `json:"type"`
	SessionID   string           `json:"sessionId"`
	Text        string           `json:"text,omitempty"`
	Mode        PromptMode       `json:"mode,omitempty"`
	Attachments []string         `json:"attachments,omitempty"`
	CallID      string           `json:"callId,omitempty"`
	Decision    ApprovalDecision `json:"decision,omitempty"`
}
