// Package chanlun 背驰判定处理器单元测试。
package chanlun

import (
	"testing"

	"trade/internal/types"
)

// ====== 趋势背驰测试（第53课定义） ======
//
// 背驰 = 比较最后一个中枢的进入段 vs 离开段的累积强度。
// 趋势背驰需要至少 1 个中枢（走势类型）才能比较。

// mkDivStroke 快速创建用于背驰测试的笔。
func mkDivStroke(index int, startPrice, endPrice float64, direction types.ChanDirection) *stroke {
	var high, low float64
	if startPrice > endPrice {
		high = startPrice
		low = endPrice
	} else {
		high = endPrice
		low = startPrice
	}
	return &stroke{
		Index:      index,
		StartPrice: startPrice,
		EndPrice:   endPrice,
		High:       high,
		Low:        low,
		Direction:  direction,
		Confirmed:  true,
	}
}

// TestDivergence_TrendUp_Confirmed 验证：上涨趋势（≥1中枢），离开段力度 < 进入段 → 确认顶背驰。
//
// 进入段: bi0 Up(10→30, 强度=20, 高价=30)
// 中枢:   bi1-bi3 (Down→Up→Down, 区间 [25,35])
// 离开段: bi4 Up(25→40, 强度=15, 高价=40)
// 高价比较: exitHigh(40) > entryHigh(30) ✓
// 力度衰减: exitMACD(15) / entryMACD(20) = 0.75 < 0.95 → 确认!
func TestDivergence_TrendUp_Confirmed(t *testing.T) {
	dp := NewDivergenceProcessor()

	strokes := []*stroke{
		mkDivStroke(0, 10, 30, types.DirectionUp),     // 进入段
		mkDivStroke(1, 30, 20, types.DirectionDown),    // 中枢
		mkDivStroke(2, 20, 35, types.DirectionUp),      // 中枢
		mkDivStroke(3, 35, 25, types.DirectionDown),    // 中枢 → [25,35]
		mkDivStroke(4, 25, 40, types.DirectionUp),      // 离开段，强度=15 < 20
	}
	pivotZones := []*pivotZone{
		{index: 0, StartStrokeIdx: 1, EndStrokeIdx: 3, ZG: 35, ZD: 25, Direction: types.DirectionUp, SegmentsCount: 3},
	}
	patterns := []*trendPattern{
		{Index: 0, Type: "consolidation", Direction: types.DirectionUp, PivotZoneIDs: []int{0},
			StartStrokeIdx: 0, EndStrokeIdx: 4, Completed: false},
	}

	divs := dp.Process("TEST", strokes, pivotZones, patterns)

	found := false
	for _, d := range divs {
		t.Logf("背驰: %s ratio=%.2f confirmed=%v", d.Type, d.Ratio, d.Confirmed)
		if d.Type == "topDivergence" && d.Confirmed {
			found = true
		}
	}
	if !found {
		t.Error("上涨趋势力度减弱时应确认顶背驰")
	}
}

// TestDivergence_TrendDown_Confirmed 验证：下跌趋势（≥1中枢），离开段力度 < 进入段 → 确认底背驰。
//
// 进入段: bi0 Down(40→10, 强度=30, 低价=10)
// 中枢:   bi1-bi3 (Up→Down→Up, 区间 [15,30])
// 离开段: bi4 Down(25→8, 强度=17, 低价=8)
// 低价比较: exitLow(8) < entryLow(10) ✓
// 力度衰减: exitMACD(17) / entryMACD(30) = 0.57 < 0.95 → 确认!
func TestDivergence_TrendDown_Confirmed(t *testing.T) {
	dp := NewDivergenceProcessor()

	strokes := []*stroke{
		mkDivStroke(0, 40, 10, types.DirectionDown),   // 进入段
		mkDivStroke(1, 10, 28, types.DirectionUp),      // 中枢
		mkDivStroke(2, 28, 15, types.DirectionDown),    // 中枢
		mkDivStroke(3, 15, 30, types.DirectionUp),      // 中枢 → [15,30]
		mkDivStroke(4, 25, 8, types.DirectionDown),     // 离开段，强度=17 < 30
	}

	pivotZones := []*pivotZone{
		{index: 0, StartStrokeIdx: 1, EndStrokeIdx: 3, ZG: 30, ZD: 15, Direction: types.DirectionDown, SegmentsCount: 3},
	}
	patterns := []*trendPattern{
		{Index: 0, Type: "consolidation", Direction: types.DirectionDown, PivotZoneIDs: []int{0},
			StartStrokeIdx: 0, EndStrokeIdx: 4, Completed: false},
	}

	divs := dp.Process("TEST", strokes, pivotZones, patterns)

	found := false
	for _, d := range divs {
		t.Logf("背驰: %s ratio=%.2f confirmed=%v", d.Type, d.Ratio, d.Confirmed)
		if d.Type == "bottomDivergence" && d.Confirmed {
			found = true
		}
	}
	if !found {
		t.Error("下跌趋势力度减弱时应确认底背驰")
	}
}

// TestDivergence_NoTrend 验证：无中枢列表时 → 无背驰信号。
func TestDivergence_NoTrend(t *testing.T) {
	dp := NewDivergenceProcessor()

	strokes := []*stroke{
		mkDivStroke(0, 30, 60, types.DirectionUp),
		mkDivStroke(1, 60, 45, types.DirectionDown),
	}

	divs := dp.Process("TEST", strokes, nil, nil)

	if len(divs) != 0 {
		t.Errorf("无中枢时期望 0 个背驰, 实际 %d", len(divs))
	}
}

// TestDivergence_CompletedTrendSkipped 验证：已完成的走势类型不重复检测。
func TestDivergence_CompletedTrendSkipped(t *testing.T) {
	dp := NewDivergenceProcessor()

	strokes := []*stroke{
		mkDivStroke(0, 30, 60, types.DirectionUp),
		mkDivStroke(1, 60, 45, types.DirectionDown),
		mkDivStroke(2, 45, 85, types.DirectionUp),
		mkDivStroke(3, 85, 55, types.DirectionDown),
		mkDivStroke(4, 55, 90, types.DirectionUp),
	}
	patterns := []*trendPattern{
		{Index: 0, Type: "trend", Direction: types.DirectionUp, PivotZoneIDs: []int{0, 1},
			StartStrokeIdx: 0, EndStrokeIdx: 4, Completed: true}, // 已完成
	}

	divs := dp.Process("TEST", strokes, nil, patterns)

	if len(divs) != 0 {
		t.Errorf("已完成的走势不检测背驰, 期望 0, 实际 %d", len(divs))
	}
}

// TestDivergence_EmptyEntryOrExit 验证：进入段或离开段为空时跳过。
func TestDivergence_EmptyEntryOrExit(t *testing.T) {
	dp := NewDivergenceProcessor()

	strokes := []*stroke{
		mkDivStroke(0, 10, 5, types.DirectionDown),
		mkDivStroke(1, 5, 12, types.DirectionUp),
		mkDivStroke(2, 12, 6, types.DirectionDown),
		mkDivStroke(3, 6, 18, types.DirectionUp),
	}
	// 中枢从 index 0 开始 → 进入段为空
	pivotZones := []*pivotZone{
		{index: 0, StartStrokeIdx: 0, EndStrokeIdx: 2, ZG: 12, ZD: 6, Direction: types.DirectionUp, SegmentsCount: 3},
	}
	patterns := []*trendPattern{
		{Index: 0, Type: "consolidation", Direction: types.DirectionUp, PivotZoneIDs: []int{0},
			StartStrokeIdx: 0, EndStrokeIdx: 3, Completed: false},
	}

	divs := dp.Process("TEST", strokes, pivotZones, patterns)

	if len(divs) != 0 {
		t.Errorf("进入段为空时应跳过, 期望 0, 实际 %d", len(divs))
	}
}

// TestDivergence_Reset 验证：重置后状态清空。
func TestDivergence_Reset(t *testing.T) {
	dp := NewDivergenceProcessor()

	strokes := []*stroke{
		mkDivStroke(0, 10, 5, types.DirectionDown),
		mkDivStroke(1, 5, 12, types.DirectionUp),
		mkDivStroke(2, 12, 6, types.DirectionDown),
	}
	pivotZones := []*pivotZone{
		{index: 0, StartStrokeIdx: 1, EndStrokeIdx: 3, ZG: 12, ZD: 6, Direction: types.DirectionUp, SegmentsCount: 3},
	}
	patterns := []*trendPattern{
		{Index: 0, Type: "consolidation", Direction: types.DirectionUp, PivotZoneIDs: []int{0},
			StartStrokeIdx: 0, EndStrokeIdx: 2, Completed: false},
	}

	dp.Process("TEST", strokes, pivotZones, patterns)
	dp.Reset("TEST")

	divs := dp.Load("TEST")
	if len(divs) != 0 {
		t.Errorf("重置后期望 0 个背驰, 实际 %d", len(divs))
	}
}
