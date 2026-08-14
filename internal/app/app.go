package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"capsnap/internal/agent"
	"capsnap/internal/config"
	"capsnap/internal/embedding"
	"capsnap/internal/httpapi"
	"capsnap/internal/llm"
	"capsnap/internal/ocr"
	"capsnap/internal/storage"
	"capsnap/internal/store"
	"capsnap/internal/tools"
	"capsnap/internal/vectorstore"
)

// App 聚合后端依赖。
type App struct {
	Config    config.Config
	Store     store.Repository
	Storage   *storage.Local
	Embedder  embedding.Provider
	Vectors   vectorstore.VectorStore
	Runner    *agent.AgentRunner
	Worker    *agent.Worker
	Server    *httpapi.Server
}

func New(ctx context.Context, cfg config.Config) (*App, error) {
	repo, err := openRepository(ctx, cfg)
	if err != nil {
		return nil, err
	}

	localStorage, err := storage.NewLocal(cfg.UploadDir)
	if err != nil {
		return nil, fmt.Errorf("初始化本地存储失败: %w", err)
	}

	llmProvider := llm.NewOpenAICompatible(
		cfg.LLMBaseURL,
		cfg.LLMAPIKey,
		cfg.LLMModel,
		cfg.LLMTimeout,
		cfg.LLMInputCost,
		cfg.LLMOutputCost,
	)
	ocrProvider := ocr.NewVision(cfg.OCRBinaryPath, cfg.OCRScriptPath, cfg.OCRTimeout)

	embedder := embedding.NewDashScope(
		cfg.LLMBaseURL,
		cfg.LLMAPIKey,
		cfg.EmbeddingModel,
		cfg.EmbeddingDimension,
		cfg.LLMTimeout,
	)

	vectors, err := openVectorStore(ctx, cfg)
	if err != nil {
		slog.Warn("向量库不可用，语义搜索将降级为关键词搜索", "error", err)
		vectors = nil
	}

	toolList := []tools.Tool{
		tools.NewOCRTool(repo, ocrProvider),
		tools.NewLLMInsightTool(llmProvider),
		tools.NewMemoryTool(repo),
	}
	if vectors != nil {
		toolList = append(toolList, tools.NewEmbeddingTool(embedder, vectors))
	}

	registry := tools.NewRegistry(toolList...)
	runner := agent.NewAgentRunner(repo, registry)
	runner.RegisterType(agent.NewScreenshotOrganizeType())

	worker := agent.NewWorker(repo, runner, agent.WorkerOptions{
		PollInterval: cfg.WorkerPollInterval,
		LockTimeout:  cfg.WorkerLockTimeout,
		Concurrency:  cfg.WorkerConcurrency,
		WorkerID:     "api-worker",
	})

	server := httpapi.New(repo, localStorage, cfg.WorkerMaxAttempts, embedder, vectors)

	if cfg.LLMAPIKey == "" {
		slog.Warn("未配置 LLM_API_KEY，将使用 Mock LLM / Mock Embedding")
	}

	return &App{
		Config:   cfg,
		Store:    repo,
		Storage:  localStorage,
		Embedder: embedder,
		Vectors:  vectors,
		Runner:   runner,
		Worker:   worker,
		Server:   server,
	}, nil
}

func openVectorStore(ctx context.Context, cfg config.Config) (vectorstore.VectorStore, error) {
	if !cfg.QdrantEnabled || strings.EqualFold(cfg.DatabaseDSN, "memory") {
		slog.Info("使用内存向量库", "qdrant_enabled", cfg.QdrantEnabled, "dsn", cfg.DatabaseDSN)
		vs := vectorstore.NewMemory()
		return vs, vs.EnsureCollection(ctx)
	}
	vs, err := vectorstore.NewQdrant(cfg.QdrantHost, cfg.QdrantPort, cfg.QdrantCollection, cfg.EmbeddingDimension)
	if err != nil {
		return nil, err
	}
	if err := vs.EnsureCollection(ctx); err != nil {
		_ = vs.Close()
		return nil, err
	}
	slog.Info("Qdrant 已连接", "host", cfg.QdrantHost, "port", cfg.QdrantPort, "collection", cfg.QdrantCollection)
	return vs, nil
}

func openRepository(ctx context.Context, cfg config.Config) (store.Repository, error) {
	if strings.EqualFold(cfg.DatabaseDSN, "memory") {
		slog.Warn("当前使用内存存储模式，适合快速预览；重启后数据会丢失")
		return store.NewMemory(), nil
	}

	db, err := store.Open(cfg.DatabaseDSN)
	if err != nil {
		return nil, fmt.Errorf("连接 MySQL 失败: %w", err)
	}
	if cfg.AutoMigrate {
		for _, migration := range []string{
			"migrations/001_init.sql",
			"migrations/002_agent_tasks.sql",
			"migrations/003_request_id.sql",
		} {
			if err := db.ApplyMigration(ctx, migration); err != nil {
				_ = db.Close()
				return nil, fmt.Errorf("执行数据库迁移失败 (%s): %w", migration, err)
			}
		}
	}
	return db, nil
}

func SetupLogger() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
}
