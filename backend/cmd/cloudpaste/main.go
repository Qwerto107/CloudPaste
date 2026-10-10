package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cloudpaste/backend/internal/server"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := server.ConfigFromEnv()
	if err != nil {
		logger.Error("配置无效", "error", err)
		os.Exit(1)
	}
	app, err := server.New(cfg, logger)
	if err != nil {
		logger.Error("初始化失败", "error", err)
		os.Exit(1)
	}
	defer app.Close()
	httpServer := &http.Server{
		Addr: cfg.ListenAddr, Handler: app.Handler(),
		ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second,
		ReadTimeout:    30 * time.Second,
		MaxHeaderBytes: 32 << 10,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		// 给正在进行的下载留出结束时间，随后由进程退出关闭剩余连接。
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	logger.Info("Go 后端启动", "address", cfg.ListenAddr, "uploads", true)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("HTTP 服务失败", "error", err)
		os.Exit(1)
	}
}

// 镜像使用同一二进制检查服务，无需安装 shell 或额外下载工具。
func healthcheck() int {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8787"
	}
	client := http.Client{Timeout: 3 * time.Second}
	response, err := client.Get("http://127.0.0.1:" + port + "/api/health")
	if err != nil {
		return 1
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
