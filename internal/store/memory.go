package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

type Memory struct {
	mu          sync.Mutex
	devices     map[string]time.Time
	screenshots map[string]Screenshot
	runs        map[string]map[string]any
	steps       []map[string]any
	tools       []map[string]any
	llmCalls    []map[string]any
	tasks       []Task
	nextStepID  int64
	nextToolID  int64
	nextLLMID   int64
	nextTaskID  int64
}

func NewMemory() *Memory {
	return &Memory{
		devices:     map[string]time.Time{},
		screenshots: map[string]Screenshot{},
		runs:        map[string]map[string]any{},
	}
}

func (m *Memory) EnsureDevice(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.devices[id]; !ok {
		m.devices[id] = time.Now()
	}
	return nil
}

func (m *Memory) CreateScreenshot(ctx context.Context, shot Screenshot) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	shot.CreatedAt = now
	shot.UpdatedAt = now
	m.screenshots[shot.ID] = shot
	return nil
}

func (m *Memory) UpdateScreenshotInsight(ctx context.Context, id, status, ocrText, summary, category, tagsText, explanation string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	shot, ok := m.screenshots[id]
	if !ok {
		return sql.ErrNoRows
	}
	shot.Status = status
	shot.OCRText = ocrText
	shot.Summary = summary
	shot.Category = category
	shot.TagsText = tagsText
	shot.Explanation = explanation
	shot.UpdatedAt = time.Now()
	m.screenshots[id] = shot
	return nil
}

func (m *Memory) GetScreenshot(ctx context.Context, id string) (*Screenshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	shot, ok := m.screenshots[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return &shot, nil
}

func (m *Memory) ListScreenshots(ctx context.Context, deviceID string, limit int) ([]Screenshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var results []Screenshot
	for _, shot := range m.screenshots {
		if shot.DeviceID == deviceID {
			results = append(results, shot)
		}
	}
	sortScreenshots(results)
	return limitScreenshots(results, limit), nil
}

func (m *Memory) SearchScreenshots(ctx context.Context, deviceID, query string, limit int) ([]Screenshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	query = strings.ToLower(query)
	var results []Screenshot
	for _, shot := range m.screenshots {
		if shot.DeviceID != deviceID {
			continue
		}
		haystack := strings.ToLower(strings.Join([]string{
			shot.OriginalFilename,
			shot.OCRText,
			shot.Summary,
			shot.Category,
			shot.TagsText,
			shot.Explanation,
		}, " "))
		if strings.Contains(haystack, query) {
			results = append(results, shot)
		}
	}
	sortScreenshots(results)
	return limitScreenshots(results, limit), nil
}

func (m *Memory) GetScreenshotsByIDs(ctx context.Context, ids []string) ([]Screenshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	results := make([]Screenshot, 0, len(ids))
	for _, id := range ids {
		if shot, ok := m.screenshots[id]; ok {
			results = append(results, shot)
		}
	}
	return results, nil
}

func (m *Memory) ListOrganizedScreenshots(ctx context.Context, limit, offset int) ([]Screenshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var results []Screenshot
	for _, shot := range m.screenshots {
		if shot.Status == "organized" || shot.Status == "partial" {
			results = append(results, shot)
		}
	}
	sortScreenshotsAsc(results)
	if offset >= len(results) {
		return nil, nil
	}
	results = results[offset:]
	return limitScreenshots(results, limit), nil
}

func sortScreenshotsAsc(items []Screenshot) {
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j].CreatedAt.Before(items[i].CreatedAt) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}

func (m *Memory) CreateAgentRun(ctx context.Context, id, deviceID, screenshotID, runType, requestID string, input any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs[id] = map[string]any{
		"id":            id,
		"request_id":    requestID,
		"device_id":     deviceID,
		"screenshot_id": screenshotID,
		"type":          runType,
		"status":        "running",
		"input_json":    toJSON(input),
		"output_json":   "",
		"error_message": nil,
		"duration_ms":   int64(0),
		"created_at":    time.Now().Format(time.RFC3339),
		"updated_at":    time.Now().Format(time.RFC3339),
	}
	return nil
}

func (m *Memory) FinishAgentRun(ctx context.Context, id, status string, output any, errMessage string, duration time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	run, ok := m.runs[id]
	if !ok {
		return sql.ErrNoRows
	}
	run["status"] = status
	run["output_json"] = toJSON(output)
	run["error_message"] = nilIfEmpty(errMessage)
	run["duration_ms"] = duration.Milliseconds()
	run["updated_at"] = time.Now().Format(time.RFC3339)
	return nil
}

func (m *Memory) AddStep(ctx context.Context, runID, name, status string, input, output any, errMessage string, duration time.Duration) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextStepID++
	id := m.nextStepID
	m.steps = append(m.steps, map[string]any{
		"id":            id,
		"run_id":        runID,
		"name":          name,
		"status":        status,
		"input_json":    toJSON(input),
		"output_json":   toJSON(output),
		"error_message": nilIfEmpty(errMessage),
		"duration_ms":   duration.Milliseconds(),
		"created_at":    time.Now().Format(time.RFC3339),
	})
	return id, nil
}

func (m *Memory) AddToolCall(ctx context.Context, runID string, stepID int64, toolName string, input, output any, errMessage string, duration time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextToolID++
	m.tools = append(m.tools, map[string]any{
		"id":            m.nextToolID,
		"run_id":        runID,
		"step_id":       stepID,
		"tool_name":     toolName,
		"input_json":    toJSON(input),
		"output_json":   toJSON(output),
		"error_message": nilIfEmpty(errMessage),
		"duration_ms":   duration.Milliseconds(),
		"created_at":    time.Now().Format(time.RFC3339),
	})
	return nil
}

func (m *Memory) AddLLMCall(ctx context.Context, runID, provider, model, prompt, response string, promptTokens, completionTokens int, cost float64, duration time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextLLMID++
	m.llmCalls = append(m.llmCalls, map[string]any{
		"id":                 m.nextLLMID,
		"run_id":             runID,
		"provider":           provider,
		"model":              model,
		"prompt":             prompt,
		"response":           response,
		"prompt_tokens":      promptTokens,
		"completion_tokens":  completionTokens,
		"estimated_cost_usd": cost,
		"duration_ms":        duration.Milliseconds(),
		"created_at":         time.Now().Format(time.RFC3339),
	})
	return nil
}

func (m *Memory) GetStats(ctx context.Context, query StatsQuery) (*Stats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	since := memTimeRangeSince(query.TimeRange)
	stats := &Stats{TimeRange: query.TimeRange}

	// Agent 运行统计
	var durations []int64
	for _, run := range m.runs {
		createdStr, _ := run["created_at"].(string)
		created, _ := time.Parse(time.RFC3339, createdStr)
		if created.Before(since) {
			continue
		}
		stats.Agent.Total++
		status := asStr(run["status"])
		switch status {
		case "completed":
			stats.Agent.Completed++
		case "partial_success":
			stats.Agent.PartialSuccess++
		case "failed":
			stats.Agent.Failed++
		}
		if status == "completed" || status == "partial_success" {
			if d, ok := run["duration_ms"].(int64); ok {
				durations = append(durations, d)
			}
		}
	}
	if len(durations) > 0 {
		var sum int64
		for _, d := range durations {
			sum += d
		}
		stats.Agent.AvgDurationMs = sum / int64(len(durations))
		sortInt64s(durations)
		p95Idx := int(float64(len(durations)) * 0.95)
		if p95Idx >= len(durations) {
			p95Idx = len(durations) - 1
		}
		stats.Agent.P95DurationMs = durations[p95Idx]
	}
	finished := stats.Agent.Completed + stats.Agent.PartialSuccess + stats.Agent.Failed
	if finished > 0 {
		stats.Agent.SuccessRate = float64(stats.Agent.Completed) / float64(finished)
	}

	// LLM 统计
	for _, lc := range m.llmCalls {
		createdStr, _ := lc["created_at"].(string)
		created, _ := time.Parse(time.RFC3339, createdStr)
		if created.Before(since) {
			continue
		}
		stats.LLM.TotalCalls++
		if pt, ok := lc["prompt_tokens"].(int); ok {
			stats.LLM.TotalTokens += pt
		}
		if ct, ok := lc["completion_tokens"].(int); ok {
			stats.LLM.TotalTokens += ct
		}
		if cost, ok := lc["estimated_cost_usd"].(float64); ok {
			stats.LLM.TotalCostUSD += cost
		}
	}
	successRuns := stats.Agent.Completed + stats.Agent.PartialSuccess
	if successRuns > 0 {
		stats.LLM.AvgCostPerRun = stats.LLM.TotalCostUSD / float64(successRuns)
	}

	// OCR 空文本统计
	for _, shot := range m.screenshots {
		if shot.CreatedAt.Before(since) {
			continue
		}
		stats.OCR.TotalProcessed++
		if shot.OCRText == "" || strings.HasPrefix(shot.OCRText, "OCR 未能识别") {
			stats.OCR.EmptyTextCount++
		}
	}
	if stats.OCR.TotalProcessed > 0 {
		stats.OCR.EmptyTextRate = float64(stats.OCR.EmptyTextCount) / float64(stats.OCR.TotalProcessed)
	}

	// 队列状态
	for _, t := range m.tasks {
		switch t.Status {
		case "queued":
			stats.Queue.Queued++
		case "running":
			stats.Queue.Running++
		case "dead":
			stats.Queue.Dead++
		}
	}

	// 截图总览
	for _, shot := range m.screenshots {
		if shot.CreatedAt.Before(since) {
			continue
		}
		stats.Screenshots.Total++
		switch shot.Status {
		case "organized":
			stats.Screenshots.Organized++
		case "partial":
			stats.Screenshots.Partial++
		case "failed":
			stats.Screenshots.Failed++
		}
	}

	return stats, nil
}

func memTimeRangeSince(r string) time.Time {
	now := time.Now()
	switch r {
	case "week":
		return now.AddDate(0, 0, -7)
	case "month":
		return now.AddDate(0, -1, 0)
	case "all":
		return time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	default:
		y, m, d := now.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	}
}

func sortInt64s(a []int64) {
	for i := 0; i < len(a); i++ {
		for j := i + 1; j < len(a); j++ {
			if a[j] < a[i] {
				a[i], a[j] = a[j], a[i]
			}
		}
	}
}

func (m *Memory) GetAgentRun(ctx context.Context, runID string) (*AgentRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run, ok := m.runs[runID]
	if !ok {
		return nil, sql.ErrNoRows
	}
	inputJSON, _ := run["input_json"].(string)
	return &AgentRun{
		ID:           runID,
		RequestID:    asStr(run["request_id"]),
		DeviceID:     asStr(run["device_id"]),
		ScreenshotID: asStr(run["screenshot_id"]),
		Type:         asStr(run["type"]),
		Status:       asStr(run["status"]),
		InputJSON:    inputJSON,
	}, nil
}

func (m *Memory) EnqueueTask(ctx context.Context, runID string, maxAttempts int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextTaskID++
	m.tasks = append(m.tasks, Task{
		ID:          m.nextTaskID,
		RunID:       runID,
		Status:      "queued",
		Attempts:    0,
		MaxAttempts: maxAttempts,
	})
	return nil
}

func (m *Memory) DequeueTask(ctx context.Context, workerID string) (*Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.tasks {
		if m.tasks[i].Status == "queued" {
			m.tasks[i].Status = "running"
			m.tasks[i].Attempts++
			task := m.tasks[i]
			return &task, nil
		}
	}
	return nil, sql.ErrNoRows
}

func (m *Memory) CompleteTask(ctx context.Context, taskID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.tasks {
		if m.tasks[i].ID == taskID {
			m.tasks[i].Status = "completed"
			return nil
		}
	}
	return sql.ErrNoRows
}

func (m *Memory) RetryTask(ctx context.Context, taskID int64, errMsg string, delay time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.tasks {
		if m.tasks[i].ID == taskID {
			m.tasks[i].Status = "queued"
			return nil
		}
	}
	return sql.ErrNoRows
}

func (m *Memory) MarkTaskDead(ctx context.Context, taskID int64, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.tasks {
		if m.tasks[i].ID == taskID {
			m.tasks[i].Status = "dead"
			return nil
		}
	}
	return sql.ErrNoRows
}

func (m *Memory) ReleaseStaleLockedTasks(ctx context.Context, timeout time.Duration) (int, error) {
	return 0, nil
}

func (m *Memory) GetAgentRunDetail(ctx context.Context, runID string) (*AgentRunDetail, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run, ok := m.runs[runID]
	if !ok {
		return nil, sql.ErrNoRows
	}
	var shot *Screenshot
	if screenshotID, ok := run["screenshot_id"].(string); ok && screenshotID != "" {
		if value, exists := m.screenshots[screenshotID]; exists {
			copied := value
			shot = &copied
		}
	}
	return &AgentRunDetail{
		Run:        copyMap(run),
		Steps:      filterMaps(m.steps, runID),
		Tools:      filterMaps(m.tools, runID),
		LLMCalls:   filterMaps(m.llmCalls, runID),
		Screenshot: shot,
	}, nil
}

func sortScreenshots(items []Screenshot) {
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j].CreatedAt.After(items[i].CreatedAt) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}

func limitScreenshots(items []Screenshot, limit int) []Screenshot {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}

func filterMaps(items []map[string]any, runID string) []map[string]any {
	var results []map[string]any
	for _, item := range items {
		if item["run_id"] == runID {
			results = append(results, copyMap(item))
		}
	}
	return results
}

func copyMap(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func toJSON(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func asStr(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func nilIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
