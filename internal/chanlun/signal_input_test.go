// Package chanlun — ToSignalInput 转换测试。
package chanlun

import (
	"testing"

	"trade/internal/types"
)

// TestToSignalInput_Basic 验证 PipelineOutput → SignalInput 的基本转换。
func TestToSignalInput_Basic(t *testing.T) {
	output := &PipelineOutput{
		Symbol: "TEST",
		Strokes: []*stroke{
			{Index: 0, Direction: types.DirectionUp, StartPrice: 10, EndPrice: 20, High: 22, Low: 8},
			{Index: 1, Direction: types.DirectionDown, StartPrice: 20, EndPrice: 14, High: 20, Low: 12},
		},
		PivotZones: []*pivotZone{
			{index: 0, ZG: 18, ZD: 12, Direction: types.DirectionUp, EndStrokeIdx: 2},
		},
		TrendPatterns: []*trendPattern{
			{Index: 0, Type: "consolidation", Direction: types.DirectionUp, PivotZoneIDs: []int{0}},
		},
		Divergences: []*divergence{
			{Type: "bottomDivergence", Stroke1Idx: 0, Stroke2Idx: 2, Ratio: 0.5, Confirmed: true},
		},
	}

	input := output.ToSignalInput()

	if input == nil {
		t.Fatal("ToSignalInput 返回 nil")
	}
	if input.Symbol != "TEST" {
		t.Errorf("Symbol 期望 TEST, 实际 %s", input.Symbol)
	}
	if len(input.Strokes) != 2 {
		t.Errorf("Strokes 长度期望 2, 实际 %d", len(input.Strokes))
	}
	if len(input.PivotZones) != 1 {
		t.Errorf("PivotZones 长度期望 1, 实际 %d", len(input.PivotZones))
	}
	if len(input.TrendPatterns) != 1 {
		t.Errorf("TrendPatterns 长度期望 1, 实际 %d", len(input.TrendPatterns))
	}
	if len(input.Divergences) != 1 {
		t.Errorf("Divergences 长度期望 1, 实际 %d", len(input.Divergences))
	}

	// 验证笔信息
	if input.Strokes[0].Index != 0 || input.Strokes[0].Direction != types.DirectionUp {
		t.Error("Stroke[0] 信息不正确")
	}
	if input.Strokes[0].StartPrice != 10 || input.Strokes[0].EndPrice != 20 {
		t.Error("Stroke[0] 价格不正确")
	}

	// 验证中枢信息
	if input.PivotZones[0].ZG != 18 || input.PivotZones[0].ZD != 12 {
		t.Error("PivotZone[0] 价格不正确")
	}

	// 验证走势类型信息
	if input.TrendPatterns[0].Type != "consolidation" {
		t.Error("TrendPattern[0] 类型不正确")
	}

	// 验证背驰信息
	if input.Divergences[0].Type != "bottomDivergence" || !input.Divergences[0].Confirmed {
		t.Error("Divergence[0] 信息不正确")
	}
	if input.Divergences[0].Ratio != 0.5 {
		t.Errorf("Divergence[0] Ratio 期望 0.5, 实际 %.2f", input.Divergences[0].Ratio)
	}
}

// TestToSignalInput_NilOutput 验证：nil 输入返回 nil。
func TestToSignalInput_NilOutput(t *testing.T) {
	var output *PipelineOutput = nil
	input := output.ToSignalInput()
	if input != nil {
		t.Error("nil 输入应返回 nil")
	}
}

// TestToSignalInput_EmptyStructures 验证：空结构转换。
func TestToSignalInput_EmptyStructures(t *testing.T) {
	output := &PipelineOutput{
		Symbol: "TEST",
	}

	input := output.ToSignalInput()

	if input == nil {
		t.Fatal("ToSignalInput 返回 nil")
	}
	if len(input.Strokes) != 0 {
		t.Errorf("空 Strokes 期望 0, 实际 %d", len(input.Strokes))
	}
	if len(input.PivotZones) != 0 {
		t.Errorf("空 PivotZones 期望 0, 实际 %d", len(input.PivotZones))
	}
	if len(input.TrendPatterns) != 0 {
		t.Errorf("空 TrendPatterns 期望 0, 实际 %d", len(input.TrendPatterns))
	}
	if len(input.Divergences) != 0 {
		t.Errorf("空 Divergences 期望 0, 实际 %d", len(input.Divergences))
	}
}

// TestToSignalInput_DivergenceDetails 验证：背驰详情完整转换。
func TestToSignalInput_DivergenceDetails(t *testing.T) {
	output := &PipelineOutput{
		Symbol: "TEST",
		Strokes: []*stroke{
			{Index: 0, Direction: types.DirectionDown, StartPrice: 100, EndPrice: 80, High: 100, Low: 80},
			{Index: 1, Direction: types.DirectionUp, StartPrice: 80, EndPrice: 95, High: 95, Low: 80},
			{Index: 2, Direction: types.DirectionDown, StartPrice: 95, EndPrice: 70, High: 95, Low: 70},
			{Index: 3, Direction: types.DirectionUp, StartPrice: 70, EndPrice: 85, High: 85, Low: 70},
			{Index: 4, Direction: types.DirectionDown, StartPrice: 85, EndPrice: 50, High: 85, Low: 50},
		},
		Divergences: []*divergence{
			{
				Type:       "bottomDivergence",
				Stroke1Idx: 2,
				Stroke2Idx: 4,
				Price1:     70,
				Price2:     50,
				Strength1:  30,
				Strength2:  15,
				Ratio:      0.5,
				Confirmed:  true,
			},
		},
	}

	input := output.ToSignalInput()

	if len(input.Divergences) != 1 {
		t.Fatalf("Divergences 期望 1, 实际 %d", len(input.Divergences))
	}

	d := input.Divergences[0]
	if d.Stroke1Idx != 2 || d.Stroke2Idx != 4 {
		t.Error("背驰笔索引不正确")
	}
	if d.Price1 != 70 || d.Price2 != 50 {
		t.Error("背驰价格不正确")
	}
	if d.Strength1 != 30 || d.Strength2 != 15 {
		t.Error("背驰强度不正确")
	}
	if d.Ratio != 0.5 {
		t.Errorf("背驰比率期望 0.5, 实际 %.2f", d.Ratio)
	}
	if !d.Confirmed {
		t.Error("背驰应已确认")
	}
}

// TestToSignalInput_TrendPatternZoneIDs 验证：走势类型的中枢 ID 转换。
func TestToSignalInput_TrendPatternZoneIDs(t *testing.T) {
	output := &PipelineOutput{
		Symbol: "TEST",
		TrendPatterns: []*trendPattern{
			{
				Index:        0,
				Type:         "trend",
				Direction:    types.DirectionUp,
				PivotZoneIDs: []int{0, 1},
				Completed:    true,
			},
		},
	}

	input := output.ToSignalInput()

	if len(input.TrendPatterns) != 1 {
		t.Fatalf("TrendPatterns 期望 1, 实际 %d", len(input.TrendPatterns))
	}

	tp := input.TrendPatterns[0]
	if len(tp.PivotZoneIDs) != 2 {
		t.Errorf("PivotZoneIDs 期望 2, 实际 %d", len(tp.PivotZoneIDs))
	}
	if tp.PivotZoneIDs[0] != 0 || tp.PivotZoneIDs[1] != 1 {
		t.Error("PivotZoneIDs 内容不正确")
	}
}
