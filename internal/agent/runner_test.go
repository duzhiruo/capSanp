package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"capsnap/internal/store"
	"capsnap/internal/tools"
)

// mockTool 用于测试的 mock Tool 实现。
type mockTool struct {
	name      string
	executeFn func(ctx context.Context, input map[string]any) (map[string]any, error)
}

func (m *mockTool) Name() string { return m.name }
func (m *mockTool) Execute(ctx context.Context, input map[string]any) (map[string]any, error) {
	return m.executeFn(ctx, input)
}

func newTestRunner(toolList ...tools.Tool) (*AgentRunner, store.Repository) {
	repo := store.NewMemory()
	registry := tools.NewRegistry(toolList...)
	runner := NewAgentRunner(repo, registry)
	return runner, repo
}

func setupRun(t *testing.T, repo store.Repository, runID, agentType string, input map[string]any) {
	t.Helper()
	ctx := context.Background()
	_ = repo.EnsureDevice(ctx, "test-device")
	_ = repo.CreateScreenshot(ctx, store.Screenshot{
		ID: "shot_test", DeviceID: "test-device", OriginalFilename: "test.png",
		StoragePath: "/tmp/test.png", Status: "uploaded",
	})
	if err := repo.CreateAgentRun(ctx, runID, "test-device", "shot_test", agentType, "", input); err != nil {
		t.Fatalf("创建 agent run 失败: %v", err)
	}
}

func TestRunner_AllStepsSucceed(t *testing.T) {
	ocrTool := &mockTool{
		name: tools.OCRToolName,
		executeFn: func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{"ocr_text": "测试文本内容"}, nil
		},
	}
	llmTool := &mockTool{
		name: tools.LLMInsightToolName,
		executeFn: func(_ context.Context, input map[string]any) (map[string]any, error) {
			return map[string]any{
				"summary": "测试摘要", "category": "文档资料",
				"tags": []string{"测试"}, "explanation": "测试解释",
			}, nil
		},
	}
	memTool := &mockTool{
		name: tools.MemoryToolName,
		executeFn: func(_ context.Context, input map[string]any) (map[string]any, error) {
			return map[string]any{"screenshot_id": input["screenshot_id"]}, nil
		},
	}

	runner, repo := newTestRunner(ocrTool, llmTool, memTool)
	runner.RegisterType(NewScreenshotOrganizeType())

	runID := "run_test_ok"
	input := map[string]any{"screenshot_id": "shot_test", "ocr_hint": ""}
	setupRun(t, repo, runID, "screenshot_organize", input)

	result, err := runner.Execute(context.Background(), runID, "screenshot_organize", input)
	if err != nil {
		t.Fatalf("Execute 应该成功: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("期望 status=completed，实际 %s", result.Status)
	}
	if len(result.Steps) != 4 {
		t.Errorf("期望 4 个步骤，实际 %d", len(result.Steps))
	}
	for _, s := range result.Steps {
		if s.Name == "embedding" {
			// 测试未注册 embedding 工具时 FailSkip → skipped
			if s.Status != "skipped" && s.Status != "completed" {
				t.Errorf("embedding 步骤期望 skipped/completed，实际 %s", s.Status)
			}
			continue
		}
		if s.Status != "completed" {
			t.Errorf("步骤 %s 期望 completed，实际 %s", s.Name, s.Status)
		}
	}
}

func TestRunner_FirstStepFails_Abort(t *testing.T) {
	ocrTool := &mockTool{
		name: tools.OCRToolName,
		executeFn: func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return nil, errors.New("OCR 服务不可用")
		},
	}
	llmTool := &mockTool{name: tools.LLMInsightToolName, executeFn: func(_ context.Context, _ map[string]any) (map[string]any, error) {
		t.Fatal("LLM 不应被调用")
		return nil, nil
	}}
	memTool := &mockTool{name: tools.MemoryToolName, executeFn: func(_ context.Context, _ map[string]any) (map[string]any, error) {
		t.Fatal("Memory 不应被调用")
		return nil, nil
	}}

	runner, repo := newTestRunner(ocrTool, llmTool, memTool)
	runner.RegisterType(NewScreenshotOrganizeType())

	runID := "run_test_ocr_fail"
	input := map[string]any{"screenshot_id": "shot_test", "ocr_hint": ""}
	setupRun(t, repo, runID, "screenshot_organize", input)

	result, err := runner.Execute(context.Background(), runID, "screenshot_organize", input)
	if err == nil {
		t.Fatal("OCR 失败时 Execute 应返回错误")
	}
	if result.Status != "failed" {
		t.Errorf("期望 status=failed，实际 %s", result.Status)
	}
	if len(result.Steps) != 1 {
		t.Errorf("期望只有 1 个步骤记录（OCR），实际 %d", len(result.Steps))
	}
}

func TestRunner_MiddleStepFails_Degraded(t *testing.T) {
	ocrTool := &mockTool{
		name: tools.OCRToolName,
		executeFn: func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{"ocr_text": "降级测试文本"}, nil
		},
	}
	llmCallCount := 0
	llmTool := &mockTool{
		name: tools.LLMInsightToolName,
		executeFn: func(_ context.Context, _ map[string]any) (map[string]any, error) {
			llmCallCount++
			return nil, errors.New("LLM 超时")
		},
	}
	memTool := &mockTool{
		name: tools.MemoryToolName,
		executeFn: func(_ context.Context, input map[string]any) (map[string]any, error) {
			if input["summary"] == nil || input["summary"] == "" {
				t.Error("降级后 summary 不应为空（应来自 fallback）")
			}
			return map[string]any{"screenshot_id": input["screenshot_id"]}, nil
		},
	}

	runner, repo := newTestRunner(ocrTool, llmTool, memTool)
	runner.RegisterType(NewScreenshotOrganizeType())

	runID := "run_test_degraded"
	input := map[string]any{"screenshot_id": "shot_test", "ocr_hint": ""}
	setupRun(t, repo, runID, "screenshot_organize", input)

	result, err := runner.Execute(context.Background(), runID, "screenshot_organize", input)
	if err != nil {
		t.Fatalf("降级模式下 Execute 不应返回错误: %v", err)
	}
	if result.Status != "partial_success" {
		t.Errorf("期望 status=partial_success，实际 %s", result.Status)
	}
	// LLM 步骤有 1 次重试，共 2 次调用
	if llmCallCount != 2 {
		t.Errorf("LLM 应被调用 2 次（1 次原始 + 1 次重试），实际 %d 次", llmCallCount)
	}
	// 验证步骤状态
	if result.Steps[1].Status != "degraded" {
		t.Errorf("LLM 步骤期望 degraded，实际 %s", result.Steps[1].Status)
	}
	if result.Steps[2].Status != "completed" {
		t.Errorf("Memory 步骤期望 completed，实际 %s", result.Steps[2].Status)
	}
}

func TestRunner_StepRetry_ThenSucceed(t *testing.T) {
	callCount := 0
	ocrTool := &mockTool{
		name: tools.OCRToolName,
		executeFn: func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{"ocr_text": "重试测试"}, nil
		},
	}
	llmTool := &mockTool{
		name: tools.LLMInsightToolName,
		executeFn: func(_ context.Context, _ map[string]any) (map[string]any, error) {
			callCount++
			if callCount == 1 {
				return nil, errors.New("第一次失败")
			}
			return map[string]any{
				"summary": "重试成功", "category": "文档资料",
				"tags": []string{"重试"}, "explanation": "重试后成功",
			}, nil
		},
	}
	memTool := &mockTool{
		name: tools.MemoryToolName,
		executeFn: func(_ context.Context, input map[string]any) (map[string]any, error) {
			return map[string]any{"screenshot_id": input["screenshot_id"]}, nil
		},
	}

	runner, repo := newTestRunner(ocrTool, llmTool, memTool)
	runner.RegisterType(NewScreenshotOrganizeType())

	runID := "run_test_retry_ok"
	input := map[string]any{"screenshot_id": "shot_test", "ocr_hint": ""}
	setupRun(t, repo, runID, "screenshot_organize", input)

	result, err := runner.Execute(context.Background(), runID, "screenshot_organize", input)
	if err != nil {
		t.Fatalf("重试成功后不应返回错误: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("期望 status=completed，实际 %s", result.Status)
	}
	if callCount != 2 {
		t.Errorf("LLM 应被调用 2 次，实际 %d", callCount)
	}
	if result.Steps[1].Attempts != 2 {
		t.Errorf("LLM 步骤期望 2 次尝试，实际 %d", result.Steps[1].Attempts)
	}
}

func TestRunner_StepTimeout(t *testing.T) {
	slowTool := &mockTool{
		name: "slow.tool",
		executeFn: func(ctx context.Context, _ map[string]any) (map[string]any, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(5 * time.Second):
				return map[string]any{"result": "done"}, nil
			}
		},
	}

	repo := store.NewMemory()
	registry := tools.NewRegistry(slowTool)
	runner := NewAgentRunner(repo, registry)
	runner.RegisterType(&AgentType{
		Name: "timeout_test",
		Steps: []StepDefinition{
			{
				Name:      "slow_step",
				ToolName:  "slow.tool",
				BuildInput: func(rc *RunContext) map[string]any { return nil },
				Timeout:   50 * time.Millisecond,
				OnFailure: FailAbort,
			},
		},
	})

	ctx := context.Background()
	_ = repo.EnsureDevice(ctx, "test-device")
	_ = repo.CreateAgentRun(ctx, "run_timeout", "test-device", "", "timeout_test", "", nil)

	result, err := runner.Execute(ctx, "run_timeout", "timeout_test", map[string]any{})
	if err == nil {
		t.Fatal("超时步骤应返回错误")
	}
	if result.Status != "failed" {
		t.Errorf("期望 status=failed，实际 %s", result.Status)
	}
}

func TestRunner_UnknownAgentType(t *testing.T) {
	runner, _ := newTestRunner()
	_, err := runner.Execute(context.Background(), "run_x", "nonexistent", nil)
	if err == nil {
		t.Fatal("未注册类型应返回错误")
	}
}
