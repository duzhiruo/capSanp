package store

import (
	"context"
	"time"
)

type Repository interface {
	// 设备
	EnsureDevice(ctx context.Context, id string) error

	// 截图
	CreateScreenshot(ctx context.Context, shot Screenshot) error
	UpdateScreenshotInsight(ctx context.Context, id, status, ocrText, summary, category, tagsText, explanation string) error
	GetScreenshot(ctx context.Context, id string) (*Screenshot, error)
	ListScreenshots(ctx context.Context, deviceID string, limit int) ([]Screenshot, error)
	SearchScreenshots(ctx context.Context, deviceID, query string, limit int) ([]Screenshot, error)
	GetScreenshotsByIDs(ctx context.Context, ids []string) ([]Screenshot, error)
	ListOrganizedScreenshots(ctx context.Context, limit, offset int) ([]Screenshot, error)

	// Agent Run
	CreateAgentRun(ctx context.Context, id, deviceID, screenshotID, runType, requestID string, input any) error
	FinishAgentRun(ctx context.Context, id, status string, output any, errMessage string, duration time.Duration) error
	GetAgentRun(ctx context.Context, runID string) (*AgentRun, error)
	GetAgentRunDetail(ctx context.Context, runID string) (*AgentRunDetail, error)

	// Agent 追踪
	AddStep(ctx context.Context, runID, name, status string, input, output any, errMessage string, duration time.Duration) (int64, error)
	AddToolCall(ctx context.Context, runID string, stepID int64, toolName string, input, output any, errMessage string, duration time.Duration) error
	AddLLMCall(ctx context.Context, runID, provider, model, prompt, response string, promptTokens, completionTokens int, cost float64, duration time.Duration) error

	// 统计
	GetStats(ctx context.Context, query StatsQuery) (*Stats, error)

	// 任务队列
	EnqueueTask(ctx context.Context, runID string, maxAttempts int) error
	DequeueTask(ctx context.Context, workerID string) (*Task, error)
	CompleteTask(ctx context.Context, taskID int64) error
	RetryTask(ctx context.Context, taskID int64, errMsg string, delay time.Duration) error
	MarkTaskDead(ctx context.Context, taskID int64, errMsg string) error
	ReleaseStaleLockedTasks(ctx context.Context, timeout time.Duration) (int, error)
}

// Task 表示一个待执行的 Agent 任务。
type Task struct {
	ID          int64
	RunID       string
	Status      string
	Attempts    int
	MaxAttempts int
}

// StatsQuery 是统计查询参数。
type StatsQuery struct {
	TimeRange string // today / week / month / all
	DeviceID  string
}

// Stats 是统计 API 的返回结构。
type Stats struct {
	TimeRange   string    `json:"time_range"`
	Agent       AgentStats `json:"agent"`
	LLM         LLMStats   `json:"llm"`
	OCR         OCRStats   `json:"ocr"`
	Queue       QueueStats `json:"queue"`
	Screenshots ShotStats  `json:"screenshots"`
}

type AgentStats struct {
	Total          int     `json:"total"`
	Completed      int     `json:"completed"`
	PartialSuccess int     `json:"partial_success"`
	Failed         int     `json:"failed"`
	SuccessRate    float64 `json:"success_rate"`
	AvgDurationMs  int64   `json:"avg_duration_ms"`
	P95DurationMs  int64   `json:"p95_duration_ms"`
}

type LLMStats struct {
	TotalCalls    int     `json:"total_calls"`
	TotalTokens   int     `json:"total_tokens"`
	TotalCostUSD  float64 `json:"total_cost_usd"`
	AvgCostPerRun float64 `json:"avg_cost_per_run"`
}

type OCRStats struct {
	TotalProcessed int     `json:"total_processed"`
	EmptyTextCount int     `json:"empty_text_count"`
	EmptyTextRate  float64 `json:"empty_text_rate"`
}

type QueueStats struct {
	Queued  int `json:"queued"`
	Running int `json:"running"`
	Dead    int `json:"dead"`
}

type ShotStats struct {
	Total     int `json:"total"`
	Organized int `json:"organized"`
	Partial   int `json:"partial"`
	Failed    int `json:"failed"`
}

// AgentRun 表示一次 Agent 执行记录。
type AgentRun struct {
	ID           string
	RequestID    string
	DeviceID     string
	ScreenshotID string
	Type         string
	Status       string
	InputJSON    string
}
