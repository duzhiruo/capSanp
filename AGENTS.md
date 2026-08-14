# capSanp (CapSnap 知识捕手)

## 项目定位

智能截图整理与搜索的 Agent 后端（v0.1）。核心流程：上传截图 → OCR 提取文字 → LLM 生成摘要/标签 → 持久化 → 混合搜索（关键词 + 语义）。

## 技术栈

| 组件 | 选型 |
|------|------|
| 语言 | Go 1.23+ |
| 模块名 | `capsnap` |
| HTTP | 标准库 `net/http` + `ServeMux`（Go 1.22+ 路由模式） |
| 数据库 | MySQL 8.4（Docker）或内存模式 |
| DB 访问 | 原生 `database/sql`（无 ORM） |
| LLM | OpenAI 兼容 API（默认 DeepSeek / DashScope） |
| Embedding | DashScope text-embedding-v3（1024 维） |
| 向量库 | Qdrant（Docker）或内存暴力余弦 |
| OCR | macOS Vision（Swift CLI，仅 macOS） |
| 文件存储 | 本地 `data/uploads/` |
| 日志 | `log/slog` |
| 配置 | 环境变量（`.env`） |

## 目录结构

```
cmd/
├── api/main.go                    → HTTP 服务入口 + Worker 启动 + 优雅关停
├── migrate/main.go                → 数据库迁移工具
└── backfill/main.go               → 存量截图 embedding 补录
internal/
├── agent/                         → Pipeline / Runner / Worker
├── app/                           → 依赖注入/组装
├── config/                        → 环境变量配置
├── embedding/                     → Embedding Provider（DashScope + Mock）
├── httpapi/                       → REST + 中间件 + Stats + 混合搜索
├── reqctx/                        → request_id context
├── llm/                           → LLM provider + prompt
├── ocr/                           → OCR provider
├── storage/                       → 本地文件存储
├── store/                         → MySQL / memory 数据访问
├── tools/                         → Tool 注册表（OCR / LLM / Memory / Embedding）
└── vectorstore/                   → 向量存储（Qdrant / Memory）
migrations/
├── 001_init.sql
├── 002_agent_tasks.sql
└── 003_request_id.sql
```

## 分层架构约定

```
cmd/ → internal/app → internal/httpapi
                    → internal/agent → internal/tools
                    → internal/store / vectorstore / embedding
```

## Agent 运行时架构

### 异步执行流

```
POST /v1/screenshots → 创建 screenshot + agent_run + agent_task → 202 {run_id, status: "queued"}
Worker → dequeue → AgentRunner 按声明式 Pipeline 执行 → 更新状态
```

### 声明式 Pipeline（screenshot_organize）

1. **OCR** → `ocr.extract_text`（abort）
2. **LLM** → `llm.generate_insight`（continue_degraded，重试 1 次）
3. **Memory** → `memory.save_screenshot_insight`（abort，重试 1 次）
4. **Embedding** → `embedding.generate`（skip，失败不影响核心整理）

### 混合搜索

```
GET /v1/search?q=...&mode=hybrid
→ 并行 FULLTEXT + Qdrant ANN → RRF(k=60) 融合 → 返回
mode: keyword | semantic | hybrid（默认 hybrid，向量库不可用时自动降级）
```

## 本地开发

```bash
make ocr-build
docker compose up -d           # MySQL + Qdrant
cp .env.example .env           # 填写 LLM_API_KEY
make migrate
make dev                       # :8080

# 内存预览（无需 MySQL/Qdrant）
make dev-memory

# 存量截图补录 embedding
go run ./cmd/backfill --batch-size=10
```

## API 端点

| 方法 | 路径 | 描述 |
|------|------|------|
| POST | `/v1/screenshots` | 上传截图 + 触发 Agent |
| GET | `/v1/screenshots` | 截图列表 |
| GET | `/v1/screenshots/{id}` | 截图详情 |
| POST | `/v1/screenshots/{id}/retry-agent` | 重跑 Agent |
| GET | `/v1/agent-runs/{id}` | Agent 运行追踪 |
| GET | `/v1/search?q=...&mode=hybrid` | 混合搜索（keyword/semantic/hybrid） |
| GET | `/v1/stats?range=today` | 运营统计 |

## 当前状态

- v0.1 后端 + 企业级 Agent 运行时 + 统计/链路追踪 + 向量语义搜索
- 混合搜索：关键词 FULLTEXT + Qdrant 语义 + RRF 融合
- Embedding 作为 Pipeline 第 4 步，失败 skip 降级
- 未实现：iOS 客户端

## 已有文档

- `docs/prd.md` / `docs/implementation-plan.md` / `docs/dev-setup.md`
- `docs/需求分析-企业级Agent运行时.md` / `docs/设计文档-企业级Agent运行时.md`
- `docs/需求分析-统计API与链路追踪.md` / `docs/设计文档-统计API与链路追踪.md`
- `docs/需求分析-向量搜索与语义搜索.md` / `docs/设计文档-向量搜索与语义搜索.md`
