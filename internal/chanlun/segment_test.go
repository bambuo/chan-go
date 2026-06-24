// Package chanlun 特征序列与线段划分算法测试。
package chanlun

import (
	"testing"

	"trade/internal/types"
)

// ====== 辅助函数：构建笔序列 ======

// mkStroke 快速创建一根确认笔。
func mkStroke(index int, startHigh, startLow, endHigh, endLow float64, direction types.ChanDirection) *stroke {
	var startPrice, endPrice, high, low float64
	if direction == types.DirectionUp {
		startPrice = startLow
		endPrice = endHigh
	} else {
		startPrice = startHigh
		endPrice = endLow
	}
	high = startPrice
	if endPrice > high {
		high = endPrice
	}
	low = startPrice
	if endPrice < low {
		low = endPrice
	}

	return &stroke{
		Index:      index,
		Direction:  direction,
		Confirmed:  true,
		StartPrice: startPrice,
		EndPrice:   endPrice,
		High:       high,
		Low:        low,
	}
}

// ====== 特征序列测试 ======

// TestFeatureSeq_Basic 验证：向上线段的特征序列 = 向下笔。
func TestFeatureSeq_Basic(t *testing.T) {
	bis := []*stroke{
		mkStroke(0, 10, 5, 20, 15, types.DirectionUp),    // 向上笔
		mkStroke(1, 20, 15, 15, 8, types.DirectionDown),  // 向下笔 → 特征序列元素
		mkStroke(2, 15, 8, 25, 18, types.DirectionUp),    // 向上笔
		mkStroke(3, 25, 18, 18, 10, types.DirectionDown), // 向下笔 → 特征序列元素
	}

	fs := buildFeatureSeq(bis, types.DirectionUp, types.FeatureSeqPrimary) // 向上线段的特征序列

	if len(fs.elems) != 2 {
		t.Fatalf("特征序列期望 2 个元素, 实际 %d", len(fs.elems))
	}
	if fs.elems[0].sourceStroke != bis[1] {
		t.Error("第 1 个特征元素应来自第 2 根笔（向下）")
	}
	if fs.elems[1].sourceStroke != bis[3] {
		t.Error("第 2 个特征元素应来自第 4 根笔（向下）")
	}
}

// TestFeatureSeq_FractalTop 验证：特征序列顶分型检测。
func TestFeatureSeq_FractalTop(t *testing.T) {
	// 构造特征序列：低 → 高 → 中（左<中>右 → 顶分型）
	bis := []*stroke{
		mkStroke(0, 10, 5, 15, 10, types.DirectionUp),
		mkStroke(1, 15, 10, 12, 6, types.DirectionDown), // 低
		mkStroke(2, 12, 6, 18, 12, types.DirectionUp),
		mkStroke(3, 18, 12, 14, 8, types.DirectionDown), // 高（顶分型中间）
		mkStroke(4, 14, 8, 16, 10, types.DirectionUp),
		mkStroke(5, 16, 10, 13, 7, types.DirectionDown), // 中
	}

	fs := buildFeatureSeq(bis, types.DirectionUp, types.FeatureSeqPrimary)
	fractal := fs.detectFractal()

	if !fractal.HasTop {
		t.Error("期望检测到顶分型")
	}
}

// TestFeatureSeq_Containment 验证：特征序列包含处理。
func TestFeatureSeq_Containment(t *testing.T) {
	// 构造被包含的特征K线
	bis := []*stroke{
		mkStroke(0, 10, 5, 20, 12, types.DirectionUp),
		mkStroke(1, 20, 12, 14, 6, types.DirectionDown), // 特征1: high=14 low=6
		mkStroke(2, 14, 6, 18, 10, types.DirectionUp),
		mkStroke(3, 18, 10, 13, 7, types.DirectionDown), // 特征2: high=13 low=7 → 被特征1包含
	}

	fs := buildFeatureSeq(bis, types.DirectionUp, types.FeatureSeqPrimary)

	// 包含处理后应只剩 1 个特征元素
	if len(fs.elems) != 1 {
		t.Fatalf("包含处理后期望 1 个元素, 实际 %d", len(fs.elems))
	}
}

// ====== 线段划分测试 ======

// TestSegment_SingleUp 验证：单条向上线段。
func TestSegment_SingleUp(t *testing.T) {
	sp := NewSegmentProcessor()

	bis := []*stroke{
		mkStroke(0, 10, 5, 20, 15, types.DirectionUp),
		mkStroke(1, 20, 15, 30, 25, types.DirectionUp),
		mkStroke(2, 30, 25, 40, 35, types.DirectionUp),
	}

	segs := sp.Process("TEST", bis)

	if len(segs) != 0 {
		t.Fatalf("仅同向笔时不应有已完成的线段, 实际 %d", len(segs))
	}
}

// TestSegment_FirstBiDefinesDirection 验证：第一笔方向定义线段方向。
func TestSegment_FirstBiDefinesDirection(t *testing.T) {
	sp := NewSegmentProcessor()

	bis := []*stroke{
		mkStroke(0, 10, 5, 20, 15, types.DirectionUp),
	}

	segs := sp.Process("TEST", bis)
	t.Logf("线段数: %d", len(segs))
}

// TestSegment_NoBreak_UntilFractal 验证：仅同向笔 + 少量反向笔（不足以形成特征序列分型）时，不产出已完成线段。
func TestSegment_NoBreak_UntilFractal(t *testing.T) {
	sp := NewSegmentProcessor()

	// 向上线段：2 根向上笔 + 1 根向下笔。特征序列仅 1 个元素，无法形成分型。
	bis := []*stroke{
		mkStroke(0, 10, 5, 20, 15, types.DirectionUp),
		mkStroke(1, 20, 15, 30, 25, types.DirectionUp),
		mkStroke(2, 30, 25, 22, 18, types.DirectionDown),
	}

	segs := sp.Process("TEST", bis)

	if len(segs) != 0 {
		t.Fatalf("特征序列不足以形成分型时不应有已完成线段, 实际 %d", len(segs))
	}
}

// TestSegment_Type1_NoGap 验证情况一（第67课）：特征序列分型第1、2元素间无缺口 → 直接确认线段结束。
//
// 构造向上线段，特征序列（向下笔）形成顶分型，且顶分型第1、2元素区间重叠（无缺口）。
func TestSegment_Type1_NoGap(t *testing.T) {
	sp := NewSegmentProcessor()

	// 向上线段 + 向下笔特征序列。
	// 特征序列元素（向下笔的 High/Low）：
	//   X1: [40, 52]   ← 顶分型第1元素
	//   X2: [48, 60]   ← 顶分型中点（High 最高、Low 最高）
	//   X3: [42, 50]   ← 顶分型第3元素
	// X1 与 X2 区间 [40,52] vs [48,60] 重叠（48<=52）→ 无缺口 → 情况一。
	// 顶分型：X2.high=60>X1.high=52 且 >X3.high=50；X2.low=48>X1.low=40 且 >X3.low=42。
	strokes := []*stroke{
		// 向上线段（2 根向上笔）
		{Index: 0, Direction: types.DirectionUp, StartPrice: 10, EndPrice: 50, High: 52, Low: 8, Confirmed: true},
		{Index: 1, Direction: types.DirectionUp, StartPrice: 50, EndPrice: 58, High: 60, Low: 48, Confirmed: true},
		// 特征序列（向下笔）
		{Index: 2, Direction: types.DirectionDown, StartPrice: 58, EndPrice: 42, High: 52, Low: 40, Confirmed: true}, // X1 [40,52]
		// 中间一根向上笔分隔特征元素
		{Index: 3, Direction: types.DirectionUp, StartPrice: 42, EndPrice: 56, High: 58, Low: 40, Confirmed: true},
		{Index: 4, Direction: types.DirectionDown, StartPrice: 56, EndPrice: 48, High: 60, Low: 48, Confirmed: true}, // X2 [48,60] 中点
		{Index: 5, Direction: types.DirectionUp, StartPrice: 48, EndPrice: 54, High: 56, Low: 46, Confirmed: true},
		{Index: 6, Direction: types.DirectionDown, StartPrice: 54, EndPrice: 44, High: 50, Low: 42, Confirmed: true}, // X3 [42,50]
	}

	segs := sp.Process("TEST", strokes)

	if len(segs) == 0 {
		t.Fatal("情况一无缺口应直接确认线段结束, 实际无线段完成")
	}
	completed := segs[0]
	if completed.direction != types.DirectionUp {
		t.Errorf("已完成线段方向期望 up, 实际 %v", completed.direction)
	}
	if !completed.confirmed {
		t.Error("已完成线段 confirmed 应为 true")
	}
	t.Logf("情况一确认成功: 线段方向=%v 笔数=%d 起止=(%.0f,%.0f)",
		completed.direction, len(completed.strokes), completed.startPrice, completed.endPrice)
}

// TestSegment_Type2_WithGap_NeedsSecondSeq 验证情况二（第67课）：特征序列分型第1、2元素间有缺口 → 需第二特征序列分型才确认。
//
// 仅给到第一特征序列分型形成、但第二特征序列尚未出现分型的阶段，断言线段仍未完成。
func TestSegment_Type2_WithGap_NeedsSecondSeq(t *testing.T) {
	sp := NewSegmentProcessor()

	// 向上线段，特征序列顶分型第1、2元素有缺口。
	// X1: [10, 20]   ← 第1元素
	// X2: [40, 55]   ← 中点（high 最高、low 最高），与 X1 区间 [10,20] 不重叠 → 有缺口 → 情况二
	// X3: [35, 48]   ← 第3元素
	// 此时第二特征序列（转折点后与原线段同向的向上笔）仅 1 根，无法形成分型 → 不确认。
	strokes := []*stroke{
		{Index: 0, Direction: types.DirectionUp, StartPrice: 5, EndPrice: 25, High: 27, Low: 3, Confirmed: true},
		{Index: 1, Direction: types.DirectionUp, StartPrice: 25, EndPrice: 30, High: 32, Low: 23, Confirmed: true},
		// 特征序列
		{Index: 2, Direction: types.DirectionDown, StartPrice: 30, EndPrice: 12, High: 20, Low: 10, Confirmed: true}, // X1 [10,20]
		{Index: 3, Direction: types.DirectionUp, StartPrice: 12, EndPrice: 42, High: 44, Low: 10, Confirmed: true},
		{Index: 4, Direction: types.DirectionDown, StartPrice: 42, EndPrice: 40, High: 55, Low: 40, Confirmed: true}, // X2 [40,55] 中点
		{Index: 5, Direction: types.DirectionUp, StartPrice: 40, EndPrice: 46, High: 48, Low: 38, Confirmed: true},
		{Index: 6, Direction: types.DirectionDown, StartPrice: 46, EndPrice: 36, High: 48, Low: 35, Confirmed: true}, // X3 [35,48]
	}

	segs := sp.Process("TEST", strokes)

	if len(segs) != 0 {
		t.Fatalf("情况二有缺口且第二特征序列未成形时不应确认, 实际完成 %d 段", len(segs))
	}
	state := sp.getState("TEST")
	if state.pending == nil {
		t.Error("情况二应记录 pending 待确认态")
	}
	t.Log("情况二正确进入待确认态，未提前确认 ✅")
}

// TestSegment_Type2_SecondSeqConfirms 验证情况二：第二特征序列出现分型后确认线段结束。
//
// 第一特征序列顶分型（X1/X2/X3）有缺口 → 情况二。
// 转折点 = X2 对应的向下笔。其后构成假设的新（向下）线段，
// 其特征序列 = 向上笔，需形成底分型。
func TestSegment_Type2_SecondSeqConfirms(t *testing.T) {
	sp := NewSegmentProcessor()

	// 第一特征序列（向下笔）顶分型，第1/2元素有缺口：
	//   X1: [10, 20]   (Index2)
	//   X2: [40, 55]   (Index4) ← 中点（high 最高、low 最高），转折点
	//   X3: [35, 48]   (Index6)
	// X1[10,20] 与 X2[40,55] 不重叠 → 有缺口 → 情况二。
	//
	// 转折点(Index4)之后，新向下线段的第二特征序列 = 向上笔，需形成底分型：
	//   U1: [34, 50]   (Index5)  ← 左
	//   U2: [20, 42]   (Index7)  ← 中（low=20 最低、high=42 最低）
	//   U3: [38, 55]   (Index9)  ← 右
	// U1/U2/U3 互不包含；底分型成立。
	strokes := []*stroke{
		{Index: 0, Direction: types.DirectionUp, StartPrice: 5, EndPrice: 25, High: 27, Low: 3, Confirmed: true},
		{Index: 1, Direction: types.DirectionUp, StartPrice: 25, EndPrice: 30, High: 32, Low: 23, Confirmed: true},
		{Index: 2, Direction: types.DirectionDown, StartPrice: 30, EndPrice: 12, High: 20, Low: 10, Confirmed: true}, // X1 [10,20]
		{Index: 3, Direction: types.DirectionUp, StartPrice: 12, EndPrice: 42, High: 44, Low: 10, Confirmed: true},
		{Index: 4, Direction: types.DirectionDown, StartPrice: 42, EndPrice: 40, High: 55, Low: 40, Confirmed: true}, // X2 [40,55] 转折点
		{Index: 5, Direction: types.DirectionUp, StartPrice: 40, EndPrice: 50, High: 50, Low: 34, Confirmed: true},  // U1 [34,50]
		{Index: 6, Direction: types.DirectionDown, StartPrice: 50, EndPrice: 36, High: 48, Low: 35, Confirmed: true}, // X3 [35,48]
		{Index: 7, Direction: types.DirectionUp, StartPrice: 36, EndPrice: 42, High: 42, Low: 20, Confirmed: true},  // U2 [20,42] 中点
		{Index: 8, Direction: types.DirectionDown, StartPrice: 42, EndPrice: 36, High: 44, Low: 34, Confirmed: true},
		{Index: 9, Direction: types.DirectionUp, StartPrice: 36, EndPrice: 55, High: 55, Low: 38, Confirmed: true}, // U3 [38,55]
	}

	segs := sp.Process("TEST", strokes)

	if len(segs) == 0 {
		t.Fatal("情况二第二特征序列出现分型后应确认线段结束, 实际无线段完成")
	}
	completed := segs[0]
	if completed.direction != types.DirectionUp {
		t.Errorf("已完成线段方向期望 up, 实际 %v", completed.direction)
	}
	if !completed.confirmed {
		t.Error("已完成线段 confirmed 应为 true")
	}
	// 线段终点方向应与线段方向一致（向上线段结束于向上笔）
	lastStroke := completed.strokes[len(completed.strokes)-1]
	if lastStroke.Direction != types.DirectionUp {
		t.Errorf("向上线段应结束于向上笔, 实际终点笔方向 %v", lastStroke.Direction)
	}
	t.Logf("情况二第二特征序列确认成功: 线段方向=%v 笔数=%d 起止=(%.0f,%.0f)",
		completed.direction, len(completed.strokes), completed.startPrice, completed.endPrice)
}

// TestSegment_PipelineIntegration 验证：完整 contain→fractal→stroke→segment pipeline。
func TestSegment_PipelineIntegration(t *testing.T) {
	p := NewPipeline()

	// 用较大的 zigzag 输入确保足够跨度
	klines := []float64{
		20, 15, // 顶分型
		10, 5, // 底分型
		18, 12, // 顶分型
		8, 3, // 底分型
		15, 10, // 顶分型
	}

	for i := 0; i < len(klines); i += 2 {
		high := klines[i]
		low := klines[i+1]
		raw := mkline(low+2, high, low, (high+low)/2, int64(i/2), "TEST_SEG")
		p.Process(raw)
	}

	state := p.GetState("TEST_SEG")
	t.Logf("元素: %d, 分型: %d, 笔: %d, 线段: %d",
		len(state.AllElements), len(state.AllFractals), len(state.Strokes), len(state.Segments))
}
