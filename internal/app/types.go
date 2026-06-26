// Package app 负责系统组装与生命周期管理。
package app

import (
	"context"

	"github.com/redis/go-redis/v9"
)

// Direction 是 K 线方向类型。
type Direction int

// K 线方向常量。
const (
	DirectionUp   = Direction(1)  // 向上
	DirectionDown = Direction(-1) // 向下
)

// Fractal 是分型类型。
type Fractal int

// 分型类型常量。
const (
	FractalNone   = Fractal(0)  // 非分型
	FractalTop    = Fractal(1)  // 顶分型
	FractalBottom = Fractal(-1) // 底分型
)

// KLine 表示一根原始 K 线数据。
type KLine struct {
	// Symbol 是交易对名称，如 BTCUSDT。
	Symbol string `json:"symbol"`
	// Open 是开盘价。
	Open float64 `json:"open"`
	// High 是最高价。
	High float64 `json:"high"`
	// Low 是最低价。
	Low float64 `json:"low"`
	// Close 是收盘价。
	Close float64 `json:"close"`
	// Volume 是成交量。
	Volume float64 `json:"volume"`
	// Timestamp 是 K 线时间戳（毫秒）。
	Timestamp int64 `json:"ts"`
}

// ChanKLine 表示缠论 K 线（包含处理后的合并 K 线）。
type ChanKLine struct {
	// High 是合并后的最高价。
	High float64 `json:"high"`
	// Low 是合并后的最低价。
	Low float64 `json:"low"`
	// Direction 是方向，取值为 DirectionUp 或 DirectionDown。
	Direction Direction `json:"direction"`
	// Fractal 是分型类型，取值为 FractalNone / FractalTop / FractalBottom。
	Fractal Fractal `json:"fractal"`
	// Timestamp 是该 K 线对应的最后一根原始 K 线时间戳（毫秒）。
	Timestamp int64 `json:"ts"`
}

// ChanKLineSequence 是缠论 K 线序列，负责包含处理与持久化。
// 内部含两个环形缓冲区，职责隔离：
//   - mergedRing (容量 1)：保留最后一根合并 K 线，用于包含判定与持久化触发
//   - nonIncRing (容量 2)：保留最后两根非包含 K 线，用于方向更新
type ChanKLineSequence struct {
	mergedRing *Ring[*ChanKLine] // 合并 K 线序列（结果序列）
	nonIncRing *Ring[*ChanKLine] // 非包含 K 线序列（方向判定用）
	store      *ChanKLineStore   // Redis 持久化
	fractal    *FractalDetector  // 分型识别器
}

// NewChanKLineSequence 创建一个新的缠论 K 线序列。
func NewChanKLineSequence(rdb *redis.Client, symbol string) *ChanKLineSequence {
	return &ChanKLineSequence{
		mergedRing: NewRing[*ChanKLine](1), // 仅需保留结果序列中最后一根
		nonIncRing: NewRing[*ChanKLine](2), // 仅需非包含序列中最后两个元素
		store:      NewChanKLineStore(rdb, symbol),
		fractal:    NewFractalDetector(),
	}
}

// Last 返回最后一根合并 K 线，供包含判定使用。
func (s *ChanKLineSequence) Last() *ChanKLine {
	v, ok := s.mergedRing.Last()
	if !ok {
		return nil
	}
	return v
}

// Len 返回已产生的合并 K 线总数。
func (s *ChanKLineSequence) Len() int {
	return s.mergedRing.Len()
}

// ProcessInclusion 执行包含处理算法，将原始 K 线加入序列。
// 根据缠论原文，处理 K 线之间的包含关系。
// 当合并 K 线移出内存窗口时自动持久化到 Redis。
func (s *ChanKLineSequence) ProcessInclusion(ctx context.Context, kline *KLine) {
	// 如果没有缠论 K 线，直接添加第一根
	last, ok := s.mergedRing.Last()
	if !ok {
		chanLine := &ChanKLine{
			High:      kline.High,
			Low:       kline.Low,
			Direction: DirectionUp, // 初始方向向上
			Timestamp: kline.Timestamp,
		}
		s.mergedRing.Append(chanLine)
		s.nonIncRing.Append(chanLine)
		s.fractal.Feed(chanLine)
		return
	}

	// 判断包含关系
	if isInclusion(last, kline) {
		// 存在包含关系，进行合并（原地更新，不触发覆盖）
		mergeChanKLine(last, kline)
	} else {
		// 不存在包含关系，产生新缠论 K 线
		// 同时加入 mergedRing（用于包含判定）和 nonIncRing（用于方向更新）
		chanLine := &ChanKLine{
			High:      kline.High,
			Low:       kline.Low,
			Direction: last.Direction, // 暂用上一根方向，随后更新
			Timestamp: kline.Timestamp,
		}

		// mergedRing 满时 old 被覆盖 → 持久化
		if evicted, ok := s.mergedRing.Append(chanLine); ok {
			s.persistEvicted(ctx, evicted)
		}

		// nonIncRing 满时 old 直接丢弃（不需要持久化，已在 mergedRing 被覆盖时持久化过）
		s.nonIncRing.Append(chanLine)

		// 从非包含序列更新方向
		s.updateDirection()

		// 送入分型识别器
		s.fractal.Feed(chanLine)
	}
}

// persistEvicted 将被覆盖的合并 K 线持久化到 Redis。
func (s *ChanKLineSequence) persistEvicted(ctx context.Context, evicted *ChanKLine) {
	lines := []*ChanKLine{evicted}
	if err := s.store.Save(ctx, lines); err != nil {
		// 持久化失败不影响主流程，下次被覆盖时重试
		return
	}
}

// isInclusion 判断结果序列最后一根合并 K 线与原始 K 线是否存在包含关系。
// 包含关系：当前高 <= 前一根高 且 当前低 >= 前一根低，或者
// 当前高 >= 前一根高 且 当前低 <= 前一根低
func isInclusion(chanLine *ChanKLine, kline *KLine) bool {
	return (kline.High <= chanLine.High && kline.Low >= chanLine.Low) ||
		(kline.High >= chanLine.High && kline.Low <= chanLine.Low)
}

// mergeChanKLine 合并原始 K 线到合并 K 线。
// 根据方向决定合并方式：
// - 向上：取两边高点中的更高者、低点中的更高者
// - 向下：取两边高点中的更低者、低点中的更低者
func mergeChanKLine(chanLine *ChanKLine, kline *KLine) {
	if chanLine.Direction == DirectionUp {
		// 向上：取高高，低高
		chanLine.High = max(chanLine.High, kline.High)
		chanLine.Low = max(chanLine.Low, kline.Low)
	} else {
		// 向下：取低高，低低
		chanLine.High = min(chanLine.High, kline.High)
		chanLine.Low = min(chanLine.Low, kline.Low)
	}
	// 更新时间戳为最后一根原始 K 线的时间戳
	chanLine.Timestamp = kline.Timestamp
}

// updateDirection 从非包含序列更新当前方向。
// 以非包含序列中最后两个元素为基准判定方向：
// - 若第二根高点 > 第一根高点 且 第二根低点 > 第一根低点 → 方向更新为向上
// - 若第二根高点 < 第一根高点 且 第二根低点 < 第一根低点 → 方向更新为向下
// - 否则方向保持不变
func (s *ChanKLineSequence) updateDirection() {
	if s.nonIncRing.Len() < 2 {
		return
	}

	current, _ := s.nonIncRing.At(s.nonIncRing.Len() - 1)
	previous, _ := s.nonIncRing.At(s.nonIncRing.Len() - 2)

	if current.High > previous.High && current.Low > previous.Low {
		current.Direction = DirectionUp
	} else if current.High < previous.High && current.Low < previous.Low {
		current.Direction = DirectionDown
	} else {
		current.Direction = previous.Direction // 保持前一个方向
	}
}
