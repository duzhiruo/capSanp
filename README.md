# capSanp

CapSnap 是一个以截图整理为第一个业务场景的 Go Agent 项目。

## 当前阶段

v0.1 企业级 Agent 运行时，核心能力：

- **声明式 Agent Pipeline** — OCR → LLM → Memory → Embedding
- **异步 Worker** — MySQL 任务队列，`SELECT FOR UPDATE SKIP LOCKED`
- **Step 级韧性** — 超时 / 重试 / abort·skip·degraded
- **混合搜索** — MySQL FULLTEXT + Qdrant 语义 + RRF 融合
- **链路追踪** — request_id 全链路贯通
- **统计 API** — Agent/LLM/OCR/Queue/Screenshots
- macOS Vision OCR + DashScope Embedding + OpenAI-compatible LLM

## 快速启动

```bash
make ocr-build
docker compose up -d    # MySQL + Qdrant
cp .env.example .env    # 填写 LLM_API_KEY
make migrate
make dev
```

或内存预览：`make dev-memory`（无 MySQL/Qdrant，语义搜索用内存向量库）。

调试 UI：http://localhost:8080

## 文档

- [docs/prd.md](docs/prd.md) — 产品需求
- [docs/implementation-plan.md](docs/implementation-plan.md) — 实施计划
- [docs/dev-setup.md](docs/dev-setup.md) — 开发环境
- [docs/设计文档-向量搜索与语义搜索.md](docs/设计文档-向量搜索与语义搜索.md) — 向量搜索设计
- [docs/设计文档-统计API与链路追踪.md](docs/设计文档-统计API与链路追踪.md) — 统计与链路追踪
