// Package chanlun 实现中枢识别算法（中枢识别算法.md）。
//
// pivotZone = 连续至少 3 笔的重叠区间。
// ZG = 三笔最高价的最小值（上沿）
// ZD = 三笔最低价的最大值（下沿）
// ZG > ZD 时为有效中枢。
package chanlun

import (
	"math"
	"sync"

	"trade/internal/types"
)

// pivotZone 中枢内部类型。
type pivotZone struct {
	index          int                 // 中枢序号
	StartStrokeIdx int                 // 起始笔索引
	EndStrokeIdx   int                 // 结束笔索引
	ZG             float64             // 中枢上沿（高）
	ZD             float64             // 中枢下沿（低）
	Direction      types.ChanDirection // 方向
	SegmentsCount  int                 // 已延伸段数
	Completed      bool                // 是否已完成
}

// Index 返回中枢序号。
func (pz *pivotZone) Index() int { return pz.index }

// StrokeCount 返回中枢已延伸笔数（段数）。
func (pz *pivotZone) StrokeCount() int { return pz.SegmentsCount }

// pivotZoneState 单个 symbol 的中枢识别状态。
type pivotZoneState struct {
	mu           sync.Mutex
	strokes      []*stroke    // 当前所有笔
	pivotZones   []*pivotZone // 已识别的中枢
	processedIdx int          // 已处理的笔索引（增量用）
}

// PivotZoneProcessor 中枢识别处理器。
type PivotZoneProcessor struct {
	states map[string]*pivotZoneState
	mu     sync.Mutex
}

// NewPivotZoneProcessor 创建中枢识别处理器。
func NewPivotZoneProcessor() *PivotZoneProcessor {
	return &PivotZoneProcessor{
		states: make(map[string]*pivotZoneState),
	}
}

// Process 增量处理笔列表，返回变化后的中枢列表。
func (zp *PivotZoneProcessor) Process(symbol string, strokes []*stroke) []*pivotZone {
	st := zp.getState(symbol)
	st.mu.Lock()
	defer st.mu.Unlock()

	st.strokes = strokes

	startIdx := st.processedIdx
	if startIdx > 2 {
		startIdx -= 2
	}

	for i := startIdx; i < len(strokes); i++ {
		if st.withinAnyPivotZone(i) {
			continue
		}
		if i+2 >= len(strokes) {
			break
		}
		if zs := tryFormPivotZone(strokes, i, len(st.pivotZones)); zs != nil {
			st.extendPivotZone(zs, strokes, i+3)
			st.pivotZones = append(st.pivotZones, zs)
			st.processedIdx = zs.EndStrokeIdx
			i = zs.EndStrokeIdx
		}
	}

	return st.pivotZones
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

// tryFormPivotZone 尝试用三笔形成中枢。
func tryFormPivotZone(strokes []*stroke, startIdx int, nextIndex int) *pivotZone {
	if startIdx+2 >= len(strokes) {
		return nil
	}
	s1, s2, s3 := strokes[startIdx], strokes[startIdx+1], strokes[startIdx+2]
	zg := math.Min(s1.High, math.Min(s2.High, s3.High))
	zd := math.Max(s1.Low, math.Max(s2.Low, s3.Low))
	if zg <= zd {
		return nil
	}
	dir := s1.Direction
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

// extendPivotZone 检查后续笔是否与中枢区间重叠，进行延伸。
func (st *pivotZoneState) extendPivotZone(zs *pivotZone, strokes []*stroke, startFrom int) {
	for i := startFrom; i < len(strokes); i++ {
		s := strokes[i]
		if s.High >= zs.ZD && s.Low <= zs.ZG {
			zs.SegmentsCount++
			zs.EndStrokeIdx = i
		} else {
			break
		}
	}
	next := zs.EndStrokeIdx + 1
	if next < len(strokes) {
		s := strokes[next]
		if s.High < zs.ZD || s.Low > zs.ZG {
			zs.Completed = true
		}
	} else {
		zs.Completed = false
	}
}

// withinAnyPivotZone 检查指定笔索引是否已被任何中枢覆盖。
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
