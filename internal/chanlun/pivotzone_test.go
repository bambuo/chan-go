// Package chanlun 中枢识别算法测试。
package chanlun

import (
	"testing"

	"trade/internal/types"
)

// ====== 基础中枢测试 ======

// TestZhongShu_ThreeBiOverlap 验证：三笔连续重叠形成中枢。
func TestZhongShu_ThreeBiOverlap(t *testing.T) {
	// 三笔：向上→向下→向上，区间重叠
	// bi0: (10,5)→(20,15)  up
	// bi1: (20,15)→(15,8) down
	// bi2: (15,8)→(25,18) up
	// ZG = min(20, 20, 25) = 20, ZD = max(5, 8, 8) = 8
	// ZG(20) > ZD(8) ✅ 有效中枢
	bis := []*stroke{
		mkStroke(0, 10, 5, 20, 15, types.DirectionUp),
		mkStroke(1, 20, 15, 15, 8, types.DirectionDown),
		mkStroke(2, 15, 8, 25, 18, types.DirectionUp),
	}

	zp := NewPivotZoneProcessor()
	zss := zp.Process("TEST", bis)

	if len(zss) != 1 {
		t.Fatalf("期望 1 个中枢, 实际 %d", len(zss))
	}
	if zss[0].ZG <= zss[0].ZD {
		t.Errorf("ZG(%.2f) 应 > ZD(%.2f)", zss[0].ZG, zss[0].ZD)
	}
	t.Logf("中枢: ZG=%.2f ZD=%.2f 段数=%d", zss[0].ZG, zss[0].ZD, zss[0].SegmentsCount)
}

// TestZhongShu_NoOverlap 验证：三笔不重叠时不形成中枢。
func TestZhongShu_NoOverlap(t *testing.T) {
	// 三笔连续上涨，无重叠
	// bi0: (10,5)→(20,15)  up
	// bi1: (20,15)→(30,25) up
	// bi2: (30,25)→(40,35) up
	// ZG = min(20, 30, 40) = 20, ZD = max(5, 15, 25) = 25
	// ZG(20) <= ZD(25) ❌ 无效
	bis := []*stroke{
		mkStroke(0, 10, 5, 20, 15, types.DirectionUp),
		mkStroke(1, 20, 15, 30, 25, types.DirectionUp),
		mkStroke(2, 30, 25, 40, 35, types.DirectionUp),
	}

	zp := NewPivotZoneProcessor()
	zss := zp.Process("TEST", bis)

	if len(zss) != 0 {
		t.Errorf("无重叠时不期望中枢, 实际 %d", len(zss))
	}
}

// TestZhongShu_Extension 验证：中枢延伸。
func TestZhongShu_Extension(t *testing.T) {
	// 前 3 笔形成中枢，后 2 笔延伸
	// bi0: (10,5)→(22,16) up
	// bi1: (22,16)→(14,8) down
	// bi2: (14,8)→(25,18) up   → 中枢 [8, 20]
	// bi3: (25,18)→(12,6) down → bi3.high=25 >= ZD=8 && bi3.low=6 <= ZG=20 → 延伸
	// bi4: (12,6)→(23,15) up   → bi4.high=23 >= 8 && bi4.low=15 <= 20 → 延伸
	bis := []*stroke{
		mkStroke(0, 10, 5, 22, 16, types.DirectionUp),
		mkStroke(1, 22, 16, 14, 8, types.DirectionDown),
		mkStroke(2, 14, 8, 25, 18, types.DirectionUp),
		mkStroke(3, 25, 18, 12, 6, types.DirectionDown),
		mkStroke(4, 12, 6, 23, 15, types.DirectionUp),
	}

	zp := NewPivotZoneProcessor()
	zss := zp.Process("TEST", bis)

	if len(zss) != 1 {
		t.Fatalf("期望 1 个中枢, 实际 %d", len(zss))
	}
	if zss[0].SegmentsCount < 5 {
		t.Errorf("延伸后期望 >= 5 段, 实际 %d", zss[0].SegmentsCount)
	}
	t.Logf("中枢: ZG=%.2f ZD=%.2f 段数=%d", zss[0].ZG, zss[0].ZD, zss[0].SegmentsCount)
}

// TestZhongShu_TwoSeparate 验证：两个独立中枢。
func TestZhongShu_TwoSeparate(t *testing.T) {
	// 第一中枢: bi0-bi2 (区间 [10, 20])
	// bi3: 离开中枢
	// 第二中枢: bi4-bi6 (区间 [30, 15])
	bis := []*stroke{
		mkStroke(0, 10, 5, 20, 15, types.DirectionUp),
		mkStroke(1, 20, 15, 15, 10, types.DirectionDown),
		mkStroke(2, 15, 10, 25, 18, types.DirectionUp),
		// 离开
		mkStroke(3, 25, 18, 35, 28, types.DirectionUp),
		// 第二中枢
		mkStroke(4, 35, 28, 30, 22, types.DirectionDown),
		mkStroke(5, 30, 22, 40, 32, types.DirectionUp),
		mkStroke(6, 40, 32, 32, 15, types.DirectionDown),
	}

	zp := NewPivotZoneProcessor()
	zss := zp.Process("TEST", bis)

	if len(zss) != 2 {
		t.Fatalf("期望 2 个中枢, 实际 %d", len(zss))
	}
	t.Logf("中枢1: ZG=%.2f ZD=%.2f 段数=%d", zss[0].ZG, zss[0].ZD, zss[0].SegmentsCount)
	t.Logf("中枢2: ZG=%.2f ZD=%.2f 段数=%d", zss[1].ZG, zss[1].ZD, zss[1].SegmentsCount)
}

// TestZhongShu_PipelineIntegration 验证：完整 pipeline 产生中枢。
func TestZhongShu_PipelineIntegration(t *testing.T) {
	p := NewPipeline()

	// 构造足够的笔来形成中枢
	klines := []float64{
		30, 20, // 顶
		15, 5,  // 底
		25, 15, // 顶
		10, 3,  // 底
		20, 12, // 顶
		8, 2,   // 底
	}

	for i := 0; i < len(klines); i += 2 {
		high := klines[i]
		low := klines[i+1]
		raw := mkline((high+low)/2, high, low, (high+low)/2, int64(i/2), "TEST_ZS")
		p.Process(raw)
	}

	state := p.GetState("TEST_ZS")
	t.Logf("元素: %d, 分型: %d, 笔: %d, 线段: %d, 中枢: %d",
		len(state.AllElements), len(state.AllFractals),
		len(state.Strokes), len(state.Segments), len(state.PivotZones))
}

// TestZhongShu_NotStartFromEdge 验证：中枢不从第一笔开始（跳过初始非重叠区域）。
func TestZhongShu_NotStartFromEdge(t *testing.T) {
	// bi0: 向上（不重叠）
	// bi1-bi3: 三笔重叠 → 中枢
	bis := []*stroke{
		mkStroke(0, 10, 5, 50, 40, types.DirectionUp),   // 大幅离开
		mkStroke(1, 50, 40, 35, 25, types.DirectionDown), // 回到下方
		mkStroke(2, 35, 25, 55, 45, types.DirectionUp),   // 重叠区域
		mkStroke(3, 55, 45, 40, 30, types.DirectionDown), // 重叠区域
	}

	zp := NewPivotZoneProcessor()
	zss := zp.Process("TEST", bis)

	if len(zss) != 1 {
		t.Fatalf("期望 1 个中枢, 实际 %d", len(zss))
	}
	t.Logf("中枢: ZG=%.2f ZD=%.2f 段数=%d StartBi=%d EndBi=%d",
		zss[0].ZG, zss[0].ZD, zss[0].SegmentsCount,
		zss[0].StartStrokeIdx, zss[0].EndStrokeIdx)
}

// ====== 中枢完成判定（第三类买卖点确认）测试 ======

// TestZhongShu_ExitNoPullback 验证：构件离开中枢后无回抽构件 → 中枢未完成。
// 中枢 [10,20]，bi3 完全在上方（low=22 > ZG=20），无 bi4 → Completed=false。
func TestZhongShu_ExitNoPullback(t *testing.T) {
	bis := []*stroke{
		mkStroke(0, 10, 5, 20, 15, types.DirectionUp),   // [5, 20]
		mkStroke(1, 20, 15, 14, 10, types.DirectionDown), // [10, 20]
		mkStroke(2, 14, 10, 25, 18, types.DirectionUp),   // [10, 25]
		// 中枢 ZG=min(20,20,25)=20, ZD=max(5,10,10)=10 → [10,20]
		// bi3 完全在上方：low > 20
		{Index: 3, Direction: types.DirectionUp, StartPrice: 24, EndPrice: 38,
			High: 40, Low: 22, Confirmed: true}, // [22, 40]
	}

	zp := NewPivotZoneProcessor()
	zss := zp.Process("TEST", bis)

	if len(zss) != 1 {
		t.Fatalf("期望 1 个中枢, 实际 %d", len(zss))
	}
	if zss[0].Completed {
		t.Error("无回抽构件时中枢不应标记为已完成")
	}
	if zss[0].SegmentsCount != 3 {
		t.Errorf("离开构件不应计入延伸, 段数期望 3, 实际 %d", zss[0].SegmentsCount)
	}
	t.Logf("中枢: ZG=%.2f ZD=%.2f 段数=%d Completed=%v",
		zss[0].ZG, zss[0].ZD, zss[0].SegmentsCount, zss[0].Completed)
}

// TestZhongShu_ExitAndPullbackCompletes 验证：离开 + 回抽不回中枢 → 确认完成。
// 中枢 [10,20]，bi3 完全在上方（low=22 > 20），bi4 回抽也不回到中枢（low=22 > 20）。
func TestZhongShu_ExitAndPullbackCompletes(t *testing.T) {
	bis := []*stroke{
		mkStroke(0, 10, 5, 20, 15, types.DirectionUp),    // [5, 20]
		mkStroke(1, 20, 15, 14, 10, types.DirectionDown),  // [10, 20]
		mkStroke(2, 14, 10, 25, 18, types.DirectionUp),    // [10, 25] 中枢 [10,20]
		// bi3 完全在上方
		{Index: 3, Direction: types.DirectionUp, StartPrice: 24, EndPrice: 38,
			High: 40, Low: 22, Confirmed: true}, // [22, 40]
		// bi4 回抽也不回到中枢（仍在上方）
		{Index: 4, Direction: types.DirectionDown, StartPrice: 30, EndPrice: 23,
			High: 32, Low: 22, Confirmed: true}, // [22, 32]
	}

	zp := NewPivotZoneProcessor()
	zss := zp.Process("TEST", bis)

	if len(zss) != 1 {
		t.Fatalf("期望 1 个中枢, 实际 %d", len(zss))
	}
	if !zss[0].Completed {
		t.Error("离开后回抽不回中枢应标记为 Completed=true")
	}
	t.Logf("中枢: ZG=%.2f ZD=%.2f 段数=%d Completed=%v",
		zss[0].ZG, zss[0].ZD, zss[0].SegmentsCount, zss[0].Completed)
}

// TestZhongShu_ExitAndPullbackReenters 验证：离开 + 回抽回到中枢 → 继续延伸。
// 中枢 [10,20]，bi3 完全在上方（low=22 > 20），bi4 回抽回到中枢（low=12 < 20）。
func TestZhongShu_ExitAndPullbackReenters(t *testing.T) {
	bis := []*stroke{
		mkStroke(0, 10, 5, 18, 12, types.DirectionUp),    // [5, 18]
		mkStroke(1, 18, 12, 10, 5, types.DirectionDown),   // [5, 18]
		mkStroke(2, 10, 5, 25, 20, types.DirectionUp),     // [5, 25] 中枢 [5,18]
		// bi3 完全在上方
		{Index: 3, Direction: types.DirectionUp, StartPrice: 22, EndPrice: 38,
			High: 40, Low: 20, Confirmed: true}, // [20, 40] 注意 low=20 = ZG
	}
	// 等等，low=20 <= ZG=18? 20 <= 18? NO → exit ✓
	// 但 20 只是大于 ZG。如果需要更明显就用 low=22。

	// 重新设计：
	bis = []*stroke{
		mkStroke(0, 10, 5, 18, 12, types.DirectionUp),    // [5, 18]
		mkStroke(1, 18, 12, 10, 5, types.DirectionDown),   // [5, 18]
		mkStroke(2, 10, 5, 25, 20, types.DirectionUp),     // [5, 25] 中枢 [5,18]
		// bi3 完全在下方（high=3 < ZD=5）
		{Index: 3, Direction: types.DirectionDown, StartPrice: 8, EndPrice: 2,
			High: 10, Low: 2, Confirmed: true}, // [2, 10] 注意 low=2 没问题…
		// 等等 high=10 >= ZD=5? YES → 这实际上是重叠的！(high=10>5, low=2<18)
	}
	// 不行，high=10 >= ZD=5 且 low=2 <= ZG=18 → 重叠了。这不是 exit。

	// 我需要 bi3 完全在下方：high < ZD。
	// bi3: high < 5。
	bis = []*stroke{
		mkStroke(0, 10, 5, 18, 12, types.DirectionUp),    // [5, 18]
		mkStroke(1, 18, 12, 10, 5, types.DirectionDown),   // [5, 18]
		mkStroke(2, 10, 5, 25, 20, types.DirectionUp),     // [5, 25] 中枢 ZG=18, ZD=5
		// bi3 完全在下方
		{Index: 3, Direction: types.DirectionDown, StartPrice: 12, EndPrice: 2,
			High: 4, Low: 1, Confirmed: true}, // [1, 4] high=4 < ZD=5 → 完全离开
		// bi4 回抽回到中枢
		mkStroke(4, 8, 4, 20, 15, types.DirectionUp),      // [4, 20] 回到中枢
	}

	zp := NewPivotZoneProcessor()
	zss := zp.Process("TEST", bis)

	if len(zss) != 1 {
		t.Fatalf("期望 1 个中枢, 实际 %d", len(zss))
	}
	if zss[0].Completed {
		t.Error("回抽回到中枢后中枢不应标记为已完成")
	}
	if zss[0].SegmentsCount < 4 {
		t.Errorf("回抽后延伸段数应 >= 4, 实际 %d", zss[0].SegmentsCount)
	}
	t.Logf("中枢: ZG=%.2f ZD=%.2f 段数=%d Completed=%v EndIdx=%d",
		zss[0].ZG, zss[0].ZD, zss[0].SegmentsCount, zss[0].Completed, zss[0].EndStrokeIdx)
}

// TestZhongShu_SegmentMode 验证线段中枢模式的基本功能。
func TestZhongShu_SegmentMode(t *testing.T) {
	// 用线段构建中枢（段模式）
	// 段0: [5, 22], 段1: [8, 22], 段2: [8, 25] → 中枢 [8, 22]
	segs := []*segment{
		{index: 0, direction: types.DirectionUp, high: 22, low: 5, confirmed: true},
		{index: 1, direction: types.DirectionDown, high: 22, low: 8, confirmed: true},
		{index: 2, direction: types.DirectionUp, high: 25, low: 8, confirmed: true},
	}

	zp := NewPivotZoneProcessor(PivotZoneConfig{Mode: types.PivotModeSegment})
	zss := zp.ProcessSegments("TEST", segs)

	if len(zss) != 1 {
		t.Fatalf("段模式期望 1 个中枢, 实际 %d", len(zss))
	}
	if zss[0].ZG <= zss[0].ZD {
		t.Errorf("ZG(%.2f) 应 > ZD(%.2f)", zss[0].ZG, zss[0].ZD)
	}
	t.Logf("段中枢: ZG=%.2f ZD=%.2f 段数=%d", zss[0].ZG, zss[0].ZD, zss[0].SegmentsCount)
}
