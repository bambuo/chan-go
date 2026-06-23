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
