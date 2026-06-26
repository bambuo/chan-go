package chanlun

// ──────────────────────────────────────────────
// 笔识别（流式）
// 使用 RingBuffer 存储最近 1 根确认笔做状态延续
// ──────────────────────────────────────────────

// strokeCandidate 是笔候选端点。
type strokeCandidate struct {
	kline *ChanKLine
	index int64
	ftype FractalType
}

// StrokeProcessor 是笔识别处理器。
// 流式处理：输入分型，输出新形成的确认笔。
type StrokeProcessor struct {
	strokes           *Ring[*AlgoStroke] // 最多 1 根：最后确认笔
	lastEndpoint      *ChanKLine
	lastEndpointType  *FractalType
	candidates        *Ring[*strokeCandidate] // 容量 6，候选分型端点 FIFO
	replacedEndpoints []*strokeCandidate
	pendingOutput     []*AlgoStroke
	lastReturnedID    int64
	strictMode        bool
}

// NewStrokeProcessor 创建一个笔识别处理器。
// strictMode=true 时要求 span >= 4（缠论原文），false 时 span >= 3。
func NewStrokeProcessor(strictMode bool) *StrokeProcessor {
	return &StrokeProcessor{
		strokes:           NewRing[*AlgoStroke](1),
		lastEndpoint:      nil,
		lastEndpointType:  nil,
		candidates:        NewRing[*strokeCandidate](6),
		replacedEndpoints: make([]*strokeCandidate, 0),
		pendingOutput:     make([]*AlgoStroke, 0),
		lastReturnedID:    -1,
		strictMode:        strictMode,
	}
}

// Process 处理一个分型，返回新形成的确认笔。
// latestKline: 最新合并 K 线，用于虚笔更新。
// withVirtual: 是否启用虚笔（趋势延续过程中的临时笔）。
func (p *StrokeProcessor) Process(fractal Fractal, latestKline *ChanKLine, withVirtual bool) []*AlgoStroke {
	p.pendingOutput = p.pendingOutput[:0]
	p.cleanupVirtual()
	p.processFractal(fractal)
	if withVirtual {
		p.updateVirtual(latestKline)
	}

	// 收集自上次返回后的新笔
	var result []*AlgoStroke
	for _, s := range p.pendingOutput {
		if !s.IsVirtual && s.EndIndex > p.lastReturnedID {
			result = append(result, s)
			p.lastReturnedID = s.EndIndex
		}
	}
	return result
}

// GetLastStroke 返回最后一根确认笔。
func (p *StrokeProcessor) GetLastStroke() *AlgoStroke {
	if last, ok := p.strokes.Last(); ok {
		return last
	}
	return nil
}

// Reset 清空处理器状态。
func (p *StrokeProcessor) Reset() {
	p.strokes.Clear()
	p.lastEndpoint = nil
	p.lastEndpointType = nil
	p.candidates.Clear()
	p.replacedEndpoints = make([]*strokeCandidate, 0)
	p.pendingOutput = make([]*AlgoStroke, 0)
	p.lastReturnedID = -1
}

// ── 内部 ──

func (p *StrokeProcessor) processFractal(fractal Fractal) {
	if fractal.FType != FractalTypeTop && fractal.FType != FractalTypeBottom {
		return
	}
	if p.strokes.Len() == 0 {
		p.tryFirstStroke(fractal)
		return
	}
	if p.lastEndpointType != nil && fractal.FType == *p.lastEndpointType {
		p.tryUpdateEndpoint(fractal)
	} else if p.lastEndpointType != nil {
		p.tryCreateStroke(fractal)
	} else {
		p.tryFirstStroke(fractal)
	}
}

func (p *StrokeProcessor) tryFirstStroke(fractal Fractal) {
	allCandidates := p.candidates.ToSlice()
	for _, c := range allCandidates {
		if c.ftype != fractal.FType && p.canFormStroke(c.kline, fractal.KLine, c.index, fractal.Index) {
			p.createStroke(c.kline, fractal.KLine, c.index, fractal.Index, c.ftype, false)
			p.lastEndpoint = fractal.KLine
			p.lastEndpointType = &fractal.FType
			p.candidates.Clear()
			return
		}
	}
	p.candidates.Append(&strokeCandidate{kline: fractal.KLine, index: fractal.Index, ftype: fractal.FType})
}

func (p *StrokeProcessor) tryCreateStroke(fractal Fractal) {
	if p.strokes.Len() == 0 {
		p.tryFirstStroke(fractal)
		return
	}
	last, ok := p.strokes.Last()
	if !ok {
		p.tryFirstStroke(fractal)
		return
	}
	if p.canFormStroke(last.End, fractal.KLine, last.EndIndex, fractal.Index) {
		if p.lastEndpointType != nil {
			p.createStroke(last.End, fractal.KLine, last.EndIndex, fractal.Index, *p.lastEndpointType, false)
			p.lastEndpoint = fractal.KLine
			p.lastEndpointType = &fractal.FType
		} else {
			p.tryFirstStroke(fractal)
		}
	}
}

func (p *StrokeProcessor) canFormStroke(start, end *ChanKLine, startIdx, endIdx int64) bool {
	span := endIdx - startIdx
	if span < 0 {
		span = -span
	}
	if p.strictMode && span < 4 {
		return false
	}
	if !p.strictMode && span < 3 {
		return false
	}
	if startIdx < endIdx {
		return end.High > start.Low || end.Low < start.High
	}
	return start.High > end.Low || start.Low < end.High
}

func (p *StrokeProcessor) tryUpdateEndpoint(fractal Fractal) {
	if p.strokes.Len() == 0 {
		return
	}
	last, ok := p.strokes.Last()
	if !ok {
		return
	}

	var ok2 bool
	switch last.Direction {
	case DirectionUp:
		ok2 = fractal.FType == FractalTypeTop && fractal.KLine.High >= last.End.High
	case DirectionDown:
		ok2 = fractal.FType == FractalTypeBottom && fractal.KLine.Low <= last.End.Low
	}
	if !ok2 {
		return
	}

	endPrice := fractal.KLine.High
	if last.Direction == DirectionDown {
		endPrice = fractal.KLine.Low
	}

	high := last.High
	if fractal.KLine.High > high {
		high = fractal.KLine.High
	}
	low := last.Low
	if fractal.KLine.Low < low {
		low = fractal.KLine.Low
	}

	// RingBuffer 容量 1：add 自然覆盖旧笔
	p.strokes.Append(&AlgoStroke{
		Start:      last.Start,
		End:        fractal.KLine,
		StartIndex: last.StartIndex,
		EndIndex:   fractal.Index,
		Direction:  last.Direction,
		IsVirtual:  last.IsVirtual,
		StartPrice: last.StartPrice,
		EndPrice:   endPrice,
		High:       high,
		Low:        low,
	})
	p.lastEndpoint = fractal.KLine
}

func (p *StrokeProcessor) createStroke(start, end *ChanKLine, startIdx, endIdx int64, startType FractalType, isVirtual bool) {
	dir := DirectionUp
	if startType == FractalTypeTop {
		dir = DirectionDown
	}

	sp := start.Low
	ep := end.High
	if dir == DirectionDown {
		sp = start.High
		ep = end.Low
	}

	high := start.High
	if end.High > high {
		high = end.High
	}
	low := start.Low
	if end.Low < low {
		low = end.Low
	}

	ns := &AlgoStroke{
		Start:      start,
		End:        end,
		StartIndex: startIdx,
		EndIndex:   endIdx,
		Direction:  dir,
		IsVirtual:  isVirtual,
		StartPrice: sp,
		EndPrice:   ep,
		High:       high,
		Low:        low,
	}
	p.strokes.Append(ns)
	p.pendingOutput = append(p.pendingOutput, ns)
}

// ── 虚笔 ──

func (p *StrokeProcessor) cleanupVirtual() {
	if p.strokes.Len() == 0 {
		return
	}
	last, ok := p.strokes.Last()
	if !ok || !last.IsVirtual {
		return
	}

	if len(p.replacedEndpoints) > 0 {
		// 有替换端点：重建状态
		ep := p.replacedEndpoints[len(p.replacedEndpoints)-1]
		p.replacedEndpoints = p.replacedEndpoints[:len(p.replacedEndpoints)-1]
		p.lastEndpoint = ep.kline
		p.lastEndpointType = &ep.ftype

		// 重建后续候选
		for len(p.replacedEndpoints) > 0 {
			re := p.replacedEndpoints[len(p.replacedEndpoints)-1]
			p.replacedEndpoints = p.replacedEndpoints[:len(p.replacedEndpoints)-1]
			if p.lastEndpointType != nil && *p.lastEndpointType != re.ftype {
				if p.lastEndpoint != nil && p.canFormStroke(p.lastEndpoint, re.kline, 0, re.index) {
					p.createStroke(p.lastEndpoint, re.kline, 0, re.index, *p.lastEndpointType, false)
					p.lastEndpoint = re.kline
					p.lastEndpointType = &re.ftype
				}
			}
		}
	} else {
		// 没有替换端点：清除虚笔
		p.strokes.Clear()
	}
}

func (p *StrokeProcessor) updateVirtual(latestKline *ChanKLine) {
	if p.strokes.Len() == 0 {
		return
	}
	last, ok := p.strokes.Last()
	if !ok {
		return
	}

	extended := false
	if last.Direction == DirectionUp && latestKline.High > last.End.High {
		extended = true
	} else if last.Direction == DirectionDown && latestKline.Low < last.End.Low {
		extended = true
	}

	if extended {
		ep := latestKline.High
		if last.Direction == DirectionDown {
			ep = latestKline.Low
		}
		high := last.High
		if latestKline.High > high {
			high = latestKline.High
		}
		low := last.Low
		if latestKline.Low < low {
			low = latestKline.Low
		}
		p.strokes.Append(&AlgoStroke{
			Start:      last.Start,
			End:        latestKline,
			StartIndex: last.StartIndex,
			EndIndex:   last.EndIndex + 1,
			Direction:  last.Direction,
			IsVirtual:  true,
			StartPrice: last.StartPrice,
			EndPrice:   ep,
			High:       high,
			Low:        low,
		})
		return
	}

	// 尝试分叉（新的候选端点）
	if p.lastEndpoint != nil && p.lastEndpointType != nil {
		if p.canFormStroke(p.lastEndpoint, latestKline, 0, last.EndIndex+1) {
			p.replacedEndpoints = append(p.replacedEndpoints, &strokeCandidate{
				kline: last.End,
				index: last.EndIndex,
				ftype: *p.lastEndpointType,
			})
			p.createStroke(p.lastEndpoint, latestKline, 0, last.EndIndex+1, *p.lastEndpointType, true)
		}
	}
}
