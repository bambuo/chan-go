package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"trade/internal/app"
	"trade/internal/config"
	"trade/internal/redis"
)

var symbols string

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "启动信号分析引擎服务",
	RunE:  runServer,
}

func init() {
	fs := serverCmd.Flags()
	fs.StringVar(&symbols, "symbols", "BTCUSDT,ETHUSDT", "交易对列表，逗号分隔")
}

func runServer(_ *cobra.Command, _ []string) error {
	cfg := config.Load()
	if symbols != "" {
		cfg.Server.Symbols = symbols
	}

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

	symList := parseSymbols(cfg.Server.Symbols)
	if len(symList) == 0 {
		return errors.New("未指定交易对")
	}

	// 创建引擎并启动
	engine := app.New(rdb.Client, symList)
	if err := engine.Start(ctx); err != nil {
		return err
	}

	slog.Info("引擎已启动", "symbols", symList)

	// 等待中断信号
	<-ctx.Done()
	slog.Info("收到关闭信号，正在停止引擎...")
	engine.Shutdown()
	slog.Info("引擎已安全关闭")
	return nil
}

// parseSymbols 将逗号分隔的交易对字符串解析为切片。
func parseSymbols(s string) []string {
	parts := strings.Split(s, ",")
	res := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			res = append(res, trimmed)
		}
	}
	return res
}
