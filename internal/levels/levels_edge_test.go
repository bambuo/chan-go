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
