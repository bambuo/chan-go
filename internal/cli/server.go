package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"trade/internal/app"
	"trade/internal/config"
	"trade/internal/redis"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "启动信号分析引擎服务",
	RunE:  runServer,
}

func runServer(_ *cobra.Command, _ []string) error {
	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 初始化 Redis 连接
	rdb, err := redis.NewClient(ctx, cfg.Redis)
	if err != nil {
		return fmt.Errorf("初始化 Redis 失败: %w", err)
	}
	defer func() {
		if err := rdb.Close(); err != nil {
			slog.Error("关闭 Redis 连接时出错", "error", err)
		}
	}()

	// 创建引擎并启动（自动发现 trade:kline:* 流）
	engine := app.New(rdb.Client)
	engine.Start(ctx)

	slog.Info("引擎已启动")

	// 等待中断信号
	<-ctx.Done()
	slog.Info("收到关闭信号，正在停止引擎...")
	engine.Shutdown()
	slog.Info("引擎已安全关闭")
	return nil
}
