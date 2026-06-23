package eventbus

import (
	"sync/atomic"
	"testing"
	"time"

	"trade/internal/types"
)

// TestGeneric_PublishSubscribe 验证 GenericBus 的基本发布-订阅功能。
func TestGeneric_PublishSubscribe(t *testing.T) {
	bus := NewGeneric()
	received := make(chan types.Event, 1)

	id := bus.Subscribe(types.EventSignalCreated, func(evt types.Event) {
		received <- evt
	})

	event := types.Event{
		Type:   types.EventSignalCreated,
		Symbol: "BTCUSDT",
		TS:     time.Now().UnixMilli(),
		Payload: types.SignalEventPayload{
			Signal: &types.Signal{Symbol: "BTCUSDT"},
		},
	}
	bus.Publish(event)

	select {
	case evt := <-received:
		if evt.Symbol != "BTCUSDT" {
			t.Errorf("期望 Symbol=BTCUSDT, 实际 %s", evt.Symbol)
		}
		if evt.Type != types.EventSignalCreated {
			t.Errorf("期望 Type=signal.created, 实际 %s", evt.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("等待事件超时")
	}

	bus.Unsubscribe(types.EventSignalCreated, id)
}

// TestGeneric_NoEventForWrongType 验证订阅者只收到匹配的事件类型。
func TestGeneric_NoEventForWrongType(t *testing.T) {
	bus := NewGeneric()
	var count atomic.Int32

	bus.Subscribe(types.EventSignalCreated, func(evt types.Event) {
		count.Add(1)
	})

	// 发布不匹配类型的事件
	bus.Publish(types.Event{
		Type:   types.EventResonanceTriggered,
		Symbol: "ETHUSDT",
	})

	time.Sleep(100 * time.Millisecond)

	if count.Load() != 0 {
		t.Errorf("期望不匹配事件调用0次，实际 %d", count.Load())
	}
}

// TestGeneric_MultipleSubscribers 验证多个订阅者都能收到同一事件。
func TestGeneric_MultipleSubscribers(t *testing.T) {
	bus := NewGeneric()
	const n = 5
	counters := make([]atomic.Int32, n)

	for i := 0; i < n; i++ {
		idx := i
		bus.Subscribe(types.EventSignalCreated, func(evt types.Event) {
			counters[idx].Add(1)
		})
	}

	bus.Publish(types.Event{
		Type:   types.EventSignalCreated,
		Symbol: "BTCUSDT",
	})

	time.Sleep(200 * time.Millisecond)

	for i := 0; i < n; i++ {
		if counters[i].Load() != 1 {
			t.Errorf("订阅者 %d: 期望1次调用，实际 %d", i, counters[i].Load())
		}
	}
}

// TestGeneric_Unsubscribe 验证取消后不再接收事件。
func TestGeneric_Unsubscribe(t *testing.T) {
	bus := NewGeneric()
	var count atomic.Int32

	id := bus.Subscribe(types.EventSignalCreated, func(evt types.Event) {
		count.Add(1)
	})

	bus.Publish(types.Event{Type: types.EventSignalCreated, Symbol: "BTCUSDT"})
	time.Sleep(100 * time.Millisecond)

	bus.Unsubscribe(types.EventSignalCreated, id)

	bus.Publish(types.Event{Type: types.EventSignalCreated, Symbol: "ETHUSDT"})
	time.Sleep(100 * time.Millisecond)

	if count.Load() != 1 {
		t.Errorf("取消订阅后期望1次调用，实际 %d", count.Load())
	}
}

// TestGeneric_NoSubscribers 验证无订阅者时发布不 panic。
func TestGeneric_NoSubscribers(t *testing.T) {
	bus := NewGeneric()
	bus.Publish(types.Event{Type: types.EventSignalCreated, Symbol: "BTCUSDT"})
	// 不应 panic
}

// TestGeneric_PanicRecovery 验证一个订阅者 panic 不影响其他订阅者。
func TestGeneric_PanicRecovery(t *testing.T) {
	bus := NewGeneric()
	var count atomic.Int32

	bus.Subscribe(types.EventSignalCreated, func(evt types.Event) {
		panic("预期内的 panic")
	})
	bus.Subscribe(types.EventSignalCreated, func(evt types.Event) {
		count.Add(1)
	})

	bus.Publish(types.Event{Type: types.EventSignalCreated, Symbol: "BTCUSDT"})

	time.Sleep(200 * time.Millisecond)

	if count.Load() != 1 {
		t.Errorf("期望正常订阅者仍被调用1次，实际 %d", count.Load())
	}
}

// TestGeneric_SubscriberCount 验证订阅者计数正确。
func TestGeneric_SubscriberCount(t *testing.T) {
	bus := NewGeneric()

	if c := bus.SubscriberCount(types.EventSignalCreated); c != 0 {
		t.Errorf("期望初始0个订阅者，实际 %d", c)
	}

	id1 := bus.Subscribe(types.EventSignalCreated, func(evt types.Event) {})
	id2 := bus.Subscribe(types.EventSignalCreated, func(evt types.Event) {})

	if c := bus.SubscriberCount(types.EventSignalCreated); c != 2 {
		t.Errorf("期望2个订阅者，实际 %d", c)
	}

	bus.Unsubscribe(types.EventSignalCreated, id1)
	if c := bus.SubscriberCount(types.EventSignalCreated); c != 1 {
		t.Errorf("取消一个后期望1个订阅者，实际 %d", c)
	}

	bus.Unsubscribe(types.EventSignalCreated, id2)
	if c := bus.SubscriberCount(types.EventSignalCreated); c != 0 {
		t.Errorf("全部取消后期望0个订阅者，实际 %d", c)
	}
}

// TestGeneric_MultipleEventTypes 验证不同类型的事件分发给不同订阅者。
func TestGeneric_MultipleEventTypes(t *testing.T) {
	bus := NewGeneric()
	var signalCount, resonanceCount atomic.Int32

	bus.Subscribe(types.EventSignalCreated, func(evt types.Event) {
		signalCount.Add(1)
	})
	bus.Subscribe(types.EventResonanceTriggered, func(evt types.Event) {
		resonanceCount.Add(1)
	})

	bus.Publish(types.Event{Type: types.EventSignalCreated, Symbol: "BTCUSDT"})
	bus.Publish(types.Event{Type: types.EventResonanceTriggered, Symbol: "BTCUSDT"})

	time.Sleep(200 * time.Millisecond)

	if signalCount.Load() != 1 {
		t.Errorf("信号订阅者期望1次调用，实际 %d", signalCount.Load())
	}
	if resonanceCount.Load() != 1 {
		t.Errorf("共振订阅者期望1次调用，实际 %d", resonanceCount.Load())
	}
}

// TestGeneric_UnsubscribeNonExistent 验证取消不存在的订阅不 panic。
func TestGeneric_UnsubscribeNonExistent(t *testing.T) {
	bus := NewGeneric()
	// 对一个从未注册过的类型取消订阅
	bus.Unsubscribe(types.EventSignalCreated, 999)
	bus.Unsubscribe(types.EventResonanceTriggered, 0)
	// 不应 panic
}

// TestGeneric_SequentialIDs 验证订阅 ID 单调递增。
func TestGeneric_SequentialIDs(t *testing.T) {
	bus := NewGeneric()

	id1 := bus.Subscribe(types.EventSignalCreated, func(evt types.Event) {})
	id2 := bus.Subscribe(types.EventSignalCreated, func(evt types.Event) {})

	if id2 <= id1 {
		t.Errorf("期望订阅 ID 单调递增: id1=%d, id2=%d", id1, id2)
	}
}
