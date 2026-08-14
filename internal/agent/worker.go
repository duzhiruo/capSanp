package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"sync"
	"time"

	"capsnap/internal/reqctx"
	"capsnap/internal/store"
)

// WorkerOptions 配置 Worker 行为。
type WorkerOptions struct {
	PollInterval time.Duration
	LockTimeout  time.Duration
	Concurrency  int
	WorkerID     string
}

func DefaultWorkerOptions() WorkerOptions {
	return WorkerOptions{
		PollInterval: 3 * time.Second,
		LockTimeout:  5 * time.Minute,
		Concurrency:  2,
		WorkerID:     "worker-1",
	}
}

// Worker 从 agent_tasks 表消费任务并通过 AgentRunner 执行。
type Worker struct {
	store  store.Repository
	runner *AgentRunner
	opts   WorkerOptions
}

func NewWorker(s store.Repository, runner *AgentRunner, opts WorkerOptions) *Worker {
	if opts.PollInterval <= 0 {
		opts.PollInterval = 3 * time.Second
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 2
	}
	if opts.WorkerID == "" {
		opts.WorkerID = "worker-" + NewID("w")
	}
	return &Worker{store: s, runner: runner, opts: opts}
}

// Start 启动 Worker，阻塞直到 ctx 取消。
// 启动 concurrency 个 goroutine 并行消费任务，ctx 取消时等待当前任务完成后退出。
func (w *Worker) Start(ctx context.Context) {
	slog.Info("Worker 启动", "id", w.opts.WorkerID, "concurrency", w.opts.Concurrency, "poll_interval", w.opts.PollInterval)

	if w.opts.LockTimeout > 0 {
		go w.staleLockCleaner(ctx)
	}

	var wg sync.WaitGroup
	for i := 0; i < w.opts.Concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			w.pollLoop(ctx, id)
		}(i)
	}
	wg.Wait()
	slog.Info("Worker 已停止", "id", w.opts.WorkerID)
}

func (w *Worker) pollLoop(ctx context.Context, goroutineID int) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		processed := w.processOne(ctx)
		if !processed {
			select {
			case <-ctx.Done():
				return
			case <-time.After(w.opts.PollInterval):
			}
		}
	}
}

// processOne 尝试消费并执行一个任务。返回 true 表示处理了一个任务（无论成败）。
// 任务执行使用独立 context，不受父 context 取消影响（优雅关停：完成当前任务再退出）。
func (w *Worker) processOne(ctx context.Context) bool {
	task, err := w.store.DequeueTask(ctx, w.opts.WorkerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false
		}
		slog.Error("Dequeue 失败", "error", err)
		return false
	}

	reqID := NewID("req")
	slog.Info("开始执行任务", "task_id", task.ID, "run_id", task.RunID, "attempt", task.Attempts, "request_id", reqID)

	// 任务执行使用独立 context：即使父 ctx 取消，也让当前任务跑完
	execCtx := reqctx.With(context.Background(), reqID)

	run, err := w.store.GetAgentRun(execCtx, task.RunID)
	if err != nil {
		slog.Error("加载 AgentRun 失败", "run_id", task.RunID, "error", err)
		w.failOrRetry(execCtx, task, err)
		return true
	}

	var input map[string]any
	if run.InputJSON != "" {
		_ = json.Unmarshal([]byte(run.InputJSON), &input)
	}
	if input == nil {
		input = make(map[string]any)
	}

	_, execErr := w.runner.Execute(execCtx, run.ID, run.Type, input)

	if execErr != nil {
		slog.Warn("任务执行失败", "task_id", task.ID, "run_id", task.RunID, "request_id", reqID, "error", execErr)
		w.failOrRetry(execCtx, task, execErr)
	} else {
		slog.Info("任务执行成功", "task_id", task.ID, "run_id", task.RunID, "request_id", reqID)
		_ = w.store.CompleteTask(execCtx, task.ID)
	}
	return true
}

func (w *Worker) failOrRetry(ctx context.Context, task *store.Task, err error) {
	if task.Attempts >= task.MaxAttempts {
		slog.Warn("任务超过最大重试次数，标记为 dead", "task_id", task.ID, "run_id", task.RunID)
		_ = w.store.MarkTaskDead(ctx, task.ID, err.Error())
		_ = w.store.FinishAgentRun(ctx, task.RunID, "failed", nil, "超过最大重试次数: "+err.Error(), 0)
		return
	}
	delay := retryDelay(task.Attempts)
	slog.Info("任务将重试", "task_id", task.ID, "run_id", task.RunID, "delay", delay)
	_ = w.store.RetryTask(ctx, task.ID, err.Error(), delay)
}

// staleLockCleaner 定期释放超时锁定的任务。
func (w *Worker) staleLockCleaner(ctx context.Context) {
	ticker := time.NewTicker(w.opts.LockTimeout / 2)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			released, err := w.store.ReleaseStaleLockedTasks(ctx, w.opts.LockTimeout)
			if err != nil {
				slog.Error("释放超时锁失败", "error", err)
			} else if released > 0 {
				slog.Warn("释放超时锁定任务", "count", released)
			}
		}
	}
}

func retryDelay(attempts int) time.Duration {
	seconds := math.Pow(2, float64(attempts))
	if seconds > 300 {
		seconds = 300
	}
	return time.Duration(seconds) * time.Second
}
