package chanlun

import (
	"testing"

	"trade/internal/types"
)

// TestFractal_TopFractal 验证简单的顶分型识别。
func TestFractal_TopFractal(t *testing.T) {
	p := NewFractalProcessor()

	// 三个元素形成顶分型：中间元素高点最高。
	elems := []*types.ChanKline{
		{High: 15, Low: 8, OpenTime: 1},
		{High: 20, Low: 12, OpenTime: 2}, // 顶分型: high=20 > 15 且 20 > 17
		{High: 17, Low: 10, OpenTime: 3},
	}

	result := p.ProcessBatch(elems)

	if len(result) != 1 {
		t.Fatalf("期望1个分型，实际 %d", len(result))
	}
	if result[0].Type != types.FractalTop {
		t.Errorf("期望顶分型，实际 %v", result[0].Type)
	}
	if result[0].High != 20 {
		t.Errorf("期望high=20，实际 %.1f", result[0].High)
	}
	if result[0].Confirmed {
		t.Error("仅有3个元素时不应确认分型")
	}
}

// TestFractal_BottomFractal 验证简单的底分型识别。
func TestFractal_BottomFractal(t *testing.T) {
	p := NewFractalProcessor()

	elems := []*types.ChanKline{
		{High: 20, Low: 15, OpenTime: 1},
		{High: 15, Low: 8, OpenTime: 2}, // 底分型: low=8 < 15 且 8 < 12
		{High: 18, Low: 12, OpenTime: 3},
	}

	result := p.ProcessBatch(elems)

	if len(result) != 1 {
		t.Fatalf("期望1个分型，实际 %d", len(result))
	}
	if result[0].Type != types.FractalBottom {
		t.Errorf("期望底分型，实际 %v", result[0].Type)
	}
	if result[0].Low != 8 {
		t.Errorf("期望low=8，实际 %.1f", result[0].Low)
	}
}

// TestFractal_Confirmation 验证分型在第4个元素出现后被确认。
func TestFractal_Confirmation(t *testing.T) {
	p := NewFractalProcessor()

	elems := []*types.ChanKline{
		{High: 15, Low: 8, OpenTime: 1},
		{High: 20, Low: 12, OpenTime: 2}, // 顶分型候选
		{High: 17, Low: 10, OpenTime: 3},
	}

	// 3个元素时，分型尚未确认。
	p.ProcessBatch(elems)
	fractals := p.Fractals()
	if len(fractals) != 0 {
		t.Error("仅有3个元素时分型不应被确认")
	}

	// 添加第4个元素以确认。
	p.Process(&types.ChanKline{High: 16, Low: 9, OpenTime: 4})
	fractals = p.Fractals()
	if len(fractals) != 1 {
		t.Fatalf("期望1个已确认的分型，实际 %d", len(fractals))
	}
	if !fractals[0].Confirmed {
		t.Error("第4个元素后分型应被确认")
	}
}

// TestFractal_NoFractal 验证条件不满足时无分型识别。
func TestFractal_NoFractal(t *testing.T) {
	p := NewFractalProcessor()

	elems := []*types.ChanKline{
		{High: 15, Low: 8, OpenTime: 1},
		{High: 12, Low: 10, OpenTime: 2}, // 中间元素，但不是清晰的顶或底
		{High: 18, Low: 11, OpenTime: 3},
	}

	result := p.ProcessBatch(elems)
	if len(result) != 0 {
		t.Errorf("期望0个分型，实际 %d", len(result))
	}
}

// TestFractal_MultipleFractals 验证连续两个分型的识别。
func TestFractal_MultipleFractals(t *testing.T) {
	p := NewFractalProcessor()

	elems := []*types.ChanKline{
		{High: 15, Low: 8, OpenTime: 1},
		{High: 20, Low: 12, OpenTime: 2}, // 顶分型1 (索引1)
		{High: 17, Low: 10, OpenTime: 3},
		{High: 14, Low: 7, OpenTime: 4}, // 底分型 (索引3)
		{High: 16, Low: 9, OpenTime: 5},
	}

	result := p.ProcessBatch(elems)

	if len(result) != 2 {
		t.Fatalf("期望2个分型，实际 %d", len(result))
	}
	if result[0].Type != types.FractalTop {
		t.Errorf("分型0: 期望顶分型，实际 %v", result[0].Type)
	}
	if result[1].Type != types.FractalBottom {
		t.Errorf("分型1: 期望底分型，实际 %v", result[1].Type)
	}
}

// TestFractal_Reset 验证Reset()。
func TestFractal_Reset(t *testing.T) {
	p := NewFractalProcessor()
	p.ProcessBatch([]*types.ChanKline{
		{High: 15, Low: 8, OpenTime: 1},
		{High: 20, Low: 12, OpenTime: 2},
		{High: 17, Low: 10, OpenTime: 3},
		{High: 16, Low: 9, OpenTime: 4},
	})

	if len(p.Fractals()) != 1 {
		t.Fatal("重置前期望1个分型")
	}

	p.Reset()
	if len(p.AllFractals()) != 0 {
		t.Fatal("重置后期望0个分型")
	}
	if len(p.Fractals()) != 0 {
		t.Fatal("重置后期望0个已确认分型")
	}
}

// TestIsTopFractal 测试顶分型辅助函数。
func TestIsTopFractal(t *testing.T) {
	tests := []struct {
		name   string
		first  *types.ChanKline
		mid    *types.ChanKline
		last   *types.ChanKline
		result bool
	}{
		{"顶分型", ck(10, 5), ck(15, 8), ck(12, 6), true},
		{"非顶分型", ck(15, 5), ck(12, 8), ck(10, 6), false},
		{"高点相等", ck(10, 5), ck(10, 8), ck(12, 6), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTopFractal(tt.first, tt.mid, tt.last); got != tt.result {
				t.Errorf("IsTopFractal: 期望 %v, 实际 %v", tt.result, got)
			}
		})
	}
}

// TestIsBottomFractal 测试底分型辅助函数。
func TestIsBottomFractal(t *testing.T) {
	tests := []struct {
		name   string
		first  *types.ChanKline
		mid    *types.ChanKline
		last   *types.ChanKline
		result bool
	}{
		{"底分型", ck(15, 10), ck(12, 5), ck(14, 8), true},
		{"非底分型", ck(10, 5), ck(15, 8), ck(12, 10), false},
		{"低点相等", ck(10, 5), ck(12, 5), ck(14, 8), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsBottomFractal(tt.first, tt.mid, tt.last); got != tt.result {
				t.Errorf("IsBottomFractal: 期望 %v, 实际 %v", tt.result, got)
			}
		})
	}
}
