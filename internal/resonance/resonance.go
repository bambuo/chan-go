// Package m6_resonance 共振引擎（M6）。
//
// 职责（PRD §9）：
//   - G-2 区间套（最高优先级）：信号是否在大级别背驰段内
//   - G-1 跨层共振：是否有其他级别同向信号
//   - A3 方向过滤：多级别方向一致性检查
//   - 共振等待窗口管理（G-1 超时机制）
//   - 信号 confidence 的共振因子更新（directionAlignment × nestingDepth）
//
// 处理流程：
//
//	OnSignal → G-2 区间套检查 → G-1 跨层检查 → A3 方向过滤
//	→ 更新 signal.Resonance → 发布 EventResonanceTriggered
//	→ 若 G-1 等待中 → 启动等待窗口
package resonance

import (
	"log/slog"
	"math"
	"sync"
	"time"

	"trade/internal/eventbus"
	"trade/internal/log"
	"trade/internal/observability"
	"trade/internal/structure"
	"trade/internal/types"
)

// G-2 区间套检查中涉及的最高级别
const maxCheckLevel = types.LevelL4

// minIntervalMs 一个完整的 K 线周期（1min），用于等待窗口超时估算
const minIntervalMs = 60000

// G-1 跨层共振等待窗口的默认超时倍数（大级别 × 3 根 K 线）
const g1WindowTimeoutMultiplier = 3

// ResonanceEngine 共振引擎。
type ResonanceEngine struct {
	bus    *eventbus.GenericBus
	tree   *structure.Tree
	logger *slog.Logger

	mu sync.RWMutex

	// 所有 symbol 下各级别的当前信号（用于 G-1 跨层查找）
	// map[symbol]map[level][]*types.Signal
	signalsByLevel map[string]map[types.Level][]*types.Signal

	// 共振等待窗口（PRD §9.3 R2）
	pendingWindows map[string]*resonanceWindow // signalId → 等待窗口

	// 事件订阅 ID
	subIDs []int64
}

type resonanceWindow struct {
	Signal    *types.Signal
	StartTS   int64
	TimeoutTS int64 // = StartTS + 大级别 × 3 根 K 线
	Level     types.Level
}

// New 创建共振引擎，订阅信号事件。
// 需要 bus 用于事件发布/订阅，tree 用于查询多级别结构（G-2 区间套）。
func New(bus *eventbus.GenericBus, tree *structure.Tree) *ResonanceEngine {
	e := &ResonanceEngine{
		bus:            bus,
		tree:           tree,
		logger:         log.Component("m6.resonance"),
		signalsByLevel: make(map[string]map[types.Level][]*types.Signal),
		pendingWindows: make(map[string]*resonanceWindow),
	}

	// 订阅 M5 信号创建事件
	id1 := bus.Subscribe(types.EventSignalCreated, e.onSignalCreated)
	e.subIDs = append(e.subIDs, id1)

	e.logger.Info("共振引擎已初始化")
	return e
}

// ========================================================================
// 事件处理
// ========================================================================

// onSignalCreated M5 信号创建事件处理。
func (e *ResonanceEngine) onSignalCreated(evt types.Event) {
	payload, ok := evt.Payload.(types.SignalEventPayload)
	if !ok || payload.Signal == nil {
		return
	}

	signal := payload.Signal

	// 注册信号到级别索引
	e.registerSignal(signal)

	// 执行共振判定
	e.processSignal(signal)
}

// processSignal 对单个信号执行完整的共振判定流程。
func (e *ResonanceEngine) processSignal(signal *types.Signal) {
	if signal == nil {
		return
	}

	e.logger.Debug("共振判定",
		"signalId", signal.SignalID,
		"type", signal.Type,
		"level", signal.Level,
		"symbol", signal.Symbol,
	)

	// 1. G-2 区间套：检查是否在大级别背驰段内
	nestingDepth := e.checkIntervalNesting(signal)

	// 2. G-1 跨层：检查是否有其他级别同向信号
	crossLevelSignals := e.checkCrossLevel(signal)

	// 3. A3 方向过滤：检查多级别方向一致性
	dirFilter := e.checkDirectionAlignment(signal)

	// 4. 构建 Resonace 结果
	resonance := e.buildResonance(signal, nestingDepth, crossLevelSignals, dirFilter)

	// 5. 更新 signal 的 Resonance 字段
	signal.Resonance = resonance

	// 6. 更新 signal confidence 的共振因子（PRD §8.4）
	//   confidence = base × divergenceStrength × directionAlignment × nestingDepth × (1-recastRisk)
	//   共振引擎贡献 directionAlignment × nestingDepth 因子
	e.applyConfidenceBoost(signal, resonance)

	// 7. 填充 Evidence.IntervalNestingChain（区间套链）
	if nestingDepth > 1 {
		e.populateNestingChain(signal)
	}

	// 8. 发布共振事件
	if resonance.Kind != types.ResonanceStandalone {
		e.publishResonanceEvent(signal, resonance)
	}

	// 9. G-1 等待窗口管理
	if resonance.Kind == types.ResonanceCrossLevel && !e.hasCrossLevelConfirmation(signal) {
		e.startWaitingWindow(signal)
	}
}

// ========================================================================
// G-2 区间套检查
// ========================================================================

// checkIntervalNesting 检查信号是否在大级别背驰段内（G-2）。
//
// G-2 判定规则（PRD §9）：
//   - 检查 L(N+1)、L(N+2)… 最高到 L4 的结构
//   - 若高级别有同方向的趋势（trend）或盘整（consolidation）且已完成，
//     且信号价格在高级别的走势价格区间内 → 视为区间套
//   - 返回区间套层数（1 = 仅本级别，2 = 嵌套 1 层，3 = 嵌套 2 层…）
//
// 简化实现：高于信号级别的每个级别，检查其确认轨中是否有完成的走势类型
// 与信号同向，且信号价格在走势类型的区间内。
func (e *ResonanceEngine) checkIntervalNesting(signal *types.Signal) int {
	depth := 1 // 本级始终算 1 层

	for level := signal.Level + 1; level <= maxCheckLevel; level++ {
		state := e.tree.GetCurrentState(signal.Symbol, level)
		if state == nil {
			continue
		}

		// 从确认轨检查完成的走势类型
		for _, tp := range state.Confirmed.TrendPatterns {
			if !tp.Completed {
				continue
			}
			// 走势类型方向与信号方向一致
			if !isSameDirection(tp.Direction, signal.Type) {
				continue
			}
			// 信号价格在走势类型的区间内
			if signal.Price >= tp.Low && signal.Price <= tp.High {
				depth++
				e.logger.Debug("G-2 区间套命中",
					"level", level,
					"trendPatternType", tp.Type,
					"direction", tp.Direction,
					"signalPrice", signal.Price,
					"tpHigh", tp.High,
					"tpLow", tp.Low,
					"depth", depth,
				)
				break // 该级别只计 1 层
			}
		}
	}

	return depth
}

// ========================================================================
// G-1 跨层共振检查
// ========================================================================

// checkCrossLevel 检查是否有其他级别存在同向信号（G-1）。
//
// G-1 判定规则（PRD §9.1）：
//   - 检查 L1~L4 中除本级别外的其他级别
//   - 若某个级别存在同类型（buy/sell）的信号 → 跨层共振
//   - 返回所有参与者列表
func (e *ResonanceEngine) checkCrossLevel(signal *types.Signal) []*types.Signal {
	var participants []*types.Signal

	e.mu.RLock()
	symbolSignals, ok := e.signalsByLevel[signal.Symbol]
	e.mu.RUnlock()
	if !ok {
		return nil
	}

	for level := types.LevelL1; level <= maxCheckLevel; level++ {
		if level == signal.Level {
			continue
		}

		e.mu.RLock()
		sigs, ok := symbolSignals[level]
		e.mu.RUnlock()
		if !ok {
			continue
		}

		for _, s := range sigs {
			if isSameDirectionBuySell(signal.Type, s.Type) {
				participants = append(participants, s)
				break // 每个级别只列 1 个
			}
		}
	}

	return participants
}

// hasCrossLevelConfirmation 检查指定信号是否已有同向跨层信号处于 confirmed 状态。
func (e *ResonanceEngine) hasCrossLevelConfirmation(signal *types.Signal) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	symbolSignals, ok := e.signalsByLevel[signal.Symbol]
	if !ok {
		return false
	}

	for level := types.LevelL1; level <= maxCheckLevel; level++ {
		if level == signal.Level {
			continue
		}
		sigs, ok := symbolSignals[level]
		if !ok {
			continue
		}
		for _, s := range sigs {
			if isSameDirectionBuySell(signal.Type, s.Type) && s.State == types.SignalConfirmed {
				return true
			}
		}
	}
	return false
}

// ========================================================================
// A3 方向过滤
// ========================================================================

// checkDirectionAlignment 检查多级别方向一致性（A3）。
//
// A3 判定规则（PRD §9.1）：
//   - 检查 L1~L4 各个级别的当前结构方向
//   - 与信号同向的级别数量 / 总有效级别数
//   - 若比例 ≥ 50% → aligned = true, boost = proportion × 0.2
//
// 结构方向从级别的双轨状态推导：
//   - 若级别的最新走势类型方向与信号同向 → 算对齐
func (e *ResonanceEngine) checkDirectionAlignment(signal *types.Signal) *types.DirectionFilter {
	filter := &types.DirectionFilter{
		AlignedLevels: make([]types.Level, 0),
	}

	totalLevels := 0
	alignedCount := 0
	signalIsBuy := isBuyType(signal.Type)

	for level := types.LevelL1; level <= maxCheckLevel; level++ {
		state := e.tree.GetCurrentState(signal.Symbol, level)
		if state == nil {
			continue
		}

		// 从确认轨获取最新走势类型的方向
		patterns := state.Confirmed.TrendPatterns
		if len(patterns) == 0 {
			continue
		}

		latest := patterns[len(patterns)-1]
		totalLevels++

		// 将走势类型方向映射为 buy/sell
		levelIsUp := latest.Direction == types.DirectionUp
		if (signalIsBuy && levelIsUp) || (!signalIsBuy && !levelIsUp) {
			alignedCount++
			filter.AlignedLevels = append(filter.AlignedLevels, level)
		}
	}

	if totalLevels > 0 {
		ratio := float64(alignedCount) / float64(totalLevels)
		filter.Aligned = ratio >= 0.5
		filter.Boost = ratio * 0.2 // boost ∈ [0, 0.2]
	}

	return filter
}

// ========================================================================
// 共振结果构建
// ========================================================================

// buildResonance 根据 G-2、G-1、A3 结果构建最终共振信息。
//
// 优先级（PRD §9.1）：
//   1. G-2 区间套（最高优先级）—— intervalNesting
//   2. G-1 跨层共振 —— crossLevel
//   3. A3 方向过滤 + 无其他共振 —— directionOnly
//   4. 无任何共振 —— standalone
func (e *ResonanceEngine) buildResonance(
	signal *types.Signal,
	nestingDepth int,
	crossLevelSignals []*types.Signal,
	dirFilter *types.DirectionFilter,
) types.Resonance {
	resonance := types.Resonance{}

	// 确定共振类型（高优先级优先）
	if nestingDepth > 1 {
		resonance.Kind = types.ResonanceIntervalNesting
	} else if len(crossLevelSignals) > 0 {
		resonance.Kind = types.ResonanceCrossLevel
	} else if dirFilter.Aligned {
		resonance.Kind = types.ResonanceDirectionOnly
	} else {
		resonance.Kind = types.ResonanceStandalone
	}

	// 参与者
	if len(crossLevelSignals) > 0 {
		for _, s := range crossLevelSignals {
			resonance.Participants = append(resonance.Participants, types.ResonanceParticipant{
				Level:    s.Level,
				Type:     s.Type,
				SignalID: s.SignalID,
			})
		}
	}

	// 方向过滤
	if dirFilter != nil {
		resonance.DirectionFilter = dirFilter
	}

	e.logger.Debug("共振结果",
		"signalId", signal.SignalID,
		"kind", resonance.Kind,
		"nestingDepth", nestingDepth,
		"crossLevelCount", len(crossLevelSignals),
		"directionAligned", dirFilter.Aligned,
		"directionBoost", dirFilter.Boost,
	)

	return resonance
}

// publishResonanceEvent 发布共振触发事件。
func (e *ResonanceEngine) publishResonanceEvent(signal *types.Signal, resonance types.Resonance) {
	observability.M.RecordSignalCreated(signal.Symbol, types.SignalType(string(signal.Type)+"_RESONANCE_"+string(resonance.Kind)), signal.Level)

	e.bus.Publish(types.Event{
		Type:   types.EventResonanceTriggered,
		Symbol: signal.Symbol,
		TS:     time.Now().UnixMilli(),
		Payload: types.ResonanceEventPayload{
			Signal:    signal,
			Resonance: resonance,
		},
	})

	e.logger.Info("共振触发",
		"signalId", signal.SignalID,
		"kind", resonance.Kind,
		"symbol", signal.Symbol,
		"type", signal.Type,
		"level", signal.Level,
	)
}

// ========================================================================
// 等待窗口管理
// ========================================================================

// startWaitingWindow 为 G-1 跨层共振启动等待窗口（PRD §9.3 R2）。
//
// 当出现一个级别的信号但尚未有其他级别确认时，启动等待窗口。
// 窗口超时 = 大级别 × 3 根 K 线（默认 3min）。
// 窗口内若出现同向信号 → 升级为 crossLevel 共振。
// 窗口超时 → 信号以 standalone 发出。
func (e *ResonanceEngine) startWaitingWindow(signal *types.Signal) {
	now := time.Now().UnixMilli()
	// 窗口超时：以大一级别 × 3 根 K 线估算
	timeout := now + int64(g1WindowTimeoutMultiplier)*minIntervalMs

	e.mu.Lock()
	e.pendingWindows[signal.SignalID] = &resonanceWindow{
		Signal:    signal,
		StartTS:   now,
		TimeoutTS: timeout,
		Level:     signal.Level,
	}
	e.mu.Unlock()

	e.logger.Debug("G-1 等待窗口启动",
		"signalId", signal.SignalID,
		"level", signal.Level,
		"timeoutMs", g1WindowTimeoutMultiplier*minIntervalMs,
	)
}

// OnTimeout 检查是否有超时的等待窗口（应定期调用）。
// 窗口超时后，信号保留当前共振类型不变（不再升级）。
func (e *ResonanceEngine) OnTimeout() {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now().UnixMilli()
	for id, w := range e.pendingWindows {
		if now >= w.TimeoutTS {
			e.logger.Debug("共振等待窗口超时",
				"signalId", id,
				"level", w.Level,
				"kind", w.Signal.Resonance.Kind,
			)
			delete(e.pendingWindows, id)
		}
	}
}

// ========================================================================
// 信号注册
// ========================================================================

// registerSignal 将信号注册到级别索引（用于 G-1 跨层查找）。
func (e *ResonanceEngine) registerSignal(signal *types.Signal) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, ok := e.signalsByLevel[signal.Symbol]; !ok {
		e.signalsByLevel[signal.Symbol] = make(map[types.Level][]*types.Signal)
	}

	level := signal.Level
	e.signalsByLevel[signal.Symbol][level] = append(
		e.signalsByLevel[signal.Symbol][level],
		signal,
	)

	// 限制每个级别保留的信号数量（防止内存膨胀）
	sigs := e.signalsByLevel[signal.Symbol][level]
	if len(sigs) > 100 {
		e.signalsByLevel[signal.Symbol][level] = sigs[len(sigs)-100:]
	}
}

// GetActiveSignals 返回指定 symbol+level 的信号列表（供外部查询）。
func (e *ResonanceEngine) GetActiveSignals(symbol string, level types.Level) []*types.Signal {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if _, ok := e.signalsByLevel[symbol]; !ok {
		return nil
	}
	sigs := e.signalsByLevel[symbol][level]
	if len(sigs) == 0 {
		return nil
	}
	result := make([]*types.Signal, len(sigs))
	copy(result, sigs)
	return result
}

// Stop 停止共振引擎。
func (e *ResonanceEngine) Stop() {
	for _, id := range e.subIDs {
		e.bus.Unsubscribe(types.EventSignalCreated, id)
	}
	e.logger.Info("共振引擎已停止")
}

// ========================================================================
// 辅助函数
// ========================================================================

// isBuyType 判断信号类型是否为买入。
func isBuyType(st types.SignalType) bool {
	return st == types.SignalBuy1 || st == types.SignalBuy2 || st == types.SignalBuy3
}

// isSameDirection 判断走势类型方向与信号类型在区间套语义下是否一致。
//
// G-2 区间套判定：大级别走势方向与信号类型的匹配关系为
//   - 大级别 DOWN → BUY 信号（大级别下跌段内的小级别底背驰买点）
//   - 大级别 UP → SELL 信号（大级别上涨段内的小级别顶背驰卖点）
//
// 这是因为区间套检查的是"大级别背驰段内是否出现同向的小级别背驰"：
// 大级别向下背驰段内出现小级别向下背驰（Buy）= 区间套 ✓
// 大级别向上背驰段内出现小级别向上背驰（Sell）= 区间套 ✓
func isSameDirection(dir types.ChanDirection, st types.SignalType) bool {
	if dir == types.DirectionDown {
		return st == types.SignalBuy1 || st == types.SignalBuy2 || st == types.SignalBuy3
	}
	if dir == types.DirectionUp {
		return st == types.SignalSell1 || st == types.SignalSell2 || st == types.SignalSell3
	}
	return false
}

// isSameDirectionBuySell 判断两个信号类型是否同为买入或同为卖出。
func isSameDirectionBuySell(a, b types.SignalType) bool {
	aBuy := a == types.SignalBuy1 || a == types.SignalBuy2 || a == types.SignalBuy3
	bBuy := b == types.SignalBuy1 || b == types.SignalBuy2 || b == types.SignalBuy3
	return aBuy == bBuy
}

// CalculateConfidenceBoost 根据共振结果为信号计算 confidence 因子。
// 返回 (directionAlignmentFactor, nestingDepthFactor, 总因子)。
//
// directionAlignment = 1 + A3Boost (∈ [1, 1.2])
// nestingDepth = 1 + 0.1 × (区间套层数 - 1)  (∈ [1, ...))
func CalculateConfidenceBoost(resonance types.Resonance) (directionAlignment, nestingDepth, total float64) {
	directionAlignment = 1.0
	nestingDepth = 1.0

	if resonance.DirectionFilter != nil && resonance.DirectionFilter.Aligned {
		directionAlignment = 1.0 + math.Min(resonance.DirectionFilter.Boost, 0.2)
	}

	switch resonance.Kind {
	case types.ResonanceIntervalNesting:
		// 有区间套时，nestingDepth = 1 + 0.1 × (层数 - 1)
		// 默认 2 层
		nestingDepth = 1.0 + 0.1
	case types.ResonanceCrossLevel:
		// 跨层共振给 1.1
		nestingDepth = 1.1
	case types.ResonanceDirectionOnly:
		// 仅方向对齐给 1.05
		nestingDepth = 1.05
	default:
		nestingDepth = 1.0
	}

	total = directionAlignment * nestingDepth
	return
}

// applyConfidenceBoost 将共振因子乘入信号 confidence（PRD §8.4）。
//
// 公式：confidence = base × divergenceStrength × directionAlignment × nestingDepth × (1-recastRisk)
// 共振引擎贡献 directionAlignment × nestingDepth 因子。
// 原 confidence（来自信号引擎的 calcDivergenceConfidence）作为 divergenceStrength 看待。
func (e *ResonanceEngine) applyConfidenceBoost(signal *types.Signal, resonance types.Resonance) {
	_, _, total := CalculateConfidenceBoost(resonance)

	oldConfidence := signal.Confidence
	signal.Confidence = math.Min(oldConfidence*total, 1.0)

	e.logger.Debug("confidence 共振因子应用",
		"signalId", signal.SignalID,
		"old", oldConfidence,
		"boost", total,
		"new", signal.Confidence,
		"kind", resonance.Kind,
	)
}

// populateNestingChain 填充 Evidence.IntervalNestingChain（PRD §8.1）。
//
// 遍历 L(N+1)~L4 中与信号同向的走势类型，标记其是否在区间套内。
func (e *ResonanceEngine) populateNestingChain(signal *types.Signal) {
	if signal.Evidence.IntervalNestingChain != nil {
		return // 已有数据，不覆盖
	}

	for level := signal.Level + 1; level <= maxCheckLevel; level++ {
		state := e.tree.GetCurrentState(signal.Symbol, level)
		if state == nil {
			continue
		}
		for _, tp := range state.Confirmed.TrendPatterns {
			if !tp.Completed || !isSameDirection(tp.Direction, signal.Type) {
				continue
			}
			if signal.Price >= tp.Low && signal.Price <= tp.High {
				signal.Evidence.IntervalNestingChain = append(
					signal.Evidence.IntervalNestingChain,
					types.NestingLink{
						Level:               level,
						InDivergenceSegment: true,
					},
				)
				break
			}
		}
	}
}
