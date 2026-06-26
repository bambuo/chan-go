// Package chanlun 提供缠论（Chan Theory）算法的核心数据结构与处理器。
//
// 包含：
//   - K 线、缠论 K 线、分型、笔、线段、中枢、走势类型等基础类型
//   - 包含处理（ContainProcessor）
//   - 分型识别（FractalProcessor）
//   - 笔识别（StrokeProcessor）
//   - 线段划分（SegmentProcessor）
//   - 中枢识别（PivotZoneProcessor）
//   - 走势类型分类（TrendPatternProcessor）
//   - 背驰判定（DivergenceProcessor）
//   - 9 步流式管线（Pipeline）
package chanlun

// Direction 表示方向。
type Direction int

const (
	DirectionUp   Direction = 1
	DirectionDown Direction = -1
)

// Opposite 返回相反方向。
func (d Direction) Opposite() Direction {
	if d == DirectionUp {
		return DirectionDown
	}
	return DirectionUp
}

// FractalType 表示分型类型。
type FractalType int

const (
	FractalTypeNone   FractalType = 0
	FractalTypeTop    FractalType = 1
	FractalTypeBottom FractalType = -1
)

// Opposite 返回相反分型类型。
func (f FractalType) Opposite() FractalType {
	if f == FractalTypeTop {
		return FractalTypeBottom
	} else if f == FractalTypeBottom {
		return FractalTypeTop
	}
	return FractalTypeNone
}

// PivotZoneMode 表示中枢构建模式。
type PivotZoneMode int

const (
	PivotZoneModeStroke  PivotZoneMode = iota // 基于笔构建中枢
	PivotZoneModeSegment                      // 基于线段构建中枢
)

// KLine 表示一根原始 K 线数据。
type KLine struct {
	Symbol    string
	OpenTime  int64
	CloseTime int64
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	IsClosed  bool
}

// NewTrendKLine 从走势完成结果构造一条趋势 K 线，用于级别升级。
func NewTrendKLine(time int64, symbol string, high, low float64) *KLine {
	return &KLine{
		Symbol:    symbol,
		OpenTime:  time,
		CloseTime: time,
		Open:      high,
		High:      high,
		Low:       low,
		Close:     low,
		Volume:    0,
		IsClosed:  true,
	}
}

// ChanKLine 表示缠论 K 线（包含处理后的合并 K 线）。
//
// Go 不支持运算符重载，故使用方法替代：
//
//	IsAbove(other)  — a > b 方向判定：高点更高且低点更高
//	IsBelow(other)  — a < b 方向判定：高点更低且低点更低
//	Contains(other) — a >= b 包含关系
//	ContainedBy(other) — a <= b 被包含关系
//	MergeUp(other)  — a + b 向上合并
//	MergeDown(other) — a - b 向下合并
type ChanKLine struct {
	Time      int64
	High      float64
	Low       float64
	Direction Direction
	Index     int64
}

// IsAbove 判定当前 K 线是否在 other 之上（AND 条件：高点更高且低点更高）。
func (c *ChanKLine) IsAbove(other *ChanKLine) bool {
	return c.High > other.High && c.Low > other.Low
}

// IsBelow 判定当前 K 线是否在 other 之下（AND 条件：高点更低且低点更低）。
func (c *ChanKLine) IsBelow(other *ChanKLine) bool {
	return c.High < other.High && c.Low < other.Low
}

// Contains 判定当前 K 线是否包含 other（高点 >= other 高点 且 低点 <= other 低点）。
func (c *ChanKLine) Contains(other *ChanKLine) bool {
	return c.High >= other.High && c.Low <= other.Low
}

// ContainedBy 判定当前 K 线是否被 other 包含。
func (c *ChanKLine) ContainedBy(other *ChanKLine) bool {
	return c.High <= other.High && c.Low >= other.Low
}

// MergeUp 向上合并：取两者高点中的更高者、低点中的更高者。
func (c *ChanKLine) MergeUp(other *ChanKLine) {
	if other.High > c.High {
		c.High = other.High
	}
	if other.Low > c.Low {
		c.Low = other.Low
	}
}

// MergeDown 向下合并：取两者高点中的更低者、低点中的更低者。
func (c *ChanKLine) MergeDown(other *ChanKLine) {
	if other.High < c.High {
		c.High = other.High
	}
	if other.Low < c.Low {
		c.Low = other.Low
	}
}

// Clone 返回一个深度副本。
func (c *ChanKLine) Clone() *ChanKLine {
	return &ChanKLine{
		Time:      c.Time,
		High:      c.High,
		Low:       c.Low,
		Direction: c.Direction,
		Index:     c.Index,
	}
}

// ──────────────────────────────────────────────
// 分型
// ──────────────────────────────────────────────

// Fractal 表示一个已确认的分型。
type Fractal struct {
	FType FractalType
	KLine *ChanKLine
	Index int64
}

// ──────────────────────────────────────────────
// 笔（算法内部表示）
// ──────────────────────────────────────────────

// AlgoStroke 表示一条笔（算法内部使用）。
type AlgoStroke struct {
	Start, End           *ChanKLine
	StartIndex, EndIndex int64
	Direction            Direction
	IsVirtual            bool
	StartPrice, EndPrice float64
	High, Low            float64
}

// ──────────────────────────────────────────────
// 线段（算法内部表示）
// ──────────────────────────────────────────────

// AlgoSegment 表示一条线段（算法内部使用）。
type AlgoSegment struct {
	Direction            Direction
	Strokes              []*AlgoStroke
	StartIndex, EndIndex int64
	StartTime, EndTime   int64
	StartPrice, EndPrice float64
	High, Low            float64
}

// ──────────────────────────────────────────────
// 中枢（算法内部表示）
// ──────────────────────────────────────────────

// AlgoPivotZone 表示一个中枢（算法内部使用）。
type AlgoPivotZone struct {
	ZG, ZD               float64
	StartIndex, EndIndex int64
	Direction            Direction
	Completed            bool
	StartTime, EndTime   int64
	StartPrice, EndPrice float64
}

// ──────────────────────────────────────────────
// 走势类型（算法内部表示）
// ──────────────────────────────────────────────

// AlgoTrendPattern 表示一个走势类型（算法内部使用）。
type AlgoTrendPattern struct {
	Direction        Direction
	IsTrend          bool
	Zones            []*AlgoPivotZone
	Completed        bool
	CompletionReason string
}

// ──────────────────────────────────────────────
// 背驰（算法内部表示）
// ──────────────────────────────────────────────

// AlgoDivergence 表示一个背驰信号。
type AlgoDivergence struct {
	Symbol         string
	DivergenceType string
	Confirmed      bool
	EntryMACD      float64
	ExitMACD       float64
	Ratio          float64
	PriceHigh      float64
}

// ──────────────────────────────────────────────
// Pipeline 处理结果
// ──────────────────────────────────────────────

// ProcessResult 是 Pipeline 处理结果，仅含级别升级所需信息。
// 各节点的产出已通过 OutputPipe 即时流入 Redis。
type ProcessResult struct {
	Symbol            string
	Time              int64
	HasCompletedTrend bool
	TrendDirection    Direction
	TrendHigh         float64
	TrendLow          float64
	HasChange         bool
}
