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

	// 验证每个元素的高/低值与输入匹配（无合并）。
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
		t.Fatalf("期望2个非包含元素（2个合并为1个），实际 %d", len(result))
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
		t.Fatalf("期望2个非包含元素，实际 %d", len(result))
	}

	// 向下包含合并后：high变为21, low保持15。
	if result[1].High != 21 {
		t.Errorf("期望合并后high=21，实际 %.1f", result[1].High)
	}
	if result[1].Low != 15 {
		t.Errorf("期望合并后low=15，实际 %.1f", result[1].Low)
	}
}

// TestContain_SequentialContainment 测试级联包含（连续3+根K线）。
func TestContain_SequentialContainment(t *testing.T) {
	p := NewContainProcessor()

	inputs := []*types.Kline{
		rk(10, 15, 8, 12, 1),  // K1: high=15, low=8
		rk(12, 19, 11, 17, 2), // K2: high=19, low=11 - 向上
		rk(14, 18, 12, 16, 3), // K3: 被K2包含 (18<=19, 12>=11)
		rk(13, 20, 13, 15, 4), // K4: 20 > 19 所以不被K2包含
	}

	var result []*types.ChanKline
	for _, k := range inputs {
		result = p.Process(k)
	}

	// K3合并入K2（向上包含），形成high=19, low=12。
	// K4的high=20 > 19, low=13 > 12 → 不被包含 → 新元素。
	if len(result) != 3 {
		t.Fatalf("期望3个非包含元素，实际 %d", len(result))
	}

	if result[1].High != 19 || result[1].Low != 12 {
		t.Errorf("K2合并后: 期望high=19 low=12, 实际high=%.1f low=%.1f", result[1].High, result[1].Low)
	}
	if result[1].MergedFrom != 2 {
		t.Errorf("K2期望MergedFrom=2（吸收了K3），实际 %d", result[1].MergedFrom)
	}
}

// TestContain_DirectionChangeAfterMerge 验证合并后方向被正确重新评估。
func TestContain_DirectionChangeAfterMerge(t *testing.T) {
	p := NewContainProcessor()

	inputs := []*types.Kline{
		rk(10, 15, 8, 12, 1),  // K1: high=15, low=8
		rk(12, 18, 11, 16, 2), // K2: high=18, low=11 - 向上
		rk(14, 14, 13, 13, 3), // K3: 被K2包含 (14<=18, 13>=11)
		rk(13, 17, 10, 14, 4), // K4: high=17, low=10... K2合并后high=18,low=13: 17<=18 && 10<13 → 不被包含（新方向）
	}

	var result []*types.ChanKline
	for _, k := range inputs {
		result = p.Process(k)
	}

	if len(result) < 3 {
		t.Fatalf("期望至少3个元素，实际 %d", len(result))
	}
}

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
		{"外部包含", ck(18, 2), ck(15, 5), true},
		{"不被包含（更高）", ck(18, 8), ck(15, 5), false},
		{"不被包含（更低）", ck(12, 2), ck(15, 5), false},
		{"完全相同", ck(10, 5), ck(10, 5), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsContained(tt.curr, tt.prev); got != tt.result {
				t.Errorf("IsContained(%s): 期望 %v, 实际 %v", tt.name, tt.result, got)
			}
		})
	}
}

// TestContain_RawKlineProperties 验证原始（未合并）属性被保留。
func TestContain_RawKlineProperties(t *testing.T) {
	p := NewContainProcessor()

	p.Process(rk(10, 15, 8, 12, 1))  // K1
	p.Process(rk(12, 18, 11, 16, 2)) // K2
	p.Process(rk(14, 17, 12, 15, 3)) // K3 - 被包含

	// 检查原始元素。
	for _, e := range p.elements {
		if e.OpenTime == 3 {
			if !e.Contained {
				t.Error("K3应被标记为包含")
			}
			if e.RawHigh != 17 || e.RawLow != 12 {
				t.Errorf("原始属性未被保留: 期望high=17 low=12, 实际high=%.1f low=%.1f",
					e.RawHigh, e.RawLow)
			}
		}
	}
}

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
