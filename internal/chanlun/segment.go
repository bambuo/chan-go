package chanlun

import "math"

// ──────────────────────────────────────────────
// 线段划分（流式）
// 使用特征序列方法进行线段划分
// ──────────────────────────────────────────────

// featureK 是特征序列元素。
type featureK struct {
	high      float64
	low       float64
	strokeIdx int64
	isUp      bool
}

// SegmentProcessor 是线段划分处理器。
// 基于特征序列方法，检测特征分型来划分线段。
type SegmentProcessor struct {
	segments          *Ring[*AlgoSegment] // 最多 1 段：最后一段
	currentDir        Direction
	segmentStart      int64
	segmentStartTime  int64
	segmentEndTime    int64
	segmentStartPrice float64
	segmentEndPrice   float64
	procSeq           []*featureK        // 特征序列
	maxProcSeq        int64              // 30
	pendingIdx        int64              // -1 表示无待定
	allStrokes        *Ring[*AlgoStroke] // 最近 200 笔
	hasInitialized    bool
}

// NewSegmentProcessor 创建一个线段划分处理器。
func NewSegmentProcessor() *SegmentProcessor {
	return &SegmentProcessor{
		segments:          NewRing[*AlgoSegment](1),
		currentDir:        DirectionUp,
		segmentStart:      0,
		segmentStartTime:  0,
		segmentEndTime:    0,
		segmentStartPrice: 0,
		segmentEndPrice:   0,
		procSeq:           make([]*featureK, 0),
		maxProcSeq:        30,
		pendingIdx:        -1,
		allStrokes:        NewRing[*AlgoStroke](200),
		hasInitialized:    false,
	}
}

// Process 处理新输入的笔，返回新形成的线段。
func (p *SegmentProcessor) Process(newStrokes []*AlgoStroke) []*AlgoSegment {
	var result []*AlgoSegment
	for _, s := range newStrokes {
		p.allStrokes.Append(s)
		p.processStroke(s, &result)
	}
	return result
}

// GetSegments 返回当前所有线段。
func (p *SegmentProcessor) GetSegments() []*AlgoSegment {
	return p.segments.ToSlice()
}

// Reset 清空处理器状态。
func (p *SegmentProcessor) Reset() {
	p.segments.Clear()
	p.currentDir = DirectionUp
	p.segmentStart = 0
	p.segmentStartTime = 0
	p.segmentEndTime = 0
	p.segmentStartPrice = 0
	p.segmentEndPrice = 0
	p.procSeq = make([]*featureK, 0)
	p.pendingIdx = -1
	p.allStrokes.Clear()
	p.hasInitialized = false
}

// ── 内部 ──

func (p *SegmentProcessor) processStroke(s *AlgoStroke, result *[]*AlgoSegment) {
	// 跟踪线段起止时间和价格
	if p.segmentStartTime == 0 {
		p.segmentStartTime = s.Start.Time
		p.segmentStartPrice = s.StartPrice
	}
	p.segmentEndTime = s.End.Time
	p.segmentEndPrice = s.EndPrice

	// 首笔决定方向
	if !p.hasInitialized {
		p.currentDir = s.Direction
		p.hasInitialized = true
	}

	// 反向笔加入特征序列
	if s.Direction != p.currentDir {
		fk := &featureK{
			high:      s.High,
			low:       s.Low,
			strokeIdx: s.EndIndex,
			isUp:      s.Direction == DirectionUp,
		}
		p.procSeq = append(p.procSeq, fk)
		if int64(len(p.procSeq)) > p.maxProcSeq {
			// 超限丢弃最旧
			p.procSeq = p.procSeq[1:]
		}
		p.mergeTail()
	}

	// 检测线段结束
	if p.pendingIdx >= 0 {
		if sg := p.checkPending(); sg != nil {
			*result = append(*result, sg)
		}
	} else {
		if sg := p.checkFractal(); sg != nil {
			*result = append(*result, sg)
		}
	}
}

// mergeTail 合并特征序列尾部的包含关系。
func (p *SegmentProcessor) mergeTail() {
	if len(p.procSeq) < 2 {
		return
	}
	for len(p.procSeq) >= 2 {
		a := p.procSeq[len(p.procSeq)-2]
		b := p.procSeq[len(p.procSeq)-1]

		// 检查包含关系
		aContainsB := a.high >= b.high && a.low <= b.low
		bContainsA := b.high >= a.high && b.low <= a.low
		if !aContainsB && !bContainsA {
			break
		}

		m := p.mergeFK(a, b, false)
		p.procSeq = p.procSeq[:len(p.procSeq)-2]
		p.procSeq = append(p.procSeq, m)
	}
}

// mergeFK 合并两个特征元素。
// isSecond=true 表示第二元素的合并（向上/向下规则取反）。
func (p *SegmentProcessor) mergeFK(a, b *featureK, isSecond bool) *featureK {
	if isSecond {
		if p.currentDir == DirectionUp {
			return &featureK{
				high:      math.Max(a.high, b.high),
				low:       math.Max(a.low, b.low),
				strokeIdx: b.strokeIdx,
				isUp:      true,
			}
		}
		return &featureK{
			high:      math.Min(a.high, b.high),
			low:       math.Min(a.low, b.low),
			strokeIdx: b.strokeIdx,
			isUp:      false,
		}
	}

	// 第一元素的合并规则
	if p.currentDir == DirectionUp {
		return &featureK{
			high:      math.Min(a.high, b.high),
			low:       math.Min(a.low, b.low),
			strokeIdx: b.strokeIdx,
			isUp:      false,
		}
	}
	return &featureK{
		high:      math.Max(a.high, b.high),
		low:       math.Max(a.low, b.low),
		strokeIdx: b.strokeIdx,
		isUp:      true,
	}
}

// checkFractal 检测特征序列中的标准分型。
func (p *SegmentProcessor) checkFractal() *AlgoSegment {
	if len(p.procSeq) < 3 {
		return nil
	}
	a := p.procSeq[len(p.procSeq)-3]
	b := p.procSeq[len(p.procSeq)-2]
	c := p.procSeq[len(p.procSeq)-1]

	var hasFractal bool
	if p.currentDir == DirectionUp {
		hasFractal = b.high > a.high && b.high > c.high && b.low > a.low && b.low > c.low
	} else {
		hasFractal = b.low < a.low && b.low < c.low && b.high < a.high && b.high < c.high
	}

	if !hasFractal {
		return nil
	}

	// 检查是否有 gap（缺口）
	aAboveBLow := a.high < b.low
	bLowBelowAHigh := a.low > b.high
	hasGap := !aAboveBLow && !bLowBelowAHigh
	if !hasGap {
		return p.closeSegment(b.strokeIdx)
	}

	// 有 gap，进入待定模式
	p.pendingIdx = b.strokeIdx
	p.procSeq = p.procSeq[:0]
	return nil
}

// checkPending 检查待定模式下的线段结束条件。
func (p *SegmentProcessor) checkPending() *AlgoSegment {
	// 收集待定后的同向笔
	var seq []*featureK
	allStrokesList := p.allStrokes.ToSlice()

	for i, s := range allStrokesList {
		if s.EndIndex <= p.pendingIdx {
			continue
		}
		if s.Direction == p.currentDir {
			fk := &featureK{
				high:      s.High,
				low:       s.Low,
				strokeIdx: int64(i),
				isUp:      s.Direction == DirectionUp,
			}
			seq = append(seq, fk)

			// 合并包含
			if len(seq) >= 2 {
				for len(seq) >= 2 {
					a := seq[len(seq)-2]
					b := seq[len(seq)-1]
					aContainsB := a.high >= b.high && a.low <= b.low
					bContainsA := b.high >= a.high && b.low <= a.low
					if !aContainsB && !bContainsA {
						break
					}
					m := p.mergeFK(a, b, true)
					seq = seq[:len(seq)-2]
					seq = append(seq, m)
				}
			}
		}
	}

	if len(seq) < 3 {
		return nil
	}

	// 在合并后的序列中找底（向上线段）或顶（向下线段）分型
	for i := 0; i < len(seq)-2; i++ {
		a := seq[i]
		b := seq[i+1]
		c := seq[i+2]
		var hasFractal bool
		if p.currentDir == DirectionUp {
			// 找底分型
			hasFractal = b.low < a.low && b.low < c.low && b.high < a.high && b.high < c.high
		} else {
			// 找顶分型
			hasFractal = b.high > a.high && b.high > c.high && b.low > a.low && b.low > c.low
		}
		if hasFractal {
			savedPending := p.pendingIdx
			p.pendingIdx = -1
			return p.closeSegment(savedPending)
		}
	}
	return nil
}

// closeSegment 关闭当前线段并返回。
func (p *SegmentProcessor) closeSegment(endIdx int64) *AlgoSegment {
	if p.segmentStart > endIdx {
		return nil
	}

	seg := &AlgoSegment{
		Direction:  p.currentDir,
		Strokes:    make([]*AlgoStroke, 0),
		StartIndex: p.segmentStart,
		EndIndex:   endIdx,
		StartTime:  p.segmentStartTime,
		EndTime:    p.segmentEndTime,
		StartPrice: p.segmentStartPrice,
		EndPrice:   p.segmentEndPrice,
		High:       0,
		Low:        0,
	}
	p.segments.Append(seg)

	// 重置线段状态
	p.segmentStart = endIdx + 1
	p.segmentStartTime = 0
	p.segmentStartPrice = 0
	p.currentDir = p.currentDir.Opposite()
	p.procSeq = p.procSeq[:0]
	return seg
}
