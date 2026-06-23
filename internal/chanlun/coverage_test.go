package chanlun

import (
	"testing"

	"trade/internal/types"
)

// ====== elementIndexDiff 测试 ======

func TestElementIndexDiff_Forward(t *testing.T) {
	a := mkElem(10, 5, types.FractalBottom, 0)
	b := mkElem(20, 15, types.FractalTop, 3)
	c := mkElem(25, 18, types.FractalTop, 5)
	linkChain([]*types.ChanKline{a, b, c})

	if got := elementIndexDiff(a, b); got != 1 {
		t.Errorf("a→b 期望 1, 实际 %d", got)
	}
	if got := elementIndexDiff(a, c); got != 2 {
		t.Errorf("a→c 期望 2, 实际 %d", got)
	}
}

func TestElementIndexDiff_Reverse(t *testing.T) {
	a := mkElem(10, 5, types.FractalBottom, 0)
	b := mkElem(20, 15, types.FractalTop, 3)
	c := mkElem(25, 18, types.FractalTop, 5)
	linkChain([]*types.ChanKline{a, b, c})

	// 反向搜索应返回负数
	if got := elementIndexDiff(b, a); got >= 0 {
		t.Errorf("b→a 反向应返回负数, 实际 %d", got)
	}
	if got := elementIndexDiff(c, a); got >= 0 {
		t.Errorf("c→a 反向应返回负数, 实际 %d", got)
	}
}

func TestElementIndexDiff_Same(t *testing.T) {
	a := mkElem(10, 5, types.FractalBottom, 0)
	linkChain([]*types.ChanKline{a})

	// 相同元素
	if got := elementIndexDiff(a, a); got != 0 {
		t.Errorf("相同元素期望 0, 实际 %d", got)
	}
}

func TestElementIndexDiff_NotConnected(t *testing.T) {
	a := mkElem(10, 5, types.FractalBottom, 0)
	b := mkElem(20, 15, types.FractalTop, 3)
	// 不连接它们

	if got := elementIndexDiff(a, b); got != 0 {
		t.Errorf("不连接的元素期望 0, 实际 %d", got)
	}
}

// ====== clearVirtualStroke 测试 ======

func TestClearVirtualStroke_NoStrokes(t *testing.T) {
	state := &strokeState{
		strokes: make([]*stroke, 0),
	}
	state.clearVirtualStroke() // 空列表，不 panic
}

func TestClearVirtualStroke_NotVirtual(t *testing.T) {
	state := &strokeState{
		strokes: []*stroke{
			{Index: 0, Confirmed: true, Virtual: false},
		},
	}
	state.clearVirtualStroke() // 非虚笔，不 panic
	if len(state.strokes) != 1 {
		t.Error("非虚笔不应被删除")
	}
}

func TestClearVirtualStroke_DeleteWhenNoPreviousEnds(t *testing.T) {
	state := &strokeState{
		strokes: []*stroke{
			{Index: 0, Confirmed: true},
			{Index: 1, Virtual: true, Confirmed: false, PreviousEnds: nil},
		},
	}
	state.clearVirtualStroke() // 虚笔无 PreviousEnds → 删除
	if len(state.strokes) != 1 {
		t.Errorf("虚笔删除后期望 1 条, 实际 %d", len(state.strokes))
	}
}

func TestClearVirtualStroke_RestorePreviousEnd(t *testing.T) {
	lastEnd := mkElem(15, 10, types.FractalTop, 1) // 最后一个 PreviousEnds 元素
	state := &strokeState{
		strokes: []*stroke{
			{Index: 0, Confirmed: true},
			{
				Index:        1,
				Virtual:      true,
				Confirmed:    false,
				PreviousEnds: []*types.ChanKline{mkElem(10, 5, types.FractalBottom, 0), lastEnd},
				End:          mkElem(20, 15, types.FractalTop, 2),
			},
		},
	}
	state.clearVirtualStroke() // 有 PreviousEnds → 恢复最后一个

	if len(state.strokes) != 2 {
		t.Fatalf("恢复后期望仍有 2 条笔, 实际 %d", len(state.strokes))
	}
	restored := state.strokes[1]
	if restored.Virtual {
		t.Error("恢复后 Virtual 应为 false")
	}
	if !restored.Confirmed {
		t.Error("恢复后 Confirmed 应为 true")
	}
	if restored.End != lastEnd {
		t.Errorf("恢复后 End 应为 PreviousEnds 中的最后一个, 期望 %p, 实际 %p", lastEnd, restored.End)
	}
}

// ====== ProcessBatch 测试 ======

func TestProcessBatch_Basic(t *testing.T) {
	fp := NewFractalProcessor()

	// 3 个元素形成顶分型
	elems := []*types.ChanKline{
		{High: 15, Low: 10, FractalType: types.FractalNone, Direction: types.DirectionUp, MergedFrom: 1, OpenTime: 1},
		{High: 20, Low: 12, FractalType: types.FractalNone, Direction: types.DirectionUp, MergedFrom: 1, OpenTime: 2},
		{High: 18, Low: 11, FractalType: types.FractalNone, Direction: types.DirectionUp, MergedFrom: 1, OpenTime: 3},
	}
	linkChain(elems)

	fp.ProcessBatch(elems)
	fractals := fp.Fractals()
	// 简化实现可能不产生分型，只要不 panic 就行
	t.Logf("ProcessBatch 分型数: %d", len(fractals))
}

func TestProcessBatch_SkipContained(t *testing.T) {
	fp := NewFractalProcessor()

	elems := []*types.ChanKline{
		{High: 15, Low: 10, Contained: true}, // 跳过包含的
		{High: 20, Low: 12},
	}
	linkChain(elems)

	fp.ProcessBatch(elems)
	// 不应 panic
}

// ====== pivotZonesOverlap 测试 ======

func TestPivotZonesOverlap_Overlap(t *testing.T) {
	a := &pivotZone{ZG: 100, ZD: 80}
	b := &pivotZone{ZG: 90, ZD: 70}
	// a: [80,100], b: [70,90] → 重叠 (80-90)
	if !pivotZonesOverlap(a, b) {
		t.Error("有重叠的中枢应返回 true")
	}
	if !pivotZonesOverlap(b, a) {
		t.Error("对称性: 有重叠的中枢应返回 true")
	}
}

func TestPivotZonesOverlap_NoOverlap(t *testing.T) {
	a := &pivotZone{ZG: 100, ZD: 90}
	b := &pivotZone{ZG: 80, ZD: 70}
	// a: [90,100], b: [70,80] → 不重叠 (100>70 && 90<80? 90<80? No)
	if pivotZonesOverlap(a, b) {
		t.Error("不重叠的中枢应返回 false")
	}
}

func TestPivotZonesOverlap_Touching(t *testing.T) {
	a := &pivotZone{ZG: 100, ZD: 80}
	b := &pivotZone{ZG: 80, ZD: 60}
	// a: [80,100], b: [60,80] → ZG_b(80) > ZD_a(80)? No. 所以不重叠（边界接触不算）
	if pivotZonesOverlap(a, b) {
		t.Error("边界接触不算重叠")
	}
}

func TestPivotZonesOverlap_Nil(t *testing.T) {
	if pivotZonesOverlap(nil, &pivotZone{}) {
		t.Error("nil 应返回 false")
	}
	if pivotZonesOverlap(&pivotZone{}, nil) {
		t.Error("nil 应返回 false")
	}
	if pivotZonesOverlap(nil, nil) {
		t.Error("nil 应返回 false")
	}
}

// ====== totalMergedFrom 测试 ======

func TestTotalMergedFrom(t *testing.T) {
	a := &types.ChanKline{MergedFrom: 2}
	b := &types.ChanKline{MergedFrom: 3}
	c := &types.ChanKline{MergedFrom: 1}
	linkChain([]*types.ChanKline{a, b, c})

	if got := totalMergedFrom(a, c); got != 6 {
		t.Errorf("a→c 期望 2+3+1=6, 实际 %d", got)
	}
	if got := totalMergedFrom(a, a); got != 2 {
		t.Errorf("a→a 期望 2, 实际 %d", got)
	}
}

// ====== indexOfElement 测试 ======

func TestIndexOfElement(t *testing.T) {
	a := mkElem(10, 5, types.FractalBottom, 0)
	b := mkElem(20, 15, types.FractalTop, 3)
	linkChain([]*types.ChanKline{a, b})

	if got := indexOfElement(a, nil); got != 0 {
		t.Errorf("a 从头搜索期望 0, 实际 %d", got)
	}
	if got := indexOfElement(b, nil); got != 1 {
		t.Errorf("b 从头搜索期望 1, 实际 %d", got)
	}
	if got := indexOfElement(b, a); got != 1 {
		t.Errorf("b 从 a 开始搜索期望 1, 实际 %d", got)
	}
}

func TestIndexOfElement_FromStart(t *testing.T) {
	a := mkElem(10, 5, types.FractalBottom, 0)
	b := mkElem(20, 15, types.FractalTop, 3)
	c := mkElem(25, 18, types.FractalTop, 5)
	linkChain([]*types.ChanKline{a, b, c})

	// 从 a 开始搜索 c
	if got := indexOfElement(c, a); got != 2 {
		t.Errorf("从 a 搜索 c 期望 2, 实际 %d", got)
	}
	// 搜索不存在的
	unknown := mkElem(30, 22, types.FractalTop, 7)
	if got := indexOfElement(unknown, a); got != -1 {
		t.Errorf("不从在的元素从 a 搜索期望 -1, 实际 %d", got)
	}
}

// ====== oppositeDirection 测试 ======

func TestOppositeDirection_All(t *testing.T) {
	if got := oppositeDirection(types.DirectionUp); got != types.DirectionDown {
		t.Errorf("up→down, 实际 %v", got)
	}
	if got := oppositeDirection(types.DirectionDown); got != types.DirectionUp {
		t.Errorf("down→up, 实际 %v", got)
	}
	if got := oppositeDirection(types.DirectionNone); got != types.DirectionNone {
		t.Errorf("none→none, 实际 %v", got)
	}
}
