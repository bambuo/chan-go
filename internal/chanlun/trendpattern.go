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
	strokes      []*stroke  // 笔列表（笔中枢模式）
	segments     []*segment // 线段列表（线段中枢模式）
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

// classify 增量更新走势类型分类（只处理最后一个未完成走势 + 新中枢）。
//
// 核心原则（缠论）：已完成的走势固定不变，只有当前走势会被新中枢推进或破坏。
//
// 四种情况：
//  1. 无任何走势 → 全量构建
//  2. 无新中枢 → 不动
//  3. 最后一个走势已被背驰确认完成 → 新中枢从零开始构建新走势
//  4. 最后一个走势未完成 → 带着它的老中枢 + 新中枢一起重新评估
//
// "重新评估"的含义：
//   - 新中枢延续同一方向 → 当前走势延伸
//   - 方向变化/重叠/衰竭 → 当前走势完成（reverseBreak），新中枢另起走势
func (st *trendPatternState) classify() {
	if len(st.pivotZones) == 0 {
		st.patterns = nil
		return
	}

	// ---------- 情况1：首次或回溯后清空 → 全量构建 ----------
	if len(st.patterns) == 0 {
		groups := groupPivotZones(st.pivotZones)
		st.patterns = nil
		for gi, g := range groups {
			isLast := gi == len(groups)-1
			p := st.buildPattern(g.zones)
			if !isLast {
				p.Completed = true
				p.EndReason = "reverseBreak"
			}
			st.patterns = append(st.patterns, p)
		}
		st.processedIdx = len(st.pivotZones)
		return
	}

	// ---------- 情况2：无新中枢 → 不动 ----------
	if st.processedIdx >= len(st.pivotZones) {
		return
	}

	lastPattern := st.patterns[len(st.patterns)-1]

	// ---------- 情况3：最后一个走势已被背驰确认完成 → 新中枢独立建走势 ----------
	if lastPattern.Completed {
		newZones := st.pivotZones[st.processedIdx:]
		if len(newZones) == 0 {
			return
		}
		groups := groupPivotZones(newZones)
		for _, g := range groups {
			p := st.buildPattern(g.zones)
			// 所有组都是新的，只有最后一组是当前走势
			// 但此处我们无法判断后续是否还有新中枢，故全标记为未完成
			// （后续 classify 会用情况4继续推进）
			st.patterns = append(st.patterns, p)
		}
		st.processedIdx = len(st.pivotZones)
		return
	}

	// ---------- 情况4：最后一个走势未完成 → 带老中枢 + 新中枢一起重新评估 ----------
	// 从最后一个走势的第一个中枢开始，连同新中枢重新分组
	reEvalStart := 0
	if len(lastPattern.PivotZoneIDs) > 0 {
		reEvalStart = lastPattern.PivotZoneIDs[0]
	}
	// 保护：reEvalStart 不能越界
	if reEvalStart >= len(st.pivotZones) {
		reEvalStart = 0
	}

	// 删除最后一个走势（将被重新评估替换）
	st.patterns = st.patterns[:len(st.patterns)-1]

	// 对影响区域重新分组
	affectedZones := st.pivotZones[reEvalStart:]
	groups := groupPivotZones(affectedZones)

	for gi, g := range groups {
		isLast := gi == len(groups)-1
		p := st.buildPattern(g.zones)
		if !isLast {
			// 被后续反向走势破坏 → 完成
			p.Completed = true
			p.EndReason = "reverseBreak"
		}
		st.patterns = append(st.patterns, p)
	}
	st.processedIdx = len(st.pivotZones)
}

// zoneGroup 中枢分组（内部类型）。
type zoneGroup struct {
	zones []*pivotZone
	dir   types.ChanDirection
}

// groupPivotZones 将中枢列表按方向/重叠/单调性分组。
//
// 分组规则（对应缠论原文第72/78课）：
//   - 一组互不重叠的同向中枢构成一个走势类型。
//   - 每当中枢方向变化或重叠时，结束当前走势、开始新的走势。
//   - 同向不重叠但单调性衰竭（上涨不创新高、下跌不创新低）也结束走势。
func groupPivotZones(zones []*pivotZone) []zoneGroup {
	if len(zones) == 0 {
		return nil
	}
	var groups []zoneGroup
	current := zoneGroup{zones: []*pivotZone{zones[0]}, dir: zones[0].Direction}

	for i := 1; i < len(zones); i++ {
		prev := zones[i-1]
		curr := zones[i]

		// 方向变化或重叠 → 新走势
		if pivotZonesOverlap(prev, curr) || curr.Direction != prev.Direction {
			groups = append(groups, current)
			current = zoneGroup{zones: []*pivotZone{curr}, dir: curr.Direction}
			continue
		}

		// 同向不重叠时，校验方向单调性（第72课）：
		//   上涨趋势：中枢逐次抬高（新 ZD > 旧 ZG）
		//   下跌趋势：中枢逐次降低（新 ZG < 旧 ZD）
		// 不满足单调性说明走势已衰竭，应分属不同走势类型。
		monotonic := false
		if curr.Direction == types.DirectionUp {
			monotonic = curr.ZD > prev.ZG
		} else {
			monotonic = curr.ZG < prev.ZD
		}
		if !monotonic {
			groups = append(groups, current)
			current = zoneGroup{zones: []*pivotZone{curr}, dir: curr.Direction}
		} else {
			current.zones = append(current.zones, curr)
		}
	}
	groups = append(groups, current)
	return groups
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
