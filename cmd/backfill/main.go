package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"strings"

	"capsnap/internal/app"
	"capsnap/internal/config"
	"capsnap/internal/tools"
)

func main() {
	batchSize := flag.Int("batch-size", 10, "每批处理的截图数量")
	dryRun := flag.Bool("dry-run", false, "只统计不写入")
	flag.Parse()

	app.SetupLogger()
	cfg := config.Load()
	if strings.EqualFold(cfg.DatabaseDSN, "memory") {
		slog.Error("backfill 不支持 memory 模式")
		os.Exit(1)
	}

	ctx := context.Background()
	application, err := app.New(ctx, cfg)
	if err != nil {
		slog.Error("初始化失败", "error", err)
		os.Exit(1)
	}
	if application.Vectors == nil || application.Embedder == nil {
		slog.Error("向量库或 Embedding 未就绪，无法 backfill")
		os.Exit(1)
	}

	embedTool := tools.NewEmbeddingTool(application.Embedder, application.Vectors)
	offset := 0
	total, ok, fail := 0, 0, 0

	for {
		shots, err := application.Store.ListOrganizedScreenshots(ctx, *batchSize, offset)
		if err != nil {
			slog.Error("查询截图失败", "error", err)
			os.Exit(1)
		}
		if len(shots) == 0 {
			break
		}

		if *dryRun {
			slog.Info("dry-run", "offset", offset, "count", len(shots))
			total += len(shots)
			offset += len(shots)
			continue
		}

		for _, shot := range shots {
			_, err := embedTool.Execute(ctx, map[string]any{
				"screenshot_id": shot.ID,
				"device_id":     shot.DeviceID,
				"ocr_text":      shot.OCRText,
				"summary":       shot.Summary,
				"tags":          shot.TagsText,
			})
			if err != nil {
				slog.Warn("写入向量失败", "screenshot_id", shot.ID, "error", err)
				fail++
				continue
			}
			ok++
		}
		total += len(shots)
		offset += len(shots)
		slog.Info("进度", "processed", total, "ok", ok, "fail", fail)
	}

	slog.Info("backfill 完成", "total", total, "ok", ok, "fail", fail, "dry_run", *dryRun)
}
