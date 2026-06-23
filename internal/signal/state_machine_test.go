// Package signal 信号状态机测试（PRD §8.3）。
package signal

import (
	"testing"
	"time"

	"trade/internal/chanlun"
	"trade/internal/eventbus"
	"trade/internal/types"
)

// ========================================================================
// 辅助：创建买入信号输入
// ========================================================================

func buy1SignalInput(symbol string) *chanlun.SignalInput {
	return &chanlun.SignalInput{
		Symbol: symbol,
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
			{Type: "bottomDivergence", Stroke1Idx: 2, Stroke2Idx: 4, Price1: 70, Price2: 50,
				Strength1: 30, Strength2: 15, Ratio: 0.5, Confirmed: true},
		},
	}
}

// ========================================================================
// Buy1 → Confirmed
// ========================================================================

// TestStateMachine_Buy1_Confirmed 验证：反弹笔形成 + 不创新低 → 一买确认。
func TestStateMachine_Buy1_Confirmed(t *testing.T) {
	eng := New(nil)

	// 第一次输入：产生一买候选
	eng.OnSignalInput(buy1SignalInput("TEST_1C"))

	signals := eng.GetActiveSignals("TEST_1C")
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
	if buy1.State != types.SignalCandidate {
		t.Fatalf("初始应为 candidate, 实际 %s", buy1.State)
	}
	t.Logf("一买初态: %s price=%.2f", buy1.State, buy1.Price)

	// 第二次输入：反弹笔形成（向上笔）且 low >= 信号价格（50）
	// 新增笔 5（向上反弹）+ 笔 6（继续向上确认）
	input2 := buy1SignalInput("TEST_1C")
	input2.Strokes = append(input2.Strokes,
		chanlun.StrokeInfo{Index: 5, Direction: types.DirectionUp, StartPrice: 50, EndPrice: 65, High: 65, Low: 52},
	)
	eng.OnSignalInput(input2)

	// 验证已确认为 confirmed
	signals2 := eng.GetActiveSignals("TEST_1C")
	for _, s := range signals2 {
		if s.Type == types.SignalBuy1 {
			buy1 = s
			break
		}
	}
	if buy1.State != types.SignalConfirmed {
		t.Errorf("期望 confirmed, 实际 %s", buy1.State)
	}
	t.Logf("一买终态: %s", buy1.State)
}

// ========================================================================
// Buy1 → Invalidated
// ========================================================================

// TestStateMachine_Buy1_Invalidated 验证：价格创新低 → 一买失效。
func TestStateMachine_Buy1_Invalidated(t *testing.T) {
	eng := New(nil)

	eng.OnSignalInput(buy1SignalInput("TEST_1I"))

	// 第二次输入：价格创新低（跌破 50）
	input2 := buy1SignalInput("TEST_1I")
	input2.Strokes = append(input2.Strokes,
		chanlun.StrokeInfo{Index: 5, Direction: types.DirectionDown, StartPrice: 50, EndPrice: 45, High: 50, Low: 45},
	)
	eng.OnSignalInput(input2)

	signals := eng.GetActiveSignals("TEST_1I")
	for _, s := range signals {
		if s.Type == types.SignalBuy1 {
			if s.State != types.SignalInvalidated {
				t.Errorf("期望 invalidated, 实际 %s", s.State)
			}
			t.Logf("一买: state=%s price=%.2f latestLow=45", s.State, s.Price)
			return
		}
	}
	t.Fatal("未找到一买信号")
}

// ========================================================================
// Sell1 → Confirmed
// ========================================================================

// TestStateMachine_Sell1_Confirmed 验证：回落笔形成 + 不创新高 → 一卖确认。
func TestStateMachine_Sell1_Confirmed(t *testing.T) {
	eng := New(nil)

	sellInput := &chanlun.SignalInput{
		Symbol: "TEST_S1C",
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
			{Type: "topDivergence", Stroke1Idx: 2, Stroke2Idx: 4, Price1: 80, Price2: 100,
				Strength1: 30, Strength2: 15, Ratio: 0.5, Confirmed: true},
		},
	}
	eng.OnSignalInput(sellInput)

	// 第二次输入：回落笔形成（向下）且 high <= 信号价格（100）
	input2 := sellInput
	input2.Strokes = append(input2.Strokes,
		chanlun.StrokeInfo{Index: 5, Direction: types.DirectionDown, StartPrice: 100, EndPrice: 85, High: 100, Low: 85},
	)
	eng.OnSignalInput(input2)

	signals := eng.GetActiveSignals("TEST_S1C")
	for _, s := range signals {
		if s.Type == types.SignalSell1 {
			if s.State != types.SignalConfirmed {
				t.Errorf("期望 confirmed, 实际 %s", s.State)
			}
			t.Logf("一卖: state=%s price=%.2f", s.State, s.Price)
			return
		}
	}
	t.Fatal("未找到一卖信号")
}

// ========================================================================
// Buy1 保持 Candidate（条件未满足）
// ========================================================================

// TestStateMachine_Buy1_RemainsCandidate 验证：条件不充分时信号保持 candidate。
func TestStateMachine_Buy1_RemainsCandidate(t *testing.T) {
	eng := New(nil)

	eng.OnSignalInput(buy1SignalInput("TEST_RC"))

	// 第二次输入：反弹尚未确认（笔数不够）
	input2 := buy1SignalInput("TEST_RC")
	input2.Strokes = append(input2.Strokes,
		chanlun.StrokeInfo{Index: 5, Direction: types.DirectionUp, StartPrice: 50, EndPrice: 52, High: 53, Low: 49},
	)
	// 注意：low=49 < 50，所以不满足"不创新低"
	eng.OnSignalInput(input2)

	signals := eng.GetActiveSignals("TEST_RC")
	for _, s := range signals {
		if s.Type == types.SignalBuy1 {
			if s.State != types.SignalCandidate {
				t.Errorf("条件不满足时应保持 candidate, 实际 %s", s.State)
			}
			t.Logf("一买: state=%s (如预期保持候选中)", s.State)
			return
		}
	}
	t.Fatal("未找到一买信号")
}

// ========================================================================
// 状态变更事件发布
// ========================================================================

// TestStateMachine_EventPublished 验证：状态变更时发布 EventSignalStateChanged。
func TestStateMachine_EventPublished(t *testing.T) {
	bus := eventbus.NewGeneric()
	eng := New(bus)

	var stateChangedCount int
	var lastPayload types.SignalEventPayload
	bus.Subscribe(types.EventSignalStateChanged, func(evt types.Event) {
		stateChangedCount++
		if p, ok := evt.Payload.(types.SignalEventPayload); ok {
			lastPayload = p
		}
	})

	eng.OnSignalInput(buy1SignalInput("TEST_EVT"))

	// 第二次输入触发状态变更
	input2 := buy1SignalInput("TEST_EVT")
	input2.Strokes = append(input2.Strokes,
		chanlun.StrokeInfo{Index: 5, Direction: types.DirectionDown, StartPrice: 50, EndPrice: 45, High: 50, Low: 45},
	)
	eng.OnSignalInput(input2)

	time.Sleep(5 * time.Millisecond)

	if stateChangedCount == 0 {
		t.Error("期望 EventSignalStateChanged 被发布")
	} else {
		t.Logf("状态变更事件: %d 次", stateChangedCount)
		if lastPayload.Signal != nil {
			t.Logf("最后变更: id=%s state=%s", lastPayload.Signal.SignalID, lastPayload.Signal.State)
		}
	}
}
