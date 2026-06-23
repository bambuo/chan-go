// Package signal 完整端到端测试：原始 K 线 → 缠论结构 → 买卖点信号。
//
// 验证链路（PRD §18）：
//
//	原始 K 线 → Pipeline(M2) → M3Bridge → Structure Tree(M3)
//	  → M4 级别递归 → SignalEngine(M5) → ResonanceEngine(M6)
//
// 测试覆盖：
//   - M2 Pipeline：K线包含 → 分型 → 笔 → 线段 → 中枢 → 走势 → 背驰
//   - M3Bridge：结构提交、lineage 注册
//   - M3 结构树：版本化管理、双轨状态
//   - M5 信号引擎：信号创建/状态机、字段完整性
//   - M6 共振引擎：G-2/G-1/A3 判定
//
// 说明：
//   - L1 级别锯齿波只能产出 1 个中枢（盘整），无法产出 ≥2 个不重叠中枢（趋势）
//   - 因此 BUY_1 等趋势型信号无法由 L1 原始 K 线直接触发，需要 M4 从 L1 走势
//     类型递归构建 L2+ 级别
//   - 信号识别逻辑的精确验证由 resonance/e2e_test.go 通过构造 SignalInput 完成
//   - 本测试聚焦于"链路通畅 + 结构正确 + 字段完整"，不过度断言信号数量
//
// 本测试不依赖外部 Redis，所有组件在进程内完成。
package signal

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"trade/internal/chanlun"
	"trade/internal/eventbus"
	"trade/internal/levels"
	"trade/internal/resonance"
	"trade/internal/structure"
	"trade/internal/types"

	"github.com/shopspring/decimal"
)

// e2eKlines 构造原始 K 线数据集。
//
// 模式：大锯齿 zigzag，形成清晰的顶/底交替分型。
// 每根 K 线 {high, low} 足够开敞以避免意外包含合并改变分型模式。
//
// 设计约束（对应缠论算法要求）：
//   - 分型：连续 3 根非包含 K 线，中间元素为极值
//   - 笔：严格模式跨度（span）≥ 4
//   - 中枢：≥ 3 笔重叠区间
//   - 走势：同向互不重叠的中枢 ≥ 2 → 趋势
//
// readBTCUSDTKlines 从 BTCUSDT_1m.csv 读取最近 N 个月的 1m K 线数据。
// 使用单遍扫描 + 滑动窗口，只保留尾部所需行，避免两遍读盘。
func readBTCUSDTKlines(symbol string, months int) ([]*types.Kline, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return nil, fmt.Errorf("runtime.Caller 获取路径失败")
	}
	csvPath := filepath.Join(filepath.Dir(filename), "../../docs/dataset/BTCUSDT_1m.csv")

	f, err := os.Open(csvPath)
	if err != nil {
		return nil, fmt.Errorf("打开 CSV 文件失败: %w", err)
	}
	defer f.Close()

	rowsNeeded := 30 * 24 * 60 * months // 一个月 ≈ 43200 行

	// 单遍扫描：环形缓冲区只保留最后 rowsNeeded 行，避免滑动窗口的 O(N^2) 拷贝
	ring := make([]string, rowsNeeded)
	pos := 0
	total := 0

	scanner := bufio.NewScanner(f)
	// 分配足够大的 buffer（单行 csv 可能较长）
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		ring[pos%rowsNeeded] = scanner.Text()
		pos++
		total++
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("扫描 CSV 失败: %w", err)
	}

	if total == 0 {
		return nil, fmt.Errorf("CSV 文件中无数据")
	}

	// 将环形缓冲区转为有序切片
	var lines []string
	if total < rowsNeeded {
		lines = ring[:total]
	} else {
		start := pos % rowsNeeded
		lines = make([]string, rowsNeeded)
		copy(lines, ring[start:])
		copy(lines[rowsNeeded-start:], ring[:start])
	}

	// 解析：用最近时间为基线推导时间戳（保持 K 线顺序）
	baseTS := time.Now().UnixMilli() - int64(len(lines))*60000
	klines := make([]*types.Kline, 0, len(lines))
	for i, text := range lines {
		rec := strings.Split(text, ",")
		if len(rec) < 6 {
			continue
		}

		open, _ := strconv.ParseFloat(strings.TrimSpace(rec[1]), 64)
		high, _ := strconv.ParseFloat(strings.TrimSpace(rec[2]), 64)
		low, _ := strconv.ParseFloat(strings.TrimSpace(rec[3]), 64)
		close_, _ := strconv.ParseFloat(strings.TrimSpace(rec[4]), 64)
		vol, _ := strconv.ParseFloat(strings.TrimSpace(rec[5]), 64)

		ot := baseTS + int64(i)*60000
		klines = append(klines, &types.Kline{
			Symbol:     symbol,
			Open:       decimal.NewFromFloat(open),
			High:       decimal.NewFromFloat(high),
			Low:        decimal.NewFromFloat(low),
			Close:      decimal.NewFromFloat(close_),
			BaseVolume: decimal.NewFromFloat(vol),
			OpenTime:   ot,
			CloseTime:  ot + 59999,
			IsClosed:   true,
		})
	}

	return klines, nil
}

// e2eKlines 构造锯齿波 K 线数据集，用于合成数据测试。
func e2eKlines(symbol string) []*types.Kline {
	type kl struct{ h, l float64 }
	els := []kl{
		// === 周期 1: 向上盘整（5 笔，1 中枢）===

		// 笔 0 (上): span=6, BOTTOM@2 → TOP@8
		{51, 39},
		{46, 34},
		{42, 28}, // BOTTOM
		{45, 31},
		{48, 34},
		{52, 38},
		{56, 42},
		{58, 44},
		{60, 46}, // TOP
		{57, 43},
		{54, 40},

		// 笔 1 (下): span=6, TOP@8 → BOTTOM@14
		{50, 36},
		{46, 32},
		{42, 28},
		{40, 26}, // BOTTOM
		{43, 29},
		{46, 32},

		// 笔 2 (上): span=6, BOTTOM@14 → TOP@20
		{50, 36},
		{54, 40},
		{58, 44},
		{62, 48}, // TOP
		{59, 45},
		{55, 41},

		// 笔 3 (下): span=6, TOP@20 → BOTTOM@26
		{50, 37},
		{46, 33},
		{42, 29},
		{40, 25}, // BOTTOM
		{43, 28},
		{46, 32},

		// 笔 4 (上): span=6, BOTTOM@26 → TOP@32
		{50, 36},
		{54, 40},
		{58, 44},
		{62, 48}, // TOP
		{59, 45},
		{55, 41},

		// === 过渡到周期 2（延续最后一个 TOP 的回落）===
		{51, 37},
		{47, 33},
		{43, 29},

		// === 周期 2: 向下盘整（笔 5-7，价格低位运行）===

		// 笔 5 (下): span=6, TOP@32 → BOTTOM@38
		{39, 23}, // BOTTOM
		{42, 26},
		{45, 29},

		// 笔 6 (上): span=6, BOTTOM@38 → TOP@44
		{48, 32},
		{51, 35},
		{54, 38},
		{56, 42}, // TOP
		{53, 39},
		{50, 36},
		{47, 33},

		// 笔 7 (下): span=6, TOP@44 → BOTTOM@50
		{43, 29},
		{39, 25},
		{35, 21}, // BOTTOM
		{38, 24},
		{41, 27},
		{44, 30},

		// === 过渡 ===
		{47, 33},
		{50, 36},

		// === 周期 3: 向上盘整（笔 8-10，价格回升）===

		// 笔 8 (上): span=6, BOTTOM@50 → TOP@56
		{53, 39},
		{55, 43}, // TOP
		{52, 40},
		{49, 37},
		{46, 34},

		// 笔 9 (下): span=6, TOP@56 → BOTTOM@62
		{42, 30},
		{38, 26},
		{34, 22}, // BOTTOM
		{37, 25},
		{40, 28},
		{43, 31},

		// 笔 10 (上): span=6, BOTTOM@62 → TOP@68
		{46, 34},
		{49, 37},
		{52, 40},
		{54, 44}, // TOP
		{51, 41},
		{48, 38},
		{45, 35},

		// 尾部确认最后一个分型
		{41, 31},
		{37, 27},
		{33, 23},
	}

	klines := make([]*types.Kline, len(els))
	baseTS := time.Now().UnixMilli()
	for i, p := range els {
		o := p.l + (p.h-p.l)*0.3
		c := p.l + (p.h-p.l)*0.7
		klines[i] = &types.Kline{
			Symbol:     symbol,
			Open:       decimal.NewFromFloat(o),
			High:       decimal.NewFromFloat(p.h),
			Low:        decimal.NewFromFloat(p.l),
			Close:      decimal.NewFromFloat(c),
			BaseVolume: decimal.NewFromFloat(100),
			OpenTime:   baseTS + int64(i)*60000,
			CloseTime:  baseTS + int64(i)*60000 + 59999,
			IsClosed:   true,
		}
	}
	return klines
}

// TestE2E_FullFlow_KlineToSignal 验证完整端到端链路。
func TestE2E_FullFlow_KlineToSignal(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := structure.New(bus)
	pipeline := chanlun.NewPipeline()
	bridge := chanlun.NewM3Bridge(pipeline, tree)
	sigEngine := New(bus)
	resEngine := resonance.New(bus, tree)
	levelBldr := levels.New(bus, tree)

	bridge.WithSignalSink(sigEngine)

	klines := e2eKlines("E2E_FULL")
	t.Logf("输入 K 线: %d 根", len(klines))

	// 订阅事件
	var signalEvents []types.SignalEventPayload
	var resonanceEvents []types.ResonanceEventPayload
	var versionEvents []types.StructureVersionPayload

	s1 := bus.Subscribe(types.EventSignalCreated, func(evt types.Event) {
		if p, ok := evt.Payload.(types.SignalEventPayload); ok {
			signalEvents = append(signalEvents, p)
		}
	})
	s2 := bus.Subscribe(types.EventSignalStateChanged, func(evt types.Event) {
		if p, ok := evt.Payload.(types.SignalEventPayload); ok {
			signalEvents = append(signalEvents, p)
		}
	})
	s3 := bus.Subscribe(types.EventResonanceTriggered, func(evt types.Event) {
		if p, ok := evt.Payload.(types.ResonanceEventPayload); ok {
			resonanceEvents = append(resonanceEvents, p)
		}
	})
	s4 := bus.Subscribe(types.EventStructureVersionChanged, func(evt types.Event) {
		if p, ok := evt.Payload.(types.StructureVersionPayload); ok {
			versionEvents = append(versionEvents, p)
		}
	})
	defer func() {
		bus.Unsubscribe(types.EventSignalCreated, s1)
		bus.Unsubscribe(types.EventSignalStateChanged, s2)
		bus.Unsubscribe(types.EventResonanceTriggered, s3)
		bus.Unsubscribe(types.EventStructureVersionChanged, s4)
	}()

	// 逐根处理 K 线
	for _, k := range klines {
		bridge.OnKline(k)
	}

	time.Sleep(50 * time.Millisecond)

	// ====== M2 Pipeline 状态 ======
	pState := pipeline.GetState("E2E_FULL")
	t.Logf("=== M2 Pipeline ===")
	t.Logf("元素=%d 分型=%d 笔=%d 线段=%d 中枢=%d 走势=%d 背驰=%d",
		len(pState.AllElements), len(pState.AllFractals),
		len(pState.Strokes), len(pState.Segments),
		len(pState.PivotZones), len(pState.TrendPatterns),
		len(pState.Divergences))

	for i, s := range pState.Strokes {
		t.Logf("  笔[%d]: %s [%.0f→%.0f]", i, s.Direction, s.StartPrice, s.EndPrice)
	}

	// ====== 结构断言（PRD §7）======

	// 1. Pipeline 应产出足够的结构
	if len(pState.AllElements) == 0 {
		t.Error("无非包含元素产出")
	}
	if len(pState.AllFractals) < 8 {
		t.Errorf("分型 < 8, 实际 %d", len(pState.AllFractals))
	}
	if len(pState.Strokes) < 8 {
		t.Errorf("笔 < 8, 实际 %d", len(pState.Strokes))
	}
	if len(pState.TrendPatterns) < 1 {
		t.Error("无走势类型产出")
	}

	// ====== M3 结构树 ======
	state := tree.GetCurrentState("E2E_FULL", types.LevelL1)
	if state == nil {
		t.Fatal("M3 中无 L1 状态")
	}
	t.Logf("\n=== M3 结构树 L1 ===")
	t.Logf("笔=%d 中枢=%d 走势=%d 双轨同步=%v 版本数=%d",
		len(state.Provisional.Strokes),
		len(state.Provisional.PivotZones),
		len(state.Provisional.TrendPatterns),
		state.InSync,
		len(versionEvents))

	// lineage 追踪（PRD §10.5）
	if len(pState.Strokes) > 0 {
		lineageID := "L_E2E_FULL_bi_0"
		if eid, ok := tree.ResolveLineage(lineageID); ok {
			t.Logf("lineage: %s → %s", lineageID, eid)
		}
	}

	// 结构树版本历史
	versions := tree.GetVersionHistory("E2E_FULL", types.LevelL1)
	if len(versions) > 0 {
		t.Logf("L1 版本数: %d", len(versions))
	}

	// ====== M4 级别递归 ======
	t.Logf("\n=== M4 级别递归 ===")
	for _, lvl := range []types.Level{types.LevelL1, types.LevelL2, types.LevelL3, types.LevelL4} {
		lState := tree.GetCurrentState("E2E_FULL", lvl)
		if lState == nil {
			t.Logf("  %s: (无数据)", lvl)
			continue
		}
		t.Logf("  %s: 笔=%d 中枢=%d 走势=%d 同步=%v",
			lvl, len(lState.Provisional.Strokes),
			len(lState.Provisional.PivotZones),
			len(lState.Provisional.TrendPatterns),
			lState.InSync)

		// 详细日志：各级别的笔信息（高级别为 L1 盘整升级而来）
		for i, st := range lState.Provisional.Strokes {
			t.Logf("    %s 笔[%d]: %s [%.0f→%.0f]", lvl, i, st.Direction, st.StartPrice, st.EndPrice)
		}

		if hint := levelBldr.GetTimeHint("E2E_FULL", lvl); hint != nil {
			t.Logf("     timeHint: avg=%.0fs p10=%.0fs p90=%.0fs N=%d",
				hint.AvgDurationSec, hint.P10DurationSec,
				hint.P90DurationSec, hint.SampleCount)
		}
	}

	// ====== M5 信号引擎 ======
	activeSignals := sigEngine.GetActiveSignals("E2E_FULL")
	t.Logf("\n=== M5 信号引擎 ===")
	t.Logf("信号事件: %d (created+stateChanged) 共振事件: %d",
		len(signalEvents), len(resonanceEvents))
	t.Logf("活跃信号: %d", len(activeSignals))

	for _, s := range activeSignals {
		t.Logf("  [%s] %s %s price=%.2f conf=%.4f strength=%.4f res=%s",
			s.SignalID, s.Type, s.State, s.Price,
			s.Confidence, s.Strength, s.Resonance.Kind)
	}

	// ====== 信号字段完整性断言（PRD §8）======
	for _, s := range activeSignals {
		if s.SignalID == "" {
			t.Error("SignalID 为空")
		}
		if s.Type == "" {
			t.Error("Type 为空")
		}
		if s.State == "" {
			t.Error("State 为空")
		}
		if s.Confidence <= 0 || s.Confidence > 1.0 {
			t.Errorf("Confidence 超范围: %.4f", s.Confidence)
		}
		if s.Strength < 0 || s.Strength > 1.0 {
			t.Errorf("Strength 超范围: %.4f", s.Strength)
		}
		if s.Anchor.Kind == "" {
			t.Error("Anchor.Kind 为空")
		}
		if s.Targets.InvalidationPrice == 0 {
			t.Error("InvalidationPrice 为 0")
		}
		if s.Evidence.TrendDirection == "" {
			t.Error("Evidence.TrendDirection 为空")
		}
		if s.Resonance.Kind == "" {
			t.Error("Resonance.Kind 为空")
		}
	}

	// ====== 共振事件完整性 ======
	for _, e := range resonanceEvents {
		if e.Signal == nil {
			t.Error("共振事件 Signal 不能为空")
		}
		if e.Resonance.Kind == "" {
			t.Error("共振事件 Resonance.Kind 不能为空")
		}
	}

	// ====== 版本事件完整性 ======
	for _, e := range versionEvents {
		if e.NewVersion.VersionID == "" {
			t.Error("版本事件 VersionID 不能为空")
		}
		if e.Level == 0 {
			t.Error("版本事件 Level 为 0")
		}
	}

	resEngine.Stop()
	levelBldr.Stop()
}

// TestE2E_FullFlow_RealKline 用 BTCUSDT 真实 1 个月 1m K 线验证完整端到端链路。
//
// 与 TestE2E_FullFlow_KlineToSignal 的区别：
//   - 使用真实市场数据而非合成锯齿波
//   - 断言更宽松（真实数据噪声大）
//   - 侧重于 Pipeline 不崩溃 + 输出结构多样性
func TestE2E_FullFlow_RealKline(t *testing.T) {
	bus := eventbus.NewGeneric()
	tree := structure.New(bus)
	pipeline := chanlun.NewPipeline()
	bridge := chanlun.NewM3Bridge(pipeline, tree)
	sigEngine := New(bus)
	resEngine := resonance.New(bus, tree)
	levelBldr := levels.New(bus, tree)

	bridge.WithSignalSink(sigEngine)

	klines, err := readBTCUSDTKlines("BTCUSDT", 1)
	if err != nil {
		t.Fatalf("加载 BTCUSDT K 线失败: %v", err)
	}
	t.Logf("输入 BTCUSDT 1m K 线: %d 根（最近 1 个月）", len(klines))

	// 订阅事件
	var signalEvents []types.SignalEventPayload
	var resonanceEvents []types.ResonanceEventPayload
	var versionEvents []types.StructureVersionPayload

	s1 := bus.Subscribe(types.EventSignalCreated, func(evt types.Event) {
		if p, ok := evt.Payload.(types.SignalEventPayload); ok {
			signalEvents = append(signalEvents, p)
		}
	})
	s2 := bus.Subscribe(types.EventSignalStateChanged, func(evt types.Event) {
		if p, ok := evt.Payload.(types.SignalEventPayload); ok {
			signalEvents = append(signalEvents, p)
		}
	})
	s3 := bus.Subscribe(types.EventResonanceTriggered, func(evt types.Event) {
		if p, ok := evt.Payload.(types.ResonanceEventPayload); ok {
			resonanceEvents = append(resonanceEvents, p)
		}
	})
	s4 := bus.Subscribe(types.EventStructureVersionChanged, func(evt types.Event) {
		if p, ok := evt.Payload.(types.StructureVersionPayload); ok {
			versionEvents = append(versionEvents, p)
		}
	})
	defer func() {
		bus.Unsubscribe(types.EventSignalCreated, s1)
		bus.Unsubscribe(types.EventSignalStateChanged, s2)
		bus.Unsubscribe(types.EventResonanceTriggered, s3)
		bus.Unsubscribe(types.EventStructureVersionChanged, s4)
	}()

	// 逐根处理 K 线
	t.Logf("开始逐根处理 %d 根 K 线...", len(klines))
	for _, k := range klines {
		bridge.OnKline(k)
	}

	time.Sleep(100 * time.Millisecond)

	// ====== M2 Pipeline 状态 ======
	pState := pipeline.GetState("BTCUSDT")
	t.Logf("=== M2 Pipeline ===")
	t.Logf("元素=%d 分型=%d 笔=%d 线段=%d 中枢=%d 走势=%d 背驰=%d",
		len(pState.AllElements), len(pState.AllFractals),
		len(pState.Strokes), len(pState.Segments),
		len(pState.PivotZones), len(pState.TrendPatterns),
		len(pState.Divergences))

	for i, s := range pState.Strokes {
		t.Logf("  笔[%d]: %s [%.0f→%.0f]", i, s.Direction, s.StartPrice, s.EndPrice)
	}

	// ====== 结构快速断言 ======
	if len(pState.AllElements) == 0 {
		t.Error("无非包含元素产出（可能 CSV 加载失败）")
	}
	if len(pState.AllFractals) < 2 {
		t.Logf("分型仅 %d 个（真实数据可能较稀少）", len(pState.AllFractals))
	}

	// ====== M3 结构树 ======
	state := tree.GetCurrentState("BTCUSDT", types.LevelL1)
	if state == nil {
		t.Fatal("M3 中无 L1 状态")
	}
	t.Logf("\n=== M3 结构树 L1 ===")
	t.Logf("笔=%d 中枢=%d 走势=%d 双轨同步=%v 版本数=%d",
		len(state.Provisional.Strokes),
		len(state.Provisional.PivotZones),
		len(state.Provisional.TrendPatterns),
		state.InSync,
		len(versionEvents))

	// ====== M4 级别递归 ======
	t.Logf("\n=== M4 级别递归 ===")
	for _, lvl := range []types.Level{types.LevelL1, types.LevelL2, types.LevelL3, types.LevelL4} {
		lState := tree.GetCurrentState("BTCUSDT", lvl)
		if lState == nil {
			t.Logf("  %s: (无数据)", lvl)
			continue
		}
		t.Logf("  %s: 笔=%d 中枢=%d 走势=%d 同步=%v",
			lvl, len(lState.Provisional.Strokes),
			len(lState.Provisional.PivotZones),
			len(lState.Provisional.TrendPatterns),
			lState.InSync)

		for i, st := range lState.Provisional.Strokes {
			t.Logf("    %s 笔[%d]: %s [%.0f→%.0f]", lvl, i, st.Direction, st.StartPrice, st.EndPrice)
		}

		if hint := levelBldr.GetTimeHint("BTCUSDT", lvl); hint != nil {
			t.Logf("     timeHint: avg=%.0fs p10=%.0fs p90=%.0fs N=%d",
				hint.AvgDurationSec, hint.P10DurationSec,
				hint.P90DurationSec, hint.SampleCount)
		}
	}

	// ====== M5 信号引擎 ======
	activeSignals := sigEngine.GetActiveSignals("BTCUSDT")
	t.Logf("\n=== M5 信号引擎 ===")
	t.Logf("信号事件: %d (created+stateChanged) 共振事件: %d",
		len(signalEvents), len(resonanceEvents))
	t.Logf("活跃信号: %d", len(activeSignals))

	for _, s := range activeSignals {
		t.Logf("  [%s] %s %s price=%.2f conf=%.4f strength=%.4f res=%s",
			s.SignalID, s.Type, s.State, s.Price,
			s.Confidence, s.Strength, s.Resonance.Kind)
	}

	// ====== 信号字段完整性（仅当有信号时验证）======
	for _, s := range activeSignals {
		if s.SignalID == "" {
			t.Error("SignalID 为空")
		}
		if s.Type == "" {
			t.Error("Type 为空")
		}
		if s.Confidence <= 0 || s.Confidence > 1.0 {
			t.Errorf("Confidence 超范围: %.4f", s.Confidence)
		}
	}

	// ====== 事件完整性 ======
	for _, e := range resonanceEvents {
		if e.Signal == nil {
			t.Error("共振事件 Signal 不能为空")
		}
	}
	for _, e := range versionEvents {
		if e.NewVersion.VersionID == "" {
			t.Error("版本事件 VersionID 不能为空")
		}
	}

	t.Logf("\n真实数据全链路测试完成: %d 根 K 线, %d 笔, %d 中枢, %d 走势, %d 背驰",
		len(klines),
		len(pState.Strokes),
		len(pState.PivotZones),
		len(pState.TrendPatterns),
		len(pState.Divergences))

	resEngine.Stop()
	levelBldr.Stop()
}
