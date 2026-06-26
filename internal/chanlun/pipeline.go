package chanlun

import "strings"

// ──────────────────────────────────────────────
// 流式管线
// 每个步骤产出增量，即时推入 OutputPipe → Redis
// 返回值仅含升级所需信息
// ──────────────────────────────────────────────

// Pipeline 是缠论 9 步流式处理管线。
// 每个交易对所有独立处理器实例，通过 Process 方法流式处理。
type Pipeline struct {
	states map[string]*SymbolState
}

// NewPipeline 创建一个新的管线。
func NewPipeline() *Pipeline {
	return &Pipeline{
		states: make(map[string]*SymbolState),
	}
}

// getOrCreate 获取或创建交易对的状态。
func (p *Pipeline) getOrCreate(symbol string) *SymbolState {
	key := strings.ToLower(symbol)
	if s, ok := p.states[key]; ok {
		return s
	}
	s := &SymbolState{
		symbol:     symbol,
		contain:    NewContainProcessor(),
		fractal:    NewFractalProcessor(),
		stroke:     NewStrokeProcessor(true),
		segment:    NewSegmentProcessor(),
		pivotZone:  NewPivotZoneProcessor(PivotZoneModeStroke),
		trend:      NewTrendPatternProcessor(),
		divergence: NewDivergenceProcessor(),
	}
	p.states[key] = s
	return s
}

// Reset 重置指定交易对的状态。
func (p *Pipeline) Reset(symbol string) {
	key := strings.ToLower(symbol)
	if s, ok := p.states[key]; ok {
		s.Reset()
	}
}

// Process 流式处理一根 K 线，通过 pipe 即时推入每步结果到 OutputPipe。
func (p *Pipeline) Process(kline *KLine, pipe PipeWriter) *ProcessResult {
	symbol := strings.ToLower(kline.Symbol)
	state := p.getOrCreate(symbol)
	state.totalKlines++

	var hasChange bool
	var hasCompletedTrend bool
	var trendDir Direction = DirectionUp
	var trendHigh, trendLow float64

	// ── Step 1: 包含处理 ──
	merged := state.contain.Process(kline)
	if merged != nil {
		pipe.Push("chan_kline", float64(merged.Time), (&ChanKlineRecord{
			Time: merged.Time, High: merged.High, Low: merged.Low,
		}).ToJSON())
		hasChange = true
	}

	// ── Step 2: 分型识别 ──
	lastElements, baseOffset := state.contain.GetLastWithOffset(3)
	newFractals := state.fractal.Scan(lastElements, baseOffset)
	for _, f := range newFractals {
		ftype := "top"
		if f.FType == FractalTypeBottom {
			ftype = "bottom"
		}
		pipe.Push("fractal", float64(f.KLine.Time), (&FractalRecord{
			Time: f.KLine.Time, FType: ftype, High: f.KLine.High, Low: f.KLine.Low, Index: f.Index,
		}).ToJSON())
		hasChange = true
	}

	// ── Step 3: 笔识别 ──
	latestCK := state.contain.GetLatest()
	var latestCKClone ChanKLine
	if latestCK != nil {
		latestCKClone = *latestCK
	} else {
		latestCKClone = ChanKLine{}
	}

	var newStrokes []*AlgoStroke
	for _, f := range newFractals {
		ns := state.stroke.Process(f, &latestCKClone, false)
		newStrokes = append(newStrokes, ns...)
	}
	for _, s := range newStrokes {
		dir := "up"
		if s.Direction == DirectionDown {
			dir = "down"
		}
		pipe.Push("stroke", float64(kline.OpenTime), (&StrokeRecord{
			Time: kline.OpenTime, StartTime: s.Start.Time, EndTime: s.End.Time,
			Direction: dir, StartIndex: s.StartIndex, EndIndex: s.EndIndex,
			StartPrice: s.StartPrice, EndPrice: s.EndPrice, High: s.High, Low: s.Low,
		}).ToJSON())
		hasChange = true
	}

	// ── Step 4: 线段 ──
	var newSegments []*AlgoSegment
	var newPivotZones []*AlgoPivotZone
	var newTrendPatterns []*AlgoTrendPattern

	if len(newStrokes) > 0 {
		newSegments = state.segment.Process(newStrokes)
		for _, seg := range newSegments {
			dir := "up"
			if seg.Direction == DirectionDown {
				dir = "down"
			}
			pipe.Push("segment", float64(kline.OpenTime), (&SegmentRecord{
				Time: kline.OpenTime, StartTime: seg.StartTime, EndTime: seg.EndTime,
				StartPrice: seg.StartPrice, EndPrice: seg.EndPrice,
				Direction: dir, StartIndex: seg.StartIndex, EndIndex: seg.EndIndex,
			}).ToJSON())
			hasChange = true
		}

		// ── Step 5: 中枢识别 ──
		newPivotZones = state.pivotZone.ProcessStrokes(newStrokes)
		for _, z := range newPivotZones {
			dir := "up"
			if z.Direction == DirectionDown {
				dir = "down"
			}
			pipe.Push("pivot_zone", float64(kline.OpenTime), (&PivotZoneRecord{
				Time: kline.OpenTime, StartTime: z.StartTime, EndTime: z.EndTime,
				StartPrice: z.StartPrice, EndPrice: z.EndPrice,
				ZG: z.ZG, ZD: z.ZD, StartIndex: z.StartIndex, EndIndex: z.EndIndex,
				Direction: dir, Completed: z.Completed,
			}).ToJSON())
			hasChange = true
		}

		// ── Step 6: 走势类型 ──
		newTrendPatterns = state.trend.Process(newPivotZones)
		for _, tp := range newTrendPatterns {
			tpDir := "up"
			if tp.Direction == DirectionDown {
				tpDir = "down"
			}
			var tStartTime, tEndTime int64
			var tStartPrice, tEndPrice float64
			if len(tp.Zones) > 0 {
				tStartTime = tp.Zones[0].StartTime
				tEndTime = tp.Zones[len(tp.Zones)-1].EndTime
				tStartPrice = tp.Zones[0].StartPrice
				tEndPrice = tp.Zones[len(tp.Zones)-1].EndPrice
			}
			pipe.Push("trend_pattern", float64(kline.OpenTime), (&TrendPatternRecord{
				Time: kline.OpenTime, StartTime: tStartTime, EndTime: tEndTime,
				StartPrice: tStartPrice, EndPrice: tEndPrice,
				Direction: tpDir, Completed: tp.Completed, ZonesCount: len(tp.Zones),
			}).ToJSON())
			hasChange = true
		}
	}

	// ── Step 7: 背驰判定 ──
	state.divergence.FeedClose(symbol, kline.Close)

	var lastDir Direction = DirectionUp
	var lastCompleted bool
	var entryPrice float64
	if len(newTrendPatterns) > 0 {
		lastTP := newTrendPatterns[len(newTrendPatterns)-1]
		lastDir = lastTP.Direction
		lastCompleted = lastTP.Completed
		if len(newStrokes) > 0 {
			entryPrice = newStrokes[len(newStrokes)-1].EndPrice
		}
	}
	newDivergences := state.divergence.Process(symbol, entryPrice, lastDir, lastCompleted)
	for _, d := range newDivergences {
		pipe.Push("divergence", float64(kline.OpenTime), (&DivergenceRecord{
			Time: kline.OpenTime, Price: kline.Close,
			DType: d.DivergenceType, Ratio: d.Ratio, Confirmed: d.Confirmed,
		}).ToJSON())
		hasChange = true
	}

	// ── Step 8: 走势结束判定（背驰 → 标记完成） ──
	if len(newDivergences) > 0 && len(newTrendPatterns) > 0 {
		lastTP := newTrendPatterns[len(newTrendPatterns)-1]
		if !lastTP.Completed {
			for _, d := range newDivergences {
				m := (d.DivergenceType == "bottomDivergence" && lastTP.Direction == DirectionDown) ||
					(d.DivergenceType == "topDivergence" && lastTP.Direction == DirectionUp)
				if m {
					state.trend.MarkLastCompleted("divergence")
					break
				}
			}
		}
	}

	// ── Step 9: 买卖点识别 ──
	// 第一类买卖点（背驰）
	for _, d := range newDivergences {
		signalType := ""
		switch d.DivergenceType {
		case "bottomDivergence":
			signalType = "Buy1"
		case "topDivergence":
			signalType = "Sell1"
		}
		if signalType != "" {
			pipe.Push("signal", float64(kline.OpenTime), (&SignalRecord{
				Time: kline.OpenTime, SType: signalType, Price: kline.Close, Strength: 0.8,
			}).ToJSON())
			hasChange = true
			state.hasFirstSignal = true
		}
	}

	// 第三类买卖点（中枢完成）
	if len(newPivotZones) > 0 {
		for _, z := range newPivotZones {
			if z.Completed {
				signalType := "Buy3"
				if z.Direction == DirectionUp {
					signalType = "Sell3"
				}
				pipe.Push("signal", float64(kline.OpenTime), (&SignalRecord{
					Time: kline.OpenTime, SType: signalType, Price: kline.Close, Strength: 0.7,
				}).ToJSON())
				hasChange = true
			}
		}
	}

	// 第二类买卖点（已有第一类后的回调方向）
	if state.hasFirstSignal {
		patterns := state.trend.GetPatterns()
		if len(patterns) > 0 {
			lastTP := patterns[len(patterns)-1]
			signalType := "Buy2"
			if lastTP.Direction == DirectionDown {
				signalType = "Sell2"
			}
			pipe.Push("signal", float64(kline.OpenTime), (&SignalRecord{
				Time: kline.OpenTime, SType: signalType, Price: kline.Close, Strength: 0.6,
			}).ToJSON())
			hasChange = true
		}
	}

	// 检查完成趋势——用于级别升级
	for _, tp := range newTrendPatterns {
		if tp.Completed {
			hasCompletedTrend = true
			trendDir = tp.Direction
			if len(tp.Zones) > 0 {
				trendHigh = tp.Zones[0].ZG
				trendLow = tp.Zones[0].ZD
			}
			break
		}
	}

	return &ProcessResult{
		Symbol:            symbol,
		Time:              kline.OpenTime,
		HasCompletedTrend: hasCompletedTrend,
		TrendDirection:    trendDir,
		TrendHigh:         trendHigh,
		TrendLow:          trendLow,
		HasChange:         hasChange,
	}
}

// SymbolState 是一个交易对在管线中的完整状态。
type SymbolState struct {
	symbol         string
	contain        *ContainProcessor
	fractal        *FractalProcessor
	stroke         *StrokeProcessor
	segment        *SegmentProcessor
	pivotZone      *PivotZoneProcessor
	trend          *TrendPatternProcessor
	divergence     *DivergenceProcessor
	totalKlines    int64
	hasFirstSignal bool
}

// Reset 重置交易对状态。
func (s *SymbolState) Reset() {
	s.contain.Reset()
	s.fractal.Reset()
	s.stroke.Reset()
	s.segment.Reset()
	s.pivotZone.Reset()
	s.trend.Reset()
	s.divergence.Reset()
	s.totalKlines = 0
	s.hasFirstSignal = false
}

// PipeWriter 是 Pipeline 写入 OutputPipe 的接口抽象。
// 便于测试时 mock。
type PipeWriter interface {
	Push(entityType string, score float64, value string)
}
