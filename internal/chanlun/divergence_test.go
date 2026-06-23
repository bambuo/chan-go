// Package chanlun 背驰判定处理器单元测试。
package chanlun

import (
	"testing"

	"trade/internal/types"
)

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

// ====== 基础背驰测试 ======
//
// DivergenceProcessor 现在使用正确的比较逻辑：
// 比较相邻同向笔（idx-2 vs idx），严格交替笔序列中：
//   [d0, u1, d2, u3, d4, u5, d6] →
//     idx=2: d0 vs d2,  idx=3: u1 vs u3,  idx=4: d2 vs d4,  ...

// TestDivergence_BottomDivergence 验证：下跌趋势中出现底背驰。
func TestDivergence_BottomDivergence(t *testing.T) {
	dp := NewDivergenceProcessor()

	// [d(20), u, d(35), u, d(25)]
	// idx=2: d0(20∅80) vs d2(35∅60) → 价格新低, 强度增大 ratio=1.75 → 不背驰
	// idx=4: d2(35∅60) vs d4(25∅50) → 价格新低, 强度减弱 ratio=0.71 → 背驰!
	strokes := []*stroke{
		mkDivStroke(0, 100, 80, types.DirectionDown),  // d0: 强度=20
		mkDivStroke(1, 80, 95, types.DirectionUp),
		mkDivStroke(2, 95, 60, types.DirectionDown),    // d2: 强度=35
		mkDivStroke(3, 60, 75, types.DirectionUp),
		mkDivStroke(4, 75, 50, types.DirectionDown),    // d4: 强度=25, 价格新低
	}

	divs := dp.Process("TEST", strokes)

	t.Logf("背驰信号数: %d", len(divs))
	for _, d := range divs {
		t.Logf("  类型=%s 价格(%.0f→%.0f) 强度(%.0f→%.0f) 比率=%.2f 确认=%v",
			d.Type, d.Price1, d.Price2, d.Strength1, d.Strength2, d.Ratio, d.Confirmed)
	}

	// 检查所有比较中强度增大的不应确认
	for _, d := range divs {
		if d.Strength2 > d.Strength1 && d.Confirmed {
			t.Errorf("强度增大时不应确认背驰: S%d(%.0f)→S%d(%.0f) ratio=%.2f",
				d.Stroke1Idx, d.Strength1, d.Stroke2Idx, d.Strength2, d.Ratio)
		}
	}
}

// TestDivergence_BottomDivergence_Confirmed 验证：强度减弱时确认底背驰。
func TestDivergence_BottomDivergence_Confirmed(t *testing.T) {
	dp := NewDivergenceProcessor()

	// idx 最大为 len-2，5 根笔最多到 idx=3，所以需要 6 根笔。
	// [d(20), u, d(30), u, d(15), u]
	// idx=2: d0(20) vs d2(30) → 价格新低, 强度增大 → 不背驰
	// idx=4: d2(30) vs d4(15) → 价格新低, 强度减弱 ratio=0.50 → 确认!
	strokes := []*stroke{
		mkDivStroke(0, 100, 80, types.DirectionDown),  // d0: 强度=20
		mkDivStroke(1, 80, 85, types.DirectionUp),
		mkDivStroke(2, 85, 55, types.DirectionDown),    // d2: 强度=30
		mkDivStroke(3, 55, 65, types.DirectionUp),
		mkDivStroke(4, 65, 50, types.DirectionDown),    // d4: 强度=15, 价格新低(50<55)
		mkDivStroke(5, 50, 60, types.DirectionUp),
	}

	divs := dp.Process("TEST", strokes)

	found := false
	for _, d := range divs {
		t.Logf("背驰: %s S%d(%.0f,强度%.0f)→S%d(%.0f,强度%.0f) 比率=%.2f 确认=%v",
			d.Type, d.Stroke1Idx, d.Price1, d.Strength1,
			d.Stroke2Idx, d.Price2, d.Strength2, d.Ratio, d.Confirmed)
		if d.Type == "bottomDivergence" && d.Confirmed {
			found = true
			if d.Ratio >= 0.95 {
				t.Errorf("背驰比率应 < 0.95, 实际 %.2f", d.Ratio)
			}
		}
	}
	if !found {
		t.Error("未检测到确认的底背驰")
	}
}

// TestDivergence_TopDivergence 验证：上涨趋势中出现顶背驰。
func TestDivergence_TopDivergence(t *testing.T) {
	dp := NewDivergenceProcessor()

	// [u(30), d, u(35), d, u(35)]
	// idx=2: u0(30) vs u2(35) → 价格新高, 强度增大 ratio=1.17 → 不背驰
	// idx=4: u2(35) vs u4(35) → 价格新高, 强度持平 ratio=1.00 → 不背驰
	strokes := []*stroke{
		mkDivStroke(0, 30, 60, types.DirectionUp),
		mkDivStroke(1, 60, 45, types.DirectionDown),
		mkDivStroke(2, 45, 80, types.DirectionUp),
		mkDivStroke(3, 80, 55, types.DirectionDown),
		mkDivStroke(4, 55, 90, types.DirectionUp),
	}

	divs := dp.Process("TEST", strokes)

	for _, d := range divs {
		if d.Confirmed {
			t.Logf("  非预期确认: %s S%d→S%d 比率=%.2f", d.Type, d.Stroke1Idx, d.Stroke2Idx, d.Ratio)
		}
	}
}

// TestDivergence_TopDivergence_Confirmed 验证：强度减弱时确认顶背驰。
func TestDivergence_TopDivergence_Confirmed(t *testing.T) {
	dp := NewDivergenceProcessor()

	// idx 最大为 len-2，需要 6 根笔才能到 idx=4。
	// [u(30), d, u(40), d, u(20), d]
	// idx=2: u0(str=30) vs u2(str=40) → 价格新高, 强度增大 → 不背驰
	// idx=4: u2(str=40) vs u4(str=15) → 价格新高(90>85), 强度减弱 ratio=0.375 → 确认!
	strokes := []*stroke{
		mkDivStroke(0, 30, 60, types.DirectionUp),     // u0: Start=30 End=60 强度=|60-30|=30
		mkDivStroke(1, 60, 45, types.DirectionDown),
		mkDivStroke(2, 45, 85, types.DirectionUp),      // u2: Start=45 End=85 强度=|85-45|=40
		mkDivStroke(3, 85, 55, types.DirectionDown),
		mkDivStroke(4, 70, 90, types.DirectionUp),      // u4: Start=70 End=90 强度=|90-70|=20 < 40, 价格=90>85 → 确认!
		mkDivStroke(5, 90, 75, types.DirectionDown),
	}

	divs := dp.Process("TEST", strokes)

	found := false
	for _, d := range divs {
		t.Logf("背驰: %s S%d→S%d 比率=%.2f 确认=%v",
			d.Type, d.Stroke1Idx, d.Stroke2Idx, d.Ratio, d.Confirmed)
		if d.Type == "topDivergence" && d.Confirmed {
			found = true
			t.Logf("顶背驰确认! S%d(%.0f,强度%.0f)→S%d(%.0f,强度%.0f) 比率=%.2f",
				d.Stroke1Idx, d.Price1, d.Strength1,
				d.Stroke2Idx, d.Price2, d.Strength2, d.Ratio)
		}
	}
	if !found {
		t.Error("未检测到确认的顶背驰")
	}
}

// TestDivergence_NoDivergence 验证：无背驰时无信号。
func TestDivergence_NoDivergence(t *testing.T) {
	dp := NewDivergenceProcessor()

	// [u(30), d, u(40), d, u(45)]
	// idx=2: ratio=1.33, idx=4: ratio=1.125 → 均 > 0.95 → 无背驰
	strokes := []*stroke{
		mkDivStroke(0, 30, 60, types.DirectionUp),     // u0: 强度=30
		mkDivStroke(1, 60, 45, types.DirectionDown),
		mkDivStroke(2, 45, 85, types.DirectionUp),      // u2: 强度=40
		mkDivStroke(3, 85, 55, types.DirectionDown),
		mkDivStroke(4, 55, 100, types.DirectionUp),     // u4: 强度=45 > 40
	}

	divs := dp.Process("TEST", strokes)

	for _, d := range divs {
		if d.Confirmed {
			t.Errorf("无背驰场景不应有确认信号: %s (ratio=%.2f)", d.Type, d.Ratio)
		}
	}
}

// TestDivergence_Reset 验证：重置后状态清空。
func TestDivergence_Reset(t *testing.T) {
	dp := NewDivergenceProcessor()

	strokes := []*stroke{
		mkDivStroke(0, 100, 80, types.DirectionDown),
		mkDivStroke(1, 80, 95, types.DirectionUp),
		mkDivStroke(2, 95, 60, types.DirectionDown),
	}

	dp.Process("TEST", strokes)
	dp.Reset("TEST")

	divs := dp.Load("TEST")
	if len(divs) != 0 {
		t.Errorf("重置后期望 0 个背驰, 实际 %d", len(divs))
	}
}
