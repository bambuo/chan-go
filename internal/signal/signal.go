// Package m5_signal 信号引擎（M5）。
//
// 职责（PRD §8）：
//   - 三类买卖点识别（一买/二买/三买，一卖/二卖/三卖）
//   - candidate/confirmed/invalidated 状态机
//   - confidence 计算（PRD §8.4）
//   - strength 计算（PRD §8.5）
//   - 信号身份判定与去重（PRD §8.6）
//   - 目标位/失效位计算（PRD §8.7）
package signal

import (
	"log/slog"
	"sync"

	"trade/internal/eventbus"
	"trade/internal/log"
	"trade/internal/types"
)

// SignalEngine 信号引擎。
type SignalEngine struct {
	bus    *eventbus.GenericBus
	logger *slog.Logger

	mu sync.RWMutex

	// 所有活跃信号（未 invalidated/superseded）
	activeSignals map[string]*types.Signal // signalId → Signal

	// 按 (symbol, level, type, anchor) 索引，用于去重
	anchorIndex map[string]*types.Signal // anchorKey → Signal
}

// New 创建信号引擎。
func New(bus *eventbus.GenericBus) *SignalEngine {
	return &SignalEngine{
		bus:           bus,
		logger:        log.Component("m5.signal"),
		activeSignals: make(map[string]*types.Signal),
		anchorIndex:   make(map[string]*types.Signal),
	}
}

// OnStructureChange 结构变更时调用（由 M4 触发），识别买卖点。
func (e *SignalEngine) OnStructureChange(level types.Level, state *types.DualTrackState) {
	// TODO: 实际实现
	// 1. 在最新结构中识别三类买卖点候选
	// 2. 计算 anchor → 查重
	// 3. 计算 confidence (§8.4)
	// 4. 计算 strength (§8.5)
	// 5. 计算 targets (§8.7)
	// 6. 状态机流转
	// 7. 发信号事件
}

// GetActiveSignals 返回指定 symbol 的所有活跃信号。
func (e *SignalEngine) GetActiveSignals(symbol string) []*types.Signal {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var result []*types.Signal
	for _, s := range e.activeSignals {
		if s.Symbol == symbol {
			result = append(result, s)
		}
	}
	return result
}

// GetSignal 返回指定 signalId 的信号。
func (e *SignalEngine) GetSignal(signalID string) *types.Signal {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.activeSignals[signalID]
}
