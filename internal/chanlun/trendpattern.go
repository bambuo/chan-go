package chanlun

// ──────────────────────────────────────────────
// 走势类型分类（流式）
// RingBuffer 保留最近 1 个走势类型做延续判断
// ──────────────────────────────────────────────

// TrendPatternProcessor 是走势类型分类处理器。
// 相同方向的中枢叠加为趋势（2+中枢），否则为盘整。
type TrendPatternProcessor struct {
	patterns *Ring[*AlgoTrendPattern] // 容量 1
}

// NewTrendPatternProcessor 创建一个走势类型分类处理器。
func NewTrendPatternProcessor() *TrendPatternProcessor {
	return &TrendPatternProcessor{
		patterns: NewRing[*AlgoTrendPattern](1),
	}
}

// Process 处理新输入的中枢，返回新形成的走势类型。
func (p *TrendPatternProcessor) Process(newZones []*AlgoPivotZone) []*AlgoTrendPattern {
	var result []*AlgoTrendPattern

	for _, z := range newZones {
		lastOpt, hasLast := p.patterns.Last()

		if hasLast {
			if !lastOpt.Completed && lastOpt.Direction == z.Direction {
				// 同方向中枢：尝试叠加
				prev := lastOpt.Zones[len(lastOpt.Zones)-1]
				ok := false
				if z.Direction == DirectionUp {
					ok = z.ZD > prev.ZG
				} else {
					ok = z.ZG < prev.ZD
				}
				if ok {
					lastOpt.Zones = append(lastOpt.Zones, z)
					if len(lastOpt.Zones) >= 2 {
						lastOpt.IsTrend = true
					}
					continue
				}
				// 不能叠加，标记前一个完成
				lastOpt.Completed = true
				result = append(result, lastOpt)
			} else {
				// 不同方向或已完成的走势：标记前一个完成
				lastOpt.Completed = true
				result = append(result, lastOpt)
			}
		}

		// 创建新走势
		np := &AlgoTrendPattern{
			Direction:        z.Direction,
			IsTrend:          false,
			Zones:            []*AlgoPivotZone{z},
			Completed:        false,
			CompletionReason: "",
		}
		p.patterns.Append(np)
		result = append(result, np)
	}

	return result
}

// GetPatterns 返回当前所有走势类型。
func (p *TrendPatternProcessor) GetPatterns() []*AlgoTrendPattern {
	return p.patterns.ToSlice()
}

// MarkLastCompleted 标记最后一个走势为完成状态。
func (p *TrendPatternProcessor) MarkLastCompleted(reason string) {
	if last, ok := p.patterns.Last(); ok {
		last.Completed = true
		last.CompletionReason = reason
	}
}

// Reset 清空处理器状态。
func (p *TrendPatternProcessor) Reset() {
	p.patterns.Clear()
}
