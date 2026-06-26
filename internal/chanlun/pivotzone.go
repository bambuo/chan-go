package chanlun

// ──────────────────────────────────────────────
// 中枢识别（流式）
// 支持笔中枢和线段中枢两种模式
// ──────────────────────────────────────────────

// PivotZoneProcessor 是中�的识别处理器。
// 支持基于笔（Stroke）或线段（Segment）构建中枢。
type PivotZoneProcessor struct {
	zones         *Ring[*AlgoPivotZone]
	strokeBuffer  *Ring[*AlgoStroke]  // 笔模式：积累笔做滑动检测
	segmentBuffer *Ring[*AlgoSegment] // 线段模式：积累线段做滑动检测
	mode          PivotZoneMode
}

// NewPivotZoneProcessor 创建一个中枢识别处理器。
// mode 指定中枢构建模式（笔中枢或线段中枢）。
func NewPivotZoneProcessor(mode PivotZoneMode) *PivotZoneProcessor {
	return &PivotZoneProcessor{
		zones:         NewRing[*AlgoPivotZone](50),
		strokeBuffer:  NewRing[*AlgoStroke](100),
		segmentBuffer: NewRing[*AlgoSegment](50),
		mode:          mode,
	}
}

// ProcessStrokes 基于笔检测中枢，返回新发现的中枢。
func (p *PivotZoneProcessor) ProcessStrokes(newStrokes []*AlgoStroke) []*AlgoPivotZone {
	for _, s := range newStrokes {
		p.strokeBuffer.Append(s)
	}
	return p.detectFromStrokes()
}

// ProcessSegments 基于线段检测中枢，返回新发现的中枢。
func (p *PivotZoneProcessor) ProcessSegments(newSegments []*AlgoSegment) []*AlgoPivotZone {
	for _, s := range newSegments {
		p.segmentBuffer.Append(s)
	}
	return p.detectFromSegments()
}

// GetZones 返回当前所有中枢。
func (p *PivotZoneProcessor) GetZones() []*AlgoPivotZone {
	return p.zones.ToSlice()
}

// Reset 清空处理器状态。
func (p *PivotZoneProcessor) Reset() {
	p.zones.Clear()
	p.strokeBuffer.Clear()
	p.segmentBuffer.Clear()
}

// ── 内部：笔中枢检测 ──

func (p *PivotZoneProcessor) detectFromStrokes() []*AlgoPivotZone {
	var result []*AlgoPivotZone
	all := p.strokeBuffer.ToSlice()
	if len(all) < 3 {
		return result
	}

	start := 0
	if len(all) > 5 {
		start = len(all) - 5
	}

	for i := start; i < len(all)-2; i++ {
		s1, s2, s3 := all[i], all[i+1], all[i+2]

		// ZG = 三笔高点中的最低者
		zg := minOf(s1.High, s2.High, s3.High)
		// ZD = 三笔低点中的最高者
		zd := maxOf(s1.Low, s2.Low, s3.Low)

		if zg <= zd {
			continue
		}

		z := &AlgoPivotZone{
			ZG:         zg,
			ZD:         zd,
			StartIndex: int64(i),
			EndIndex:   int64(i + 2),
			Direction:  s2.Direction,
			Completed:  false,
			StartTime:  s1.Start.Time,
			EndTime:    s3.End.Time,
			StartPrice: s1.StartPrice,
			EndPrice:   s3.EndPrice,
		}

		// 延展检测
		ei := i + 3
		for ei < len(all) && all[ei].High >= zd && all[ei].Low <= zg {
			z.EndIndex = int64(ei)
			ei++
		}
		// 完成判定：后续笔脱离区间
		if ei < len(all) {
			next := all[ei]
			if !(next.High >= zd && next.Low <= zg) {
				z.Completed = true
			}
		}

		if !p.isDuplicate(z) {
			p.zones.Append(z)
			result = append(result, z)
		}
		break // 每次只检测最近一组
	}
	return result
}

// ── 内部：线段中枢检测 ──

func (p *PivotZoneProcessor) detectFromSegments() []*AlgoPivotZone {
	var result []*AlgoPivotZone
	all := p.segmentBuffer.ToSlice()
	if len(all) < 3 {
		return result
	}

	start := 0
	if len(all) > 20 {
		start = len(all) - 20
	}

	for i := start; i < len(all)-2; i++ {
		s1, s2, s3 := all[i], all[i+1], all[i+2]

		zg := minOf(s1.High, s2.High, s3.High)
		zd := maxOf(s1.Low, s2.Low, s3.Low)

		if zg <= zd {
			continue
		}

		z := &AlgoPivotZone{
			ZG:         zg,
			ZD:         zd,
			StartIndex: int64(i),
			EndIndex:   int64(i + 2),
			Direction:  s2.Direction,
			Completed:  false,
			StartTime:  s1.StartTime,
			EndTime:    s3.EndTime,
			StartPrice: s1.StartPrice,
			EndPrice:   s3.EndPrice,
		}

		ei := i + 3
		for ei < len(all) && all[ei].High >= zd && all[ei].Low <= zg {
			z.EndIndex = int64(ei)
			ei++
		}
		if ei < len(all) {
			if !(all[ei].High >= zd && all[ei].Low <= zg) {
				z.Completed = true
			}
		}

		if !p.isDuplicate(z) {
			p.zones.Append(z)
			result = append(result, z)
		}
		break
	}
	return result
}

// ── 工具 ──

func (p *PivotZoneProcessor) isDuplicate(z *AlgoPivotZone) bool {
	for _, ez := range p.zones.ToSlice() {
		if ez.ZG == z.ZG && ez.ZD == z.ZD && ez.StartIndex == z.StartIndex {
			return true
		}
	}
	return false
}

func minOf(a, b, c float64) float64 {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}

func maxOf(a, b, c float64) float64 {
	m := a
	if b > m {
		m = b
	}
	if c > m {
		m = c
	}
	return m
}
