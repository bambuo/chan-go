// Package types — 本文件：中枢模式枚举（中枢识别算法.md）
package types

// PivotZoneMode 中枢构建模式。
type PivotZoneMode int

const (
	PivotModeStroke  PivotZoneMode = iota // 笔中枢（默认）：从笔序列构建中枢
	PivotModeSegment                       // 线段中枢：从线段序列构建中枢
)
