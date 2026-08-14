package main

import (
	"context"
	"log/slog"
	"os"

	"capsnap/internal/config"
	"capsnap/internal/store"
)

func main() {
	cfg := config.Load()
	if cfg.DatabaseDSN == "memory" {
		slog.Error("migrate 不支持 memory 模式，请配置 MySQL DSN")
		os.Exit(1)
	}

	db, err := store.Open(cfg.DatabaseDSN)
	if err != nil {
		slog.Error("连接 MySQL 失败", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	for _, path := range []string{
		"migrations/001_init.sql",
		"migrations/002_agent_tasks.sql",
		"migrations/003_request_id.sql",
	} {
		if err := db.ApplyMigration(context.Background(), path); err != nil {
			slog.Error("执行迁移失败", "path", path, "error", err)
			os.Exit(1)
		}
		slog.Info("迁移完成", "path", path)
	}
}
