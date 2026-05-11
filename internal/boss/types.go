package boss

// TaskStatus tracks a single task in a boss plan.
type TaskStatus string

const (
	TaskPending TaskStatus = "pending"
	TaskRunning TaskStatus = "running"
	TaskDone    TaskStatus = "done"
	TaskFailed  TaskStatus = "failed"
)

// WorkerType selects the engine for a task.
type WorkerType string

const (
	WorkerClaude WorkerType = "claude"
	WorkerCodex  WorkerType = "codex"
)

// Task represents a single unit of work in a boss plan.
type Task struct {
	ID      string     `json:"id"`
	Title   string     `json:"title"`
	Agent   string     `json:"agent"`
	Status  TaskStatus `json:"status"`
	Worker  WorkerType `json:"worker"`
	Depends []string   `json:"depends,omitempty"` // task IDs this task depends on
	Group   string     `json:"group,omitempty"`   // parallel group name
}

// Plan is a collection of tasks with ordering.
type Plan struct {
	Tasks       []*Task `json:"tasks"`
	CurrentTask string  `json:"currentTask,omitempty"`
}
