// Package config 系统运行时配置结构体（PRD §16.6）。
package config

import "fmt"

// Config 系统运行时配置。
type Config struct {
	// === 日志 ===
	LogLevel string // 日志级别 (debug/info/warn/error)
	LogJSON  bool   // JSON 格式输出日志

	// === 数据源（当前直连模式，PRD 终态切换为 Redis Stream）===
	Symbols  []string // 交易对列表（大写，如 BTCUSDT）
	Interval string   // K线时间周期（如 "1m"）
	WSURL    string   // WebSocket 地址（空则使用币安默认）

	// === 输入网关（M1）— Redis Stream ===
	RedisAddr     string // Redis 地址（如 "localhost:6379"）
	RedisPassword string // Redis 密码
	RedisDB       int    // Redis DB 编号
	StreamPrefix  string // Stream 前缀（默认 "chan:klines"）
	ConsumerGroup string // 消费组名称（默认 "chan-engine"）

	// === 输出网关（M8）===
	HTTPAddr     string // HTTP 监听地址（如 "0.0.0.0"）
	HTTPPort     int    // HTTP 端口（如 8080）
	RateLimitRPS int    // REST 每 Key 每秒请求上限（默认 50）
	WSChannelMax int    // WS 每连接最大 channel 数（默认 20）

	// === 鉴权 ===
	AuthEnabled bool   // 是否启用 API Key 鉴权
	AuthKeyFile string // API Key 文件路径

	// === 快照（M0）===
	SnapshotDir     string // 快照存储目录
	SnapshotPeriod  int    // 快照周期（秒，默认 300 = 5min）
	SnapshotRetain  int    // 保留最近快照数（默认 24）

	// === 数据库（历史归档）===
	DBPath string // SQLite 数据库文件路径
}

// DSN 返回完整的 SQLite DSN。
func (c *Config) DSN() string {
	return fmt.Sprintf("file:%s?cache=shared&_journal_mode=WAL&_fk=1", c.DBPath)
}

// DBFile 返回数据库文件路径。
func (c *Config) DBFile() string {
	return c.DBPath
}

// Default 返回推荐的默认配置。
func Default() Config {
	return Config{
		LogLevel: "info",
		LogJSON:  true,
		Symbols:  []string{"BTCUSDT", "ETHUSDT"},
		Interval: "1m",

		RedisAddr:     "localhost:6379",
		RedisPassword: "",
		RedisDB:       0,
		StreamPrefix:  "chan:klines",
		ConsumerGroup: "chan-engine",

		HTTPAddr:     "0.0.0.0",
		HTTPPort:     8080,
		RateLimitRPS: 50,
		WSChannelMax: 20,

		AuthEnabled: false,
		AuthKeyFile: "",

		SnapshotDir:    "data/snapshots",
		SnapshotPeriod: 300,
		SnapshotRetain: 24,

		DBPath: "data/klines.db",
	}
}
