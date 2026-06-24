// Package chanlun — 信号引擎输入类型。
//
// 这些类型是 PipelineOutput 的导出子集，供 signal 包使用。
// 定义在此处而非 signal 包以避免循环依赖（signal 导入 chanlun 的结构类型）。
package chanlun

import "trade/internal/types"

// SignalInput 信号引擎输入，由 M3Bridge 从 PipelineOutput 转换而来。
type SignalInput struct {
	Symbol        string
	Strokes       []StrokeInfo
	PivotZones    []PivotZoneInfo
	TrendPatterns []TrendPatternInfo
	Divergences   []DivergenceInfo
}

// StrokeInfo 笔信息子集。
type StrokeInfo struct {
	Index      int
	Direction  types.ChanDirection
	StartPrice float64
	EndPrice   float64
	High       float64
	Low        float64
}

// PivotZoneInfo 中枢信息子集。
type PivotZoneInfo struct {
	Index        int
	ZG           float64
	ZD           float64
	Direction    types.ChanDirection
	EndStrokeIdx int
}

// TrendPatternInfo 走势类型信息子集。
type TrendPatternInfo struct {
	Type         string
	Direction    types.ChanDirection
	PivotZoneIDs []int
}

// DivergenceInfo 背驰信息子集。
type DivergenceInfo struct {
	Type       string  // "topDivergence" / "bottomDivergence"
	ZoneIdx    int     // 最后中枢索引
	EntryMACD  float64 // 进入段累积强度
	ExitMACD   float64 // 离开段累积强度
	EntryPrice float64 // 进入段极值价格
	ExitPrice  float64 // 离开段极值价格
	ExitEnd    int     // 离开段结束笔索引（用于信号溯源）
	EntryEnd   int     // 进入段结束笔索引（用于信号溯源）
	Ratio      float64 // ExitMACD / EntryMACD
	Confirmed  bool    // Ratio < 0.95
}

// ToSignalInput 将 PipelineOutput 转换为 SignalInput。
func (out *PipelineOutput) ToSignalInput() *SignalInput {
	if out == nil {
		return nil
	}

	input := &SignalInput{
		Symbol: out.Symbol,
	}

	for _, s := range out.Strokes {
		input.Strokes = append(input.Strokes, StrokeInfo{
			Index:      s.Index,
			Direction:  s.Direction,
			StartPrice: s.StartPrice,
			EndPrice:   s.EndPrice,
			High:       s.High,
			Low:        s.Low,
		})
	}

	for _, pz := range out.PivotZones {
		input.PivotZones = append(input.PivotZones, PivotZoneInfo{
			Index:        pz.index,
			ZG:           pz.ZG,
			ZD:           pz.ZD,
			Direction:    pz.Direction,
			EndStrokeIdx: pz.EndStrokeIdx,
		})
	}

	for _, tp := range out.TrendPatterns {
		input.TrendPatterns = append(input.TrendPatterns, TrendPatternInfo{
			Type:         tp.Type,
			Direction:    tp.Direction,
			PivotZoneIDs: tp.PivotZoneIDs,
		})
	}

	for _, d := range out.Divergences {
		input.Divergences = append(input.Divergences, DivergenceInfo{
			Type:      d.Type,
			ZoneIdx:   d.ZoneIdx,
			EntryMACD: d.EntryMACD,
			ExitMACD:  d.ExitMACD,
			EntryPrice: d.EntryPrice,
			ExitPrice:  d.ExitPrice,
			ExitEnd:   d.ExitEnd,
			EntryEnd:  d.EntryEnd,
			Ratio:     d.Ratio,
			Confirmed: d.Confirmed,
		})
	}

	return input
}
