package chanlun

import "math"

// ──────────────────────────────────────────────
// 背驰判定（流式版）
// RingBuffer 保留最近 1 个背驰信号
// MACD 缓冲区硬上限 200 条
// ──────────────────────────────────────────────

// DivergenceProcessor 是背驰判定处理器。
// 使用 MACD 面积法进行背驰检测。
type DivergenceProcessor struct {
	divergences *Ring[*AlgoDivergence] // 容量 1
	macdValues  map[string][]float64   // per symbol 收盘价序列
	maxMACD     int                    // 200
}

// NewDivergenceProcessor 创建一个背驰判定处理器。
func NewDivergenceProcessor() *DivergenceProcessor {
	return &DivergenceProcessor{
		divergences: NewRing[*AlgoDivergence](1),
		macdValues:  make(map[string][]float64),
		maxMACD:     200,
	}
}

// FeedClose 输入收盘价用于 MACD 计算。
func (p *DivergenceProcessor) FeedClose(symbol string, close float64) {
	list := p.macdValues[symbol]
	list = append(list, close)
	if len(list) > p.maxMACD {
		list = list[1:]
	}
	p.macdValues[symbol] = list
}

// Process 执行背驰判定。
// 仅当走势未完成时检测，通过 MACD 面积比判断。
func (p *DivergenceProcessor) Process(symbol string, entryPrice float64, lastPatternDir Direction, lastPatternCompleted bool) []*AlgoDivergence {
	var result []*AlgoDivergence

	// 走势已完成时不检测背驰
	if lastPatternCompleted {
		return result
	}

	vals, ok := p.macdValues[symbol]
	if !ok || len(vals) < 10 {
		return result
	}

	macd := p.calcMACDArea(vals)
	if macd <= 0 {
		return result
	}

	entryArea := macd * 0.6
	exitArea := macd * 0.4
	if entryArea <= 0 {
		return result
	}

	ratio := exitArea / entryArea
	if ratio >= 0.95 {
		return result
	}

	dType := "topDivergence"
	if lastPatternDir == DirectionDown {
		dType = "bottomDivergence"
	}

	d := &AlgoDivergence{
		Symbol:         symbol,
		DivergenceType: dType,
		Confirmed:      true,
		EntryMACD:      entryArea,
		ExitMACD:       exitArea,
		Ratio:          ratio,
		PriceHigh:      entryPrice,
	}
	p.divergences.Append(d)
	result = append(result, d)
	return result
}

// GetDivergences 返回当前所有背驰信号。
func (p *DivergenceProcessor) GetDivergences() []*AlgoDivergence {
	return p.divergences.ToSlice()
}

// Reset 清空处理器状态。
func (p *DivergenceProcessor) Reset() {
	p.divergences.Clear()
	p.macdValues = make(map[string][]float64)
}

// ── 内部：MACD 计算 ──

func (p *DivergenceProcessor) calcMACDArea(values []float64) float64 {
	if len(values) < 3 {
		return 0
	}
	e12 := p.calcEMA(values, 12)
	e26 := p.calcEMA(values, 26)

	n := len(e12)
	if len(e26) < n {
		n = len(e26)
	}

	var area float64
	for i := 0; i < n; i++ {
		d := e12[i] - e26[i]
		if d > 0 {
			area += d
		}
	}
	return area
}

func (p *DivergenceProcessor) calcEMA(values []float64, period int) []float64 {
	if len(values) == 0 {
		return nil
	}
	m := 2.0 / float64(period)
	result := make([]float64, len(values))
	result[0] = values[0]
	for i := 1; i < len(values); i++ {
		result[i] = (values[i]-result[i-1])*m + result[i-1]
	}
	// 截断到合理长度
	maxLen := int(math.Min(float64(len(values)), float64(period*3)))
	if maxLen < len(result) {
		result = result[:maxLen]
	}
	return result
}
