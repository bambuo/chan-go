// Package chanlun 实现缠中说禅笔识别算法（笔.md）。
//
// StrokeProcessor 是一个增量状态机，接收经包含处理后的合并K线（含分型标记），
// 逐步识别、创建、修正笔结构，支持确认笔和虚笔两种模式。
package chanlun

import (
	"math"

	"trade/internal/types"
)

// stroke 表示算法内部的一条笔（笔.md §1.3）。
type stroke struct {
	Index        int
	Start        *types.ChanKline
	End          *types.ChanKline
	Direction    types.ChanDirection
	Confirmed    bool
	Virtual      bool
	StartPrice   float64
	EndPrice     float64
	High         float64
	Low          float64
	PreviousEnds []*types.ChanKline // 虚笔曾替换过的确认终点
}

// strokeState 笔识别状态机（笔.md §3）。
type strokeState struct {
	strokes            []*stroke          // 已生成的笔（最后一条可能为虚笔）
	lastEndpoint       *types.ChanKline   // 最后一笔的终点分型
	candidates         []*types.ChanKline // 第一笔前的候选分型
	cfg                types.StrokeConfig
	pendingFractals    []*types.ChanKline // OpenTime 分型记录器，等待确认
	processedFractalTS map[int64]bool     // 已处理的 OpenTime 集合

	// confirmedFractalMap 是已确认分型的 OpenTime 索引（增量累积），
	// 替代每次遍历 allFractals 的 O(N) 扫描，实现 O(1) 查找。
	// PRD §10.4: 引擎不允许全量重算。
	confirmedFractalMap map[int64]bool
	// 记录上次处理的 allFractals 长度，用于检测是否缩短（单元测试场景）
	lastAllFractalsLen int

	// 回溯修正跟踪
	modifiedStrokeIdx int  // 本轮被修改的最早笔索引，-1 表示无修改
	strokeRemoved     bool // 是否有笔被删除
	strokeRemovedIdx  int  // 被删除的笔索引
}

// StrokeProcessor 笔识别处理器。
type StrokeProcessor struct {
	states map[string]*strokeState // symbol → state
}

// NewStrokeProcessor 创建笔识别处理器。
func NewStrokeProcessor() *StrokeProcessor {
	return &StrokeProcessor{
		states: make(map[string]*strokeState),
	}
}

// Process 接收当前所有已确认分型列表，增量更新笔列表。
//
// 增量优化（PRD §10.4）：通过 confirmedFractalMap 维护累积的已确认分型索引，
// 每次只索引新增的分型，匹配时 O(1) 查表，避免全量扫描 allFractals。
func (bp *StrokeProcessor) Process(symbol string, elem *types.ChanKline, allFractals []types.Fractal) []*stroke {
	st := bp.getOrCreateState(symbol)

	// 如果当前元素有有效分型且尚未处理，记录它
	if elem.FractalType != types.FractalNone && !st.processedFractalTS[elem.OpenTime] {
		st.pendingFractals = append(st.pendingFractals, elem)
		// 虚笔尝试（笔.md §10.2 情况二）：分型已识别但尚未确认时，
		// 若它与最后确认终点异类且满足成笔条件，创建一条虚笔用于实时显示。
		// 虚笔被 Strokes() 的 !Virtual 过滤，不外泄到下游；分型后续确认时由
		// clearVirtualStroke 清理并转为确认笔。
		st.tryVirtualStroke(elem)
	}

	// 增量索引：维护已确认分型的 OpenTime map，O(1) 查找替代全量扫描。
	// 当 allFractals 缩短时（单元测试场景），重建 map 以确保完整。
	if len(allFractals) < st.lastAllFractalsLen {
		// allFractals 缩短 → 测试重置场景：重建完整 map
		st.confirmedFractalMap = make(map[int64]bool, len(allFractals))
		for _, f := range allFractals {
			if f.Confirmed && f.OpenTime > 0 {
				st.confirmedFractalMap[f.OpenTime] = true
			}
		}
	} else {
		// allFractals 增长或持平 → 只索引新增的分型（增量）
		for i := st.lastAllFractalsLen; i < len(allFractals); i++ {
			f := allFractals[i]
			if f.Confirmed && f.OpenTime > 0 {
				st.confirmedFractalMap[f.OpenTime] = true
			}
		}
	}
	st.lastAllFractalsLen = len(allFractals)

	// 检查所有待处理的分型中是否有新确认的
	// 优先用 OpenTime 匹配（O(1) 查表）；OpenTime 为 0 时（单元测试场景）用索引匹配
	newlyConfirmed := false
	for _, pe := range st.pendingFractals {
		if st.processedFractalTS[pe.OpenTime] {
			continue
		}
		matched := false
		if pe.OpenTime > 0 {
			// O(1) 查表：已确认分型的 OpenTime 在 map 中
			matched = st.confirmedFractalMap[pe.OpenTime]
			// 兜底：当 Fractal 的 OpenTime 为 0（单元测试场景）但 ChanKline 有 OpenTime 时，
			// 回退到 index 匹配。此时 allFractals 数据量极小，线性扫描可接受。
			if !matched {
				for _, f := range allFractals {
					if f.Confirmed && f.Index == indexOfElement(pe, nil) {
						matched = true
						break
					}
				}
			}
		} else {
			// OpenTime 未设置时（单元测试）匹配 FractalType + Index
			// 单元测试数据量极小，线性扫描可接受
			for _, f := range allFractals {
				if f.Confirmed && f.Index == indexOfElement(pe, nil) {
					matched = true
					break
				}
			}
		}
		if matched {
			st.processConfirmedFractal(pe)
			st.processedFractalTS[pe.OpenTime] = true
			newlyConfirmed = true
		}
	}

	if newlyConfirmed {
		// 清理已处理的分型记录
		var remaining []*types.ChanKline
		for _, pe := range st.pendingFractals {
			if !st.processedFractalTS[pe.OpenTime] {
				remaining = append(remaining, pe)
			}
		}
		st.pendingFractals = remaining
	}

	return st.strokes
}

// trackModification 记录笔修改的位置（用于回溯修正检测）。
func (s *strokeState) trackModification(strokeIdx int) {
	if s.modifiedStrokeIdx < 0 || strokeIdx < s.modifiedStrokeIdx {
		s.modifiedStrokeIdx = strokeIdx
	}
}

// CheckModifications 检查本轮是否有笔被修改或删除。
// 返回最早变更的笔索引，-1 表示无变更。
// 调用后重置状态，供下一轮使用。
func (s *strokeState) CheckModifications() (firstModifiedIdx int, hadRemoval bool) {
	idx := s.modifiedStrokeIdx
	removed := s.strokeRemoved
	removedIdx := s.strokeRemovedIdx

	// 重置
	s.modifiedStrokeIdx = -1
	s.strokeRemoved = false
	s.strokeRemovedIdx = -1

	if removed && (idx < 0 || removedIdx < idx) {
		idx = removedIdx
	}
	return idx, removed
}

// CheckModifications 检查指定 symbol 的笔修改状态（PRD §10.4.3 回溯修正）。
func (bp *StrokeProcessor) CheckModifications(symbol string) (int, bool) {
	st := bp.getOrCreateState(symbol)
	return st.CheckModifications()
}

// Strokes returns all confirmed strokes (笔).
func (bp *StrokeProcessor) Strokes(symbol string) []*stroke {
	st := bp.getOrCreateState(symbol)
	var result []*stroke
	for _, b := range st.strokes {
		if b.Confirmed && !b.Virtual {
			result = append(result, b)
		}
	}
	return result
}

// getOrCreateState 获取或创建指定 symbol 的状态机。
func (bp *StrokeProcessor) getOrCreateState(symbol string) *strokeState {
	if s, ok := bp.states[symbol]; ok {
		return s
	}
	s := &strokeState{
		strokes:             make([]*stroke, 0),
		candidates:          make([]*types.ChanKline, 0),
		pendingFractals:     make([]*types.ChanKline, 0),
		processedFractalTS:  make(map[int64]bool),
		confirmedFractalMap: make(map[int64]bool),
		cfg:                 types.DefaultStrokeConfig(),
		modifiedStrokeIdx:   -1,
		strokeRemovedIdx:    -1,
	}
	bp.states[symbol] = s
	return s
}

// Reset 重置指定 symbol 的状态。
func (bp *StrokeProcessor) Reset(symbol string) {
	delete(bp.states, symbol)
}

// processConfirmedFractal 处理一个已确认的分型（笔.md §3.2）。
func (s *strokeState) processConfirmedFractal(elem *types.ChanKline) {
	// 先清理上一轮的虚笔（§10.1）
	s.clearVirtualStroke()

	// 若笔列表为空，尝试创建第一笔（§4）
	if len(s.strokes) == 0 {
		s.handleFirstStroke(elem)
		return
	}

	lastStroke := s.strokes[len(s.strokes)-1]

	// 若新分型与最后终点类型相同（同为顶或同为底），尝试更新终点（§8）
	if elem.FractalType == s.lastEndpoint.FractalType {
		s.tryUpdateEndpoint(elem, lastStroke)
		return
	}

	// 异类分型，尝试成笔（§5）
	if s.canFormStroke(s.lastEndpoint, elem) {
		newStroke := s.createConfirmedStroke(s.lastEndpoint, elem)
		s.strokes = append(s.strokes, newStroke)
		s.lastEndpoint = elem
	} else {
		// 不能成笔，尝试更新候选
		s.candidates = append(s.candidates, elem)
	}
}

// handleFirstStroke 处理第一笔生成（笔.md §4）。
func (s *strokeState) handleFirstStroke(elem *types.ChanKline) {
	// 缓存候选分型
	s.candidates = append(s.candidates, elem)
	s.lastEndpoint = elem

	// 检查是否能在缓存中找到异类分型成笔
	for i := 0; i < len(s.candidates)-1; i++ {
		start := s.candidates[i]
		if start.FractalType != elem.FractalType &&
			s.canFormStroke(start, elem) {
			newStroke := s.createConfirmedStroke(start, elem)
			s.strokes = append(s.strokes, newStroke)
			s.lastEndpoint = elem
			// 清空候选，保留当前终点
			s.candidates = nil
			return
		}
	}
}

// canFormStroke 判断两个异类分型是否能成笔（笔.md §5）。
func (s *strokeState) canFormStroke(start, end *types.ChanKline) bool {
	// §5.1: 基础条件 - 必须是异类分型
	if start.FractalType == end.FractalType {
		return false
	}
	if start.FractalType == types.FractalNone || end.FractalType == types.FractalNone {
		return false
	}

	// §5.2: 跨度检查
	if !s.checkSpan(start, end) {
		return false
	}

	// §5.3: 价格区间检查
	if !s.checkPriceRange(start, end) {
		return false
	}

	// §5.4: 分型有效性检查（缠论原文无此定义，通过）
	// §5.5: 终点峰值检查（缠论原文无此定义，通过）

	return true
}

// checkSpan 检查笔的跨度（笔.md §5.2）。
func (s *strokeState) checkSpan(start, end *types.ChanKline) bool {
	// 计算合并K线跨度
	span := elementIndexDiff(start, end)
	if span < 0 {
		span = -span
	}

	if s.cfg.Strict {
		// 严格模式：合并K线跨度 ≥ 4
		if span < 4 {
			return false
		}
	} else {
		// 非严格模式：合并K线跨度 ≥ 3
		if span < 3 {
			return false
		}
		// 中间包含的原始K线数量至少为 3
		totalMerged := totalMergedFrom(start, end)
		if totalMerged < 3 {
			return false
		}
	}

	return true
}

// checkPriceRange 检查价格区间（笔.md §5.3）。
func (s *strokeState) checkPriceRange(start, end *types.ChanKline) bool {
	if start.FractalType == types.FractalBottom {
		// 上升笔：终点的最高价必须高于起点的最低价
		return end.High > start.Low
	}
	// 下降笔：终点的最低价必须低于起点的最高价
	return end.Low < start.High
}

// tryUpdateEndpoint 同类分型更新终点（笔.md §8）。
// 返回 true 表示更新成功。
func (s *strokeState) tryUpdateEndpoint(newFractal *types.ChanKline, lastStroke *stroke) bool {
	if lastStroke.Direction == types.DirectionUp {
		// 向上笔，新分型必须是顶分型且高点不低于当前终点高点
		if newFractal.FractalType == types.FractalTop && newFractal.High >= lastStroke.End.High {
			lastStroke.End = newFractal
			lastStroke.EndPrice = newFractal.High
			lastStroke.High = math.Max(lastStroke.High, newFractal.High)
			s.lastEndpoint = newFractal
			s.trackModification(lastStroke.Index)
			return true
		}
	} else {
		// 向下笔，新分型必须是底分型且低点不高于当前终点低点
		if newFractal.FractalType == types.FractalBottom && newFractal.Low <= lastStroke.End.Low {
			lastStroke.End = newFractal
			lastStroke.EndPrice = newFractal.Low
			lastStroke.Low = math.Min(lastStroke.Low, newFractal.Low)
			s.lastEndpoint = newFractal
			s.trackModification(lastStroke.Index)
			return true
		}
	}
	return false
}

// clearVirtualStroke 清理或恢复虚笔（笔.md §10.1）。
func (s *strokeState) clearVirtualStroke() {
	if len(s.strokes) == 0 {
		return
	}
	lastStroke := s.strokes[len(s.strokes)-1]
	if !lastStroke.Virtual {
		return
	}

	if len(lastStroke.PreviousEnds) > 0 {
		// 恢复到最后一条确认终点
		lastStroke.End = lastStroke.PreviousEnds[len(lastStroke.PreviousEnds)-1]
		lastStroke.PreviousEnds = lastStroke.PreviousEnds[:len(lastStroke.PreviousEnds)-1]
		lastStroke.Virtual = false
		lastStroke.Confirmed = true
	} else {
		// 无确认终点记录，直接删除
		s.strokes = s.strokes[:len(s.strokes)-1]
	}
}

// createConfirmedStroke 创建一条确认笔。
func (s *strokeState) createConfirmedStroke(start, end *types.ChanKline) *stroke {
	var dir types.ChanDirection
	var startPrice, endPrice, high, low float64

	if start.FractalType == types.FractalBottom {
		dir = types.DirectionUp
		startPrice = start.Low
		endPrice = end.High
	} else {
		dir = types.DirectionDown
		startPrice = start.High
		endPrice = end.Low
	}

	high = math.Max(startPrice, endPrice)
	low = math.Min(startPrice, endPrice)

	// 扫描区间内实际极值
	curr := start.NextElement
	for curr != nil && curr != end.NextElement {
		if curr.High > high {
			high = curr.High
		}
		if curr.Low < low {
			low = curr.Low
		}
		if curr == end {
			break
		}
		curr = curr.NextElement
	}

	newStroke := &stroke{
		Index:      len(s.strokes),
		Start:      start,
		End:        end,
		Direction:  dir,
		Confirmed:  true,
		StartPrice: startPrice,
		EndPrice:   endPrice,
		High:       high,
		Low:        low,
	}
	return newStroke
}

// createVirtualStroke 创建一条虚笔（笔.md §7/§10.2）。
// 复用确认笔的方向与极值计算逻辑，差异仅 Virtual=true。
// 虚笔表示尚未确认的实时笔，被 Strokes() 的 !Virtual 过滤，不外泄到下游。
func (s *strokeState) createVirtualStroke(start, end *types.ChanKline) *stroke {
	vs := s.createConfirmedStroke(start, end)
	vs.Virtual = true
	return vs
}

// tryVirtualStroke 尝试基于一个"已识别但尚未确认"的分型创建或更新虚笔（笔.md §10.2）。
//
// 分两种情况：
//  1. 最后一笔是虚笔：新分型与虚笔终点同类（沿虚笔方向）→ 延伸虚笔终点（记录 PreviousEnds）；
//     新分型与虚笔终点异类（虚笔方向反转）→ 清理虚笔，回到情况 2 处理。
//  2. 最后一笔不是虚笔：新分型与 lastEndpoint（确认终点）异类且满足成笔条件 → 创建新虚笔。
//
// 虚笔不改变 lastEndpoint（确认终点状态），确保分型确认后能正确清理/转换。
func (s *strokeState) tryVirtualStroke(elem *types.ChanKline) {
	if s.lastEndpoint == nil {
		return
	}

	// 情况1：最后一笔是虚笔 → 用虚笔终点判断同类/异类。
	if len(s.strokes) > 0 {
		last := s.strokes[len(s.strokes)-1]
		if last.Virtual {
			if elem.FractalType == last.End.FractalType {
				// 同类（沿虚笔方向）→ 延伸虚笔终点。
				s.tryUpdateVirtualEndpoint(elem)
				return
			}
			// 异类（虚笔方向反转）→ 清理虚笔，落到情况2基于 lastEndpoint 判断。
			s.clearVirtualStroke()
		}
	}

	// 情况2：基于 lastEndpoint（确认终点）创建新虚笔。
	// 仅异类分型才可能成笔；同类分型对确认终点无意义（确认笔的同类更新由 processConfirmedFractal 处理）。
	if elem.FractalType == s.lastEndpoint.FractalType {
		return
	}
	if !s.canFormStroke(s.lastEndpoint, elem) {
		return
	}

	// 清理后若最后一笔仍是虚笔（理论上不应发生），放弃本次创建。
	if len(s.strokes) > 0 && s.strokes[len(s.strokes)-1].Virtual {
		return
	}

	vs := s.createVirtualStroke(s.lastEndpoint, elem)
	s.strokes = append(s.strokes, vs)
	// 不更新 lastEndpoint：虚笔是预测性的，确认终点保持不变。
}

// tryUpdateVirtualEndpoint 同类分型更新虚笔的虚拟终点（笔.md §10.2 情况一）。
// 仅当最后一笔是虚笔、且新分型沿虚笔方向创新高/低时，更新虚拟终点并记录 PreviousEnds。
func (s *strokeState) tryUpdateVirtualEndpoint(elem *types.ChanKline) {
	if len(s.strokes) == 0 {
		return
	}
	last := s.strokes[len(s.strokes)-1]
	if !last.Virtual {
		return
	}

	if last.Direction == types.DirectionUp {
		// 向上虚笔：新顶分型且高点更高才延伸。
		if elem.FractalType == types.FractalTop && elem.High > last.End.High {
			last.PreviousEnds = append(last.PreviousEnds, last.End)
			last.End = elem
			last.EndPrice = elem.High
			if elem.High > last.High {
				last.High = elem.High
			}
		}
	} else {
		// 向下虚笔：新底分型且低点更低才延伸。
		if elem.FractalType == types.FractalBottom && elem.Low < last.End.Low {
			last.PreviousEnds = append(last.PreviousEnds, last.End)
			last.End = elem
			last.EndPrice = elem.Low
			if elem.Low < last.Low {
				last.Low = elem.Low
			}
		}
	}
}

// Reset 重置笔状态。
func (s *strokeState) Reset() {
	s.strokes = make([]*stroke, 0)
	s.lastEndpoint = nil
	s.candidates = make([]*types.ChanKline, 0)
}

// ====== 辅助函数 ======

// elementIndexDiff 计算两个 ChanKline 之间在非包含序列中的索引差。
func elementIndexDiff(a, b *types.ChanKline) int {
	count := 0
	curr := a
	for curr != nil && curr != b {
		curr = curr.NextElement
		count++
	}
	if curr == b {
		return count
	}
	// 反向搜索
	count = 0
	curr = a
	for curr != nil && curr != b {
		curr = curr.PrevElement
		count++
	}
	if curr == b {
		return -count
	}
	return 0 // 不应发生
}

// indexOfElement 返回元素在链表中的索引（从 start 开始搜索, start 为 nil 则从头搜索）。
func indexOfElement(elem *types.ChanKline, start *types.ChanKline) int {
	idx := 0
	curr := start
	if curr == nil {
		// 从最前开始
		curr = elem
		for curr.PrevElement != nil {
			curr = curr.PrevElement
		}
	}
	for curr != nil {
		if curr == elem {
			return idx
		}
		curr = curr.NextElement
		idx++
	}
	return -1
}

// totalMergedFrom 计算 start 到 end 之间（含两端）合并的原始 K 线总数。
func totalMergedFrom(start, end *types.ChanKline) int {
	total := 0
	curr := start
	for curr != nil {
		total += curr.MergedFrom
		if curr == end {
			break
		}
		curr = curr.NextElement
	}
	return total
}
