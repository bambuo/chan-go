// Package engine 提供缠论信号分析引擎的核心组件：事件总线、数据源、级别运行器等。
package engine

import (
	"sync"
)

// EventType 是事件类型。
type EventType int

const (
	EventKlineReceived EventType = iota // 原始 K 线到达
	EventKlineClosed                    // K 线收盘
	EventError                          // 错误
)

// String 返回事件类型的可读名称。
func (et EventType) String() string {
	switch et {
	case EventKlineReceived:
		return "KlineReceived"
	case EventKlineClosed:
		return "KlineClosed"
	case EventError:
		return "ErrorEvent"
	default:
		return "Unknown"
	}
}

// Event 表示一个领域事件。
type Event struct {
	Type EventType
	Data interface{}
}

// GenericBus 是通用事件总线，基于 EventType 的发布/订阅模式。
// 线程安全：订阅/发布均受读写锁保护。
type GenericBus struct {
	mu       sync.RWMutex
	handlers map[EventType][]func(Event)
}

// NewGenericBus 创建一个新的事件总线。
func NewGenericBus() *GenericBus {
	return &GenericBus{
		handlers: make(map[EventType][]func(Event)),
	}
}

// Subscribe 订阅指定类型的事件。
func (b *GenericBus) Subscribe(t EventType, handler func(Event)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[t] = append(b.handlers[t], handler)
}

// Publish 发布事件，同步调用所有已订阅的处理器。
// 用快照方式避免发布期间加锁导致死锁。
func (b *GenericBus) Publish(event Event) {
	b.mu.RLock()
	list := b.handlers[event.Type]
	// 快照复制
	snapshot := make([]func(Event), len(list))
	copy(snapshot, list)
	b.mu.RUnlock()

	for _, handler := range snapshot {
		handler(event)
	}
}

// 确保 chanlun 包中的事件类型与 engine 同步。
// engine 包使用自己的 EventType 而非从 chanlun 导入，
// 以避免包循环依赖。
