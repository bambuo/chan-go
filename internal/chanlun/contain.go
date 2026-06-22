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
//  6. 级联包含在后续 Process 调用中自然处理，无需回溯。
type ContainProcessor struct {
	// 所有元素，包括被标记为包含（已合并）的元素。
	elements []*types.ChanKline
}

// NewContainProcessor 创建新的包含处理器。
func NewContainProcessor() *ContainProcessor {
	return &ContainProcessor{
		elements: make([]*types.ChanKline, 0),
	}
}

// Process 处理一根原始K线并将其纳入非包含序列。
// 返回更新后的非包含元素列表。
func (p *ContainProcessor) Process(raw *types.Kline) []*types.ChanKline {
	ck := &types.ChanKline{
		High:       raw.High.InexactFloat64(),
		Low:        raw.Low.InexactFloat64(),
		RawHigh:    raw.High.InexactFloat64(),
		RawLow:     raw.Low.InexactFloat64(),
		OpenTime:   raw.OpenTime,
		CloseTime:  raw.CloseTime,
		Direction:  types.DirectionNone,
		Contained:  false,
		MergedFrom: 1,
	}

	p.elements = append(p.elements, ck)
	p.resolve()
	return p.nonContained()
}

// Reset 清除所有已处理的元素。
func (p *ContainProcessor) Reset() {
	p.elements = make([]*types.ChanKline, 0)
}

// nonContained 返回非包含元素的序列。
func (p *ContainProcessor) nonContained() []*types.ChanKline {
	result := make([]*types.ChanKline, 0, len(p.elements))
	for _, e := range p.elements {
		if !e.Contained {
			result = append(result, e)
		}
	}
	return result
}

// Elements 返回当前的非包含元素序列。
func (p *ContainProcessor) Elements() []*types.ChanKline {
	return p.nonContained()
}

// resolve 标准缠论包含处理（从左到右顺序递进）。
//
// 每次只处理最新添加的元素，检查其与最近一个非包含元素的关系。
// 如果被包含，则确定方向后进行合并；否则不做任何操作。
// 级联包含在后续 Process 调用中自然处理，无需回溯扫描。
func (p *ContainProcessor) resolve() {
	n := len(p.elements)
	if n < 2 {
		return
	}

	// 获取最新添加的元素。
	curr := p.elements[n-1]
	if curr.Contained {
		return
	}

	// 找到前一个非包含元素的索引。
	prevIdx := p.prevNonContainedIndex(n - 1)
	if prevIdx < 0 {
		return
	}
	prev := p.elements[prevIdx]

	// 检查是否被包含。
	if !isContained(curr, prev) {
		return
	}

	// 被包含，确定方向。
	dir := p.resolveDirection(prevIdx)

	// 合并：prev 吸收 curr。
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
		// 前两根K线包含，按向上处理。
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
// 合并后，当前元素被标记为已包含。
func (p *ContainProcessor) merge(target, curr *types.ChanKline, dir types.ChanDirection) {
	if dir == types.DirectionDown {
		// 向下：取较低高点和较低低点。
		if curr.High < target.High {
			target.High = curr.High
		}
		if curr.Low < target.Low {
			target.Low = curr.Low
		}
	} else {
		// DirectionUp / DirectionNone：取较高高点和较高低点。
		if curr.High > target.High {
			target.High = curr.High
		}
		if curr.Low > target.Low {
			target.Low = curr.Low
		}
	}

	curr.Contained = true
	target.MergedFrom += curr.MergedFrom
}

// isContained 检查元素 curr 是否与元素 prev 存在包含关系。
//
// 包含关系定义（对称）：
//   - curr的高点≤prev的高点 且 curr的低点≥prev的低点（curr在prev内部）
//   - 或 curr的高点≥prev的高点 且 curr的低点≤prev的低点（prev在curr内部）
func isContained(curr, prev *types.ChanKline) bool {
	return (curr.High <= prev.High && curr.Low >= prev.Low) ||
		(curr.High >= prev.High && curr.Low <= prev.Low)
}

// IsContained 是 isContained 的公开包装，方便测试。
func IsContained(curr, prev *types.ChanKline) bool {
	return isContained(curr, prev)
}
