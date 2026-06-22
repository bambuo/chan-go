package chanlun

import (
	"testing"

	"trade/internal/types"

	"github.com/shopspring/decimal"
)

// ck 根据高/低值创建一个ChanKline辅助函数。
func ck(high, low float64) *types.ChanKline {
	return &types.ChanKline{
		High:       high,
		Low:        low,
		RawHigh:    high,
		RawLow:     low,
		MergedFrom: 1,
		Direction:  types.DirectionNone,
	}
}

// rk 创建原始K线的辅助函数。
func rk(open, high, low, close float64, openTime int64) *types.Kline {
	return &types.Kline{
		Open:     decimal.NewFromFloat(open),
		High:     decimal.NewFromFloat(high),
		Low:      decimal.NewFromFloat(low),
		Close:    decimal.NewFromFloat(close),
		OpenTime: openTime,
		IsClosed: true,
	}
}

// --- 基础测试 ---

// TestContain_NoContainment 验证不包含的K线原样通过。
func TestContain_NoContainment(t *testing.T) {
	p := NewContainProcessor()
	inputs := []*types.Kline{
		rk(10, 15, 8, 12, 1),
		rk(12, 18, 11, 16, 2),
		rk(16, 20, 14, 18, 3),
	}

	var result []*types.ChanKline
	for _, k := range inputs {
		result = p.Process(k)
	}

	if len(result) != 3 {
		t.Fatalf("期望3个非包含元素，实际 %d", len(result))
	}

	expected := []struct{ high, low float64 }{
		{15, 8},
		{18, 11},
		{20, 14},
	}
	for i, e := range result {
		if e.High != expected[i].high || e.Low != expected[i].low {
			t.Errorf("元素%d: 期望high=%.1f low=%.1f, 实际high=%.1f low=%.1f",
				i, expected[i].high, expected[i].low, e.High, e.Low)
		}
		if e.Contained {
			t.Errorf("元素%d不应被标记为包含", i)
		}
	}
}

// TestContain_SimpleUpContainment 测试简单的向上包含合并。
func TestContain_SimpleUpContainment(t *testing.T) {
	p := NewContainProcessor()

	inputs := []*types.Kline{
		rk(10, 15, 8, 12, 1),  // K1: high=15, low=8
		rk(12, 18, 11, 16, 2), // K2: high=18, low=11 - 从K1向上
		rk(14, 17, 12, 15, 3), // K3: high=17 <= 18 且 low=12 >= 11 -> 被包含!
	}

	var result []*types.ChanKline
	for _, k := range inputs {
		result = p.Process(k)
	}

	if len(result) != 2 {
		t.Fatalf("期望2个非包含元素（K2吸收K3），实际 %d", len(result))
	}

	// 向上包含合并后：K2吸收K3 → high保持18, low变为12。
	if result[1].High != 18 {
		t.Errorf("期望合并后high=18，实际 %.1f", result[1].High)
	}
	if result[1].Low != 12 {
		t.Errorf("期望合并后low=12，实际 %.1f", result[1].Low)
	}
	if result[1].MergedFrom != 2 {
		t.Errorf("期望MergedFrom=2，实际 %d", result[1].MergedFrom)
	}
}

// TestContain_SimpleDownContainment 测试简单的向下包含合并。
func TestContain_SimpleDownContainment(t *testing.T) {
	p := NewContainProcessor()

	inputs := []*types.Kline{
		rk(20, 25, 18, 22, 1), // K1: high=25, low=18
		rk(18, 22, 15, 19, 2), // K2: high=22, low=15 - 从K1向下
		rk(16, 21, 16, 17, 3), // K3: high=21 <= 22 且 low=16 >= 15 -> 被包含!
	}

	var result []*types.ChanKline
	for _, k := range inputs {
		result = p.Process(k)
	}

	if len(result) != 2 {
		t.Fatalf("期望2个非包含元素（K2吸收K3），实际 %d", len(result))
	}

	// 向下包含合并后：high变为21（取较低高），low保持15（取较低低）。
	if result[1].High != 21 {
		t.Errorf("期望合并后high=21，实际 %.1f", result[1].High)
	}
	if result[1].Low != 15 {
		t.Errorf("期望合并后low=15，实际 %.1f", result[1].Low)
	}
}

// TestContain_SequentialContainment 测试级联包含（连续包含，merge后下一根继续包含）。
func TestContain_SequentialContainment(t *testing.T) {
	p := NewContainProcessor()

	inputs := []*types.Kline{
		rk(10, 15, 8, 12, 1),  // K1: high=15, low=8
		rk(12, 19, 11, 17, 2), // K2: high=19, low=11 - 向上
		rk(14, 18, 12, 16, 3), // K3: 被K2包含 (18<=19, 12>=11) → 向上合并K2(19,12)
		rk(13, 20, 13, 15, 4), // K4: high=20 > 19, low=13 > 12 → 不被包含, 新方向向上
	}

	var result []*types.ChanKline
	for _, k := range inputs {
		result = p.Process(k)
	}

	if len(result) != 3 {
		t.Fatalf("期望3个非包含元素（K1, K2+K3, K4），实际 %d", len(result))
	}

	// K2合并后 high=19, low=12, MergedFrom=2
	if result[1].High != 19 || result[1].Low != 12 {
		t.Errorf("K2合并后: 期望high=19 low=12, 实际high=%.1f low=%.1f", result[1].High, result[1].Low)
	}
	if result[1].MergedFrom != 2 {
		t.Errorf("K2期望MergedFrom=2（吸收了K3），实际 %d", result[1].MergedFrom)
	}
	// K4 不被包含，方向K2→K4向上
	if result[2].High != 20 || result[2].Low != 13 {
		t.Errorf("K4: 期望high=20 low=13, 实际high=%.1f low=%.1f", result[2].High, result[2].Low)
	}
}

// --- 方向变化测试 ---

// TestContain_DirectionChangeAfterMerge 验证合并后方向被正确重新评估。
func TestContain_DirectionChangeAfterMerge(t *testing.T) {
	p := NewContainProcessor()

	inputs := []*types.Kline{
		rk(10, 15, 8, 12, 1),  // K1: high=15, low=8
		rk(12, 18, 11, 16, 2), // K2: high=18, low=11 - 从K1向上
		rk(14, 14, 13, 13, 3), // K3: 被K2包含 (14<=18, 13>=11) → 向上合并K2(18,13)
		rk(13, 17, 10, 14, 4), // K4: vs K2(18,13): 17<18且10<13 → 向下, 不被包含 → 新元素
	}

	var result []*types.ChanKline
	for _, k := range inputs {
		result = p.Process(k)
	}

	if len(result) != 3 {
		t.Fatalf("期望3个非包含元素，实际 %d", len(result))
	}

	// K2合并后 high=18, low=13
	if result[1].High != 18 || result[1].Low != 13 {
		t.Errorf("K2合并后: 期望high=18 low=13, 实际high=%.1f low=%.1f", result[1].High, result[1].Low)
	}
	// K4 不被包含，成为独立元素
	if result[2].High != 17 || result[2].Low != 10 {
		t.Errorf("K4: 期望high=17 low=10, 实际high=%.1f low=%.1f", result[2].High, result[2].Low)
	}
}

// TestContain_DirectionChangeMidSequence 验证中间方向变化场景。
func TestContain_DirectionChangeMidSequence(t *testing.T) {
	p := NewContainProcessor()

	// 场景：向上→向下→向上，包含发生在各段内部
	inputs := []*types.Kline{
		rk(10, 20, 10, 12, 1), // K1: high=20, low=10
		rk(12, 22, 12, 16, 2), // K2: high=22, low=12 - 向上
		rk(14, 18, 14, 15, 3), // K3: vs K2(22,12): 18<=22且14>=12 → 包含, 向上合并 → K2(22,14)
		rk(13, 17, 9, 14, 4),  // K4: vs K2(22,14): 17<22且9<14 → 向下, 不包含 → 新元素
		rk(12, 16, 8, 13, 5),  // K5: vs K4(17,9): 16<=17且8>=9? No. 16>=17? No. 不包含。
		//     方向K4→K5: 16<17且8<9 → 向下, 不包含 → 新元素
		rk(11, 14, 11, 12, 6), // K6: vs K5(16,8): 14<=16且11>=8 → 包含, 向下合并 → K5(14,8)
		rk(10, 15, 10, 11, 7), // K7: vs K5(14,8): 15>=14且10>=8? 15>14, so not (15<=14). 15>=14 && 10<=8? No (10>8). 不包含。
		//     方向K5→K7: 15>14且10>8 → 向上, 新元素
	}

	var result []*types.ChanKline
	for _, k := range inputs {
		result = p.Process(k)
	}

	if len(result) != 5 {
		t.Fatalf("期望5个非包含元素，实际 %d", len(result))
	}

	// result[0] = K1(20,10)
	// result[1] = K2+K3(22,14)
	// result[2] = K4(17,9)
	// result[3] = K5+K6(14,8)
	// result[4] = K7(15,10)

	checks := []struct {
		idx             int
		expHigh, expLow float64
		expMerged       int
	}{
		{0, 20, 10, 1},
		{1, 22, 14, 2},
		{2, 17, 9, 1},
		{3, 14, 8, 2},
		{4, 15, 10, 1},
	}
	for _, c := range checks {
		e := result[c.idx]
		if e.High != c.expHigh || e.Low != c.expLow {
			t.Errorf("元素%d: 期望high=%.1f low=%.1f, 实际high=%.1f low=%.1f",
				c.idx, c.expHigh, c.expLow, e.High, e.Low)
		}
		if e.MergedFrom != c.expMerged {
			t.Errorf("元素%d: 期望MergedFrom=%d, 实际%d", c.idx, c.expMerged, e.MergedFrom)
		}
	}
}

// --- 特殊场景测试 ---

// TestContain_DirectionNoneMerge 验证方向无法确定时走向上分支。
//
// 注：缠论中两根K线方向不明（DirectionNone）时一定存在包含关系，
// 所以 resolveDirection 的 DirectionNone→DirectionUp 分支在实际流程中
// 仅当 prevPrevIdx<0（只有一根非包含元素）时可达。
// 此场景已在 TestContain_InitialTwoContained 等测试中覆盖。
//
// 这里直接测试 resolveDirection 的逻辑正确性。
func TestContain_DirectionNoneMerge(t *testing.T) {
	p := NewContainProcessor()

	// 场景1：prevPrevIdx < 0 → DirectionUp
	// 只有一根非包含元素，前两根K线包含时触发。
	p.Process(rk(10, 15, 5, 12, 1))
	result := p.Process(rk(8, 12, 8, 9, 2))

	if len(result) != 1 {
		t.Fatalf("场景1失败：期望1个非包含元素，实际 %d", len(result))
	}
	if result[0].High != 15 || result[0].Low != 8 {
		t.Errorf("场景1失败：期望high=15 low=8, 实际high=%.1f low=%.1f", result[0].High, result[0].Low)
	}
	if result[0].MergedFrom != 2 {
		t.Errorf("场景1失败：期望MergedFrom=2, 实际 %d", result[0].MergedFrom)
	}

	// 场景2：两根非包含元素方向明确（向上）
	// 验证第三根包含时向上合并正常。
	p2 := NewContainProcessor()
	p2.Process(rk(10, 15, 8, 12, 1))
	p2.Process(rk(12, 18, 11, 16, 2))      // K2向上
	r := p2.Process(rk(14, 17, 12, 15, 3)) // K3被K2包含
	if len(r) != 2 {
		t.Fatalf("场景2失败：期望2个非包含元素，实际 %d", len(r))
	}
	if r[1].High != 18 || r[1].Low != 12 {
		t.Errorf("场景2失败：期望high=18 low=12, 实际high=%.1f low=%.1f", r[1].High, r[1].Low)
	}

	// 场景3：两根非包含元素方向明确（向下）
	p3 := NewContainProcessor()
	p3.Process(rk(20, 25, 18, 22, 1))
	p3.Process(rk(18, 22, 15, 19, 2))       // K2向下
	r3 := p3.Process(rk(16, 21, 16, 17, 3)) // K3被K2包含
	if len(r3) != 2 {
		t.Fatalf("场景3失败：期望2个非包含元素，实际 %d", len(r3))
	}
	if r3[1].High != 21 || r3[1].Low != 15 {
		t.Errorf("场景3失败：期望high=21 low=15, 实际high=%.1f low=%.1f", r3[1].High, r3[1].Low)
	}
}

// TestContain_InitialTwoContained 验证前两根K线包含时按向上处理。
//
// 缠论规定：起始方向无法判断时统一按向上合并。
func TestContain_InitialTwoContained(t *testing.T) {
	p := NewContainProcessor()

	// K1(15,5), K2(12,8): K2被K1包含 → 方向：只有一根非包含元素 → DirectionUp
	//   向上合并：K1(15,8)
	p.Process(rk(10, 15, 5, 12, 1))
	result := p.Process(rk(8, 12, 8, 9, 2))

	if len(result) != 1 {
		t.Fatalf("期望1个非包含元素（K1吸收K2），实际 %d", len(result))
	}
	if result[0].High != 15 {
		t.Errorf("向上合并后期望high=15，实际 %.1f", result[0].High)
	}
	if result[0].Low != 8 {
		t.Errorf("向上合并后期望low=8，实际 %.1f", result[0].Low)
	}
	if result[0].MergedFrom != 2 {
		t.Errorf("期望MergedFrom=2，实际 %d", result[0].MergedFrom)
	}
}

// TestContain_ForwardCascade 验证正向级联包含（三根K线依次被同一元素吸收）。
func TestContain_ForwardCascade(t *testing.T) {
	p := NewContainProcessor()

	// K1(10,5), K2(15,8) → 向上
	// K3(14,9): 被K2包含 → 向上合并 K2(15,9)
	// K4(13,10): 被K2(15,9)包含 → 向上合并 K2(15,10)
	// K5(16,11): vs K2(15,10): 16>15且11>10 → 不包含, 新方向向上
	inputs := []*types.Kline{
		rk(10, 10, 5, 8, 1),
		rk(12, 15, 8, 13, 2),
		rk(12, 14, 9, 13, 3),
		rk(11, 13, 10, 12, 4),
		rk(14, 16, 11, 15, 5),
	}

	var result []*types.ChanKline
	for _, k := range inputs {
		result = p.Process(k)
	}

	if len(result) != 3 {
		t.Fatalf("期望3个非包含元素，实际 %d", len(result))
	}
	// result[0] = K1(10,5)
	// result[1] = K2+K3+K4(15,10)
	// result[2] = K5(16,11)
	if result[1].High != 15 || result[1].Low != 10 {
		t.Errorf("K2合并后: 期望high=15 low=10, 实际high=%.1f low=%.1f", result[1].High, result[1].Low)
	}
	if result[1].MergedFrom != 3 {
		t.Errorf("K2期望MergedFrom=3（吸收了K3和K4），实际 %d", result[1].MergedFrom)
	}
}

// TestContain_EqualValuesMergeUp 验证等值时包含合并行为（应走向上）。
//
// K1(10,8), K2(10,6): same high, different low。K2 contained by K1。
//
//	prevPrevIdx=-1 → DirectionUp. 向上合并 K1(10,8).
func TestContain_EqualValuesMergeUp(t *testing.T) {
	p := NewContainProcessor()

	p.Process(rk(10, 10, 8, 9, 1))
	result := p.Process(rk(8, 10, 6, 9, 2))

	if len(result) != 1 {
		t.Fatalf("期望1个非包含元素，实际 %d", len(result))
	}
	if result[0].High != 10 || result[0].Low != 8 {
		t.Errorf("向上合并后期望high=10 low=8, 实际high=%.1f low=%.1f", result[0].High, result[0].Low)
	}
}

// --- 辅助函数测试 ---

// TestContain_NoDirection 验证方向无法确定时返回DirectionNone。
func TestContain_NoDirection(t *testing.T) {
	a := ck(10, 5)
	b := ck(10, 8) // 相同高点，不同低点 → 方向不明确
	dir := (&ContainProcessor{}).determineDirection(a, b)
	if dir != types.DirectionNone {
		t.Errorf("期望DirectionNone，实际 %v", dir)
	}
}

// TestIsContained 测试isContained辅助函数。
func TestIsContained(t *testing.T) {
	tests := []struct {
		name   string
		curr   *types.ChanKline
		prev   *types.ChanKline
		result bool
	}{
		{"内部包含", ck(12, 8), ck(15, 5), true},
		{"外部包含（prev在curr内部）", ck(18, 2), ck(15, 5), true},
		{"不被包含（更高更低）", ck(18, 8), ck(15, 5), false},
		{"不被包含（更低更高）", ck(12, 2), ck(15, 5), false},
		{"完全相同", ck(10, 5), ck(10, 5), true},
		// 边界：高低点相等
		{"高点相等内部", ck(10, 6), ck(10, 5), true},
		{"低点相等内部", ck(12, 5), ck(15, 5), true},
		{"完全相等", ck(10, 5), ck(10, 5), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsContained(tt.curr, tt.prev); got != tt.result {
				t.Errorf("IsContained(%s): 期望 %v, 实际 %v", tt.name, tt.result, got)
			}
		})
	}
}

// --- 属性保留测试 ---

// TestContain_RawKlineProperties 验证原始（未合并）属性被保留。
func TestContain_RawKlineProperties(t *testing.T) {
	p := NewContainProcessor()

	p.Process(rk(10, 15, 8, 12, 1))  // K1
	p.Process(rk(12, 18, 11, 16, 2)) // K2
	p.Process(rk(14, 17, 12, 15, 3)) // K3 - 被K2包含

	// 验证K3（被包含元素）Raw属性被保留。
	k3found := false
	for _, e := range p.elements {
		if e.OpenTime == 3 {
			k3found = true
			if !e.Contained {
				t.Error("K3应被标记为包含")
			}
			if e.RawHigh != 17 || e.RawLow != 12 {
				t.Errorf("原始属性未被保留: 期望RawHigh=17 RawLow=12, 实际RawHigh=%.1f RawLow=%.1f",
					e.RawHigh, e.RawLow)
			}
		}
	}
	if !k3found {
		t.Fatal("未找到K3元素")
	}

	// K1的Raw值应等于合并值（K1未参与合并）。
	for _, e := range p.elements {
		if e.OpenTime == 1 {
			if e.RawHigh != e.High || e.RawLow != e.Low {
				t.Errorf("K1原始值应等于合并值: RawHigh=%.1f High=%.1f RawLow=%.1f Low=%.1f",
					e.RawHigh, e.High, e.RawLow, e.Low)
			}
			if e.Contained {
				t.Error("K1不应被标记为包含")
			}
		}
	}

	// K2的Low应被合并改变（吸收了K3的low=12 > K2.low=11）。
	for _, e := range p.elements {
		if e.OpenTime == 2 {
			if !e.Contained {
				if e.MergedFrom != 2 {
					t.Errorf("K2期望MergedFrom=2，实际 %d", e.MergedFrom)
				}
				if e.High != 18 || e.Low != 12 {
					t.Errorf("K2合并后期望high=18 low=12, 实际high=%.1f low=%.1f", e.High, e.Low)
				}
			}
		}
	}
}

// --- 生命周期测试 ---

// TestContain_Reset 验证Reset()清除状态。
func TestContain_Reset(t *testing.T) {
	p := NewContainProcessor()
	p.Process(rk(10, 15, 8, 12, 1))
	p.Process(rk(12, 18, 11, 16, 2))

	if len(p.Elements()) != 2 {
		t.Fatal("重置前期望2个元素")
	}

	p.Reset()
	if len(p.Elements()) != 0 {
		t.Fatal("重置后期望0个元素")
	}
}

// TestContain_EdgeCase_SingleElement 验证只有一个元素时的行为。
func TestContain_EdgeCase_SingleElement(t *testing.T) {
	p := NewContainProcessor()
	result := p.Process(rk(10, 15, 8, 12, 1))
	if len(result) != 1 {
		t.Fatalf("期望1个元素，实际 %d", len(result))
	}
	if result[0].High != 15 || result[0].Low != 8 {
		t.Errorf("值不符合预期: high=%.1f low=%.1f", result[0].High, result[0].Low)
	}
}

// TestContain_EdgeCase_Empty 验证新处理器返回空。
func TestContain_EdgeCase_Empty(t *testing.T) {
	p := NewContainProcessor()
	result := p.Elements()
	if len(result) != 0 {
		t.Fatalf("期望0个元素，实际 %d", len(result))
	}
}

// TestContain_EdgeCase_ContainedByNewElement 验证新K线包含旧K线的情况。
//
// K1(10,5), K2(13,8): 方向向上，不包含。
// K3(15,3): vs K2(13,8): 15>=13 且 3<=8 → 包含（K3范围涵盖K2）。
//
//	direction: K1(10,5)→K2(13,8) 向上。
//	向上合并：K2取较高值 → high=max(13,15)=15, low=max(8,3)=8
func TestContain_EdgeCase_ContainedByNewElement(t *testing.T) {
	p := NewContainProcessor()

	// K1(10,5), K2(13,8): 向上, 不包含
	p.Process(rk(10, 10, 5, 8, 1))
	p.Process(rk(11, 13, 8, 12, 2))

	// K3(15,3): vs K2(13,8): 15>=13且3<=8 → 包含（K3包含K2）！
	//   direction: K1→K2 向上，向上合并取较高值
	result := p.Process(rk(10, 15, 3, 12, 3))

	if len(result) != 2 {
		t.Fatalf("期望2个非包含元素，实际 %d", len(result))
	}
	// K2吸收K3后 high=15, low=8
	if result[1].High != 15 {
		t.Errorf("向上合并后期望high=15，实际 %.1f", result[1].High)
	}
	if result[1].Low != 8 {
		t.Errorf("向上合并后期望low=8，实际 %.1f", result[1].Low)
	}
	if result[1].MergedFrom != 2 {
		t.Errorf("期望MergedFrom=2，实际 %d", result[1].MergedFrom)
	}
}

// TestContain_IsIdempotent 验证相同输入序列多次处理结果一致。
func TestContain_IsIdempotent(t *testing.T) {
	inputs := []*types.Kline{
		rk(10, 15, 8, 12, 1),
		rk(12, 18, 11, 16, 2),
		rk(14, 17, 12, 15, 3),
		rk(13, 19, 10, 14, 4),
		rk(11, 16, 9, 13, 5),
	}

	// 第一次运行。
	p1 := NewContainProcessor()
	for _, k := range inputs {
		p1.Process(k)
	}
	r1 := p1.Elements()

	// 第二次运行。
	p2 := NewContainProcessor()
	for _, k := range inputs {
		p2.Process(k)
	}
	r2 := p2.Elements()

	if len(r1) != len(r2) {
		t.Fatalf("两次运行结果长度不同: %d vs %d", len(r1), len(r2))
	}
	for i := range r1 {
		if r1[i].High != r2[i].High || r1[i].Low != r2[i].Low || r1[i].MergedFrom != r2[i].MergedFrom {
			t.Errorf("元素%d结果不一致", i)
		}
	}
}

// --- 流式实时更新测试 ---

// TestContain_RealtimeUpdateNonContained 验证同周期实时更新非包含元素。
func TestContain_RealtimeUpdateNonContained(t *testing.T) {
	p := NewContainProcessor()

	p.Process(rk(10, 10, 5, 8, 1))  // K1(10,5)
	p.Process(rk(12, 15, 8, 13, 2)) // K2(15,8) 向上

	// 实时更新K2: high=18, low=7（高点变高、低点变低，仍然不包含K1）
	result := p.Process(rk(14, 18, 7, 15, 2))

	if len(result) != 2 {
		t.Fatalf("期望2个非包含元素，实际 %d", len(result))
	}
	if result[1].High != 18 {
		t.Errorf("期望high=18，实际 %.1f", result[1].High)
	}
	if result[1].Low != 7 {
		t.Errorf("期望low=7，实际 %.1f", result[1].Low)
	}
	if len(p.elements) != 2 {
		t.Errorf("实时更新不应增加元素数量，实际 %d", len(p.elements))
	}
}

// TestContain_RealtimeUpdateContainByLowDrop 验证实时更新低点下移导致被包含。
//
// K1(10,5), K2(15,8) 向上不包含。
// 实时更新K2: low=3 → K2(15,3) vs K1(10,5): 15>=10且3<=5 → 包含！
//
//	方向K1→K2向上。向上合并K1: high=max(10,15)=15, low=max(5,3)=5 → K1(15,5).
func TestContain_RealtimeUpdateContainByLowDrop(t *testing.T) {
	p := NewContainProcessor()

	p.Process(rk(10, 10, 5, 8, 1))  // K1(10,5)
	p.Process(rk(12, 15, 8, 13, 2)) // K2(15,8) 向上

	// 实时更新K2: low=3
	result := p.Process(rk(11, 15, 3, 12, 2))

	if len(result) != 1 {
		t.Fatalf("包含后期望1个非包含元素，实际 %d", len(result))
	}
	if result[0].High != 15 || result[0].Low != 5 {
		t.Errorf("期望K1(15,5)，实际 (%.1f,%.1f)", result[0].High, result[0].Low)
	}
	if len(p.elements) != 2 {
		t.Errorf("实时更新不应增加元素数量，实际 %d", len(p.elements))
	}
}

// TestContain_RealtimeBreakContainment 验证实时更新使被包含元素变为非包含。
//
// K1(10,5), K2(15,8), K3(14,9): K3被K2包含 → 向上合并 K2(15,9)
// 实时更新K3: low=6 → K3(14,6) vs K2(15,9): 14<=15且6>=9? No. 14>=15? No. → 不包含！
//
//	回退K2到(15,8), K3设为非包含(14,6)
func TestContain_RealtimeBreakContainment(t *testing.T) {
	p := NewContainProcessor()

	p.Process(rk(10, 10, 5, 8, 1))  // K1(10,5)
	p.Process(rk(12, 15, 8, 13, 2)) // K2(15,8) 向上
	p.Process(rk(14, 14, 9, 13, 3)) // K3(14,9) 被K2包含 → 向上合并K2(15,9)

	elems := p.Elements()
	if len(elems) != 2 {
		t.Fatalf("K3包含后期望2个非包含元素，实际 %d", len(elems))
	}
	if elems[1].High != 15 || elems[1].Low != 9 {
		t.Errorf("K2合并后期望(15,9)，实际 (%.1f,%.1f)", elems[1].High, elems[1].Low)
	}

	// 实时更新K3: low=6 → 不包含K2
	result := p.Process(rk(11, 14, 6, 12, 3))

	if len(result) != 3 {
		t.Fatalf("突破包含后期望3个非包含元素，实际 %d", len(result))
	}
	// K2回到原始值(15,8)
	if result[1].High != 15 || result[1].Low != 8 {
		t.Errorf("K2回退后期望(15,8)，实际 (%.1f,%.1f)", result[1].High, result[1].Low)
	}
	// K3独立(14,6)
	if result[2].High != 14 || result[2].Low != 6 {
		t.Errorf("K3后期望(14,6)，实际 (%.1f,%.1f)", result[2].High, result[2].Low)
	}
}

// TestContain_RealtimeBreakWithCleanRevert 验证回退后K2不被K1级联包含。
//
// K1(10,5), K2(15,8), K3(12,7): K3被K2包含 → 向上合并K2(15,8)
//
//	（12<=15且7>=8? No. 12>=15? No. K3与K2不包含！）
//
// 重新设计：K1(10,5), K2(20,8), K3(12,9): 方向K1→K2向上，K2→K3向下。
//
//	K3(12,9) vs K2(20,8): 12<=20且9>=8 → 包含！方向K1→K2向上。
//	向上合并K2(20,9). K3.Contained=true.
//
// 实时更新K3: high=15, low=12 → K3(15,12). 15<=20且12>=9 → 仍包含。
//
//	回退K2到(20,8), K3(15,12). resolveLast: 15<=20且12>=8 → 包含！
//	向上合并K2(20,12). 又回去了。
//
// 再设计：需要K3更新后与K2不包含。
// K1(10,5), K2(20,8)... K3(15,6) contained by K2 (15<=20且6>=8? No. 15>=20? No.) Not contained.
//
// 需要K3真正被K2包含：K3(18,9) vs K2(20,8): 18<=20且9>=8 → contained!
// 向上合并K2(20,9).
// 更新K3: low=5 → K3(18,5) vs K2(20,9): 18<=20且5>=9? No. 18>=20? No. → 不包含！
// 回退K2到(20,8). K3(18,5). resolveLast: 不包含。
//
//	K2回退(20,8) vs K1(10,5): 20>=10且8>=5? 20>10, 8>5 → 不包含。
//	4个非包含元素？不对。回退后的K2(20,8) vs K1(10,5): 不包含，因为20>10且8>5 → DirectionUp, not contained.
//	所以最终: [K1(10,5), K2(20,8), K3(18,5)]. 3个。✅
func TestContain_RealtimeBreakWithCleanRevert(t *testing.T) {
	p := NewContainProcessor()

	p.Process(rk(10, 10, 5, 8, 1))  // K1(10,5)
	p.Process(rk(15, 20, 8, 18, 2)) // K2(20,8) 向上
	p.Process(rk(17, 18, 9, 18, 3)) // K3(18,9) 被K2包含 → 向上合并K2(20,9)

	elems := p.Elements()
	if len(elems) != 2 {
		t.Fatalf("K3包含后期望2个非包含元素，实际 %d", len(elems))
	}

	// 实时更新K3: low=5 → 不包含K2
	result := p.Process(rk(15, 18, 5, 16, 3))

	if len(result) != 3 {
		t.Fatalf("突破包含后期望3个非包含元素，实际 %d", len(result))
	}
	// K2回到原始值(20,8)
	if result[1].High != 20 || result[1].Low != 8 {
		t.Errorf("K2回退后期望(20,8)，实际 (%.1f,%.1f)", result[1].High, result[1].Low)
	}
	// K3独立(18,5)
	if result[2].High != 18 || result[2].Low != 5 {
		t.Errorf("K3后期望(18,5)，实际 (%.1f,%.1f)", result[2].High, result[2].Low)
	}
}

// --- 性能测试 ---

// BenchmarkContain_Simple 基准测试：1000根K线无包含。
func BenchmarkContain_Simple(b *testing.B) {
	inputs := make([]*types.Kline, 1000)
	for i := 0; i < 1000; i++ {
		h := float64(100 + i)
		l := float64(90 + i)
		inputs[i] = rk(l, h, l-2, h-1, int64(i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := NewContainProcessor()
		for _, k := range inputs {
			p.Process(k)
		}
	}
}

// BenchmarkContain_AllContained 基准测试：1000根K线全部包含（最差情况）。
func BenchmarkContain_AllContained(b *testing.B) {
	// 先升后降，产生大量包含。
	inputs := make([]*types.Kline, 1000)
	for i := 0; i < 500; i++ {
		high := 100.0 + float64(i)*0.5
		low := 80.0 + float64(i)*0.3
		inputs[i] = rk(low, high, low-1, high-1, int64(i))
	}
	for i := 500; i < 1000; i++ {
		high := 100.0 + float64(1000-i)*0.5
		low := 80.0 + float64(1000-i)*0.3
		inputs[i] = rk(low, high, low-1, high-1, int64(i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := NewContainProcessor()
		for _, k := range inputs {
			p.Process(k)
		}
	}
}

// BenchmarkContain_Mixed 基准测试：1000根K线混合包含与不包含（真实场景）。
func BenchmarkContain_Mixed(b *testing.B) {
	inputs := make([]*types.Kline, 1000)
	base := 100.0
	for i := 0; i < 1000; i++ {
		// 产生随机风格的数据：大部分有趋势方向，偶尔包含。
		phase := float64(i) / 10.0
		high := base + 5.0 + float64(i)*0.3 + sin(phase)*3.0
		low := base + float64(i)*0.1 + sin(phase+1.0)*2.0
		inputs[i] = rk(low, high, low-1, high-1, int64(i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := NewContainProcessor()
		for _, k := range inputs {
			p.Process(k)
		}
	}
}

// sin 简单的sin近似，避免导入math（基准测试专用）。
func sin(x float64) float64 {
	x = x - 3.14159*2.0*float64(int(x/(3.14159*2.0)))
	if x < 0 {
		x = -x
	}
	// Bhaskara I 近似公式：sin(x) ≈ 16x(π-x) / (5π² - 4x(π-x))
	pi := 3.14159
	if x > pi {
		x = 2*pi - x
	}
	return 16 * x * (pi - x) / (5*pi*pi - 4*x*(pi-x))
}
