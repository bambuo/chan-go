// Package cli 提供 cobra 命令行定义。
// 所有业务逻辑委托给 internal/app 包。
package cli

import (
	"context"
	"os"
	"strings"

	"trade/internal/app"
	"trade/internal/config"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

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
			cfg := parseConfig()
			sys, err := app.New(cfg)
			if err != nil {
				return err
			}
			return sys.Run(context.Background())
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

// parseConfig 从 viper 读取配置。
func parseConfig() config.Config {
	var cfg config.Config

	cfg.LogLevel = viper.GetString("log-level")
	cfg.LogJSON = viper.GetBool("log-json")
	cfg.DBPath = viper.GetString("db")

	for _, s := range splitAndTrim(viper.GetString("symbols"), ",") {
		if s != "" {
			cfg.Symbols = append(cfg.Symbols, s)
		}
	}

	cfg.Interval = viper.GetString("interval")
	cfg.WSURL = viper.GetString("ws-url")
	return cfg
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
