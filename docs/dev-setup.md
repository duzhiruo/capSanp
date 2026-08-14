# CapSnap 开发环境说明

## 环境要求

- Go 1.23+
- macOS（当前 OCR 使用 Vision 框架）
- MySQL 8（正式开发推荐）
- 可选：Docker（用于启动 MySQL）

## 快速开始

### 1. 内存模式（最快预览）

适合验证 Agent 链路，不需要 MySQL：

```bash
make ocr-build
make dev-memory
```

访问 `http://localhost:8080`。

### 2. MySQL 模式（正式开发）

```bash
docker compose up -d mysql
cp .env.example .env
# 编辑 .env，填入 LLM_API_KEY
make ocr-build
make migrate
make dev
```

## 目录结构

```text
cmd/api        HTTP 服务入口
cmd/migrate    数据库迁移工具
internal/app   应用启动与依赖注入
internal/agent Agent Runtime 与截图整理 Agent
internal/tools 工具注册表与 OCR/LLM/Memory 工具
internal/llm   LLM Provider 与提示词模板
internal/ocr   OCR Provider（macOS Vision）
internal/store MySQL 与内存存储
migrations     数据库迁移
web            调试页面
tools          macOS Vision OCR 脚本
```

## 常用命令

```bash
make dev          # 使用 .env 启动服务
make dev-memory   # 内存模式启动
make ocr-build    # 编译 OCR 二进制
make migrate      # 执行 MySQL 迁移
make test         # 运行测试
make fmt          # 格式化代码
```

## LLM 配置示例（DeepSeek）

```env
LLM_BASE_URL=https://api.deepseek.com
LLM_MODEL=deepseek-chat
LLM_API_KEY=你的密钥
```

## 当前 v0.1 能力

- 上传截图并创建 Agent Run
- macOS Vision OCR 提取文字
- DeepSeek / OpenAI-compatible LLM 生成摘要、分类、标签
- 记录 Agent Step、Tool Call、LLM Call
- 关键词搜索与时间线浏览

## 下一步

- 接入 MySQL 作为默认持久化（已完成 schema，待稳定使用）
- 拆分 `cmd/worker` 支持异步 Agent 任务
- iOS SwiftUI 客户端
- 服务端 OCR（PaddleOCR / Tesseract）替代 macOS 专用方案
