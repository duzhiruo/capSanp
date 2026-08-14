# 技术设计：统计 API 与链路追踪

> **版本**：v1.0 | **日期**：2026-07-17 | **状态**：待确认

## 1. 设计总览

两个独立但互补的功能，分 3 批交付：

| 批次 | 内容 |
|------|------|
| B1 | 链路追踪基础设施：request_id context 机制 + HTTP 中间件 + 迁移 SQL |
| B2 | 统计 API：Repository 接口 + MySQL/Memory 实现 + HTTP handler |
| B3 | 全链路贯通：Runner/Worker 注入 request_id + slog 附加 + 测试 |

## 2. 链路追踪设计

### 2.1 request_id 机制（`internal/httpapi/requestid.go`）

```go
type ctxKey int
const requestIDKey ctxKey = iota

// NewRequestID 生成 req_ 前缀的唯一 ID。
func NewRequestID() string {
    return agent.NewID("req")
}

// WithRequestID 将 request_id 注入 context。
func WithRequestID(ctx context.Context, id string) context.Context

// GetRequestID 从 context 提取 request_id，不存在返回空字符串。
func GetRequestID(ctx context.Context) string
```

### 2.2 HTTP 中间件（`internal/httpapi/middleware.go`）

```go
func (s *Server) withRequestID(next http.Handler) http.Handler
// 1. 从 X-Request-ID header 读取，如果没有则生成新的
// 2. 注入 context
// 3. 设置响应头 X-Request-ID
// 4. 调用 next

func (s *Server) withAccessLog(next http.Handler) http.Handler
// 1. 包装 ResponseWriter 捕获 status code
// 2. 记录开始时间
// 3. 调用 next
// 4. slog.Info 记录：method, path, status, duration_ms, request_id
```

中间件链顺序：`withRequestID → withAccessLog → withCORS → mux`

### 2.3 数据库变更（`migrations/003_request_id.sql`）

```sql
ALTER TABLE agent_runs ADD COLUMN request_id VARCHAR(64) NULL AFTER id;
ALTER TABLE agent_runs ADD INDEX idx_agent_runs_created (created_at);
```

### 2.4 Agent Runner / Worker 中的 request_id 传递

**上传接口（HTTP → 入队）：**
- HTTP request_id 通过 context 传递到 `CreateAgentRun`
- `CreateAgentRun` 接口新增 `requestID` 参数，写入 agent_runs.request_id

**Worker 执行（后台）：**
- Worker 在 `processOne` 中为每个任务生成新的 request_id
- 注入 context，传给 `AgentRunner.Execute`
- Runner 和 Tracer 中所有 slog 调用自动附加 request_id

**slog 附加方式：** 使用 `slog.With("request_id", requestID)` 创建带属性的 logger，而非修改全局 logger。通过 context 传递。

```go
// 从 context 中获取 request_id 并生成带属性的 slog 调用
func logInfo(ctx context.Context, msg string, args ...any) {
    reqID := httpapi.GetRequestID(ctx)
    if reqID != "" {
        args = append(args, "request_id", reqID)
    }
    slog.Info(msg, args...)
}
```

实际实现中不新增辅助函数，直接在关键 slog 调用点加 request_id 参数。保持简单。

## 3. 统计 API 设计

### 3.1 数据结构

```go
// Stats 是 GET /v1/stats 的返回结构。
type Stats struct {
    TimeRange   string      `json:"time_range"`
    Agent       AgentStats  `json:"agent"`
    LLM         LLMStats    `json:"llm"`
    OCR         OCRStats    `json:"ocr"`
    Queue       QueueStats  `json:"queue"`
    Screenshots ShotStats   `json:"screenshots"`
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
```

### 3.2 Repository 接口

```go
type Repository interface {
    // ... 现有方法 ...

    // 统计
    GetStats(ctx context.Context, opts StatsQuery) (*Stats, error)
}

type StatsQuery struct {
    TimeRange string // today / week / month / all
    DeviceID  string // 可选
}
```

### 3.3 MySQL 统计查询

核心用一条聚合查询 + 几个小查询完成，不做复杂的子查询嵌套：

```sql
-- Agent 运行统计（带时间范围）
SELECT
  COUNT(*) AS total,
  SUM(status = 'completed') AS completed,
  SUM(status = 'partial_success') AS partial_success,
  SUM(status = 'failed') AS failed,
  AVG(CASE WHEN status IN ('completed','partial_success') THEN duration_ms END) AS avg_duration_ms
FROM agent_runs
WHERE created_at >= ?;  -- 时间范围下界

-- P95 耗时（近似）
SELECT duration_ms FROM agent_runs
WHERE status IN ('completed','partial_success') AND created_at >= ?
ORDER BY duration_ms ASC
LIMIT 1 OFFSET ?;  -- offset = total * 0.95

-- LLM 统计
SELECT
  COUNT(*) AS total_calls,
  COALESCE(SUM(prompt_tokens + completion_tokens), 0) AS total_tokens,
  COALESCE(SUM(estimated_cost_usd), 0) AS total_cost_usd
FROM llm_calls
WHERE created_at >= ?;

-- OCR 空文本统计
SELECT
  COUNT(*) AS total,
  SUM(ocr_text IS NULL OR ocr_text = '' OR ocr_text LIKE 'OCR 未能识别%') AS empty_count
FROM screenshots
WHERE created_at >= ?;

-- 队列状态（实时，不需要时间范围）
SELECT
  SUM(status = 'queued') AS queued,
  SUM(status = 'running') AS running,
  SUM(status = 'dead') AS dead
FROM agent_tasks;

-- 截图总览
SELECT
  COUNT(*) AS total,
  SUM(status = 'organized') AS organized,
  SUM(status = 'partial') AS partial,
  SUM(status = 'failed') AS failed
FROM screenshots
WHERE created_at >= ?;
```

时间范围转换：
- `today` → `CURDATE()`
- `week` → `DATE_SUB(CURDATE(), INTERVAL 7 DAY)`
- `month` → `DATE_SUB(CURDATE(), INTERVAL 30 DAY)`
- `all` → `'1970-01-01'`

### 3.4 Memory 实现

遍历内存中的 maps/slices 做简单计数。不需要精确的 P95，返回 0 即可。

### 3.5 HTTP Handler

```
GET /v1/stats?range=today&device_id=xxx
```

返回 `Stats` JSON。

## 4. 文件变更清单

| 操作 | 文件 | 说明 |
|------|------|------|
| **新增** | `internal/httpapi/requestid.go` | context key + WithRequestID + GetRequestID |
| **新增** | `internal/httpapi/middleware.go` | withRequestID + withAccessLog + statusWriter |
| **新增** | `internal/httpapi/stats.go` | Stats 数据结构 + handleStats handler |
| **新增** | `internal/httpapi/middleware_test.go` | 中间件测试 |
| **新增** | `migrations/003_request_id.sql` | agent_runs 加列加索引 |
| **修改** | `internal/store/repository.go` | 新增 Stats 结构体 + GetStats + StatsQuery |
| **修改** | `internal/store/store.go` | MySQL GetStats 实现 |
| **修改** | `internal/store/memory.go` | Memory GetStats 实现 |
| **修改** | `internal/httpapi/server.go` | 中间件链 + stats 路由 + CreateAgentRun 传 request_id |
| **修改** | `internal/agent/worker.go` | 生成 request_id 注入 context |
| **修改** | `internal/agent/runner.go` | slog 调用附加 request_id |
| **修改** | `internal/app/app.go` | 迁移 003 |

## 5. 开发顺序

| 批次 | 内容 | 验证 |
|------|------|------|
| B1 | `requestid.go` + `middleware.go` + `middleware_test.go` + `003_request_id.sql` + server.go 挂中间件 | 编译通过 + 中间件测试通过 |
| B2 | Stats 结构体 + `repository.go` + `store.go` + `memory.go` + `stats.go` handler + 路由 | 编译通过 + `make dev-memory` 可调用 `/v1/stats` |
| B3 | runner.go / worker.go 注入 request_id + slog 附加 + `app.go` 迁移 | 全量测试通过 |
