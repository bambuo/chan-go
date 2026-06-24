package chanlun

import (
	"testing"

	"trade/internal/types"
)

// ====== 特征序列缺口检测测试 ======

// TestFeatureSeq_Gap 验证缺口检测 (hasGap): 两根特征K线区间完全不重叠。
func TestFeatureSeq_Gap(t *testing.T) {
	// 向下线段 segDir=down → 特征序列=向上笔, 合并方向=向上
	// 构造两个完全不重叠的向上笔,之间有缺口
	biUp1 := &stroke{Index: 0, Direction: types.DirectionUp, StartPrice: 10, EndPrice: 20, High: 22, Low: 8, Confirmed: true}
	biDown := &stroke{Index: 1, Direction: types.DirectionDown, StartPrice: 20, EndPrice: 12, High: 22, Low: 10, Confirmed: true}
	biUp2 := &stroke{Index: 2, Direction: types.DirectionUp, StartPrice: 12, EndPrice: 25, High: 28, Low: 10, Confirmed: true}

	// 向下线段,特征序列=向上笔
	strokes := []*stroke{biUp1, biDown, biUp2}
	segDir := types.DirectionDown
	fs := buildFeatureSeq(strokes, segDir, types.FeatureSeqPrimary)

	// 特征序列应该是向上笔: biUp1, biUp2
	if len(fs.raw) != 2 {
		t.Fatalf("原始特征序列期望 2 个元素, 实际 %d", len(fs.raw))
	}

	// 检查是否有缺口: biUp1.high=22 < biUp2.low=10? No. biUp1.low=8 > biUp2.high=28? No.
	// 所以没有缺口
	if hasGap(fs.raw[0], fs.raw[1]) {
		t.Error("biUp1[8,22] 和 biUp2[10,28] 有重叠,不应检测为缺口")
	}

	// 直接用 fk 测试 hasGap
	t.Run("direct_hasGap", func(t *testing.T) {
		// No gap: ranges overlap
		a := &fk{high: 20, low: 10}
		b := &fk{high: 25, low: 15}
		if hasGap(a, b) {
			t.Error("[10,20] 和 [15,25] 有重叠,不应有缺口")
		}
		if hasGap(b, a) {
			t.Error("[15,25] 和 [10,20] 有重叠,不应有缺口")
		}

		// Gap: ranges don't overlap (gap up: a is below b)
		c := &fk{high: 10, low: 5}
		d := &fk{high: 30, low: 20}
		if !hasGap(c, d) {
			t.Error("[5,10] 和 [20,30] 无重叠,应有缺口")
		}
		if !hasGap(d, c) {
			t.Error("[20,30] 和 [5,10] 无重叠,应有缺口")
		}

		// Adjacent but not overlapping (edge case: touch)
		// Actually if high == low, it's still overlapping (touch counts as contained)
		e := &fk{high: 15, low: 10}
		f := &fk{high: 15, low: 12}
		if hasGap(e, f) {
			t.Error("[10,15] 和 [12,15] 有接触,不应有缺口")
		}
	})
}

// ====== 向下线段情况二完整确认测试（对称方向） ======

// TestSegment_Type2_Down_SecondSeqConfirms 验证向下线段情况二：
// 第一特征序列（向上笔）底分型有缺口 → 情况二，
// 第二特征序列（向下笔）出现顶分型后确认线段结束。
//
// 向下线段的特征序列 = 向上笔，只考察底分型（第67课）。
//   X1: [40, 52]   (Index2)   ← 第1元素
//   X2: [10, 22]   (Index4)   ← 中点（low 最低、high 最低），转折点
//   X3: [30, 42]   (Index6)   ← 第3元素
// X1[40,52] 与 X2[10,22] 不重叠 → 有缺口 → 情况二。
//
// 转折点(Index4)之后，新向上线段的第二特征序列 = 向下笔，需形成顶分型：
//   D1: [34, 48]   (Index5)   ← 左
//   D2: [50, 62]   (Index7)   ← 中（high 最高、low 最高）
//   D3: [38, 52]   (Index9)   ← 右
func TestSegment_Type2_Down_SecondSeqConfirms(t *testing.T) {
	sp := NewSegmentProcessor()

	strokes := []*stroke{
		{Index: 0, Direction: types.DirectionDown, StartPrice: 60, EndPrice: 42, High: 62, Low: 40, Confirmed: true},
		{Index: 1, Direction: types.DirectionDown, StartPrice: 42, EndPrice: 36, High: 44, Low: 34, Confirmed: true},
		{Index: 2, Direction: types.DirectionUp, StartPrice: 36, EndPrice: 50, High: 52, Low: 40, Confirmed: true}, // X1 [40,52]
		{Index: 3, Direction: types.DirectionDown, StartPrice: 50, EndPrice: 20, High: 22, Low: 18, Confirmed: true},
		{Index: 4, Direction: types.DirectionUp, StartPrice: 20, EndPrice: 28, High: 22, Low: 10, Confirmed: true}, // X2 [10,22] 转折点
		{Index: 5, Direction: types.DirectionDown, StartPrice: 28, EndPrice: 36, High: 48, Low: 34, Confirmed: true}, // D1 [34,48]
		{Index: 6, Direction: types.DirectionUp, StartPrice: 36, EndPrice: 44, High: 42, Low: 30, Confirmed: true}, // X3 [30,42]
		{Index: 7, Direction: types.DirectionDown, StartPrice: 44, EndPrice: 58, High: 62, Low: 50, Confirmed: true}, // D2 [50,62] 中点
		{Index: 8, Direction: types.DirectionUp, StartPrice: 58, EndPrice: 50, High: 60, Low: 48, Confirmed: true},
		{Index: 9, Direction: types.DirectionDown, StartPrice: 50, EndPrice: 44, High: 52, Low: 38, Confirmed: true}, // D3 [38,52]
	}

	segs := sp.Process("TEST_DOWN", strokes)

	if len(segs) == 0 {
		t.Fatal("向下线段情况二第二特征序列出现分型后应确认, 实际无线段完成")
	}
	completed := segs[0]
	if completed.direction != types.DirectionDown {
		t.Errorf("已完成线段方向期望 down, 实际 %v", completed.direction)
	}
	if !completed.confirmed {
		t.Error("已完成线段 confirmed 应为 true")
	}
	// 向下线段必须结束于向下笔
	lastStroke := completed.strokes[len(completed.strokes)-1]
	if lastStroke.Direction != types.DirectionDown {
		t.Errorf("向下线段应结束于向下笔, 实际终点笔方向 %v", lastStroke.Direction)
	}
	t.Logf("向下线段情况二确认成功: 方向=%v 笔数=%d 起止=(%.0f,%.0f)",
		completed.direction, len(completed.strokes), completed.startPrice, completed.endPrice)
}

// ====== 辅助功能测试 ======

// TestFeatureFractalToStrokeIndex 验证特征序列分型索引到笔列表索引的映射。
func TestFeatureFractalToStrokeIndex(t *testing.T) {
	// 向上线段，特征序列 = 向下笔
	// 笔序列: up, up, down, down, down, up, down
	// 特征序列中风向索引从 0 开始
	strokes := []*stroke{
		{Index: 0, Direction: types.DirectionUp, Confirmed: true},
		{Index: 1, Direction: types.DirectionUp, Confirmed: true},
		{Index: 2, Direction: types.DirectionDown, Confirmed: true}, // 特征序列 [0]
		{Index: 3, Direction: types.DirectionDown, Confirmed: true}, // 特征序列 [1]
		{Index: 4, Direction: types.DirectionDown, Confirmed: true}, // 特征序列 [2]
		{Index: 5, Direction: types.DirectionUp, Confirmed: true},
		{Index: 6, Direction: types.DirectionDown, Confirmed: true}, // 特征序列 [3]
	}

	segState := &segState{}
	// 特征序列索引 0 → strokes 索引 2
	if got := segState.featureFractalToStrokeIndex(strokes, types.DirectionUp, 0); got != 2 {
		t.Errorf("特征索引 0 期望 strokes[2], 实际 strokes[%d]", got)
	}
	// 特征序列索引 1 → strokes 索引 3
	if got := segState.featureFractalToStrokeIndex(strokes, types.DirectionUp, 1); got != 3 {
		t.Errorf("特征索引 1 期望 strokes[3], 实际 strokes[%d]", got)
	}
	// 特征序列索引 2 → strokes 索引 4
	if got := segState.featureFractalToStrokeIndex(strokes, types.DirectionUp, 2); got != 4 {
		t.Errorf("特征索引 2 期望 strokes[4], 实际 strokes[%d]", got)
	}
	// 特征序列索引 3 → strokes 索引 6
	if got := segState.featureFractalToStrokeIndex(strokes, types.DirectionUp, 3); got != 6 {
		t.Errorf("特征索引 3 期望 strokes[6], 实际 strokes[%d]", got)
	}

	// 向下线段，特征序列 = 向上笔
	strokes2 := []*stroke{
		{Index: 0, Direction: types.DirectionDown, Confirmed: true},
		{Index: 1, Direction: types.DirectionDown, Confirmed: true},
		{Index: 2, Direction: types.DirectionUp, Confirmed: true}, // 特征序列 [0]
		{Index: 3, Direction: types.DirectionDown, Confirmed: true},
		{Index: 4, Direction: types.DirectionUp, Confirmed: true}, // 特征序列 [1]
		{Index: 5, Direction: types.DirectionUp, Confirmed: true}, // 特征序列 [2]
	}

	// 特征索引 0 → strokes[2]
	if got := segState.featureFractalToStrokeIndex(strokes2, types.DirectionDown, 0); got != 2 {
		t.Errorf("特征索引 0 期望 strokes[2], 实际 strokes[%d]", got)
	}
	// 特征索引 1 → strokes[4]
	if got := segState.featureFractalToStrokeIndex(strokes2, types.DirectionDown, 1); got != 4 {
		t.Errorf("特征索引 1 期望 strokes[4], 实际 strokes[%d]", got)
	}
	// 特征索引 2 → strokes[5]
	if got := segState.featureFractalToStrokeIndex(strokes2, types.DirectionDown, 2); got != 5 {
		t.Errorf("特征索引 2 期望 strokes[5], 实际 strokes[%d]", got)
	}

	// 越界索引应返回最后一个笔索引
	if got := segState.featureFractalToStrokeIndex(strokes2, types.DirectionDown, 99); got != 5 {
		t.Errorf("超界特征索引期望 strokes[5], 实际 strokes[%d]", got)
	}
}

// TestOppositeDirection 验证方向取反函数。
func TestOppositeDirection(t *testing.T) {
	if got := oppositeDirection(types.DirectionUp); got != types.DirectionDown {
		t.Errorf("up 取反期望 down, 实际 %v", got)
	}
	if got := oppositeDirection(types.DirectionDown); got != types.DirectionUp {
		t.Errorf("down 取反期望 up, 实际 %v", got)
	}
	if got := oppositeDirection(types.DirectionNone); got != types.DirectionNone {
		t.Errorf("none 取反期望 none, 实际 %v", got)
	}
}

// TestSegmentToTypes 验证线段到导出类型的转换。
func TestSegmentToTypes(t *testing.T) {
	s := &segment{
		index:       3,
		direction:   types.DirectionUp,
		strokes:     []*stroke{{Index: 0, Confirmed: true}, {Index: 1, Confirmed: true}},
		startStroke: &stroke{Index: 0, Confirmed: true},
		endStroke:   &stroke{Index: 1, Confirmed: true},
		startPrice:  10,
		endPrice:    30,
		high:        35,
		low:         8,
		confirmed:   true,
	}

	ts := segmentToTypes(s)
	if ts.Index != 3 {
		t.Errorf("Index 期望 3, 实际 %d", ts.Index)
	}
	if ts.Direction != types.DirectionUp {
		t.Errorf("Direction 期望 up, 实际 %v", ts.Direction)
	}
	if ts.StartPrice != 10 {
		t.Errorf("StartPrice 期望 10, 实际 %f", ts.StartPrice)
	}
	if ts.EndPrice != 30 {
		t.Errorf("EndPrice 期望 30, 实际 %f", ts.EndPrice)
	}
	if !ts.Confirmed {
		t.Error("Confirmed 应为 true")
	}
	if len(ts.Strokes) != 2 {
		t.Errorf("Strokes 长度期望 2, 实际 %d", len(ts.Strokes))
	}
}

// TestSegmentsToInterface 验证线段列表到 interface 切片的转换。
func TestSegmentsToInterface(t *testing.T) {
	segs := []*segment{
		{index: 0, direction: types.DirectionUp, confirmed: true},
		{index: 1, direction: types.DirectionDown, confirmed: false},
	}

	result := SegmentsToInterface(segs)
	if len(result) != 2 {
		t.Fatalf("结果长度期望 2, 实际 %d", len(result))
	}

	s0, ok := result[0].(*segment)
	if !ok {
		t.Fatal("result[0] 类型不为 *segment")
	}
	if s0.index != 0 {
		t.Errorf("result[0].index 期望 0, 实际 %d", s0.index)
	}

	s1, ok := result[1].(*segment)
	if !ok {
		t.Fatal("result[1] 类型不为 *segment")
	}
	if s1.index != 1 || s1.confirmed {
		t.Errorf("result[1]: 期望 index=1,confirmed=false; 实际 index=%d,confirmed=%v", s1.index, s1.confirmed)
	}

	// 空列表
	empty := SegmentsToInterface(nil)
	if len(empty) != 0 {
		t.Errorf("nil 输入期望空结果, 实际 %d", len(empty))
	}
}

// TestStrokeToSegmentStrokes 验证笔列表到 interface 切片的转换。
func TestStrokeToSegmentStrokes(t *testing.T) {
	strokes := []*stroke{
		{Index: 0, Direction: types.DirectionUp, Confirmed: true},
		{Index: 1, Direction: types.DirectionDown, Confirmed: true},
	}

	result := strokeToSegmentStrokes(strokes)
	if len(result) != 2 {
		t.Fatalf("结果长度期望 2, 实际 %d", len(result))
	}

	// 空列表
	empty := strokeToSegmentStrokes(nil)
	if len(empty) != 0 {
		t.Errorf("nil 输入期望空结果, 实际 %d", len(empty))
	}
}

// TestSegmentProcessor_Reset 验证重置功能。
func TestSegmentProcessor_Reset(t *testing.T) {
	sp := NewSegmentProcessor()

	strokes := []*stroke{
		{Index: 0, Direction: types.DirectionUp, StartPrice: 10, EndPrice: 20, High: 22, Low: 8, Confirmed: true},
		{Index: 1, Direction: types.DirectionUp, StartPrice: 20, EndPrice: 30, High: 32, Low: 18, Confirmed: true},
	}
	sp.Process("TEST", strokes)

	// 重置
	sp.Reset("TEST")

	// 重置后查询应返回空
	segs := sp.CurrentSegments("TEST")
	if len(segs) != 0 {
		t.Errorf("重置后期望空线段列表, 实际 %d", len(segs))
	}

	// 重置另一个未创建的 symbol 不应 panic
	sp.Reset("NONEXIST")
}
