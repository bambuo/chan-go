// Package config 系统运行时配置结构体。
package config

import "fmt"

// Config 系统运行时配置。
type Config struct {
	LogLevel string   // 日志级别
	LogJSON  bool     // JSON 格式输出日志
	DBPath   string   // SQLite 数据库文件路径
	Symbols  []string // 交易对列表
	Interval string   // K线时间周期
	WSURL    string   // WebSocket 地址（空则使用币安默认）
}

// DSN 返回完整的 SQLite DSN（含连接参数）。
func (c *Config) DSN() string {
	return fmt.Sprintf("file:%s?cache=shared&_journal_mode=WAL&_fk=1", c.DBPath)
}

// DBFile 返回数据库文件路径（与 DBPath 相同）。
func (c *Config) DBFile() string {
	return c.DBPath
}
