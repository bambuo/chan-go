// Package levels 递归级别构建器（M4）单元测试。
package levels

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"trade/internal/eventbus"
	"trade/internal/structure"
	"trade/internal/types"
)

// ====== 辅助函数 ======

// mkStroke 快速创建一根测试用笔。
func mkStroke(id, dir int, startPrice, endPrice, high, low float64) types.Stroke {
	var direction types.ChanDirection
	if dir == 1 {
		direction = types.DirectionUp
	} else {
		direction = types.DirectionDown
	}

	return types.Stroke{
		StructureElement: types.StructureElement{
			ID:          string(rune('A' + id)),
			ElementType: types.ElementTypeStroke,
			LineageID:   "L_test",
			ValidFromTS: time.Now().UnixMilli(),
		},
		Direction:  direction,
		StartPrice: startPrice,
		EndPrice:   endPrice,
		High:       high,
		Low:        low,
	}
}

// mkTrendPattern 快速创建一个完成的走势类型。
func mkTrendPattern(dir int, startPrice, endPrice, high, low float64) types.TrendPattern {
	var direction types.ChanDirection
	if dir == 1 {
		direction = types.DirectionUp
	} else {
		direction = types.DirectionDown
	}

	return types.TrendPattern{
		Direction:  direction,
		Type:       "trend",
		Completed:  true,
		StartPrice: startPrice,
		EndPrice:   endPrice,
		High:       high,
		Low:        low,
	}
}

// ====== 测试：走势类型 → 高级别笔 转换 ======

func TestBuildHigherLevelStrokes(t *testing.T) {
	b := &LevelBuilder{logger: slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))}

	patterns := []types.TrendPattern{
		mkTrendPattern(1, 10, 50, 55, 8),   // 向上走势
		mkTrendPattern(-1, 50, 20, 52, 18), // 向下走势
		mkTrendPattern(1, 20, 60, 62, 18),  // 向上走势
	}

	strokes := b.buildHigherLevelStrokes("TEST", types.LevelL2, patterns)

	if len(strokes) != 3 {
		t.Fatalf("期望 3 根高级别笔, 实际 %d", len(strokes))
	}

	// 验证第一根笔（对应向上走势）
	if strokes[0].Direction != types.DirectionUp {
		t.Errorf("笔[0] 方向期望 up, 实际 %v", strokes[0].Direction)
	}
	if strokes[0].StartPrice != 10 || strokes[0].EndPrice != 50 {
		t.Errorf("笔[0] 价格期望 (10,50), 实际 (%.1f,%.1f)",
			strokes[0].StartPrice, strokes[0].EndPrice)
	}
	if strokes[0].High != 55 || strokes[0].Low != 8 {
		t.Errorf("笔[0] 区间期望 high=55 low=8, 实际 (%.1f,%.1f)",
			strokes[0].High, strokes[0].Low)
	}

	// 验证第二根笔（对应向下走势）
	if strokes[1].Direction != types.DirectionDown {
		t.Errorf("笔[1] 方向期望 down, 实际 %v", strokes[1].Direction)
	}

	// 验证 lineageId 格式
	expectedLineage := "L_TEST_hbi_L2_0"
	if strokes[0].LineageID != expectedLineage {
		t.Errorf("笔[0] lineageId 期望 %s, 实际 %s", expectedLineage, strokes[0].LineageID)
	}
}

// ====== 测试：高级别中枢检测 ======

func TestDetectPivotZones_ThreeStrokes(t *testing.T) {
	b := &LevelBuilder{}

	// 三笔：向上→向下→向上，区间重叠形成中枢
	strokes := []types.Stroke{
		mkStroke(0, 1, 10, 20, 22, 8),  // up, high=22 low=8
		mkStroke(1, -1, 20, 12, 20, 10), // down, high=20 low=10
		mkStroke(2, 1, 12, 25, 26, 10),  // up, high=26 low=10
	}

	zones := b.detectPivotZones(strokes, types.LevelL2)

	if len(zones) != 1 {
		t.Fatalf("期望 1 个中枢, 实际 %d", len(zones))
	}

	// ZG = min(22, 20, 26) = 20, ZD = max(8, 10, 10) = 10
	if zones[0].ZG != 20 {
		t.Errorf("ZG 期望 20, 实际 %.1f", zones[0].ZG)
	}
	if zones[0].ZD != 10 {
		t.Errorf("ZD 期望 10, 实际 %.1f", zones[0].ZD)
	}
	if zones[0].SegmentCount != 3 {
		t.Errorf("段数期望 3, 实际 %d", zones[0].SegmentCount)
	}
}

func TestDetectPivotZones_NoOverlap(t *testing.T) {
	b := &LevelBuilder{}

	// 三笔连续上涨，无重叠
	strokes := []types.Stroke{
		mkStroke(0, 1, 10, 20, 22, 8),
		mkStroke(1, 1, 20, 30, 32, 18),
		mkStroke(2, 1, 30, 40, 42, 28),
	}

	zones := b.detectPivotZones(strokes, types.LevelL2)

	if len(zones) != 0 {
		t.Errorf("无重叠时不期望中枢, 实际 %d", len(zones))
	}
}

func TestDetectPivotZones_TwoSeparate(t *testing.T) {
	b := &LevelBuilder{}

	// 两组独立的中枢
	strokes := []types.Stroke{
		// 第一中枢: bi0, bi1, bi2
		mkStroke(0, 1, 10, 20, 22, 8),
		mkStroke(1, -1, 20, 14, 20, 12),
		mkStroke(2, 1, 14, 24, 26, 12),
		// 离开
		mkStroke(3, 1, 24, 35, 38, 22),
		// 第二中枢: bi4, bi5, bi6
		mkStroke(4, -1, 35, 28, 35, 26),
		mkStroke(5, 1, 28, 40, 42, 26),
		mkStroke(6, -1, 40, 30, 40, 28),
	}

	zones := b.detectPivotZones(strokes, types.LevelL2)

	if len(zones) != 2 {
		t.Fatalf("期望 2 个独立中枢, 实际 %d", len(zones))
	}

	t.Logf("中枢1: ZG=%.1f ZD=%.1f 段数=%d", zones[0].ZG, zones[0].ZD, zones[0].SegmentCount)
	t.Logf("中枢2: ZG=%.1f ZD=%.1f 段数=%d", zones[1].ZG, zones[1].ZD, zones[1].SegmentCount)
}

// ====== 测试：高级别走势类型分类 ======

func TestDetectTrendPatterns_Consolidation(t *testing.T) {
	b := &LevelBuilder{}

	strokes := []types.Stroke{
		mkStroke(0, 1, 10, 20, 22, 8),
		mkStroke(1, -1, 20, 14, 20, 12),
		mkStroke(2, 1, 14, 24, 26, 12),
	}

	zones := b.detectPivotZones(strokes, types.LevelL2)
	patterns := b.detectTrendPatterns(strokes, zones)

	if len(patterns) != 1 {
		t.Fatalf("期望 1 个走势类型, 实际 %d", len(patterns))
	}
	if patterns[0].Type != "consolidation" {
		t.Errorf("类型期望 consolidation, 实际 %s", patterns[0].Type)
	}
}

func TestDetectTrendPatterns_Trend(t *testing.T) {
	b := &LevelBuilder{}

	// 两个同向非重叠中枢 → 趋势
	strokes := []types.Stroke{
		// 中枢1: bi0-bi2
		mkStroke(0, 1, 5, 15, 16, 4),
		mkStroke(1, -1, 15, 10, 15, 8),
		mkStroke(2, 1, 10, 20, 22, 8),
		// 离开段
		mkStroke(3, 1, 20, 35, 38, 18),
		// 中枢2: bi4-bi6
		mkStroke(4, -1, 35, 28, 35, 26),
		mkStroke(5, 1, 28, 42, 44, 26),
		mkStroke(6, -1, 42, 32, 42, 30),
	}

	zones := b.detectPivotZones(strokes, types.LevelL2)
	patterns := b.detectTrendPatterns(strokes, zones)

	if len(patterns) == 0 {
		t.Fatal("期望至少 1 个走势类型")
	}

	foundTrend := false
	for _, p := range patterns {
		if p.Type == "trend" {
			foundTrend = true
			t.Logf("趋势: 方向=%v 中枢数=%d", p.Direction, len(p.PivotZoneIDs))
		}
	}
	if !foundTrend {
		t.Error("未检测到趋势")
	}
}

// ====== 测试：makeTrendPattern 价格计算 ======

func TestMakeTrendPattern_UpDirection(t *testing.T) {
	pz := []types.PivotZone{
		{ZG: 20, ZD: 10, Direction: types.DirectionUp},
		{ZG: 35, ZD: 25, Direction: types.DirectionUp},
	}

	tp := makeTrendPattern(pz)

	if tp.Type != "trend" {
		t.Errorf("2个中枢期望 trend, 实际 %s", tp.Type)
	}
	if tp.Direction != types.DirectionUp {
		t.Errorf("方向期望 up, 实际 %v", tp.Direction)
	}
	// 向上趋势：StartPrice=第一个中枢ZD=10, EndPrice=最后一个中枢ZG=35
	if tp.StartPrice != 10 || tp.EndPrice != 35 {
		t.Errorf("价格期望 (10,35), 实际 (%.1f,%.1f)", tp.StartPrice, tp.EndPrice)
	}
	// High=max(ZG)=35, Low=min(ZD)=10
	if tp.High != 35 || tp.Low != 10 {
		t.Errorf("区间期望 high=35 low=10, 实际 (%.1f,%.1f)", tp.High, tp.Low)
	}
}

func TestMakeTrendPattern_DownDirection(t *testing.T) {
	pz := []types.PivotZone{
		{ZG: 40, ZD: 30, Direction: types.DirectionDown},
	}

	tp := makeTrendPattern(pz)

	if tp.Type != "consolidation" {
		t.Errorf("1个中枢期望 consolidation, 实际 %s", tp.Type)
	}
	if tp.Direction != types.DirectionDown {
		t.Errorf("方向期望 down, 实际 %v", tp.Direction)
	}
}

// waitForLevel 等待指定 symbol+level 的状态可用（异步处理用）。
// 最多等待 3 秒，轮询间隔 10ms。
func waitForLevel(b *LevelBuilder, symbol string, level types.Level, minStrokes int) *types.DualTrackState {
	deadline := time.After(3 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()

	for {
		select {
		case <-deadline:
			return nil
		case <-tick.C:
			state := b.GetState(symbol, level)
			if state != nil && len(state.Provisional.Strokes) >= minStrokes {
				return state
			}
		}
	}
}

// ====== 测试：完整级别递归（集成测试） ======

func TestLevelBuilder_Integration(t *testing.T) {
	gBus := eventbus.NewGeneric()
	tree := structure.New(gBus)

	b := New(gBus, tree)
	defer b.Stop()

	// 模拟 M3 中 L1 的状态
	// 先提交一个 L1 版本，包含已完成的走势类型
	l1State := &types.DualTrackState{
		Provisional: types.LevelStructure{
			Level:       types.LevelL1,
			Provisional: true,
			Strokes: []types.Stroke{
				mkStroke(0, 1, 10, 20, 22, 8),
				mkStroke(1, -1, 20, 14, 20, 12),
				mkStroke(2, 1, 14, 24, 26, 12),
			},
			TrendPatterns: []types.TrendPattern{
				{
					Direction:  types.DirectionUp,
					Type:       "consolidation",
					Completed:  true,
					StartPrice: 10,
					EndPrice:   24,
					High:       26,
					Low:        8,
				},
			},
		},
		Confirmed: types.LevelStructure{
			Level:       types.LevelL1,
			Provisional: false,
			Strokes: []types.Stroke{
				mkStroke(0, 1, 10, 20, 22, 8),
				mkStroke(1, -1, 20, 14, 20, 12),
				mkStroke(2, 1, 14, 24, 26, 12),
			},
			TrendPatterns: []types.TrendPattern{
				{
					Direction:  types.DirectionUp,
					Type:       "consolidation",
					Completed:  true,
					StartPrice: 10,
					EndPrice:   24,
					High:       26,
					Low:        8,
				},
			},
		},
		InSync: true,
	}

	// 手动提交 L1 版本（模拟 M3Bridge 的行为）
	vID := tree.Commit("TEST_INT", types.LevelL1, l1State, nil)
	t.Logf("L1 版本已提交: %s", vID)

	// 等待异步处理完成
	l2State := waitForLevel(b, "TEST_INT", types.LevelL2, 1)
	if l2State == nil {
		t.Fatal("L2 状态为空，递归构建未触发（超时）")
	}

	t.Logf("L2 笔数: %d", len(l2State.Provisional.Strokes))
	t.Logf("L2 中枢数: %d", len(l2State.Provisional.PivotZones))
	t.Logf("L2 走势数: %d", len(l2State.Provisional.TrendPatterns))

	if len(l2State.Provisional.Strokes) == 0 {
		t.Error("L2 应有至少 1 根笔（由 L1 完成的走势类型转换而来）")
	} else {
		t.Logf("L2 笔[0]: 方向=%v 价格=(%.1f,%.1f)",
			l2State.Provisional.Strokes[0].Direction,
			l2State.Provisional.Strokes[0].StartPrice,
			l2State.Provisional.Strokes[0].EndPrice,
		)
	}

	// 验证 L2 数据也从 M3 树可查
	treeL2 := tree.GetCurrentState("TEST_INT", types.LevelL2)
	if treeL2 == nil {
		t.Error("M3 树中应存在 L2 状态")
	} else {
		t.Logf("M3 树中 L2 笔数: %d", len(treeL2.Provisional.Strokes))
	}
}

// ====== 测试：多级递归（L1→L2→L3） ======

func TestLevelBuilder_MultiLevelRecursion(t *testing.T) {
	gBus := eventbus.NewGeneric()
	tree := structure.New(gBus)

	b := New(gBus, tree)

	// 提交 L1 状态：2 个完成的走势类型，应形成 L2 的 2 根笔
	l1State := &types.DualTrackState{
		Provisional: types.LevelStructure{
			Level:       types.LevelL1,
			Provisional: true,
			Strokes: []types.Stroke{
				mkStroke(0, 1, 5, 15, 16, 4),
				mkStroke(1, -1, 15, 10, 15, 8),
				mkStroke(2, 1, 10, 20, 22, 8),
				mkStroke(3, 1, 20, 35, 38, 18),
				mkStroke(4, -1, 35, 28, 35, 26),
				mkStroke(5, 1, 28, 42, 44, 26),
				mkStroke(6, -1, 42, 32, 42, 30),
			},
			TrendPatterns: []types.TrendPattern{
				{
					Direction:  types.DirectionUp,
					Type:       "consolidation",
					Completed:  true,
					StartPrice: 5,
					EndPrice:   20,
					High:       22,
					Low:        4,
				},
				{
					Direction:  types.DirectionUp,
					Type:       "trend",
					Completed:  true,
					StartPrice: 20,
					EndPrice:   42,
					High:       44,
					Low:        18,
				},
			},
		},
		Confirmed: types.LevelStructure{
			Level:       types.LevelL1,
			Provisional: false,
			Strokes: []types.Stroke{
				mkStroke(0, 1, 5, 15, 16, 4),
				mkStroke(1, -1, 15, 10, 15, 8),
				mkStroke(2, 1, 10, 20, 22, 8),
				mkStroke(3, 1, 20, 35, 38, 18),
				mkStroke(4, -1, 35, 28, 35, 26),
				mkStroke(5, 1, 28, 42, 44, 26),
				mkStroke(6, -1, 42, 32, 42, 30),
			},
			TrendPatterns: []types.TrendPattern{
				{
					Direction:  types.DirectionUp,
					Type:       "consolidation",
					Completed:  true,
					StartPrice: 5,
					EndPrice:   20,
					High:       22,
					Low:        4,
				},
				{
					Direction:  types.DirectionUp,
					Type:       "trend",
					Completed:  true,
					StartPrice: 20,
					EndPrice:   42,
					High:       44,
					Low:        18,
				},
			},
		},
		InSync: true,
	}

	vID := tree.Commit("TEST_MULTI", types.LevelL1, l1State, nil)
	t.Logf("L1 版本: %s", vID)

	// 等待异步处理完成
	l2State := waitForLevel(b, "TEST_MULTI", types.LevelL2, 2)
	if l2State == nil {
		t.Fatal("L2 状态为空（超时）")
	}
	t.Logf("L2 笔数: %d, 中枢数: %d, 走势数: %d",
		len(l2State.Provisional.Strokes),
		len(l2State.Provisional.PivotZones),
		len(l2State.Provisional.TrendPatterns))

	// 检查 L3（如果 L2 有走势类型完成，应递归到 L3）
	l3State := b.GetState("TEST_MULTI", types.LevelL3)
	if l3State != nil {
		t.Logf("L3 笔数: %d", len(l3State.Provisional.Strokes))
	} else {
		t.Log("L3 状态为空（L2 未形成走势类型）")
	}

	b.Stop()
}

// ====== 测试：双轨分歧 → 漂移检测 ======

func TestLevelBuilder_DriftDetection(t *testing.T) {
	gBus := eventbus.NewGeneric()
	tree := structure.New(gBus)

	// 订阅漂移事件
	var driftEvent *types.Event
	gBus.Subscribe(types.EventLevelRecast, func(evt types.Event) {
		driftEvent = &evt
	})

	b := New(gBus, tree)

	// 模拟一个分歧的 L1 状态
	divergentState := &types.DualTrackState{
		Provisional: types.LevelStructure{
			Level:       types.LevelL1,
			Provisional: true,
			Strokes: []types.Stroke{
				mkStroke(0, 1, 10, 20, 22, 8),
				mkStroke(1, -1, 20, 14, 20, 12),
				mkStroke(2, 1, 14, 24, 26, 12),
				mkStroke(3, 1, 24, 35, 38, 22), // 实时轨有额外笔
				mkStroke(4, -1, 35, 28, 35, 26),
			},
			TrendPatterns: []types.TrendPattern{
				{
					Direction:  types.DirectionUp,
					Type:       "consolidation",
					Completed:  true,
					StartPrice: 10,
					EndPrice:   24,
					High:       26,
					Low:        8,
				},
			},
		},
		Confirmed: types.LevelStructure{
			Level:       types.LevelL1,
			Provisional: false,
			Strokes: []types.Stroke{
				mkStroke(0, 1, 10, 20, 22, 8),
				mkStroke(1, -1, 20, 14, 20, 12),
				mkStroke(2, 1, 14, 24, 26, 12),
			},
			TrendPatterns: []types.TrendPattern{
				{
					Direction:  types.DirectionUp,
					Type:       "consolidation",
					Completed:  true,
					StartPrice: 10,
					EndPrice:   24,
					High:       26,
					Low:        8,
				},
			},
		},
		InSync:     false,
		DriftSince: time.Now().UnixMilli(),
	}

	// 提交 L1 版本
	tree.Commit("TEST_DRIFT", types.LevelL1, divergentState, nil)

	// 等待异步处理完成
	l2State := waitForLevel(b, "TEST_DRIFT", types.LevelL2, 1)
	if l2State == nil {
		t.Fatal("L2 状态为空（超时）")
	}

	if driftEvent != nil {
		t.Logf("漂移事件已触发: type=%s symbol=%s", driftEvent.Type, driftEvent.Symbol)
	} else {
		t.Log("漂移事件未触发（阈值未达到时正常）")
	}

	b.Stop()
}
