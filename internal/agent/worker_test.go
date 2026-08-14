package agent

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"capsnap/internal/store"
	"capsnap/internal/tools"
)

func setupWorkerTest(t *testing.T, ocrFn, llmFn, memFn func(context.Context, map[string]any) (map[string]any, error)) (*Worker, store.Repository) {
	t.Helper()
	ocrTool := &mockTool{name: tools.OCRToolName, executeFn: ocrFn}
	llmTool := &mockTool{name: tools.LLMInsightToolName, executeFn: llmFn}
	memTool := &mockTool{name: tools.MemoryToolName, executeFn: memFn}

	repo := store.NewMemory()
	registry := tools.NewRegistry(ocrTool, llmTool, memTool)
	runner := NewAgentRunner(repo, registry)
	runner.RegisterType(NewScreenshotOrganizeType())

	worker := NewWorker(repo, runner, WorkerOptions{
		PollInterval: 50 * time.Millisecond,
		Concurrency:  1,
		WorkerID:     "test-worker",
	})
	return worker, repo
}

func enqueueTestTask(t *testing.T, repo store.Repository, runID string) {
	t.Helper()
	ctx := context.Background()
	_ = repo.EnsureDevice(ctx, "test-device")
	_ = repo.CreateScreenshot(ctx, store.Screenshot{
		ID: "shot_test", DeviceID: "test-device", OriginalFilename: "test.png",
		StoragePath: "/tmp/test.png", Status: "uploaded",
	})
	input := map[string]any{"screenshot_id": "shot_test", "ocr_hint": ""}
	_ = repo.CreateAgentRun(ctx, runID, "test-device", "shot_test", "screenshot_organize", "", input)
	_ = repo.EnqueueTask(ctx, runID, 3)
}

func successOCR(_ context.Context, _ map[string]any) (map[string]any, error) {
	return map[string]any{"ocr_text": "Worker 测试文本"}, nil
}
func successLLM(_ context.Context, _ map[string]any) (map[string]any, error) {
	return map[string]any{
		"summary": "Worker 测试摘要", "category": "文档资料",
		"tags": []string{"worker"}, "explanation": "测试",
	}, nil
}
func successMem(_ context.Context, input map[string]any) (map[string]any, error) {
	return map[string]any{"screenshot_id": input["screenshot_id"]}, nil
}

func TestWorker_ProcessTask(t *testing.T) {
	worker, repo := setupWorkerTest(t, successOCR, successLLM, successMem)
	enqueueTestTask(t, repo, "run_worker_ok")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go worker.Start(ctx)
	// 等待任务被消费
	time.Sleep(500 * time.Millisecond)
	cancel()

	run, err := repo.GetAgentRun(context.Background(), "run_worker_ok")
	if err != nil {
		t.Fatalf("获取 run 失败: %v", err)
	}
	if run.Status != "completed" {
		t.Errorf("期望 status=completed，实际 %s", run.Status)
	}
}

func TestWorker_RetryOnFailure(t *testing.T) {
	var callCount int32
	failLLM := func(_ context.Context, _ map[string]any) (map[string]any, error) {
		n := atomic.AddInt32(&callCount, 1)
		if n <= 4 {
			return nil, errors.New("LLM 暂时不可用")
		}
		return map[string]any{
			"summary": "恢复成功", "category": "文档资料",
			"tags": []string{"retry"}, "explanation": "恢复",
		}, nil
	}

	worker, repo := setupWorkerTest(t, successOCR, failLLM, successMem)
	enqueueTestTask(t, repo, "run_worker_retry")

	// LLM 步骤有 2 秒重试延迟，需要等足够长的时间让 Worker 完成执行
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	go worker.Start(ctx)
	time.Sleep(4 * time.Second)
	cancel()

	run, err := repo.GetAgentRun(context.Background(), "run_worker_retry")
	if err != nil {
		t.Fatalf("获取 run 失败: %v", err)
	}
	// LLM 步骤配置了 FailContinueDegraded，第一次 Worker 执行就会降级为 partial_success
	if run.Status != "partial_success" && run.Status != "completed" {
		t.Errorf("期望 partial_success 或 completed，实际 %s", run.Status)
	}
}

func TestWorker_MaxAttemptsExceeded(t *testing.T) {
	failOCR := func(_ context.Context, _ map[string]any) (map[string]any, error) {
		return nil, errors.New("OCR 持续失败")
	}

	worker, repo := setupWorkerTest(t, failOCR, successLLM, successMem)

	ctx := context.Background()
	_ = repo.EnsureDevice(ctx, "test-device")
	_ = repo.CreateScreenshot(ctx, store.Screenshot{
		ID: "shot_dead", DeviceID: "test-device", OriginalFilename: "dead.png",
		StoragePath: "/tmp/dead.png", Status: "uploaded",
	})
	input := map[string]any{"screenshot_id": "shot_dead", "ocr_hint": ""}
	_ = repo.CreateAgentRun(ctx, "run_dead", "test-device", "shot_dead", "screenshot_organize", "", input)
	_ = repo.EnqueueTask(ctx, "run_dead", 2) // 最多 2 次

	wCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go worker.Start(wCtx)
	time.Sleep(2 * time.Second)
	cancel()

	run, err := repo.GetAgentRun(context.Background(), "run_dead")
	if err != nil {
		t.Fatalf("获取 run 失败: %v", err)
	}
	if run.Status != "failed" {
		t.Errorf("期望 status=failed（dead），实际 %s", run.Status)
	}
}

func TestWorker_GracefulShutdown(t *testing.T) {
	var processing int32
	slowOCR := func(_ context.Context, _ map[string]any) (map[string]any, error) {
		atomic.StoreInt32(&processing, 1)
		// 不检查 ctx.Done()，模拟不可中断的工作
		time.Sleep(500 * time.Millisecond)
		return map[string]any{"ocr_text": "slow"}, nil
	}

	worker, repo := setupWorkerTest(t, slowOCR, successLLM, successMem)
	enqueueTestTask(t, repo, "run_graceful")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		worker.Start(ctx)
		close(done)
	}()

	// 等任务开始处理
	for i := 0; i < 40; i++ {
		if atomic.LoadInt32(&processing) == 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	cancel() // 发送停止信号

	select {
	case <-done:
		// Worker 正常退出
	case <-time.After(5 * time.Second):
		t.Fatal("Worker 未在 5 秒内优雅退出")
	}

	// 验证任务执行完成（优雅关停，不丢弃正在执行的任务）
	run, err := repo.GetAgentRun(context.Background(), "run_graceful")
	if err != nil {
		t.Fatalf("获取 run 失败: %v", err)
	}
	if run.Status != "completed" && run.Status != "partial_success" {
		t.Errorf("优雅关停后任务应完成，实际 status=%s", run.Status)
	}
}
