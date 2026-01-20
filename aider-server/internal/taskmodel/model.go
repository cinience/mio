package taskmodel

import "time"

type Status string

const (
	StatusPending     Status = "pending"
	StatusRunning     Status = "running"
	StatusCompleted   Status = "completed"
	StatusFailed      Status = "failed"
	StatusInterrupted Status = "interrupted"
)

type Protocol string

const (
	ProtocolPTY       Protocol = "pty"
	ProtocolAppServer Protocol = "app-server"
)

type Task struct {
	ID         string            `json:"id"`
	Client     string            `json:"client"`
	Protocol   Protocol          `json:"protocol,omitempty"`
	Command    string            `json:"command"`
	Args       []string          `json:"args"`
	Env        map[string]string `json:"env,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
	RunnerID   string            `json:"runner_id,omitempty"`
	Status     Status            `json:"status"`
	CreatedAt  time.Time         `json:"created_at"`
	StartedAt  *time.Time        `json:"started_at,omitempty"`
	FinishedAt *time.Time        `json:"finished_at,omitempty"`
	ExitCode   *int              `json:"exit_code,omitempty"`
	Error      string            `json:"error,omitempty"`
	WorkDir    string            `json:"work_dir,omitempty"`
}

type CreateRequest struct {
	Client      string            `json:"client"`
	Protocol    Protocol          `json:"protocol"`
	Args        []string          `json:"args"`
	Env         map[string]string `json:"env"`
	Labels      map[string]string `json:"labels"`
	Prompt      string            `json:"prompt"`
	Interactive bool              `json:"interactive"`
}

type ListFilter struct {
	Status Status
	Labels map[string]string
	From   *time.Time
	To     *time.Time
	Limit  int
	Offset int
}

type LogEntry struct {
	TaskID string    `json:"task_id"`
	Seq    int64     `json:"seq"`
	TS     time.Time `json:"ts"`
	Stream string    `json:"stream"`
	Line   string    `json:"line"`
}
