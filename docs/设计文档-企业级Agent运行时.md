# 技术设计：企业级 Agent 运行时升级

> **版本**：v1.0 | **日期**：2026-07-17 | **状态**：待确认

## 1. 设计总览

### 1.1 现有架构（同步）

```
POST /v1/screenshots
  → 保存文件 + 创建 screenshot 记录
  → Organizer.Run()（同步阻塞）
      → OCR Tool → LLM Tool → Memory Tool
  → 返回完整结果
```

### 1.2 目标架构（异步 + 声明式编排）

```
POST /v1/screenshots
  → 保存文件 + 创建 screenshot + 创建 agent_run + 入队 agent_task
  → 立即返回 {screenshot_id, run_id, status: "queued"}

Worker goroutine（后台循环）
  → 从 agent_tasks 表 dequeue（SELECT FOR UPDATE SKIP LOCKED）
  → 根据 agent_run.type 查找注册的 AgentType
  → AgentRunner 按 StepDefinition 逐步执行
      → 每步：超时控制 + 重试 + 失败策略 + 追踪记录
  → 更新 agent_run 状态 + 完成/失败 agent_task
```

## 2. 核心数据结构

### 2.1 Pipeline 定义（`internal/agent/pipeline.go`）

```go
// FailurePolicy 定义步骤失败时的处理策略。
type FailurePolicy int

const (
    FailAbort           FailurePolicy = iota // 中止整个 Run
    FailSkip                                 // 跳过此步骤继续
    FailContinueDegraded                     // 降级继续（使用 fallback 输出）
)

// RunContext 在步骤之间传递状态。
type RunContext struct {
    RunID       string
    Input       map[string]any            // Agent Run 的原始输入
    StepOutputs map[string]map[string]any // stepName → 该步骤输出
    Degraded    bool                      // 是否有步骤降级
}

// StepDefinition 声明 Pipeline 中的一个步骤。
type StepDefinition struct {
    Name           string
    ToolName       string
    BuildInput     func(rc *RunContext) map[string]any
    Timeout        time.Duration
    MaxRetries     int
    RetryDelay     time.Duration
    OnFailure      FailurePolicy
    FallbackOutput func(rc *RunContext, err error) map[string]any // FailContinueDegraded 时使用
}

// AgentType 是一种已注册的 Agent 类型，包含名称和步骤列表。
type AgentType struct {
    Name  string
    Steps []StepDefinition
}
```

### 2.2 截图整理类型注册（`internal/agent/screenshot_organize.go`）

```go
func NewScreenshotOrganizeType() *AgentType {
    return &AgentType{
        Name: "screenshot_organize",
        Steps: []StepDefinition{
            {
                Name:      "ocr",
                ToolName:  tools.OCRToolName,
                BuildInput: func(rc *RunContext) map[string]any {
                    return map[string]any{
                        "screenshot_id": rc.Input["screenshot_id"],
                        "ocr_hint":      rc.Input["ocr_hint"],
                    }
                },
                Timeout:   60 * time.Second,
                OnFailure: FailAbort,
            },
            {
                Name:      "llm_insight",
                ToolName:  tools.LLMInsightToolName,
                BuildInput: func(rc *RunContext) map[string]any {
                    ocrText, _ := rc.StepOutputs["ocr"]["ocr_text"].(string)
                    return map[string]any{"ocr_text": ocrText}
                },
                Timeout:    45 * time.Second,
                MaxRetries: 1,
                RetryDelay: 2 * time.Second,
                OnFailure:  FailContinueDegraded,
                FallbackOutput: func(rc *RunContext, err error) map[string]any {
                    ocrText, _ := rc.StepOutputs["ocr"]["ocr_text"].(string)
                    mock := llm.MockInsight(ocrText)
                    return map[string]any{
                        "summary":     mock.Insight.Summary,
                        "category":    mock.Insight.Category,
                        "tags":        mock.Insight.Tags,
                        "explanation": "LLM 调用失败，使用降级结果: " + err.Error(),
                        "_llm_result": mock,
                    }
                },
            },
            {
                Name:      "save_memory",
                ToolName:  tools.MemoryToolName,
                BuildInput: func(rc *RunContext) map[string]any {
                    llmOutput := rc.StepOutputs["llm_insight"]
                    return map[string]any{
                        "screenshot_id": rc.Input["screenshot_id"],
                        "ocr_text":      rc.StepOutputs["ocr"]["ocr_text"],
                        "summary":       llmOutput["summary"],
                        "category":      llmOutput["category"],
                        "tags":          llmOutput["tags"],
                        "explanation":   llmOutput["explanation"],
                    }
                },
                Timeout:    10 * time.Second,
                MaxRetries: 1,
                RetryDelay: 1 * time.Second,
                OnFailure:  FailAbort,
            },
        },
    }
}
```

### 2.3 AgentRunner（`internal/agent/runner.go`）

```go
type AgentRunner struct {
    store    store.Repository
    registry *tools.Registry
    types    map[string]*AgentType
}

func NewAgentRunner(store store.Repository, registry *tools.Registry) *AgentRunner

// RegisterType 注册一种 Agent 类型。
func (r *AgentRunner) RegisterType(at *AgentType)

// Execute 执行一次 Agent Run。由 Worker 调用。
// 1. 创建 RunContext
// 2. 遍历 steps，每步：创建 context with timeout → 重试循环 → 记录追踪
// 3. 根据所有步骤结果确定 Run 最终状态
func (r *AgentRunner) Execute(ctx context.Context, runID, agentType string, input map[string]any) error
```

**Execute 核心逻辑伪代码：**

```
func Execute(ctx, runID, agentType, input):
    at = r.types[agentType]
    rc = &RunContext{RunID: runID, Input: input, StepOutputs: {}}
    tracer = NewTracer(r.store, runID)
    
    for each step in at.Steps:
        stepCtx = context.WithTimeout(ctx, step.Timeout)  // 若 Timeout > 0
        stepInput = step.BuildInput(rc)
        
        var output map[string]any
        var lastErr error
        
        for attempt = 0; attempt <= step.MaxRetries; attempt++:
            if attempt > 0:
                sleep(step.RetryDelay)
            tool = r.registry.Get(step.ToolName)
            output, lastErr = tool.Execute(stepCtx, stepInput)
            if lastErr == nil:
                break
        
        // 记录 step 和 tool call 追踪（包括重试次数）
        tracer.RecordStep(...)
        tracer.RecordTool(...)
        if step.ToolName == tools.LLMInsightToolName:
            tracer.RecordLLM(...)  // 记录 LLM 特有信息
        
        if lastErr != nil:
            switch step.OnFailure:
            case FailAbort:
                finishRun(status="failed")
                return lastErr
            case FailSkip:
                rc.StepOutputs[step.Name] = map[string]any{}
                continue
            case FailContinueDegraded:
                rc.StepOutputs[step.Name] = step.FallbackOutput(rc, lastErr)
                rc.Degraded = true
                continue
        else:
            rc.StepOutputs[step.Name] = output
    
    if rc.Degraded:
        finishRun(status="partial_success")
    else:
        finishRun(status="completed")
```

### 2.4 Worker（`internal/agent/worker.go`）

```go
type Worker struct {
    store        store.Repository
    runner       *AgentRunner
    id           string        // 唯一标识，用于锁定记录
    pollInterval time.Duration
    lockTimeout  time.Duration // 锁超时（超时后自动释放）
    concurrency  int           // 并发 goroutine 数
}

func NewWorker(store store.Repository, runner *AgentRunner, opts WorkerOptions) *Worker

// Start 启动 Worker，阻塞直到 ctx 取消。
// 1. 启动 concurrency 个 goroutine
// 2. 每个 goroutine 循环：dequeue → execute → complete/fail
// 3. ctx 取消时，等待当前任务完成后退出
func (w *Worker) Start(ctx context.Context)
```

**Worker 单次循环伪代码：**

```
func processOne(ctx):
    task = store.DequeueTask(ctx, w.id)
    if task == nil:
        sleep(pollInterval)
        return
    
    run = store.GetAgentRun(ctx, task.RunID)
    err = w.runner.Execute(ctx, run.ID, run.Type, run.Input)
    
    if err == nil:
        store.CompleteTask(ctx, task.ID)
    else:
        if task.Attempts >= task.MaxAttempts:
            store.MarkTaskDead(ctx, task.ID, err.Error())
        else:
            delay = retryDelay(task.Attempts)  // 指数退避：2^attempts 秒
            store.RetryTask(ctx, task.ID, err.Error(), delay)
```

## 3. 数据库变更

### 3.1 新增 `agent_tasks` 表（`migrations/002_agent_tasks.sql`）

```sql
CREATE TABLE IF NOT EXISTS agent_tasks (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  run_id VARCHAR(64) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'queued',
  attempts INT NOT NULL DEFAULT 0,
  max_attempts INT NOT NULL DEFAULT 3,
  last_error TEXT NULL,
  locked_at TIMESTAMP NULL,
  locked_by VARCHAR(128) NULL,
  scheduled_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_tasks_run (run_id),
  INDEX idx_tasks_dequeue (status, scheduled_at),
  CONSTRAINT fk_tasks_run FOREIGN KEY (run_id) REFERENCES agent_runs(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
```

**Dequeue 查询（核心）：**

```sql
SELECT id, run_id, attempts, max_attempts
FROM agent_tasks
WHERE status = 'queued'
  AND scheduled_at <= NOW()
ORDER BY scheduled_at ASC
LIMIT 1
FOR UPDATE SKIP LOCKED;

-- 紧接着同一事务内：
UPDATE agent_tasks
SET status = 'running', locked_at = NOW(), locked_by = ?, attempts = attempts + 1
WHERE id = ?;
```

**超时释放（定期执行）：**

```sql
UPDATE agent_tasks
SET status = 'queued', locked_at = NULL, locked_by = NULL
WHERE status = 'running'
  AND locked_at < NOW() - INTERVAL 5 MINUTE;
```

### 3.2 `agent_runs` 表变更

不需要 DDL 变更。`status` 字段已是 `VARCHAR(32)`，只需在应用层新增 `partial_success` 和 `queued` 状态值。

## 4. Repository 接口变更

### 4.1 新增方法

```go
type Repository interface {
    // ... 现有方法保持不变 ...

    // 任务队列
    EnqueueTask(ctx context.Context, runID string, maxAttempts int) error
    DequeueTask(ctx context.Context, workerID string) (*Task, error)
    CompleteTask(ctx context.Context, taskID int64) error
    RetryTask(ctx context.Context, taskID int64, errMsg string, delay time.Duration) error
    MarkTaskDead(ctx context.Context, taskID int64, errMsg string) error
    ReleaseStaleLockedTasks(ctx context.Context, timeout time.Duration) (int, error)

    // 新增查询（Runner 需要从 run_id 加载输入）
    GetAgentRun(ctx context.Context, runID string) (*AgentRun, error)
}

// 新增数据结构
type Task struct {
    ID          int64
    RunID       string
    Status      string
    Attempts    int
    MaxAttempts int
}

type AgentRun struct {
    ID           string
    DeviceID     string
    ScreenshotID string
    Type         string
    Status       string
    InputJSON    string
}
```

### 4.2 Memory 实现

`store.Memory` 需要新增对应方法。任务队列用 `[]Task` slice + mutex 模拟，保证测试能完整走通异步流程。

## 5. API 变更

### 5.1 `POST /v1/screenshots` 响应变更

**之前（同步）：**
```json
{
  "run_id": "run_xxx",
  "screenshot_id": "shot_xxx",
  "ocr_text": "...",
  "insight": { "summary": "...", "category": "...", "tags": [...], "explanation": "..." }
}
```

**之后（异步）：**
```json
{
  "screenshot_id": "shot_xxx",
  "run_id": "run_xxx",
  "status": "queued"
}
```

客户端用 `GET /v1/agent-runs/{run_id}` 轮询获取结果。

### 5.2 `GET /v1/screenshots/{id}` 增加 `status` 语义

已有 `status` 字段，现在值域扩展：
- `uploaded`：刚上传，Agent 未开始
- `processing`：Agent 正在执行
- `organized`：Agent 完成（全部成功）
- `partial`：Agent 部分成功（有降级步骤）
- `failed`：Agent 执行失败

### 5.3 其他 API 不变

`POST /v1/screenshots/{id}/retry-agent` 改为创建新的 agent_run + 入队，立即返回。

## 6. 文件变更清单

| 操作 | 文件 | 说明 |
|------|------|------|
| **新增** | `internal/agent/pipeline.go` | FailurePolicy, RunContext, StepDefinition, AgentType |
| **新增** | `internal/agent/runner.go` | AgentRunner：通用执行器 |
| **新增** | `internal/agent/worker.go` | Worker：后台任务消费 |
| **新增** | `internal/agent/screenshot_organize.go` | screenshot_organize 类型注册 |
| **新增** | `internal/agent/runner_test.go` | AgentRunner 单元测试 |
| **新增** | `internal/agent/worker_test.go` | Worker 单元测试 |
| **新增** | `internal/tools/mock_test.go` | 测试用 mock Tool |
| **新增** | `migrations/002_agent_tasks.sql` | 新增 agent_tasks 表 |
| **修改** | `internal/store/repository.go` | 新增 Task 相关接口方法 |
| **修改** | `internal/store/store.go` | MySQL 实现 Task 相关方法 |
| **修改** | `internal/store/memory.go` | 内存实现 Task 相关方法 |
| **修改** | `internal/httpapi/server.go` | 上传接口改异步返回，retry 改入队 |
| **修改** | `internal/app/app.go` | 用 Runner+Worker 替换 Organizer |
| **修改** | `internal/config/config.go` | 新增 Worker 相关配置 |
| **修改** | `cmd/api/main.go` | 启动 Worker goroutine + 优雅关停 |
| **修改** | `internal/llm/provider.go` | 导出 MockInsight 函数（原 mockInsight） |
| **删除** | `internal/agent/organizer.go` | 被 runner + screenshot_organize 替代 |

## 7. 配置新增

```bash
# Worker 配置
WORKER_POLL_INTERVAL=3        # 轮询间隔（秒），默认 3
WORKER_CONCURRENCY=2          # 并发 goroutine 数，默认 2
WORKER_LOCK_TIMEOUT=300       # 锁超时（秒），默认 300
WORKER_MAX_ATTEMPTS=3         # 默认最大重试次数，默认 3
```

## 8. 启动流程变更

```go
// cmd/api/main.go（变更后）
func main() {
    app.SetupLogger()
    cfg := config.Load()

    application, err := app.New(context.Background(), cfg)
    // ...

    // 启动 Worker（后台 goroutine）
    workerCtx, workerCancel := context.WithCancel(context.Background())
    go application.Worker.Start(workerCtx)

    // 优雅关停
    srv := &http.Server{Addr: cfg.Addr, Handler: application.Server.Handler()}
    go func() {
        sigCh := make(chan os.Signal, 1)
        signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
        <-sigCh
        workerCancel()           // 停止 Worker（等待当前任务完成）
        srv.Shutdown(context.Background())
    }()
    srv.ListenAndServe()
}
```

## 9. 开发顺序

按依赖关系分 5 批，每批完成后编译通过 + 测试通过：

| 批次 | 内容 | 验证方式 |
|------|------|----------|
| **B1** | `pipeline.go`（纯数据结构定义）+ `migrations/002_agent_tasks.sql` | 编译通过 |
| **B2** | `repository.go` 新增接口 + `store.go` MySQL 实现 + `memory.go` 内存实现 | 编译通过 |
| **B3** | `runner.go` + `screenshot_organize.go` + `llm/provider.go` 导出 MockInsight + `runner_test.go` | 测试通过 |
| **B4** | `worker.go` + `worker_test.go` | 测试通过 |
| **B5** | `httpapi/server.go` 改异步 + `app.go` 组装 + `config.go` 新增配置 + `main.go` 优雅关停 + 删除 `organizer.go` | 编译通过 + `make dev-memory` 启动正常 |

## 10. 测试策略

### 10.1 AgentRunner 测试（`runner_test.go`）

用 `store.Memory` + mock Tool 测试：

| 用例 | 场景 |
|------|------|
| `TestRunner_AllStepsSucceed` | 正常执行 3 步，Run 状态 = completed |
| `TestRunner_FirstStepFails_Abort` | OCR 步骤失败（FailAbort），Run 状态 = failed |
| `TestRunner_MiddleStepFails_Degraded` | LLM 步骤失败（FailContinueDegraded），使用 fallback 输出，Run 状态 = partial_success |
| `TestRunner_StepRetry_ThenSucceed` | 步骤第一次失败、重试后成功 |
| `TestRunner_StepRetry_AllFail` | 步骤重试耗尽后按 OnFailure 策略处理 |
| `TestRunner_StepTimeout` | 步骤超时视为失败，按 OnFailure 策略处理 |

### 10.2 Worker 测试（`worker_test.go`）

| 用例 | 场景 |
|------|------|
| `TestWorker_ProcessTask` | 入队一个任务，Worker 消费并执行成功 |
| `TestWorker_RetryOnFailure` | 任务执行失败，Worker 标记重试，再次消费后成功 |
| `TestWorker_MaxAttemptsExceeded` | 超过最大重试次数，任务标记为 dead |
| `TestWorker_GracefulShutdown` | 取消 context 后，Worker 完成当前任务再退出 |

### 10.3 Mock Tool

```go
// internal/tools/mock_test.go（仅测试包可见）
type MockTool struct {
    name      string
    executeFn func(ctx context.Context, input map[string]any) (map[string]any, error)
}
```
