// Package eventbus 提供基于channel的轻量级事件广播系统。
package eventbus

import (
	"log/slog"
	"sync"
	"sync/atomic"

	"trade/internal/log"
	"trade/internal/types"
)

// EventHandler 是处理KlineEvent的回调函数。
// 实现不应长时间阻塞；总线不保证同一事件类型下不同订阅者的顺序。
type EventHandler func(types.KlineEvent)

// Bus 是一个支持发布/订阅语义的轻量级事件总线。
// 协程安全。
type Bus struct {
	mu          sync.RWMutex
	subscribers map[types.KlineEventType]map[int64]EventHandler
	nextID      atomic.Int64
	logger      *slog.Logger
}

// New 创建一个新的事件总线。
func New() *Bus {
	return &Bus{
		subscribers: make(map[types.KlineEventType]map[int64]EventHandler),
		logger:      log.Component("eventbus"),
	}
}

// Subscribe 注册一个指定事件类型的处理器。
// 返回可传递给Unsubscribe的订阅ID。
func (b *Bus) Subscribe(eventType types.KlineEventType, handler EventHandler) int64 {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := b.nextID.Add(1)

	if _, ok := b.subscribers[eventType]; !ok {
		b.subscribers[eventType] = make(map[int64]EventHandler)
	}
	b.subscribers[eventType][id] = handler

	b.logger.Debug("注册订阅者", "event_type", eventType, "sub_id", id)
	return id
}

// Unsubscribe 移除一个之前注册的订阅。
func (b *Bus) Unsubscribe(eventType types.KlineEventType, id int64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if handlers, ok := b.subscribers[eventType]; ok {
		delete(handlers, id)
		if len(handlers) == 0 {
			delete(b.subscribers, eventType)
		}
	}
	b.logger.Debug("取消订阅者", "event_type", eventType, "sub_id", id)
}

// Publish 将事件顺序分发给所有匹配的订阅者。
// 所有处理器在同一协程中同步调用，保证处理顺序。
// 调用者发布后不应再修改事件。
func (b *Bus) Publish(event types.KlineEvent) {
	b.mu.RLock()
	handlers, ok := b.subscribers[event.Type]
	b.mu.RUnlock()

	if !ok || len(handlers) == 0 {
		return
	}

	for id, handler := range handlers {
		recoverFunc(event, handler, id)
	}

	b.logger.Debug("事件已发布",
		"event_type", event.Type,
		"symbol", event.Kline.Symbol,
		"subscriber_count", len(handlers),
	)
}

// recoverFunc 执行处理器并捕获panic，防止单个订阅者导致整个系统崩溃。
func recoverFunc(event types.KlineEvent, handler EventHandler, id int64) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("订阅者panic",
				"sub_id", id,
				"event_type", event.Type,
				"panic", r,
			)
		}
	}()
	handler(event)
}

// SubscriberCount 返回指定事件类型的处理器数量。
func (b *Bus) SubscriberCount(eventType types.KlineEventType) int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if handlers, ok := b.subscribers[eventType]; ok {
		return len(handlers)
	}
	return 0
}
