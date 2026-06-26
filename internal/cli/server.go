package cli

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"trade/internal/app"
	"trade/internal/config"
	"trade/internal/logger"
	"trade/internal/redis"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "启动信号分析引擎服务",
	RunE:  runServer,
}

func runServer(_ *cobra.Command, _ []string) error {
	cfg := config.Load()

	log, err := logger.New()
	if err != nil {
		return fmt.Errorf("初始化日志失败: %w", err)
	}
	defer func() { _ = log.Sync() }()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	rdb, err := redis.NewClient(ctx, cfg.Redis, log.With("module", "redis"))
	if err != nil {
		return fmt.Errorf("初始化 Redis 失败: %w", err)
	}
	defer func() {
		if err := rdb.Close(); err != nil {
			log.Error("关闭 Redis 连接时出错", "error", err)
		}
	}()

	engine := app.New(rdb.Client, log.With("module", "engine"))
	engine.Start(ctx)

	log.Info("引擎已启动")

	<-ctx.Done()
	log.Info("收到关闭信号，正在停止引擎...")
	engine.Shutdown()
	log.Info("引擎已安全关闭")
	return nil
}
