// Package chanlun 实现中枢识别算法（中枢识别算法.md）。
//
// pivotZone = 连续至少 3 笔（或 3 段）的重叠区间。
// ZG = 三构件最高价的最小值（上沿）
// ZD = 三构件最低价的最大值（下沿）
// ZG > ZD 时为有效中枢。
//
// 支持两种模式：
//   - 笔中枢（默认）：从笔序列构建中枢，快速直观，适合大级别复盘。
//   - 线段中枢：从线段序列构建中枢，稳定性更强，更接近缠论标准递归路径。
package chanlun

import (
	"math"
	"sync"

	"trade/internal/types"
)

// zoneComp 中枢构件的统一表示（笔或线段经转换后生成）。
type zoneComp struct {
	high      float64
	low       float64
	direction types.ChanDirection
}

// strokesToComps 将笔列表转换为统一构件。
func strokesToComps(strokes []*stroke) []zoneComp {
	comps := make([]zoneComp, len(strokes))
	for i, s := range strokes {
		comps[i] = zoneComp{high: s.High, low: s.Low, direction: s.Direction}
	}
	return comps
}

// segmentsToComps 将线段列表转换为统一构件。
// 每个线段作为中枢的一个构件。
func segmentsToComps(segments []*segment) []zoneComp {
	comps := make([]zoneComp, len(segments))
	for i, s := range segments {
		comps[i] = zoneComp{high: s.high, low: s.low, direction: s.direction}
	}
	return comps
}

// pivotZone 中枢内部类型。
type pivotZone struct {
	index          int                 // 中枢序号
	StartStrokeIdx int                 // 起始构件索引
	EndStrokeIdx   int                 // 结束构件索引
	ZG             float64             // 中枢上沿（高）
	ZD             float64             // 中枢下沿（低）
	Direction      types.ChanDirection // 方向
	SegmentsCount  int                 // 已延伸段数
	Completed      bool                // 是否已完成
}

// Index 返回中枢序号。
func (pz *pivotZone) Index() int { return pz.index }

// StrokeCount 返回中枢已延伸构件数（段数）。
func (pz *pivotZone) StrokeCount() int { return pz.SegmentsCount }

// PivotZoneConfig 中枢识别配置。
type PivotZoneConfig struct {
	Mode types.PivotZoneMode // 笔中枢或线段中枢
}

// pivotZoneState 单个 symbol 的中枢识别状态。
type pivotZoneState struct {
	mu           sync.Mutex
	pivotZones   []*pivotZone // 已识别的中枢
	processedIdx int          // 已处理的构件索引（增量用）
}

// PivotZoneProcessor 中枢识别处理器。
type PivotZoneProcessor struct {
	states map[string]*pivotZoneState
	config PivotZoneConfig
	mu     sync.Mutex
}

// NewPivotZoneProcessor 创建中枢识别处理器。
func NewPivotZoneProcessor(config ...PivotZoneConfig) *PivotZoneProcessor {
	mode := types.PivotModeStroke
	if len(config) > 0 {
		mode = config[0].Mode
	}
	return &PivotZoneProcessor{
		states: make(map[string]*pivotZoneState),
		config: PivotZoneConfig{Mode: mode},
	}
}

// Process 增量处理笔列表（笔中枢模式），返回变化后的中枢列表。
func (zp *PivotZoneProcessor) Process(symbol string, strokes []*stroke) []*pivotZone {
	comps := strokesToComps(strokes)
	return zp.process(symbol, comps)
}

// ProcessSegments 增量处理线段列表（线段中枢模式），返回变化后的中枢列表。
func (zp *PivotZoneProcessor) ProcessSegments(symbol string, segments []*segment) []*pivotZone {
	comps := segmentsToComps(segments)
	return zp.process(symbol, comps)
}

// process 内部核心算法：在统一构件 comps 上增量扫描形成中枢。
func (zp *PivotZoneProcessor) process(symbol string, comps []zoneComp) []*pivotZone {
	st := zp.getState(symbol)
	st.mu.Lock()
	defer st.mu.Unlock()

	startIdx := st.processedIdx
	if startIdx > 2 {
		startIdx -= 2
	}

	for i := startIdx; i < len(comps); i++ {
		if st.withinAnyPivotZone(i) {
			continue
		}
		if i+2 >= len(comps) {
			break
		}
		if zs := tryFormPivotZone(comps, i, len(st.pivotZones)); zs != nil {
			st.extendPivotZone(zs, comps, i+3)
			st.pivotZones = append(st.pivotZones, zs)
			st.processedIdx = zs.EndStrokeIdx
			i = zs.EndStrokeIdx
		}
	}

	return st.pivotZones
}

// ReprocessFrom 从指定构件索引开始重算中枢（PRD §10.4.3 回溯修正）。
// fromIdx 传入时始终是笔索引，段模式下内部自动映射为段索引。
func (zp *PivotZoneProcessor) ReprocessFrom(symbol string, fromIdx int) {
	st := zp.getState(symbol)
	st.mu.Lock()
	defer st.mu.Unlock()

	// 段模式下，笔索引需映射为段索引
	if zp.config.Mode == types.PivotModeSegment {
		// 在段模式下，fromIdx 是笔索引。由于中枢记录的是段索引，
		// 将 fromIdx 映射为段索引：保守取 fromIdx - 1。
		// 因为每段至少含 3 笔，段索引 ≈ 笔索引 / 3。
		// 用 -1 回退确保受影响线段都被重算。
		segIdx := fromIdx/3 - 1
		if segIdx < 0 {
			segIdx = 0
		}
		fromIdx = segIdx
	}

	// 删除从 fromIdx 之后的中枢
	var kept []*pivotZone
	for _, zs := range st.pivotZones {
		if zs.EndStrokeIdx < fromIdx {
			kept = append(kept, zs)
		}
	}
	st.pivotZones = kept

	// 重置已处理索引，允许重算
	resetIdx := fromIdx - 3
	if resetIdx < 0 {
		resetIdx = 0
	}
	st.processedIdx = resetIdx
}

// Load 返回所有已识别的中枢。
func (zp *PivotZoneProcessor) Load(symbol string) []*pivotZone {
	st := zp.getState(symbol)
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.pivotZones
}

// Reset 重置指定 symbol 的状态。
func (zp *PivotZoneProcessor) Reset(symbol string) {
	zp.mu.Lock()
	defer zp.mu.Unlock()
	delete(zp.states, symbol)
}

// getState 获取或创建指定 symbol 的中枢识别状态。
func (zp *PivotZoneProcessor) getState(symbol string) *pivotZoneState {
	zp.mu.Lock()
	defer zp.mu.Unlock()
	if s, ok := zp.states[symbol]; ok {
		return s
	}
	s := &pivotZoneState{}
	zp.states[symbol] = s
	return s
}

// tryFormPivotZone 尝试用三个构件形成中枢。
func tryFormPivotZone(comps []zoneComp, startIdx int, nextIndex int) *pivotZone {
	if startIdx+2 >= len(comps) {
		return nil
	}
	c1, c2, c3 := comps[startIdx], comps[startIdx+1], comps[startIdx+2]
	zg := math.Min(c1.high, math.Min(c2.high, c3.high))
	zd := math.Max(c1.low, math.Max(c2.low, c3.low))
	if zg <= zd {
		return nil
	}
	dir := c1.direction
	if dir == types.DirectionNone {
		dir = types.DirectionUp
	}
	return &pivotZone{
		index:          nextIndex,
		StartStrokeIdx: startIdx,
		EndStrokeIdx:   startIdx + 2,
		ZG:             zg,
		ZD:             zd,
		Direction:      dir,
		SegmentsCount:  3,
		Completed:      false,
	}
}

// extendPivotZone 检查后续构件是否与中枢区间重叠，进行延伸。
//
// 完成判定（第三类买卖点确认）：
//   - 离开构件（完全脱离中枢区间）→ 不立即标记完成，等待回抽构件。
//   - 回抽构件不回到中枢区间 → Completed = true（第三类买卖点确认）。
//   - 回抽构件回到中枢区间 → 中枢继续延伸（吸收离开构件）。
func (st *pivotZoneState) extendPivotZone(zs *pivotZone, comps []zoneComp, startFrom int) {
	exitIdx := -1
	for i := startFrom; i < len(comps); i++ {
		s := comps[i]
		if s.high >= zs.ZD && s.low <= zs.ZG {
			zs.SegmentsCount++
			zs.EndStrokeIdx = i
		} else {
			exitIdx = i
			break
		}
	}

	if exitIdx < 0 {
		// 所有剩余构件都与中枢重叠，无限延伸
		zs.Completed = false
		return
	}

	// 有构件离开了中枢区间，检查下一个构件（回抽）是否回到中枢
	pullbackIdx := exitIdx + 1
	if pullbackIdx >= len(comps) {
		// 尚未出现回抽构件，中枢暂未完成
		zs.Completed = false
		return
	}

	pb := comps[pullbackIdx]
	if pb.high >= zs.ZD && pb.low <= zs.ZG {
		// 回抽构件回到了中枢区间 → 中枢继续延伸（吸收离开构件）
		zs.SegmentsCount++
		zs.EndStrokeIdx = pullbackIdx
		// 继续向后延伸
		for i := pullbackIdx + 1; i < len(comps); i++ {
			s := comps[i]
			if s.high >= zs.ZD && s.low <= zs.ZG {
				zs.SegmentsCount++
				zs.EndStrokeIdx = i
			} else {
				break
			}
		}
		zs.Completed = false
	} else {
		// 回抽构件也未回到中枢区间 → 第三类买卖点确认，中枢完成
		zs.Completed = true
	}
}

// withinAnyPivotZone 检查指定构件索引是否已被任何中枢覆盖。
func (st *pivotZoneState) withinAnyPivotZone(idx int) bool {
	for _, zs := range st.pivotZones {
		if idx >= zs.StartStrokeIdx && idx <= zs.EndStrokeIdx {
			return true
		}
	}
	return false
}

// pivotZoneToTypes 将内部 pivotZone 转为 types.PivotZone（用于 M3）。
func pivotZoneToTypes(zs *pivotZone) types.PivotZone {
	return types.PivotZone{
		ZG:           zs.ZG,
		ZD:           zs.ZD,
		Direction:    zs.Direction,
		SegmentCount: zs.SegmentsCount,
	}
}

// PivotZonesToTypes 批量转换。
func PivotZonesToTypes(zss []*pivotZone) []types.PivotZone {
	result := make([]types.PivotZone, len(zss))
	for i, zs := range zss {
		result[i] = pivotZoneToTypes(zs)
	}
	return result
}
