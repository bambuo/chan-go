// Package chanlun 走势类型分类处理器单元测试。
package chanlun

import (
	"testing"

	"trade/internal/types"
)

// ====== 走势类型分类测试 ======

// TestTrendPattern_Consolidation 验证：单个中枢 → 盘整。
func TestTrendPattern_Consolidation(t *testing.T) {
	tp := NewTrendPatternProcessor()

	strokes := []*stroke{
		mkStroke(0, 10, 5, 20, 15, types.DirectionUp),
		mkStroke(1, 20, 15, 15, 8, types.DirectionDown),
		mkStroke(2, 15, 8, 25, 18, types.DirectionUp),
	}
	pivotZones := []*pivotZone{
		{
			index:          0,
			StartStrokeIdx: 0,
			EndStrokeIdx:   2,
			ZG:             20,
			ZD:             8,
			Direction:      types.DirectionUp,
			SegmentsCount:  3,
			Completed:      true,
		},
	}

	patterns := tp.Process("TEST", strokes, pivotZones)

	if len(patterns) != 1 {
		t.Fatalf("期望 1 个走势类型, 实际 %d", len(patterns))
	}
	if patterns[0].Type != "consolidation" {
		t.Errorf("类型期望 consolidation, 实际 %s", patterns[0].Type)
	}
	if !patterns[0].Completed {
		t.Error("走势类型应标记为已完成")
	}
}

// TestTrendPattern_Trend 验证：两个同向非重叠中枢 → 趋势。
func TestTrendPattern_Trend(t *testing.T) {
	tp := NewTrendPatternProcessor()

	strokes := []*stroke{
		mkStroke(0, 10, 5, 20, 15, types.DirectionUp),    // bi0
		mkStroke(1, 20, 15, 15, 8, types.DirectionDown),  // bi1
		mkStroke(2, 15, 8, 25, 18, types.DirectionUp),    // bi2 → 中枢1
		mkStroke(3, 25, 18, 35, 28, types.DirectionUp),   // bi3 离开段
		mkStroke(4, 35, 28, 30, 22, types.DirectionDown), // bi4
		mkStroke(5, 30, 22, 40, 32, types.DirectionUp),   // bi5 → 中枢2
		mkStroke(6, 40, 32, 35, 25, types.DirectionDown), // bi6
	}

	pivotZones := []*pivotZone{
		{
			index:          0,
			StartStrokeIdx: 0,
			EndStrokeIdx:   2,
			ZG:             20,
			ZD:             10,
			Direction:      types.DirectionUp,
			SegmentsCount:  3,
			Completed:      true,
		},
		{
			index:          1,
			StartStrokeIdx: 4,
			EndStrokeIdx:   6,
			ZG:             35,
			ZD:             22,
			Direction:      types.DirectionUp,
			SegmentsCount:  3,
			Completed:      true,
		},
	}

	patterns := tp.Process("TEST", strokes, pivotZones)

	if len(patterns) == 0 {
		t.Fatal("期望至少 1 个走势类型")
	}

	foundTrend := false
	for _, p := range patterns {
		t.Logf("走势: 类型=%s 方向=%v 中枢数=%d", p.Type, p.Direction, len(p.PivotZoneIDs))
		if p.Type == "trend" {
			foundTrend = true
		}
	}
	if !foundTrend {
		t.Error("未检测到趋势")
	}
}

// TestTrendPattern_MultipleGroups 验证：中枢方向变化时分组。
func TestTrendPattern_MultipleGroups(t *testing.T) {
	tp := NewTrendPatternProcessor()

	strokes := []*stroke{
		mkStroke(0, 10, 5, 20, 15, types.DirectionUp),    // bi0
		mkStroke(1, 20, 15, 15, 8, types.DirectionDown),  // bi1
		mkStroke(2, 15, 8, 25, 18, types.DirectionUp),    // bi2 → 向上中枢1
		mkStroke(3, 25, 18, 30, 22, types.DirectionUp),   // bi3
		mkStroke(4, 30, 22, 20, 15, types.DirectionDown), // bi4
		mkStroke(5, 20, 15, 15, 10, types.DirectionDown), // bi5
		mkStroke(6, 15, 10, 25, 18, types.DirectionUp),   // bi6
		mkStroke(7, 25, 18, 15, 8, types.DirectionDown),  // bi7
		mkStroke(8, 15, 8, 10, 5, types.DirectionDown),   // bi8 → 向下中枢2
	}

	pivotZones := []*pivotZone{
		{
			index:          0,
			StartStrokeIdx: 0,
			EndStrokeIdx:   2,
			ZG:             20,
			ZD:             10,
			Direction:      types.DirectionUp,
			SegmentsCount:  3,
			Completed:      true,
		},
		{
			index:          1,
			StartStrokeIdx: 6,
			EndStrokeIdx:   8,
			ZG:             18,
			ZD:             8,
			Direction:      types.DirectionDown,
			SegmentsCount:  3,
			Completed:      true,
		},
	}

	patterns := tp.Process("TEST", strokes, pivotZones)

	if len(patterns) != 2 {
		t.Fatalf("方向变化后期望 2 个走势类型, 实际 %d", len(patterns))
	}

	t.Logf("走势1: 类型=%s 方向=%v", patterns[0].Type, patterns[0].Direction)
	t.Logf("走势2: 类型=%s 方向=%v", patterns[1].Type, patterns[1].Direction)

	if patterns[0].Direction == patterns[1].Direction {
		t.Error("方向变化后两个走势类型方向应不同")
	}
}

// TestTrendPattern_NoPivotZones 验证：无中枢时无走势类型。
func TestTrendPattern_NoPivotZones(t *testing.T) {
	tp := NewTrendPatternProcessor()

	strokes := []*stroke{
		mkStroke(0, 10, 5, 20, 15, types.DirectionUp),
		mkStroke(1, 20, 15, 30, 25, types.DirectionUp),
	}

	patterns := tp.Process("TEST", strokes, nil)

	if len(patterns) != 0 {
		t.Errorf("无中枢时期望 0 个走势类型, 实际 %d", len(patterns))
	}
}

// TestTrendPattern_Load 验证：Load 返回所有走势类型。
func TestTrendPattern_Load(t *testing.T) {
	tp := NewTrendPatternProcessor()

	strokes := []*stroke{
		mkStroke(0, 10, 5, 20, 15, types.DirectionUp),
		mkStroke(1, 20, 15, 15, 8, types.DirectionDown),
		mkStroke(2, 15, 8, 25, 18, types.DirectionUp),
	}
	pivotZones := []*pivotZone{
		{
			index:          0,
			StartStrokeIdx: 0,
			EndStrokeIdx:   2,
			ZG:             20,
			ZD:             8,
			Direction:      types.DirectionUp,
			SegmentsCount:  3,
			Completed:      true,
		},
	}

	tp.Process("TEST", strokes, pivotZones)
	loaded := tp.Load("TEST")

	if len(loaded) != 1 {
		t.Errorf("Load 期望 1 个走势类型, 实际 %d", len(loaded))
	}
}

// TestTrendPattern_Reset 验证：重置后状态清空。
func TestTrendPattern_Reset(t *testing.T) {
	tp := NewTrendPatternProcessor()

	strokes := []*stroke{
		mkStroke(0, 10, 5, 20, 15, types.DirectionUp),
		mkStroke(1, 20, 15, 15, 8, types.DirectionDown),
		mkStroke(2, 15, 8, 25, 18, types.DirectionUp),
	}
	pivotZones := []*pivotZone{
		{
			index:          0,
			StartStrokeIdx: 0,
			EndStrokeIdx:   2,
			ZG:             20,
			ZD:             8,
			Direction:      types.DirectionUp,
			SegmentsCount:  3,
			Completed:      true,
		},
	}

	tp.Process("TEST", strokes, pivotZones)
	tp.Reset("TEST")

	loaded := tp.Load("TEST")
	if len(loaded) != 0 {
		t.Errorf("重置后期望 0 个走势类型, 实际 %d", len(loaded))
	}
}
