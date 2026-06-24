// Package chanlun 实现背驰判定算法（背驰判定算法.md）。
//
// 背驰 = 价格创新高/低的同时，对应的动力（MACD 面积）未能同步。
// 缠论原文（第53课）：比较最后一个中枢的进入段与离开段。
// 进入段 = 走势起点到该中枢起点（不含）的笔序列
// 离开段 = 该中枢终点（不含）到走势终点的笔序列
// 当离开段的累积强度 < 进入段的累积强度，且价格创新高/低时 → 背驰确认。
package chanlun

import (
	"math"
	"sync"

	"trade/internal/types"
)

// divergence 背驰判定结果。
// 记录比较的中枢位置、进入段/离开段的笔索引范围和各自的累积强度。
type divergence struct {
	Type       string  // "topDivergence"(顶背驰) / "bottomDivergence"(底背驰)
	Scope      string  // "trend"(趋势背驰) / "consolidation"(盘整背驰)
	TrendIdx   int     // 所属走势类型索引
	ZoneIdx    int     // 被比较的最后中枢索引
	EntryStart int     // 进入段起始笔索引（含）
	EntryEnd   int     // 进入段结束笔索引（含）
	ExitStart  int     // 离开段起始笔索引（含）
	ExitEnd    int     // 离开段结束笔索引（含）
	EntryMACD  float64 // 进入段累积强度
	ExitMACD   float64 // 离开段累积强度
	EntryPrice float64 // 进入段最高价（顶背驰）或最低价（底背驰）
	ExitPrice  float64 // 离开段最高价（顶背驰）或最低价（底背驰）
	Ratio      float64 // ExitMACD / EntryMACD
	Confirmed  bool    // Ratio < 0.95
}

// divergenceState 背驰判定状态。
type divergenceState struct {
	mu              sync.Mutex
	strokes         []*stroke
	pivotZones      []*pivotZone
		patterns        []*trendPattern
		divergences     []*divergence
		processedStroke int
		processedPattern int

		// MACD 计算状态（第53课标准算法）
		// 每根原始 K 线调用一次 FeedClose，维护 EMA 并记录 MACD 柱值。
		ema12Init bool       // EMA12 是否已初始化
		ema26Init bool       // EMA26 是否已初始化
		ema12     float64    // 当前 EMA12
		ema26     float64    // 当前 EMA26
		dea       float64    // 当前 DEA（9 周期 DIF 的 EMA）
		macdBars  []float64  // 每根原始 K 线的 MACD 柱值，按 KlineIdx 索引
	}

	// emaMultiplier 计算 EMA 平滑系数。
	func emaMultiplier(period int) float64 {
		return 2.0 / float64(period+1)
	}

	// FeedClose 输入一根原始 K 线的收盘价，更新 MACD 状态。
	// 每次 Process 前应先将该 symbol 的所有新增原始 K 线 Close 喂入。
	func (bcp *DivergenceProcessor) FeedClose(symbol string, closePrice float64) {
		st := bcp.getState(symbol)

		// EMA12
		if !st.ema12Init {
			st.ema12 = closePrice
			st.ema12Init = true
		} else {
			st.ema12 = closePrice*emaMultiplier(12) + st.ema12*(1-emaMultiplier(12))
		}

		// EMA26
		if !st.ema26Init {
			st.ema26 = closePrice
			st.ema26Init = true
		} else {
			st.ema26 = closePrice*emaMultiplier(26) + st.ema26*(1-emaMultiplier(26))
		}

		dif := st.ema12 - st.ema26

		// DEA（DIF 的 9 周期 EMA）
		if len(st.macdBars) == 0 {
			st.dea = dif
		} else {
			st.dea = dif*emaMultiplier(9) + st.dea*(1-emaMultiplier(9))
		}

		macdBar := 2 * (dif - st.dea)
		st.macdBars = append(st.macdBars, macdBar)
	}

// DivergenceProcessor 背驰判定处理器。
type DivergenceProcessor struct {
	states map[string]*divergenceState
	mu     sync.Mutex
}

// NewDivergenceProcessor 创建背驰判定处理器。
func NewDivergenceProcessor() *DivergenceProcessor {
	return &DivergenceProcessor{
		states: make(map[string]*divergenceState),
	}
}

// Process 处理笔序列 + 中枢列表 + 走势类型列表，更新背驰信号。
func (bcp *DivergenceProcessor) Process(symbol string, strokes []*stroke, pivotZones []*pivotZone, patterns []*trendPattern) []*divergence {
	st := bcp.getState(symbol)
	st.mu.Lock()
	defer st.mu.Unlock()

	st.strokes = strokes
	st.pivotZones = pivotZones
	st.patterns = patterns

	// 增量处理新的走势类型
	for st.processedPattern < len(st.patterns) {
		p := st.patterns[st.processedPattern]
		st.detectTrendDivergence(p, st.pivotZones, strokes)
		st.processedPattern++
	}

	st.processedStroke = len(strokes)
	return st.divergences
}

// detectTrendDivergence 对一个走势类型检测趋势背驰。
// 取最后一个中枢，比较进入段 vs 离开段的累积强度。
// 仅检测进行中的走势（已完成的走势不重复检测）。
func (st *divergenceState) detectTrendDivergence(p *trendPattern, pivotZones []*pivotZone, strokes []*stroke) {
	if len(p.PivotZoneIDs) == 0 || p.Completed {
		return
	}

	// 取最后一个中枢
	lastZoneID := p.PivotZoneIDs[len(p.PivotZoneIDs)-1]
	if lastZoneID < 0 || lastZoneID >= len(pivotZones) {
		return
	}
	lastZone := pivotZones[lastZoneID]

	// 进入段：走势起点到最后一个中枢起点之间（不含中枢第一笔）
	entryStart := p.StartStrokeIdx
	entryEnd := lastZone.StartStrokeIdx - 1

	// 离开段：最后一个中枢终点之后到走势终点（不含中枢最后一笔）
	exitStart := lastZone.EndStrokeIdx + 1
	exitEnd := p.EndStrokeIdx

	// 任一段为空则不能成段比较
	if entryStart > entryEnd || exitStart > exitEnd {
		return
	}
	if entryEnd >= len(strokes) || exitEnd >= len(strokes) {
		return
	}

	// 计算两段的累积强度和价格极值
	entryMACD, entryHigh, entryLow := st.segmentStats(strokes[entryStart : entryEnd+1])
	exitMACD, exitHigh, exitLow := st.segmentStats(strokes[exitStart : exitEnd+1])

	if entryMACD <= 0 || exitMACD <= 0 {
		return
	}

	// 确认方向匹配
	var bcType string
	var priceHigher bool
	if p.Direction == types.DirectionUp {
		// 顶背驰：离开段最高价 > 进入段最高价（创新高），且力度减弱
		bcType = "topDivergence"
		priceHigher = exitHigh > entryHigh
	} else {
		// 底背驰：离开段最低价 < 进入段最低价（创新低），且力度减弱
		bcType = "bottomDivergence"
		priceHigher = exitLow < entryLow
	}
	if !priceHigher {
		return
	}

	// 确定比较用的价格值
	var entryPrice, exitPrice float64
	if p.Direction == types.DirectionUp {
		entryPrice = entryHigh
		exitPrice = exitHigh
	} else {
		entryPrice = entryLow
		exitPrice = exitLow
	}

	ratio := exitMACD / entryMACD
	confirmed := ratio < 0.95

	scope := "consolidation"
	if p.Type == "trend" {
		scope = "trend"
	}

	bc := &divergence{
		Type:       bcType,
		Scope:      scope,
		TrendIdx:   p.Index,
		ZoneIdx:    lastZoneID,
		EntryStart: entryStart,
		EntryEnd:   entryEnd,
		ExitStart:  exitStart,
		ExitEnd:    exitEnd,
		EntryMACD:  entryMACD,
		ExitMACD:   exitMACD,
		EntryPrice: entryPrice,
		ExitPrice:  exitPrice,
		Ratio:      ratio,
		Confirmed:  confirmed,
	}
	st.divergences = append(st.divergences, bc)
}

// segmentStats 计算一段笔序列的累积 MACD 面积（从 macdBars 按 KlineIdx 索引）和价格极值。
func (st *divergenceState) segmentStats(strokes []*stroke) (macd float64, high float64, low float64) {
	if len(strokes) == 0 {
		return 0, 0, 0
	}
	high = strokes[0].High
	low = strokes[0].Low
	for _, s := range strokes {
		macd += st.strokeMACD(s)
		if s.High > high {
			high = s.High
		}
		if s.Low < low {
			low = s.Low
		}
	}
	return macd, high, low
}

// strokeMACD 累加一根笔所包含的所有原始 K 线的 MACD 柱值。
// 通过笔的 ChanKline 链表遍历，按 KlineIdx + MergedFrom 索引 macdBars。
func (st *divergenceState) strokeMACD(s *stroke) float64 {
	total := 0.0
	if s.Start == nil || len(st.macdBars) == 0 {
		// 无 MACD 数据时回退到 strokeStrength 近似
		return strokeStrength(s)
	}
	curr := s.Start
	for curr != nil {
		idx := curr.KlineIdx
		for i := 0; i < curr.MergedFrom; i++ {
			if idx+i >= 0 && idx+i < len(st.macdBars) {
				total += st.macdBars[idx+i]
			}
		}
		if curr == s.End {
			break
		}
		curr = curr.NextElement
	}
	return total
}

// ReprocessFrom 从指定笔索引开始重算背驰（PRD §10.4.3 回溯修正）。
func (bcp *DivergenceProcessor) ReprocessFrom(symbol string, fromIdx int) {
	st := bcp.getState(symbol)
	st.mu.Lock()
	defer st.mu.Unlock()

	var kept []*divergence
	for _, d := range st.divergences {
		if d.ExitEnd < fromIdx {
			kept = append(kept, d)
		}
	}
	st.divergences = kept

	resetIdx := 0
	if fromIdx > 2 {
		resetIdx = fromIdx - 2
	}
	st.processedStroke = resetIdx
	st.processedPattern = 0
}

// Load 返回所有背驰信号。
func (bcp *DivergenceProcessor) Load(symbol string) []*divergence {
	st := bcp.getState(symbol)
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.divergences
}

// Reset 重置。
func (bcp *DivergenceProcessor) Reset(symbol string) {
	bcp.mu.Lock()
	defer bcp.mu.Unlock()
	delete(bcp.states, symbol)
}

// getState 获取或创建指定 symbol 的背驰判定状态。
func (bcp *DivergenceProcessor) getState(symbol string) *divergenceState {
	bcp.mu.Lock()
	defer bcp.mu.Unlock()
	if s, ok := bcp.states[symbol]; ok {
		return s
	}
	s := &divergenceState{}
	bcp.states[symbol] = s
	return s
}

// strokeStrength 计算一根笔的 MACD 能量强度。
// 优先遍历笔内的 ChanKline 链表，累加相邻 K 线收盘价差的绝对值 × 成交量，
// 近似于缠论中 MACD 面积（∑|Closeᵢ - Closeᵢ₋₁| × Volumeᵢ）。
// 当无 ChanKline 数据时（如测试场景），回退到 |EndPrice - StartPrice| 近似。
func strokeStrength(s *stroke) float64 {
	total := 0.0
	if s.Start == nil || s.Start.NextElement == nil {
		// 笔内无独立 K 线或数据不足时，用 Start/End 价格差近似
		return math.Abs(s.EndPrice - s.StartPrice)
	}
	prevClose := s.Start.Close
	curr := s.Start.NextElement
	for curr != nil {
		priceMove := math.Abs(curr.Close - prevClose)
		total += priceMove * curr.Volume
		if curr == s.End {
			break
		}
		prevClose = curr.Close
		curr = curr.NextElement
	}
	// 当 Volume 全为 0（无成交量数据），回退到价格区间近似
	if total == 0 {
		return math.Abs(s.EndPrice - s.StartPrice)
	}
	return total
}

// divergenceToEvidence 转为输出结构。
func divergenceToEvidence(bc *divergence) map[string]interface{} {
		return map[string]interface{}{
			"type":      bc.Type,
			"scope":     bc.Scope,
			"price1":    bc.EntryMACD,
			"price2":    bc.ExitMACD,
			"ratio":     bc.Ratio,
			"confirmed": bc.Confirmed,
		}
}
