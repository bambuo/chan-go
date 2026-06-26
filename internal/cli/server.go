package cli

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"trade/internal/app"
	"trade/internal/config"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "启动信号分析引擎服务",
	RunE:  runServer,
}

func runServer(_ *cobra.Command, _ []string) error {
	cfg := config.DefaultConfig().LoadFromEnv()

	application, err := app.NewApp(cfg)
	if err != nil {
		return fmt.Errorf("初始化应用失败: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := application.Run(ctx); err != nil {
		return fmt.Errorf("应用运行失败: %w", err)
	}

	<-ctx.Done()
	fmt.Println()

	application.Shutdown()
	return nil
}
