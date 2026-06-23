package resonance

import (
	"testing"
	"time"

	"trade/internal/eventbus"
	"trade/internal/structure"
	"trade/internal/types"
)

// ====== isSameDirection 测试 ======

func TestIsSameDirection_DownBuy(t *testing.T) {
	// 大级别向下 → 买信号 = true
	if !isSameDirection(types.DirectionDown, types.SignalBuy1) {
		t.Error("down+Buy1 应 true")
	}
	if !isSameDirection(types.DirectionDown, types.SignalBuy2) {
		t.Error("down+Buy2 应 true")
	}
	if !isSameDirection(types.DirectionDown, types.SignalBuy3) {
		t.Error("down+Buy3 应 true")
	}
}

func TestIsSameDirection_DownSell(t *testing.T) {
	// 大级别向下 → 卖信号 = false
	if isSameDirection(types.DirectionDown, types.SignalSell1) {
		t.Error("down+Sell1 应 false")
	}
}

func TestIsSameDirection_UpSell(t *testing.T) {
	// 大级别向上 → 卖信号 = true
	if !isSameDirection(types.DirectionUp, types.SignalSell1) {
		t.Error("up+Sell1 应 true")
	}
	if !isSameDirection(types.DirectionUp, types.SignalSell2) {
		t.Error("up+Sell2 应 true")
	}
	if !isSameDirection(types.DirectionUp, types.SignalSell3) {
		t.Error("up+Sell3 应 true")
	}
}

func TestIsSameDirection_UpBuy(t *testing.T) {
	if isSameDirection(types.DirectionUp, types.SignalBuy1) {
		t.Error("up+Buy1 应 false")
	}
}

func TestIsSameDirection_None(t *testing.T) {
	if isSameDirection(types.DirectionNone, types.SignalBuy1) {
		t.Error("none+Buy1 应 false")
	}
	if isSameDirection(types.DirectionNone, types.SignalSell1) {
		t.Error("none+Sell1 应 false")
	}
}

// ====== OnTimeout 测试 ======

func TestOnTimeout_NoWindows(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := structure.New(bus)
	eng := New(bus, tree)

	// 无等待窗口，不应 panic
	eng.OnTimeout()
}

func TestOnTimeout_ExpiredWindow(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := structure.New(bus)
	eng := New(bus, tree)

	// 注册一个已经超时的窗口
	eng.mu.Lock()
	eng.pendingWindows["expired_1"] = &resonanceWindow{
		Signal: &types.Signal{
			SignalID: "expired_1",
			Symbol:   "TEST",
			Type:     types.SignalBuy1,
			State:    types.SignalCandidate,
		},
		StartTS:   1000,
		TimeoutTS: time.Now().UnixMilli() - 1000, // 已超时
	}
	eng.mu.Unlock()

	// 触发超时检查
	eng.OnTimeout()

	// 验证窗口已被删除
	eng.mu.RLock()
	_, exists := eng.pendingWindows["expired_1"]
	eng.mu.RUnlock()
	if exists {
		t.Error("超时窗口应被删除")
	}
}

func TestOnTimeout_NotExpired(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := structure.New(bus)
	eng := New(bus, tree)

	eng.mu.Lock()
	eng.pendingWindows["active_1"] = &resonanceWindow{
		Signal: &types.Signal{
			SignalID: "active_1",
			Symbol:   "TEST",
			Type:     types.SignalBuy1,
		},
		StartTS:   time.Now().UnixMilli(),
		TimeoutTS: time.Now().UnixMilli() + 60000, // 1分钟后才超时
	}
	eng.mu.Unlock()

	eng.OnTimeout()

	// 窗口应仍在
	eng.mu.RLock()
	_, exists := eng.pendingWindows["active_1"]
	eng.mu.RUnlock()
	if !exists {
		t.Error("未超时的窗口不应被删除")
	}
}

// ====== isSameDirectionBuySell 测试 ======

func TestIsSameDirectionBuySell(t *testing.T) {
	if !isSameDirectionBuySell(types.SignalBuy1, types.SignalBuy2) {
		t.Error("两个买信号应 true")
	}
	if !isSameDirectionBuySell(types.SignalSell1, types.SignalSell3) {
		t.Error("两个卖信号应 true")
	}
	if isSameDirectionBuySell(types.SignalBuy1, types.SignalSell1) {
		t.Error("买+卖应 false")
	}
	if isSameDirectionBuySell(types.SignalBuy3, types.SignalSell2) {
		t.Error("买+卖应 false")
	}
}

// ====== isBuyType 测试 ======

func TestIsBuyType(t *testing.T) {
	if !isBuyType(types.SignalBuy1) {
		t.Error("Buy1 应 true")
	}
	if !isBuyType(types.SignalBuy2) {
		t.Error("Buy2 应 true")
	}
	if !isBuyType(types.SignalBuy3) {
		t.Error("Buy3 应 true")
	}
	if isBuyType(types.SignalSell1) {
		t.Error("Sell1 应 false")
	}
}
