// Package cli 提供 cobra 命令行定义。
package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"trade/internal/app"
	"trade/internal/config"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	defaultLogLevel           = "info"
	defaultLogJSON            = true
	defaultSymbols            = "BTCUSDT,ETHUSDT"
	defaultRedisAddr          = "localhost:6379"
	defaultHTTPAddr           = "0.0.0.0"
	defaultHTTPPort           = 8080
	defaultSnapshotDir        = "data/snapshots"
	defaultSnapshotPeriod     = 300
	defaultRetainSnapshot     = 24
	defaultStreamPrefix       = "chan:klines"
	defaultConsumerGroup      = "chan-engine"
	defaultOutputStreamPrefix = "chan:signals"
)

// Execute runs the root command.
func Execute() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "信号分析系统",
		Long: `基于缠中说禅理论的实时信号分析引擎。

作为黑盒计算引擎消费 Redis Stream 中的 K 线数据，
计算多级别缠论结构并产出结构化买卖点信号，
通过 REST API 和 WebSocket 对外输出。`,
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

	// === 日志 ===
	flags.String("log-level", envDefault("CL_LOG_LEVEL", defaultLogLevel),
		"日志级别 (debug/info/warn/error)")
	flags.Bool("log-json", envDefaultBool("CL_LOG_JSON", defaultLogJSON),
		"JSON 格式输出日志")

	// === 数据源 ===
	flags.String("symbols", envDefault("CL_SYMBOLS", defaultSymbols),
		"交易对列表，逗号分隔（大写，如 BTCUSDT,ETHUSDT）")

	// === Redis（M1 输入网关）===
	flags.String("redis-addr", envDefault("CL_REDIS_ADDR", defaultRedisAddr),
		"Redis 地址")
	flags.String("redis-password", envDefault("CL_REDIS_PASSWORD", ""),
		"Redis 密码")
	flags.Int("redis-db", envDefaultInt("CL_REDIS_DB", 0),
		"Redis DB 编号")
	flags.String("stream-prefix", envDefault("CL_STREAM_PREFIX", defaultStreamPrefix),
		"K线输入 Stream 前缀")
	flags.String("consumer-group", envDefault("CL_CONSUMER_GROUP", defaultConsumerGroup),
		"消费组名称")

	// === HTTP（M8 输出网关）===
	flags.String("http-addr", envDefault("CL_HTTP_ADDR", defaultHTTPAddr),
		"HTTP 监听地址")
	flags.Int("http-port", envDefaultInt("CL_HTTP_PORT", defaultHTTPPort),
		"HTTP 端口")
	flags.String("output-stream-prefix", envDefault("CL_OUTPUT_STREAM_PREFIX", defaultOutputStreamPrefix),
		"Redis Stream 输出前缀")

	// === 快照（M0）===
	flags.String("snapshot-dir", envDefault("CL_SNAPSHOT_DIR", defaultSnapshotDir),
		"快照存储目录")
	flags.Int("snapshot-period", envDefaultInt("CL_SNAPSHOT_PERIOD", defaultSnapshotPeriod),
		"快照周期（秒）")
	flags.Int("snapshot-retain", envDefaultInt("CL_SNAPSHOT_RETAIN", defaultRetainSnapshot),
		"保留最近快照数")

	// === 调试结构输出 ===
	flags.String("debug-structure-dir", envDefault("CL_DEBUG_STRUCTURE_DIR", ""),
		"缠论结构调试输出目录（非空时启用，如 data/debug）")

	return cmd
}

// parseConfig 从 viper 读取配置。
func parseConfig() config.Config {
	cfg := config.Default()

	cfg.LogLevel = viper.GetString("log-level")
	cfg.LogJSON = viper.GetBool("log-json")

	for _, s := range splitAndTrim(viper.GetString("symbols"), ",") {
		if s != "" {
			cfg.Symbols = append(cfg.Symbols, strings.ToUpper(s))
		}
	}

	cfg.RedisAddr = viper.GetString("redis-addr")
	cfg.RedisPassword = viper.GetString("redis-password")
	cfg.RedisDB = viper.GetInt("redis-db")
	cfg.StreamPrefix = viper.GetString("stream-prefix")
	cfg.ConsumerGroup = viper.GetString("consumer-group")

	cfg.HTTPAddr = viper.GetString("http-addr")
	cfg.HTTPPort = viper.GetInt("http-port")
	cfg.OutputStreamPrefix = viper.GetString("output-stream-prefix")

	cfg.SnapshotDir = viper.GetString("snapshot-dir")
	cfg.SnapshotPeriod = viper.GetInt("snapshot-period")
	cfg.SnapshotRetain = viper.GetInt("snapshot-retain")

	cfg.DebugStructureDir = viper.GetString("debug-structure-dir")

	return cfg
}

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

func envDefaultInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
		return def
	}
	return n
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
