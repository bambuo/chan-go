// Package types — 本文件：笔配置与算法类型定义（笔.md）
package types

// StrokeConfig 笔识别算法配置（笔.md §2）。
type StrokeConfig struct {
	Algorithm    string // "normal" 或 "topBottomAsStroke"
	Strict       bool   // 是否严格跨度（默认 true）
	FractalCheck string // "loose"/"half"/"strict"/"full"（默认 "half"）
	GapAsKline   bool   // 缺口是否计入跨度（默认 true）
	PeakEndPoint bool   // 笔终点是否必须为区间极值（默认 true）
	AllowSubPeak bool   // 是否允许次峰值修正被跳过（默认 true）
}

// DefaultStrokeConfig 返回默认笔配置。
func DefaultStrokeConfig() StrokeConfig {
	return StrokeConfig{
		Algorithm:    "normal",
		Strict:       true,
		FractalCheck: "half",
		GapAsKline:   true,
		PeakEndPoint: true,
		AllowSubPeak: true,
	}
}
