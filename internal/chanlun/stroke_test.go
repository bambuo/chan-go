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

// TestCheckFractalLoose_Up 验证宽松模式向上笔通过检查。
func TestCheckFractalLoose_Up(t *testing.T) {
	state := &strokeState{}

	// 底→顶: end.High > start.High && end.Low < start.Low
	// 顶分型的高点 > 底分型的高点, 且顶分型的低点 < 底分型的低点
	bottom := mkElem(10, 5, types.FractalBottom, 0)
	top := mkElem(22, 3, types.FractalTop, 3)
	ok := state.checkFractalLoose(bottom, top)
	if !ok {
		t.Error("宽松模式(底→顶): end.High(22)>start.High(10) && end.Low(3)<start.Low(5) 应通过")
	}

	// 顶→底: end.Low > start.High
	top2 := mkElem(25, 20, types.FractalTop, 0)
	bottom2 := mkElem(15, 26, types.FractalBottom, 3) // low=26 > start.High=25
	ok = state.checkFractalLoose(top2, bottom2)
	if !ok {
		t.Error("宽松模式(顶→底): end.Low(26) > start.High(25) 应通过")
	}
}

// TestCheckFractalLoose_Down 验证宽松模式向下笔通过检查。
func TestCheckFractalLoose_Down(t *testing.T) {
	state := &strokeState{}

	// 正确通过场景: end.Low > start.High
	start := mkElem(30, 22, types.FractalTop, 0)
	end := mkElem(25, 32, types.FractalBottom, 3) // low=32 > start.High=30
	ok := state.checkFractalLoose(start, end)
	if !ok {
		t.Error("宽松模式(顶→底): end.Low(32) > start.High(30) 应通过")
	}
}

// TestCheckFractalStrict 验证严格模式检查。
func TestCheckFractalStrict(t *testing.T) {
	state := &strokeState{}

	// 底→顶: 需要检查 end.Prev/Next 和 start.Prev/Next
	elem1 := mkElem(10, 5, types.FractalBottom, 0)
	elem2 := mkElem(13, 7, types.FractalNone, 1)
	elem3 := mkElem(16, 10, types.FractalNone, 2)
	elem4 := mkElem(22, 18, types.FractalTop, 3)
	linkChain([]*types.ChanKline{elem1, elem2, elem3, elem4})

	// start=elem1(底), end=elem4(顶)
	// endLow = min(elem3.low=10, elem4.low=18, elem4.Next?nil) = 10
	// startHigh = max(elem1.high=10, elem1.Prev?nil, elem2.high=13) = 13
	// return start.Low(5) < endLow(10) && end.High(22) > startHigh(13) → true && true → true
	ok := state.checkFractalStrict(elem1, elem4)
	if !ok {
		t.Error("严格模式: 底→顶(5→22) 应通过检查")
	}

	// 顶→底
	elem5 := mkElem(25, 20, types.FractalTop, 0)
	elem6 := mkElem(22, 17, types.FractalNone, 1)
	elem7 := mkElem(18, 13, types.FractalNone, 2)
	elem8 := mkElem(14, 8, types.FractalBottom, 3)
	linkChain([]*types.ChanKline{elem5, elem6, elem7, elem8})

	// start=elem5(顶), end=elem8(底)
	// endHigh = max(elem7.high=18, elem8.high=14, elem8.Next?nil) = 18
	// startLow = min(elem5.low=20, elem5.Prev?nil, elem6.low=17) = 17
	// return start.High(25) > endHigh(18) && end.Low(8) < startLow(17) → true && true → true
	ok = state.checkFractalStrict(elem5, elem8)
	if !ok {
		t.Error("严格模式: 顶→底 应通过检查")
	}
}

// TestCheckFractalFull 验证完全分离模式检查。
func TestCheckFractalFull(t *testing.T) {
	state := &strokeState{}

	// 底→顶: start.Low < end.High
	bottom := mkElem(10, 5, types.FractalBottom, 0)
	top := mkElem(22, 18, types.FractalTop, 3)
	ok := state.checkFractalFull(bottom, top)
	if !ok {
		t.Error("完全分离(底→顶): Low(5) < High(22) 应通过")
	}

	// 不通过: start.High >= end.Low 说明有重叠
	bottom2 := mkElem(10, 5, types.FractalBottom, 0)
	top2 := mkElem(12, 8, types.FractalTop, 3)
	ok = state.checkFractalFull(bottom2, top2)
	if ok {
		t.Error("完全分离(底→顶): Low(5) < High(12) 但有重叠风险,预期严格不通过？")
	}
	// 实际 checkFractalFull for bottom start: return start.High < end.Low
	// start.High(10) < end.Low(8)? No → false
	if ok {
		t.Error("完全分离: start.High(10) < end.Low(8) 为假,应不通过")
	}

	// 顶→底: start.Low > end.High
	top3 := mkElem(25, 20, types.FractalTop, 0)
	bottom3 := mkElem(14, 8, types.FractalBottom, 3)
	ok = state.checkFractalFull(top3, bottom3)
	// start.Low(20) > end.High(14)? Yes → true
	if !ok {
		t.Error("完全分离(顶→底): Low(20) > High(14) 应通过")
	}
}

// TestCheckFractalHalf 验证半模式检查（默认模式）。
func TestCheckFractalHalf(t *testing.T) {
	state := &strokeState{}

	// 底→顶
	elem1 := mkElem(10, 5, types.FractalBottom, 0)
	elem2 := mkElem(13, 7, types.FractalNone, 1)
	elem3 := mkElem(22, 18, types.FractalTop, 3)
	linkChain([]*types.ChanKline{elem1, elem2, elem3})

	// endLow = min(elem2.low=7, elem3.low=18) = 7
	// startHigh = max(elem1.high=10, elem2.high=13) = 13
	// start.Low(5) < endLow(7) && end.High(22) > startHigh(13) → true
	ok := state.checkFractalHalf(elem1, elem3)
	if !ok {
		t.Error("半模式(底→顶) 应通过")
	}
}

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

// ====== 笔终点峰值检查测试 ======

// TestCheckPeakEndPoint_Up 验证上升笔的终点峰值检查。
func TestCheckPeakEndPoint_Up(t *testing.T) {
	state := &strokeState{}

	start := mkElem(10, 5, types.FractalBottom, 0)
	mid := mkElem(18, 12, types.FractalNone, 1)
	end := mkElem(22, 16, types.FractalTop, 2)
	linkChain([]*types.ChanKline{start, mid, end})

	// mid.High(18) <= end.High(22) → 通过
	ok := state.checkPeakEndPoint(start, end)
	if !ok {
		t.Error("笔内无更高价应通过")
	}

	// 中间有更高的价
	start2 := mkElem(10, 5, types.FractalBottom, 0)
	mid2 := mkElem(25, 12, types.FractalNone, 1) // high=25 > end.High=22
	end2 := mkElem(22, 16, types.FractalTop, 2)
	linkChain([]*types.ChanKline{start2, mid2, end2})

	ok = state.checkPeakEndPoint(start2, end2)
	if ok {
		t.Error("笔内有更高价应不通过")
	}
}

// TestCheckPeakEndPoint_Down 验证下降笔的终点峰值检查。
func TestCheckPeakEndPoint_Down(t *testing.T) {
	state := &strokeState{}

	start := mkElem(30, 25, types.FractalTop, 0)
	mid := mkElem(25, 18, types.FractalNone, 1)
	end := mkElem(20, 14, types.FractalBottom, 2)
	linkChain([]*types.ChanKline{start, mid, end})

	// mid.Low(18) >= end.Low(14) → 通过
	ok := state.checkPeakEndPoint(start, end)
	if !ok {
		t.Error("笔内无更低价的下降笔应通过")
	}

	// 中间有更低的价格
	start2 := mkElem(30, 25, types.FractalTop, 0)
	mid2 := mkElem(25, 10, types.FractalNone, 1) // low=10 < end.Low=14
	end2 := mkElem(20, 14, types.FractalBottom, 2)
	linkChain([]*types.ChanKline{start2, mid2, end2})

	ok = state.checkPeakEndPoint(start2, end2)
	if ok {
		t.Error("笔内有更低价的下降笔应不通过")
	}
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

// ====== trySubPeakCorrection 直接测试 ======

// TestTrySubPeakCorrection_UpStroke 验证向上笔的次峰值修正路径。
func TestTrySubPeakCorrection_UpStroke(t *testing.T) {
	// 构造一个场景：两条笔(下+上)，然后一个更高的顶分型到来。
	// 预期：tryUpdateEndpoint 更新上笔终点 → trySubPeakCorrection 尝试修正前一笔

	elemStart0 := mkElem(30, 25, types.FractalTop, 0) // 顶部
	elemMid0 := mkElem(25, 18, types.FractalNone, 1)
	elemEnd0 := mkElem(18, 12, types.FractalBottom, 2) // 底部

	elemStart1 := mkElem(18, 12, types.FractalBottom, 3)
	elemMid1 := mkElem(22, 15, types.FractalNone, 4)
	elemEnd1 := mkElem(28, 20, types.FractalTop, 5) // 顶部

	elemNew := mkElem(35, 24, types.FractalTop, 6) // 更高的新顶

	// 连接元素链
	linkChain([]*types.ChanKline{
		elemStart0, elemMid0, elemEnd0,
		elemStart1, elemMid1, elemEnd1,
		elemNew,
	})

	// 创建两条笔: down0 (25→12) + up1 (12→28)
	downStroke := &stroke{
		Index:      0,
		Direction:  types.DirectionDown,
		Start:      elemStart0,
		End:        elemEnd0,
		StartPrice: elemStart0.High,
		EndPrice:   elemEnd0.Low,
		High:       30,
		Low:        12,
		Confirmed:  true,
	}

	upStroke := &stroke{
		Index:      1,
		Direction:  types.DirectionUp,
		Start:      elemEnd0,
		End:        elemEnd1,
		StartPrice: elemEnd0.Low,
		EndPrice:   elemEnd1.High,
		High:       28,
		Low:        12,
		Confirmed:  true,
	}

	state := &strokeState{
		strokes:            []*stroke{downStroke, upStroke},
		lastEndpoint:       elemEnd1,
		candidates:         make([]*types.ChanKline, 0),
		processedFractalTS: make(map[int64]bool),
		cfg:                types.DefaultStrokeConfig(),
	}
	state.cfg.AllowSubPeak = false // 确保不走捷径

	// 模拟 tryUpdateEndpoint 成功后的流程:
	// elemNew(顶分型) 与 lastEndpoint(顶分型) 类型相同 → 尝试更新
	updated := state.tryUpdateEndpoint(elemNew, upStroke)
	if !updated {
		t.Fatal("tryUpdateEndpoint 应成功（更高顶）")
	}

	if upStroke.End != elemNew {
		t.Error("终点应更新为 elemNew")
	}
	if upStroke.EndPrice != elemNew.High {
		t.Errorf("EndPrice 期望 %.0f, 实际 %.0f", elemNew.High, upStroke.EndPrice)
	}

	// 现在调用 trySubPeakCorrection
	// 条件分析:
	//   1. AllowSubPeak=false ✓
	//   2. len(strokes)=2 ≥ 2 ✓
	//   3. lastStroke=up(DirectionUp), elem=FractalTop → 不过滤
	//   4. lastStroke.Direction=up → elem.High(35) <= prev.Start.High(30)? 35<=30? No → 不过滤
	//   5. lastStroke.Direction=up → lastStroke.End.Low(24) <= prev.Start.Low(25)? 24<=25? Yes → 应过滤!
	//
	// 条件5过滤: lastStroke.End.Low(24) <= prevStroke.Start.Low(25) → return
	// 因为 upStroke 的终点低点(24) 跌破了 downStroke 起点(顶部)的低点(25)

	state.trySubPeakCorrection(elemNew)

	// 因为条件5过滤，笔不应该被修改
	if len(state.strokes) != 2 {
		t.Errorf("条件5过滤: 期望仍有 2 条笔, 实际 %d", len(state.strokes))
	}

	// 创建一个条件5通过的新场景：让 upStroke.End.Low > prev.Start.Low
	elemNew2 := mkElem(33, 26, types.FractalTop, 7) // high=33, low=26

	// 重建 upStroke 使其终点低点更高
	upStroke2 := &stroke{
		Index:      1,
		Direction:  types.DirectionUp,
		Start:      elemEnd0,
		End:        elemNew2,
		StartPrice: elemEnd0.Low,
		EndPrice:   elemNew2.High,
		High:       33,
		Low:        12,
		Confirmed:  true,
	}

	// prev.Start = elemStart0(顶部), Start.Low = 25
	// elemNew2.low = 26 > 25 → 条件5不满足 → 不过滤 ✓

	state2 := &strokeState{
		strokes:            []*stroke{downStroke, upStroke2},
		lastEndpoint:       elemNew2,
		candidates:         make([]*types.ChanKline, 0),
		processedFractalTS: make(map[int64]bool),
		cfg:                types.DefaultStrokeConfig(),
	}
	state2.cfg.AllowSubPeak = false

	// 调用 trySubPeakCorrection
	// 这会尝试移除 upStroke2,然后更新 downStroke 的终点为 elemNew2
	// 但 downStroke 是 DirectionDown, elemNew2 是 FractalTop,类型不匹配 → 更新失败 → 恢复
	state2.trySubPeakCorrection(elemNew2)

	// 应恢复（因为 downStroke 方向是 down, 需要 bottom 类型才能更新）
	if len(state2.strokes) != 2 {
		t.Errorf("修正失败后期望恢复为 2 条笔, 实际 %d", len(state2.strokes))
	}
}

// TestTrySubPeakCorrection_DownStroke 验证下降笔的次峰值修正路径。
func TestTrySubPeakCorrection_DownStroke(t *testing.T) {
	// 构造：两条笔(上+下)，然后一个更低的底到来
	// 预期：更新下笔终点 → 尝试修正前一笔

	elemStart0 := mkElem(10, 5, types.FractalBottom, 0) // 底部 (up stroke start)
	elemMid0 := mkElem(15, 8, types.FractalNone, 1)
	elemEnd0 := mkElem(22, 16, types.FractalTop, 2) // 顶部 (up stroke end)

	elemStart1 := mkElem(22, 16, types.FractalTop, 3) // 顶部 (down stroke start)
	elemMid1 := mkElem(18, 12, types.FractalNone, 4)
	elemEnd1 := mkElem(14, 8, types.FractalBottom, 5) // 底部 (down stroke end)

	elemNew := mkElem(12, 4, types.FractalBottom, 6) // 更低的底

	linkChain([]*types.ChanKline{
		elemStart0, elemMid0, elemEnd0,
		elemStart1, elemMid1, elemEnd1,
		elemNew,
	})

	upStroke := &stroke{
		Index:      0,
		Direction:  types.DirectionUp,
		Start:      elemStart0,
		End:        elemEnd0,
		StartPrice: elemStart0.Low,
		EndPrice:   elemEnd0.High,
		High:       22,
		Low:        5,
		Confirmed:  true,
	}

	downStroke := &stroke{
		Index:      1,
		Direction:  types.DirectionDown,
		Start:      elemEnd0,
		End:        elemEnd1,
		StartPrice: elemEnd0.High,
		EndPrice:   elemEnd1.Low,
		High:       22,
		Low:        8,
		Confirmed:  true,
	}

	state := &strokeState{
		strokes:            []*stroke{upStroke, downStroke},
		lastEndpoint:       elemEnd1,
		candidates:         make([]*types.ChanKline, 0),
		processedFractalTS: make(map[int64]bool),
		cfg:                types.DefaultStrokeConfig(),
	}
	state.cfg.AllowSubPeak = false

	// elemNew 类型与 lastEndpoint 同(都是 bottom) → tryUpdateEndpoint
	updated := state.tryUpdateEndpoint(elemNew, downStroke)
	if !updated {
		t.Fatal("tryUpdateEndpoint 应成功（更低的底）")
	}

	// trySubPeakCorrection 会被调用
	// 条件5 (down): lastStroke.End.High >= prevStroke.Start.High?
	//   lastStroke.End 是 elemNew(底), End.High = elemNew.High = 12
	//   prevStroke.Start = elemStart0(底), Start.High = elemStart0.High = 10
	//   12 >= 10? Yes → 条件5过滤 → return

	state.trySubPeakCorrection(elemNew)

	if len(state.strokes) != 2 {
		t.Errorf("条件5过滤: 期望仍有 2 条笔, 实际 %d", len(state.strokes))
	}

	// 让条件5不通过: 让 elemNew 的 High < prev.Start.High
	elemNew2 := mkElem(9, 4, types.FractalBottom, 7) // 底, high=9, low=4

	downStroke2 := &stroke{
		Index:      1,
		Direction:  types.DirectionDown,
		Start:      elemEnd0,
		End:        elemNew2,
		StartPrice: elemEnd0.High,
		EndPrice:   elemNew2.Low,
		High:       22,
		Low:        4,
		Confirmed:  true,
	}

	state2 := &strokeState{
		strokes:            []*stroke{upStroke, downStroke2},
		lastEndpoint:       elemNew2,
		candidates:         make([]*types.ChanKline, 0),
		processedFractalTS: make(map[int64]bool),
		cfg:                types.DefaultStrokeConfig(),
	}
	state2.cfg.AllowSubPeak = false

	// 这次条件5不通过: elemNew2.High(9) < prev.Start.High(10) → 不过滤
	// 后续: 移除 downStroke2, 尝试用 elemNew2 更新 upStroke(方向 up)
	// upStroke.Direction=up, elemNew2.FractalType=bottom → 更新失败(类型不匹配)
	state2.trySubPeakCorrection(elemNew2)

	// 应恢复
	if len(state2.strokes) != 2 {
		t.Errorf("修正失败后期望恢复为 2 条笔, 实际 %d", len(state2.strokes))
	}
}

// TestTrySubPeakCorrection_AllowSubPeak 验证 AllowSubPeak=true 时直接返回。
func TestTrySubPeakCorrection_AllowSubPeak(t *testing.T) {
	state := &strokeState{
		strokes: []*stroke{{Index: 0, Confirmed: true}},
		cfg:     types.DefaultStrokeConfig(),
	}
	state.cfg.AllowSubPeak = true

	// 不应 panic
	state.trySubPeakCorrection(nil)
}

// TestTrySubPeakCorrection_NotEnoughStrokes 验证笔数不足时直接返回。
func TestTrySubPeakCorrection_NotEnoughStrokes(t *testing.T) {
	state := &strokeState{
		strokes: []*stroke{},
		cfg:     types.DefaultStrokeConfig(),
	}
	state.cfg.AllowSubPeak = false

	// strokes 为空，直接返回，不 panic
	state.trySubPeakCorrection(mkElem(10, 5, types.FractalTop, 0))
}
