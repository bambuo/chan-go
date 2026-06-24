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
	StartStrokeIdx int                 // 起始构件索引
	EndStrokeIdx   int                 // 结束构件索引
	StartPrice     float64             // 起点价格
	EndPrice       float64             // 终点价格
	High           float64             // 区间最高
	Low            float64             // 区间最低
	Completed      bool                // 是否已结束
	EndReason      string              // "divergence"(背驰) / "reverseBreak"(反向破坏)
}

// TrendPatternConfig 走势类型分类配置。
type TrendPatternConfig struct {
	Mode types.PivotZoneMode // 笔中枢或线段中枢
}

// trendPatternState 走势类型状态。
type trendPatternState struct {
	mu           sync.Mutex
	strokes      []*stroke    // 笔列表（笔中枢模式）
	segments     []*segment   // 线段列表（线段中枢模式）
	pivotZones   []*pivotZone
	patterns     []*trendPattern
	processedIdx int
	mode         types.PivotZoneMode // 当前模式
}

// TrendPatternProcessor 走势类型分类处理器。
type TrendPatternProcessor struct {
	states map[string]*trendPatternState
	config TrendPatternConfig
	mu     sync.Mutex
}

// NewTrendPatternProcessor 创建走势分类处理器。
func NewTrendPatternProcessor(config ...TrendPatternConfig) *TrendPatternProcessor {
	mode := types.PivotModeStroke
	if len(config) > 0 {
		mode = config[0].Mode
	}
	return &TrendPatternProcessor{
		states: make(map[string]*trendPatternState),
		config: TrendPatternConfig{Mode: mode},
	}
}

// Process 增量处理中枢列表（笔中枢模式），更新走势类型分类。
func (zp *TrendPatternProcessor) Process(symbol string, strokes []*stroke, pivotZones []*pivotZone) []*trendPattern {
	st := zp.getState(symbol)
	st.mu.Lock()
	defer st.mu.Unlock()

	st.strokes = strokes
	st.pivotZones = pivotZones
	st.mode = types.PivotModeStroke

	if len(pivotZones) <= st.processedIdx {
		return st.patterns
	}

	st.classify()
	st.processedIdx = len(pivotZones)

	return st.patterns
}

// ProcessSegments 增量处理中枢列表（线段中枢模式），更新走势类型分类。
func (zp *TrendPatternProcessor) ProcessSegments(symbol string, segments []*segment, pivotZones []*pivotZone) []*trendPattern {
	st := zp.getState(symbol)
	st.mu.Lock()
	defer st.mu.Unlock()

	st.segments = segments
	st.pivotZones = pivotZones
	st.mode = types.PivotModeSegment

	if len(pivotZones) <= st.processedIdx {
		return st.patterns
	}

	st.classify()
	st.processedIdx = len(pivotZones)

	return st.patterns
}

// MarkLastCompleted 标记最后一个走势类型为已完成（由背驰或外部信号触发）。
//
// PRD §6.1 R1: 走势结束判定 = 背驰或反向破坏，先发生者。
// 当背驰在当前走势的最后一段上被检测到时，调用此方法。
// EndReason: "divergence" 或 "reverseBreak"。
func (zp *TrendPatternProcessor) MarkLastCompleted(symbol string, endReason string) {
	st := zp.getState(symbol)
	st.mu.Lock()
	defer st.mu.Unlock()

	if len(st.patterns) == 0 {
		return
	}
	last := st.patterns[len(st.patterns)-1]
	if last.Completed {
		return // 已完成，不重复标记
	}
	last.Completed = true
	last.EndReason = endReason
}

// ReprocessFrom 从指定笔索引开始重算走势类型（PRD §10.4.3 回溯修正）。
// 走势类型完全从中枢重建，所以只需重置 processedIdx。
func (zp *TrendPatternProcessor) ReprocessFrom(symbol string, fromIdx int) {
	st := zp.getState(symbol)
	st.mu.Lock()
	defer st.mu.Unlock()

	// 删除从 fromIdx 之后的走势类型
	var kept []*trendPattern
	for _, tp := range st.patterns {
		if tp.EndStrokeIdx < fromIdx {
			kept = append(kept, tp)
		}
	}
	st.patterns = kept

	// 重置已处理中枢索引
	resetIdx := 0
	for _, tp := range kept {
		for _, pzID := range tp.PivotZoneIDs {
			if pzID+1 > resetIdx {
				resetIdx = pzID + 1
			}
		}
	}
	st.processedIdx = resetIdx
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

// classify 基于当前中枢列表执行走势类型分类（PRD §6.1 + 走势类型分类算法.md）。
//
// 走势结束判定（PRD R1）：背驰出现 或 被反向走势破坏，二者先发生者。
// 反向破坏 = 次级别出现一个完整的反向走势类型（≥1 反向中枢 + 有效突破）。
//
// 分组规则：
//   - 一组互不重叠的同向中枢构成一个走势类型。
//   - 每当中枢方向变化或重叠时，结束当前走势、开始新的走势。
//   - 当前分组（最后一组）暂不标记 completed，等待后续确认或反向破坏。
func (st *trendPatternState) classify() {
	if len(st.pivotZones) == 0 {
		st.patterns = nil
		return
	}

	// 全量重算：从已有中枢重建分组
	type group struct {
		zones []*pivotZone
		dir   types.ChanDirection
	}
	var groups []group

	current := group{zones: []*pivotZone{st.pivotZones[0]}, dir: st.pivotZones[0].Direction}

	for i := 1; i < len(st.pivotZones); i++ {
		prev := st.pivotZones[i-1]
		curr := st.pivotZones[i]

		if pivotZonesOverlap(prev, curr) || curr.Direction != prev.Direction {
			// 方向变化或重叠 = 当前走势结束，反向走势开始
			groups = append(groups, current)
			current = group{zones: []*pivotZone{curr}, dir: curr.Direction}
		} else {
			current.zones = append(current.zones, curr)
		}
	}
	// 最后一组（当前正在进行的走势）暂不标记完成
	groups = append(groups, current)

	// 转换为 trendPattern，标记完成状态
	st.patterns = nil
	for gi, g := range groups {
		isLast := gi == len(groups)-1
		p := st.buildPattern(g.zones)
		if !isLast {
			// 非最后一组：走势已被后续反向走势破坏 → 标记为 completed
			p.Completed = true
			p.EndReason = "reverseBreak"
		} else {
			// 最后一组：当前走势，暂不标记完成
			p.Completed = false
			p.EndReason = ""
		}
		st.patterns = append(st.patterns, p)
	}
}

// buildPattern 从中枢组创建一个走势类型。
func (st *trendPatternState) buildPattern(group []*pivotZone) *trendPattern {
	if len(group) == 0 {
		return nil
	}

	typeStr := "consolidation"
	if len(group) >= 2 {
		typeStr = "trend"
	}

	dir := group[0].Direction

	startStrokeIdx := group[0].StartStrokeIdx
	endStrokeIdx := group[len(group)-1].EndStrokeIdx

	// 根据当前模式从笔或段查询价格
	var startPrice, endPrice, high, low float64
	if st.mode == types.PivotModeStroke {
		startPrice = st.strokes[startStrokeIdx].StartPrice
		endPrice = st.strokes[endStrokeIdx].EndPrice
		high = st.strokes[startStrokeIdx].High
		low = st.strokes[startStrokeIdx].Low
	} else {
		startPrice = st.segments[startStrokeIdx].startPrice
		endPrice = st.segments[endStrokeIdx].endPrice
		high = st.segments[startStrokeIdx].high
		low = st.segments[startStrokeIdx].low
	}
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

	return &trendPattern{
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
		Completed:      false,
	}
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
		Completed: zs.Completed, EndReason: zs.EndReason, StartPrice: zs.StartPrice,
		EndPrice: zs.EndPrice,
		High:     zs.High,
		Low:      zs.Low,
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
