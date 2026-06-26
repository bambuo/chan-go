// Package config 提供应用配置的加载与管理。
package config

import (
	"os"
	"strconv"
	"time"
)

// Config 是应用全局配置。
type Config struct {
	Redis  RedisConfig
	Server ServerConfig
}

// RedisConfig 包含 Redis 连接配置。
type RedisConfig struct {
	Addr         string
	Password     string
	DB           int
	URL          string
	MaxRetries   int
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// ServerConfig 包含服务运行配置。
type ServerConfig struct {
	Symbols       []string
	StreamPrefix  string
	ConsumerGroup string
}

// ConfigBuilder 是 Config 的构建器，支持链式调用。
type ConfigBuilder struct {
	cfg *Config
}

// NewConfigBuilder 创建一个配置构建器。
func NewConfigBuilder() *ConfigBuilder {
	return &ConfigBuilder{
		cfg: &Config{
			Redis: RedisConfig{
				Addr:         "127.0.0.1:6379",
				Password:     "",
				DB:           0,
				MaxRetries:   0,
				DialTimeout:  5 * time.Second,
				ReadTimeout:  3 * time.Second,
				WriteTimeout: 3 * time.Second,
			},
			Server: ServerConfig{
				Symbols:       []string{},
				StreamPrefix:  "trade:kline",
				ConsumerGroup: "chan-go-group",
			},
		},
	}
}

// RedisAddr 设置 Redis 地址。
func (b *ConfigBuilder) RedisAddr(v string) *ConfigBuilder {
	b.cfg.Redis.Addr = v
	return b
}

// RedisPassword 设置 Redis 密码。
func (b *ConfigBuilder) RedisPassword(v string) *ConfigBuilder {
	b.cfg.Redis.Password = v
	return b
}

// RedisDB 设置 Redis 数据库编号。
func (b *ConfigBuilder) RedisDB(v int) *ConfigBuilder {
	b.cfg.Redis.DB = v
	return b
}

// RedisURL 设置完整的 Redis URL。
func (b *ConfigBuilder) RedisURL(v string) *ConfigBuilder {
	b.cfg.Redis.URL = v
	return b
}

// AddSymbol 添加一个监控交易对。
func (b *ConfigBuilder) AddSymbol(v string) *ConfigBuilder {
	b.cfg.Server.Symbols = append(b.cfg.Server.Symbols, v)
	return b
}

// StreamPrefix 设置 Stream 前缀。
func (b *ConfigBuilder) StreamPrefix(v string) *ConfigBuilder {
	b.cfg.Server.StreamPrefix = v
	return b
}

// ConsumerGroup 设置消费组名。
func (b *ConfigBuilder) ConsumerGroup(v string) *ConfigBuilder {
	b.cfg.Server.ConsumerGroup = v
	return b
}

// Build 构建配置。
func (b *ConfigBuilder) Build() *Config {
	if len(b.cfg.Server.Symbols) == 0 {
		b.cfg.Server.Symbols = []string{"BTCUSDT", "ETHUSDT"}
	}
	return b.cfg
}

// DefaultConfig 返回默认开发配置。
func DefaultConfig() *Config {
	return NewConfigBuilder().
		AddSymbol("BTCUSDT").
		AddSymbol("ETHUSDT").
		Build()
}

// LoadFromEnv 从环境变量加载配置覆盖默认值。
func (c *Config) LoadFromEnv() *Config {
	if v := os.Getenv("REDIS_ADDR"); v != "" {
		c.Redis.Addr = v
	}
	if v := os.Getenv("REDIS_PASSWORD"); v != "" {
		c.Redis.Password = v
	}
	if v := os.Getenv("REDIS_DB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Redis.DB = n
		}
	}
	if v := os.Getenv("REDIS_URL"); v != "" {
		c.Redis.URL = v
	}
	if v := os.Getenv("SYMBOLS"); v != "" {
		c.Server.Symbols = splitAndTrim(v, ",")
	}
	return c
}

func splitAndTrim(s, sep string) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if string(s[i]) == sep {
			if i > start {
				result = append(result, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		result = append(result, s[start:])
	}
	return result
}
