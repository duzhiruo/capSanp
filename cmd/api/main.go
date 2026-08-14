package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"capsnap/internal/app"
	"capsnap/internal/config"
)

func main() {
	app.SetupLogger()
	cfg := config.Load()

	application, err := app.New(context.Background(), cfg)
	if err != nil {
		slog.Error("应用启动失败", "error", err)
		os.Exit(1)
	}

	// 启动 Worker（后台 goroutine）
	workerCtx, workerCancel := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() {
		application.Worker.Start(workerCtx)
		close(workerDone)
	}()

	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: application.Server.Handler(),
	}

	// 优雅关停
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		slog.Info("收到关停信号", "signal", sig)

		workerCancel()
		slog.Info("等待 Worker 完成当前任务...")
		<-workerDone

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("HTTP 服务关停失败", "error", err)
		}
	}()

	slog.Info("CapSnap Agent 服务已启动", "addr", cfg.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("服务运行失败", "error", err)
	}
	slog.Info("服务已停止")
}
