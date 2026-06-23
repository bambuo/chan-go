package structure

import (
	"sync/atomic"
	"testing"
	"time"

	"trade/internal/eventbus"
	"trade/internal/types"
)

// mkState 创建一个简单的双轨状态用于测试。
func mkState(strokes int, level types.Level) *types.DualTrackState {
	ls := types.LevelStructure{
		Level:       level,
		Strokes:     make([]types.Stroke, strokes),
		Provisional: true,
	}
	for i := range ls.Strokes {
		ls.Strokes[i] = types.Stroke{
			StructureElement: types.StructureElement{
				ID:        string(rune('A' + i)),
				LineageID: string(rune('L' + i)),
			},
		}
	}
	return &types.DualTrackState{
		Provisional: ls,
		Confirmed:   ls,
		InSync:      true,
	}
}

// TestNew 验证结构树创建。
func TestNew(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := New(bus)
	if tree == nil {
		t.Fatal("New() 返回 nil")
	}
	if tree.currentState == nil {
		t.Error("currentState 未初始化")
	}
	if tree.versions == nil {
		t.Error("versions 未初始化")
	}
	if tree.lineages == nil {
		t.Error("lineages 未初始化")
	}
	if tree.elements == nil {
		t.Error("elements 未初始化")
	}
}

// TestCommit_Basic 验证基本提交功能。
func TestCommit_Basic(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := New(bus)

	state := mkState(3, types.LevelL1)
	diff := &types.VersionDiff{
		AddedElementIDs: []string{"e1", "e2"},
	}

	versionID := tree.Commit("BTCUSDT", types.LevelL1, state, diff)

	if versionID == "" {
		t.Fatal("Commit 返回空 versionID")
	}

	// 验证可通过 GetCurrentState 查询
	got := tree.GetCurrentState("BTCUSDT", types.LevelL1)
	if got == nil {
		t.Fatal("GetCurrentState 返回 nil")
	}
	if got.Provisional.Level != types.LevelL1 {
		t.Errorf("期望 L1, 实际 %s", got.Provisional.Level)
	}
	if len(got.Provisional.Strokes) != 3 {
		t.Errorf("期望 3 笔, 实际 %d", len(got.Provisional.Strokes))
	}
	if !got.InSync {
		t.Error("期望 InSync=true")
	}
}

// TestCommit_VersionChain 验证版本链连续性。
func TestCommit_VersionChain(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := New(bus)

	// 提交两个版本（确保有微小时间差）
	v1 := tree.Commit("BTCUSDT", types.LevelL1, mkState(2, types.LevelL1), nil)
	time.Sleep(time.Millisecond)
	v2 := tree.Commit("BTCUSDT", types.LevelL1, mkState(4, types.LevelL1), nil)

	if v1 == v2 {
		t.Fatal("两个版本 ID 不应相同")
	}

	// 检查版本列表
	tree.mu.RLock()
	versions := tree.versions["BTCUSDT"][types.LevelL1]
	tree.mu.RUnlock()

	if len(versions) != 2 {
		t.Fatalf("期望 2 个版本, 实际 %d", len(versions))
	}

	if versions[0].VersionID != v1 {
		t.Errorf("版本[0] 期望 %s, 实际 %s", v1, versions[0].VersionID)
	}
	if versions[1].VersionID != v2 {
		t.Errorf("版本[1] 期望 %s, 实际 %s", v2, versions[1].VersionID)
	}

	// 验证 v1 已被 v2 取代
	if versions[0].SupersededBy == nil {
		t.Fatal("v1 的 SupersededBy 不应为 nil")
	}
	if *versions[0].SupersededBy != v2 {
		t.Errorf("v1 的 SupersededBy 期望 %s, 实际 %s", v2, *versions[0].SupersededBy)
	}

	// 验证 v1 的 ParentVersion 为空
	if versions[0].ParentVersion != "" {
		t.Errorf("v1 的 ParentVersion 期望空, 实际 %s", versions[0].ParentVersion)
	}

	// 验证 v2 的 ParentVersion 指向 v1
	if versions[1].ParentVersion != v1 {
		t.Errorf("v2 的 ParentVersion 期望 %s, 实际 %s", v1, versions[1].ParentVersion)
	}

	// 验证版本 ID 格式
	for _, v := range versions {
		if v.VersionID == "" {
			t.Error("版本 ID 不应为空")
		}
		if v.ValidFromTS == 0 {
			t.Error("ValidFromTS 不应为 0")
		}
	}
}

// TestCommit_EventPublished 验证提交时发布版本变更事件。
func TestCommit_EventPublished(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := New(bus)

	var eventCount atomic.Int32
	bus.Subscribe(types.EventStructureVersionChanged, func(evt types.Event) {
		eventCount.Add(1)
		if evt.Symbol != "BTCUSDT" {
			t.Errorf("期望 Symbol=BTCUSDT, 实际 %s", evt.Symbol)
		}
		payload, ok := evt.Payload.(types.StructureVersionPayload)
		if !ok {
			t.Error("Payload 类型不为 StructureVersionPayload")
			return
		}
		if payload.NewVersion.VersionID == "" {
			t.Error("事件中的 VersionID 不应为空")
		}
		if payload.Level != types.LevelL1 {
			t.Errorf("事件中的 Level 期望 L1, 实际 %s", payload.Level)
		}
	})

	tree.Commit("BTCUSDT", types.LevelL1, mkState(3, types.LevelL1), nil)
	tree.Commit("BTCUSDT", types.LevelL1, mkState(4, types.LevelL1), nil)

	if eventCount.Load() != 2 {
		t.Errorf("期望 2 个事件, 实际 %d", eventCount.Load())
	}
}

// TestGetCurrentState_NotFound 验证查询不存在的 symbol 返回 nil。
func TestGetCurrentState_NotFound(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := New(bus)

	state := tree.GetCurrentState("NONEXIST", types.LevelL1)
	if state != nil {
		t.Error("期望不存在的 symbol 返回 nil")
	}
}

// TestRegisterElement_Basic 验证元素注册和 lineage 解析。
func TestRegisterElement_Basic(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := New(bus)

	ts := time.Now().UnixMilli()
	tree.RegisterElement("BTCUSDT_bi_0@v1", "L_BTCUSDT_bi_0", types.ElementTypeStroke, ts)

	// 解析 lineage
	elementID, ok := tree.ResolveLineage("L_BTCUSDT_bi_0")
	if !ok {
		t.Fatal("ResolveLineage 返回 false")
	}
	if elementID != "BTCUSDT_bi_0@v1" {
		t.Errorf("期望 BTCUSDT_bi_0@v1, 实际 %s", elementID)
	}

	// 获取完整 lineage
	lineage, ok := tree.GetLineage("L_BTCUSDT_bi_0")
	if !ok {
		t.Fatal("GetLineage 返回 false")
	}
	if lineage.LineageID != "L_BTCUSDT_bi_0" {
		t.Errorf("期望 LineageID=L_BTCUSDT_bi_0, 实际 %s", lineage.LineageID)
	}
	if len(lineage.SameAs) != 1 {
		t.Errorf("期望 SameAs 长度 1, 实际 %d", len(lineage.SameAs))
	}
}

// TestRegisterElement_MultipleVersions 验证跨版本的 lineage 映射。
func TestRegisterElement_MultipleVersions(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := New(bus)

	ts := time.Now().UnixMilli()

	// 模拟同一个跨版本 ID 在不同版本中的元素
	tree.RegisterElement("BTCUSDT_bi_0@v1", "L_BTCUSDT_bi_0", types.ElementTypeStroke, ts)
	tree.RegisterElement("BTCUSDT_bi_0@v2", "L_BTCUSDT_bi_0", types.ElementTypeStroke, ts+1000)

	// 解析 lineage 应返回最新版本
	elementID, ok := tree.ResolveLineage("L_BTCUSDT_bi_0")
	if !ok {
		t.Fatal("ResolveLineage 返回 false")
	}
	if elementID != "BTCUSDT_bi_0@v2" {
		t.Errorf("期望最新版本 BTCUSDT_bi_0@v2, 实际 %s", elementID)
	}

	// 完整 lineage 应包含两个版本
	lineage, ok := tree.GetLineage("L_BTCUSDT_bi_0")
	if !ok {
		t.Fatal("GetLineage 返回 false")
	}
	if len(lineage.SameAs) != 2 {
		t.Errorf("期望 SameAs 长度 2, 实际 %d", len(lineage.SameAs))
	}
	if lineage.SameAs[0] != "BTCUSDT_bi_0@v1" {
		t.Errorf("SameAs[0] 期望 BTCUSDT_bi_0@v1, 实际 %s", lineage.SameAs[0])
	}
	if lineage.SameAs[1] != "BTCUSDT_bi_0@v2" {
		t.Errorf("SameAs[1] 期望 BTCUSDT_bi_0@v2, 实际 %s", lineage.SameAs[1])
	}
}

// TestRegisterElement_Duplicate 验证重复注册相同 elementID 不会重复添加。
func TestRegisterElement_Duplicate(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := New(bus)

	ts := time.Now().UnixMilli()
	tree.RegisterElement("e1", "L1", types.ElementTypeStroke, ts)
	tree.RegisterElement("e1", "L1", types.ElementTypeStroke, ts)

	lineage, ok := tree.GetLineage("L1")
	if !ok {
		t.Fatal("GetLineage 返回 false")
	}
	if len(lineage.SameAs) != 1 {
		t.Errorf("重复注册应只保留 1 个, 实际 %d", len(lineage.SameAs))
	}
}

// TestResolveLineage_NotFound 验证解析不存在的 lineage 返回 false。
func TestResolveLineage_NotFound(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := New(bus)

	_, ok := tree.ResolveLineage("NONEXIST")
	if ok {
		t.Error("不存在的 lineage 应返回 false")
	}
}

// TestGetLineage_NotFound 验证获取不存在的 lineage 返回 false。
func TestGetLineage_NotFound(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := New(bus)

	_, ok := tree.GetLineage("NONEXIST")
	if ok {
		t.Error("不存在的 lineage 应返回 false")
	}
}

// TestCommit_MultipleSymbols 验证多交易对的版本隔离。
func TestCommit_MultipleSymbols(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := New(bus)

	tree.Commit("BTCUSDT", types.LevelL1, mkState(2, types.LevelL1), nil)
	tree.Commit("ETHUSDT", types.LevelL1, mkState(3, types.LevelL1), nil)

	btc := tree.GetCurrentState("BTCUSDT", types.LevelL1)
	eth := tree.GetCurrentState("ETHUSDT", types.LevelL1)

	if btc == nil || eth == nil {
		t.Fatal("GetCurrentState 返回 nil")
	}
	if len(btc.Provisional.Strokes) != 2 {
		t.Errorf("BTC 期望 2 笔, 实际 %d", len(btc.Provisional.Strokes))
	}
	if len(eth.Provisional.Strokes) != 3 {
		t.Errorf("ETH 期望 3 笔, 实际 %d", len(eth.Provisional.Strokes))
	}

	// 隔离性：查询不同 symbol 不应相互影响
	btc2 := tree.GetCurrentState("BTCUSDT", types.LevelL1)
	if btc2 == nil || len(btc2.Provisional.Strokes) != 2 {
		t.Error("BTC 状态被 ETH 提交影响")
	}
}

// TestCommit_MultipleLevels 验证同一 symbol 的多级别版本隔离。
func TestCommit_MultipleLevels(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := New(bus)

	tree.Commit("BTCUSDT", types.LevelL1, mkState(3, types.LevelL1), nil)
	tree.Commit("BTCUSDT", types.LevelL2, mkState(1, types.LevelL2), nil)

	l1 := tree.GetCurrentState("BTCUSDT", types.LevelL1)
	l2 := tree.GetCurrentState("BTCUSDT", types.LevelL2)

	if l1 == nil {
		t.Error("L1 状态为 nil")
	}
	if l2 == nil {
		t.Error("L2 状态为 nil")
	}
	if l1 != nil && l1.Provisional.Level != types.LevelL1 {
		t.Errorf("L1 状态 Level 期望 L1, 实际 %s", l1.Provisional.Level)
	}
	if l2 != nil && l2.Provisional.Level != types.LevelL2 {
		t.Errorf("L2 状态 Level 期望 L2, 实际 %s", l2.Provisional.Level)
	}
}

// TestGetLatestStructure 验证获取最新结构快照。
func TestGetLatestStructure(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := New(bus)

	state := mkState(3, types.LevelL1)
	tree.Commit("BTCUSDT", types.LevelL1, state, nil)

	ls := tree.GetLatestStructure("BTCUSDT", types.LevelL1)
	if ls == nil {
		t.Fatal("GetLatestStructure 返回 nil")
	}
	if len(ls.Strokes) != 3 {
		t.Errorf("期望 3 笔, 实际 %d", len(ls.Strokes))
	}
}

// TestGetLatestStructure_NotFound 验证不存在的 symbol/level 返回 nil。
func TestGetLatestStructure_NotFound(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := New(bus)

	ls := tree.GetLatestStructure("NONEXIST", types.LevelL1)
	if ls != nil {
		t.Error("不存在时 GetLatestStructure 应返回 nil")
	}
}

// TestCommit_WithDiff 验证带 diff 的提交。
func TestCommit_WithDiff(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := New(bus)

	diff := &types.VersionDiff{
		RemovedElementIDs: []string{"old_bi_0"},
		AddedElementIDs:   []string{"new_bi_0", "new_bi_1"},
		AffectedWindowTS:  []int64{1000, 2000},
	}

	versionID := tree.Commit("BTCUSDT", types.LevelL1, mkState(2, types.LevelL1), diff)

	// 验证 diff 保存在版本记录中
	tree.mu.RLock()
	versions := tree.versions["BTCUSDT"][types.LevelL1]
	tree.mu.RUnlock()

	if len(versions) != 1 {
		t.Fatalf("期望 1 个版本, 实际 %d", len(versions))
	}
	if versions[0].Diff == nil {
		t.Fatal("Diff 不应为 nil")
	}
	if len(versions[0].Diff.RemovedElementIDs) != 1 {
		t.Errorf("期望 1 个被删元素, 实际 %d", len(versions[0].Diff.RemovedElementIDs))
	}
	if len(versions[0].Diff.AddedElementIDs) != 2 {
		t.Errorf("期望 2 个新增元素, 实际 %d", len(versions[0].Diff.AddedElementIDs))
	}
	if versions[0].VersionID != versionID {
		t.Errorf("版本 ID 不匹配")
	}
}
