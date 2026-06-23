// Package config 系统运行时配置结构体（PRD §16.6）。
package config

// Config 系统运行时配置。
type Config struct {
	// === 日志 ===
	LogLevel string // 日志级别 (debug/info/warn/error)
	LogJSON  bool   // JSON 格式输出日志

	// === 数据源（Redis Stream）===
	Symbols []string // 交易对列表（大写，如 BTCUSDT）

	// === 输入网关（M1）— Redis Stream ===
	RedisAddr     string // Redis 地址（如 "localhost:6379"）
	RedisPassword string // Redis 密码
	RedisDB       int    // Redis DB 编号
	StreamPrefix  string // K 线输入 Stream 前缀（默认 "chan:klines"）
	ConsumerGroup string // 消费组名称（默认 "chan-engine"）

	// === 输出网关（M8）===
	HTTPAddr           string // HTTP 监听地址（如 "0.0.0.0"）
	HTTPPort           int    // HTTP 端口（如 8080）
	OutputStreamPrefix string // Redis Stream 输出前缀（默认 "chan:signals"）

	// === 快照（M0）===
	SnapshotDir    string // 快照存储目录
	SnapshotPeriod int    // 快照周期（秒，默认 300 = 5min）
	SnapshotRetain int    // 保留最近快照数（默认 24）

}

// Default 返回推荐的默认配置。
func Default() Config {
	return Config{
		LogLevel: "info",
		LogJSON:  true,
		Symbols:  []string{"BTCUSDT", "ETHUSDT"},

		RedisAddr:     "localhost:6379",
		RedisPassword: "",
		RedisDB:       0,
		StreamPrefix:  "chan:klines",
		ConsumerGroup: "chan-engine",

		HTTPAddr:           "0.0.0.0",
		HTTPPort:           8080,
		OutputStreamPrefix: "chan:signals",

		SnapshotDir:    "data/snapshots",
		SnapshotPeriod: 300,
		SnapshotRetain: 24,
	}
}
