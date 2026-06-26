package chanlun

// ──────────────────────────────────────────────
// 分型识别（流式）
// 使用 RingBuffer 存储最近分型用于去重，自动淘汰旧记录
// ──────────────────────────────────────────────

// fractalKey 用于分型去重。
type fractalKey struct {
	ftype FractalType
	index int64
}

// FractalProcessor 是分型识别处理器。
// 流式扫描最后三个元素，检查中间元素是否为分型。
// RingBuffer 容量 4（去重回溯最多 1 个位置 × 2 种分型）。
type FractalProcessor struct {
	seen *Ring[*fractalKey] // 容量 4，防重复
}

// NewFractalProcessor 创建一个分型识别处理器。
func NewFractalProcessor() *FractalProcessor {
	return &FractalProcessor{
		seen: NewRing[*fractalKey](4),
	}
}

// Scan 扫描最后三个合并 K 线，返回新发现的分型。
// baseOffset 用于修正绝对索引。
func (p *FractalProcessor) Scan(elements []ChanKLine, baseOffset int64) []Fractal {
	var result []Fractal
	if len(elements) < 3 {
		return result
	}
	count := len(elements)
	mid := elements[count-2]
	midIdx := int64(count-2) + baseOffset // 绝对索引

	// 顶分型：中间元素高点比两边都高，低点比两边都高
	if mid.IsAbove(&elements[count-3]) && mid.IsAbove(&elements[count-1]) {
		if !p.hasSeen(FractalTypeTop, midIdx) {
			p.recordSeen(FractalTypeTop, midIdx)
			m := &ChanKLine{
				Time:  mid.Time,
				High:  mid.High,
				Low:   mid.Low,
				Index: midIdx,
			}
			result = append(result, Fractal{FType: FractalTypeTop, KLine: m, Index: midIdx})
		}
	}

	// 底分型：中间元素低点比两边都低，高点比两边都低
	if mid.IsBelow(&elements[count-3]) && mid.IsBelow(&elements[count-1]) {
		if !p.hasSeen(FractalTypeBottom, midIdx) {
			p.recordSeen(FractalTypeBottom, midIdx)
			m := &ChanKLine{
				Time:  mid.Time,
				High:  mid.High,
				Low:   mid.Low,
				Index: midIdx,
			}
			result = append(result, Fractal{FType: FractalTypeBottom, KLine: m, Index: midIdx})
		}
	}

	return result
}

// Reset 清空处理器状态。
func (p *FractalProcessor) Reset() {
	p.seen.Clear()
}

// ── 内部 ──

func (p *FractalProcessor) hasSeen(ftype FractalType, index int64) bool {
	all := p.seen.ToSlice()
	for _, k := range all {
		if k.ftype == ftype && k.index == index {
			return true
		}
	}
	return false
}

func (p *FractalProcessor) recordSeen(ftype FractalType, index int64) {
	p.seen.Append(&fractalKey{ftype: ftype, index: index})
}
