package levels

import (
	"testing"

	"trade/internal/eventbus"
	"trade/internal/structure"
	"trade/internal/types"
)

// TestOnLowerLevelComplete 验证低级别完成回调不 panic。
func TestOnLowerLevelComplete(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := structure.New(bus)
	builder := New(bus, tree)

	// 空实现，确保不 panic
	builder.OnLowerLevelComplete("TEST", types.LevelL1, nil)
	builder.OnLowerLevelComplete("", types.LevelL2, &types.TrendPattern{})
}

// TestGetState 验证获取状态。
func TestGetState(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := structure.New(bus)
	builder := New(bus, tree)

	state := builder.GetState("TEST", types.LevelL1)
	if state != nil {
		t.Log("GetState 返回非 nil（初始状态可能存在）")
	}
}

// ====== detectLevelDrift 测试 ======

func TestDetectLevelDrift_InSync(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := structure.New(bus)
	builder := New(bus, tree)

	// InSync=true 不应触发漂移
	state := &types.DualTrackState{
		Provisional: types.LevelStructure{Strokes: make([]types.Stroke, 10)},
		Confirmed:   types.LevelStructure{Strokes: make([]types.Stroke, 5)},
		InSync:      true,
	}
	builder.detectLevelDrift("TEST", types.LevelL1, state)
	// 不 panic 且不产生事件
}

func TestDetectLevelDrift_NoConfirmed(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := structure.New(bus)
	builder := New(bus, tree)

	// Confirmed 为空时不触发
	state := &types.DualTrackState{
		Provisional: types.LevelStructure{Strokes: make([]types.Stroke, 10)},
		Confirmed:   types.LevelStructure{Strokes: make([]types.Stroke, 0)},
		InSync:      false,
	}
	builder.detectLevelDrift("TEST", types.LevelL1, state)
}

func TestDetectLevelDrift_BelowThreshold(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := structure.New(bus)
	builder := New(bus, tree)

	// driftThresholdPct=0.3, ratio=(5-4)/4=0.25 < 0.3 → 不触发
	state := &types.DualTrackState{
		Provisional: types.LevelStructure{Strokes: make([]types.Stroke, 5)},
		Confirmed:   types.LevelStructure{Strokes: make([]types.Stroke, 4)},
		InSync:      false,
	}
	builder.detectLevelDrift("TEST", types.LevelL1, state)
}

func TestDetectLevelDrift_ExceedsThreshold(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := structure.New(bus)
	builder := New(bus, tree)

	// ratio=(10-4)/4=1.5 > 0.3 → 应触发漂移事件
	var eventCount int
	bus.Subscribe(types.EventLevelRecast, func(evt types.Event) {
		eventCount++
		if evt.Symbol != "TEST" {
			t.Errorf("Symbol 期望 TEST, 实际 %s", evt.Symbol)
		}
	})

	state := &types.DualTrackState{
		Provisional: types.LevelStructure{Strokes: make([]types.Stroke, 10)},
		Confirmed:   types.LevelStructure{Strokes: make([]types.Stroke, 4)},
		InSync:      false,
	}
	builder.detectLevelDrift("TEST", types.LevelL1, state)

	if eventCount == 0 {
		t.Error("漂移超过阈值时应发布 EventLevelRecast 事件")
	}
}

func TestDetectLevelDrift_ZeroConfirmed(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := structure.New(bus)
	builder := New(bus, tree)

	// Confirmed=0, ratio 无意义, 直接返回
	state := &types.DualTrackState{
		Provisional: types.LevelStructure{Strokes: make([]types.Stroke, 10)},
		Confirmed:   types.LevelStructure{Strokes: make([]types.Stroke, 0)},
		InSync:      false,
	}
	builder.detectLevelDrift("TEST", types.LevelL1, state)
	// 不应 panic
}
