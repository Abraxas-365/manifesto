package jobx

import (
	"encoding/json"
	"time"
)

// JobStatus represents the current state of a job.
type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusActive    JobStatus = "active"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
	JobStatusRetrying  JobStatus = "retrying"
)

// Job represents a unit of work to be enqueued.
type Job struct {
	Type    string          `json:"type"`
	Queue   string          `json:"queue"`
	Payload json.RawMessage `json:"payload"`

	// MaxRetries is the maximum number of retry attempts. Default is 3.
	MaxRetries int `json:"max_retries"`
}

// JobInfo is the full representation of a job stored in the backend.
type JobInfo struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Queue      string          `json:"queue"`
	Payload    json.RawMessage `json:"payload"`
	Status     JobStatus       `json:"status"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
	MaxRetries int             `json:"max_retries"`
	Attempts   int             `json:"attempts"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}
