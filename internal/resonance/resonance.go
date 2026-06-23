// Package m6_resonance 共振引擎（M6）。
//
// 职责（PRD §9）：
//   - G-2 区间套（最高优先级）
//   - G-1 跨层共振
//   - A3 方向过滤
//   - 共振等待窗口管理
package resonance

import (
	"log/slog"
	"sync"
	"time"

	"trade/internal/eventbus"
	"trade/internal/log"
	"trade/internal/types"
)

// ResonanceEngine 共振引擎。
type ResonanceEngine struct {
	bus    *eventbus.GenericBus
	logger *slog.Logger

	mu sync.RWMutex

	// 共振等待窗口（PRD §9.3 R2）
	pendingWindows map[string]*resonanceWindow // signalId → 等待窗口
}

type resonanceWindow struct {
	Signal    *types.Signal
	StartTS   int64
	TimeoutTS int64 // = StartTS + 大级别 × 3 根 K 线
	Level     types.Level
}

// New 创建共振引擎。
func New(bus *eventbus.GenericBus) *ResonanceEngine {
	return &ResonanceEngine{
		bus:            bus,
		logger:         log.Component("m6.resonance"),
		pendingWindows: make(map[string]*resonanceWindow),
	}
}

// OnSignal 信号引擎产出新信号时调用。
func (e *ResonanceEngine) OnSignal(signal *types.Signal) {
	// TODO: 实际实现
	// 1. G-2 区间套：检查该信号是否在大级别背驰段内
	// 2. G-1 跨层：检查是否有其他级别同向信号
	// 3. A3 方向过滤：检查多级别方向一致性
	// 4. 更新 signal.Resonance
	// 5. 若符合共振 → 发 EventResonanceTriggered
	// 6. 若 G-1 等待 → 启动等待窗口
}

// OnTimeout 检查是否有超时的等待窗口（应定期调用）。
func (e *ResonanceEngine) OnTimeout() {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now().UnixMilli()
	for id, w := range e.pendingWindows {
		if now >= w.TimeoutTS {
			// 窗口超时 → 信号以 standalone 发出（PRD §9.3）
			e.logger.Debug("共振等待窗口超时",
				"signalId", id,
				"level", w.Level,
			)
			delete(e.pendingWindows, id)
		}
	}
}
