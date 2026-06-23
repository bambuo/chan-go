// Package structure 版本化结构树（M3）。
//
// 职责（PRD §10.2）：
//   - 存储缠论结构为不可变版本化树
//   - 每次变更生成新版本
//   - 维护跨版本元素 lineage 映射（PRD §10.5）
//   - 支持查询任意历史时刻的结构快照
package structure

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"trade/internal/eventbus"
	"trade/internal/log"
	"trade/internal/types"
)

// Tree 版本化结构树。
type Tree struct {
	bus    *eventbus.GenericBus
	logger *slog.Logger

	mu sync.RWMutex

	// symbol → level → 当前双轨状态
	currentState map[string]map[types.Level]*types.DualTrackState

	// symbol → level → 版本列表（从旧到新）
	versions map[string]map[types.Level][]types.StructureVersion

	// lineage 映射：lineageId → ElementLineage
	lineages map[string]*types.ElementLineage

	// 所有版本内的结构元素
	elements map[string]*types.StructureElement
}

// New 创建新的结构树。
func New(bus *eventbus.GenericBus) *Tree {
	return &Tree{
		bus:          bus,
		logger:       log.Component("m3.structure"),
		currentState: make(map[string]map[types.Level]*types.DualTrackState),
		versions:     make(map[string]map[types.Level][]types.StructureVersion),
		lineages:     make(map[string]*types.ElementLineage),
		elements:     make(map[string]*types.StructureElement),
	}
}

// Commit 提交一个新版本的结构。
//
// 参数：
//   - symbol: 交易对
//   - level: 变更发生的级别
//   - state: 更新后的双轨状态
//   - diff: 与上一版本的差异（nil 表示无回溯修正）
//
// 返回新版本 ID。
func (t *Tree) Commit(symbol string, level types.Level, state *types.DualTrackState, diff *types.VersionDiff) string {
	t.mu.Lock()
	defer t.mu.Unlock()

	// 初始化 symbol 的级别状态映射
	if _, ok := t.currentState[symbol]; !ok {
		t.currentState[symbol] = make(map[types.Level]*types.DualTrackState)
	}
	if _, ok := t.versions[symbol]; !ok {
		t.versions[symbol] = make(map[types.Level][]types.StructureVersion)
	}

	// 获取上一版本的 ID
	prevID := ""
	if prev, ok := t.currentState[symbol][level]; ok {
		if prev.Provisional.Version.VersionID != "" {
			prevID = prev.Provisional.Version.VersionID
		}
	}

	versionID := fmt.Sprintf("sv_%s_%s_%d", symbol, level.String(), time.Now().UnixMilli())

	ver := types.StructureVersion{
		VersionID:     versionID,
		ValidFromTS:   time.Now().UnixMilli(),
		ParentVersion: prevID,
		Diff:          diff,
		Reason:        "增量处理",
	}

	// 更新状态中的版本引用
	state.Provisional.Version = ver
	t.currentState[symbol][level] = state
	t.versions[symbol][level] = append(t.versions[symbol][level], ver)

	// 标记上一版本被取代
	if prevID != "" {
		versions := t.versions[symbol][level]
		for i := len(versions) - 1; i >= 0; i-- {
			if versions[i].VersionID == prevID {
				s := versionID
				versions[i].SupersededBy = &s
				break
			}
		}
	}

	// 发送版本变更事件
	t.bus.Publish(types.Event{
		Type:   types.EventStructureVersionChanged,
		Symbol: symbol,
		TS:     time.Now().UnixMilli(),
		Payload: types.StructureVersionPayload{
			Symbol:     symbol,
			NewVersion: ver,
			Level:      level,
		},
	})

	t.logger.Debug("结构版本提交",
		"symbol", symbol,
		"level", level,
		"versionId", versionID,
	)
	return versionID
}

// GetCurrentState 返回指定 symbol + level 的当前双轨状态。
func (t *Tree) GetCurrentState(symbol string, level types.Level) *types.DualTrackState {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if sym, ok := t.currentState[symbol]; ok {
		if lvl, ok := sym[level]; ok {
			return lvl
		}
	}
	return nil
}

// RegisterElement 注册一个结构元素并维护其 lineage。
// elementID 是版本内 ID（如 "BTCUSDT_bi_3@v5"），lineageID 是跨版本稳定的 ID。
func (t *Tree) RegisterElement(elementID, lineageID string, elemType string, ts int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// 注册元素
	t.elements[elementID] = &types.StructureElement{
		ID:          elementID,
		ElementType: elemType,
		LineageID:   lineageID,
		ValidFromTS: ts,
	}

	// 维护 lineage 映射
	if existing, ok := t.lineages[lineageID]; ok {
		// 检查是否已记录该 elementID
		for _, eid := range existing.SameAs {
			if eid == elementID {
				return
			}
		}
		existing.SameAs = append(existing.SameAs, elementID)
	} else {
		t.lineages[lineageID] = &types.ElementLineage{
			LineageID: lineageID,
			SameAs:    []string{elementID},
		}
	}
}

// ResolveLineage 解析 lineageId 到最新版本内的元素 ID。
func (t *Tree) ResolveLineage(lineageID string) (string, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	entry, ok := t.lineages[lineageID]
	if !ok || len(entry.SameAs) == 0 {
		return "", false
	}
	return entry.SameAs[len(entry.SameAs)-1], true
}

// GetLineage 返回指定 lineageId 的完整映射。
func (t *Tree) GetLineage(lineageID string) (*types.ElementLineage, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	entry, ok := t.lineages[lineageID]
	if !ok {
		return nil, false
	}
	return entry, true
}

// GetLatestStructure 返回指定 symbol + level 的最新结构快照（构建用）。
func (t *Tree) GetLatestStructure(symbol string, level types.Level) *types.LevelStructure {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if sym, ok := t.currentState[symbol]; ok {
		if lvl, ok := sym[level]; ok {
			return &lvl.Provisional
		}
	}
	return nil
}
