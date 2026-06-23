// Package m4_levels 递归级别构建器（M4）。
//
// 职责（PRD §10.1/§10.3）：
//   - 双轨制构建多级别（实时轨 + 确认轨）
//   - L(N-1) 走势类型 → LN 笔 递归
//   - 级别漂移检测 → 触发 recast
package levels

import (
	"log/slog"
	"sync"

	"trade/internal/eventbus"
	"trade/internal/log"
	"trade/internal/types"
)

// LevelBuilder 递归级别构建器。
type LevelBuilder struct {
	bus    *eventbus.GenericBus
	logger *slog.Logger

	mu sync.RWMutex

	// 每个级别的当前结构（双轨状态）
	states map[types.Level]*types.DualTrackState
}

// New 创建级别构建器。
func New(bus *eventbus.GenericBus) *LevelBuilder {
	return &LevelBuilder{
		bus:    bus,
		logger: log.Component("m4.levels"),
		states: make(map[types.Level]*types.DualTrackState),
	}
}

// OnLowerLevelComplete 下级走势类型完成时调用，尝试构建上一级笔。
// level: 完成的走势类型所在级别
// TrendPattern: 完成的走势类型
func (b *LevelBuilder) OnLowerLevelComplete(level types.Level, trendPattern *types.TrendPattern) {
	// TODO: 实际实现
	// 1. 将 L(N-1) 的完成走势类型作为 LN 的一根笔
	// 2. 更新 LN 的实时轨/确认轨
	// 3. 检测级别漂移
	// 4. 若漂移 → 发 EventLevelRecast
}

// GetState 返回指定级别的双轨状态。
func (b *LevelBuilder) GetState(level types.Level) *types.DualTrackState {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.states[level]
}
