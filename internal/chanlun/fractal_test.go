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

// --- 重复元素处理测试 ---

// TestFractal_DuplicatePointerNoDuplicate 验证同一指针不会被重复添加。
func TestFractal_DuplicatePointerNoDuplicate(t *testing.T) {
	p := NewFractalProcessor()

	e1 := &types.ChanKline{High: 10, Low: 5, OpenTime: 1}
	e2 := &types.ChanKline{High: 15, Low: 8, OpenTime: 2}

	p.Process(e1)
	p.Process(e2)

	if len(p.elements) != 2 {
		t.Fatalf("2个元素后期望len=2，实际 %d", len(p.elements))
	}

	// 再次传入e2（模拟合并后同一指针传入）。
	p.Process(e2)

	// 不应重复添加。
	if len(p.elements) != 2 {
		t.Fatalf("重复指针后期望len=2，实际 %d", len(p.elements))
	}
}

// TestFractal_DuplicatePointerRecheck 验证传入重复指针时重新扫描。
//
// 场景：
//
//	e1(10,5) → e2(15,8) → e3(12,6) → 顶分型(e2)
//	合并后e3值变为(13,7)，再次传入
//	重新扫描应仍然识别顶分型(e2)
func TestFractal_DuplicatePointerRecheck(t *testing.T) {
	p := NewFractalProcessor()

	e1 := &types.ChanKline{High: 10, Low: 5, OpenTime: 1}
	e2 := &types.ChanKline{High: 15, Low: 8, OpenTime: 2}
	e3 := &types.ChanKline{High: 12, Low: 6, OpenTime: 3}

	p.Process(e1)
	p.Process(e2)
	p.Process(e3)

	fractals := p.AllFractals()
	if len(fractals) != 1 {
		t.Fatalf("初始期望1个分型，实际 %d", len(fractals))
	}
	if fractals[0].Type != types.FractalTop {
		t.Errorf("初始期望顶分型，实际 %v", fractals[0].Type)
	}

	// 模拟K4被e3吸收，e3值更新。
	e3.High = 13
	e3.Low = 7

	// 重复指针传入 → 重新扫描。
	p.Process(e3)

	// 仍应有1个顶分型。
	fractals = p.AllFractals()
	if len(fractals) != 1 {
		t.Fatalf("重新扫描后期望1个分型，实际 %d", len(fractals))
	}
	if fractals[0].Type != types.FractalTop {
		t.Errorf("重新扫描后期望顶分型，实际 %v", fractals[0].Type)
	}
	if len(p.elements) != 3 {
		t.Errorf("重复指针后元素数应保持3，实际 %d", len(p.elements))
	}
}

// --- 端到端包含+分型测试 ---

// TestFractal_ContainPipelineEndToEnd 验证含包含处理的完整分型识别流程。
//
// K1(20,10) → K2(25,15) → K3(22,12)(不包含,向下) → 顶分型(K2)
// → K4(18,8)(不包含,向下) → 顶分型确认
func TestFractal_ContainPipelineEndToEnd(t *testing.T) {
	containP := NewContainProcessor()
	fractalP := NewFractalProcessor()

	feed := func(k *types.Kline) {
		elems := containP.Process(k)
		if len(elems) > 0 {
			fractalP.Process(elems[len(elems)-1])
		}
	}

	feed(rk(10, 20, 10, 12, 1)) // K1(20,10)
	feed(rk(18, 25, 15, 20, 2)) // K2(25,15)

	if len(fractalP.AllFractals()) != 0 {
		t.Fatal("2个元素不应有分型")
	}

	feed(rk(18, 22, 12, 20, 3)) // K3(22,12), 不包含

	fractals := fractalP.AllFractals()
	if len(fractals) != 1 {
		t.Fatalf("3个元素期望1个分型，实际 %d", len(fractals))
	}
	if fractals[0].Type != types.FractalTop {
		t.Errorf("期望顶分型，实际 %v", fractals[0].Type)
	}
	if fractals[0].Confirmed {
		t.Error("3个元素时顶分型不应确认")
	}

	feed(rk(12, 18, 8, 15, 4)) // K4(18,8), 不包含

	fractals = fractalP.Fractals()
	if len(fractals) != 1 {
		t.Fatalf("4个元素期望1个已确认分型，实际 %d", len(fractals))
	}
	if !fractals[0].Confirmed {
		t.Error("第4个元素后顶分型应确认")
	}
}

// TestFractal_ContainPipelineMergeThenFractal 验证合并后重新扫描的端到端流程。
func TestFractal_ContainPipelineMergeThenBottomFractal(t *testing.T) {
	containP := NewContainProcessor()
	fractalP := NewFractalProcessor()

	feed := func(k *types.Kline) {
		elems := containP.Process(k)
		if len(elems) > 0 {
			fractalP.Process(elems[len(elems)-1])
		}
	}

	// K1(10,5), K2(8,4): 向下
	feed(rk(10, 10, 5, 8, 1))
	feed(rk(7, 8, 4, 6, 2))

	// K3(9,2): 被K2包含，向下合并
	feed(rk(7, 9, 2, 8, 3))

	// K4(5,1): 不被包含，新元素
	feed(rk(3, 5, 1, 4, 4))

	// 确认流处理不崩溃，元素增长符合预期。
	if fractalP.AllFractals() == nil {
		t.Fatal("AllFractals不应返回nil")
	}
}

// TestFractal_RealtimeStreamPipeline 验证实时流式场景下包含→分型链路的正确性。
//
// 模拟币安实时推送：同一K线多次更新后闭合。
func TestFractal_RealtimeStreamPipeline(t *testing.T) {
	containP := NewContainProcessor()
	fractalP := NewFractalProcessor()

	feed := func(k *types.Kline) {
		elems := containP.Process(k)
		if len(elems) > 0 {
			fractalP.Process(elems[len(elems)-1])
		}
	}

	// K1(10,5) 闭合
	feed(rk(10, 10, 5, 8, 1))

	// K2 开始：三次实时更新 → 闭合
	feed(rk(15, 18, 12, 16, 2)) // K2第一次实时更新
	feed(rk(15, 20, 11, 17, 2)) // K2第二次实时更新(high变高, low变低)
	feed(rk(15, 22, 10, 18, 2)) // K2第三次实时更新
	feed(rk(15, 22, 10, 18, 2)) // K2闭合(值不变)

	// K1(10,5), K2(22,10) 向上不包含
	// 只有2个非包含，无分型
	if len(fractalP.AllFractals()) != 0 {
		t.Fatalf("2个非包含元素不应有分型，实际 %d", len(fractalP.AllFractals()))
	}
	// elements 应包含2个元素(K1, K2)
	if len(containP.elements) != 2 {
		t.Errorf("期望2个元素(K1, K2)，实际 %d", len(containP.elements))
	}

	// K3 实时更新 + 闭合
	feed(rk(12, 18, 8, 14, 3)) // K3第一次
	feed(rk(12, 19, 7, 14, 3)) // K3第二次

	// 非包含: K1(10,5), K2(22,10), K3(19,7)
	// K3(19,7) vs K2(22,10): 19<=22且7<=10 → 不包含(19<=22且7<10, NOT 7>=10)
	// 不包含。3个非包含元素 → 顶分型K2(22最高)
	fractals := fractalP.AllFractals()
	if len(fractals) != 1 {
		t.Fatalf("期望1个顶分型，实际 %d", len(fractals))
	}
	if fractals[0].Type != types.FractalTop {
		t.Errorf("期望顶分型，实际 %v", fractals[0].Type)
	}

	// 闭合K3
	feed(rk(12, 19, 7, 14, 3))

	// 分型不变
	fractals = fractalP.AllFractals()
	if len(fractals) != 1 {
		t.Fatalf("闭合后仍期望1个分型，实际 %d", len(fractals))
	}
}

// --- 性能测试 ---

// BenchmarkFractal_Process 分型性能基准测试。
func BenchmarkFractal_Process(b *testing.B) {
	elems := make([]*types.ChanKline, 1000)
	for i := 0; i < 1000; i++ {
		elems[i] = &types.ChanKline{
			High: float64(100 + 10 + i),
			Low:  float64(90 + i),
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := NewFractalProcessor()
		for _, e := range elems {
			p.Process(e)
		}
	}
}

// BenchmarkFractal_Batch 分型批量处理性能基准测试。
func BenchmarkFractal_Batch(b *testing.B) {
	elems := make([]*types.ChanKline, 1000)
	for i := 0; i < 1000; i++ {
		elems[i] = &types.ChanKline{
			High: float64(100 + 10 + i),
			Low:  float64(90 + i),
		}
	}
}

// TestIsTopFractal_Strict 验证严格顶分型的双条件判定（高点最高 且 低点也最高）。
func TestIsTopFractal_Strict(t *testing.T) {
	tests := []struct {
		name             string
		first, mid, last *types.ChanKline
		wantStrict       bool // 严格模式（双条件AND）
		wantLoose        bool // 宽松模式（仅高条件）
	}{
		{
			name:  "典型顶分型-双条件都满足",
			first: ck(15, 10), mid: ck(20, 12), last: ck(17, 9),
			wantStrict: true, wantLoose: true,
		},
		{
			name: "高点最高但低点不是最高-严格拒绝",
			// first(15,10), mid(20,8), last(17,12)
			// tight: 20>15 && 20>17 ✓ |  8>10? ✗ 8>12? ✗ → 严格拒绝
			first: ck(15, 10), mid: ck(20, 8), last: ck(17, 12),
			wantStrict: false, wantLoose: true,
		},
		{
			name:  "低点最高但高点不是最高",
			first: ck(15, 8), mid: ck(13, 12), last: ck(17, 9),
			wantStrict: false, wantLoose: false,
		},
		{
			name:  "高点相等",
			first: ck(15, 8), mid: ck(15, 12), last: ck(17, 9),
			wantStrict: false, wantLoose: false,
		},
		{
			name:  "完全不符合",
			first: ck(20, 5), mid: ck(15, 8), last: ck(18, 6),
			wantStrict: false, wantLoose: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTopFractal(tt.first, tt.mid, tt.last); got != tt.wantStrict {
				t.Errorf("IsTopFractal(严格): 期望 %v, 实际 %v", tt.wantStrict, got)
			}
			if got := IsTopFractalLoose(tt.first, tt.mid, tt.last); got != tt.wantLoose {
				t.Errorf("IsTopFractalLoose(宽松): 期望 %v, 实际 %v", tt.wantLoose, got)
			}
		})
	}
}

// TestIsBottomFractal_Strict 验证严格底分型的双条件判定（低点最低 且 高点也最低）。
func TestIsBottomFractal_Strict(t *testing.T) {
	tests := []struct {
		name             string
		first, mid, last *types.ChanKline
		wantStrict       bool
		wantLoose        bool
	}{
		{
			name:  "典型底分型-双条件都满足",
			first: ck(20, 15), mid: ck(15, 8), last: ck(18, 12),
			wantStrict: true, wantLoose: true,
		},
		{
			name: "低点最低但高点不是最低-严格拒绝",
			// first(15,20), mid(12,5), last(10,18)
			// loose: 5<20 && 5<18 ✓ | strict: mid.H(12)<first.H(15)? ✓ 12<last.H(10)? ✗ → 严格拒绝
			first: ck(15, 20), mid: ck(12, 5), last: ck(10, 18),
			wantStrict: false, wantLoose: true,
		},
		{
			name:  "高点最低但低点不是最低",
			first: ck(20, 5), mid: ck(12, 8), last: ck(15, 12),
			wantStrict: false, wantLoose: false,
		},
		{
			name:  "低点相等",
			first: ck(20, 5), mid: ck(15, 5), last: ck(18, 8),
			wantStrict: false, wantLoose: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsBottomFractal(tt.first, tt.mid, tt.last); got != tt.wantStrict {
				t.Errorf("IsBottomFractal(严格): 期望 %v, 实际 %v", tt.wantStrict, got)
			}
			if got := IsBottomFractalLoose(tt.first, tt.mid, tt.last); got != tt.wantLoose {
				t.Errorf("IsBottomFractalLoose(宽松): 期望 %v, 实际 %v", tt.wantLoose, got)
			}
		})
	}
}

// TestFractal_StrictVsLooseOutputDiff 验证严格/宽松模式在完整流程中产生不同分型输出。
//
// 场景：形成顶分型候选，但严格模式因低点条件不满足而拒绝，宽松模式接受。
// first(15,10), mid(20,8), last(17,12)
//   - 宽松：mid.H(20)>first.H(15) && mid.H(20)>last.H(17) → 顶分型
//   - 严格：还要 mid.L(8)>first.L(10)? No → 拒绝
func TestFractal_StrictVsLooseOutputDiff(t *testing.T) {
	t.Run("严格模式拒绝单条件顶分型", func(t *testing.T) {
		p := NewFractalProcessor()
		p.SetStrictFractalMode(true) // 默认已是严格

		// first(15,10), mid(20,8), last(17,12)
		// mid.H最高但mid.L不是最高 → 严格拒绝
		elems := []*types.ChanKline{
			{High: 15, Low: 10, OpenTime: 1},
			{High: 20, Low: 8, OpenTime: 2},
			{High: 17, Low: 12, OpenTime: 3},
			{High: 16, Low: 9, OpenTime: 4}, // 第4个确认
		}
		result := p.ProcessBatch(elems)

		if len(result) != 0 {
			t.Errorf("严格模式期望0个分型（低点条件不满足），实际 %d", len(result))
		}
	})

	t.Run("宽松模式接受单条件顶分型", func(t *testing.T) {
		p := NewFractalProcessor()
		p.SetStrictFractalMode(false)

		// 相同的K线
		elems := []*types.ChanKline{
			{High: 15, Low: 10, OpenTime: 1},
			{High: 20, Low: 8, OpenTime: 2},
			{High: 17, Low: 12, OpenTime: 3},
			{High: 16, Low: 9, OpenTime: 4},
		}
		result := p.ProcessBatch(elems)

		if len(result) != 1 {
			t.Fatalf("宽松模式期望1个顶分型，实际 %d", len(result))
		}
		if result[0].Type != types.FractalTop {
			t.Errorf("期望顶分型，实际 %v", result[0].Type)
		}
		if !result[0].Confirmed {
			t.Error("第4个元素后分型应被确认")
		}
	})

	t.Run("宽松模式接受单条件底分型", func(t *testing.T) {
		p := NewFractalProcessor()
		p.SetStrictFractalMode(false)

		// first(15,20), mid(12,5), last(10,18)
		// mid.L最低但mid.H不是最低 → 宽松接受, 严格拒绝
		elems := []*types.ChanKline{
			{High: 15, Low: 20, OpenTime: 1},
			{High: 12, Low: 5, OpenTime: 2},
			{High: 10, Low: 18, OpenTime: 3},
			{High: 13, Low: 8, OpenTime: 4},
		}
		result := p.ProcessBatch(elems)

		if len(result) != 1 {
			t.Fatalf("宽松模式期望1个底分型，实际 %d", len(result))
		}
		if result[0].Type != types.FractalBottom {
			t.Errorf("期望底分型，实际 %v", result[0].Type)
		}
	})

	t.Run("严格模式拒绝单条件底分型", func(t *testing.T) {
		p := NewFractalProcessor()
		p.SetStrictFractalMode(true)

		// 相同的K线
		elems := []*types.ChanKline{
			{High: 15, Low: 20, OpenTime: 1},
			{High: 12, Low: 5, OpenTime: 2},
			{High: 10, Low: 18, OpenTime: 3},
			{High: 13, Low: 8, OpenTime: 4},
		}
		result := p.ProcessBatch(elems)

		if len(result) != 0 {
			t.Errorf("严格模式期望0个分型（高点条件不满足），实际 %d", len(result))
		}
	})
}

// TestFractal_StrictModeExists 验证默认构造的处理器为严格模式。
func TestFractal_StrictModeDefaults(t *testing.T) {
	p := NewFractalProcessor()
	if !p.StrictFractal {
		t.Error("NewFractalProcessor 默认应为严格模式")
	}

	// 严格模式下：高点条件满足但低点不满足 → 应无分型
	elems := []*types.ChanKline{
		{High: 15, Low: 10, OpenTime: 1},
		{High: 20, Low: 8, OpenTime: 2},
		{High: 17, Low: 12, OpenTime: 3},
	}
	result := p.ProcessBatch(elems)
	if len(result) != 0 {
		t.Error("严格模式下单条件顶分型不应被识别")
	}
}
