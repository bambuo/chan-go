// Package chanlun 实现特征序列与线段划分算法（特征序列算法.md / 线段划分算法.md）。
//
// SegmentProcessor 从已确认的笔序列出发，逐步构建线段结构。
//
// 核心流程遵循缠论原文第67/71/78课的"假设转折点"判定法：
//   - 反向笔出现 → 假设其为两线段分界点。
//   - 构建第一特征序列，检测分型（顶/底，双AND定义）。
//   - 分型第1、2元素间无缺口 → 情况一，直接确认线段结束。
//   - 分型第1、2元素间有缺口 → 情况二，需第二特征序列出现分型才确认。
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
// seqKind: 特征序列类型，决定包含处理方向（第67/78课）。
//
//	第一特征序列：包含方向与线段方向相反（第67课）。
//	第二特征序列：包含方向与线段方向一致（第78课）。
func buildFeatureSeq(strokes []*stroke, segDir types.ChanDirection, seqKind types.FeatureSeqType) *featureSeq {
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
	// 包含方向：与线段方向相反（向上线段向下合并，向下线段向上合并）。
	//
	// 第一特征序列（第67课）：segDir = 当前线段方向，与线段方向相反 = 与 segDir 相反。
	// 第二特征序列（第78课）：segDir = 新线段方向 = opposite(原方向)，
	//   与 segDir 相反 = 与原线段方向一致（符合第78课要求）。
	// 两种情况下公式相同：mergeUp = (segDir == Down)。
	mergeUp := fs.segDir == types.DirectionDown
	fs.applyInclusion(mergeUp)
	return fs
}

// applyInclusion 对特征K线做包含处理。
// mergeUp=true → 向上合并（取较高高点和较高低点）；
// mergeUp=false → 向下合并（取较低高点和较低低点）。
func (fs *featureSeq) applyInclusion(mergeUp bool) {
	if len(fs.raw) == 0 {
		return
	}

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

// detectFractal 检测特征序列分型（参照普通分型 AND 双条件定义）。
// 原文："参照一般K线图关于顶分型与底分型的定义"
// 即顶分型要求高点最高且低点也最高，底分型要求低点最低且高点也最低。
func (fs *featureSeq) detectFractal() types.FeatureSeqFractal {
	var result types.FeatureSeqFractal
	elems := fs.elems

	for i := 1; i < len(elems)-1; i++ {
		// 顶分型：中点最高 且 中点也最高（AND，同普通分型）
		if elems[i].high > elems[i-1].high && elems[i].high > elems[i+1].high &&
			elems[i].low > elems[i-1].low && elems[i].low > elems[i+1].low {
			result.HasTop = true
			result.TopIndex = elems[i].index
		}
		// 底分型：低点最低 且 高点也最低（AND，同普通分型）
		if elems[i].low < elems[i-1].low && elems[i].low < elems[i+1].low &&
			elems[i].high < elems[i-1].high && elems[i].high < elems[i+1].high {
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

// segPending 情况二的待确认中间态。
// 第一特征序列出现分型且分型第1、2元素间有缺口时，记录转折点信息，
// 等待第二特征序列出现分型后才确认线段结束。
type segPending struct {
	dir          types.ChanDirection // 被破坏线段方向（即 current.direction）
	pivotStroke  *stroke             // 第一特征序列分型中间元素对应的笔（转折点）
}

// segState 线段划分状态机。
type segState struct {
	segments     []*segment  // 已完成和当前的线段
	current      *segment    // 当前正在构建的线段
	pending      *segPending // 情况二待确认中间态
	totalStrokes int         // 已处理的总笔数
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

// ReprocessFrom 从指定笔索引开始重算线段（PRD §10.4.3 回溯修正）。
// 删除 fromIdx 之后的所有线段，重置 totalStrokes 到 fromIdx。
func (sp *SegmentProcessor) ReprocessFrom(symbol string, fromIdx int) {
	st := sp.getState(symbol)
	if fromIdx >= st.totalStrokes {
		return // 无需重算
	}

	// 删除从 fromIdx 开始的线段
	var kept []*segment
	for _, seg := range st.segments {
		if len(seg.strokes) == 0 {
			continue
		}
		lastStroke := seg.strokes[len(seg.strokes)-1]
		if lastStroke.Index < fromIdx {
			kept = append(kept, seg)
		}
	}
	st.segments = kept
	st.current = nil
	st.pending = nil

	// 修正已处理笔计数
	// 需要删除的笔至少跨越一个线段，保守起见重置到 fromIdx - 2
	resetIdx := fromIdx - 2
	if resetIdx < 0 {
		resetIdx = 0
	}
	st.totalStrokes = resetIdx
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

// ====== segment getter ======

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

// ====== segState 方法 ======

// currentSegments 返回当前所有线段。
func (s *segState) currentSegments() []*segment {
	return s.segments
}

// processStroke 增量处理一根新笔（线段划分算法.md 完整流程）。
func (s *segState) processStroke(newStroke *stroke) {
	if s.current == nil {
		// 第一笔 → 创建首个线段
		s.startNewSegment(newStroke)
		return
	}

	if newStroke.Direction == s.current.direction {
		// 同向：延伸当前线段
		s.extendCurrent(newStroke)
		// 同向笔到来时，若存在情况二待确认态，需重新尝试第二特征序列确认
		s.tryPendingConfirm()
		return
	}

	// 反向笔：进入假设转折点判定
	s.trySegmentBreak(newStroke)
}

// startNewSegment 用第一笔创建首个线段。
func (s *segState) startNewSegment(newStroke *stroke) {
	seg := &segment{
		index:       len(s.segments),
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

// trySegmentBreak 处理反向笔：假设转折点判定。
// 将反向笔并入当前线段后，构建第一特征序列，检测分型：
//   - 无分型 → 等待更多笔。
//   - 有分型且第1、2元素无缺口（情况一）→ 直接确认线段结束。
//   - 有分型且第1、2元素有缺口（情况二）→ 记录 pending，等待第二特征序列确认。
func (s *segState) trySegmentBreak(newStroke *stroke) {
	currSeg := s.current
	currSeg.strokes = append(currSeg.strokes, newStroke)
	currSeg.endStroke = newStroke
	currSeg.endPrice = newStroke.EndPrice
	if newStroke.High > currSeg.high {
		currSeg.high = newStroke.High
	}
	if newStroke.Low < currSeg.low {
		currSeg.low = newStroke.Low
	}

	// 向上线段只看特征序列顶分型；向下线段只看底分型（第67课）。
	fs1 := buildFeatureSeq(currSeg.strokes, currSeg.direction, types.FeatureSeqPrimary)
	fractal := fs1.detectFractal()

	var hasFractal bool
	var fractalIdx int
	if currSeg.direction == types.DirectionUp && fractal.HasTop {
		hasFractal = true
		fractalIdx = fractal.TopIndex
	} else if currSeg.direction == types.DirectionDown && fractal.HasBottom {
		hasFractal = true
		fractalIdx = fractal.BottomIdx
	}

	if !hasFractal {
		return // 继续等待
	}

	// 取分型中间元素及其前一个元素（第1、2元素），判缺口。
	if fractalIdx < 1 || fractalIdx >= len(fs1.elems) {
		return
	}
	pivotStroke := fs1.elems[fractalIdx].sourceStroke // 转折点笔（用指针定位，避免索引失效）
	elem1 := fs1.elems[fractalIdx-1]                  // 第1元素
	elem2 := fs1.elems[fractalIdx]                    // 第2元素（分型中间）

	if !hasGap(elem1, elem2) {
		// 情况一：无缺口，直接确认线段在分型极值点结束。
		s.confirmBreak(pivotStroke)
		return
	}

	// 情况二：有缺口，记录 pending（存转折点笔指针），等待第二特征序列确认。
	s.pending = &segPending{
		dir:         currSeg.direction,
		pivotStroke: pivotStroke,
	}
	// 立即尝试一次第二特征序列确认（当前笔数可能已足够）。
	s.tryPendingConfirm()
}

// tryPendingConfirm 情况二：尝试用第二特征序列确认线段结束。
// 从第一特征序列分型转折点笔之后，构建第二特征序列。
// 第67课："第二个序列中的分型，不分第一二种情况，只要有分型就可以。"
func (s *segState) tryPendingConfirm() {
	if s.pending == nil || s.current == nil {
		return
	}

	currSeg := s.current
	pivotStroke := s.pending.pivotStroke

	// 定位转折点笔在 currSeg.strokes 中的位置。
	pivotPos := -1
	for i, b := range currSeg.strokes {
		if b == pivotStroke {
			pivotPos = i
			break
		}
	}
	if pivotPos < 0 {
		return
	}

	// 第二特征序列：转折点之后的笔。
	// 转折点笔方向与原线段相反（它是原线段特征序列的元素），
	// 从它开始构成假设的新线段，方向 = opposite(currSeg.direction)。
	// 新线段的特征序列 = 与新线段反向的笔 = 与原线段同向的笔。
	subStrokes := currSeg.strokes[pivotPos:]
	if len(subStrokes) < 3 {
		return // 笔数不足以构成第二特征序列
	}

	newDir := oppositeDirection(currSeg.direction)
	fs2 := buildFeatureSeq(subStrokes, newDir, types.FeatureSeqSecondary)
	fractal2 := fs2.detectFractal()

	// 第二特征序列只需出现分型即可（不分情况，第67课）。
	// 新线段方向的规则同第一特征序列（第67课第5922行）：
	// 向上线段只看顶分型，向下线段只看底分型。
	confirmed := false
	if newDir == types.DirectionUp && fractal2.HasTop {
		confirmed = true
	} else if newDir == types.DirectionDown && fractal2.HasBottom {
		confirmed = true
	}

	if confirmed {
		s.confirmBreak(pivotStroke)
	}
	// 未确认则保持 pending，等待后续笔再来尝试。
}

// confirmBreak 确认当前线段在转折点处结束，并起一个新线段。
// pivotStroke: 第一特征序列分型中间元素对应的笔（转折点）。
func (s *segState) confirmBreak(pivotStroke *stroke) {
	currSeg := s.current

	// 定位转折点笔在 currSeg.strokes 中的位置。
	pivotPos := -1
	for i, b := range currSeg.strokes {
		if b == pivotStroke {
			pivotPos = i
			break
		}
	}
	if pivotPos < 0 {
		return
	}

	// 校验线段方向首尾一致性（第78课）：
	// 向上线段必须结束于向上笔（向上笔的顶 = 终点），
	// 向下线段必须结束于向下笔（向下笔的底 = 终点）。
	// pivotStroke 是特征序列元素，方向与线段相反；其前一笔同向于线段，为真正终点。
	endStroke := pivotStroke
	if pivotPos > 0 {
		endStroke = currSeg.strokes[pivotPos-1]
	}
	if endStroke.Direction != currSeg.direction {
		return // 方向不一致，转折点假设不成立，放弃确认
	}

	remainStrokes := currSeg.strokes[:pivotPos] // endStroke 及之前属于原线段
	if len(remainStrokes) == 0 {
		return
	}

	// 完成当前线段
	completedSeg := s.finalizeSegment(currSeg, remainStrokes)
	s.segments = append(s.segments, completedSeg)

	// 新线段：pivotPos 之后的笔，方向取反
	newSegStrokes := currSeg.strokes[pivotPos:]
	newDir := oppositeDirection(currSeg.direction)
	if len(newSegStrokes) > 0 {
		// 校验新线段首笔方向
		if newSegStrokes[0].Direction == newDir {
			s.startNewSegmentFromStrokes(newSegStrokes, newDir)
		} else {
			s.current = nil
		}
	} else {
		s.current = nil
	}
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
