package chanlun

import (
	"testing"

	"trade/internal/types"
)

// ====== finalizeSegment 直接测试 ======

func TestFinalizeSegment(t *testing.T) {
	strokes := []*stroke{
		{Index: 0, Direction: types.DirectionUp, StartPrice: 10, EndPrice: 25, High: 28, Low: 8, Confirmed: true},
		{Index: 1, Direction: types.DirectionUp, StartPrice: 25, EndPrice: 40, High: 42, Low: 22, Confirmed: true},
	}

	seg := &segment{
		index:     0,
		direction: types.DirectionUp,
		strokes:   strokes,
	}

	state := &segState{}
	result := state.finalizeSegment(seg, strokes)

	if result == nil {
		t.Fatal("finalizeSegment 返回 nil")
	}
	if result.index != 0 {
		t.Errorf("index 期望 0, 实际 %d", result.index)
	}
	if result.direction != types.DirectionUp {
		t.Errorf("direction 期望 up, 实际 %v", result.direction)
	}
	if !result.confirmed {
		t.Error("confirmed 应为 true")
	}
	if len(result.strokes) != 2 {
		t.Errorf("strokes 长度期望 2, 实际 %d", len(result.strokes))
	}
	if result.startPrice != 10 {
		t.Errorf("startPrice 期望 10, 实际 %f", result.startPrice)
	}
	if result.endPrice != 40 {
		t.Errorf("endPrice 期望 40, 实际 %f", result.endPrice)
	}
	if result.high != 42 {
		t.Errorf("high 期望 42, 实际 %f", result.high)
	}
	if result.low != 8 {
		t.Errorf("low 期望 8, 实际 %f", result.low)
	}

	// 验证返回的是新对象（不是原指针）
	if result == seg {
		t.Error("应返回新 segment 对象")
	}
}

func TestFinalizeSegment_Empty(t *testing.T) {
	strokes := []*stroke{
		{Index: 0, Direction: types.DirectionUp, StartPrice: 10, EndPrice: 20, Confirmed: true},
	}
	state := &segState{}
	result := state.finalizeSegment(&segment{index: 0, direction: types.DirectionUp}, strokes)
	if result == nil || len(result.strokes) != 1 {
		t.Fatal("单元素段应返回有效结果")
	}
}

// TestFinalizeSegment_Down 验证下降线段完成。
func TestFinalizeSegment_Down(t *testing.T) {
	strokes := []*stroke{
		{Index: 0, Direction: types.DirectionDown, StartPrice: 50, EndPrice: 35, High: 55, Low: 33, Confirmed: true},
		{Index: 1, Direction: types.DirectionDown, StartPrice: 35, EndPrice: 20, High: 38, Low: 18, Confirmed: true},
	}

	state := &segState{}
	result := state.finalizeSegment(&segment{index: 1, direction: types.DirectionDown}, strokes)

	if result.high != 55 {
		t.Errorf("high 期望 55, 实际 %f", result.high)
	}
	if result.low != 18 {
		t.Errorf("low 期望 18, 实际 %f", result.low)
	}
	if result.startPrice != 50 {
		t.Errorf("startPrice 期望 50, 实际 %f", result.startPrice)
	}
	if result.endPrice != 20 {
		t.Errorf("endPrice 期望 20, 实际 %f", result.endPrice)
	}
}

// ====== startNewSegmentFromStrokes 直接测试 ======

func TestStartNewSegmentFromStrokes(t *testing.T) {
	state := &segState{
		segments: make([]*segment, 0),
	}

	strokes := []*stroke{
		{Index: 0, Direction: types.DirectionDown, StartPrice: 40, EndPrice: 25, High: 42, Low: 22, Confirmed: true},
		{Index: 1, Direction: types.DirectionDown, StartPrice: 25, EndPrice: 10, High: 28, Low: 8, Confirmed: true},
	}

	state.startNewSegmentFromStrokes(strokes, types.DirectionDown)

	if len(state.segments) != 1 {
		t.Fatalf("segments 期望 1 个, 实际 %d", len(state.segments))
	}

	seg := state.segments[0]
	if seg.direction != types.DirectionDown {
		t.Errorf("direction 期望 down, 实际 %v", seg.direction)
	}
	if len(seg.strokes) != 2 {
		t.Errorf("strokes 长度期望 2, 实际 %d", len(seg.strokes))
	}
	if seg.startStroke != strokes[0] {
		t.Error("startStroke 应为 strokes[0]")
	}
	if seg.endStroke != strokes[1] {
		t.Error("endStroke 应为 strokes[1]")
	}
	if seg.index != 0 {
		t.Errorf("index 期望 0, 实际 %d", seg.index)
	}
	if seg.confirmed {
		t.Error("新线段 confirmed 应为 false")
	}
}

// ====== 特征序列底分型检测 ======

func TestFeatureSeqFractal_Bottom(t *testing.T) {
	// 直接测试 detectFractal 的底分型分支
	fs := &featureSeq{
		segDir: types.DirectionDown,
		elems: []*fk{
			{high: 35, low: 28}, // 左
			{high: 30, low: 20}, // 中 ← lowest low, 底分型
			{high: 32, low: 25}, // 右
		},
	}

	result := fs.detectFractal()
	if !result.HasBottom {
		t.Error("期望检测到底分型")
	}

	// 无分型：单调递增
	fs2 := &featureSeq{
		elems: []*fk{
			{high: 30, low: 20},
			{high: 35, low: 25},
			{high: 40, low: 30},
		},
	}
	result2 := fs2.detectFractal()
	if result2.HasBottom {
		t.Error("单调递增,不应有底分型")
	}
	if result2.HasTop {
		t.Error("单调递增,不应有顶分型")
	}
}

// ====== getter 方法测试 ======

func TestSegmentGetters(t *testing.T) {
	s := &segment{
		index:      5,
		direction:  types.DirectionUp,
		startPrice: 100,
		endPrice:   200,
		high:       220,
		low:        90,
		confirmed:  true,
	}

	if got := s.Direction(); got != types.DirectionUp {
		t.Errorf("Direction() 期望 up, 实际 %v", got)
	}
	if got := s.StartPrice(); got != 100 {
		t.Errorf("StartPrice() 期望 100, 实际 %f", got)
	}
	if got := s.EndPrice(); got != 200 {
		t.Errorf("EndPrice() 期望 200, 实际 %f", got)
	}
	if got := s.High(); got != 220 {
		t.Errorf("High() 期望 220, 实际 %f", got)
	}
	if got := s.Low(); got != 90 {
		t.Errorf("Low() 期望 90, 实际 %f", got)
	}
	if got := s.Completed(); got != true {
		t.Errorf("Completed() 期望 true, 实际 %v", got)
	}
}

// ====== Pipeline getter 测试 ======

func TestPipelineGetters(t *testing.T) {
	p := NewPipeline()

	// elementID 和 lineageID 的格式检查
	p.mu.Lock()
	state := p.symbols["TEST"]
	_ = state
	p.mu.Unlock()

	// 通过 GetOrCreate 创建
	state2 := p.GetOrCreate("TEST")
	eid := state2.elementID("bi", 3)
	expected := "TEST_bi_3"
	if eid != expected {
		t.Errorf("elementID 期望 %s, 实际 %s", expected, eid)
	}

	lid := state2.lineageID("bi", 3)
	expected = "L_TEST_bi_3"
	if lid != expected {
		t.Errorf("lineageID 期望 %s, 实际 %s", expected, lid)
	}
}

func TestPipelineReset(t *testing.T) {
	p := NewPipeline()

	// 先创建 symbol 状态
	p.GetOrCreate("TEST_RESET")

	// 重置 - 应不会 panic
	p.Reset("TEST_RESET")

	// 重置不存在的 symbol 不应 panic
	p.Reset("NONEXIST")
}

// ====== PivotZone getter 测试 ======

func TestPivotZoneGetters(t *testing.T) {
	pz := &pivotZone{
		index:         3,
		SegmentsCount: 5,
		Completed:     true,
	}

	if got := pz.Index(); got != 3 {
		t.Errorf("Index() 期望 3, 实际 %d", got)
	}
	if got := pz.StrokeCount(); got != 5 {
		t.Errorf("StrokeCount() 期望 5, 实际 %d", got)
	}

	pz2 := &pivotZone{index: 0, SegmentsCount: 0}
	if got := pz2.Index(); got != 0 {
		t.Errorf("Index() 期望 0, 实际 %d", got)
	}
}

func TestPivotZoneReset(t *testing.T) {
	zp := NewPivotZoneProcessor()

	zp.Process("TEST", []*stroke{
		{Index: 0, Direction: types.DirectionUp, StartPrice: 10, EndPrice: 20, High: 22, Low: 8, Confirmed: true},
	})

	zp.Reset("TEST")

	pz := zp.Load("TEST")
	if len(pz) != 0 {
		t.Error("重置后期望空中枢列表")
	}

	// 重置不存在的 symbol 不应 panic
	zp.Reset("NONEXIST")
}

// ====== Bridge setter 测试 ======

func TestBridgeSetters(t *testing.T) {
	p := NewPipeline()
	b := NewM3Bridge(p, nil)

	if b.signalSink != nil {
		t.Error("初始 signalSink 应为 nil")
	}
	if b.debugWriter != nil {
		t.Error("初始 debugWriter 应为 nil")
	}

	// 测试 WithSignalSink 链式调用
	sink := &mockSignalSink{}
	b2 := b.WithSignalSink(sink)
	if b2 != b {
		t.Error("WithSignalSink 应返回自身")
	}
	if b.signalSink != sink {
		t.Error("signalSink 未设置")
	}

	// 测试 WithDebugWriter
	w := &mockDebugWriter{}
	b3 := b.WithDebugWriter(w)
	if b3 != b {
		t.Error("WithDebugWriter 应返回自身")
	}
	if b.debugWriter != w {
		t.Error("debugWriter 未设置")
	}
}

type mockSignalSink struct{}

func (m *mockSignalSink) OnSignalInput(input *SignalInput) {}

type mockDebugWriter struct{}

func (m *mockDebugWriter) Write(output *PipelineOutput) error { return nil }

// ====== divergenceToEvidence 测试 ======

func TestDivergenceToEvidence(t *testing.T) {
		d := &divergence{
			Type:       "topDivergence",
			ZoneIdx:    0,
			EntryStart: 0,
			EntryEnd:   0,
			ExitStart:  1,
			ExitEnd:    2,
			EntryMACD:  1.5,
			ExitMACD:   1.2,
			EntryPrice: 100,
			ExitPrice:  120,
			Ratio:      0.8,
			Confirmed:  true,
		}

	e := divergenceToEvidence(d)
	if e == nil {
		t.Fatal("divergenceToEvidence 返回 nil")
	}
	if e["type"] != "topDivergence" {
		t.Errorf("type 期望 topDivergence, 实际 %v", e["type"])
	}
	if e["ratio"] != 0.8 {
		t.Errorf("ratio 期望 0.8, 实际 %v", e["ratio"])
	}
	if e["confirmed"] != true {
		t.Error("confirmed 应为 true")
	}

	// 底背驰
	d2 := &divergence{
		Type: "bottomDivergence",
	}
	e2 := divergenceToEvidence(d2)
	if e2["type"] != "bottomDivergence" {
		t.Errorf("type 期望 bottomDivergence, 实际 %v", e2["type"])
	}
}

// ====== StrokeProcessor Reset 测试 ======

func TestStrokeProcessorReset(t *testing.T) {
	bp := NewStrokeProcessor()
	state := bp.getOrCreateState("TEST")
	state.strokes = append(state.strokes, &stroke{Index: 0, Confirmed: true})

	bp.Reset("TEST")

	// 重置后 Strokes 应返回空
	if len(bp.Strokes("TEST")) != 0 {
		t.Error("重置后期望空笔列表")
	}

	// 不存在的 symbol 不 panic
	bp.Reset("NONEXIST")
}
