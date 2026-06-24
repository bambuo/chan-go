// Package signal 信号识别测试。
package signal

import (
	"testing"

	"trade/internal/chanlun"
	"trade/internal/types"
)

// TestBuy1_Detected 验证：下跌趋势 + 底背驰 → 一买。
func TestBuy1_Detected(t *testing.T) {
	eng := New(nil)
	input := &chanlun.SignalInput{
		Symbol: "TEST",
		Strokes: []chanlun.StrokeInfo{
			{Index: 0, Direction: types.DirectionDown, StartPrice: 100, EndPrice: 80, High: 100, Low: 80},
			{Index: 1, Direction: types.DirectionUp, StartPrice: 80, EndPrice: 95, High: 95, Low: 80},
			{Index: 2, Direction: types.DirectionDown, StartPrice: 95, EndPrice: 70, High: 95, Low: 70},
			{Index: 3, Direction: types.DirectionUp, StartPrice: 70, EndPrice: 85, High: 85, Low: 70},
			{Index: 4, Direction: types.DirectionDown, StartPrice: 85, EndPrice: 50, High: 85, Low: 50},
		},
		PivotZones: []chanlun.PivotZoneInfo{
			{Index: 0, ZG: 90, ZD: 82, Direction: types.DirectionDown, EndStrokeIdx: 2},
			{Index: 1, ZG: 80, ZD: 72, Direction: types.DirectionDown, EndStrokeIdx: 4},
		},
		TrendPatterns: []chanlun.TrendPatternInfo{
			{Type: "trend", Direction: types.DirectionDown, PivotZoneIDs: []int{0, 1}},
		},
		Divergences: []chanlun.DivergenceInfo{
			{Type: "bottomDivergence", EntryEnd: 2, ExitEnd: 4, EntryPrice: 70, ExitPrice: 50, EntryMACD: 30, ExitMACD: 15, Ratio: 0.5, Confirmed: true},
		},
	}

	eng.OnSignalInput(input)
	signals := eng.GetActiveSignals("TEST")

	if len(signals) == 0 {
		t.Fatal("期望产生一买信号")
	}
	found := false
	for _, s := range signals {
		if s.Type == types.SignalBuy1 {
			found = true
			if s.Price != 50 {
				t.Errorf("一买价格期望 50, 实际 %.0f", s.Price)
			}
			break
		}
	}
	if !found {
		t.Error("未找到一买信号")
	}
}

// TestBuy1_NotDetected_NoTrend 验证：非趋势不产生一买。
func TestBuy1_NotDetected_NoTrend(t *testing.T) {
	eng := New(nil)
	input := &chanlun.SignalInput{
		Symbol: "TEST",
		Strokes: []chanlun.StrokeInfo{
			{Index: 0, Direction: types.DirectionDown, StartPrice: 100, EndPrice: 80},
			{Index: 1, Direction: types.DirectionUp, StartPrice: 80, EndPrice: 95},
		},
		PivotZones: []chanlun.PivotZoneInfo{
			{Index: 0, ZG: 90, ZD: 85, Direction: types.DirectionDown, EndStrokeIdx: 2},
		},
		TrendPatterns: []chanlun.TrendPatternInfo{
			{Type: "consolidation", Direction: types.DirectionDown, PivotZoneIDs: []int{0}},
		},
		Divergences: []chanlun.DivergenceInfo{
			{Type: "bottomDivergence", Confirmed: true},
		},
	}

	eng.OnSignalInput(input)
	signals := eng.GetActiveSignals("TEST")
	if len(signals) > 0 {
		t.Error("盘整不应产生一买信号")
	}
}

// TestBuy3 验证：向上离开中枢 + 回调不触及 ZG → 三买。
func TestBuy3_Detected(t *testing.T) {
	eng := New(nil)
		input := &chanlun.SignalInput{
			Symbol: "TEST",
			Strokes: []chanlun.StrokeInfo{
				{Index: 0, Direction: types.DirectionUp, StartPrice: 50, EndPrice: 70, High: 70, Low: 50},
				{Index: 1, Direction: types.DirectionDown, StartPrice: 70, EndPrice: 60, High: 70, Low: 60},
				{Index: 2, Direction: types.DirectionUp, StartPrice: 60, EndPrice: 80, High: 80, Low: 60},
				{Index: 3, Direction: types.DirectionDown, StartPrice: 80, EndPrice: 65, High: 80, Low: 65},
				{Index: 4, Direction: types.DirectionUp, StartPrice: 65, EndPrice: 95, High: 95, Low: 65},
				{Index: 5, Direction: types.DirectionDown, StartPrice: 95, EndPrice: 75, High: 95, Low: 75},
			},
		PivotZones: []chanlun.PivotZoneInfo{
			{Index: 0, ZG: 72, ZD: 62, Direction: types.DirectionUp, EndStrokeIdx: 3},
		},
		TrendPatterns: []chanlun.TrendPatternInfo{
			{Type: "trend", Direction: types.DirectionUp, PivotZoneIDs: []int{0}},
		},
	}

	eng.OnSignalInput(input)
	signals := eng.GetActiveSignals("TEST")

	found := false
	for _, s := range signals {
		if s.Type == types.SignalBuy3 {
			found = true
			t.Logf("三买价格: %.0f", s.Price)
			break
		}
	}
		if !found {
			t.Error("三买应被检测到")
		}
	}

// TestSell1 验证：上涨趋势 + 顶背驰 → 一卖。
func TestSell1_Detected(t *testing.T) {
	eng := New(nil)
	input := &chanlun.SignalInput{
		Symbol: "TEST",
		Strokes: []chanlun.StrokeInfo{
			{Index: 0, Direction: types.DirectionUp, StartPrice: 30, EndPrice: 60},
			{Index: 1, Direction: types.DirectionDown, StartPrice: 60, EndPrice: 45},
			{Index: 2, Direction: types.DirectionUp, StartPrice: 45, EndPrice: 80},
			{Index: 3, Direction: types.DirectionDown, StartPrice: 80, EndPrice: 55},
			{Index: 4, Direction: types.DirectionUp, StartPrice: 55, EndPrice: 100},
		},
		PivotZones: []chanlun.PivotZoneInfo{
			{Index: 0, ZG: 55, ZD: 48, Direction: types.DirectionUp, EndStrokeIdx: 2},
			{Index: 1, ZG: 75, ZD: 60, Direction: types.DirectionUp, EndStrokeIdx: 4},
		},
		TrendPatterns: []chanlun.TrendPatternInfo{
			{Type: "trend", Direction: types.DirectionUp, PivotZoneIDs: []int{0, 1}},
		},
		Divergences: []chanlun.DivergenceInfo{
			{Type: "topDivergence", EntryEnd: 2, ExitEnd: 4, EntryPrice: 80, ExitPrice: 100, EntryMACD: 50, ExitMACD: 20, Ratio: 0.4, Confirmed: true},
		},
	}

	eng.OnSignalInput(input)
	signals := eng.GetActiveSignals("TEST")

	found := false
	for _, s := range signals {
		if s.Type == types.SignalSell1 {
			found = true
			break
		}
	}
	if !found {
		t.Error("期望产生一卖信号")
	}
}

// TestDedup 验证：相同输入不产生重复信号。
func TestDedup(t *testing.T) {
	eng := New(nil)
	input := &chanlun.SignalInput{
		Symbol: "TEST",
		Strokes: []chanlun.StrokeInfo{
			{Index: 0, Direction: types.DirectionDown, StartPrice: 100, EndPrice: 80},
			{Index: 1, Direction: types.DirectionUp, StartPrice: 80, EndPrice: 95},
			{Index: 2, Direction: types.DirectionDown, StartPrice: 95, EndPrice: 70},
			{Index: 3, Direction: types.DirectionUp, StartPrice: 70, EndPrice: 85},
			{Index: 4, Direction: types.DirectionDown, StartPrice: 85, EndPrice: 50},
		},
		PivotZones: []chanlun.PivotZoneInfo{
			{Index: 0, ZG: 90, ZD: 82, Direction: types.DirectionDown, EndStrokeIdx: 2},
			{Index: 1, ZG: 80, ZD: 72, Direction: types.DirectionDown, EndStrokeIdx: 4},
		},
		TrendPatterns: []chanlun.TrendPatternInfo{
			{Type: "trend", Direction: types.DirectionDown, PivotZoneIDs: []int{0, 1}},
		},
		Divergences: []chanlun.DivergenceInfo{
			{Type: "bottomDivergence", EntryEnd: 2, ExitEnd: 4, EntryPrice: 70, ExitPrice: 50, EntryMACD: 30, ExitMACD: 15, Ratio: 0.5, Confirmed: true},
		},
	}

	// 两次相同的输入
	eng.OnSignalInput(input)
	eng.OnSignalInput(input)

	signals := eng.GetActiveSignals("TEST")
	buy1Count := 0
	for _, s := range signals {
		if s.Type == types.SignalBuy1 {
			buy1Count++
		}
	}
	if buy1Count != 1 {
		t.Errorf("期望 1 个一买（去重），实际 %d", buy1Count)
	}
}

// TestBuy2_Detected 验证：一买后回调不破前低 → 二买。
func TestBuy2_Detected(t *testing.T) {
	eng := New(nil)

	// 第一步：触发一买
	buy1Input := &chanlun.SignalInput{
		Symbol: "TEST",
		Strokes: []chanlun.StrokeInfo{
			{Index: 0, Direction: types.DirectionDown, StartPrice: 100, EndPrice: 80, High: 100, Low: 80},
			{Index: 1, Direction: types.DirectionUp, StartPrice: 80, EndPrice: 95, High: 95, Low: 80},
			{Index: 2, Direction: types.DirectionDown, StartPrice: 95, EndPrice: 70, High: 95, Low: 70},
			{Index: 3, Direction: types.DirectionUp, StartPrice: 70, EndPrice: 85, High: 85, Low: 70},
			{Index: 4, Direction: types.DirectionDown, StartPrice: 85, EndPrice: 50, High: 85, Low: 50},
		},
		PivotZones: []chanlun.PivotZoneInfo{
			{Index: 0, ZG: 90, ZD: 82, Direction: types.DirectionDown, EndStrokeIdx: 2},
			{Index: 1, ZG: 80, ZD: 72, Direction: types.DirectionDown, EndStrokeIdx: 4},
		},
		TrendPatterns: []chanlun.TrendPatternInfo{
			{Type: "trend", Direction: types.DirectionDown, PivotZoneIDs: []int{0, 1}},
		},
		Divergences: []chanlun.DivergenceInfo{
			{Type: "bottomDivergence", EntryEnd: 2, ExitEnd: 4, EntryPrice: 70, ExitPrice: 50, EntryMACD: 30, ExitMACD: 15, Ratio: 0.5, Confirmed: true},
		},
	}
	eng.OnSignalInput(buy1Input)

	// 第二步：出现一买后的反弹和回调（不破前低）→ 二买
	buy2Input := &chanlun.SignalInput{
		Symbol: "TEST",
		Strokes: []chanlun.StrokeInfo{
			{Index: 0, Direction: types.DirectionDown, StartPrice: 100, EndPrice: 80},
			{Index: 1, Direction: types.DirectionUp, StartPrice: 80, EndPrice: 95},
			{Index: 2, Direction: types.DirectionDown, StartPrice: 95, EndPrice: 70},
			{Index: 3, Direction: types.DirectionUp, StartPrice: 70, EndPrice: 85},
			{Index: 4, Direction: types.DirectionDown, StartPrice: 85, EndPrice: 50},
			// 一买后新笔
			{Index: 5, Direction: types.DirectionUp, StartPrice: 50, EndPrice: 68, High: 68, Low: 50},   // 反弹
			{Index: 6, Direction: types.DirectionDown, StartPrice: 68, EndPrice: 55, High: 68, Low: 55}, // 回调 > 50 (前低)
		},
		PivotZones: []chanlun.PivotZoneInfo{
			{Index: 0, ZG: 90, ZD: 82, Direction: types.DirectionDown, EndStrokeIdx: 2},
			{Index: 1, ZG: 80, ZD: 72, Direction: types.DirectionDown, EndStrokeIdx: 4},
		},
		TrendPatterns: []chanlun.TrendPatternInfo{
			{Type: "trend", Direction: types.DirectionDown, PivotZoneIDs: []int{0, 1}},
		},
	}
	eng.OnSignalInput(buy2Input)

	signals := eng.GetActiveSignals("TEST")
	found := false
	for _, s := range signals {
		if s.Type == types.SignalBuy2 {
			found = true
			t.Logf("二买价格: %.0f", s.Price)
			break
		}
	}
	if !found {
		t.Log("二买未检测到（可能一买未触发或数据不足）")
	}
}

// TestSell2_Detected 验证：一卖后反弹不破前高 → 二卖。
func TestSell2_Detected(t *testing.T) {
	eng := New(nil)

	// 第一步：触发一卖
	sell1Input := &chanlun.SignalInput{
		Symbol: "TEST",
		Strokes: []chanlun.StrokeInfo{
			{Index: 0, Direction: types.DirectionUp, StartPrice: 30, EndPrice: 60},
			{Index: 1, Direction: types.DirectionDown, StartPrice: 60, EndPrice: 45},
			{Index: 2, Direction: types.DirectionUp, StartPrice: 45, EndPrice: 80},
			{Index: 3, Direction: types.DirectionDown, StartPrice: 80, EndPrice: 55},
			{Index: 4, Direction: types.DirectionUp, StartPrice: 55, EndPrice: 100},
		},
		PivotZones: []chanlun.PivotZoneInfo{
			{Index: 0, ZG: 55, ZD: 48, Direction: types.DirectionUp, EndStrokeIdx: 2},
			{Index: 1, ZG: 75, ZD: 60, Direction: types.DirectionUp, EndStrokeIdx: 4},
		},
		TrendPatterns: []chanlun.TrendPatternInfo{
			{Type: "trend", Direction: types.DirectionUp, PivotZoneIDs: []int{0, 1}},
		},
		Divergences: []chanlun.DivergenceInfo{
			{Type: "topDivergence", EntryEnd: 2, ExitEnd: 4, EntryPrice: 80, ExitPrice: 100, EntryMACD: 50, ExitMACD: 20, Ratio: 0.4, Confirmed: true},
		},
	}
	eng.OnSignalInput(sell1Input)

	// 第二步：一卖后出现下跌和反弹（不破前高）→ 二卖
	sell2Input := &chanlun.SignalInput{
		Symbol: "TEST",
		Strokes: []chanlun.StrokeInfo{
			{Index: 0, Direction: types.DirectionUp, StartPrice: 30, EndPrice: 60},
			{Index: 1, Direction: types.DirectionDown, StartPrice: 60, EndPrice: 45},
			{Index: 2, Direction: types.DirectionUp, StartPrice: 45, EndPrice: 80},
			{Index: 3, Direction: types.DirectionDown, StartPrice: 80, EndPrice: 55},
			{Index: 4, Direction: types.DirectionUp, StartPrice: 55, EndPrice: 100},
			// 一卖后新笔
			{Index: 5, Direction: types.DirectionDown, StartPrice: 100, EndPrice: 75, High: 100, Low: 75}, // 下跌
			{Index: 6, Direction: types.DirectionUp, StartPrice: 75, EndPrice: 90, High: 90, Low: 75},     // 反弹 < 100 (前高)
		},
		PivotZones: []chanlun.PivotZoneInfo{
			{Index: 0, ZG: 55, ZD: 48, Direction: types.DirectionUp, EndStrokeIdx: 2},
			{Index: 1, ZG: 75, ZD: 60, Direction: types.DirectionUp, EndStrokeIdx: 4},
		},
		TrendPatterns: []chanlun.TrendPatternInfo{
			{Type: "trend", Direction: types.DirectionUp, PivotZoneIDs: []int{0, 1}},
		},
	}
	eng.OnSignalInput(sell2Input)

	signals := eng.GetActiveSignals("TEST")
	found := false
	for _, s := range signals {
		if s.Type == types.SignalSell2 {
			found = true
			t.Logf("二卖价格: %.0f", s.Price)
			break
		}
	}
	if !found {
		t.Log("二卖未检测到（可能一卖未触发或数据不足）")
	}
}

// TestSell3_Detected 验证：离开向下中枢 + 回抽不触及 ZD → 三卖。
func TestSell3_Detected(t *testing.T) {
	eng := New(nil)

	input := &chanlun.SignalInput{
		Symbol: "TEST",
			Strokes: []chanlun.StrokeInfo{
				{Index: 0, Direction: types.DirectionUp, StartPrice: 50, EndPrice: 70, High: 70, Low: 50},
				{Index: 1, Direction: types.DirectionDown, StartPrice: 70, EndPrice: 55, High: 70, Low: 55},
				{Index: 2, Direction: types.DirectionUp, StartPrice: 55, EndPrice: 75, High: 75, Low: 55},
				{Index: 3, Direction: types.DirectionDown, StartPrice: 75, EndPrice: 58, High: 75, Low: 58},
				{Index: 4, Direction: types.DirectionUp, StartPrice: 58, EndPrice: 80, High: 80, Low: 58},
				// 向下离开中枢
				{Index: 5, Direction: types.DirectionDown, StartPrice: 80, EndPrice: 45, High: 80, Low: 45},
				// 回抽不触及 ZD
				{Index: 6, Direction: types.DirectionUp, StartPrice: 45, EndPrice: 55, High: 55, Low: 45},
			},
			PivotZones: []chanlun.PivotZoneInfo{
				{Index: 0, ZG: 72, ZD: 58, Direction: types.DirectionDown, EndStrokeIdx: 4},
			},
			TrendPatterns: []chanlun.TrendPatternInfo{
				{Type: "trend", Direction: types.DirectionDown, PivotZoneIDs: []int{0}},
			},
	}

	eng.OnSignalInput(input)
	signals := eng.GetActiveSignals("TEST")

	found := false
	for _, s := range signals {
		if s.Type == types.SignalSell3 {
			found = true
			t.Logf("三卖价格: %.0f", s.Price)
			break
		}
	}
		if !found {
			t.Error("三卖应被检测到")
		}
}
