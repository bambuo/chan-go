// Package eventbus 提供通用事件广播系统。
//
// GenericBus 支持 Event 类型的发布/订阅，用于 M1~M10 模块间通信。
// 与现有 Bus（仅 KlineEvent）并存，不冲突。
package eventbus

import (
	"log/slog"
	"sync"
	"sync/atomic"

	"trade/internal/log"
	"trade/internal/types"
)

// GenericHandler 是通用事件处理回调。
type GenericHandler func(types.Event)

// GenericBus 支持多事件类型的通用事件总线。
// 协程安全。
type GenericBus struct {
	mu          sync.RWMutex
	subscribers map[types.EventType]map[int64]GenericHandler
	nextID      atomic.Int64
	logger      *slog.Logger
}

// NewGeneric 创建新的通用事件总线。
func NewGeneric() *GenericBus {
	return &GenericBus{
		subscribers: make(map[types.EventType]map[int64]GenericHandler),
		logger:      log.Component("eventbus.generic"),
	}
}

// Subscribe 注册一个指定事件类型的处理器。
// 返回可传递给 Unsubscribe 的订阅 ID。
func (b *GenericBus) Subscribe(eventType types.EventType, handler GenericHandler) int64 {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := b.nextID.Add(1)
	if _, ok := b.subscribers[eventType]; !ok {
		b.subscribers[eventType] = make(map[int64]GenericHandler)
	}
	b.subscribers[eventType][id] = handler

	b.logger.Debug("注册通用订阅者", "eventType", eventType, "subId", id)
	return id
}

// Unsubscribe 移除一个之前注册的订阅。
func (b *GenericBus) Unsubscribe(eventType types.EventType, id int64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if handlers, ok := b.subscribers[eventType]; ok {
		delete(handlers, id)
		if len(handlers) == 0 {
			delete(b.subscribers, eventType)
		}
	}
	b.logger.Debug("取消通用订阅者", "eventType", eventType, "subId", id)
}

// Publish 将事件顺序分发给所有匹配的订阅者。
// 所有处理器在同一协程中同步调用，保证处理顺序。
func (b *GenericBus) Publish(event types.Event) {
	b.mu.RLock()
	handlers, ok := b.subscribers[event.Type]
	b.mu.RUnlock()

	if !ok || len(handlers) == 0 {
		b.logger.Info("通用事件无订阅者",
			"eventType", event.Type,
			"symbol", event.Symbol,
		)
		return
	}

	for id, handler := range handlers {
		recoverGeneric(event, handler, id)
	}

	b.logger.Info("通用事件已发布",
		"eventType", event.Type,
		"symbol", event.Symbol,
		"subscriberCount", len(handlers),
	)
}

// recoverGeneric 执行处理器并捕获 panic。
func recoverGeneric(event types.Event, handler GenericHandler, id int64) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("通用订阅者 panic",
				"subId", id,
				"eventType", event.Type,
				"panic", r,
			)
		}
	}()
	handler(event)
}

// SubscriberCount 返回指定事件类型的处理器数量。
func (b *GenericBus) SubscriberCount(eventType types.EventType) int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if handlers, ok := b.subscribers[eventType]; ok {
		return len(handlers)
	}
	return 0
}
