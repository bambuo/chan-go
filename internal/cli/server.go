package cli

import (
	"context"
	"log/slog"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

var (
	symbols string
)

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
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("引擎已启动", "symbols", symbols)

	// 等待中断信号
	<-ctx.Done()

	slog.Info("收到关闭信号，引擎退出")
	return nil
}
