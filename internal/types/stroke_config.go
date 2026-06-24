// Package types — 本文件：笔配置（笔.md）
package types

// StrokeConfig 笔识别算法配置（笔.md §2）。
// 本项目仅实现严笔（跨度≥3/4），不实现顶底即成笔的宽笔模式。
type StrokeConfig struct {
	Strict bool // 是否严格跨度（默认 true，严格=4，非严格=3）
}

// DefaultStrokeConfig 返回默认笔配置。
func DefaultStrokeConfig() StrokeConfig {
	return StrokeConfig{
		Strict: true,
	}
}
