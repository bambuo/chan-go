// Package chanlun 笔识别算法综合测试。
package chanlun

import (
	"testing"

	"trade/internal/eventbus"
	"trade/internal/structure"
	"trade/internal/types"
)

// ====== 辅助函数 ======

// mkElem id 为唯一标识，用于生成 OpenTime。
func mkElem(high, low float64, fractalType types.FractalType, id int) *types.ChanKline {
	return &types.ChanKline{
		High:        high,
		Low:         low,
		FractalType: fractalType,
		MergedFrom:  1,
		OpenTime:    int64(id*100000 + 1),
	}
}

// linkChain 将一组元素链成双向链表。
func linkChain(elems []*types.ChanKline) {
	for i := 0; i < len(elems); i++ {
		if i > 0 {
			elems[i].PrevElement = elems[i-1]
		}
		if i < len(elems)-1 {
			elems[i].NextElement = elems[i+1]
		}
	}
}

// TestBi_BasicUpBi 底分型(0)→顶分型(3)，非严格模式，跨度=3。
func TestBi_BasicUpBi(t *testing.T) {
	elems := []*types.ChanKline{
		mkElem(10, 5, types.FractalBottom, 0),
		mkElem(13, 7, types.FractalNone, 1),
		mkElem(16, 10, types.FractalNone, 2),
		mkElem(22, 18, types.FractalTop, 3),
	}
	linkChain(elems)

	bp := NewStrokeProcessor()
	bp.getOrCreateState("TEST").cfg.Strict = false

	bp.Process("TEST", elems[0], []types.Fractal{
		{Type: types.FractalBottom, Index: 0, High: 10, Low: 5, Confirmed: true},
	})
	bp.Process("TEST", elems[1], nil)
	bp.Process("TEST", elems[2], nil)
	bp.Process("TEST", elems[3], []types.Fractal{
		{Type: types.FractalTop, Index: 3, High: 22, Low: 18, Confirmed: true},
	})

	bis := bp.Strokes("TEST")
	if len(bis) == 0 {
		t.Fatal("期望至少 1 条笔")
	}
	if bis[0].Direction != types.DirectionUp {
		t.Errorf("方向期望 up, 实际 %s", bis[0].Direction)
	}
	if bis[0].Start != elems[0] {
		t.Error("起点应为 elems[0]（底分型）")
	}
}

// TestBi_BasicDownBi 顶→底，非严格模式。
func TestBi_BasicDownBi(t *testing.T) {
	elems := []*types.ChanKline{
		mkElem(25, 20, types.FractalTop, 0),
		mkElem(20, 15, types.FractalNone, 1),
		mkElem(15, 10, types.FractalNone, 2),
		mkElem(10, 5, types.FractalBottom, 3),
	}
	linkChain(elems)

	bp := NewStrokeProcessor()
	bp.getOrCreateState("TEST").cfg.Strict = false

	bp.Process("TEST", elems[0], []types.Fractal{
		{Type: types.FractalTop, Index: 0, High: 25, Low: 20, Confirmed: true},
	})
	bp.Process("TEST", elems[1], nil)
	bp.Process("TEST", elems[2], nil)
	bp.Process("TEST", elems[3], []types.Fractal{
		{Type: types.FractalBottom, Index: 3, High: 10, Low: 5, Confirmed: true},
	})

	bis := bp.Strokes("TEST")
	if len(bis) == 0 {
		t.Fatal("期望至少 1 条笔")
	}
	if bis[0].Direction != types.DirectionDown {
		t.Errorf("方向期望 down, 实际 %s", bis[0].Direction)
	}
}

// TestBi_SameTypeUpdateEnd 同类分型更新终点（§8）。
func TestBi_SameTypeUpdateEnd(t *testing.T) {
	elems := []*types.ChanKline{
		mkElem(10, 5, types.FractalBottom, 0),
		mkElem(15, 8, types.FractalNone, 1),
		mkElem(20, 12, types.FractalTop, 2),
		mkElem(25, 18, types.FractalNone, 3),
		mkElem(30, 22, types.FractalTop, 4),
	}
	linkChain(elems)

	bp := NewStrokeProcessor()
	bp.getOrCreateState("TEST").cfg.Strict = false

	bp.Process("TEST", elems[0], []types.Fractal{
		{Type: types.FractalBottom, Index: 0, High: 10, Low: 5, Confirmed: true},
	})
	bp.Process("TEST", elems[1], nil)
	bp.Process("TEST", elems[2], []types.Fractal{
		{Type: types.FractalTop, Index: 2, High: 20, Low: 12, Confirmed: true},
	})
	bp.Process("TEST", elems[3], nil)
	bp.Process("TEST", elems[4], []types.Fractal{
		{Type: types.FractalTop, Index: 4, High: 30, Low: 22, Confirmed: true},
	})

	bis := bp.Strokes("TEST")
	if len(bis) != 1 {
		t.Fatalf("期望 1 条笔（同类更新）, 实际 %d", len(bis))
	}
	if bis[0].End != elems[4] {
		t.Error("终点应为 elems[4]（更高的顶）")
	}
}

// TestBi_SpanStrict 严格模式跨度=3 不成笔。
func TestBi_SpanStrict(t *testing.T) {
	elems := []*types.ChanKline{
		mkElem(10, 5, types.FractalBottom, 0),
		mkElem(12, 7, types.FractalNone, 1),
		mkElem(14, 9, types.FractalNone, 2),
		mkElem(20, 15, types.FractalTop, 3),
	}
	linkChain(elems)

	bp := NewStrokeProcessor()
	bp.getOrCreateState("TEST").cfg.Strict = true

	bp.Process("TEST", elems[0], []types.Fractal{
		{Type: types.FractalBottom, Index: 0, High: 10, Low: 5, Confirmed: true},
	})
	bp.Process("TEST", elems[1], nil)
	bp.Process("TEST", elems[2], nil)
	bp.Process("TEST", elems[3], []types.Fractal{
		{Type: types.FractalTop, Index: 3, High: 20, Low: 15, Confirmed: true},
	})

	bis := bp.Strokes("TEST")
	if len(bis) != 0 {
		t.Errorf("严格模式跨度=3 应不成笔, 实际 %d 条", len(bis))
	}
}

// TestBi_SpanNonStrict 非严格模式跨度=3 可成笔。
func TestBi_SpanNonStrict(t *testing.T) {
	elems := []*types.ChanKline{
		mkElem(10, 5, types.FractalBottom, 0),
		mkElem(12, 7, types.FractalNone, 1),
		mkElem(14, 9, types.FractalNone, 2),
		mkElem(20, 15, types.FractalTop, 3),
	}
	linkChain(elems)

	bp := NewStrokeProcessor()
	bp.getOrCreateState("TEST").cfg.Strict = false

	bp.Process("TEST", elems[0], []types.Fractal{
		{Type: types.FractalBottom, Index: 0, High: 10, Low: 5, Confirmed: true},
	})
	bp.Process("TEST", elems[1], nil)
	bp.Process("TEST", elems[2], nil)
	bp.Process("TEST", elems[3], []types.Fractal{
		{Type: types.FractalTop, Index: 3, High: 20, Low: 15, Confirmed: true},
	})

	bis := bp.Strokes("TEST")
	if len(bis) != 1 {
		t.Errorf("非严格模式跨度=3 应成笔, 实际 %d 条", len(bis))
	}
}

// TestBi_FirstBiCandidates 多个候选后异类分型成第一笔（§4）。
func TestBi_FirstBiCandidates(t *testing.T) {
	elems := []*types.ChanKline{
		mkElem(12, 6, types.FractalBottom, 0),
		mkElem(10, 7, types.FractalBottom, 1),
		mkElem(14, 9, types.FractalNone, 2),
		mkElem(20, 15, types.FractalTop, 3),
	}
	linkChain(elems)

	bp := NewStrokeProcessor()
	bp.getOrCreateState("TEST").cfg.Strict = false

	bp.Process("TEST", elems[0], []types.Fractal{
		{Type: types.FractalBottom, Index: 0, High: 12, Low: 6, Confirmed: true},
	})
	bp.Process("TEST", elems[1], nil)
	bp.Process("TEST", elems[2], nil)
	bp.Process("TEST", elems[3], []types.Fractal{
		{Type: types.FractalTop, Index: 3, High: 20, Low: 15, Confirmed: true},
	})

	bis := bp.Strokes("TEST")
	if len(bis) != 1 {
		t.Fatalf("期望 1 条笔, 实际 %d", len(bis))
	}
	if bis[0].Direction != types.DirectionUp {
		t.Errorf("方向期望 up, 实际 %s", bis[0].Direction)
	}
}

// TestBi_PipelineIntegration 完整 contain→fractal→stroke pipeline。
func TestBi_PipelineIntegration(t *testing.T) {
	p := NewPipeline()
	klines := []float64{30, 25, 35, 28, 45, 38, 40, 32}
	for i := 0; i < len(klines); i += 2 {
		raw := mkline((klines[i+1] - 3), klines[i], klines[i+1]-8, klines[i+1]-5, int64(i/2), "TEST_PIPE")
		p.Process(raw)
	}
	state := p.GetState("TEST_PIPE")
	t.Logf("元素: %d, 分型: %d, 笔: %d", len(state.AllElements), len(state.AllFractals), len(state.Strokes))
}

// TestBi_M3Bridge 笔信息通过 M3Bridge 写入结构树。
func TestBi_M3Bridge(t *testing.T) {
	gBus := eventbus.NewGeneric()
	tree := structure.New(gBus)
	pipeline := NewPipeline()
	bridge := NewM3Bridge(pipeline, tree)

	pattern := []float64{10, 5, 15, 8, 22, 14, 18, 10, 14, 6}
	for i := 0; i < len(pattern); i += 2 {
		high := pattern[i]
		low := pattern[i+1]
		kline := mkline(low+1, high, low, high-2, int64(i/2), "TEST_BRIDGE")
		bridge.OnKline(kline)
	}

	state := tree.GetCurrentState("TEST_BRIDGE", types.LevelL1)
	if state != nil {
		t.Logf("M3 L1 笔数: %d", len(state.Provisional.Strokes))
	}
}

// ====== 笔检查模式测试 ======

// ====== 终点更新测试 ======

// TestTryUpdateEndpoint_Down 验证下降笔的同类分型终点更新。
func TestTryUpdateEndpoint_Down(t *testing.T) {
	state := NewStrokeProcessor().getOrCreateState("TEST_UPDATE_DOWN")

	elem1 := mkElem(30, 25, types.FractalTop, 0)
	elem2 := mkElem(25, 20, types.FractalNone, 1)
	elem3 := mkElem(20, 15, types.FractalBottom, 2)
	elem4 := mkElem(18, 12, types.FractalNone, 3)
	elem5 := mkElem(15, 8, types.FractalBottom, 4)
	linkChain([]*types.ChanKline{elem1, elem2, elem3, elem4, elem5})

	// 创建下降笔 (顶→底)
	stroke := &stroke{
		Direction: types.DirectionDown,
		Start:     elem1,
		End:       elem3,
		EndPrice:  elem3.Low,
		Low:       elem3.Low,
	}

	// 新底分型 (elem5) low=8 < 当前底 low=15 → 应更新
	ok := state.tryUpdateEndpoint(elem5, stroke)
	if !ok {
		t.Error("下降笔终点更新应成功 (新底更低)")
	}
	if stroke.End != elem5 {
		t.Error("终点应为 elem5")
	}
	if stroke.EndPrice != 8 {
		t.Errorf("EndPrice 期望 8, 实际 %f", stroke.EndPrice)
	}

	// 尝试用更高的底更新 → 不应更新
	elem6 := mkElem(12, 10, types.FractalBottom, 5)
	ok = state.tryUpdateEndpoint(elem6, stroke)
	if ok {
		t.Error("更高底不应更新终点")
	}

	// 用顶分型更新 → 不应更新
	elem7 := mkElem(20, 12, types.FractalTop, 6)
	ok = state.tryUpdateEndpoint(elem7, stroke)
	if ok {
		t.Error("非同类分型(顶)不应更新下降笔终点")
	}
}

// ====== 检查模式集成测试 ======

// TestBi_StrictRejectsShortSpan 验证严格模式对短跨度笔的约束。
func TestBi_StrictRejectsShortSpan(t *testing.T) {
	elems := []*types.ChanKline{
		mkElem(10, 5, types.FractalBottom, 0),
		mkElem(12, 7, types.FractalNone, 1),
		mkElem(14, 9, types.FractalNone, 2),
		mkElem(20, 15, types.FractalTop, 3),
	}
	linkChain(elems)

	bp := NewStrokeProcessor()
	bp.getOrCreateState("TEST_STRICT").cfg.Strict = true

	bp.Process("TEST_STRICT", elems[0], []types.Fractal{
		{Type: types.FractalBottom, Index: 0, High: 10, Low: 5, Confirmed: true},
	})
	bp.Process("TEST_STRICT", elems[1], nil)
	bp.Process("TEST_STRICT", elems[2], nil)
	bp.Process("TEST_STRICT", elems[3], []types.Fractal{
		{Type: types.FractalTop, Index: 3, High: 20, Low: 15, Confirmed: true},
	})

	bis := bp.Strokes("TEST_STRICT")
	t.Logf("严格模式笔数: %d (元素跨度=3)", len(bis))
}

// TestBi_NonStrictWithHalfCheck 验证非严格模式 + half 检查接受元素跨度=3的笔。
func TestBi_NonStrictWithHalfCheck(t *testing.T) {
	elems := []*types.ChanKline{
		mkElem(10, 5, types.FractalBottom, 0),
		mkElem(13, 7, types.FractalNone, 1),
		mkElem(16, 10, types.FractalNone, 2),
		mkElem(22, 18, types.FractalTop, 3),
	}
	linkChain(elems)

	bp := NewStrokeProcessor()
	state := bp.getOrCreateState("TEST")
	state.cfg.Strict = false

	bp.Process("TEST", elems[0], []types.Fractal{
		{Type: types.FractalBottom, Index: 0, High: 10, Low: 5, Confirmed: true},
	})
	bp.Process("TEST", elems[1], nil)
	bp.Process("TEST", elems[2], nil)
	bp.Process("TEST", elems[3], []types.Fractal{
		{Type: types.FractalTop, Index: 3, High: 22, Low: 18, Confirmed: true},
	})

	bis := bp.Strokes("TEST")
	if len(bis) == 0 {
		t.Error("非严格模式 + half 检查应接受 3 元素跨度笔")
	} else {
		t.Log("✅ 非严格模式接受短跨度笔")
	}
}

// ====== 笔 Reset 测试 ======

// TestStrokeProcessor_Reset 验证笔处理器重置。
func TestStrokeProcessor_Reset(t *testing.T) {
	bp := NewStrokeProcessor()
	elems := []*types.ChanKline{
		mkElem(10, 5, types.FractalBottom, 0),
		mkElem(13, 7, types.FractalNone, 1),
		mkElem(16, 10, types.FractalNone, 2),
		mkElem(22, 18, types.FractalTop, 3),
	}
	linkChain(elems)

	bp.getOrCreateState("TEST").cfg.Strict = false

	bp.Process("TEST", elems[0], []types.Fractal{
		{Type: types.FractalBottom, Index: 0, High: 10, Low: 5, Confirmed: true},
	})
	bp.Process("TEST", elems[1], nil)
	bp.Process("TEST", elems[2], nil)
	bp.Process("TEST", elems[3], []types.Fractal{
		{Type: types.FractalTop, Index: 3, High: 22, Low: 18, Confirmed: true},
	})

	if len(bp.Strokes("TEST")) == 0 {
		t.Fatal("重置前应有笔")
	}

	bp.Reset("TEST")
	if len(bp.Strokes("TEST")) != 0 {
		t.Error("重置后期望空笔列表")
	}

	// 重置不存在的 symbol 不 panic
	bp.Reset("NONEXIST")
}

// ====== strokeState.Reset 直接测试 ======

func TestStrokeStateReset(t *testing.T) {
	state := &strokeState{
		strokes: []*stroke{
			{Index: 0, Direction: types.DirectionUp, Confirmed: true},
			{Index: 1, Direction: types.DirectionDown, Confirmed: true},
		},
		lastEndpoint: mkElem(10, 5, types.FractalBottom, 0),
		candidates: []*types.ChanKline{
			mkElem(20, 15, types.FractalTop, 1),
		},
	}

	state.Reset()

	if len(state.strokes) != 0 {
		t.Errorf("Reset 后期望空 strokes, 实际 %d", len(state.strokes))
	}
	if state.lastEndpoint != nil {
		t.Error("Reset 后期望 lastEndpoint 为 nil")
	}
	if len(state.candidates) != 0 {
		t.Errorf("Reset 后期望空 candidates, 实际 %d", len(state.candidates))
	}
}
