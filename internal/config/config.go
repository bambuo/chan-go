// Package config 提供应用配置的加载与管理.
package config

import (
	"os"
	"strconv"
	"time"
)

// Config 是应用全局配置.
type Config struct {
	Redis  RedisConfig
	Server ServerConfig
}

// RedisConfig 包含 Redis 连接配置.
type RedisConfig struct {
	// Addr 是 Redis 地址，格式 host:port.
	Addr string
	// Password 是 Redis 连接密码.
	Password string
	// DB 是 Redis 数据库编号.
	DB int
	// URL 是完整的 Redis URL (如 redis://user:pass@host:6379/0).
	// 若设置此值，会优先用于连接，忽略 Addr/Password/DB.
	URL string
	// MaxRetries 是最大重试次数，0 表示不重试，-1 表示默认.
	MaxRetries int
	// DialTimeout 是连接超时.
	DialTimeout time.Duration
	// ReadTimeout 是读取超时.
	ReadTimeout time.Duration
	// WriteTimeout 是写入超时.
	WriteTimeout time.Duration
}

// ServerConfig 包含服务运行配置.
type ServerConfig struct {
	// Symbols 是交易对列表，逗号分隔.
	Symbols string
}

// Load 从环境变量加载配置，返回带默认值的 Config.
func Load() Config {
	return Config{
		Redis: RedisConfig{
			Addr:         getEnv("REDIS_ADDR", "localhost:6379"),
			Password:     getEnv("REDIS_PASSWORD", ""),
			DB:           getEnvInt("REDIS_DB", 0),
			URL:          os.Getenv("REDIS_URL"),
			MaxRetries:   getEnvInt("REDIS_MAX_RETRIES", 0),
			DialTimeout:  getEnvDuration("REDIS_DIAL_TIMEOUT", 5*time.Second),
			ReadTimeout:  getEnvDuration("REDIS_READ_TIMEOUT", 3*time.Second),
			WriteTimeout: getEnvDuration("REDIS_WRITE_TIMEOUT", 3*time.Second),
		},
		Server: ServerConfig{
			Symbols: getEnv("SYMBOLS", "BTCUSDT,ETHUSDT"),
		},
	}
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
