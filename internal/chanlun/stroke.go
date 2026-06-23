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
// symbol: 交易对名称
// allFractals: 当前所有已确认分型。
// 返回变化后的笔列表。
//
// 每次调用时，StrokeProcessor 检查 allFractals 中是否有新的已确认分型
// 需要触发笔状态机。不再依赖逐元素传入——因为分型确认状态在
// FractalProcessor 中是延迟设置的（需等待后续元素），通过元素传入会错过确认事件。
func (bp *StrokeProcessor) Process(symbol string, elem *types.ChanKline, allFractals []types.Fractal) []*stroke {
	st := bp.getOrCreateState(symbol)

	// 如果当前元素有有效分型且尚未处理，记录它
	if elem.FractalType != types.FractalNone && !st.processedFractalTS[elem.OpenTime] {
		st.pendingFractals = append(st.pendingFractals, elem)
	}

	// 检查所有待处理的分型中是否有新确认的
	// 优先用 OpenTime 匹配；OpenTime 为 0 时（单元测试场景）用索引匹配
	newlyConfirmed := false
	for _, pe := range st.pendingFractals {
		if st.processedFractalTS[pe.OpenTime] {
			continue
		}
		for _, f := range allFractals {
			matched := false
			if f.OpenTime > 0 && pe.OpenTime > 0 {
				matched = f.Confirmed && f.OpenTime == pe.OpenTime
			} else {
				// OpenTime 未设置时（单元测试）匹配 FractalType + Index
				matched = f.Confirmed && f.Index == indexOfElement(pe, nil)
			}
			if matched {
				st.processConfirmedFractal(pe)
				st.processedFractalTS[pe.OpenTime] = true
				newlyConfirmed = true
				break
			}
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
		strokes:            make([]*stroke, 0),
		candidates:         make([]*types.ChanKline, 0),
		pendingFractals:    make([]*types.ChanKline, 0),
		processedFractalTS: make(map[int64]bool),
		cfg:                types.DefaultStrokeConfig(),
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
		if s.tryUpdateEndpoint(elem, lastStroke) {
			// 尝试次高/次低修正（§9）
			s.trySubPeakCorrection(elem)
		}
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

	// §5.4: 分型有效性检查（默认"半"）
	if !s.checkFractalValidity(start, end) {
		return false
	}

	// §5.5: 终点峰值检查
	if s.cfg.PeakEndPoint && !s.checkPeakEndPoint(start, end) {
		return false
	}

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

	// 缺口计为K线（已通过跨度计入）
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

// checkFractalValidity 检查分型有效性（笔.md §6，默认"半"方法）。
func (s *strokeState) checkFractalValidity(start, end *types.ChanKline) bool {
	switch s.cfg.FractalCheck {
	case "loose":
		return s.checkFractalLoose(start, end)
	case "half":
		return s.checkFractalHalf(start, end)
	case "strict":
		return s.checkFractalStrict(start, end)
	case "full":
		return s.checkFractalFull(start, end)
	default:
		return s.checkFractalHalf(start, end)
	}
}

// checkFractalLoose 宽松检查（笔.md §6.1/6.2）。
func (s *strokeState) checkFractalLoose(start, end *types.ChanKline) bool {
	if start.FractalType == types.FractalTop {
		// 顶分型起点：终点低点 > 起点高点
		return end.Low > start.High
	}
	// 底分型起点：终点高点 > 起点高点 且 终点低点 < 起点低点
	return end.High > start.High && end.Low < start.Low
}

// checkFractalHalf "半"检查（笔.md §6.1/6.2，默认）。
func (s *strokeState) checkFractalHalf(start, end *types.ChanKline) bool {
	if start.FractalType == types.FractalTop {
		// 顶分型起点：endHigh = max(end.Prev.High, end.High), startLow = min(start.Low, start.Next.Low)
		endHigh := end.High
		if end.PrevElement != nil && end.PrevElement.High > endHigh {
			endHigh = end.PrevElement.High
		}
		startLow := start.Low
		if start.NextElement != nil && start.NextElement.Low < startLow {
			startLow = start.NextElement.Low
		}
		return start.High > endHigh && end.Low < startLow
	}
	// 底分型起点：endLow = min(end.Prev.Low, end.Low), startHigh = max(start.High, start.Next.High)
	endLow := end.Low
	if end.PrevElement != nil && end.PrevElement.Low < endLow {
		endLow = end.PrevElement.Low
	}
	startHigh := start.High
	if start.NextElement != nil && start.NextElement.High > startHigh {
		startHigh = start.NextElement.High
	}
	return start.Low < endLow && end.High > startHigh
}

// checkFractalStrict 严格检查（笔.md §6.1/6.2）。
func (s *strokeState) checkFractalStrict(start, end *types.ChanKline) bool {
	if start.FractalType == types.FractalTop {
		endHigh := end.High
		if end.PrevElement != nil && end.PrevElement.High > endHigh {
			endHigh = end.PrevElement.High
		}
		if end.NextElement != nil && end.NextElement.High > endHigh {
			endHigh = end.NextElement.High
		}
		startLow := start.Low
		if start.PrevElement != nil && start.PrevElement.Low < startLow {
			startLow = start.PrevElement.Low
		}
		if start.NextElement != nil && start.NextElement.Low < startLow {
			startLow = start.NextElement.Low
		}
		return start.High > endHigh && end.Low < startLow
	}
	endLow := end.Low
	if end.PrevElement != nil && end.PrevElement.Low < endLow {
		endLow = end.PrevElement.Low
	}
	if end.NextElement != nil && end.NextElement.Low < endLow {
		endLow = end.NextElement.Low
	}
	startHigh := start.High
	if start.PrevElement != nil && start.PrevElement.High > startHigh {
		startHigh = start.PrevElement.High
	}
	if start.NextElement != nil && start.NextElement.High > startHigh {
		startHigh = start.NextElement.High
	}
	return start.Low < endLow && end.High > startHigh
}

// checkFractalFull 完全分离检查（笔.md §6.1/6.2）。
func (s *strokeState) checkFractalFull(start, end *types.ChanKline) bool {
	if start.FractalType == types.FractalTop {
		return start.Low > end.High
	}
	return start.High < end.Low
}

// checkPeakEndPoint 终点峰值检查（笔.md §7）。
func (s *strokeState) checkPeakEndPoint(start, end *types.ChanKline) bool {
	if start.FractalType == types.FractalBottom {
		// 上升笔：终点必须是区间内最高价
		curr := start.NextElement
		for curr != nil && curr != end {
			if curr.High > end.High {
				return false
			}
			curr = curr.NextElement
		}
		return true
	}
	// 下降笔：终点必须是区间内最低价
	curr := start.NextElement
	for curr != nil && curr != end {
		if curr.Low < end.Low {
			return false
		}
		curr = curr.NextElement
	}
	return true
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
			return true
		}
	} else {
		// 向下笔，新分型必须是底分型且低点不高于当前终点低点
		if newFractal.FractalType == types.FractalBottom && newFractal.Low <= lastStroke.End.Low {
			lastStroke.End = newFractal
			lastStroke.EndPrice = newFractal.Low
			lastStroke.Low = math.Min(lastStroke.Low, newFractal.Low)
			s.lastEndpoint = newFractal
			return true
		}
	}
	return false
}

// trySubPeakCorrection 次高/次低修正（笔.md §9）。
func (s *strokeState) trySubPeakCorrection(elem *types.ChanKline) {
	if s.cfg.AllowSubPeak {
		return
	}
	if len(s.strokes) < 2 {
		return
	}

	lastStroke := s.strokes[len(s.strokes)-1]
	prevStroke := s.strokes[len(s.strokes)-2]

	// 条件 2：待处理的分型与上一笔的方向匹配
	if lastStroke.Direction == types.DirectionUp && elem.FractalType == types.FractalBottom {
		return // 向上笔不处理低于起点的低点
	}
	if lastStroke.Direction == types.DirectionDown && elem.FractalType == types.FractalTop {
		return // 向下笔不处理高于起点的高点
	}

	// 条件 3：待处理分型与前一笔起点相比是有效峰值
	if lastStroke.Direction == types.DirectionUp {
		if elem.High <= prevStroke.Start.High {
			return
		}
	} else {
		if elem.Low >= prevStroke.Start.Low {
			return
		}
	}

	// 条件 4：上一笔的终点值没有跨过前一笔的起点值
	if lastStroke.Direction == types.DirectionUp && lastStroke.End.Low <= prevStroke.Start.Low {
		return
	}
	if lastStroke.Direction == types.DirectionDown && lastStroke.End.High >= prevStroke.Start.High {
		return
	}

	// 执行修正：移除最后一笔，尝试更新新的最后一笔的终点
	removed := s.strokes[len(s.strokes)-1]
	s.strokes = s.strokes[:len(s.strokes)-1]

	if len(s.strokes) > 0 {
		newLast := s.strokes[len(s.strokes)-1]
		if s.tryUpdateEndpoint(elem, newLast) {
			return // 修正成功
		}
	}

	// 修正失败，恢复被移除的笔
	s.strokes = append(s.strokes, removed)
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
