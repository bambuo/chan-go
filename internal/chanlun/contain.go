// Package chanlun 实现缠中说禅K线分析算法。
//
// 包含处理是基础步骤：给定原始K线序列，根据当前趋势方向合并"被包含"的K线
// （某K线的高点≤前一根高点且低点≥前一根低点）。
// 结果是用于分型识别的非包含K线序列。
package chanlun

import (
	"trade/internal/types"
)

// ContainProcessor 处理缠论K线包含合并。
//
// 算法（依据缠论原文，从左到右顺序递进）：
//  1. 新K线加入时，与当前非包含序列的最后一个元素比较。
//  2. 检查包含关系，如果不包含则直接作为新元素加入非包含序列。
//  3. 如果被包含，则从最近两根非包含元素确定方向：
//     - 向上：第二根高点>第一根高点 且 第二根低点>第一根低点
//     - 向下：第二根高点<第一根高点 且 第二根低点<第一根低点
//     - 方向不明或仅有一根非包含元素：按向上处理（缠论规定）
//  4. 按方向合并：向上取较高高点和较高低点，向下取较低高点和较低低点。
//  5. 合并后修改前一元素的 High/Low，新元素标记为已包含。
//  6. 后续 Process 调用自然处理更多K线，无需回溯。
//
// 实时更新（流式场景）：
//   - 同一 open_time 的 K 线再次传入时，更新已有元素的值并重新 resolve，
//     不创建新元素。支持实时 K 线数据持续修正 High/Low。
type ContainProcessor struct {
	// 所有元素，包括被标记为包含（已合并）的元素。
	elements []*types.ChanKline

	// 最后一次 merge 的快照，用于实时更新时回退。
	lastMergeTarget     *types.ChanKline
	lastMergeTargetHigh float64
	lastMergeTargetLow  float64

	// 已处理的原始 K 线总数（用于 ChanKline.KlineIdx）。
	totalKlines int
}

// NewContainProcessor 创建新的包含处理器。
func NewContainProcessor() *ContainProcessor {
	return &ContainProcessor{
		elements: make([]*types.ChanKline, 0),
	}
}

// Process 处理一根原始K线并将其纳入非包含序列。
//
// 如果 raw 的 open_time 与最后一个元素相同（实时更新），
// 则更新已有元素的值并重新 resolve，不创建新元素。
// 否则作为新 K 线添加。
//
// 返回更新后的非包含元素列表。
func (p *ContainProcessor) Process(raw *types.Kline) []*types.ChanKline {
	p.totalKlines++

	// 同周期实时更新：不创建新元素，更新已有值。
	if len(p.elements) > 0 && raw.OpenTime == p.elements[len(p.elements)-1].OpenTime {
		return p.updateLast(raw)
	}

		ck := &types.ChanKline{
			High:       raw.High.InexactFloat64(),
			Low:        raw.Low.InexactFloat64(),
			Close:      raw.Close.InexactFloat64(),
			Volume:     raw.BaseVolume.InexactFloat64(),
			KlineIdx:   p.totalKlines - 1,
			RawHigh:    raw.High.InexactFloat64(),
			RawLow:     raw.Low.InexactFloat64(),
			OpenTime:   raw.OpenTime,
		CloseTime:  raw.CloseTime,
		Direction:  types.DirectionNone,
		Contained:  false,
		MergedFrom: 1,
	}

	p.elements = append(p.elements, ck)
	p.totalKlines++
	p.resolveLast()
	return p.nonContained()
}

// updateLast 处理同周期 K 线实时更新。
//
// 策略：
//  1. 更新最后一个元素的 RawHigh/RawLow（只扩展，不收缩）。
//  2. 如果最后一个元素是非包含的，直接更新 High/Low 并重新 resolve。
//  3. 如果最后一个元素是被包含的，先回退 target 的合并状态，
//     然后将当前元素设为非包含，再重新 resolve。
func (p *ContainProcessor) updateLast(raw *types.Kline) []*types.ChanKline {
	last := p.elements[len(p.elements)-1]
	newHigh := raw.High.InexactFloat64()
	newLow := raw.Low.InexactFloat64()

	// 更新原始值（只扩展）。
	if newHigh > last.RawHigh {
		last.RawHigh = newHigh
	}
	if newLow < last.RawLow {
		last.RawLow = newLow
	}

	if !last.Contained {
			// 非包含：直接更新 High/Low，重新 resolve。
			if newHigh > last.High {
				last.High = newHigh
			}
			if newLow < last.Low {
				last.Low = newLow
			}
			last.Close = raw.Close.InexactFloat64()
			last.Volume = raw.BaseVolume.InexactFloat64()
			p.resolveLast()
			return p.nonContained()
		}

	// 被包含：回退 target 的合并状态。
	if p.lastMergeTarget == last {
		// 不应发生：last 是自己 merge 的 target。
		p.resolveLast()
		return p.nonContained()
	}
	if p.lastMergeTarget != nil && p.lastMergeTarget == p.prevNonContainedTarget(len(p.elements)-1) {
		// 回退 target 到合并前的值。
		p.lastMergeTarget.High = p.lastMergeTargetHigh
		p.lastMergeTarget.Low = p.lastMergeTargetLow
		p.lastMergeTarget.MergedFrom -= last.MergedFrom
		p.lastMergeTarget.Volume -= last.Volume
		if p.lastMergeTarget.Volume < 0 {
			p.lastMergeTarget.Volume = 0
		}
	}

	// 将 last 设为非包含，使用当前最新值。
	last.Contained = false
	last.High = newHigh
	last.Low = newLow
	last.Close = raw.Close.InexactFloat64()
	last.Volume = raw.BaseVolume.InexactFloat64()
	last.MergedFrom = 1

	p.resolveLast()
	return p.nonContained()
}

// prevNonContainedTarget 返回 i 之前最近的非包含元素（同 prevNonContainedIndex）。
func (p *ContainProcessor) prevNonContainedTarget(i int) *types.ChanKline {
	j := p.prevNonContainedIndex(i)
	if j < 0 {
		return nil
	}
	return p.elements[j]
}

// Reset 清除所有已处理的元素。
func (p *ContainProcessor) Reset() {
	p.elements = make([]*types.ChanKline, 0)
	p.lastMergeTarget = nil
}

// nonContained 返回非包含元素的序列，并重建 Prev/Next 链表指针。
func (p *ContainProcessor) nonContained() []*types.ChanKline {
	result := make([]*types.ChanKline, 0, len(p.elements))
	for _, e := range p.elements {
		if !e.Contained {
			result = append(result, e)
		}
	}
	// 重建 Prev/Next 链表指针
	for i := 0; i < len(result); i++ {
		if i > 0 {
			result[i].PrevElement = result[i-1]
		} else {
			result[i].PrevElement = nil
		}
		if i < len(result)-1 {
			result[i].NextElement = result[i+1]
		} else {
			result[i].NextElement = nil
		}
	}
	// 更新每个元素的方向
	for i := 1; i < len(result); i++ {
		dir := p.determineDirection(result[i-1], result[i])
		if dir == types.DirectionNone {
			dir = types.DirectionUp
		}
		result[i].Direction = dir
	}
	if len(result) > 0 {
		result[0].Direction = types.DirectionNone
	}
	return result
}

// Elements 返回当前的非包含元素序列。
func (p *ContainProcessor) Elements() []*types.ChanKline {
	return p.nonContained()
}

// resolveLast 检查最后一个元素与前一非包含元素的包含关系。
//
// 每次只处理最新添加的元素，检查其与最近一个非包含元素的关系。
// 如果被包含，则确定方向后进行合并；否则不做任何操作。
func (p *ContainProcessor) resolveLast() {
	n := len(p.elements)
	if n < 2 {
		return
	}

	curr := p.elements[n-1]
	if curr.Contained {
		return
	}

	prevIdx := p.prevNonContainedIndex(n - 1)
	if prevIdx < 0 {
		return
	}
	prev := p.elements[prevIdx]

	if !isContained(curr, prev) {
		return
	}

	dir := p.resolveDirection(prevIdx)
	p.merge(prev, curr, dir)
}

// resolveDirection 确定当前合并方向。
//
// 从非包含序列中取最后两根元素判断方向。
// - 只有一根非包含元素（前两根K线）：按向上处理（缠论规定）。
// - 方向不明时：按向上处理（缠论规定）。
func (p *ContainProcessor) resolveDirection(prevIdx int) types.ChanDirection {
	prevPrevIdx := p.prevNonContainedIndex(prevIdx)
	if prevPrevIdx < 0 {
		return types.DirectionUp
	}
	dir := p.determineDirection(p.elements[prevPrevIdx], p.elements[prevIdx])
	if dir == types.DirectionNone {
		return types.DirectionUp
	}
	return dir
}

// prevNonContainedIndex 返回 i 之前最近的非包含元素的索引，不存在返回 -1。
func (p *ContainProcessor) prevNonContainedIndex(i int) int {
	for j := i - 1; j >= 0; j-- {
		if !p.elements[j].Contained {
			return j
		}
	}
	return -1
}

// determineDirection 计算两个非包含元素之间的方向。
//
// 缠论原文规定：
//   - 向上：High 更高 且 Low 更高（AND）
//   - 向下：High 更低 且 Low 更低（AND）
//   - 方向不明：返回 DirectionNone（调用方按向上处理）
func (p *ContainProcessor) determineDirection(a, b *types.ChanKline) types.ChanDirection {
	if a.High < b.High && a.Low < b.Low {
		return types.DirectionUp
	}
	if a.High > b.High && a.Low > b.Low {
		return types.DirectionDown
	}
	return types.DirectionNone
}

// merge 根据方向将当前元素合并到目标元素中。
//
// 方向不明（DirectionNone）时按向上处理（缠论原文规定）：
//   - 向上或方向不明：取较高高点和较高低点。
//   - 向下：取较低高点和较低低点。
//
// 合并后，当前元素被标记为已包含。同时记录合并快照，
// 以便实时更新时回退。
func (p *ContainProcessor) merge(target, curr *types.ChanKline, dir types.ChanDirection) {
	// 记录快照以供实时更新回退。
	p.lastMergeTarget = target
	p.lastMergeTargetHigh = target.High
	p.lastMergeTargetLow = target.Low

	if dir == types.DirectionDown {
		if curr.High < target.High {
			target.High = curr.High
		}
		if curr.Low < target.Low {
			target.Low = curr.Low
		}
	} else {
		if curr.High > target.High {
			target.High = curr.High
		}
		if curr.Low > target.Low {
			target.Low = curr.Low
		}
	}

	// 收盘价取最新，成交量累加
	target.Close = curr.Close
	target.Volume += curr.Volume

	curr.Contained = true
	target.MergedFrom += curr.MergedFrom
}

// isContained 检查元素 curr 是否与元素 prev 存在包含关系。
func isContained(curr, prev *types.ChanKline) bool {
	return (curr.High <= prev.High && curr.Low >= prev.Low) ||
		(curr.High >= prev.High && curr.Low <= prev.Low)
}

// IsContained 是 isContained 的公开包装，方便测试。
func IsContained(curr, prev *types.ChanKline) bool {
	return isContained(curr, prev)
}
