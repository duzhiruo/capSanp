# 需求分析：统计 API 与链路追踪

> **版本**：v1.0 | **日期**：2026-07-17 | **状态**：待确认

## 1. 背景

P0 完成后系统已具备异步 Agent Runtime、声明式 Pipeline 和 Step 级韧性。但可观测性停留在"数据入库"阶段——agent_runs、agent_steps、tool_calls、llm_calls 表里有大量原始数据，却没有聚合视图；各层日志没有统一的关联标识，排查问题需要手动拼接 run_id。

## 2. 目标

1. **统计 API**：一个接口返回系统运行状态大盘，支持时间范围筛选
2. **链路追踪**：request_id 从 HTTP 入口生成，贯穿 Agent → Tool → LLM → DB 全链路，所有 slog 日志和数据库记录都携带

## 3. 功能需求

### F-S01：统计 API（`GET /v1/stats`）

**描述：** 返回系统运行状态聚合指标，支持按时间范围筛选。

**请求参数：**
- `range`：`today` / `week` / `month` / `all`（默认 `today`）
- `device_id`：可选，按设备过滤

**返回指标：**

| 类别 | 指标 | 计算方式 |
|------|------|----------|
| Agent 运行 | 总 Run 数 | COUNT(agent_runs) |
| | 成功率 | completed / (completed + partial_success + failed) |
| | 部分成功率 | partial_success / total |
| | 失败率 | failed / total |
| | 平均耗时(ms) | AVG(duration_ms) WHERE status IN (completed, partial_success) |
| | P95 耗时(ms) | 近似计算或排序取值 |
| LLM 成本 | 总调用次数 | COUNT(llm_calls) |
| | 总 Token 数 | SUM(prompt_tokens + completion_tokens) |
| | 总费用(USD) | SUM(estimated_cost_usd) |
| | 平均每张截图费用 | 总费用 / 成功 Run 数 |
| OCR 质量 | 空文本率 | OCR 步骤输出 ocr_text 为空或兜底文本的比例（从 screenshots 表统计） |
| 任务队列 | 当前排队中 | COUNT(agent_tasks WHERE status='queued') |
| | 当前执行中 | COUNT(agent_tasks WHERE status='running') |
| | 死信数 | COUNT(agent_tasks WHERE status='dead') |
| 截图总览 | 总截图数 | COUNT(screenshots) |
| | 已整理数 | COUNT(screenshots WHERE status='organized') |

### F-S02：请求链路追踪

**描述：** 每个 HTTP 请求生成唯一 `request_id`，贯穿全链路。

**行为规范：**

| 层 | 行为 |
|----|------|
| HTTP 中间件 | 生成 `req_{nanoid}` 格式的 request_id，写入 `X-Request-ID` 响应头 |
| HTTP 中间件 | 记录请求日志：method、path、status_code、duration、request_id |
| context 传递 | request_id 注入 context，所有下游通过 context 获取 |
| slog 全链路 | Agent Runner、Worker、Tool 执行的 slog 调用自动附加 request_id（如果 context 中有） |
| 数据库记录 | agent_runs 表新增 `request_id` 列（可选，用于事后关联查询） |

**对上传接口的特殊处理：**
上传接口的 HTTP request_id 和 Worker 执行的 request_id 是不同的：
- 上传请求有自己的 request_id（记录上传行为）
- Worker 执行时生成新的 request_id（记录执行行为）
- 两者通过 `run_id` 关联

## 4. 非功能需求

| 项 | 要求 |
|----|------|
| 性能 | 统计 API 在万级数据量下 < 500ms 返回 |
| 兼容 | 现有 API 不受影响，request_id 是新增响应头 |
| 可测试 | 统计查询有单元测试，中间件有测试 |

## 5. 不在范围内

- Prometheus metrics 导出（后续可加）
- OpenTelemetry span 接入
- 前端统计大盘页面
- 告警通知

## 6. 数据库变更

- `agent_runs` 表新增 `request_id VARCHAR(64) NULL` 列
- 新增索引 `idx_agent_runs_created (created_at)` 用于时间范围统计

## 7. 影响分析

| 模块 | 影响 |
|------|------|
| 新增 `internal/httpapi/middleware.go` | request_id 生成 + 请求日志 + context 注入 |
| 新增 `internal/httpapi/requestid.go` | context key 定义 + 取值辅助函数 |
| 修改 `internal/httpapi/server.go` | 中间件链挂载 + 新增 stats 路由 |
| 修改 `internal/store/repository.go` | 新增 GetStats 方法 |
| 修改 `internal/store/store.go` | MySQL 统计查询实现 |
| 修改 `internal/store/memory.go` | 内存统计实现 |
| 修改 `internal/agent/runner.go` | 从 context 取 request_id 传给 slog |
| 修改 `internal/agent/worker.go` | 执行任务前注入新 request_id 到 context |
| 新增 `migrations/003_request_id.sql` | agent_runs 表加列 + 索引 |
| 新增测试文件 | 中间件测试 + 统计查询测试 |
