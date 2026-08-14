package store

import (
	"context"
	"testing"
	"time"
)

func seedStatsData(t *testing.T, m *Memory) {
	t.Helper()
	ctx := context.Background()

	_ = m.EnsureDevice(ctx, "dev1")
	_ = m.CreateScreenshot(ctx, Screenshot{ID: "s1", DeviceID: "dev1", Status: "organized", OCRText: "hello"})
	_ = m.CreateScreenshot(ctx, Screenshot{ID: "s2", DeviceID: "dev1", Status: "failed", OCRText: ""})
	_ = m.CreateScreenshot(ctx, Screenshot{ID: "s3", DeviceID: "dev1", Status: "organized", OCRText: "world"})

	_ = m.CreateAgentRun(ctx, "r1", "dev1", "s1", "screenshot_organize", "req_1", nil)
	_ = m.FinishAgentRun(ctx, "r1", "completed", nil, "", 500*time.Millisecond)

	_ = m.CreateAgentRun(ctx, "r2", "dev1", "s2", "screenshot_organize", "req_2", nil)
	_ = m.FinishAgentRun(ctx, "r2", "failed", nil, "ocr fail", 200*time.Millisecond)

	_ = m.CreateAgentRun(ctx, "r3", "dev1", "s3", "screenshot_organize", "req_3", nil)
	_ = m.FinishAgentRun(ctx, "r3", "partial_success", nil, "", 800*time.Millisecond)

	_ = m.AddLLMCall(ctx, "r1", "deepseek", "deepseek-chat", "p", "r", 100, 50, 0.01, 100*time.Millisecond)
	_ = m.AddLLMCall(ctx, "r3", "deepseek", "deepseek-chat", "p", "r", 200, 100, 0.02, 200*time.Millisecond)

	_ = m.EnqueueTask(ctx, "r1", 3)
	_ = m.EnqueueTask(ctx, "r2", 3)
	// complete first, leave second queued
	for i := range m.tasks {
		if m.tasks[i].RunID == "r1" {
			m.tasks[i].Status = "completed"
		}
	}
}

func TestGetStats_Today(t *testing.T) {
	m := NewMemory()
	seedStatsData(t, m)

	stats, err := m.GetStats(context.Background(), StatsQuery{TimeRange: "today"})
	if err != nil {
		t.Fatalf("GetStats 失败: %v", err)
	}

	if stats.TimeRange != "today" {
		t.Errorf("TimeRange = %q, want 'today'", stats.TimeRange)
	}

	// Agent
	if stats.Agent.Total != 3 {
		t.Errorf("Agent.Total = %d, want 3", stats.Agent.Total)
	}
	if stats.Agent.Completed != 1 {
		t.Errorf("Agent.Completed = %d, want 1", stats.Agent.Completed)
	}
	if stats.Agent.Failed != 1 {
		t.Errorf("Agent.Failed = %d, want 1", stats.Agent.Failed)
	}
	if stats.Agent.PartialSuccess != 1 {
		t.Errorf("Agent.PartialSuccess = %d, want 1", stats.Agent.PartialSuccess)
	}

	// SuccessRate: 1 completed / 3 finished = 0.333...
	if stats.Agent.SuccessRate < 0.33 || stats.Agent.SuccessRate > 0.34 {
		t.Errorf("Agent.SuccessRate = %f, want ~0.333", stats.Agent.SuccessRate)
	}

	// LLM
	if stats.LLM.TotalCalls != 2 {
		t.Errorf("LLM.TotalCalls = %d, want 2", stats.LLM.TotalCalls)
	}
	if stats.LLM.TotalTokens != 450 { // (100+50) + (200+100)
		t.Errorf("LLM.TotalTokens = %d, want 450", stats.LLM.TotalTokens)
	}

	// OCR
	if stats.OCR.TotalProcessed != 3 {
		t.Errorf("OCR.TotalProcessed = %d, want 3", stats.OCR.TotalProcessed)
	}
	if stats.OCR.EmptyTextCount != 1 {
		t.Errorf("OCR.EmptyTextCount = %d, want 1", stats.OCR.EmptyTextCount)
	}

	// Queue
	if stats.Queue.Queued != 1 {
		t.Errorf("Queue.Queued = %d, want 1", stats.Queue.Queued)
	}

	// Screenshots
	if stats.Screenshots.Total != 3 {
		t.Errorf("Screenshots.Total = %d, want 3", stats.Screenshots.Total)
	}
	if stats.Screenshots.Organized != 2 {
		t.Errorf("Screenshots.Organized = %d, want 2", stats.Screenshots.Organized)
	}
}

func TestGetStats_EmptyStore(t *testing.T) {
	m := NewMemory()
	stats, err := m.GetStats(context.Background(), StatsQuery{TimeRange: "all"})
	if err != nil {
		t.Fatalf("GetStats 失败: %v", err)
	}
	if stats.Agent.Total != 0 {
		t.Errorf("空 store 应返回 0 总数，实际 %d", stats.Agent.Total)
	}
	if stats.Agent.SuccessRate != 0 {
		t.Errorf("空 store 成功率应为 0，实际 %f", stats.Agent.SuccessRate)
	}
}
