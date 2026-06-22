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
// 算法（依据缠论）：
//  1. 确定前两根非包含K线的方向：
//     - 向上：第二根高点>第一根高点 且 第二根低点>第一根低点
//     - 向下：第二根高点<第一根高点 且 第二根低点<第一根低点
//  2. 对于已确立方向的序列：
//     - 如果当前K线被前一根包含，则合并：
//     - 向上方向：取较高高点和较高低点
//     - 向下方向：取较低高点和较低低点
//  3. 当出现非包含K线时，由最近两根非包含元素重新确定方向。
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
	return p.resolve()
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

// resolve 执行增量包含处理。
// 算法：
//  1. 获取非包含序列。
//  2. 对每个新增元素（最后添加的原始元素），从后往前处理：
//     a. 如果元素已包含，跳过。
//     b. 找到它前面的最后一个非包含元素。
//     c. 从最近两个非包含元素确定当前方向。
//     d. 检查包含关系。如果被包含，则合并。
func (p *ContainProcessor) resolve() []*types.ChanKline {
	if len(p.elements) < 2 {
		return p.nonContained()
	}

	n := len(p.elements)

	// 从最新元素开始向后处理，支持级联包含。
	for i := n - 1; i >= 1; i-- {
		curr := p.elements[i]
		if curr.Contained {
			continue
		}

		// 查找curr之前的最近两个非包含元素。
		last, secondLast := p.lastTwoNonContainedBefore(i)
		if last == nil {
			continue
		}

		// 确定当前方向。
		dir := types.DirectionNone
		if secondLast != nil {
			dir = p.determineDirection(secondLast, last)
		} else {
			// curr前只有一个非包含元素。
			// 检查该元素与curr之间的方向。
			dir = p.determineDirection(last, curr)
		}

		// 检查包含关系：curr是否被last包含？
		if isContained(curr, last) {
			p.merge(last, curr, dir)
		}
	}

	return p.nonContained()
}

// lastTwoNonContainedBefore 返回给定索引之前的最近两个非包含元素。
// 第一个返回值是最接近的一个。
func (p *ContainProcessor) lastTwoNonContainedBefore(i int) (last, secondLast *types.ChanKline) {
	for j := i - 1; j >= 0; j-- {
		if !p.elements[j].Contained {
			if last == nil {
				last = p.elements[j]
			} else if secondLast == nil {
				secondLast = p.elements[j]
				return
			}
		}
	}
	return last, nil
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
// 合并后，当前元素被标记为已包含。
func (p *ContainProcessor) merge(target, curr *types.ChanKline, dir types.ChanDirection) {
	if dir == types.DirectionUp {
		// 取较高高点和较高低点。
		if curr.High > target.High {
			target.High = curr.High
		}
		if curr.Low > target.Low {
			target.Low = curr.Low
		}
	} else {
		// 取较低高点和较低低点。
		if curr.High < target.High {
			target.High = curr.High
		}
		if curr.Low < target.Low {
			target.Low = curr.Low
		}
	}

	curr.Contained = true
	target.MergedFrom += curr.MergedFrom
}

// isContained 检查元素curr是否被元素prev包含。
// 包含含义：curr的高点≤prev的高点 且 curr的低点≥prev的低点，
// 或 curr的高点≥prev的高点 且 curr的低点≤prev的低点。
func isContained(curr, prev *types.ChanKline) bool {
	return (curr.High <= prev.High && curr.Low >= prev.Low) ||
		(curr.High >= prev.High && curr.Low <= prev.Low)
}

// IsContained 是isContained的公开包装，方便测试。
func IsContained(curr, prev *types.ChanKline) bool {
	return isContained(curr, prev)
}
