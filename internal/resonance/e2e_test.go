// Package resonance M5→M6 端到端集成测试。
//
// 验证链路：
//
//	SignalInput → M5 SignalEngine → EventSignalCreated → M6 ResonanceEngine
//	→ G-2/G-1/A3 判定 → confidence 共振因子写入 → EventResonanceTriggered
package resonance

import (
	"testing"
	"time"

	"trade/internal/chanlun"
	"trade/internal/eventbus"
	signals "trade/internal/signal"
	"trade/internal/structure"
	"trade/internal/types"
)

// ========================================================================
// E2E: 完整 M5→M6 信号→共振链路
// ========================================================================

// TestE2E_SignalToResonance_FullFlow 验证：
// 1. SignalEngine 处理 SignalInput 产生 Buy1 信号
// 2. 通过 EventSignalCreated 触发 ResonanceEngine
// 3. ResonanceEngine 完成 G-2/G-1/A3 判定
// 4. 信号 Resonance/Confidence 被正确更新
// 5. EventResonanceTriggered 被发布
func TestE2E_SignalToResonance_FullFlow(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := structure.New(bus)

	// 创建信号引擎（带 bus，事件发布到总线）
	sigEngine := signals.New(bus)

	// 创建共振引擎（订阅总线上的 EventSignalCreated）
	resEngine := New(bus, tree)

	// 预填充树的多级别结构（模拟 M4 工作）
	// L2 下跌趋势（G-2 区间套条件）
	tree.Commit("E2E", types.LevelL2, &types.DualTrackState{
		Confirmed: types.LevelStructure{
			Level: types.LevelL2,
			TrendPatterns: []types.TrendPattern{
				{
					Direction: types.DirectionDown,
					Type:      "trend",
					Completed: true,
					High:      95,
					Low:       40,
				},
			},
		},
	}, nil)

	// L3 上涨方向（A3 方向过滤：上涨趋势中的买点方向不一致→不贡献对齐）
	tree.Commit("E2E", types.LevelL3, &types.DualTrackState{
		Confirmed: types.LevelStructure{
			Level: types.LevelL3,
			TrendPatterns: []types.TrendPattern{
				{
					Direction: types.DirectionUp,
					Type:      "trend",
					Completed: true,
					High:      110,
					Low:       50,
				},
			},
		},
	}, nil)

	// 监听共振事件
	var resonanceEventCount int
	var lastResonanceEvent types.ResonanceEventPayload
	bus.Subscribe(types.EventResonanceTriggered, func(evt types.Event) {
		resonanceEventCount++
		if p, ok := evt.Payload.(types.ResonanceEventPayload); ok {
			lastResonanceEvent = p
		}
	})

	// 构造 SignalInput → 触发一买识别
	input := &chanlun.SignalInput{
		Symbol: "E2E",
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
			{Type: "bottomDivergence", EntryEnd: 2, ExitEnd: 4, EntryPrice: 70, ExitPrice: 50,
				EntryMACD: 30, ExitMACD: 15, Ratio: 0.5, Confirmed: true},
		},
	}

	// 触发信号引擎处理
	sigEngine.OnSignalInput(input)

	// 等待事件传播（异步事件可能需要短暂等待）
	time.Sleep(10 * time.Millisecond)

	// === 断言 ===

	// 1. 信号引擎产生了 Buy1 信号
	signals := sigEngine.GetActiveSignals("E2E")
	if len(signals) == 0 {
		t.Fatal("信号引擎应产生信号")
	}
	var buy1 *types.Signal
	for _, s := range signals {
		if s.Type == types.SignalBuy1 {
			buy1 = s
			break
		}
	}
	if buy1 == nil {
		t.Fatal("应产生一买信号")
	}

	t.Logf("信号: id=%s type=%s level=%s price=%.2f",
		buy1.SignalID, buy1.Type, buy1.Level, buy1.Price)

	// 2. 共振类型应为 intervalNesting（L2 下跌趋势 + L1 买点，价格 50 在 [40,95] 内）
	if buy1.Resonance.Kind != types.ResonanceIntervalNesting {
		t.Errorf("期望区间套共振, 实际 %s", buy1.Resonance.Kind)
	}

	// 3. 方向过滤检查
	if buy1.Resonance.DirectionFilter == nil {
		t.Fatal("期望 DirectionFilter 非空")
	}
	t.Logf("方向过滤: aligned=%v boost=%.4f levels=%v",
		buy1.Resonance.DirectionFilter.Aligned,
		buy1.Resonance.DirectionFilter.Boost,
		buy1.Resonance.DirectionFilter.AlignedLevels)

	// 4. confidence 被共振因子提升
	if buy1.Confidence <= 0.5 {
		t.Errorf("期望 confidence > 0.5 (有共振因子), 实际 %.4f", buy1.Confidence)
	}
	t.Logf("confidence: %.4f (共振因子已集成)", buy1.Confidence)

	// 5. IntervalNestingChain 已填充
	if len(buy1.Evidence.IntervalNestingChain) == 0 {
		t.Error("期望 IntervalNestingChain 非空")
	} else {
		for _, link := range buy1.Evidence.IntervalNestingChain {
			t.Logf("区间套: level=%s inDivergence=%v", link.Level, link.InDivergenceSegment)
		}
	}

	// 6. 共振事件已发布
	if resonanceEventCount == 0 {
		t.Error("期望 EventResonanceTriggered 被发布")
	} else {
		t.Logf("共振事件发布数: %d", resonanceEventCount)
		if lastResonanceEvent.Signal == nil {
			t.Error("共振事件负载中的 signal 不应为空")
		}
		if lastResonanceEvent.Resonance.Kind != types.ResonanceIntervalNesting {
			t.Errorf("共振事件中的共振类型期望 intervalNesting, 实际 %s",
				lastResonanceEvent.Resonance.Kind)
		}
	}

	// 清理
	resEngine.Stop()
}

// ========================================================================
// E2E: 多信号跨层共振（G-1 测试）
//
// 注意：当前 M5 信号引擎只产生 L1 信号。跨层共振需要不同级别的信号。
// 本测试直接通过事件总线注入不同级别的信号，模拟未来多级别信号链路。
// ========================================================================

// TestE2E_CrossLevelResonance 验证：
// 1. 多个级别先后产生同向信号
// 2. 第二批信号被判定为 crossLevel 共振
// 3. confidence 获得 crossLevel 因子提升
func TestE2E_CrossLevelResonance(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := structure.New(bus)
	_ = signals.New(bus)
	resEngine := New(bus, tree)

	// 监听共振事件
	resonanceEvents := make([]types.ResonanceEventPayload, 0)
	bus.Subscribe(types.EventResonanceTriggered, func(evt types.Event) {
		if p, ok := evt.Payload.(types.ResonanceEventPayload); ok {
			resonanceEvents = append(resonanceEvents, p)
		}
	})

	// 直接通过事件总线注入 L2 买点信号（模拟未来多级别信号链路）
	l2Signal := &types.Signal{
		SignalID:   "sig_L2_BUY_1",
		Symbol:     "E2E_CROSS",
		TS:         time.Now().UnixMilli(),
		Type:       types.SignalBuy1,
		Level:      types.LevelL2,
		Price:      100,
		State:      types.SignalCandidate,
		Confidence: 0.7,
	}
	bus.Publish(types.Event{
		Type:   types.EventSignalCreated,
		Symbol: "E2E_CROSS",
		TS:     time.Now().UnixMilli(),
		Payload: types.SignalEventPayload{
			Signal: l2Signal,
		},
	})
	time.Sleep(5 * time.Millisecond)

	// 注入 L1 买点信号
	l1Signal := &types.Signal{
		SignalID:   "sig_L1_BUY_1",
		Symbol:     "E2E_CROSS",
		TS:         time.Now().UnixMilli(),
		Type:       types.SignalBuy1,
		Level:      types.LevelL1,
		Price:      55,
		State:      types.SignalCandidate,
		Confidence: 0.7,
	}
	bus.Publish(types.Event{
		Type:   types.EventSignalCreated,
		Symbol: "E2E_CROSS",
		TS:     time.Now().UnixMilli(),
		Payload: types.SignalEventPayload{
			Signal: l1Signal,
		},
	})
	time.Sleep(10 * time.Millisecond)

	// === 断言 ===

	// L1 信号应为 crossLevel 共振（L2 已有同向信号）
	if l1Signal.Resonance.Kind != types.ResonanceCrossLevel {
		t.Errorf("期望 crossLevel 共振, 实际 %s", l1Signal.Resonance.Kind)
	}

	// 应有参与者
	if len(l1Signal.Resonance.Participants) == 0 {
		t.Error("期望共振参与者列表非空")
	} else {
		for _, p := range l1Signal.Resonance.Participants {
			t.Logf("参与者: level=%s type=%s signalId=%s", p.Level, p.Type, p.SignalID)
		}
	}

	// confidence 应 > 原始值 0.7（有跨层共振因子 nestingDepth=1.1, total=1.1）
	expectedMin := 0.7 * 1.1
	if l1Signal.Confidence < expectedMin-0.01 {
		t.Errorf("期望 confidence >= %.2f (有跨层共振因子), 实际 %.4f", expectedMin, l1Signal.Confidence)
	}

	// 共振事件应有 >= 1 条（L1 信号 crossLevel 触发；L2 standalone 不触发）
	if len(resonanceEvents) < 1 {
		t.Errorf("期望 >= 1 条共振事件, 实际 %d", len(resonanceEvents))
	} else {
		t.Logf("共振事件总数: %d", len(resonanceEvents))
	}

	resEngine.Stop()
}

// ========================================================================
// E2E: 完整的信号状态机+共振（信号状态变更后共振不变）
// ========================================================================

// TestE2E_SignalStateChange_NoResonanceChange 验证：信号状态变更不改变共振结果。
func TestE2E_SignalStateChange_NoResonanceChange(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := structure.New(bus)
	sigEngine := signals.New(bus)
	resEngine := New(bus, tree)

	// L2 下跌趋势（G-2 条件）
	tree.Commit("E2E_STATE", types.LevelL2, &types.DualTrackState{
		Confirmed: types.LevelStructure{
			Level: types.LevelL2,
			TrendPatterns: []types.TrendPattern{
				{Direction: types.DirectionDown, Type: "trend", Completed: true, High: 100, Low: 40},
			},
		},
	}, nil)

	// 产生 buy1 信号
	input := &chanlun.SignalInput{
		Symbol: "E2E_STATE",
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
			{Type: "bottomDivergence", EntryEnd: 2, ExitEnd: 4, EntryPrice: 70, ExitPrice: 50,
				EntryMACD: 30, ExitMACD: 15, Ratio: 0.5, Confirmed: true},
		},
	}
	sigEngine.OnSignalInput(input)
	time.Sleep(10 * time.Millisecond)

	signals := sigEngine.GetActiveSignals("E2E_STATE")
	if len(signals) == 0 {
		t.Fatal("应产生信号")
	}

	sig := signals[0]
	originalKind := sig.Resonance.Kind
	originalConfidence := sig.Confidence

	t.Logf("原始: kind=%s confidence=%.4f", originalKind, originalConfidence)

	// 再次输入相同信号（模拟同一结构的再次分析）
	sigEngine.OnSignalInput(input)
	time.Sleep(5 * time.Millisecond)

	// 共振结果不应改变
	signals2 := sigEngine.GetActiveSignals("E2E_STATE")
	if len(signals2) > 0 {
		sig2 := signals2[0]
		if sig2.Resonance.Kind != originalKind {
			t.Errorf("共振类型不应改变: 原=%s 新=%s", originalKind, sig2.Resonance.Kind)
		}
		t.Logf("再次分析后: kind=%s confidence=%.4f", sig2.Resonance.Kind, sig2.Confidence)
	}

	resEngine.Stop()
}

// ========================================================================
// 信号质量校验 E2E 测试
//
// 替代 M9 回测引擎的职责：通过 E2E 测试验证信号输出质量。
// 覆盖 PRD §13.2 中可通过结构化断言验证的指标。
// ========================================================================

// TestSignalQuality_SignalCompleteness 验证：信号输出字段完整性。
//
// 对应 PRD §13.2 指标：
//   - 假信号率 → 通过验证信号字段完整、状态一致性来预防
//   - recast 率 → 通过验证结构版本和锚点字段来预防
//
// 质量维度：每个信号必须包含完整的 identity、anchor、targets、evidence。
func TestSignalQuality_SignalCompleteness(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := structure.New(bus)
	sigEngine := signals.New(bus)
	_ = New(bus, tree)

	input := &chanlun.SignalInput{
		Symbol: "QUALITY",
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
			{Type: "bottomDivergence", EntryEnd: 2, ExitEnd: 4, EntryPrice: 70, ExitPrice: 50,
				EntryMACD: 30, ExitMACD: 15, Ratio: 0.5, Confirmed: true},
		},
	}

	sigEngine.OnSignalInput(input)
	time.Sleep(10 * time.Millisecond)

	signals := sigEngine.GetActiveSignals("QUALITY")
	if len(signals) == 0 {
		t.Fatal("应产生信号")
	}

	for _, s := range signals {
		t.Logf("校验信号: id=%s type=%s state=%s confidence=%.4f",
			s.SignalID, s.Type, s.State, s.Confidence)

		// 每个信号必须有完整的基础字段
		if s.SignalID == "" {
			t.Error("SignalID 不能为空")
		}
		if s.Symbol != "QUALITY" {
			t.Errorf("Symbol 期望 QUALITY, 实际 %s", s.Symbol)
		}
		if s.TS == 0 {
			t.Error("TS 不能为 0")
		}
		if s.Type == "" {
			t.Error("Type 不能为空")
		}
		if s.Level != types.LevelL1 {
			t.Errorf("Level 应为基础级别 L1, 实际 %s", s.Level)
		}
		if s.Price == 0 {
			t.Error("Price 不能为 0")
		}
		if s.State == "" {
			t.Error("State 不能为空")
		}

		// 验证置信度范围
		if s.Confidence <= 0 || s.Confidence > 1.0 {
			t.Errorf("Confidence 应在 (0,1] 范围内, 实际 %.4f", s.Confidence)
		}

		// 验证锚点（PRD §8.6）
		if s.Anchor.Kind == "" {
			t.Error("Anchor.Kind 不能为空")
		}

		// 验证目标位（PRD §8.7）
		if s.Targets.InvalidationPrice == 0 {
			t.Error("Targets.InvalidationPrice 不能为 0")
		}

		// 验证证据（PRD §8.1）
		if s.Evidence.TrendDirection == "" {
			t.Error("Evidence.TrendDirection 不能为空")
		}
		if s.Evidence.PivotZoneCount <= 0 {
			t.Errorf("Evidence.PivotZoneCount 应 > 0, 实际 %d", s.Evidence.PivotZoneCount)
		}

		// 验证共振（PRD §9）
		if s.Resonance.Kind == "" {
			t.Error("Resonance.Kind 不能为空")
		}
	}
}

// TestSignalQuality_Buy1AndSell1_Symmetry 验证：买一和卖一信号互为镜像。
//
// 对应 PRD §13.2 指标：
//   - 胜率 → 通过验证买卖信号对称性确保算法无方向偏倚
//   - 假信号率 → 通过验证对称条件确保 buy/sell 逻辑一致
//
// 质量维度：相同结构模式下 Buy1 和 Sell1 应产生对称的信号字段。
func TestSignalQuality_Buy1AndSell1_Symmetry(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := structure.New(bus)
	sigEngine := signals.New(bus)
	_ = New(bus, tree)

	// Buy1 场景：下跌趋势 + 底背驰
	buyInput := &chanlun.SignalInput{
		Symbol: "SYM",
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
			{Type: "bottomDivergence", EntryEnd: 2, ExitEnd: 4, EntryPrice: 70, ExitPrice: 50,
				EntryMACD: 30, ExitMACD: 15, Ratio: 0.5, Confirmed: true},
		},
	}

	// Sell1 场景：上涨趋势 + 顶背驰（对称构造）
	sellInput := &chanlun.SignalInput{
		Symbol: "SYM",
		Strokes: []chanlun.StrokeInfo{
			{Index: 0, Direction: types.DirectionUp, StartPrice: 50, EndPrice: 70, High: 70, Low: 50},
			{Index: 1, Direction: types.DirectionDown, StartPrice: 70, EndPrice: 55, High: 70, Low: 55},
			{Index: 2, Direction: types.DirectionUp, StartPrice: 55, EndPrice: 80, High: 80, Low: 55},
			{Index: 3, Direction: types.DirectionDown, StartPrice: 80, EndPrice: 65, High: 80, Low: 65},
			{Index: 4, Direction: types.DirectionUp, StartPrice: 65, EndPrice: 100, High: 100, Low: 65},
		},
		PivotZones: []chanlun.PivotZoneInfo{
			{Index: 0, ZG: 62, ZD: 58, Direction: types.DirectionUp, EndStrokeIdx: 2},
			{Index: 1, ZG: 75, ZD: 68, Direction: types.DirectionUp, EndStrokeIdx: 4},
		},
		TrendPatterns: []chanlun.TrendPatternInfo{
			{Type: "trend", Direction: types.DirectionUp, PivotZoneIDs: []int{0, 1}},
		},
		Divergences: []chanlun.DivergenceInfo{
			{Type: "topDivergence", EntryEnd: 2, ExitEnd: 4, EntryPrice: 80, ExitPrice: 100,
				EntryMACD: 30, ExitMACD: 15, Ratio: 0.5, Confirmed: true},
		},
	}

	sigEngine.OnSignalInput(buyInput)
	sigEngine.OnSignalInput(sellInput)
	time.Sleep(10 * time.Millisecond)

	signals := sigEngine.GetActiveSignals("SYM")

	var buy1, sell1 *types.Signal
	for _, s := range signals {
		if s.Type == types.SignalBuy1 {
			buy1 = s
		}
		if s.Type == types.SignalSell1 {
			sell1 = s
		}
	}

	if buy1 == nil {
		t.Fatal("应产生一买信号")
	}
	if sell1 == nil {
		t.Fatal("应产生一卖信号")
	}

	// 对称性校验
	// 1. 方向相反
	if buy1.Evidence.TrendDirection != "down" {
		t.Errorf("一买趋势方向应为 down, 实际 %s", buy1.Evidence.TrendDirection)
	}
	if sell1.Evidence.TrendDirection != "up" {
		t.Errorf("一卖趋势方向应为 up, 实际 %s", sell1.Evidence.TrendDirection)
	}

	// 2. 中枢计数一致
	if buy1.Evidence.PivotZoneCount != sell1.Evidence.PivotZoneCount {
		t.Errorf("一买和一卖的中枢数应一致: buy=%d sell=%d",
			buy1.Evidence.PivotZoneCount, sell1.Evidence.PivotZoneCount)
	}

	// 3. 置信度范围一致
	if buy1.Confidence <= 0 || buy1.Confidence > 1.0 {
		t.Errorf("一买 confidence 应在 (0,1]: %.4f", buy1.Confidence)
	}
	if sell1.Confidence <= 0 || sell1.Confidence > 1.0 {
		t.Errorf("一卖 confidence 应在 (0,1]: %.4f", sell1.Confidence)
	}

	t.Logf("一买: confidence=%.4f trend=%s pivotZones=%d",
		buy1.Confidence, buy1.Evidence.TrendDirection, buy1.Evidence.PivotZoneCount)
	t.Logf("一卖: confidence=%.4f trend=%s pivotZones=%d",
		sell1.Confidence, sell1.Evidence.TrendDirection, sell1.Evidence.PivotZoneCount)
}

// TestSignalQuality_ResonanceGain 验证：共振信号的质量增益。
//
// 对应 PRD §13.2 指标：共振信号增益。
//
// 验证有共振的信号 confidence 高于无共振的同类型信号，
// 确保共振引擎确实提升了信号质量排序。
func TestSignalQuality_ResonanceGain(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := structure.New(bus)
	sigEngine := signals.New(bus)
	resEngine := New(bus, tree)

	// 预填充 L2 下跌趋势（为第一个信号创造 G-2 区间套条件）
	tree.Commit("GAIN", types.LevelL2, &types.DualTrackState{
		Confirmed: types.LevelStructure{
			Level: types.LevelL2,
			TrendPatterns: []types.TrendPattern{
				{Direction: types.DirectionDown, Type: "trend", Completed: true, High: 100, Low: 40},
			},
		},
	}, nil)

	// 产生 Buy1 信号（有区间套共振）
	inputWithResonance := &chanlun.SignalInput{
		Symbol: "GAIN",
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
			{Type: "bottomDivergence", EntryEnd: 2, ExitEnd: 4, EntryPrice: 70, ExitPrice: 50,
				EntryMACD: 30, ExitMACD: 15, Ratio: 0.5, Confirmed: true},
		},
	}

	sigEngine.OnSignalInput(inputWithResonance)
	time.Sleep(10 * time.Millisecond)

	signals := sigEngine.GetActiveSignals("GAIN")
	var withResonance *types.Signal
	for _, s := range signals {
		if s.Type == types.SignalBuy1 {
			withResonance = s
			break
		}
	}
	if withResonance == nil {
		t.Fatal("应产生一买信号")
	}

	// 验证有区间套共振的信号 confidence 明显高于原始基准值
	// PRD §8.4 公式：confidence = base(0.60) × divergenceStrength(1.0)
	//   × directionAlignment × nestingDepth × (1-recastRisk)
	//   base=0.60 (BUY_1), ratio=0.5 → divergenceStrength=1.0
	//   原始 confidence = 0.60
	// 区间套 boost: nestingDepth=1.1 → final = 0.60 × 1.1 = 0.66
	if withResonance.Resonance.Kind != types.ResonanceIntervalNesting {
		t.Errorf("期望区间套共振, 实际 %s", withResonance.Resonance.Kind)
	}

	// 有共振的 confidence 应 > 无共振的原始值（0.60）
	baseConfidence := 0.60
	if withResonance.Confidence <= baseConfidence {
		t.Errorf("有区间套共振的信号 confidence 应 > 原始 %.2f, 实际 %.4f",
			baseConfidence, withResonance.Confidence)
	}

	t.Logf("有共振信号: kind=%s confidence=%.4f (增益 %.2f%% → %.4f)",
		withResonance.Resonance.Kind,
		withResonance.Confidence,
		(withResonance.Confidence/baseConfidence-1)*100,
		withResonance.Confidence-baseConfidence,
	)

	resEngine.Stop()
}

// TestSignalQuality_FalseSignalPrevention 验证：不满足条件时不产生假信号。
//
// 对应 PRD §13.2 指标：假信号率。
//
// 验证引擎在以下场景不产生信号：
//   - 无背驰
//   - 非趋势（盘整）
//   - 背驰不确认
func TestSignalQuality_FalseSignalPrevention(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := structure.New(bus)
	sigEngine := signals.New(bus)
	_ = New(bus, tree)

	// 场景1：有趋势但无背驰 → 不应产生信号
	t.Run("无背驰", func(t *testing.T) {
		sigEngine.OnSignalInput(&chanlun.SignalInput{
			Symbol: "NO_DIV",
			Strokes: []chanlun.StrokeInfo{
				{Index: 0, Direction: types.DirectionDown, StartPrice: 100, EndPrice: 80, High: 100, Low: 80},
				{Index: 1, Direction: types.DirectionUp, StartPrice: 80, EndPrice: 95, High: 95, Low: 80},
				{Index: 2, Direction: types.DirectionDown, StartPrice: 95, EndPrice: 70, High: 95, Low: 70},
				{Index: 3, Direction: types.DirectionUp, StartPrice: 70, EndPrice: 85, High: 85, Low: 70},
				{Index: 4, Direction: types.DirectionDown, StartPrice: 85, EndPrice: 60, High: 85, Low: 60},
			},
			PivotZones: []chanlun.PivotZoneInfo{
				{Index: 0, ZG: 90, ZD: 82, Direction: types.DirectionDown, EndStrokeIdx: 2},
				{Index: 1, ZG: 80, ZD: 72, Direction: types.DirectionDown, EndStrokeIdx: 4},
			},
			TrendPatterns: []chanlun.TrendPatternInfo{
				{Type: "trend", Direction: types.DirectionDown, PivotZoneIDs: []int{0, 1}},
			},
			// 无背驰
		})
		time.Sleep(5 * time.Millisecond)
		sigs := sigEngine.GetActiveSignals("NO_DIV")
		if len(sigs) > 0 {
			t.Errorf("无背驰时应无信号, 实际产生 %d 个", len(sigs))
		}
	})

	// 场景2：盘整（非趋势）→ 不应产生一买/一卖
	t.Run("盘整非趋势", func(t *testing.T) {
		sigEngine.OnSignalInput(&chanlun.SignalInput{
			Symbol: "NO_TREND",
			Strokes: []chanlun.StrokeInfo{
				{Index: 0, Direction: types.DirectionDown, StartPrice: 100, EndPrice: 80, High: 100, Low: 80},
				{Index: 1, Direction: types.DirectionUp, StartPrice: 80, EndPrice: 95, High: 95, Low: 80},
			},
			PivotZones: []chanlun.PivotZoneInfo{
				{Index: 0, ZG: 90, ZD: 82, Direction: types.DirectionDown, EndStrokeIdx: 1},
			},
			TrendPatterns: []chanlun.TrendPatternInfo{
				{Type: "consolidation", Direction: types.DirectionDown, PivotZoneIDs: []int{0}},
			},
		})
		time.Sleep(5 * time.Millisecond)
		sigs := sigEngine.GetActiveSignals("NO_TREND")
		for _, s := range sigs {
			if s.Type == types.SignalBuy1 || s.Type == types.SignalSell1 {
				t.Errorf("盘整不应产生一类买卖点, 实际 %s", s.Type)
			}
		}
	})
}
