// Package chanlun 实现特征序列与线段划分算法（特征序列算法.md / 线段划分算法.md）。
//
// SegmentProcessor 从已确认的笔序列出发，逐步构建线段结构。
// 核心流程：每根新笔 → 加入当前线段 → 检测线段破坏（情况一/二）。
package chanlun

import (
	"trade/internal/types"
)

// ====== 特征序列 ======

// fk 特征序列K线内部类型（对应 types.FeatureKLine，但用具体 stroke 指针）。
type fk struct {
	high         float64             // 高点
	low          float64             // 低点
	sourceStroke *stroke             // 来源笔
	direction    types.ChanDirection // 方向
	contained    bool                // 是否被前一根包含
	index        int                 // 在特征序列中的索引
	prev         *fk                 // 前一根特征K线（链表）
	next         *fk                 // 后一根特征K线（链表）
}

// featureSeq 特征序列处理器。
type featureSeq struct {
	elems  []*fk               // 特征K线列表（已包含处理）
	raw    []*fk               // 原始（未包含处理）特征K线
	segDir types.ChanDirection // 所属线段方向
}

// buildFeatureSeq 从笔列表中提取特征序列。
// segDir: 线段方向。向上线段取所有向下笔，向下线段取所有向上笔。
func buildFeatureSeq(strokes []*stroke, segDir types.ChanDirection) *featureSeq {
	fs := &featureSeq{segDir: segDir}
	for _, b := range strokes {
		if b.Direction != segDir {
			fs.raw = append(fs.raw, &fk{
				high:         b.High,
				low:          b.Low,
				sourceStroke: b,
				direction:    b.Direction,
			})
		}
	}
	fs.applyInclusion()
	return fs
}

// applyInclusion 对特征K线做包含处理。
// 向上线段：特征序列包含处理方向与线段方向相反 = 向下（取较低高点和较低低点）
// 向下线段：特征序列包含处理方向与线段方向相反 = 向上（取较高高点和较高低点）
func (fs *featureSeq) applyInclusion() {
	if len(fs.raw) == 0 {
		return
	}

	// 合并方向：与线段方向相反
	mergeUp := fs.segDir == types.DirectionDown // 向下线段 → 向上合并

	for _, r := range fs.raw {
		r.index = len(fs.elems)
		if len(fs.elems) == 0 {
			fs.elems = append(fs.elems, r)
			continue
		}

		last := fs.elems[len(fs.elems)-1]
		if isFKContained(r, last) {
			// 包含 → 合并
			if mergeUp {
				// 向上合并：较高高点和较高低点
				if r.high > last.high {
					last.high = r.high
				}
				if r.low > last.low {
					last.low = r.low
				}
			} else {
				// 向下合并：较低高点和较低低点
				if r.high < last.high {
					last.high = r.high
				}
				if r.low < last.low {
					last.low = r.low
				}
			}
			r.contained = true
		} else {
			// 更新链表指针
			if len(fs.elems) > 0 {
				last.next = r
				r.prev = last
			}
			fs.elems = append(fs.elems, r)
		}
	}

	// 更新索引
	for i, e := range fs.elems {
		e.index = i
	}
}

// detectFractal 检测特征序列分型。
func (fs *featureSeq) detectFractal() types.FeatureSeqFractal {
	var result types.FeatureSeqFractal
	elems := fs.elems

	for i := 1; i < len(elems)-1; i++ {
		// 顶分型：中 > 左 且 中 > 右
		if elems[i].high > elems[i-1].high && elems[i].high > elems[i+1].high {
			result.HasTop = true
			result.TopIndex = elems[i].index
		}
		// 底分型：中 < 左 且 中 < 右
		if elems[i].low < elems[i-1].low && elems[i].low < elems[i+1].low {
			result.HasBottom = true
			result.BottomIdx = elems[i].index
		}
	}
	return result
}

// hasGap 检查两根特征K线之间是否有缺口（价格区间完全不重叠）。
func hasGap(a, b *fk) bool {
	return a.high < b.low || a.low > b.high
}

// isFKContained 检查两根特征K线是否存在包含关系。
func isFKContained(a, b *fk) bool {
	return (a.high <= b.high && a.low >= b.low) ||
		(a.high >= b.high && a.low <= b.low)
}

// ====== 线段 ======

// segment 线段内部类型。
type segment struct {
	index       int                 // 线段序号
	direction   types.ChanDirection // 线段方向
	strokes     []*stroke           // 构成笔列表
	startStroke *stroke             // 第一笔
	endStroke   *stroke             // 最后一笔
	startPrice  float64             // 起点价格
	endPrice    float64             // 终点价格
	high        float64             // 区间最高
	low         float64             // 区间最低
	confirmed   bool                // 是否已完成
}

// segState 线段划分状态机。
type segState struct {
	segments     []*segment // 已完成和当前的线段
	current      *segment   // 当前正在构建的线段
	pending      *segment   // 待确认的候选线段（情况二）
	totalStrokes int        // 已处理的总笔数
}

// SegmentProcessor 线段划分处理器。
type SegmentProcessor struct {
	states map[string]*segState
}

// NewSegmentProcessor 创建线段处理器。
func NewSegmentProcessor() *SegmentProcessor {
	return &SegmentProcessor{
		states: make(map[string]*segState),
	}
}

// Process 处理一批已确认的笔，增量更新线段列表。
func (sp *SegmentProcessor) Process(symbol string, strokes []*stroke) []*segment {
	st := sp.getState(symbol)

	for i := st.totalStrokes; i < len(strokes); i++ {
		st.processStroke(strokes[i])
		st.totalStrokes++
	}
	return st.currentSegments()
}

// CurrentSegments 返回当前所有线段。
func (sp *SegmentProcessor) CurrentSegments(symbol string) []*segment {
	st := sp.getState(symbol)
	return st.currentSegments()
}

// getState 获取/创建 symbol 的状态机。
func (sp *SegmentProcessor) getState(symbol string) *segState {
	if s, ok := sp.states[symbol]; ok {
		return s
	}
	s := &segState{}
	sp.states[symbol] = s
	return s
}

// Reset 重置指定 symbol 的状态。
func (sp *SegmentProcessor) Reset(symbol string) {
	delete(sp.states, symbol)
}

// ====== segState 方法 ======

// Direction 返回线段方向。
func (s *segment) Direction() types.ChanDirection { return s.direction }

// StartPrice 返回线段起点价格。
func (s *segment) StartPrice() float64 { return s.startPrice }

// EndPrice 返回线段终点价格。
func (s *segment) EndPrice() float64 { return s.endPrice }

// High 返回线段区间最高价。
func (s *segment) High() float64 { return s.high }

// Low 返回线段区间最低价。
func (s *segment) Low() float64 { return s.low }

// Completed 返回线段是否已完成。
func (s *segment) Completed() bool { return s.confirmed }

// currentSegments 返回当前所有线段。
func (s *segState) currentSegments() []*segment {
	return s.segments
}

// processStroke 增量处理一根新笔。
func (s *segState) processStroke(newStroke *stroke) {
	if s.current == nil {
		// 第一笔 → 创建首个线段
		s.startNewSegment(newStroke)
		return
	}

	if newStroke.Direction == s.current.direction {
		// 同向：延伸当前线段
		s.extendCurrent(newStroke)
		return
	}

	// 反向笔：检查线段破坏
	s.checkSegmentBreak(newStroke)
}

// startNewSegment 用第一笔创建首个线段。
func (s *segState) startNewSegment(newStroke *stroke) {
	seg := &segment{
		index:       0,
		direction:   newStroke.Direction,
		strokes:     []*stroke{newStroke},
		startStroke: newStroke,
		endStroke:   newStroke,
		startPrice:  newStroke.StartPrice,
		endPrice:    newStroke.EndPrice,
		high:        newStroke.High,
		low:         newStroke.Low,
	}
	s.current = seg
}

// extendCurrent 同向笔延伸当前线段。
func (s *segState) extendCurrent(newStroke *stroke) {
	s.current.strokes = append(s.current.strokes, newStroke)
	s.current.endStroke = newStroke
	s.current.endPrice = newStroke.EndPrice
	if newStroke.High > s.current.high {
		s.current.high = newStroke.High
	}
	if newStroke.Low < s.current.low {
		s.current.low = newStroke.Low
	}
}

// checkSegmentBreak 检查线段破坏（线段划分算法.md）。
func (s *segState) checkSegmentBreak(newStroke *stroke) {
	currSeg := s.current

	// 情况一：直接笔破坏
	// 向上线段 → 向下笔的低点跌破前一段倒数第二笔（向上笔）的低点
	// 向下线段 → 向上笔的高点升破前一段倒数第二笔（向下笔）的高点
	directBreak := false
	if len(currSeg.strokes) >= 2 {
		secondLastStroke := currSeg.strokes[len(currSeg.strokes)-2] // 倒数第二笔（同向）
		if currSeg.direction == types.DirectionUp {
			// 当前向上段遇到向下笔
			if newStroke.Low < secondLastStroke.Low {
				directBreak = true
			}
		} else {
			// 当前向下段遇到向上笔
			if newStroke.High > secondLastStroke.High {
				directBreak = true
			}
		}
	}

	if directBreak {
		// 情况一：需要特征序列分型确认
		s.handleType1Break(newStroke)
	} else {
		// 情况二：未直接破坏，等待特征序列分型（无缺口）
		s.handleType2Break(newStroke)
	}
}

// handleType1Break 处理情况一：直接笔破坏。
func (s *segState) handleType1Break(newStroke *stroke) {
	currSeg := s.current
	oppositeDir := oppositeDirection(currSeg.direction)

	// 创建候选线段（新方向），包含新笔
	pendingSeg := &segment{
		index:       len(s.segments),
		direction:   oppositeDir,
		strokes:     []*stroke{newStroke},
		startStroke: newStroke,
		endStroke:   newStroke,
		startPrice:  newStroke.StartPrice,
		endPrice:    newStroke.EndPrice,
		high:        newStroke.High,
		low:         newStroke.Low,
	}

	// 构建候选线段的特征序列（同方向的笔）
	// 候选线段方向 = oppositeDir，特征序列 = 方向相同（因为取反向笔）
	// 向上候选线段的特征序列 = 向下笔 = 当前 segment 方向
	// 我们只需要检查候选线段中是否有足够的笔形成特征序列分型
	if len(pendingSeg.strokes) >= 3 {
		fs := buildFeatureSeq(pendingSeg.strokes, oppositeDir)
		fractal := fs.detectFractal()
		if oppositeDir == types.DirectionUp && fractal.HasBottom {
			s.confirmSegmentBreak(pendingSeg)
			return
		}
		if oppositeDir == types.DirectionDown && fractal.HasTop {
			s.confirmSegmentBreak(pendingSeg)
			return
		}
	}

	// 特征序列分型尚未形成，将新笔加入当前线段继续等待
	// 但记录为"有笔破坏"状态
	currSeg.strokes = append(currSeg.strokes, newStroke)
	s.pending = pendingSeg
}

// handleType2Break 处理情况二：未直接笔破坏，等待特征序列分型。
func (s *segState) handleType2Break(newStroke *stroke) {
	currSeg := s.current
	oppositeDir := oppositeDirection(currSeg.direction)

	// 将新笔加入当前线段
	currSeg.strokes = append(currSeg.strokes, newStroke)

	// 构建当前线段的特征序列
	fs := buildFeatureSeq(currSeg.strokes, currSeg.direction)
	fractal := fs.detectFractal()

	// 检查是否有匹配方向的分型
	hasFractal := false
	if currSeg.direction == types.DirectionUp && fractal.HasTop {
		// 向上线段特征序列中出现的顶分型 → 确认线段被破坏
		hasFractal = true
	}
	if currSeg.direction == types.DirectionDown && fractal.HasBottom {
		hasFractal = true
	}

	if !hasFractal {
		return // 继续等待
	}

	// 找到分型，确认线段破坏
	// 将当前线段完成，从分型位置分裂
	splitIdx := -1
	if currSeg.direction == types.DirectionUp && fractal.HasTop {
		// 找到特征序列顶分型对应的笔
		splitIdx = s.featureFractalToStrokeIndex(currSeg.strokes, currSeg.direction, fractal.TopIndex)
	}
	if currSeg.direction == types.DirectionDown && fractal.HasBottom {
		splitIdx = s.featureFractalToStrokeIndex(currSeg.strokes, currSeg.direction, fractal.BottomIdx)
	}

	if splitIdx >= 0 && splitIdx < len(currSeg.strokes)-1 {
		// 分裂：分型前的笔属于原线段，分型后的笔属于新线段
		remainStrokes := currSeg.strokes[:splitIdx+1]
		newSegStrokes := currSeg.strokes[splitIdx+1:]

		// 完成当前线段
		completedSeg := s.finalizeSegment(currSeg, remainStrokes)
		s.segments = append(s.segments, completedSeg)

		// 创建新线段
		if len(newSegStrokes) > 0 {
			s.startNewSegmentFromStrokes(newSegStrokes, oppositeDir)
		} else {
			// 无剩余笔，等待下一笔
			s.current = nil
		}
	}
}

// confirmSegmentBreak 确认线段破坏（情况一确认）。
func (s *segState) confirmSegmentBreak(pendingSeg *segment) {
	currSeg := s.current

	// 找出 pendingSeg 第一笔在 currSeg 中的位置
	breakIdx := -1
	for i, b := range currSeg.strokes {
		if b == pendingSeg.strokes[0] {
			breakIdx = i
			break
		}
	}

	if breakIdx < 0 {
		// 不应发生
		return
	}

	// 当前线段完成（包含破坏笔之前的所有笔）
	completedBis := currSeg.strokes[:breakIdx]
	if len(completedBis) > 0 {
		completedSeg := s.finalizeSegment(currSeg, completedBis)
		s.segments = append(s.segments, completedSeg)
	}

	// pendingSeg 成为当前线段
	s.current = pendingSeg
	s.pending = nil
}

// finalizeSegment 将线段标记为已完成。
func (s *segState) finalizeSegment(seg *segment, strokes []*stroke) *segment {
	completed := &segment{
		index:       seg.index,
		direction:   seg.direction,
		strokes:     strokes,
		startStroke: strokes[0],
		endStroke:   strokes[len(strokes)-1],
		startPrice:  strokes[0].StartPrice,
		endPrice:    strokes[len(strokes)-1].EndPrice,
		high:        strokes[0].High,
		low:         strokes[0].Low,
		confirmed:   true,
	}
	for _, b := range strokes {
		if b.High > completed.high {
			completed.high = b.High
		}
		if b.Low < completed.low {
			completed.low = b.Low
		}
	}
	completed.startPrice = strokes[0].StartPrice
	completed.endPrice = strokes[len(strokes)-1].EndPrice
	return completed
}

// startNewSegmentFromStrokes 用一组笔创建新线段。
func (s *segState) startNewSegmentFromStrokes(strokes []*stroke, dir types.ChanDirection) {
	seg := &segment{
		index:       len(s.segments),
		direction:   dir,
		strokes:     strokes,
		startStroke: strokes[0],
		endStroke:   strokes[len(strokes)-1],
		startPrice:  strokes[0].StartPrice,
		endPrice:    strokes[len(strokes)-1].EndPrice,
		high:        strokes[0].High,
		low:         strokes[0].Low,
	}
	for _, b := range strokes {
		if b.High > seg.high {
			seg.high = b.High
		}
		if b.Low < seg.low {
			seg.low = b.Low
		}
	}
	s.segments = append(s.segments, seg)
	s.current = nil
}

// featureFractalToStrokeIndex 将特征序列分型索引映射到笔列表索引。
func (s *segState) featureFractalToStrokeIndex(strokes []*stroke, segDir types.ChanDirection, featureIdx int) int {
	strokeCount := 0
	for i, st := range strokes {
		if st.Direction != segDir {
			if strokeCount == featureIdx {
				return i
			}
			strokeCount++
		}
	}
	return len(strokes) - 1
}

// ====== 全局辅助 ======

// oppositeDirection 返回反方向。
func oppositeDirection(d types.ChanDirection) types.ChanDirection {
	if d == types.DirectionUp {
		return types.DirectionDown
	}
	if d == types.DirectionDown {
		return types.DirectionUp
	}
	return types.DirectionNone
}

// strokeToSegmentStrokes 转换 []*stroke → []interface{}（用于 types.Segment）。
func strokeToSegmentStrokes(strokes []*stroke) []interface{} {
	result := make([]interface{}, len(strokes))
	for i, b := range strokes {
		result[i] = b
	}
	return result
}

// segmentToTypes 将内部 segment 转换为 types.Segment。
func segmentToTypes(s *segment) types.Segment {
	return types.Segment{
		Index:       s.index,
		Direction:   s.direction,
		Strokes:     strokeToSegmentStrokes(s.strokes),
		StartStroke: s.startStroke,
		EndStroke:   s.endStroke,
		StartPrice:  s.startPrice,
		EndPrice:    s.endPrice,
		High:        s.high,
		Low:         s.low,
		Confirmed:   s.confirmed,
	}
}

// SegmentsToInterface 转换 []*segment → []interface{}（PipelineOutput 用）。
func SegmentsToInterface(segs []*segment) []interface{} {
	result := make([]interface{}, len(segs))
	for i, s := range segs {
		result[i] = s
	}
	return result
}
