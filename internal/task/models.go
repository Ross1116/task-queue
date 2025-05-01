package task

import (
	"database/sql"
	"time"
)

type TaskMessage struct {
	TaskID string `json:"task_id"`
	Input  string `json:"input"`
}

type TaskStatusResponse struct {
	TaskID    string         `json:"task_id"`
	Status    string         `json:"status"`
	Input     string         `json:"input,omitempty"`
	Result    sql.NullString `json:"result"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type CreateTaskRequest struct {
	Input string `json:"input" binding:"required"`
}

type CreateTaskResponse struct {
	TaskID string `json:"task_id"`
}
