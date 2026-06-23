// Package resonance 共振引擎测试。
package resonance

import (
	"testing"
	"time"

	"trade/internal/eventbus"
	"trade/internal/structure"
	"trade/internal/types"
)

// ========================================================================
// 辅助：创建测试用总线 & 树
// ========================================================================

func newTestBusAndTree() (*eventbus.GenericBus, *structure.Tree) {
	bus := eventbus.NewGeneric()
	tree := structure.New(bus)
	return bus, tree
}

// newTestSignal 创建测试信号。
func newTestSignal(symbol string, st types.SignalType, level types.Level, price float64) *types.Signal {
	return &types.Signal{
		SignalID:  "sig_test_" + string(st) + "_" + level.String(),
		Symbol:    symbol,
		TS:        time.Now().UnixMilli(),
		Type:      st,
		Level:     level,
		Price:     price,
		State:     types.SignalCandidate,
		Resonance: types.Resonance{},
	}
}

// publishSignal 通过总线发布信号创建事件。
func publishSignal(bus *eventbus.GenericBus, sig *types.Signal) {
	bus.Publish(types.Event{
		Type:   types.EventSignalCreated,
		Symbol: sig.Symbol,
		TS:     time.Now().UnixMilli(),
		Payload: types.SignalEventPayload{
			Signal: sig,
		},
	})
}

// ========================================================================
// 测试：G-2 区间套
// ========================================================================

// TestG2_IntervalNesting 验证：L1 信号在 L2 同向走势类型区间内 → 区间套。
func TestG2_IntervalNesting(t *testing.T) {
	bus, tree := newTestBusAndTree()
	_ = New(bus, tree)

	// 预填充 L2 结构：下跌趋势 + 已完成走势，价格区间 [40, 100]
	l2State := &types.DualTrackState{
		Confirmed: types.LevelStructure{
			Level: types.LevelL2,
			TrendPatterns: []types.TrendPattern{
				{
					Direction: types.DirectionDown,
					Type:      "trend",
					Completed: true,
					High:      100,
					Low:       40,
				},
			},
		},
	}
	tree.Commit("TEST", types.LevelL2, l2State, nil)

	// L1 买点信号，价格在 L2 走势内
	sig := newTestSignal("TEST", types.SignalBuy1, types.LevelL1, 60)
	publishSignal(bus, sig)

	if sig.Resonance.Kind != types.ResonanceIntervalNesting {
		t.Errorf("期望区间套共振, 实际 %s", sig.Resonance.Kind)
	}
}

// TestG2_NoNesting_PriceOutside 验证：信号价格在大级别走势区间外 → 不是区间套。
func TestG2_NoNesting_PriceOutside(t *testing.T) {
	bus, tree := newTestBusAndTree()
	_ = New(bus, tree)

	l2State := &types.DualTrackState{
		Confirmed: types.LevelStructure{
			Level: types.LevelL2,
			TrendPatterns: []types.TrendPattern{
				{
					Direction: types.DirectionDown,
					Type:      "trend",
					Completed: true,
					High:      100,
					Low:       40,
				},
			},
		},
	}
	tree.Commit("TEST", types.LevelL2, l2State, nil)

	// 信号价格在 L2 走势区间外（高于 High）
	sig := newTestSignal("TEST", types.SignalBuy1, types.LevelL1, 120)
	publishSignal(bus, sig)

	if sig.Resonance.Kind == types.ResonanceIntervalNesting {
		t.Error("价格在区间外不应判定为区间套")
	}
}

// TestG2_NoNesting_WrongDirection 验证：大级别走势方向与信号类型不匹配 → 不是区间套。
func TestG2_NoNesting_WrongDirection(t *testing.T) {
	bus, tree := newTestBusAndTree()
	_ = New(bus, tree)

	// L2 上涨趋势
	l2State := &types.DualTrackState{
		Confirmed: types.LevelStructure{
			Level: types.LevelL2,
			TrendPatterns: []types.TrendPattern{
				{
					Direction: types.DirectionUp,
					Type:      "trend",
					Completed: true,
					High:      100,
					Low:       40,
				},
			},
		},
	}
	tree.Commit("TEST", types.LevelL2, l2State, nil)

	// 买点信号 vs 上涨趋势 → 方向不匹配
	sig := newTestSignal("TEST", types.SignalBuy1, types.LevelL1, 60)
	publishSignal(bus, sig)

	if sig.Resonance.Kind == types.ResonanceIntervalNesting {
		t.Error("方向不匹配不应判定为区间套")
	}
}

// TestG2_NestingDepth_MultipleLevels 验证：跨多级别区间套链。
func TestG2_NestingDepth_MultipleLevels(t *testing.T) {
	bus, tree := newTestBusAndTree()
	_ = New(bus, tree)

	// L2 下跌趋势
	l2State := &types.DualTrackState{
		Confirmed: types.LevelStructure{
			Level: types.LevelL2,
			TrendPatterns: []types.TrendPattern{
				{
					Direction: types.DirectionDown,
					Type:      "trend",
					Completed: true,
					High:      100,
					Low:       40,
				},
			},
		},
	}
	// L3 下跌趋势
	l3State := &types.DualTrackState{
		Confirmed: types.LevelStructure{
			Level: types.LevelL3,
			TrendPatterns: []types.TrendPattern{
				{
					Direction: types.DirectionDown,
					Type:      "trend",
					Completed: true,
					High:      120,
					Low:       30,
				},
			},
		},
	}
	tree.Commit("TEST", types.LevelL2, l2State, nil)
	tree.Commit("TEST", types.LevelL3, l3State, nil)

	sig := newTestSignal("TEST", types.SignalBuy1, types.LevelL1, 60)
	publishSignal(bus, sig)

	if sig.Resonance.Kind != types.ResonanceIntervalNesting {
		t.Errorf("期望区间套共振, 实际 %s", sig.Resonance.Kind)
	}
}

// ========================================================================
// 测试：G-1 跨层共振
// ========================================================================

// TestG1_CrossLevel_OtherLevelSignal 验证：其他级别有同向信号 → 跨层共振。
func TestG1_CrossLevel_OtherLevelSignal(t *testing.T) {
	bus, tree := newTestBusAndTree()
	_ = New(bus, tree)

	// 先发布 L2 买点信号
	sigL2 := newTestSignal("TEST", types.SignalBuy1, types.LevelL2, 55)
	publishSignal(bus, sigL2)

	// 再发布 L1 买点信号
	sigL1 := newTestSignal("TEST", types.SignalBuy1, types.LevelL1, 60)
	publishSignal(bus, sigL1)

	if sigL1.Resonance.Kind != types.ResonanceCrossLevel {
		t.Errorf("期望跨层共振, 实际 %s", sigL1.Resonance.Kind)
	}

	// 验证参与者
	if len(sigL1.Resonance.Participants) == 0 {
		t.Fatal("期望有共振参与者")
	}
	found := false
	for _, p := range sigL1.Resonance.Participants {
		if p.Level == types.LevelL2 {
			found = true
			break
		}
	}
	if !found {
		t.Error("参与者应包含 L2 信号")
	}
}

// TestG1_CrossLevel_NoMatchingSignal 验证：无其他级别同向信号 → standalone。
func TestG1_CrossLevel_NoMatchingSignal(t *testing.T) {
	bus, tree := newTestBusAndTree()
	_ = New(bus, tree)

	sig := newTestSignal("TEST", types.SignalBuy1, types.LevelL1, 60)
	publishSignal(bus, sig)

	if sig.Resonance.Kind != types.ResonanceStandalone {
		t.Errorf("无其他信号时期望 standalone, 实际 %s", sig.Resonance.Kind)
	}
}

// TestG1_CrossLevel_OppositeDirection 验证：反向信号不触发跨层共振。
func TestG1_CrossLevel_OppositeDirection(t *testing.T) {
	bus, tree := newTestBusAndTree()
	_ = New(bus, tree)

	// 先发布 L2 卖点信号
	sigSell := newTestSignal("TEST", types.SignalSell1, types.LevelL2, 80)
	publishSignal(bus, sigSell)

	// 再发布 L1 买点信号（反向）
	sigBuy := newTestSignal("TEST", types.SignalBuy1, types.LevelL1, 60)
	publishSignal(bus, sigBuy)

	if sigBuy.Resonance.Kind == types.ResonanceCrossLevel {
		t.Error("反向信号不应触发跨层共振")
	}
}

// ========================================================================
// 测试：A3 方向过滤
// ========================================================================

// TestA3_DirectionAligned 验证：多级别方向一致 → 方向过滤对齐。
func TestA3_DirectionAligned(t *testing.T) {
	bus, tree := newTestBusAndTree()
	_ = New(bus, tree)

	// L2 上涨方向
	tree.Commit("TEST", types.LevelL2, &types.DualTrackState{
		Confirmed: types.LevelStructure{
			Level: types.LevelL2,
			TrendPatterns: []types.TrendPattern{
				{Direction: types.DirectionUp, Type: "trend", Completed: true, High: 100, Low: 50},
			},
		},
	}, nil)
	// L3 上涨方向
	tree.Commit("TEST", types.LevelL3, &types.DualTrackState{
		Confirmed: types.LevelStructure{
			Level: types.LevelL3,
			TrendPatterns: []types.TrendPattern{
				{Direction: types.DirectionUp, Type: "trend", Completed: true, High: 110, Low: 40},
			},
		},
	}, nil)

	// 买点信号，与上涨方向对齐（上涨趋势中的回调买点）
	sig := newTestSignal("TEST", types.SignalBuy1, types.LevelL1, 60)
	publishSignal(bus, sig)

	if sig.Resonance.DirectionFilter == nil {
		t.Fatal("期望方向过滤结果非空")
	}
	if !sig.Resonance.DirectionFilter.Aligned {
		t.Error("期望方向对齐")
	}
	if sig.Resonance.DirectionFilter.Boost <= 0 {
		t.Error("方向对齐时 boost 应 > 0")
	}
}

// TestA3_DirectionNotAligned 验证：多级别方向不一致 → 方向过滤不对齐。
func TestA3_DirectionNotAligned(t *testing.T) {
	bus, tree := newTestBusAndTree()
	_ = New(bus, tree)

	// L2 下跌方向
	tree.Commit("TEST", types.LevelL2, &types.DualTrackState{
		Confirmed: types.LevelStructure{
			Level: types.LevelL2,
			TrendPatterns: []types.TrendPattern{
				{Direction: types.DirectionDown, Type: "trend", Completed: true, High: 100, Low: 50},
			},
		},
	}, nil)

	// 买点信号 vs 下跌方向 → 不对齐
	sig := newTestSignal("TEST", types.SignalBuy1, types.LevelL1, 60)
	publishSignal(bus, sig)

	if sig.Resonance.DirectionFilter != nil && sig.Resonance.DirectionFilter.Aligned {
		t.Error("买点与下跌方向不应对齐")
	}
}

// ========================================================================
// 测试：共振优先级
// ========================================================================

// TestResonancePriority_G2OverG1 验证：G-2 优先级高于 G-1。
func TestResonancePriority_G2OverG1(t *testing.T) {
	bus, tree := newTestBusAndTree()
	_ = New(bus, tree)

	// L2 下跌趋势（G-2 条件）
	tree.Commit("TEST", types.LevelL2, &types.DualTrackState{
		Confirmed: types.LevelStructure{
			Level: types.LevelL2,
			TrendPatterns: []types.TrendPattern{
				{Direction: types.DirectionDown, Type: "trend", Completed: true, High: 100, Low: 40},
			},
		},
	}, nil)

	// 先发布 L2 买点（G-1 条件）
	sigL2 := newTestSignal("TEST", types.SignalBuy1, types.LevelL2, 55)
	publishSignal(bus, sigL2)

	// 再发布 L1 买点，同时满足 G-2（在 L2 区间内）和 G-1（有 L2 同向信号）
	sigL1 := newTestSignal("TEST", types.SignalBuy1, types.LevelL1, 60)
	publishSignal(bus, sigL1)

	if sigL1.Resonance.Kind != types.ResonanceIntervalNesting {
		t.Errorf("同时满足 G-2 和 G-1 时，期望 G-2 优先, 实际 %s", sigL1.Resonance.Kind)
	}
}

// ========================================================================
// 测试：等待窗口
// ========================================================================

// TestWaitingWindow_Timeout 验证：等待窗口超时后不 panic。
func TestWaitingWindow_Timeout(t *testing.T) {
	bus, tree := newTestBusAndTree()
	res := New(bus, tree)

	// 发布一个信号触发等待窗口（G-1 等待）
	sig := newTestSignal("TEST", types.SignalBuy1, types.LevelL1, 60)
	publishSignal(bus, sig)

	// 触发 OnTimeout 不应 panic
	res.OnTimeout()
}

// ========================================================================
// 测试：CalculateConfidenceBoost
// ========================================================================

// TestCalculateConfidenceBoost_Standalone 验证：standalone 共振不增加 confidence 因子。
func TestCalculateConfidenceBoost_Standalone(t *testing.T) {
	r := types.Resonance{
		Kind: types.ResonanceStandalone,
	}
	da, nd, total := CalculateConfidenceBoost(r)

	if da != 1.0 {
		t.Errorf("directionAlignment 期望 1.0, 实际 %.2f", da)
	}
	if nd != 1.0 {
		t.Errorf("nestingDepth 期望 1.0, 实际 %.2f", nd)
	}
	if total != 1.0 {
		t.Errorf("total 期望 1.0, 实际 %.2f", total)
	}
}

// TestCalculateConfidenceBoost_WithAlignment 验证：方向对齐时 boost 生效。
func TestCalculateConfidenceBoost_WithAlignment(t *testing.T) {
	r := types.Resonance{
		Kind: types.ResonanceDirectionOnly,
		DirectionFilter: &types.DirectionFilter{
			Aligned: true,
			Boost:   0.15,
		},
	}
	da, nd, total := CalculateConfidenceBoost(r)

	if da < 1.14 || da > 1.16 {
		t.Errorf("directionAlignment 期望 ~1.15, 实际 %.4f", da)
	}
	if nd != 1.05 {
		t.Errorf("nestingDepth 期望 1.05, 实际 %.2f", nd)
	}
	if total < 1.20 || total > 1.21 {
		t.Errorf("total 期望 ~1.2075, 实际 %.4f", total)
	}
}

// TestCalculateConfidenceBoost_IntervalNesting 验证：区间套时 nestingDepth 提升。
func TestCalculateConfidenceBoost_IntervalNesting(t *testing.T) {
	r := types.Resonance{
		Kind: types.ResonanceIntervalNesting,
	}
	_, nd, _ := CalculateConfidenceBoost(r)

	if nd < 1.09 || nd > 1.11 {
		t.Errorf("nestingDepth 期望 ~1.1, 实际 %.2f", nd)
	}
}

// ========================================================================
// 测试：信号注册与查询
// ========================================================================

// TestGetActiveSignals 验证信号查询接口。
func TestGetActiveSignals(t *testing.T) {
	bus, tree := newTestBusAndTree()
	res := New(bus, tree)

	sig := newTestSignal("TEST", types.SignalBuy1, types.LevelL1, 60)
	publishSignal(bus, sig)

	signals := res.GetActiveSignals("TEST", types.LevelL1)
	if len(signals) == 0 {
		t.Fatal("期望有活跃信号")
	}
	if signals[0].SignalID != sig.SignalID {
		t.Errorf("信号ID不匹配")
	}
}

// TestGetActiveSignals_UnknownSymbol 验证：未知 symbol 返回 nil。
func TestGetActiveSignals_UnknownSymbol(t *testing.T) {
	bus, tree := newTestBusAndTree()
	res := New(bus, tree)

	signals := res.GetActiveSignals("UNKNOWN", types.LevelL1)
	if signals != nil {
		t.Error("未知 symbol 应返回 nil")
	}
}

// ========================================================================
// 测试：卖点对称性
// ========================================================================

// TestG1_SellCrossLevel 验证：卖点跨层共振。
func TestG1_SellCrossLevel(t *testing.T) {
	bus, tree := newTestBusAndTree()
	_ = New(bus, tree)

	// L2 卖点
	sigSellL2 := newTestSignal("TEST", types.SignalSell1, types.LevelL2, 90)
	publishSignal(bus, sigSellL2)

	// L1 卖点
	sigSellL1 := newTestSignal("TEST", types.SignalSell1, types.LevelL1, 85)
	publishSignal(bus, sigSellL1)

	if sigSellL1.Resonance.Kind != types.ResonanceCrossLevel {
		t.Errorf("卖点期望跨层共振, 实际 %s", sigSellL1.Resonance.Kind)
	}
}

// TestG2_SellIntervalNesting 验证：卖点区间套。
func TestG2_SellIntervalNesting(t *testing.T) {
	bus, tree := newTestBusAndTree()
	_ = New(bus, tree)

	// L2 上涨趋势
	tree.Commit("TEST", types.LevelL2, &types.DualTrackState{
		Confirmed: types.LevelStructure{
			Level: types.LevelL2,
			TrendPatterns: []types.TrendPattern{
				{Direction: types.DirectionUp, Type: "trend", Completed: true, High: 120, Low: 50},
			},
		},
	}, nil)

	// L1 卖点，价格在 L2 区间内
	sig := newTestSignal("TEST", types.SignalSell1, types.LevelL1, 100)
	publishSignal(bus, sig)

	if sig.Resonance.Kind != types.ResonanceIntervalNesting {
		t.Errorf("卖点期望区间套共振, 实际 %s", sig.Resonance.Kind)
	}
}

// ========================================================================
// 测试：confidence 共振因子集成
// ========================================================================

// TestConfidence_Standalone 验证：无共振时 confidence 不变。
func TestConfidence_Standalone(t *testing.T) {
	bus, tree := newTestBusAndTree()
	_ = New(bus, tree)

	sig := newTestSignal("TEST", types.SignalBuy1, types.LevelL1, 60)
	sig.Confidence = 0.6
	publishSignal(bus, sig)

	// standalone 共振 → boost=1.0, confidence 不变
	if sig.Confidence != 0.6 {
		t.Errorf("standalone 期望 confidence=0.6, 实际 %.2f", sig.Confidence)
	}
}

// TestConfidence_IntervalNesting 验证：区间套共振提升 confidence。
func TestConfidence_IntervalNesting(t *testing.T) {
	bus, tree := newTestBusAndTree()
	_ = New(bus, tree)

	// L2 下跌趋势（创造 G-2 条件）
	tree.Commit("TEST", types.LevelL2, &types.DualTrackState{
		Confirmed: types.LevelStructure{
			Level: types.LevelL2,
			TrendPatterns: []types.TrendPattern{
				{Direction: types.DirectionDown, Type: "trend", Completed: true, High: 100, Low: 40},
			},
		},
	}, nil)

	sig := newTestSignal("TEST", types.SignalBuy1, types.LevelL1, 60)
	sig.Confidence = 0.6
	publishSignal(bus, sig)

	// 区间套 nestingDepth=1.1, directionAlignment=1.0, total=1.1
	// 期望 confidence ≈ 0.6 × 1.1 = 0.66
	expected := 0.66
	if sig.Confidence < expected-0.01 || sig.Confidence > expected+0.01 {
		t.Errorf("区间套期望 confidence≈%.2f, 实际 %.4f", expected, sig.Confidence)
	}
}

// TestConfidence_CrossLevelWithAlignment 验证：跨层共振+方向对齐的组合提升。
func TestConfidence_CrossLevelWithAlignment(t *testing.T) {
	bus, tree := newTestBusAndTree()
	_ = New(bus, tree)

	// L2 上涨方向（A3 对齐条件）
	tree.Commit("TEST", types.LevelL2, &types.DualTrackState{
		Confirmed: types.LevelStructure{
			Level: types.LevelL2,
			TrendPatterns: []types.TrendPattern{
				{Direction: types.DirectionUp, Type: "trend", Completed: true, High: 100, Low: 50},
			},
		},
	}, nil)

	// 卖点 → L2 上涨方向与卖点对齐
	sigSell := newTestSignal("TEST", types.SignalSell1, types.LevelL2, 90)
	publishSignal(bus, sigSell)

	sig := newTestSignal("TEST", types.SignalSell1, types.LevelL1, 85)
	sig.Confidence = 0.6
	publishSignal(bus, sig)

	// 跨层共振: nestingDepth=1.1
	// 方向对齐: boost≈0.1 (1/1级别对齐=100%, boost=0.2), directionAlignment=1.2
	// total=1.1×1.2=1.32, confidence=0.6×1.32=0.792
	if sig.Confidence <= 0.6 {
		t.Error("期望 confidence 提升")
	}
	if sig.Confidence > 1.0 {
		t.Error("confidence 不应超过 1.0")
	}
}

// TestConfidence_Capped 验证：boost 后 confidence 不超过 1.0。
func TestConfidence_Capped(t *testing.T) {
	bus, tree := newTestBusAndTree()
	_ = New(bus, tree)

	tree.Commit("TEST", types.LevelL2, &types.DualTrackState{
		Confirmed: types.LevelStructure{
			Level: types.LevelL2,
			TrendPatterns: []types.TrendPattern{
				{Direction: types.DirectionDown, Type: "trend", Completed: true, High: 100, Low: 40},
			},
		},
	}, nil)

	sig := newTestSignal("TEST", types.SignalBuy1, types.LevelL1, 60)
	sig.Confidence = 0.95 // 已经很高
	publishSignal(bus, sig)

	if sig.Confidence > 1.0 {
		t.Error("confidence 不应超过 1.0")
	}
}

// ========================================================================
// 测试：Evidence.IntervalNestingChain
// ========================================================================

// TestNestingChain_Populated 验证：区间套命中时填充 Evidence.IntervalNestingChain。
func TestNestingChain_Populated(t *testing.T) {
	bus, tree := newTestBusAndTree()
	_ = New(bus, tree)

	tree.Commit("TEST", types.LevelL2, &types.DualTrackState{
		Confirmed: types.LevelStructure{
			Level: types.LevelL2,
			TrendPatterns: []types.TrendPattern{
				{Direction: types.DirectionDown, Type: "trend", Completed: true, High: 100, Low: 40},
			},
		},
	}, nil)

	sig := newTestSignal("TEST", types.SignalBuy1, types.LevelL1, 60)
	publishSignal(bus, sig)

	if len(sig.Evidence.IntervalNestingChain) == 0 {
		t.Fatal("期望 IntervalNestingChain 非空")
	}
	foundL2 := false
	for _, link := range sig.Evidence.IntervalNestingChain {
		if link.Level == types.LevelL2 && link.InDivergenceSegment {
			foundL2 = true
			break
		}
	}
	if !foundL2 {
		t.Error("IntervalNestingChain 应包含 L2 且 inDivergenceSegment=true")
	}
}

// TestNestingChain_NoNesting 验证：无区间套时 chain 为空。
func TestNestingChain_NoNesting(t *testing.T) {
	bus, tree := newTestBusAndTree()
	_ = New(bus, tree)

	// 不填充高级别结构
	sig := newTestSignal("TEST", types.SignalBuy1, types.LevelL1, 60)
	publishSignal(bus, sig)

	if len(sig.Evidence.IntervalNestingChain) != 0 {
		t.Error("无区间套时 IntervalNestingChain 应为空")
	}
}
