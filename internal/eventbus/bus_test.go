package eventbus

import (
	"sync/atomic"
	"testing"
	"time"

	"trade/internal/types"

	"github.com/shopspring/decimal"
)

// TestBus_PublishSubscribe 验证基本的发布-订阅功能。
func TestBus_PublishSubscribe(t *testing.T) {
	bus := New()
	received := make(chan types.KlineEvent, 1)

	id := bus.Subscribe(types.EventKlineClosed, func(evt types.KlineEvent) {
		received <- evt
	})

	event := types.KlineEvent{
		Type: types.EventKlineClosed,
		Kline: &types.Kline{
			Symbol:   "BTCUSDT",
			Open:     decimal.NewFromFloat(50000),
			High:     decimal.NewFromFloat(51000),
			Low:      decimal.NewFromFloat(49000),
			Close:    decimal.NewFromFloat(50500),
			Volume:   decimal.NewFromFloat(100),
			OpenTime: time.Now().UnixMilli(),
			IsClosed: true,
		},
	}

	bus.Publish(event)

	select {
	case evt := <-received:
		if evt.Kline.Symbol != "BTCUSDT" {
			t.Errorf("期望交易对BTCUSDT，实际为 %s", evt.Kline.Symbol)
		}
	case <-time.After(time.Second):
		t.Fatal("等待事件超时")
	}

	bus.Unsubscribe(types.EventKlineClosed, id)
}

// TestBus_NoEventForWrongType 验证订阅者只收到匹配的事件类型。
func TestBus_NoEventForWrongType(t *testing.T) {
	bus := New()
	var count atomic.Int32

	bus.Subscribe(types.EventKlineRealtime, func(evt types.KlineEvent) {
		count.Add(1)
	})

	// 发布闭合事件 - 不应触发实时订阅者。
	bus.Publish(types.KlineEvent{
		Type:  types.EventKlineClosed,
		Kline: &types.Kline{Symbol: "ETHUSDT"},
	})

	time.Sleep(100 * time.Millisecond)

	if count.Load() != 0 {
		t.Errorf("期望错误事件类型调用0次，实际 %d", count.Load())
	}
}

// TestBus_MultipleSubscribers 验证多个订阅者都能收到同一个事件。
func TestBus_MultipleSubscribers(t *testing.T) {
	bus := New()
	const n = 5
	counters := make([]atomic.Int32, n)

	for i := 0; i < n; i++ {
		idx := i
		bus.Subscribe(types.EventKlineClosed, func(evt types.KlineEvent) {
			counters[idx].Add(1)
		})
	}

	bus.Publish(types.KlineEvent{
		Type:  types.EventKlineClosed,
		Kline: &types.Kline{Symbol: "BTCUSDT"},
	})

	time.Sleep(200 * time.Millisecond)

	for i := 0; i < n; i++ {
		if counters[i].Load() != 1 {
			t.Errorf("订阅者 %d: 期望1次调用，实际 %d", i, counters[i].Load())
		}
	}
}

// TestBus_Unsubscribe 验证订阅者取消订阅后不再接收事件。
func TestBus_Unsubscribe(t *testing.T) {
	bus := New()
	var count atomic.Int32

	id := bus.Subscribe(types.EventKlineClosed, func(evt types.KlineEvent) {
		count.Add(1)
	})

	bus.Publish(types.KlineEvent{Type: types.EventKlineClosed, Kline: &types.Kline{Symbol: "BTCUSDT"}})
	time.Sleep(100 * time.Millisecond)

	bus.Unsubscribe(types.EventKlineClosed, id)

	bus.Publish(types.KlineEvent{Type: types.EventKlineClosed, Kline: &types.Kline{Symbol: "ETHUSDT"}})
	time.Sleep(100 * time.Millisecond)

	if count.Load() != 1 {
		t.Errorf("取消订阅后期望1次调用，实际 %d", count.Load())
	}
}

// TestBus_NoSubscribers 验证无订阅者时发布事件不会panic。
func TestBus_NoSubscribers(t *testing.T) {
	bus := New()
	bus.Publish(types.KlineEvent{Type: types.EventKlineClosed, Kline: &types.Kline{Symbol: "BTCUSDT"}})
	// 不应panic。
}

// TestBus_PanicRecovery 验证panic的订阅者不会导致总线或其他订阅者崩溃。
func TestBus_PanicRecovery(t *testing.T) {
	bus := New()
	var count atomic.Int32

	bus.Subscribe(types.EventKlineClosed, func(evt types.KlineEvent) {
		panic("预期内的panic")
	})
	bus.Subscribe(types.EventKlineClosed, func(evt types.KlineEvent) {
		count.Add(1)
	})

	bus.Publish(types.KlineEvent{Type: types.EventKlineClosed, Kline: &types.Kline{Symbol: "BTCUSDT"}})

	time.Sleep(200 * time.Millisecond)

	if count.Load() != 1 {
		t.Errorf("期望存活订阅者1次调用，实际 %d", count.Load())
	}
}

// TestBus_SubscriberCount 验证订阅者计数。
func TestBus_SubscriberCount(t *testing.T) {
	bus := New()

	if c := bus.SubscriberCount(types.EventKlineClosed); c != 0 {
		t.Errorf("期望初始0个订阅者，实际 %d", c)
	}

	id1 := bus.Subscribe(types.EventKlineClosed, func(evt types.KlineEvent) {})
	id2 := bus.Subscribe(types.EventKlineClosed, func(evt types.KlineEvent) {})

	if c := bus.SubscriberCount(types.EventKlineClosed); c != 2 {
		t.Errorf("期望2个订阅者，实际 %d", c)
	}

	bus.Unsubscribe(types.EventKlineClosed, id1)
	if c := bus.SubscriberCount(types.EventKlineClosed); c != 1 {
		t.Errorf("取消订阅后期望1个订阅者，实际 %d", c)
	}

	bus.Unsubscribe(types.EventKlineClosed, id2)
	if c := bus.SubscriberCount(types.EventKlineClosed); c != 0 {
		t.Errorf("全部取消订阅后期望0个订阅者，实际 %d", c)
	}
}
