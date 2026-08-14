# 需求分析：企业级 Agent 运行时升级

> **版本**：v1.0 | **日期**：2026-07-17 | **状态**：待确认

## 1. 背景

v0.1 已完成 Agent 基本骨架（固定 3 步工作流 + 追踪 + LLM/OCR），但存在以下核心问题：

- Agent 在 HTTP 请求内同步阻塞执行，无法处理慢任务
- 工作流硬编码在 `Organizer.runWorkflow()` 中，无法扩展新 Agent 类型
- 任何一步失败整个 Run 标记为 failed，OCR 成功的结果也丢失
- 零测试覆盖，无法安全重构

## 2. 目标

将 CapSnap 后端从"能跑的 demo"升级为"企业级 Agent 运行时"，具体：

1. **异步执行**：上传截图后立即返回，Agent 在后台执行
2. **声明式编排**：Agent 类型通过 StepDefinition 声明工作流，Runtime 统一执行
3. **Step 级韧性**：单步超时、重试、部分成功
4. **可测试**：核心路径有单元测试和集成测试

## 3. 功能需求

### F-R01：异步 Agent Worker

**描述：** HTTP 接口只负责创建 AgentRun 记录并入队，Worker goroutine 从 MySQL 任务表消费并执行。

**用户感知：**
- `POST /v1/screenshots` 上传后 < 200ms 返回 `run_id`，不再等待 Agent 完成
- `GET /v1/agent-runs/{id}` 可查看实时执行状态（queued → running → completed/failed/partial_success）
- `GET /v1/screenshots/{id}` 返回当前已有的结果（可能还在处理中）

**技术要求：**
- MySQL 任务表作为队列，支持竞争消费（`SELECT ... FOR UPDATE SKIP LOCKED`）
- Worker 嵌入 API 进程，作为后台 goroutine 启动
- 支持优雅关停（收到 SIGTERM 后完成当前任务再退出）
- 失败任务支持自动重试（最多 3 次，指数退避）
- 超过重试次数的任务标记为 dead，不再消费

**任务表字段（新增 `agent_tasks` 表）：**
- `id`：自增主键
- `run_id`：关联 agent_runs
- `status`：queued / running / completed / failed / dead
- `attempts`：已尝试次数
- `max_attempts`：最大尝试次数（默认 3）
- `last_error`：最近一次错误信息
- `locked_at`：锁定时间（用于超时检测）
- `locked_by`：锁定者标识（worker_id）
- `scheduled_at`：计划执行时间（支持退避延迟）
- `created_at` / `updated_at`

### F-R02：声明式 Agent Pipeline

**描述：** 将 Agent 工作流从硬编码函数重构为声明式 Pipeline。每种 Agent 类型注册一组 StepDefinition，Runtime 按顺序执行。

**核心概念：**
- `AgentType`：Agent 类型注册（如 `screenshot_organize`），包含名称和步骤定义列表
- `StepDefinition`：步骤声明，包含步骤名、对应 Tool 名、输入映射函数、超时、重试策略、失败策略（abort / skip / fallback）
- `AgentRunner`：通用运行器，接收 AgentType + 输入，按 StepDefinition 逐步执行

**v0.1 只注册一种 Agent 类型（screenshot_organize），但架构必须支持注册多种。**

**对现有代码的影响：**
- `Organizer` 拆分为 `AgentRunner`（通用）+ `screenshot_organize` 类型注册（业务）
- `Tracer` 逻辑保留，嵌入 `AgentRunner` 中自动记录
- 现有 API 行为不变，内部实现替换

### F-R03：单步重试与部分成功

**描述：** Agent Run 的状态从二元的 completed/failed 扩展，支持部分成功。每个 Step 独立管理自己的重试和失败策略。

**状态定义：**
- `completed`：所有步骤成功
- `partial_success`：关键步骤成功但有非关键步骤失败（如 OCR 成功但 LLM 失败）
- `failed`：关键步骤失败

**Step 失败策略：**
- `abort`：步骤失败则整个 Run 失败（默认）
- `skip`：步骤失败跳过，继续后续步骤
- `continue_degraded`：步骤失败后降级继续（如 LLM 失败用 mock 结果）

**Step 重试策略：**
- 每个 StepDefinition 可指定 `maxRetries`（默认 0）和 `retryDelay`
- 重试在 Step 级别发生，不影响其他步骤
- 每次重试都记录到 agent_steps 表

**对截图整理场景的应用：**
- OCR 步骤：失败策略 abort（没有 OCR 文本后续无法进行）
- LLM 步骤：失败策略 continue_degraded（使用 mock insight 降级），重试 1 次
- Memory 步骤：失败策略 abort，重试 1 次

### F-R04：核心测试

**描述：** 为关键路径编写测试，确保重构安全。

**测试范围：**
- Agent Pipeline 测试：使用 Memory store + mock Tool 测试正常流程、步骤失败、部分成功、重试
- Worker 测试：任务消费、并发锁、重试逻辑、优雅关停
- Tool 单元测试：OCR Tool（mock OCR Provider）、LLM Insight Tool（mock LLM Provider）、Memory Tool
- API 集成测试：上传 → 异步执行 → 查询状态 → 查询结果

## 4. 非功能需求

| 项 | 要求 |
|----|------|
| 性能 | 上传接口 < 200ms 返回；Worker 轮询间隔 1-5 秒可配置 |
| 可靠性 | Worker 崩溃重启后自动恢复未完成任务；锁超时自动释放（5 分钟） |
| 可观测 | 每步耗时、重试次数、降级行为都记录到追踪表 |
| 兼容性 | 现有 API 端点行为保持向后兼容（返回格式可扩展字段但不删字段） |
| 可测试 | 核心路径测试覆盖率 > 60% |

## 5. 不在范围内

- iOS 客户端（后续单独做）
- 独立 Worker 进程部署（v0.1 先嵌入 API 进程）
- 第二种 Agent 类型（架构预留，不实现）
- 向量搜索 / 语义搜索
- SSE / WebSocket 推送（客户端用轮询）
- LLM 多 Provider 路由

## 6. 数据库变更

新增 `agent_tasks` 表（任务队列），修改 `agent_runs` 表增加状态。

具体 DDL 在技术设计阶段产出。

## 7. 影响分析

| 模块 | 影响 |
|------|------|
| `internal/agent/organizer.go` | 重构拆分为 AgentRunner + 类型注册 |
| `internal/agent/trace.go` | 保留，嵌入 AgentRunner |
| `internal/httpapi/server.go` | 上传接口改为异步返回 |
| `internal/store/repository.go` | 新增任务队列相关方法 |
| `internal/store/store.go` | 新增 MySQL 任务队列实现 |
| `internal/store/memory.go` | 新增内存任务队列实现（用于测试） |
| `migrations/` | 新增迁移文件 |
| 新增 `internal/agent/runner.go` | 通用 Agent 运行器 |
| 新增 `internal/agent/pipeline.go` | StepDefinition + AgentType 定义 |
| 新增 `internal/agent/worker.go` | 异步 Worker |
| 新增 `internal/agent/screenshot_organize.go` | 截图整理 Agent 类型注册 |
| 新增 `*_test.go` | 各模块测试文件 |
