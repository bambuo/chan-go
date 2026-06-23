// Package chanlun 实现走势类型分类算法（走势类型分类算法.md）。
//
// 走势类型 = 基于中枢序列的分类结果：
//   - 趋势（trend）：≥2 个互不重叠的同向中枢
//   - 盘整（consolidation）：1 个中枢
//   - 不明朗：0 个中枢
package chanlun

import (
	"sync"

	"trade/internal/types"
)

// trendPattern 走势类型内部类型。
type trendPattern struct {
	Index          int                 // 走势序号
	Type           string              // "trend"(趋势) / "consolidation"(盘整)
	Direction      types.ChanDirection // 方向
	PivotZoneIDs   []int               // 包含的中枢索引列表
	StartStrokeIdx int                 // 起始笔索引
	EndStrokeIdx   int                 // 结束笔索引
	StartPrice     float64             // 起点价格
	EndPrice       float64             // 终点价格
	High           float64             // 区间最高
	Low            float64             // 区间最低
	Completed      bool                // 是否已结束
}

// trendPatternState 走势类型状态。
type trendPatternState struct {
	mu           sync.Mutex
	strokes      []*stroke
	pivotZones   []*pivotZone
	patterns     []*trendPattern
	processedIdx int
}

// TrendPatternProcessor 走势类型分类处理器。
type TrendPatternProcessor struct {
	states map[string]*trendPatternState
	mu     sync.Mutex
}

// NewTrendPatternProcessor 创建走势分类处理器。
func NewTrendPatternProcessor() *TrendPatternProcessor {
	return &TrendPatternProcessor{
		states: make(map[string]*trendPatternState),
	}
}

// Process 增量处理中枢列表，更新走势类型分类。
func (zp *TrendPatternProcessor) Process(symbol string, strokes []*stroke, pivotZones []*pivotZone) []*trendPattern {
	st := zp.getState(symbol)
	st.mu.Lock()
	defer st.mu.Unlock()

	st.strokes = strokes
	st.pivotZones = pivotZones

	if len(pivotZones) <= st.processedIdx {
		return st.patterns
	}

	st.classify()
	st.processedIdx = len(pivotZones)

	return st.patterns
}

// Load 返回所有走势类型。
func (zp *TrendPatternProcessor) Load(symbol string) []*trendPattern {
	st := zp.getState(symbol)
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.patterns
}

// Reset 重置。
func (zp *TrendPatternProcessor) Reset(symbol string) {
	zp.mu.Lock()
	defer zp.mu.Unlock()
	delete(zp.states, symbol)
}

// getState 获取或创建指定 symbol 的走势分类状态。
func (zp *TrendPatternProcessor) getState(symbol string) *trendPatternState {
	zp.mu.Lock()
	defer zp.mu.Unlock()
	if s, ok := zp.states[symbol]; ok {
		return s
	}
	s := &trendPatternState{}
	zp.states[symbol] = s
	return s
}

// classify 基于当前中枢列表执行走势类型分类。
// 按顺序分组：一组互不重叠的同向中枢构成一个走势类型。
// 每当中枢方向变化或重叠时，结束当前走势、开始新的走势。
func (st *trendPatternState) classify() {
	st.patterns = nil
	if len(st.pivotZones) == 0 {
		return
	}

	currentGroup := []*pivotZone{st.pivotZones[0]}

	for i := 1; i < len(st.pivotZones); i++ {
		prev := st.pivotZones[i-1]
		curr := st.pivotZones[i]

		if pivotZonesOverlap(prev, curr) || curr.Direction != prev.Direction {
			st.addPattern(currentGroup)
			currentGroup = []*pivotZone{curr}
		} else {
			currentGroup = append(currentGroup, curr)
		}
	}
	st.addPattern(currentGroup)
}

// addPattern 从一组同向非重叠中枢创建一个走势类型。
func (st *trendPatternState) addPattern(group []*pivotZone) {
	if len(group) == 0 {
		return
	}

	typeStr := "consolidation"
	if len(group) >= 2 {
		typeStr = "trend"
	}

	dir := group[0].Direction

	startStrokeIdx := group[0].StartStrokeIdx
	endStrokeIdx := group[len(group)-1].EndStrokeIdx
	startPrice := st.strokes[startStrokeIdx].StartPrice
	endPrice := st.strokes[endStrokeIdx].EndPrice
	high := st.strokes[startStrokeIdx].High
	low := st.strokes[startStrokeIdx].Low
	for _, zs := range group {
		if zs.ZG > high {
			high = zs.ZG
		}
		if zs.ZD < low {
			low = zs.ZD
		}
	}

	ids := make([]int, len(group))
	for i, zs := range group {
		ids[i] = zs.index
	}

	p := &trendPattern{
		Index:          len(st.patterns),
		Type:           typeStr,
		Direction:      dir,
		PivotZoneIDs:   ids,
		StartStrokeIdx: startStrokeIdx,
		EndStrokeIdx:   endStrokeIdx,
		StartPrice:     startPrice,
		EndPrice:       endPrice,
		High:           high,
		Low:            low,
		Completed:      true,
	}
	st.patterns = append(st.patterns, p)
}

// pivotZonesOverlap 检查两个中枢的完整波动区间是否有重叠。
// 条件：a.ZG > b.ZD && a.ZD < b.ZG 表示区间有交集。
func pivotZonesOverlap(a, b *pivotZone) bool {
	if a == nil || b == nil {
		return false
	}
	return a.ZG > b.ZD && a.ZD < b.ZG
}

// trendPatternToTypes 转为 types.TrendPattern。
func trendPatternToTypes(zs *trendPattern) types.TrendPattern {
	return types.TrendPattern{
		Direction: zs.Direction,
		Type:      zs.Type,
		Completed: zs.Completed,
		StartPrice: zs.StartPrice,
		EndPrice:   zs.EndPrice,
		High:       zs.High,
		Low:        zs.Low,
	}
}

// TrendPatternsToTypes 批量转换。
func TrendPatternsToTypes(zss []*trendPattern) []types.TrendPattern {
	result := make([]types.TrendPattern, len(zss))
	for i, zs := range zss {
		result[i] = trendPatternToTypes(zs)
	}
	return result
}
