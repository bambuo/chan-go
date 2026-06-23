package chanlun

import (
	"trade/internal/types"
)

// FractalProcessor 从非包含K线序列中识别缠论分型。
//
// 顶分型由连续三根非包含K线组成，其中中间元素的最高点最高。
// 底分型由连续三根非包含K线组成，其中中间元素的最低点最低。
//
// 分型确认条件：中间元素之后至少存在一个后续非包含元素。
// （简化实现：仅检查后续元素是否存在，不验证"后续元素不否定该分型"。）
type FractalProcessor struct {
	elements []*types.ChanKline
	fractals []types.Fractal
}

// NewFractalProcessor 创建新的分型处理器。
func NewFractalProcessor() *FractalProcessor {
	return &FractalProcessor{
		elements: make([]*types.ChanKline, 0),
		fractals: make([]types.Fractal, 0),
	}
}

// Process 接收一个非包含K线元素并更新分型分析。
//
// 注意：当包含处理器发生合并时，同一元素指针可能被再次传入（值已更新）。
// 这种情况下不重复添加，但重新扫描分型以确保合并后的值被正确评估。
//
// 返回当前已识别的分型列表。
func (p *FractalProcessor) Process(elem *types.ChanKline) []types.Fractal {
	// 检测重复：相同的指针再次传入（包含合并导致的值更新）。
	if len(p.elements) > 0 && p.elements[len(p.elements)-1] == elem {
		// 值可能已变化，重新扫描最后3个元素。
		if len(p.elements) >= 3 {
			p.scanLast3()
		}
		return p.fractals
	}
	p.elements = append(p.elements, elem)
	if len(p.elements) >= 3 {
		p.scanLast3()
	}
	return p.fractals
}

// ProcessBatch 一次性处理一个非包含元素切片。
// 每个元素触发增量扫描，从而捕获中间分型。
func (p *FractalProcessor) ProcessBatch(elems []*types.ChanKline) []types.Fractal {
	for _, e := range elems {
		// 跳过已被包含的元素。
		if e.Contained {
			continue
		}
		// 与 Process 相同：跳过重复指针。
		if len(p.elements) > 0 && p.elements[len(p.elements)-1] == e {
			continue
		}
		p.elements = append(p.elements, e)
		if len(p.elements) >= 3 {
			p.scanLast3()
		}
	}
	return p.fractals
}

// Fractals 返回当前已识别且已确认的分型列表。
func (p *FractalProcessor) Fractals() []types.Fractal {
	result := make([]types.Fractal, 0, len(p.fractals))
	for _, f := range p.fractals {
		if f.Confirmed {
			result = append(result, f)
		}
	}
	return result
}

// AllFractals 返回所有已识别的分型，包括未确认的。
func (p *FractalProcessor) AllFractals() []types.Fractal {
	result := make([]types.Fractal, 0, len(p.fractals))
	result = append(result, p.fractals...)
	return result
}

// Reset 清除所有状态。
func (p *FractalProcessor) Reset() {
	p.elements = make([]*types.ChanKline, 0)
	p.fractals = make([]types.Fractal, 0)
}

// scanLast3 检查最近3个元素中的新分型并更新确认状态。
func (p *FractalProcessor) scanLast3() {
	if len(p.elements) < 3 {
		return
	}

	n := len(p.elements)

	// 检查最后3个元素是否有新分型。
	last := p.elements[n-1]
	mid := p.elements[n-2]
	first := p.elements[n-3]

	// 检查顶分型：中间元素高点最高。
	if mid.High > first.High && mid.High > last.High {
		// 确保不重复注册同一分型。
		if len(p.fractals) == 0 || p.fractals[len(p.fractals)-1].Index != n-2 {
			mid.FractalType = types.FractalTop
			p.fractals = append(p.fractals, types.Fractal{
				Type:     types.FractalTop,
				Index:    n - 2,
				High:     mid.High,
				Low:      mid.Low,
				OpenTime: mid.OpenTime,
			})
		}
	}

	// 检查底分型：中间元素低点最低。
	if mid.Low < first.Low && mid.Low < last.Low {
		if len(p.fractals) == 0 || p.fractals[len(p.fractals)-1].Index != n-2 {
			mid.FractalType = types.FractalBottom
			p.fractals = append(p.fractals, types.Fractal{
				Type:     types.FractalBottom,
				Index:    n - 2,
				High:     mid.High,
				Low:      mid.Low,
				OpenTime: mid.OpenTime,
			})
		}
	}

	// 更新确认状态。
	// 分型在至少一个后续元素出现后被确认。
	p.updateConfirmations(n)
}

// updateConfirmations 当后续元素存在时将分型标记为已确认。
func (p *FractalProcessor) updateConfirmations(n int) {
	for i := range p.fractals {
		if p.fractals[i].Confirmed {
			continue
		}
		// 非包含序列中索引为`index`的分型，当其之后存在元素时被确认。
		if p.fractals[i].Index+2 < n {
			p.fractals[i].Confirmed = true
		}
	}
}

// isTopFractal 检查三个元素是否形成顶分型。
func isTopFractal(first, mid, last *types.ChanKline) bool {
	return mid.High > first.High && mid.High > last.High
}

// IsTopFractal 公开版本，用于测试。
func IsTopFractal(first, mid, last *types.ChanKline) bool {
	return isTopFractal(first, mid, last)
}

// isBottomFractal 检查三个元素是否形成底分型。
func isBottomFractal(first, mid, last *types.ChanKline) bool {
	return mid.Low < first.Low && mid.Low < last.Low
}

// IsBottomFractal 公开版本，用于测试。
func IsBottomFractal(first, mid, last *types.ChanKline) bool {
	return isBottomFractal(first, mid, last)
}
