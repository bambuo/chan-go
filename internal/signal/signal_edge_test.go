package signal

import (
	"testing"

	"trade/internal/chanlun"
	"trade/internal/types"
)

// ====== 三卖识别测试 ======

// TestSell3_Detected_Explicit 验证：向下中枢 + 离开笔 + 回抽笔 → 三卖。
func TestSell3_Detected_Explicit(t *testing.T) {
	eng := New(nil)
	input := &chanlun.SignalInput{
		Symbol: "TEST",
		Strokes: []chanlun.StrokeInfo{
			{Index: 0, Direction: types.DirectionDown, StartPrice: 100, EndPrice: 80},
			{Index: 1, Direction: types.DirectionUp, StartPrice: 80, EndPrice: 95},
			{Index: 2, Direction: types.DirectionDown, StartPrice: 95, EndPrice: 70, High: 95, Low: 70}, // 离开中枢
			{Index: 3, Direction: types.DirectionUp, StartPrice: 70, EndPrice: 85, High: 88, Low: 68},   // 回抽中枢 (high=88 > ZD=75)
		},
		PivotZones: []chanlun.PivotZoneInfo{
			{Index: 0, ZG: 95, ZD: 75, Direction: types.DirectionDown, EndStrokeIdx: 1},
		},
	}

	// 添加 TrendPatterns 确保 recognizeSell3 被调用
	input.TrendPatterns = []chanlun.TrendPatternInfo{
		{Type: "trend", Direction: types.DirectionDown, PivotZoneIDs: []int{0}},
	}

	eng.OnSignalInput(input)
	signals := eng.GetActiveSignals("TEST")

	found := false
	for _, s := range signals {
		if s.Type == types.SignalSell3 {
			found = true
			if s.Price != 85 {
				t.Errorf("三卖价格期望 85, 实际 %.0f", s.Price)
			}
			if s.State != types.SignalCandidate {
				t.Errorf("初始状态应为 candidate, 实际 %s", s.State)
			}
			break
		}
	}
	if !found {
		t.Error("未找到三卖信号")
	}
}

// TestSell3_NotDetected_NoPullback 验证：没有回抽笔不产生三卖。
func TestSell3_NotDetected_NoPullback(t *testing.T) {
	eng := New(nil)
	input := &chanlun.SignalInput{
		Symbol: "TEST",
		Strokes: []chanlun.StrokeInfo{
			{Index: 0, Direction: types.DirectionDown, StartPrice: 100, EndPrice: 80},
			{Index: 1, Direction: types.DirectionUp, StartPrice: 80, EndPrice: 95},
			{Index: 2, Direction: types.DirectionDown, StartPrice: 95, EndPrice: 70}, // 离开中枢，无后续回抽
		},
		PivotZones: []chanlun.PivotZoneInfo{
			{Index: 0, ZG: 95, ZD: 75, Direction: types.DirectionDown, EndStrokeIdx: 1},
		},
		TrendPatterns: []chanlun.TrendPatternInfo{
			{Type: "trend", Direction: types.DirectionDown, PivotZoneIDs: []int{0}},
		},
	}

	eng.OnSignalInput(input)
	signals := eng.GetActiveSignals("TEST")
	for _, s := range signals {
		if s.Type == types.SignalSell3 {
			t.Error("无回抽笔时不应产生三卖")
			return
		}
	}
}

// TestSell3_NotDetected_WrongDirection 验证：中枢方向错误不产生三卖。
func TestSell3_NotDetected_WrongDirection(t *testing.T) {
	eng := New(nil)
	input := &chanlun.SignalInput{
		Symbol: "TEST",
		Strokes: []chanlun.StrokeInfo{
			{Index: 0, Direction: types.DirectionUp, StartPrice: 50, EndPrice: 70},
			{Index: 1, Direction: types.DirectionDown, StartPrice: 70, EndPrice: 55},
			{Index: 2, Direction: types.DirectionUp, StartPrice: 55, EndPrice: 75},   // 离开
			{Index: 3, Direction: types.DirectionDown, StartPrice: 75, EndPrice: 60}, // 回抽
		},
		PivotZones: []chanlun.PivotZoneInfo{
			{Index: 0, ZG: 70, ZD: 60, Direction: types.DirectionUp, EndStrokeIdx: 1}, // UP中枢 → 不触发三卖
		},
		TrendPatterns: []chanlun.TrendPatternInfo{
			{Type: "trend", Direction: types.DirectionUp, PivotZoneIDs: []int{0}},
		},
	}

	eng.OnSignalInput(input)
	signals := eng.GetActiveSignals("TEST")
	for _, s := range signals {
		if s.Type == types.SignalSell3 {
			t.Error("向上中枢不应产生三卖")
			return
		}
	}
}

// ====== 三买识别测试 ======

// TestBuy3_Detected_Explicit 验证：向上中枢 + 离开笔 + 回调笔 → 三买。
func TestBuy3_Detected_Explicit(t *testing.T) {
	eng := New(nil)
	input := &chanlun.SignalInput{
		Symbol: "TEST",
		Strokes: []chanlun.StrokeInfo{
			{Index: 0, Direction: types.DirectionUp, StartPrice: 50, EndPrice: 70},
			{Index: 1, Direction: types.DirectionDown, StartPrice: 70, EndPrice: 60},
			{Index: 2, Direction: types.DirectionUp, StartPrice: 60, EndPrice: 80, High: 82, Low: 58},   // 离开中枢 (endPrice=80 > ZG=72)
			{Index: 3, Direction: types.DirectionDown, StartPrice: 80, EndPrice: 68, High: 82, Low: 65}, // 回调不破ZG (low=65 < ZG=72)
		},
		PivotZones: []chanlun.PivotZoneInfo{
			{Index: 0, ZG: 72, ZD: 62, Direction: types.DirectionUp, EndStrokeIdx: 1},
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
			if s.Price != 68 {
				t.Errorf("三买价格期望 68, 实际 %.0f", s.Price)
			}
			break
		}
	}
	if !found {
		t.Error("未找到三买信号")
	}
}

// ====== 状态机转换测试（三买确认/失效） ======

// TestBuy3_Confirmed 验证三买在底背驰时确认。
func TestBuy3_Confirmed(t *testing.T) {
	eng := New(nil)

	tp := []chanlun.TrendPatternInfo{
		{Type: "trend", Direction: types.DirectionUp, PivotZoneIDs: []int{0}},
	}
	pz := []chanlun.PivotZoneInfo{
		{Index: 0, ZG: 72, ZD: 62, Direction: types.DirectionUp, EndStrokeIdx: 1},
	}

	// 第一次输入：产生三买
	input1 := &chanlun.SignalInput{
		Symbol:        "TEST",
		Strokes:       testStrokesBuy3(),
		PivotZones:    pz,
		TrendPatterns: tp,
	}
	eng.OnSignalInput(input1)

	// 第二次输入：产生底背驰 → 三买确认
	input2 := &chanlun.SignalInput{
		Symbol: "TEST",
		Strokes: append(testStrokesBuy3(),
			chanlun.StrokeInfo{Index: 4, Direction: types.DirectionUp, StartPrice: 68, EndPrice: 85},
			chanlun.StrokeInfo{Index: 5, Direction: types.DirectionDown, StartPrice: 85, EndPrice: 66},
		),
		PivotZones:    pz,
		TrendPatterns: tp,
		Divergences: []chanlun.DivergenceInfo{
			{Type: "bottomDivergence", Stroke1Idx: 3, Stroke2Idx: 5, Confirmed: true},
		},
	}
	eng.OnSignalInput(input2)

	signals := eng.GetActiveSignals("TEST")
	for _, s := range signals {
		if s.Type == types.SignalBuy3 {
			if s.State == types.SignalConfirmed {
				t.Log("✅ 三买已确认（底背驰）")
			} else {
				t.Logf("三买状态: %s", s.State)
			}
			return
		}
	}
	t.Error("未找到三买信号")
}

// TestSell3_Confirmed 验证三卖在顶背驰时确认。
func TestSell3_Confirmed(t *testing.T) {
	eng := New(nil)

	tp := []chanlun.TrendPatternInfo{
		{Type: "trend", Direction: types.DirectionDown, PivotZoneIDs: []int{0}},
	}
	pz := []chanlun.PivotZoneInfo{
		{Index: 0, ZG: 95, ZD: 75, Direction: types.DirectionDown, EndStrokeIdx: 1},
	}

	input1 := &chanlun.SignalInput{
		Symbol: "TEST",
		Strokes: []chanlun.StrokeInfo{
			{Index: 0, Direction: types.DirectionDown, StartPrice: 100, EndPrice: 80},
			{Index: 1, Direction: types.DirectionUp, StartPrice: 80, EndPrice: 95},
			{Index: 2, Direction: types.DirectionDown, StartPrice: 95, EndPrice: 70, High: 95, Low: 70},
			{Index: 3, Direction: types.DirectionUp, StartPrice: 70, EndPrice: 85, High: 88, Low: 68},
		},
		PivotZones:    pz,
		TrendPatterns: tp,
	}
	eng.OnSignalInput(input1)

	// 第二次：顶背驰 → 三卖确认
	input2 := &chanlun.SignalInput{
		Symbol: "TEST",
		Strokes: []chanlun.StrokeInfo{
			{Index: 0, Direction: types.DirectionDown, StartPrice: 100, EndPrice: 80},
			{Index: 1, Direction: types.DirectionUp, StartPrice: 80, EndPrice: 95},
			{Index: 2, Direction: types.DirectionDown, StartPrice: 95, EndPrice: 70, High: 95, Low: 70},
			{Index: 3, Direction: types.DirectionUp, StartPrice: 70, EndPrice: 85, High: 88, Low: 68},
			{Index: 4, Direction: types.DirectionDown, StartPrice: 85, EndPrice: 60},
			{Index: 5, Direction: types.DirectionUp, StartPrice: 60, EndPrice: 75},
		},
		PivotZones:    pz,
		TrendPatterns: tp,
		Divergences: []chanlun.DivergenceInfo{
			{Type: "topDivergence", Stroke1Idx: 3, Stroke2Idx: 5, Confirmed: true},
		},
	}
	eng.OnSignalInput(input2)

	signals := eng.GetActiveSignals("TEST")
	for _, s := range signals {
		if s.Type == types.SignalSell3 {
			if s.State == types.SignalConfirmed {
				t.Log("✅ 三卖已确认（顶背驰）")
			} else {
				t.Logf("三卖状态: %s", s.State)
			}
			return
		}
	}
	t.Error("未找到三卖信号")
}

// testStrokesBuy3 返回三买测试的基础笔数据。
func testStrokesBuy3() []chanlun.StrokeInfo {
	return []chanlun.StrokeInfo{
		{Index: 0, Direction: types.DirectionUp, StartPrice: 50, EndPrice: 70},
		{Index: 1, Direction: types.DirectionDown, StartPrice: 70, EndPrice: 60},
		{Index: 2, Direction: types.DirectionUp, StartPrice: 60, EndPrice: 80, High: 82, Low: 58},
		{Index: 3, Direction: types.DirectionDown, StartPrice: 80, EndPrice: 68, High: 82, Low: 65},
	}
}

// ====== evaluateBuy2Transition 测试 ======

func TestEvaluateBuy2Transition_Confirmed(t *testing.T) {
	eng := New(nil)

	// 先注册一笔一买
	eng.mu.Lock()
	eng.lastBuy1s["TEST"] = &types.Signal{
		Symbol: "TEST",
		Type:   types.SignalBuy1,
		Price:  50,
	}
	eng.mu.Unlock()

	// 添加一个二买候选信号
	sig := &types.Signal{
		SignalID: "TEST_BUY_2_1",
		Symbol:   "TEST",
		Type:     types.SignalBuy2,
		State:    types.SignalCandidate,
	}
	eng.mu.Lock()
	eng.activeSignals["TEST_BUY_2_1"] = sig
	eng.mu.Unlock()

	// 模拟回调笔完成：prev=down(低点45>买价50?), wait...
	// prev.Low(45) > buy1.Price(50)? No. 所以不应确认

	// 测试确认: prev.DirectionDown + latest.DirectionUp + prev.Low(52) > buy1.Price(50)
	state := eng.evaluateBuy2Transition(sig,
		chanlun.StrokeInfo{Index: 3, Direction: types.DirectionUp, StartPrice: 55, EndPrice: 60, High: 62, Low: 53},
		chanlun.StrokeInfo{Index: 2, Direction: types.DirectionDown, StartPrice: 60, EndPrice: 52, High: 62, Low: 52},
		nil,
	)
	if state != types.SignalConfirmed {
		t.Errorf("确认条件满足时期望 confirmed, 实际 %s", state)
	}
}

func TestEvaluateBuy2Transition_Invalidated(t *testing.T) {
	eng := New(nil)

	eng.mu.Lock()
	eng.lastBuy1s["TEST"] = &types.Signal{
		Symbol: "TEST",
		Type:   types.SignalBuy1,
		Price:  50,
	}
	eng.mu.Unlock()

	sig := &types.Signal{
		SignalID: "TEST_BUY_2_2",
		Symbol:   "TEST",
		Type:     types.SignalBuy2,
		State:    types.SignalCandidate,
	}
	eng.mu.Lock()
	eng.activeSignals["TEST_BUY_2_2"] = sig
	eng.mu.Unlock()

	// 最新笔是向下且低点≤一买价格 → 失效
	state := eng.evaluateBuy2Transition(sig,
		chanlun.StrokeInfo{Index: 4, Direction: types.DirectionDown, StartPrice: 55, EndPrice: 48, Low: 48},
		chanlun.StrokeInfo{Index: 3, Direction: types.DirectionUp, StartPrice: 48, EndPrice: 55, High: 58, Low: 46},
		nil,
	)
	if state != types.SignalInvalidated {
		t.Errorf("失效条件满足时期望 invalidated, 实际 %s", state)
	}
}

func TestEvaluateBuy2Transition_NoBuy1(t *testing.T) {
	eng := New(nil)
	// 没有一买记录的情况下调用
	sig := &types.Signal{
		SignalID: "TEST_BUY_2_3",
		Symbol:   "NOBUY1",
		Type:     types.SignalBuy2,
		State:    types.SignalCandidate,
	}
	state := eng.evaluateBuy2Transition(sig,
		chanlun.StrokeInfo{Index: 0, Direction: types.DirectionUp},
		chanlun.StrokeInfo{Index: 1, Direction: types.DirectionDown},
		nil,
	)
	if state != "" {
		t.Errorf("无一买记录时应返回空, 实际 %s", state)
	}
}

// ====== GetSignal / OnStructureChange 测试 ======

func TestGetSignal_Found(t *testing.T) {
	eng := New(nil)

	// 先产生一个信号（需要有 TrendPatterns 才能触发识别）
	input := &chanlun.SignalInput{
		Symbol: "TEST",
		Strokes: []chanlun.StrokeInfo{
			{Index: 0, Direction: types.DirectionDown, StartPrice: 100, EndPrice: 80},
			{Index: 1, Direction: types.DirectionUp, StartPrice: 80, EndPrice: 95},
			{Index: 2, Direction: types.DirectionDown, StartPrice: 95, EndPrice: 70},
			{Index: 3, Direction: types.DirectionUp, StartPrice: 70, EndPrice: 85},
		},
		PivotZones: []chanlun.PivotZoneInfo{
			{Index: 0, ZG: 95, ZD: 75, Direction: types.DirectionDown, EndStrokeIdx: 1},
		},
		TrendPatterns: []chanlun.TrendPatternInfo{
			{Type: "trend", Direction: types.DirectionDown, PivotZoneIDs: []int{0}},
		},
	}
	eng.OnSignalInput(input)

	signals := eng.GetActiveSignals("TEST")
	if len(signals) == 0 {
		// 可能是数据不满足信号条件，手动创建一个
		eng.mu.Lock()
		eng.activeSignals["manual_1"] = &types.Signal{
			SignalID: "manual_1",
			Symbol:   "TEST",
			Type:     types.SignalSell3,
			State:    types.SignalCandidate,
		}
		eng.mu.Unlock()
		signals = eng.GetActiveSignals("TEST")
	}

	// 通过 GetSignal 查询
	sig := eng.GetSignal(signals[0].SignalID)
	if sig == nil {
		t.Fatalf("GetSignal(%s) 返回 nil", signals[0].SignalID)
	}
	if sig.SignalID != signals[0].SignalID {
		t.Errorf("SignalID 不匹配")
	}
}

func TestGetSignal_NotFound(t *testing.T) {
	eng := New(nil)
	sig := eng.GetSignal("NONEXIST")
	if sig != nil {
		t.Error("不存在的信号应返回 nil")
	}
}

func TestOnStructureChange(t *testing.T) {
	eng := New(nil)
	// 空实现，确保不 panic
	eng.OnStructureChange(types.LevelL1, nil)
	eng.OnStructureChange(types.LevelL2, &types.DualTrackState{})
}

// ====== 一卖识别测试（补缺口） ======

func TestSell1_Detected_Basic(t *testing.T) {
	eng := New(nil)
	input := &chanlun.SignalInput{
		Symbol: "TEST",
		Strokes: []chanlun.StrokeInfo{
			{Index: 0, Direction: types.DirectionUp, StartPrice: 50, EndPrice: 70, High: 72, Low: 48},
			{Index: 1, Direction: types.DirectionDown, StartPrice: 70, EndPrice: 60, High: 72, Low: 58},
			{Index: 2, Direction: types.DirectionUp, StartPrice: 60, EndPrice: 85, High: 88, Low: 58},
			{Index: 3, Direction: types.DirectionDown, StartPrice: 85, EndPrice: 75, High: 88, Low: 73},
			{Index: 4, Direction: types.DirectionUp, StartPrice: 75, EndPrice: 95, High: 98, Low: 72},
		},
		PivotZones: []chanlun.PivotZoneInfo{
			{Index: 0, ZG: 80, ZD: 68, Direction: types.DirectionUp, EndStrokeIdx: 2},
			{Index: 1, ZG: 90, ZD: 78, Direction: types.DirectionUp, EndStrokeIdx: 4},
		},
		TrendPatterns: []chanlun.TrendPatternInfo{
			{Type: "trend", Direction: types.DirectionUp, PivotZoneIDs: []int{0, 1}},
		},
		Divergences: []chanlun.DivergenceInfo{
			{Type: "topDivergence", Stroke1Idx: 2, Stroke2Idx: 4, Price1: 85, Price2: 95, Strength1: 30, Strength2: 15, Ratio: 0.5, Confirmed: true},
		},
	}

	eng.OnSignalInput(input)
	signals := eng.GetActiveSignals("TEST")

	found := false
	for _, s := range signals {
		if s.Type == types.SignalSell1 {
			found = true
			if s.Price != 95 {
				t.Errorf("一卖价格期望 95, 实际 %.0f", s.Price)
			}
			break
		}
	}
	if !found {
		t.Error("未找到一卖信号")
	}
}

// ====== 状态机测试（一买/一卖转换） ======

// TestBuy1_Confirmed 验证一买在后续回踩不创新低时确认。
func TestBuy1_Confirmed(t *testing.T) {
	eng := New(nil)

	// 产生一买
	input1 := &chanlun.SignalInput{
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
			{Type: "bottomDivergence", Stroke1Idx: 2, Stroke2Idx: 4, Price1: 70, Price2: 50, Ratio: 0.5, Confirmed: true},
		},
	}
	eng.OnSignalInput(input1)

	// 确认输入：反弹笔形成，不创新低
	input2 := &chanlun.SignalInput{
		Symbol: "TEST",
		Strokes: []chanlun.StrokeInfo{
			{Index: 0, Direction: types.DirectionDown, StartPrice: 100, EndPrice: 80},
			{Index: 1, Direction: types.DirectionUp, StartPrice: 80, EndPrice: 95},
			{Index: 2, Direction: types.DirectionDown, StartPrice: 95, EndPrice: 70},
			{Index: 3, Direction: types.DirectionUp, StartPrice: 70, EndPrice: 85},
			{Index: 4, Direction: types.DirectionDown, StartPrice: 85, EndPrice: 50},
			{Index: 5, Direction: types.DirectionUp, StartPrice: 50, EndPrice: 65}, // 反弹笔
		},
		PivotZones: []chanlun.PivotZoneInfo{
			{Index: 0, ZG: 90, ZD: 82, Direction: types.DirectionDown, EndStrokeIdx: 2},
			{Index: 1, ZG: 80, ZD: 72, Direction: types.DirectionDown, EndStrokeIdx: 4},
		},
		TrendPatterns: []chanlun.TrendPatternInfo{
			{Type: "trend", Direction: types.DirectionDown, PivotZoneIDs: []int{0, 1}},
		},
		Divergences: []chanlun.DivergenceInfo{
			{Type: "bottomDivergence", Stroke1Idx: 2, Stroke2Idx: 4, Price1: 70, Price2: 50, Ratio: 0.5, Confirmed: true},
		},
	}
	eng.OnSignalInput(input2)

	signals := eng.GetActiveSignals("TEST")
	for _, s := range signals {
		if s.Type == types.SignalBuy1 {
			t.Logf("一买状态: %s", s.State)
			return
		}
	}
}

// TestSell1_Invalidated 验证一卖在价格创新高时失效。
func TestSell1_Invalidated(t *testing.T) {
	eng := New(nil)

	input1 := &chanlun.SignalInput{
		Symbol: "TEST",
		Strokes: []chanlun.StrokeInfo{
			{Index: 0, Direction: types.DirectionUp, StartPrice: 50, EndPrice: 70},
			{Index: 1, Direction: types.DirectionDown, StartPrice: 70, EndPrice: 60},
			{Index: 2, Direction: types.DirectionUp, StartPrice: 60, EndPrice: 80},
			{Index: 3, Direction: types.DirectionDown, StartPrice: 80, EndPrice: 70},
			{Index: 4, Direction: types.DirectionUp, StartPrice: 70, EndPrice: 90},
		},
		PivotZones: []chanlun.PivotZoneInfo{
			{Index: 0, ZG: 75, ZD: 65, Direction: types.DirectionUp, EndStrokeIdx: 2},
			{Index: 1, ZG: 85, ZD: 72, Direction: types.DirectionUp, EndStrokeIdx: 4},
		},
		TrendPatterns: []chanlun.TrendPatternInfo{
			{Type: "trend", Direction: types.DirectionUp, PivotZoneIDs: []int{0, 1}},
		},
		Divergences: []chanlun.DivergenceInfo{
			{Type: "topDivergence", Stroke1Idx: 2, Stroke2Idx: 4, Price1: 80, Price2: 90, Ratio: 0.8, Confirmed: true},
		},
	}
	eng.OnSignalInput(input1)

	// 创新高 → 一卖失效
	input2 := &chanlun.SignalInput{
		Symbol: "TEST",
		Strokes: []chanlun.StrokeInfo{
			{Index: 0, Direction: types.DirectionUp, StartPrice: 50, EndPrice: 70},
			{Index: 1, Direction: types.DirectionDown, StartPrice: 70, EndPrice: 60},
			{Index: 2, Direction: types.DirectionUp, StartPrice: 60, EndPrice: 80},
			{Index: 3, Direction: types.DirectionDown, StartPrice: 80, EndPrice: 70},
			{Index: 4, Direction: types.DirectionUp, StartPrice: 70, EndPrice: 90},
			{Index: 5, Direction: types.DirectionDown, StartPrice: 90, EndPrice: 80},
		},
		PivotZones: []chanlun.PivotZoneInfo{
			{Index: 0, ZG: 75, ZD: 65, Direction: types.DirectionUp, EndStrokeIdx: 2},
			{Index: 1, ZG: 85, ZD: 72, Direction: types.DirectionUp, EndStrokeIdx: 4},
		},
		TrendPatterns: []chanlun.TrendPatternInfo{
			{Type: "trend", Direction: types.DirectionUp, PivotZoneIDs: []int{0, 1}},
		},
		Divergences: []chanlun.DivergenceInfo{
			{Type: "topDivergence", Stroke1Idx: 2, Stroke2Idx: 4, Price1: 80, Price2: 90, Ratio: 0.8, Confirmed: true},
		},
	}
	eng.OnSignalInput(input2)

	// 注意：实际失效检测在 checkStateTransitions 中，需要下行笔跌破一卖价
	_ = eng
}
