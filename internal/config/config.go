// Package config 系统运行时配置结构体。
package config

import "strings"

// Config 系统运行时配置。
type Config struct {
	LogLevel string   // 日志级别
	LogJSON  bool     // JSON 格式输出日志
	DBPath   string   // SQLite 数据库路径
	Symbols  []string // 交易对列表
	Interval string   // K线时间周期
	WSURL    string   // WebSocket 地址（空则使用币安默认）
}

// DBFile 返回数据库文件路径（不含查询参数）。
func (c *Config) DBFile() string {
	if idx := strings.Index(c.DBPath, "?"); idx >= 0 {
		return c.DBPath[:idx]
	}
	return c.DBPath
}
