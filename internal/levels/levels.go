// Package m4_levels 递归级别构建器（M4）。
//
// 职责（PRD §10.1/§10.3）：
//   - 双轨制构建多级别（实时轨 + 确认轨）
//   - L(N-1) 走势类型 → LN 笔 递归
//   - 级别漂移检测 → 触发 recast
package levels

import (
	"fmt"
	"log/slog"
	"math"
	"sort"
	"sync"
	"time"

	"trade/internal/eventbus"
	"trade/internal/log"
	"trade/internal/observability"
	"trade/internal/structure"
	"trade/internal/types"
)

// 级别范围常量。
const (
	baseLevel         = types.LevelL1 // 基础级别（由 M2 Pipeline 直接产出）
	maxLevel          = types.LevelL4 // 最高递归级别
	driftThresholdPct = 0.3           // 双轨分歧阈值（30%）
)

// durationSample 单次级别持续时间的采样。
type durationSample struct {
	startTS int64   // 开始时间戳
	endTS   int64   // 结束时间戳
	durSec  float64 // 持续时间（秒）
}

// LevelBuilder 递归级别构建器（M4）。
//
// 设计说明：
//   - 事件驱动：订阅 M3 的 EventStructureVersionChanged 事件，
//     当 L1 版本变更时触发级别递归。
//   - 增量处理：通过水位线（processedTrendPatterns）追踪每个 symbol+level
//     已处理的走势类型数量，避免重复处理。
//   - 递归上升：L(N-1) 的完成走势类型 → LN 的一根笔，
//     在 LN 上检测中枢和走势类型，再继续向 L(N+1) 递归。
//   - 双轨制：实时轨即时更新，确认轨在低级别双轨同步时才更新。
//   - 级别漂移检测：当实时轨与确认轨分歧超过阈值时触发 EventLevelRecast。
type LevelBuilder struct {
	bus    *eventbus.GenericBus
	tree   *structure.Tree
	logger *slog.Logger

	mu sync.RWMutex

	// 每个 symbol+level 已处理的走势类型水位线
	processedTrendPatterns map[string]map[types.Level]int

	// 每个 symbol+level 上一次见到走势总数（检测数量变动）
	lastPatternTotal map[string]map[types.Level]int

	// 每个 symbol+level 的当前双轨状态缓存
	states map[string]map[types.Level]*types.DualTrackState

	// 事件订阅 ID（用于 Stop 时取消订阅）
	subID int64

	// LevelTimeHint 统计（PRD §6.3）
	// symbol → level → 持续时间采样列表
	timeSamples map[string]map[types.Level][]durationSample
	// symbol → level → 最后计算的 timeHint 缓存
	timeHintCache map[string]map[types.Level]*types.LevelTimeHint
}

// New 创建级别构建器，订阅 M3 版本变更事件。
//
// 订阅 EventStructureVersionChanged 事件，当基础级别（L1）有新版本时，
// 自动触发递归级别构建。
//
// 参数 bus 用于事件发布/订阅，tree 用于读写 M3 结构树。
func New(bus *eventbus.GenericBus, tree *structure.Tree) *LevelBuilder {
	b := &LevelBuilder{
		bus:                    bus,
		tree:                   tree,
		logger:                 log.Component("m4.levels"),
		processedTrendPatterns: make(map[string]map[types.Level]int),
		lastPatternTotal:       make(map[string]map[types.Level]int),
		states:                 make(map[string]map[types.Level]*types.DualTrackState),
		timeSamples:            make(map[string]map[types.Level][]durationSample),
		timeHintCache:          make(map[string]map[types.Level]*types.LevelTimeHint),
	}

	// 订阅 M3 结构版本变更事件
	b.subID = bus.Subscribe(types.EventStructureVersionChanged, b.onStructureVersionChanged)

	b.logger.Info("级别构建器已初始化",
		"baseLevel", baseLevel,
		"maxLevel", maxLevel,
	)
	return b
}

// ========================================================================
// 事件处理
// ========================================================================

// onStructureVersionChanged M3 版本变更事件处理。
//
// 当任意级别的结构版本变更时被调用。只处理基础级别（L1）的变更，
// 因为 L1 是 M2 Pipeline 直接产出的底层结构，L1 变更是递归上升的唯一触发源。
// 高级别（L2+）的版本变更由本模块自身产生，不需要重复处理。
//
// 注意：此方法在 tree.Commit 的调用栈中执行（tree.mu 写锁已持有），
// 因此不能同步调用 tree 的读方法，否则会导致死锁。
// 解决方案：将实际处理通过 goroutine 异步执行。
func (b *LevelBuilder) onStructureVersionChanged(evt types.Event) {
	symbol := evt.Symbol

	payload, ok := evt.Payload.(types.StructureVersionPayload)
	if !ok {
		b.logger.Warn("收到 M3 事件但 Payload 类型断言失败",
			"eventType", evt.Type,
			"symbol", symbol,
		)
		return
	}

	// 只处理基础级别 L1 的变更
	if payload.Level != baseLevel {
		b.logger.Info("跳过高级别事件",
			"symbol", symbol,
			"payloadLevel", payload.Level,
			"baseLevel", baseLevel,
		)
		return
	}

	b.logger.Info("M3 L1 版本变更，触发级别递归",
		"symbol", symbol,
		"versionId", payload.NewVersion.VersionID,
	)

	// 异步执行，避免阻塞 OnKline 调用链
	go b.processLevelRecursive(symbol, baseLevel)
}

// ProcessPending 保留兼容（新架构使用 LevelRunner 无需此方法）。
func (b *LevelBuilder) ProcessPending(symbol string) {}

// ========================================================================
// 核心递归逻辑
// ========================================================================

// processLevelRecursive 从 fromLevel 开始递归构建更高级别结构。
//
// 算法步骤：
//  1. 查询 fromLevel 的当前双轨状态
//  2. 获取 fromLevel 中新完成的走势类型（增量水位线）
//  3. 将每个完成的走势类型构建为 targetLevel 的一根笔（LN 笔）
//  4. 在 targetLevel 的笔序列上检测中枢（pivot zones）
//  5. 从中枢序列检测走势类型（trend patterns）
//  6. 更新 targetLevel 的双轨状态
//  7. 提交 targetLevel 新版本到 M3 结构树
//  8. 检测级别漂移，若漂移则触发 EventLevelRecast
//  9. 递归：以 targetLevel 为 fromLevel 重复步骤 1-8，直到 maxLevel
func (b *LevelBuilder) processLevelRecursive(symbol string, fromLevel types.Level) {
	defer func(start time.Time, lvl types.Level) {
		observability.M.RecordM4Duration(lvl, time.Since(start))
	}(time.Now(), fromLevel)

	targetLevel := fromLevel + 1
	if targetLevel > maxLevel {
		return
	}

	// 1. 查询 fromLevel 的当前状态
	fromState := b.tree.GetCurrentState(symbol, fromLevel)
	if fromState == nil {
		b.logger.Debug("低级别状态为空，跳过递归",
			"symbol", symbol,
			"level", fromLevel,
		)
		return
	}

	// 2. 获取新完成的走势类型
	completedPatterns := b.getNewlyCompletedTrendPatterns(symbol, fromLevel, fromState)
	if len(completedPatterns) == 0 {
		b.logger.Info("无新完成的走势类型",
			"symbol", symbol,
			"fromLevel", fromLevel,
		)
		return
	}

	b.logger.Info("检测到新完成的走势类型",
		"symbol", symbol,
		"fromLevel", fromLevel,
		"count", len(completedPatterns),
	)

	// 2b. 记录级别时间跨度采样（PRD §6.3 LevelTimeHint）
	b.recordTimeSamples(symbol, fromLevel, completedPatterns)

	// 3. 构建 targetLevel 的笔
	newStrokes := b.buildHigherLevelStrokes(symbol, targetLevel, completedPatterns)

	// 4. 更新 targetLevel 的双轨状态
	state := b.getOrCreateState(symbol, targetLevel)
	state.Provisional.Strokes = append(state.Provisional.Strokes, newStrokes...)
	state.Provisional.Provisional = true
	state.Provisional.Level = targetLevel

	// 5. 在 targetLevel 上检测中枢和走势类型
	higherPivotZones := b.detectPivotZones(state.Provisional.Strokes, targetLevel)
	state.Provisional.PivotZones = higherPivotZones

	higherTrendPatterns := b.detectTrendPatterns(state.Provisional.Strokes, higherPivotZones)
	state.Provisional.TrendPatterns = higherTrendPatterns

	// 6. 更新确认轨
	// 策略：当 fromLevel 的双轨同步时，同步同步 targetLevel 的双轨；
	// 否则 targetLevel 保持上次的确认轨，记录分歧。
	if fromState.InSync {
		state.Confirmed = state.Provisional
		state.Confirmed.Provisional = false
		state.InSync = true
		state.DriftSince = 0
	} else {
		state.InSync = false
		if state.DriftSince == 0 {
			state.DriftSince = time.Now().UnixMilli()
		}
	}

	// 7. 提交 targetLevel 版本到 M3 结构树
	diff := b.buildDiff(symbol, targetLevel, state)
	versionID := b.tree.Commit(symbol, targetLevel, state, diff)

	b.logger.Info("高级别版本已提交",
		"symbol", symbol,
		"level", targetLevel,
		"versionId", versionID,
		"strokes", len(state.Provisional.Strokes),
		"pivotZones", len(higherPivotZones),
		"trendPatterns", len(higherTrendPatterns),
	)

	// 注册新元素的 lineage
	b.registerElements(symbol, newStrokes, higherPivotZones)

	// 8. 检测级别漂移
	b.detectLevelDrift(symbol, targetLevel, state)

	// 9. 递归处理更高级别
	b.processLevelRecursive(symbol, targetLevel)
}

// ========================================================================
// 走势类型 → 笔的转换
// ========================================================================

// getNewlyCompletedTrendPatterns 返回 fromLevel 中新完成的走势类型列表。
//
// classify() 每次全量重建走势类型，当新走势出现时，前一个走势
// 会从 Completed=false 变为 Completed=true。仅用 watermark 按索引
// 跳跃会漏掉这个状态变更。解决方案：检测总走势数量变化时重置 watermark。
func (b *LevelBuilder) getNewlyCompletedTrendPatterns(symbol string, level types.Level, state *types.DualTrackState) []types.TrendPattern {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.processedTrendPatterns[symbol]; !ok {
		b.processedTrendPatterns[symbol] = make(map[types.Level]int)
	}
	if _, ok := b.lastPatternTotal[symbol]; !ok {
		b.lastPatternTotal[symbol] = make(map[types.Level]int)
	}

	watermark := b.processedTrendPatterns[symbol][level]
	patterns := state.Confirmed.TrendPatterns

	// 总数量变化 → classify() 可能改变了之前走势的 Completed 状态 → 全量重扫
	if len(patterns) != b.lastPatternTotal[symbol][level] {
		watermark = 0
	}

	if len(patterns) <= watermark {
		b.lastPatternTotal[symbol][level] = len(patterns)
		return nil
	}

	var result []types.TrendPattern
	for i := watermark; i < len(patterns); i++ {
		if patterns[i].Completed {
			result = append(result, patterns[i])
		}
	}

	b.processedTrendPatterns[symbol][level] = len(patterns)
	b.lastPatternTotal[symbol][level] = len(patterns)
	return result
}

// buildHigherLevelStrokes 将一组完成的走势类型转换为更高级别的一批笔。
//
// 每个完成的走势类型对应 LN 的一根笔：
//   - 走势类型的方向 → 笔的方向
//   - 走势类型的 StartPrice/EndPrice → 笔的起点/终点价格
//   - 走势类型的 High/Low → 笔的高低区间
//
// 返回的笔使用独立的 lineageId 命名空间（hbi = higher-level bi），
// 与 L1 的笔（bi）区分。
func (b *LevelBuilder) buildHigherLevelStrokes(symbol string, targetLevel types.Level, patterns []types.TrendPattern) []types.Stroke {
	strokes := make([]types.Stroke, 0, len(patterns))

	for i, tp := range patterns {
		// 生成跨版本稳定的 lineageId
		lineageID := fmt.Sprintf("L_%s_hbi_L%d_%d", symbol, targetLevel, i)
		// 版本内元素 ID
		elementID := fmt.Sprintf("%s_hbi_L%d_%d", symbol, targetLevel, i)

		// 方向继承自走势类型
		direction := tp.Direction
		if direction == types.DirectionNone {
			direction = types.DirectionUp
		}

		// 价格区间继承自走势类型
		startPrice := tp.StartPrice
		endPrice := tp.EndPrice
		high := tp.High
		low := tp.Low

		// 确保 high/low 正确
		if high < low {
			high, low = low, high
		}

		strokes = append(strokes, types.Stroke{
			StructureElement: types.StructureElement{
				ID:          elementID,
				ElementType: types.ElementTypeStroke,
				LineageID:   lineageID,
				ValidFromTS: time.Now().UnixMilli(),
			},
			Direction:  direction,
			StartPrice: startPrice,
			EndPrice:   endPrice,
			High:       high,
			Low:        low,
			Virtual:    false,
		})
	}

	return strokes
}

// ========================================================================
// LevelTimeHint 统计（PRD §6.3）
// ========================================================================

// recordTimeSamples 记录级别持续时间的采样点。
//
// 每个完成的走势类型提供一个采样：从 ValidFromTS 到当前的持续时间为样本。
// 走势类型完成时（Completed=true）触发。
func (b *LevelBuilder) recordTimeSamples(symbol string, level types.Level, patterns []types.TrendPattern) {
	now := time.Now().UnixMilli()

	b.mu.Lock()
	defer b.mu.Unlock()

	// 初始化 map
	if _, ok := b.timeSamples[symbol]; !ok {
		b.timeSamples[symbol] = make(map[types.Level][]durationSample)
	}

	for _, tp := range patterns {
		if !tp.Completed {
			continue
		}
		durSec := float64(now-tp.ValidFromTS) / 1000.0
		if durSec <= 0 {
			continue
		}

		b.timeSamples[symbol][level] = append(b.timeSamples[symbol][level], durationSample{
			startTS: tp.ValidFromTS,
			endTS:   now,
			durSec:  durSec,
		})
	}

	// 每次记录后重新计算缓存
	b.rebuildTimeHintCache(symbol, level)
}

// rebuildTimeHintCache 重建指定 symbol+level 的 timeHint 缓存。
func (b *LevelBuilder) rebuildTimeHintCache(symbol string, level types.Level) {
	samples := b.timeSamples[symbol][level]
	if len(samples) == 0 {
		return
	}

	// 收集所有 durSec 并排序
	durs := make([]float64, len(samples))
	for i, s := range samples {
		durs[i] = s.durSec
	}

	// 计算平均值
	sum := 0.0
	for _, d := range durs {
		sum += d
	}
	avg := sum / float64(len(durs))

	// 计算 P10/P90（简单实现：排序后取百分位）
	sort.Float64s(durs)
	p10 := percentile(durs, 10)
	p90 := percentile(durs, 90)

	// 缓存结果
	if _, ok := b.timeHintCache[symbol]; !ok {
		b.timeHintCache[symbol] = make(map[types.Level]*types.LevelTimeHint)
	}
	b.timeHintCache[symbol][level] = &types.LevelTimeHint{
		AvgDurationSec: math.Round(avg*100) / 100,
		P10DurationSec: math.Round(p10*100) / 100,
		P90DurationSec: math.Round(p90*100) / 100,
		SampleCount:    len(samples),
	}
}

// GetTimeHint 返回指定 symbol+level 的 timeHint 统计。
func (b *LevelBuilder) GetTimeHint(symbol string, level types.Level) *types.LevelTimeHint {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if _, ok := b.timeHintCache[symbol]; !ok {
		return nil
	}
	return b.timeHintCache[symbol][level]
}

// GetAllTimeHints 返回指定 symbol 的所有级别 timeHint。
func (b *LevelBuilder) GetAllTimeHints(symbol string) map[string]*types.LevelTimeHint {
	b.mu.RLock()
	defer b.mu.RUnlock()

	result := make(map[string]*types.LevelTimeHint)
	if _, ok := b.timeHintCache[symbol]; !ok {
		return result
	}
	for level, hint := range b.timeHintCache[symbol] {
		result[level.String()] = hint
	}
	return result
}

// percentile 计算排序后的浮点数切片在 p 百分位的值。
func percentile(sorted []float64, p int) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	if p == 0 {
		return sorted[0]
	}
	if p == 100 {
		return sorted[len(sorted)-1]
	}
	index := float64(p) / 100.0 * float64(len(sorted)-1)
	lower := int(index)
	upper := lower + 1
	if upper >= len(sorted) {
		return sorted[lower]
	}
	frac := index - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}

// ========================================================================
// 高级别中枢检测
// ========================================================================

// detectPivotZones 在笔序列上检测中枢。
//
// 中枢定义（与 L1 相同）：
//   - 连续三笔的重叠区间形成中枢
//   - ZG（中枢上沿）= 三笔最高价的最小值
//   - ZD（中枢下沿）= 三笔最低价的最大值
//   - ZG > ZD 时为有效中枢
//
// 延伸检测：后续笔若与中枢区间重叠则延伸（SegmentCount++）。
// 跳过已被已有中枢覆盖的笔。
func (b *LevelBuilder) detectPivotZones(strokes []types.Stroke, level types.Level) []types.PivotZone {
	if len(strokes) < 3 {
		return nil
	}

	var zones []types.PivotZone
	zoneIdx := 0

	// 标记哪些笔索引已被中枢覆盖
	covered := make([]bool, len(strokes))

	for i := 0; i <= len(strokes)-3; i++ {
		if covered[i] {
			continue
		}

		s1, s2, s3 := strokes[i], strokes[i+1], strokes[i+2]

		// 计算三笔重叠区间
		zg := math.Min(s1.High, math.Min(s2.High, s3.High))
		zd := math.Max(s1.Low, math.Max(s2.Low, s3.Low))

		if zg <= zd {
			continue // 无有效重叠，不能形成中枢
		}

		dir := s1.Direction

		// 创建中枢
		zone := types.PivotZone{
			StructureElement: types.StructureElement{
				ID:          fmt.Sprintf("%s_pz_L%d_%d", "", level, zoneIdx),
				ElementType: types.ElementTypePivotZone,
				LineageID:   fmt.Sprintf("L_pz_L%d_%d", level, zoneIdx),
				ValidFromTS: time.Now().UnixMilli(),
			},
			Direction:    dir,
			ZG:           zg,
			ZD:           zd,
			SegmentCount: 3,
			Level:        level,
		}

		// 延伸检测
		for j := i + 3; j < len(strokes); j++ {
			s := strokes[j]
			if s.High >= zd && s.Low <= zg {
				zone.SegmentCount++
			} else {
				break
			}
		}

		// 标记覆盖的笔
		endIdx := i + zone.SegmentCount
		if endIdx > len(strokes) {
			endIdx = len(strokes)
		}
		for k := i; k < endIdx; k++ {
			covered[k] = true
		}

		// 修复 elementID（需要 symbol，但此时未知，用占位符）
		zone.ID = fmt.Sprintf("pz_L%d_%d", level, zoneIdx)
		zone.LineageID = fmt.Sprintf("L_pz_L%d_%d", level, zoneIdx)

		zones = append(zones, zone)
		zoneIdx++
	}

	return zones
}

// ========================================================================
// 高级别走势类型分类
// ========================================================================

// detectTrendPatterns 从中枢序列检测走势类型。
//
// 分类规则（与 L1 相同）：
//   - 一组互不重叠的同向中枢构成一个走势类型
//   - 1 个中枢 → "consolidation"（盘整）
//   - ≥2 个中枢 → "trend"（趋势）
//   - 中枢方向变化或重叠时，结束当前走势、开始新走势
func (b *LevelBuilder) detectTrendPatterns(strokes []types.Stroke, pivotZones []types.PivotZone) []types.TrendPattern {
	if len(pivotZones) == 0 {
		return nil
	}

	var patterns []types.TrendPattern

	// 按同向非重叠分组中枢
	currentGroup := []types.PivotZone{pivotZones[0]}

	for i := 1; i < len(pivotZones); i++ {
		prev := pivotZones[i-1]
		curr := pivotZones[i]

		// 若中枢重叠或方向变化 → 结束当前走势
		if pivotZonesOverlap(prev, curr) || curr.Direction != prev.Direction {
			patterns = append(patterns, makeTrendPattern(currentGroup))
			currentGroup = []types.PivotZone{curr}
		} else {
			currentGroup = append(currentGroup, curr)
		}
	}

	// 最后一组
	patterns = append(patterns, makeTrendPattern(currentGroup))

	return patterns
}

// pivotZonesOverlap 检查两个中枢的波动区间是否有重叠。
func pivotZonesOverlap(a, b types.PivotZone) bool {
	return a.ZG > b.ZD && a.ZD < b.ZG
}

// makeTrendPattern 从一组同向非重叠中枢创建一个走势类型。
//
// 价格区间计算：
//   - High = 所有中枢 ZG 的最大值
//   - Low  = 所有中枢 ZD 的最小值
//   - StartPrice = 第一个中枢的 ZD（向上趋势）或 ZG（向下趋势）
//   - EndPrice   = 最后一个中枢的 ZG（向上趋势）或 ZD（向下趋势）
func makeTrendPattern(group []types.PivotZone) types.TrendPattern {
	typeStr := "consolidation"
	if len(group) >= 2 {
		typeStr = "trend"
	}

	dir := group[0].Direction
	high := group[0].ZG
	low := group[0].ZD
	for _, z := range group[1:] {
		if z.ZG > high {
			high = z.ZG
		}
		if z.ZD < low {
			low = z.ZD
		}
	}

	// 起止价格：基于趋势方向
	var startPrice, endPrice float64
	if dir == types.DirectionUp {
		startPrice = group[0].ZD          // 向上趋势从下沿开始
		endPrice = group[len(group)-1].ZG // 到上沿结束
	} else {
		startPrice = group[0].ZG          // 向下趋势从上沿开始
		endPrice = group[len(group)-1].ZD // 到下沿结束
	}

	pzIDs := make([]string, len(group))
	for i, z := range group {
		pzIDs[i] = z.LineageID
	}

	return types.TrendPattern{
		StructureElement: types.StructureElement{
			ID:          "",
			ElementType: types.ElementTypeTrendPattern,
			ValidFromTS: time.Now().UnixMilli(),
		},
		Direction:    dir,
		PivotZoneIDs: pzIDs,
		Type:         typeStr,
		Completed:    true,
		StartPrice:   startPrice,
		EndPrice:     endPrice,
		High:         high,
		Low:          low,
	}
}

// ========================================================================
// 级别漂移检测
// ========================================================================

// detectLevelDrift 检测指定级别的双轨分歧程度。
//
// 漂移条件：实时轨笔数比确认轨笔数超出阈值（driftThresholdPct）。
// 当漂移发生时，发布 EventLevelRecast 事件，供 M5 信号引擎和 M10 可观测层消费。
func (b *LevelBuilder) detectLevelDrift(symbol string, level types.Level, state *types.DualTrackState) {
	if state.InSync {
		return
	}

	provisionalLen := len(state.Provisional.Strokes)
	confirmedLen := len(state.Confirmed.Strokes)

	if confirmedLen <= 0 {
		return
	}

	ratio := float64(provisionalLen-confirmedLen) / float64(confirmedLen)
	if ratio <= driftThresholdPct {
		return
	}

	b.logger.Warn("级别漂移",
		"symbol", symbol,
		"level", level,
		"provisionalStrokes", provisionalLen,
		"confirmedStrokes", confirmedLen,
		"ratio", ratio,
	)

	b.bus.Publish(types.Event{
		Type:   types.EventLevelRecast,
		Symbol: symbol,
		TS:     time.Now().UnixMilli(),
		Payload: types.LevelRecastEvent{
			Symbol:   symbol,
			OldLevel: level,
			NewLevel: level,
			TS:       time.Now().UnixMilli(),
		},
	})
}

// ========================================================================
// 状态管理
// ========================================================================

// getOrCreateState 获取或创建指定 symbol+level 的双轨状态。
//
// 查找优先级：
//  1. 本地缓存（b.states）
//  2. M3 结构树（b.tree.GetCurrentState）
//  3. 创建新的空状态
func (b *LevelBuilder) getOrCreateState(symbol string, level types.Level) *types.DualTrackState {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.states[symbol]; !ok {
		b.states[symbol] = make(map[types.Level]*types.DualTrackState)
	}

	if state, ok := b.states[symbol][level]; ok {
		return state
	}

	// 尝试从 M3 树获取
	treeState := b.tree.GetCurrentState(symbol, level)
	if treeState != nil {
		b.states[symbol][level] = treeState
		return treeState
	}

	// 创建新的空状态
	state := &types.DualTrackState{
		Provisional: types.LevelStructure{
			Level:       level,
			Provisional: true,
		},
		Confirmed: types.LevelStructure{
			Level:       level,
			Provisional: false,
		},
		InSync: true,
	}
	b.states[symbol][level] = state
	return state
}

// GetState 返回指定 symbol+level 的双轨状态。
// 优先查本地缓存，回退到 M3 结构树查询。
func (b *LevelBuilder) GetState(symbol string, level types.Level) *types.DualTrackState {
	b.mu.RLock()
	if sym, ok := b.states[symbol]; ok {
		if lvl, ok := sym[level]; ok {
			b.mu.RUnlock()
			return lvl
		}
	}
	b.mu.RUnlock()

	return b.tree.GetCurrentState(symbol, level)
}

// ========================================================================
// 版本差异与元素注册
// ========================================================================

// buildDiff 计算 targetLevel 新版本与上一版本的差异。
func (b *LevelBuilder) buildDiff(symbol string, level types.Level, state *types.DualTrackState) *types.VersionDiff {
	prev := b.tree.GetLatestStructure(symbol, level)

	diff := &types.VersionDiff{
		AddedElementIDs:  []string{},
		AffectedWindowTS: []int64{time.Now().UnixMilli()},
	}

	prevStrokeCount := 0
	if prev != nil {
		prevStrokeCount = len(prev.Strokes)
	}

	for i := prevStrokeCount; i < len(state.Provisional.Strokes); i++ {
		diff.AddedElementIDs = append(diff.AddedElementIDs, state.Provisional.Strokes[i].ID)
	}

	return diff
}

// registerElements 将新元素注册到 M3 结构树，建立 lineage 映射。
func (b *LevelBuilder) registerElements(symbol string, strokes []types.Stroke, pivotZones []types.PivotZone) {
	ts := time.Now().UnixMilli()

	for i := range strokes {
		b.tree.RegisterElement(
			strokes[i].ID,
			strokes[i].LineageID,
			types.ElementTypeStroke,
			ts,
		)
	}

	for i := range pivotZones {
		b.tree.RegisterElement(
			pivotZones[i].ID,
			pivotZones[i].LineageID,
			types.ElementTypePivotZone,
			ts,
		)
	}
}

// Stop 停止级别构建器，取消事件订阅。
func (b *LevelBuilder) Stop() {
	b.bus.Unsubscribe(types.EventStructureVersionChanged, b.subID)
	b.logger.Info("级别构建器已停止")
}

// OnLowerLevelComplete 下级走势类型完成时调用（对外接口）。
//
// 当外部模块（如 M3Bridge 或回测引擎）显式通知有走势类型完成时使用。
// 此方法会触发与事件处理相同的递归构建逻辑。
func (b *LevelBuilder) OnLowerLevelComplete(symbol string, level types.Level, trendPattern *types.TrendPattern) {
	if trendPattern == nil || !trendPattern.Completed {
		return
	}

	b.logger.Debug("外部通知：下级走势类型完成",
		"symbol", symbol,
		"level", level,
		"type", trendPattern.Type,
		"direction", trendPattern.Direction,
	)

	b.processLevelRecursive(symbol, level)
}
