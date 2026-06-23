// Package types — 本文件：笔配置与算法类型定义（笔.md）
package types

// StrokeConfig 笔识别算法配置（笔.md §2）。
type StrokeConfig struct {
	Algorithm  string // "normal" 或 "topBottomAsStroke"
	Strict     bool   // 是否严格跨度（默认 true）
	GapAsKline bool   // 缺口是否计入跨度（默认 true）
}

// DefaultStrokeConfig 返回默认笔配置。
func DefaultStrokeConfig() StrokeConfig {
	return StrokeConfig{
		Algorithm:  "normal",
		Strict:     true,
		GapAsKline: true,
	}
}
