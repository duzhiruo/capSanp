package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

type Store struct {
	db *sql.DB
}

type Screenshot struct {
	ID               string    `json:"id"`
	DeviceID         string    `json:"device_id"`
	OriginalFilename string    `json:"original_filename"`
	StoragePath      string    `json:"storage_path"`
	Status           string    `json:"status"`
	OCRText          string    `json:"ocr_text"`
	Summary          string    `json:"summary"`
	Category         string    `json:"category"`
	TagsText         string    `json:"tags_text"`
	Explanation      string    `json:"explanation"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type AgentRunDetail struct {
	Run        map[string]any   `json:"run"`
	Steps      []map[string]any `json:"steps"`
	Tools      []map[string]any `json:"tool_calls"`
	LLMCalls   []map[string]any `json:"llm_calls"`
	Screenshot *Screenshot      `json:"screenshot,omitempty"`
}

func Open(dsn string) (*Store, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) ApplyMigration(ctx context.Context, path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	for _, statement := range strings.Split(string(content), ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("执行迁移失败: %w", err)
		}
	}
	return nil
}

func (s *Store) EnsureDevice(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `INSERT IGNORE INTO devices (id) VALUES (?)`, id)
	return err
}

func (s *Store) CreateScreenshot(ctx context.Context, shot Screenshot) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO screenshots (id, device_id, original_filename, storage_path, status, ocr_text)
		VALUES (?, ?, ?, ?, ?, ?)
	`, shot.ID, shot.DeviceID, shot.OriginalFilename, shot.StoragePath, shot.Status, nullString(shot.OCRText))
	return err
}

func (s *Store) UpdateScreenshotInsight(ctx context.Context, id, status, ocrText, summary, category, tagsText, explanation string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE screenshots
		SET status = ?, ocr_text = ?, summary = ?, category = ?, tags_text = ?, explanation = ?
		WHERE id = ?
	`, status, ocrText, summary, category, tagsText, explanation, id)
	return err
}

func (s *Store) GetScreenshot(ctx context.Context, id string) (*Screenshot, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, device_id, original_filename, storage_path, status,
		       COALESCE(ocr_text, ''), COALESCE(summary, ''), COALESCE(category, ''),
		       COALESCE(tags_text, ''), COALESCE(explanation, ''), created_at, updated_at
		FROM screenshots
		WHERE id = ?
	`, id)
	var shot Screenshot
	if err := row.Scan(&shot.ID, &shot.DeviceID, &shot.OriginalFilename, &shot.StoragePath, &shot.Status, &shot.OCRText, &shot.Summary, &shot.Category, &shot.TagsText, &shot.Explanation, &shot.CreatedAt, &shot.UpdatedAt); err != nil {
		return nil, err
	}
	return &shot, nil
}

func (s *Store) ListScreenshots(ctx context.Context, deviceID string, limit int) ([]Screenshot, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, device_id, original_filename, storage_path, status,
		       COALESCE(ocr_text, ''), COALESCE(summary, ''), COALESCE(category, ''),
		       COALESCE(tags_text, ''), COALESCE(explanation, ''), created_at, updated_at
		FROM screenshots
		WHERE device_id = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []Screenshot
	for rows.Next() {
		var shot Screenshot
		if err := rows.Scan(&shot.ID, &shot.DeviceID, &shot.OriginalFilename, &shot.StoragePath, &shot.Status, &shot.OCRText, &shot.Summary, &shot.Category, &shot.TagsText, &shot.Explanation, &shot.CreatedAt, &shot.UpdatedAt); err != nil {
			return nil, err
		}
		results = append(results, shot)
	}
	return results, rows.Err()
}

func (s *Store) SearchScreenshots(ctx context.Context, deviceID, query string, limit int) ([]Screenshot, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, device_id, original_filename, storage_path, status,
		       COALESCE(ocr_text, ''), COALESCE(summary, ''), COALESCE(category, ''),
		       COALESCE(tags_text, ''), COALESCE(explanation, ''), created_at, updated_at
		FROM screenshots
		WHERE device_id = ?
		  AND (
		    MATCH(ocr_text, summary, tags_text) AGAINST (? IN NATURAL LANGUAGE MODE)
		    OR original_filename LIKE ?
		    OR category LIKE ?
		  )
		ORDER BY created_at DESC
		LIMIT ?
	`, deviceID, query, "%"+query+"%", "%"+query+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []Screenshot
	for rows.Next() {
		var shot Screenshot
		if err := rows.Scan(&shot.ID, &shot.DeviceID, &shot.OriginalFilename, &shot.StoragePath, &shot.Status, &shot.OCRText, &shot.Summary, &shot.Category, &shot.TagsText, &shot.Explanation, &shot.CreatedAt, &shot.UpdatedAt); err != nil {
			return nil, err
		}
		results = append(results, shot)
	}
	return results, rows.Err()
}

func (s *Store) GetScreenshotsByIDs(ctx context.Context, ids []string) ([]Screenshot, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, device_id, original_filename, storage_path, status,
		       COALESCE(ocr_text, ''), COALESCE(summary, ''), COALESCE(category, ''),
		       COALESCE(tags_text, ''), COALESCE(explanation, ''), created_at, updated_at
		FROM screenshots
		WHERE id IN (`+strings.Join(placeholders, ",")+`)
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byID := make(map[string]Screenshot, len(ids))
	for rows.Next() {
		var shot Screenshot
		if err := rows.Scan(&shot.ID, &shot.DeviceID, &shot.OriginalFilename, &shot.StoragePath, &shot.Status, &shot.OCRText, &shot.Summary, &shot.Category, &shot.TagsText, &shot.Explanation, &shot.CreatedAt, &shot.UpdatedAt); err != nil {
			return nil, err
		}
		byID[shot.ID] = shot
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 保持输入 ID 顺序（RRF 排序结果）
	results := make([]Screenshot, 0, len(ids))
	for _, id := range ids {
		if shot, ok := byID[id]; ok {
			results = append(results, shot)
		}
	}
	return results, nil
}

func (s *Store) ListOrganizedScreenshots(ctx context.Context, limit, offset int) ([]Screenshot, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, device_id, original_filename, storage_path, status,
		       COALESCE(ocr_text, ''), COALESCE(summary, ''), COALESCE(category, ''),
		       COALESCE(tags_text, ''), COALESCE(explanation, ''), created_at, updated_at
		FROM screenshots
		WHERE status IN ('organized', 'partial')
		ORDER BY created_at ASC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []Screenshot
	for rows.Next() {
		var shot Screenshot
		if err := rows.Scan(&shot.ID, &shot.DeviceID, &shot.OriginalFilename, &shot.StoragePath, &shot.Status, &shot.OCRText, &shot.Summary, &shot.Category, &shot.TagsText, &shot.Explanation, &shot.CreatedAt, &shot.UpdatedAt); err != nil {
			return nil, err
		}
		results = append(results, shot)
	}
	return results, rows.Err()
}

func (s *Store) CreateAgentRun(ctx context.Context, id, deviceID, screenshotID, runType, requestID string, input any) error {
	inputJSON, _ := json.Marshal(input)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_runs (id, request_id, device_id, screenshot_id, type, status, input_json)
		VALUES (?, ?, ?, ?, ?, 'running', ?)
	`, id, nullString(requestID), deviceID, screenshotID, runType, string(inputJSON))
	return err
}

func (s *Store) FinishAgentRun(ctx context.Context, id, status string, output any, errMessage string, duration time.Duration) error {
	outputJSON, _ := json.Marshal(output)
	_, err := s.db.ExecContext(ctx, `
		UPDATE agent_runs
		SET status = ?, output_json = ?, error_message = ?, duration_ms = ?
		WHERE id = ?
	`, status, string(outputJSON), nullString(errMessage), duration.Milliseconds(), id)
	return err
}

func (s *Store) AddStep(ctx context.Context, runID, name, status string, input, output any, errMessage string, duration time.Duration) (int64, error) {
	inputJSON, _ := json.Marshal(input)
	outputJSON, _ := json.Marshal(output)
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_steps (run_id, name, status, input_json, output_json, error_message, duration_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, runID, name, status, string(inputJSON), string(outputJSON), nullString(errMessage), duration.Milliseconds())
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (s *Store) AddToolCall(ctx context.Context, runID string, stepID int64, toolName string, input, output any, errMessage string, duration time.Duration) error {
	inputJSON, _ := json.Marshal(input)
	outputJSON, _ := json.Marshal(output)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tool_calls (run_id, step_id, tool_name, input_json, output_json, error_message, duration_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, runID, stepID, toolName, string(inputJSON), string(outputJSON), nullString(errMessage), duration.Milliseconds())
	return err
}

func (s *Store) AddLLMCall(ctx context.Context, runID, provider, model, prompt, response string, promptTokens, completionTokens int, cost float64, duration time.Duration) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO llm_calls (run_id, provider, model, prompt, response, prompt_tokens, completion_tokens, estimated_cost_usd, duration_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, runID, provider, model, prompt, response, promptTokens, completionTokens, cost, duration.Milliseconds())
	return err
}

func (s *Store) GetStats(ctx context.Context, query StatsQuery) (*Stats, error) {
	since := timeRangeSince(query.TimeRange)
	stats := &Stats{TimeRange: query.TimeRange}

	// Agent 运行统计
	row := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COALESCE(SUM(status = 'completed'), 0),
			COALESCE(SUM(status = 'partial_success'), 0),
			COALESCE(SUM(status = 'failed'), 0),
			COALESCE(AVG(CASE WHEN status IN ('completed','partial_success') THEN duration_ms END), 0)
		FROM agent_runs WHERE created_at >= ?
	`, since)
	var avgF float64
	if err := row.Scan(&stats.Agent.Total, &stats.Agent.Completed, &stats.Agent.PartialSuccess, &stats.Agent.Failed, &avgF); err != nil {
		return nil, err
	}
	stats.Agent.AvgDurationMs = int64(avgF)
	if stats.Agent.Total > 0 {
		finished := stats.Agent.Completed + stats.Agent.PartialSuccess + stats.Agent.Failed
		if finished > 0 {
			stats.Agent.SuccessRate = float64(stats.Agent.Completed) / float64(finished)
		}
	}

	// P95 耗时（近似）
	successCount := stats.Agent.Completed + stats.Agent.PartialSuccess
	if successCount > 0 {
		offset := int(float64(successCount) * 0.95)
		if offset >= successCount {
			offset = successCount - 1
		}
		var p95 int64
		err := s.db.QueryRowContext(ctx, `
			SELECT duration_ms FROM agent_runs
			WHERE status IN ('completed','partial_success') AND created_at >= ?
			ORDER BY duration_ms ASC LIMIT 1 OFFSET ?
		`, since, offset).Scan(&p95)
		if err == nil {
			stats.Agent.P95DurationMs = p95
		}
	}

	// LLM 统计
	row = s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COALESCE(SUM(prompt_tokens + completion_tokens), 0),
			COALESCE(SUM(estimated_cost_usd), 0)
		FROM llm_calls WHERE created_at >= ?
	`, since)
	var costF float64
	if err := row.Scan(&stats.LLM.TotalCalls, &stats.LLM.TotalTokens, &costF); err != nil {
		return nil, err
	}
	stats.LLM.TotalCostUSD = costF
	if stats.Agent.Completed > 0 {
		stats.LLM.AvgCostPerRun = costF / float64(stats.Agent.Completed+stats.Agent.PartialSuccess)
	}

	// OCR 空文本统计
	row = s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COALESCE(SUM(ocr_text IS NULL OR ocr_text = '' OR ocr_text LIKE 'OCR 未能识别%'), 0)
		FROM screenshots WHERE created_at >= ?
	`, since)
	if err := row.Scan(&stats.OCR.TotalProcessed, &stats.OCR.EmptyTextCount); err != nil {
		return nil, err
	}
	if stats.OCR.TotalProcessed > 0 {
		stats.OCR.EmptyTextRate = float64(stats.OCR.EmptyTextCount) / float64(stats.OCR.TotalProcessed)
	}

	// 队列状态（实时）
	row = s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(status = 'queued'), 0),
			COALESCE(SUM(status = 'running'), 0),
			COALESCE(SUM(status = 'dead'), 0)
		FROM agent_tasks
	`)
	if err := row.Scan(&stats.Queue.Queued, &stats.Queue.Running, &stats.Queue.Dead); err != nil {
		return nil, err
	}

	// 截图总览
	row = s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COALESCE(SUM(status = 'organized'), 0),
			COALESCE(SUM(status = 'partial'), 0),
			COALESCE(SUM(status = 'failed'), 0)
		FROM screenshots WHERE created_at >= ?
	`, since)
	if err := row.Scan(&stats.Screenshots.Total, &stats.Screenshots.Organized, &stats.Screenshots.Partial, &stats.Screenshots.Failed); err != nil {
		return nil, err
	}

	return stats, nil
}

func timeRangeSince(r string) time.Time {
	now := time.Now()
	switch r {
	case "week":
		return now.AddDate(0, 0, -7)
	case "month":
		return now.AddDate(0, -1, 0)
	case "all":
		return time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	default: // today
		y, m, d := now.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	}
}

func (s *Store) GetAgentRun(ctx context.Context, runID string) (*AgentRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, device_id, COALESCE(screenshot_id, ''), type, status, COALESCE(input_json, '{}')
		FROM agent_runs WHERE id = ?
	`, runID)
	var run AgentRun
	if err := row.Scan(&run.ID, &run.DeviceID, &run.ScreenshotID, &run.Type, &run.Status, &run.InputJSON); err != nil {
		return nil, err
	}
	return &run, nil
}

func (s *Store) EnqueueTask(ctx context.Context, runID string, maxAttempts int) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_tasks (run_id, status, max_attempts, scheduled_at)
		VALUES (?, 'queued', ?, NOW())
	`, runID, maxAttempts)
	return err
}

func (s *Store) DequeueTask(ctx context.Context, workerID string) (*Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, `
		SELECT id, run_id, attempts, max_attempts
		FROM agent_tasks
		WHERE status = 'queued' AND scheduled_at <= NOW()
		ORDER BY scheduled_at ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED
	`)
	var task Task
	if err := row.Scan(&task.ID, &task.RunID, &task.Attempts, &task.MaxAttempts); err != nil {
		return nil, err
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE agent_tasks
		SET status = 'running', locked_at = NOW(), locked_by = ?, attempts = attempts + 1, updated_at = NOW()
		WHERE id = ?
	`, workerID, task.ID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	task.Status = "running"
	task.Attempts++
	return &task, nil
}

func (s *Store) CompleteTask(ctx context.Context, taskID int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE agent_tasks SET status = 'completed', locked_at = NULL, locked_by = NULL, updated_at = NOW()
		WHERE id = ?
	`, taskID)
	return err
}

func (s *Store) RetryTask(ctx context.Context, taskID int64, errMsg string, delay time.Duration) error {
	delaySec := int(delay.Seconds())
	if delaySec < 1 {
		delaySec = 1
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE agent_tasks
		SET status = 'queued', last_error = ?, locked_at = NULL, locked_by = NULL,
		    scheduled_at = DATE_ADD(NOW(), INTERVAL ? SECOND), updated_at = NOW()
		WHERE id = ?
	`, errMsg, delaySec, taskID)
	return err
}

func (s *Store) MarkTaskDead(ctx context.Context, taskID int64, errMsg string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE agent_tasks
		SET status = 'dead', last_error = ?, locked_at = NULL, locked_by = NULL, updated_at = NOW()
		WHERE id = ?
	`, errMsg, taskID)
	return err
}

func (s *Store) ReleaseStaleLockedTasks(ctx context.Context, timeout time.Duration) (int, error) {
	timeoutSec := int(timeout.Seconds())
	result, err := s.db.ExecContext(ctx, `
		UPDATE agent_tasks
		SET status = 'queued', locked_at = NULL, locked_by = NULL, updated_at = NOW()
		WHERE status = 'running'
		  AND locked_at < DATE_SUB(NOW(), INTERVAL ? SECOND)
	`, timeoutSec)
	if err != nil {
		return 0, err
	}
	rows, _ := result.RowsAffected()
	return int(rows), nil
}

func (s *Store) GetAgentRunDetail(ctx context.Context, runID string) (*AgentRunDetail, error) {
	run, err := s.queryOneMap(ctx, `SELECT * FROM agent_runs WHERE id = ?`, runID)
	if err != nil {
		return nil, err
	}
	steps, err := s.queryMaps(ctx, `SELECT * FROM agent_steps WHERE run_id = ? ORDER BY id`, runID)
	if err != nil {
		return nil, err
	}
	tools, err := s.queryMaps(ctx, `SELECT * FROM tool_calls WHERE run_id = ? ORDER BY id`, runID)
	if err != nil {
		return nil, err
	}
	llmCalls, err := s.queryMaps(ctx, `SELECT * FROM llm_calls WHERE run_id = ? ORDER BY id`, runID)
	if err != nil {
		return nil, err
	}
	var shot *Screenshot
	if screenshotID, ok := run["screenshot_id"].(string); ok && screenshotID != "" {
		shot, _ = s.GetScreenshot(ctx, screenshotID)
	}
	return &AgentRunDetail{Run: run, Steps: steps, Tools: tools, LLMCalls: llmCalls, Screenshot: shot}, nil
}

func (s *Store) queryOneMap(ctx context.Context, query string, args ...any) (map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	return scanMap(rows)
}

func (s *Store) queryMaps(ctx context.Context, query string, args ...any) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []map[string]any
	for rows.Next() {
		item, err := scanMap(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, item)
	}
	return results, rows.Err()
}

func scanMap(rows *sql.Rows) (map[string]any, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	values := make([]any, len(columns))
	pointers := make([]any, len(columns))
	for i := range values {
		pointers[i] = &values[i]
	}
	if err := rows.Scan(pointers...); err != nil {
		return nil, err
	}
	result := make(map[string]any, len(columns))
	for i, column := range columns {
		switch value := values[i].(type) {
		case []byte:
			result[column] = string(value)
		case time.Time:
			result[column] = value.Format(time.RFC3339)
		default:
			result[column] = value
		}
	}
	return result, nil
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
