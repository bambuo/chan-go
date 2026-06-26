// Package app 负责系统组装与生命周期管理。
package app

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
	// Direction 是方向：1 表示向上，-1 表示向下。
	Direction int `json:"direction"`
	// Fractal 是分型类型：1 表示顶分型，-1 表示底分型，0 表示未知。
	Fractal int `json:"fractal"`
	// Index 是在序列中的索引。
	Index int `json:"index"`
}

// ChanKLineSequence 是缠论 K 线序列，负责管理包含处理。
// 内部使用环形缓冲区，固定容量，新数据自动覆盖最旧数据。
type ChanKLineSequence struct {
	ring *Ring[*ChanKLine]
}

// NewChanKLineSequence 创建一个新的缠论 K 线序列。
func NewChanKLineSequence() *ChanKLineSequence {
	return &ChanKLineSequence{
		ring: NewRing[*ChanKLine](1000), // 默认容量 1000
	}
}

// Len 返回序列长度。
func (s *ChanKLineSequence) Len() int {
	return s.ring.Len()
}

// Last 返回最后一根缠论 K 线，序列为空时返回 nil。
func (s *ChanKLineSequence) Last() *ChanKLine {
	v, ok := s.ring.Last()
	if !ok {
		return nil
	}
	return v
}

// ProcessInclusion 执行包含处理算法，将原始 K 线加入序列。
// 根据缠论原文，处理 K 线之间的包含关系。
func (s *ChanKLineSequence) ProcessInclusion(kline *KLine) {
	// 如果没有缠论 K 线，直接添加第一根
	last, ok := s.ring.Last()
	if !ok {
		chanLine := &ChanKLine{
			High:      kline.High,
			Low:       kline.Low,
			Direction: 1, // 初始方向向上
			Index:     0,
		}
		s.ring.Append(chanLine)
		return
	}

	// 判断包含关系
	if isInclusion(last, kline) {
		// 存在包含关系，进行合并
		mergeChanKLine(last, kline)
	} else {
		// 不存在包含关系，直接添加
		chanLine := &ChanKLine{
			High:      kline.High,
			Low:       kline.Low,
			Direction: last.Direction, // 保持当前方向
			Index:     s.ring.Len(),
		}
		s.ring.Append(chanLine)

		// 更新方向
		s.updateDirection()
	}
}

// isInclusion 判断缠论 K 线与原始 K 线是否存在包含关系。
// 包含关系：当前高 <= 前一根高 且 当前低 >= 前一根低，或者
// 当前高 >= 前一根高 且 当前低 <= 前一根低
func isInclusion(chanLine *ChanKLine, kline *KLine) bool {
	return (kline.High <= chanLine.High && kline.Low >= chanLine.Low) ||
		(kline.High >= chanLine.High && kline.Low <= chanLine.Low)
}

// mergeChanKLine 合并 K 线到缠论 K 线。
// 根据方向决定合并方式：
// - 向上：取两边高点中的更高者、低点中的更高者
// - 向下：取两边高点中的更低者、低点中的更低者
func mergeChanKLine(chanLine *ChanKLine, kline *KLine) {
	if chanLine.Direction == 1 {
		// 向上：取高高，低高
		chanLine.High = max(chanLine.High, kline.High)
		chanLine.Low = max(chanLine.Low, kline.Low)
	} else {
		// 向下：取低高，低低
		chanLine.High = min(chanLine.High, kline.High)
		chanLine.Low = min(chanLine.Low, kline.Low)
	}
}

// updateDirection 更新方向。
// 以非包含序列中最后两个元素为基准判定方向：
// - 若第二根高点 > 第一根高点 且 第二根低点 > 第一根低点 → 方向更新为向上
// - 若第二根高点 < 第一根高点 且 第二根低点 < 第一根低点 → 方向更新为向下
// - 否则方向保持不变
func (s *ChanKLineSequence) updateDirection() {
	if s.ring.Len() < 2 {
		return
	}

	current, _ := s.ring.At(s.ring.Len() - 1)
	previous, _ := s.ring.At(s.ring.Len() - 2)

	if current.High > previous.High && current.Low > previous.Low {
		current.Direction = 1 // 向上
	} else if current.High < previous.High && current.Low < previous.Low {
		current.Direction = -1 // 向下
	} else {
		current.Direction = previous.Direction // 保持前一个方向
	}
}
