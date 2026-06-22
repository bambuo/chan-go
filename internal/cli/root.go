// Package cli 提供 cobra 命令行定义与系统启动逻辑。
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"trade/internal/binance"
	"trade/internal/chanlun"
	"trade/internal/config"
	"trade/internal/db"
	"trade/internal/eventbus"
	"trade/internal/log"
	"trade/internal/types"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// 默认配置常量。
const (
	defaultLogLevel = "info"
	defaultLogJSON  = true
	defaultDBPath   = "file:data/klines.db?cache=shared&_journal_mode=WAL"
	defaultSymbols  = "btcusdt,ethusdt"
	defaultInterval = "1m"
)

// Execute 运行根命令。
func Execute() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "缠论K线分析系统",
		Long: `基于币安1分钟K线数据流的缠中说禅K线分析系统。

订阅指定交易对的实时K线流，经过包含处理和分型分析，
并将闭合K线持久化到SQLite数据库中。`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			viper.SetEnvPrefix("CL")
			viper.AutomaticEnv()
			return viper.BindPFlags(cmd.Flags())
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			var cfg config.Config

			cfg.LogLevel = viper.GetString("log-level")
			cfg.LogJSON = viper.GetBool("log-json")
			cfg.DBPath = viper.GetString("db")

			syms := viper.GetString("symbols")
			for _, s := range splitAndTrim(syms, ",") {
				if s != "" {
					cfg.Symbols = append(cfg.Symbols, s)
				}
			}
			if len(cfg.Symbols) == 0 {
				return fmt.Errorf("至少需要一个交易对")
			}

			cfg.Interval = viper.GetString("interval")
			cfg.WSURL = viper.GetString("ws-url")

			return run(cfg)
		},
	}

	flags := cmd.Flags()
	flags.String("log-level", envDefault("CL_LOG_LEVEL", defaultLogLevel),
		"日志级别 (debug/info/warn/error)  [env: CL_LOG_LEVEL]")
	flags.Bool("log-json", envDefaultBool("CL_LOG_JSON", defaultLogJSON),
		"JSON 格式输出日志  [env: CL_LOG_JSON]")
	flags.String("db", envDefault("CL_DB_PATH", defaultDBPath),
		"SQLite 数据库路径  [env: CL_DB_PATH]")
	flags.String("symbols", envDefault("CL_SYMBOLS", defaultSymbols),
		"交易对列表，逗号分隔  [env: CL_SYMBOLS]")
	flags.String("interval", envDefault("CL_INTERVAL", defaultInterval),
		"K线时间周期  [env: CL_INTERVAL]")
	flags.String("ws-url", envDefault("CL_WS_URL", ""),
		"WebSocket 地址（留空使用币安默认）  [env: CL_WS_URL]")

	return cmd
}

// run 实际启动逻辑，由 cobra RunE 调用。
func run(cfg config.Config) error {
	log.Init(log.Config{
		Level:     cfg.LogLevel,
		JSON:      cfg.LogJSON,
		AddSource: true,
		Output:    os.Stdout,
	})
	logger := log.Component("main")
	logger.Info("启动缠论分析系统", "config", fmt.Sprintf("%+v", cfg))

	bus := eventbus.New()

	if err := os.MkdirAll(filepath.Dir(cfg.DBFile()), 0755); err != nil {
		return fmt.Errorf("创建数据目录: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dbClient, err := db.NewClient(ctx, cfg.DBPath)
	if err != nil {
		return fmt.Errorf("初始化数据库: %w", err)
	}
	defer dbClient.Close()

	dbSubID := bus.Subscribe(types.EventKlineClosed, dbClient.InsertClosedKlineHandler(ctx))
	defer bus.Unsubscribe(types.EventKlineClosed, dbSubID)

	containProc := chanlun.NewContainProcessor()
	fractalProc := chanlun.NewFractalProcessor()

	chanlunSubID := bus.Subscribe(types.EventKlineRealtime, func(evt types.KlineEvent) {
		elements := containProc.Process(evt.Kline)
		if len(elements) > 0 {
			last := elements[len(elements)-1]
			fractals := fractalProc.Process(last)
			if len(fractals) > 0 && fractals[len(fractals)-1].Confirmed {
				logger.Debug("识别到分型",
					"type", fractals[len(fractals)-1].Type,
					"high", fractals[len(fractals)-1].High,
					"low", fractals[len(fractals)-1].Low,
				)
			}
		}
	})
	defer bus.Unsubscribe(types.EventKlineRealtime, chanlunSubID)

	bus.Subscribe(types.EventKlineClosed, func(evt types.KlineEvent) {
		elements := containProc.Process(evt.Kline)
		if len(elements) > 0 {
			fractalProc.Process(elements[len(elements)-1])
		}
	})

	wsOpts := []binance.WSClientOption{}
	if cfg.WSURL != "" {
		wsOpts = append(wsOpts, binance.WithWSURL(cfg.WSURL))
	}
	wsClient := binance.NewWSClient(cfg.Symbols, cfg.Interval, bus, wsOpts...)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		logger.Info("正在关闭...")
		cancel()
		wsClient.Stop()
	}()

	logger.Info("系统初始化完成，启动WebSocket流",
		"symbols", cfg.Symbols,
		"interval", cfg.Interval,
	)

	if err := wsClient.Start(ctx); err != nil && err != context.Canceled {
		return err
	}

	logger.Info("系统已停止")
	return nil
}

// envDefault 环境变量值或备用默认值。
func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envDefaultBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	switch v {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}

func splitAndTrim(s, sep string) []string {
	var out []string
	for _, p := range strings.Split(s, sep) {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
