// Package m5_signal 信号引擎（M5）。
//
// 职责（PRD §8）：
//   - 三类买卖点识别（一买/二买/三买，一卖/二卖/三卖）
//   - candidate/confirmed/invalidated 状态机
//   - 信号身份判定与去重
package signal

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"trade/internal/chanlun"
	"trade/internal/eventbus"
	"trade/internal/log"
	"trade/internal/observability"
	"trade/internal/types"
)

// strengthHistory 单个信号的强度演化跟踪。
type strengthHistory struct {
	ratioHistory []float64 // MACD 面积比历史（用于判定收敛/发散趋势）
	confirmCount int       // 连续确认计数（consecutiveConfirmations）
	lastUpdateTS int64     // 上次更新时间
}

// SignalEngine 信号引擎。
type SignalEngine struct {
	logger *slog.Logger

	mu sync.RWMutex

	bus *eventbus.GenericBus // 用于发布信号事件（M6 共振引擎订阅）

	activeSignals map[string]*types.Signal
	anchorIndex   map[string]*types.Signal

	lastBuy1s  map[string]*types.Signal
	lastSell1s map[string]*types.Signal

	// strength 演化跟踪（PRD §8.5）
	strengthHist map[string]*strengthHistory // signalId → 强度历史

	// 事件订阅 ID（用于 Stop 时取消）
	subIDs []int64
}

// New 创建信号引擎。
// bus 可选，若提供则信号创建/变更时会发布事件供共振引擎消费。
func New(bus *eventbus.GenericBus) *SignalEngine {
	e := &SignalEngine{
		logger:        log.Component("m5.signal"),
		bus:           bus,
		activeSignals: make(map[string]*types.Signal),
		anchorIndex:   make(map[string]*types.Signal),
		lastBuy1s:     make(map[string]*types.Signal),
		lastSell1s:    make(map[string]*types.Signal),
		strengthHist:  make(map[string]*strengthHistory),
	}

	// 订阅级别漂移事件（PRD §10.3 recast 流程）
	if bus != nil {
		id := bus.Subscribe(types.EventLevelRecast, e.onLevelRecast)
		e.subIDs = append(e.subIDs, id)
	}

	return e
}

// OnSignalInput 由 M3Bridge 在每次管道输出变更时调用。
func (e *SignalEngine) OnSignalInput(input *chanlun.SignalInput) {
	if input == nil {
		return
	}
	// 默认 L1（向后兼容，测试中未显式设置 Level 时）
	if input.Level == types.LevelL0 {
		input.Level = types.LevelL1
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.recognize(input.Symbol, input)
}

func (e *SignalEngine) recognize(symbol string, in *chanlun.SignalInput) {
	if len(in.TrendPatterns) == 0 || len(in.PivotZones) == 0 {
		// 即使无新走势/中枢，仍可检查已有信号的状态变更
		e.checkStateTransitions(symbol, in)
		return
	}

	e.logger.Debug("信号识别", "symbol", symbol,
		"trendPatterns", len(in.TrendPatterns),
		"pivotZones", len(in.PivotZones),
		"strokes", len(in.Strokes),
		"divergences", len(in.Divergences),
	)

	e.recognizeBuy1(symbol, in)
	e.recognizeSell1(symbol, in)
	e.recognizeBuy2(symbol, in)
	e.recognizeSell2(symbol, in)
	e.recognizeBuy3(symbol, in)
	e.recognizeSell3(symbol, in)

	// 检查已有候选信号是否可以状态流转
	e.checkStateTransitions(symbol, in)

	// 更新所有活跃信号的 strength（PRD §8.5）
	for _, sig := range e.activeSignals {
		if sig.Symbol == symbol {
			e.updateStrength(sig, in)
		}
	}
}

// checkStateTransitions 检查已有候选信号是否满足状态转换条件（PRD §8.3）。
//
// 每次 OnSignalInput 携带新的笔数据，借此判断：
//   - Buy1: 反弹笔形成 + 不创新低 → confirmed；创新低 → invalidated
//   - Sell1: 回落笔形成 + 不创新高 → confirmed；创新高 → invalidated
//   - Buy2: 回调笔完成且不破一买低点 → confirmed；破低点 → invalidated
//   - Sell2: 回调笔完成且不破一卖高点 → confirmed；破高点 → invalidated
//   - Buy3: 回调中出现次级别底背驰 → confirmed；跌破 ZG → invalidated
//   - Sell3: 回升中出现次级别顶背驰 → confirmed；升破 ZD → invalidated
func (e *SignalEngine) checkStateTransitions(symbol string, in *chanlun.SignalInput) {
	strokes := in.Strokes
	if len(strokes) < 2 {
		return
	}

	latest := strokes[len(strokes)-1]
	prev := strokes[len(strokes)-2]

	for _, sig := range e.activeSignals {
		if sig.Symbol != symbol || sig.State != types.SignalCandidate {
			continue
		}

		var newState types.SignalState
		switch sig.Type {
		case types.SignalBuy1:
			newState = e.evaluateBuy1Transition(sig, latest, prev)
		case types.SignalSell1:
			newState = e.evaluateSell1Transition(sig, latest, prev)
		case types.SignalBuy2:
			newState = e.evaluateBuy2Transition(sig, latest, prev, strokes)
		case types.SignalSell2:
			newState = e.evaluateSell2Transition(sig, latest, prev, strokes)
		case types.SignalBuy3:
			newState = e.evaluateBuy3Transition(sig, latest, in)
		case types.SignalSell3:
			newState = e.evaluateSell3Transition(sig, latest, in)
		}

		if newState != "" {
			e.transitionSignal(sig, newState)
		}
	}
}

// ====== 状态转换判定 ======

// evaluateBuy1Transition 一买状态转换：
//   - 确认：出现反向笔（向上）且其低点不低于信号价格（不创新低）
//   - 失效：最新向下笔的低点低于信号价格（创新低）
func (e *SignalEngine) evaluateBuy1Transition(sig *types.Signal, latest, prev chanlun.StrokeInfo) types.SignalState {
	// 最新笔方向向上（反弹笔形成）且 low >= 信号价格 → confirmed
	if latest.Direction == types.DirectionUp && latest.Low >= sig.Price {
		return types.SignalConfirmed
	}
	// 最新笔方向向下且 low < 信号价格 → invalidated
	if latest.Direction == types.DirectionDown && latest.Low < sig.Price {
		return types.SignalInvalidated
	}
	return ""
}

// evaluateSell1Transition 一卖状态转换：
//   - 确认：出现反向笔（向下）且其高点不高于信号价格（不创新高）
//   - 失效：最新向上笔的高点高于信号价格（创新高）
func (e *SignalEngine) evaluateSell1Transition(sig *types.Signal, latest, prev chanlun.StrokeInfo) types.SignalState {
	if latest.Direction == types.DirectionDown && latest.High <= sig.Price {
		return types.SignalConfirmed
	}
	if latest.Direction == types.DirectionUp && latest.High > sig.Price {
		return types.SignalInvalidated
	}
	return ""
}

// evaluateBuy2Transition 二买状态转换：
//   - 确认：自一买后的回调笔完成（方向从下转上）且 low > 一买价格
//   - 失效：最新笔 low ≤ 一买价格
func (e *SignalEngine) evaluateBuy2Transition(sig *types.Signal, latest, prev chanlun.StrokeInfo, strokes []chanlun.StrokeInfo) types.SignalState {
	if buy1, ok := e.lastBuy1s[sig.Symbol]; ok {
		if latest.Direction == types.DirectionUp && prev.Direction == types.DirectionDown {
			// 回调笔完成（下→上转换），且不破一买价格
			if prev.Low > buy1.Price {
				return types.SignalConfirmed
			}
		}
		// 最新向下笔破了历史低点（跳过回溯检查一买价格，只检查最新向下笔是否低于已确认的最低点）
		if latest.Direction == types.DirectionDown && latest.Low <= buy1.Price {
			return types.SignalInvalidated
		}
	}
	return ""
}

// evaluateSell2Transition 二卖状态转换（对称）：
//   - 确认：自一卖后的回调笔完成且 high < 一卖价格
//   - 失效：最新笔 high ≥ 一卖价格
func (e *SignalEngine) evaluateSell2Transition(sig *types.Signal, latest, prev chanlun.StrokeInfo, strokes []chanlun.StrokeInfo) types.SignalState {
	if sell1, ok := e.lastSell1s[sig.Symbol]; ok {
		if latest.Direction == types.DirectionDown && prev.Direction == types.DirectionUp {
			if prev.High < sell1.Price {
				return types.SignalConfirmed
			}
		}
		if latest.Direction == types.DirectionUp && latest.High >= sell1.Price {
			return types.SignalInvalidated
		}
	}
	return ""
}

// evaluateBuy3Transition 三买状态转换：
//   - 确认：回调笔中出现次级别底背驰
//   - 失效：回调笔低点跌破 ZG
func (e *SignalEngine) evaluateBuy3Transition(sig *types.Signal, latest chanlun.StrokeInfo, in *chanlun.SignalInput) types.SignalState {
	// 检查是否有底背驰
	for _, d := range in.Divergences {
		if d.Type == "bottomDivergence" && d.Confirmed {
			return types.SignalConfirmed
		}
	}
	// 检查是否跌破 ZG（失效位）
	if latest.Direction == types.DirectionDown && latest.EndPrice <= sig.Targets.InvalidationPrice {
		return types.SignalInvalidated
	}
	return ""
}

// evaluateSell3Transition 三卖状态转换：
//   - 确认：回升笔中出现次级别顶背驰
//   - 失效：回升笔高点升破 ZD（回到中枢内）
//
// 注意：失效判定针对新形成的笔，而非触发候选的同一根回调笔。
// 若最新笔方向向下（回调笔已结束、新下行笔开始）且其高点 ≥ ZD，则失效。
func (e *SignalEngine) evaluateSell3Transition(sig *types.Signal, latest chanlun.StrokeInfo, in *chanlun.SignalInput) types.SignalState {
	for _, d := range in.Divergences {
		if d.Type == "topDivergence" && d.Confirmed {
			return types.SignalConfirmed
		}
	}
	// 新下行笔的高点仍高于 ZD，说明回调笔结束后价格未回到中枢内
	if latest.Direction == types.DirectionDown && latest.High >= sig.Targets.InvalidationPrice {
		return types.SignalInvalidated
	}
	return ""
}

// transitionSignal 执行信号状态转换并发布事件。
func (e *SignalEngine) transitionSignal(sig *types.Signal, newState types.SignalState) {
	oldState := sig.State
	sig.State = newState

	if newState == types.SignalConfirmed {
		observability.M.RecordSignalConfirmed(sig.Symbol, sig.Type)
	} else if newState == types.SignalInvalidated {
		observability.M.RecordSignalInvalidated(sig.Symbol, sig.Type)
	}

	e.logger.Info("信号状态变更",
		"signalId", sig.SignalID,
		"type", sig.Type,
		"old", oldState,
		"new", newState,
	)

	if e.bus != nil {
		e.bus.Publish(types.Event{
			Type:   types.EventSignalStateChanged,
			Symbol: sig.Symbol,
			TS:     time.Now().UnixMilli(),
			Payload: types.SignalEventPayload{
				Signal:    sig,
				OldSignal: nil,
			},
		})
	}
}

// ========================================================================
// Recast 流程（PRD §10.3）
// ========================================================================

// onLevelRecast 处理级别漂移事件（PRD §10.3）。
//
// 当 M4 检测到双轨分歧时触发：
//   - candidate 信号 → 撤回（标 invalidated + recastFrom 指向原信号）
//   - confirmed 信号 → 不撤回但标 superseded + 发 recast 事件
//
// 当前简化实现：标记受影响信号，不创建新级别信号（待 M4 提供新级别数据后扩展）。
func (e *SignalEngine) onLevelRecast(evt types.Event) {
	payload, ok := evt.Payload.(types.LevelRecastEvent)
	if !ok {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	symbol := payload.Symbol
	e.logger.Warn("级别漂移处理",
		"symbol", symbol,
		"oldLevel", payload.OldLevel,
		"newLevel", payload.NewLevel,
	)

	for _, sig := range e.activeSignals {
		if sig.Symbol != symbol {
			continue
		}

		switch sig.State {
		case types.SignalCandidate:
			// PRD §10.3: candidate 撤回 → 标 invalidated + recastFrom
			oldID := sig.SignalID
			sig.State = types.SignalInvalidated
			sig.RecastRisk = 0.8 // 漂移信号 recast 风险大幅上调
			observability.M.RecordSignalInvalidated(sig.Symbol, sig.Type)

			e.logger.Info("信号因级别漂移撤回",
				"signalId", oldID,
				"type", sig.Type,
				"oldLevel", payload.OldLevel,
			)

			e.bus.Publish(types.Event{
				Type:   types.EventSignalStateChanged,
				Symbol: symbol,
				TS:     time.Now().UnixMilli(),
				Payload: types.SignalEventPayload{
					Signal: sig,
				},
			})

		case types.SignalConfirmed:
			// PRD §10.3: confirmed 不撤回 → 标 superseded + 发 recast 事件
			sig.State = types.SignalSuperseded
			sig.RecastRisk = 0.6
			observability.M.RecordSignalInvalidated(sig.Symbol, sig.Type)

			e.logger.Info("信号因级别漂移被取代",
				"signalId", sig.SignalID,
				"type", sig.Type,
				"oldLevel", payload.OldLevel,
			)

			// 发布 recast 事件（机器人可据此做仓位评估）
			e.bus.Publish(types.Event{
				Type:   types.EventSignalRecast,
				Symbol: symbol,
				TS:     time.Now().UnixMilli(),
				Payload: types.SignalEventPayload{
					Signal:     sig,
					RecastFrom: sig,
				},
			})
		}
	}
}

// ====== 辅助 ======

func latestTrendPattern(tps []chanlun.TrendPatternInfo) *chanlun.TrendPatternInfo {
	if len(tps) == 0 {
		return nil
	}
	return &tps[len(tps)-1]
}

func lastPivotZone(tp *chanlun.TrendPatternInfo, pzs []chanlun.PivotZoneInfo) *chanlun.PivotZoneInfo {
	if tp == nil || len(tp.PivotZoneIDs) == 0 || len(pzs) == 0 {
		return nil
	}
	lastID := tp.PivotZoneIDs[len(tp.PivotZoneIDs)-1]
	for i := range pzs {
		if pzs[i].Index == lastID {
			return &pzs[i]
		}
	}
	return &pzs[len(pzs)-1]
}

func latestConfirmedDivergenceByType(divs []chanlun.DivergenceInfo, typ string) *chanlun.DivergenceInfo {
	var last *chanlun.DivergenceInfo
	for i, d := range divs {
		if d.Type == typ && d.Confirmed {
			last = &divs[i]
		}
	}
	return last
}

func strokeAfter(strokes []chanlun.StrokeInfo, s *chanlun.StrokeInfo) *chanlun.StrokeInfo {
	for i, st := range strokes {
		if st.Index == s.Index && i+1 < len(strokes) {
			return &strokes[i+1]
		}
	}
	return nil
}

func strokeAfterPivotZone(strokes []chanlun.StrokeInfo, pz *chanlun.PivotZoneInfo) *chanlun.StrokeInfo {
	if pz.EndStrokeIdx+1 >= len(strokes) {
		return nil
	}
	return &strokes[pz.EndStrokeIdx+1]
}

// ====== 一买 ======

func (e *SignalEngine) recognizeBuy1(symbol string, in *chanlun.SignalInput) {
	if len(in.Divergences) == 0 {
		return
	}
	div := latestConfirmedDivergenceByType(in.Divergences, "bottomDivergence")
	if div == nil {
		return
	}
	tp := latestTrendPattern(in.TrendPatterns)
	if tp == nil {
		return
	}
	if tp.Type != "trend" || tp.Direction != types.DirectionDown || len(tp.PivotZoneIDs) < 2 {
		return
	}
	lastPZ := lastPivotZone(tp, in.PivotZones)
	if lastPZ == nil {
		return
	}
	if div.ExitPrice >= lastPZ.ZD {
		return
	}

	anchorKey := fmt.Sprintf("%s|%s|BUY_1|div_%d", symbol, in.Level, div.ExitEnd)
	if _, exists := e.anchorIndex[anchorKey]; exists {
		return
	}

	now := time.Now().UnixMilli()
	sig := &types.Signal{
		SignalID:    fmt.Sprintf("%s_BUY_1_%d", symbol, now),
		Symbol:      symbol,
		TS:          now,
		Type:        types.SignalBuy1,
		Level:       in.Level,
		Price:       div.ExitPrice,
		State:       types.SignalCandidate,
		Provisional: true,
		Confidence:  calcDivergenceConfidence(div, tp),
		Anchor: types.SignalAnchor{
			Kind:                  "divergenceSegment",
			DivergenceLineage:     fmt.Sprintf("L_%s_bi_%d", symbol, div.ExitEnd),
			PreviousStrokeLineage: fmt.Sprintf("L_%s_bi_%d", symbol, div.EntryEnd),
		},
		Evidence: types.Evidence{
			TrendDirection: "down",
			PivotZoneCount: len(tp.PivotZoneIDs),
			Divergence: &types.DivergenceEvidence{
				Method:       "priceRange",
				CurrentArea:  div.ExitMACD,
				PreviousArea: div.EntryMACD,
				Ratio:        div.Ratio,
				Threshold:    0.95,
			},
		},
		Targets: calcBuy1Targets(div.ExitPrice, lastPZ),
	}

	e.addSignal(sig, anchorKey)
	e.lastBuy1s[symbol] = sig
	e.logger.Info("一买信号", "symbol", symbol, "price", div.ExitPrice, "pivotZoneCount", len(tp.PivotZoneIDs), "divergenceRatio", div.Ratio)
}

// ====== 一卖 ======

func (e *SignalEngine) recognizeSell1(symbol string, in *chanlun.SignalInput) {
	if len(in.Divergences) == 0 {
		return
	}
	div := latestConfirmedDivergenceByType(in.Divergences, "topDivergence")
	if div == nil {
		return
	}
	tp := latestTrendPattern(in.TrendPatterns)
	if tp == nil {
		return
	}
	if tp.Type != "trend" || tp.Direction != types.DirectionUp || len(tp.PivotZoneIDs) < 2 {
		return
	}
	lastPZ := lastPivotZone(tp, in.PivotZones)
	if lastPZ == nil {
		return
	}
	if div.ExitPrice <= lastPZ.ZG {
		return
	}

	anchorKey := fmt.Sprintf("%s|%s|SELL_1|div_%d", symbol, in.Level, div.ExitEnd)
	if _, exists := e.anchorIndex[anchorKey]; exists {
		return
	}

	now := time.Now().UnixMilli()
	sig := &types.Signal{
		SignalID:    fmt.Sprintf("%s_SELL_1_%d", symbol, now),
		Symbol:      symbol,
		TS:          now,
		Type:        types.SignalSell1,
		Level:       in.Level,
		Price:       div.ExitPrice,
		State:       types.SignalCandidate,
		Provisional: true,
		Confidence:  calcDivergenceConfidence(div, tp),
		Anchor: types.SignalAnchor{
			Kind:                  "divergenceSegment",
			DivergenceLineage:     fmt.Sprintf("L_%s_bi_%d", symbol, div.ExitEnd),
			PreviousStrokeLineage: fmt.Sprintf("L_%s_bi_%d", symbol, div.EntryEnd),
		},
		Evidence: types.Evidence{
			TrendDirection: "up",
			PivotZoneCount: len(tp.PivotZoneIDs),
			Divergence: &types.DivergenceEvidence{
				Method:       "priceRange",
				CurrentArea:  div.ExitMACD,
				PreviousArea: div.EntryMACD,
				Ratio:        div.Ratio,
				Threshold:    0.95,
			},
		},
		Targets: calcSell1Targets(div.ExitPrice, lastPZ),
	}

	e.addSignal(sig, anchorKey)
	e.lastSell1s[symbol] = sig
}

// ====== 二买 ======

func (e *SignalEngine) recognizeBuy2(symbol string, in *chanlun.SignalInput) {
	buy1 := e.lastBuy1s[symbol]
	if buy1 == nil {
		return
	}
	var rebound *chanlun.StrokeInfo
	for i := range in.Strokes {
		st := &in.Strokes[i]
		if st.Direction == types.DirectionUp && st.StartPrice >= buy1.Price {
			rebound = st
			break
		}
	}
	if rebound == nil {
		return
	}
	pullback := strokeAfter(in.Strokes, rebound)
	if pullback == nil || pullback.Direction != types.DirectionDown {
		return
	}
	if pullback.EndPrice <= buy1.Price {
		return
	}

	anchorKey := fmt.Sprintf("%s|%s|BUY_2|dep_%s", symbol, in.Level, buy1.SignalID)
	if _, exists := e.anchorIndex[anchorKey]; exists {
		return
	}

	now := time.Now().UnixMilli()
	// 二买 target = 同一买的 target（前最后中枢上沿 ZG），PRD §8.7
	var buy2Target *float64
	if buy1.Targets.TargetPrice != nil {
		t := *buy1.Targets.TargetPrice
		buy2Target = &t
	}
	sig := &types.Signal{
		SignalID:    fmt.Sprintf("%s_BUY_2_%d", symbol, now),
		Symbol:      symbol,
		TS:          now,
		Type:        types.SignalBuy2,
		Level:       in.Level,
		Price:       pullback.EndPrice,
		State:       types.SignalCandidate,
		Provisional: true,
		Confidence:  0.7,
		Anchor: types.SignalAnchor{
			Kind:             "dependentBuyPoint",
			DependOnSignalID: buy1.SignalID,
			CurrentStrokeID:  fmt.Sprintf("L_%s_bi_%d", symbol, pullback.Index),
		},
		Evidence: types.Evidence{
			TrendDirection: "down",
			PivotZoneCount: 2,
		},
		Targets: types.SignalTargets{
			TargetPrice:        buy2Target,
			TargetSource:       "sameAsBuy1",
			InvalidationPrice:  buy1.Price, // 一买低点（跌破即二买失败）
			InvalidationSource: "buy1Low",
		},
	}
	e.addSignal(sig, anchorKey)
}

// ====== 二卖 ======

func (e *SignalEngine) recognizeSell2(symbol string, in *chanlun.SignalInput) {
	sell1 := e.lastSell1s[symbol]
	if sell1 == nil {
		return
	}
	var rebound *chanlun.StrokeInfo
	for i := range in.Strokes {
		st := &in.Strokes[i]
		if st.Direction == types.DirectionDown && st.StartPrice <= sell1.Price {
			rebound = st
			break
		}
	}
	if rebound == nil {
		return
	}
	pullback := strokeAfter(in.Strokes, rebound)
	if pullback == nil || pullback.Direction != types.DirectionUp {
		return
	}
	if pullback.EndPrice >= sell1.Price {
		return
	}

	anchorKey := fmt.Sprintf("%s|%s|SELL_2|dep_%s", symbol, in.Level, sell1.SignalID)
	if _, exists := e.anchorIndex[anchorKey]; exists {
		return
	}

	now := time.Now().UnixMilli()
	// 二卖 target = 同一卖的 target（前最后中枢下沿 ZD），PRD §8.7
	var sell2Target *float64
	if sell1.Targets.TargetPrice != nil {
		t := *sell1.Targets.TargetPrice
		sell2Target = &t
	}
	sig := &types.Signal{
		SignalID:    fmt.Sprintf("%s_SELL_2_%d", symbol, now),
		Symbol:      symbol,
		TS:          now,
		Type:        types.SignalSell2,
		Level:       in.Level,
		Price:       pullback.EndPrice,
		State:       types.SignalCandidate,
		Provisional: true,
		Confidence:  0.7,
		Anchor: types.SignalAnchor{
			Kind:             "dependentBuyPoint",
			DependOnSignalID: sell1.SignalID,
			CurrentStrokeID:  fmt.Sprintf("L_%s_bi_%d", symbol, pullback.Index),
		},
		Evidence: types.Evidence{
			TrendDirection: "up",
			PivotZoneCount: 2,
		},
		Targets: types.SignalTargets{
			TargetPrice:        sell2Target,
			TargetSource:       "sameAsSell1",
			InvalidationPrice:  sell1.Price, // 一卖高点（涨破即二卖失败）
			InvalidationSource: "sell1High",
		},
	}
	e.addSignal(sig, anchorKey)
}

// ====== 三买 ======

func (e *SignalEngine) recognizeBuy3(symbol string, in *chanlun.SignalInput) {
	for pi := range in.PivotZones {
		pz := &in.PivotZones[pi]
		if pz.Direction != types.DirectionUp {
			continue
		}
		leaveStroke := strokeAfterPivotZone(in.Strokes, pz)
		if leaveStroke == nil {
			continue
		}
		if leaveStroke.Direction != types.DirectionUp || leaveStroke.EndPrice <= pz.ZG {
			continue
		}
		pullback := strokeAfter(in.Strokes, leaveStroke)
		if pullback == nil || pullback.Direction != types.DirectionDown {
			continue
		}
		if pullback.Low <= pz.ZG {
			continue // 回抽跌回中枢（不满足三买条件）
		}
		anchorKey := fmt.Sprintf("%s|%s|BUY_3|pz_%d_pb_%d", symbol, in.Level, pz.Index, pullback.Index)
		if _, exists := e.anchorIndex[anchorKey]; exists {
			continue
		}

		now := time.Now().UnixMilli()
		sig := &types.Signal{
			SignalID:    fmt.Sprintf("%s_BUY_3_%d", symbol, now),
			Symbol:      symbol,
			TS:          now,
			Type:        types.SignalBuy3,
			Level:       in.Level,
			Price:       pullback.EndPrice,
			State:       types.SignalCandidate,
			Provisional: true,
			Confidence:  0.75, // PRD §8.4: BUY_3 base = 0.75
			Anchor: types.SignalAnchor{
				Kind:                "pivotZoneBreakout",
				DependOnPivotZoneID: fmt.Sprintf("L_%s_pz_%d", symbol, pz.Index),
				CurrentStrokeID:     fmt.Sprintf("L_%s_bi_%d", symbol, pullback.Index),
			},
			Evidence: types.Evidence{
				TrendDirection: "up",
				PivotZoneCount: 1,
			},
			Targets: types.SignalTargets{
				// PRD §8.7: 三买无结构性 target（null），invalidation = 突破中枢的上沿 ZG
				InvalidationPrice:  pz.ZG,
				InvalidationSource: "breakoutPivotZoneZG",
			},
		}
		e.addSignal(sig, anchorKey)
	}

}

// ====== 三卖 ======

func (e *SignalEngine) recognizeSell3(symbol string, in *chanlun.SignalInput) {
	for pi := range in.PivotZones {
		pz := &in.PivotZones[pi]
		if pz.Direction != types.DirectionDown {
			continue
		}
		leaveStroke := strokeAfterPivotZone(in.Strokes, pz)
		if leaveStroke == nil {
			continue
		}
		if leaveStroke.Direction != types.DirectionDown || leaveStroke.EndPrice >= pz.ZD {
			continue
		}
		pullback := strokeAfter(in.Strokes, leaveStroke)
		if pullback == nil || pullback.Direction != types.DirectionUp {
			continue
		}
		if pullback.High >= pz.ZD {
			continue // 回抽涨回中枢（不满足三卖条件）
		}
		anchorKey := fmt.Sprintf("%s|%s|SELL_3|pz_%d_pb_%d", symbol, in.Level, pz.Index, pullback.Index)
		if _, exists := e.anchorIndex[anchorKey]; exists {
			continue
		}

		now := time.Now().UnixMilli()
		sig := &types.Signal{
			SignalID:    fmt.Sprintf("%s_SELL_3_%d", symbol, now),
			Symbol:      symbol,
			TS:          now,
			Type:        types.SignalSell3,
			Level:       in.Level,
			Price:       pullback.EndPrice,
			State:       types.SignalCandidate,
			Provisional: true,
			Confidence:  0.75, // PRD §8.4: SELL_3 base = 0.75
			Anchor: types.SignalAnchor{
				Kind:                "pivotZoneBreakout",
				DependOnPivotZoneID: fmt.Sprintf("L_%s_pz_%d", symbol, pz.Index),
				CurrentStrokeID:     fmt.Sprintf("L_%s_bi_%d", symbol, pullback.Index),
			},
			Evidence: types.Evidence{
				TrendDirection: "down",
				PivotZoneCount: 1,
			},
			Targets: types.SignalTargets{
				// PRD §8.7: 三卖无结构性 target（null），invalidation = 突破中枢的下沿 ZD
				InvalidationPrice:  pz.ZD,
				InvalidationSource: "breakoutPivotZoneZD",
			},
		}
		e.addSignal(sig, anchorKey)
	}

}

// ====== 公共方法 ======

func (e *SignalEngine) GetActiveSignals(symbol string) []*types.Signal {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var result []*types.Signal
	for _, s := range e.activeSignals {
		if s.Symbol == symbol {
			result = append(result, s)
		}
	}
	return result
}

func (e *SignalEngine) GetSignal(signalID string) *types.Signal {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.activeSignals[signalID]
}

// QuerySignals 查询信号列表，支持按级别、状态过滤。
//
// PRD §11.1: GET /v1/signals/{symbol}?level=L1&state=confirmed&limit=20
// 返回该 symbol 下所有匹配的信号，按时间降序排列。
func (e *SignalEngine) QuerySignals(symbol string, levelFilter types.Level, stateFilter types.SignalState, limit int) []*types.Signal {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var filtered []*types.Signal
	for _, s := range e.activeSignals {
		if s.Symbol != symbol {
			continue
		}
		if levelFilter != 0 && s.Level != levelFilter {
			continue
		}
		if stateFilter != "" && s.State != stateFilter {
			continue
		}
		filtered = append(filtered, s)
	}

	// 按 TS 降序排列（最新在前）
	for i := 0; i < len(filtered); i++ {
		for j := i + 1; j < len(filtered); j++ {
			if filtered[j].TS > filtered[i].TS {
				filtered[i], filtered[j] = filtered[j], filtered[i]
			}
		}
	}

	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}

	return filtered
}

// OnStructureChange 兼容旧接口（空实现）。
func (e *SignalEngine) OnStructureChange(level types.Level, state *types.DualTrackState) {}

// ====== 内部辅助 ======

func (e *SignalEngine) addSignal(sig *types.Signal, anchorKey string) {
	e.activeSignals[sig.SignalID] = sig
	e.anchorIndex[anchorKey] = sig
	e.logger.Debug("信号添加", "signalId", sig.SignalID, "type", sig.Type, "price", sig.Price)

	observability.M.RecordSignalCreated(sig.Symbol, sig.Type, sig.Level)

	// 发布信号创建事件（M6 共振引擎消费）
	if e.bus != nil {
		e.bus.Publish(types.Event{
			Type:   types.EventSignalCreated,
			Symbol: sig.Symbol,
			TS:     time.Now().UnixMilli(),
			Payload: types.SignalEventPayload{
				Signal: sig,
			},
		})
	}
}

func calcDivergenceConfidence(div *chanlun.DivergenceInfo, tp *chanlun.TrendPatternInfo) float64 {
	// PRD §8.4: base(type) — 缠论可操作点分类清单的可确定性评级
	var base float64
	if tp.Direction == types.DirectionDown {
		base = 0.60 // BUY_1
	} else {
		base = 0.60 // SELL_1
	}
	// 二买/三买/二卖/三卖有更高的 base，但此函数供一买/一卖调用

	// PRD §8.4: divergenceStrength — MACD面积比越小背驰越强
	// ratio < 0.5 视为背驰极强 → 1.0
	// ratio ∈ [0.5, 1.0] → 线性映射到 [1.0, 0.0]
	var divergenceStrength float64
	if div.Ratio <= 0.5 {
		divergenceStrength = 1.0
	} else if div.Ratio >= 1.0 {
		divergenceStrength = 0.0
	} else {
		// ratio 0.5→1.0, divergenceStrength 1.0→0.0
		divergenceStrength = 1.0 - (div.Ratio-0.5)/0.5
	}

	// PRD §8.4: base × divergenceStrength（directionAlignment × nestingDepth 由 M6 共振引擎贡献）
	confidence := base * divergenceStrength
	if confidence > 1.0 {
		confidence = 1.0
	}
	return confidence
}

func calcBuy1Targets(price float64, lastPZ *chanlun.PivotZoneInfo) types.SignalTargets {
	target := lastPZ.ZG // PRD §8.7: 前最后中枢上沿 ZG
	return types.SignalTargets{
		TargetPrice:        &target,
		TargetSource:       "lastPivotZoneZG",
		InvalidationPrice:  price, // 一买低点（跌破即背驰被打破）
		InvalidationSource: "buy1Low",
	}
}

func calcSell1Targets(price float64, lastPZ *chanlun.PivotZoneInfo) types.SignalTargets {
	target := lastPZ.ZD // PRD §8.7: 前最后中枢下沿 ZD
	return types.SignalTargets{
		TargetPrice:        &target,
		TargetSource:       "lastPivotZoneZD",
		InvalidationPrice:  price, // 一卖高点（涨破即背驰被打破）
		InvalidationSource: "sell1High",
	}
}

// ========================================================================
// Strength 计算（PRD §8.5 — 因子演化法）
// ========================================================================

// updateStrength 更新指定信号的 strength 值。
//
// strength = w1×divergenceTrendScore + w2×confirmationScore
//   - w3×pullbackScore + w4×sublevelScore
//
// 默认权重 w1~w4 各 0.25（均分），可后续回测标定。
func (e *SignalEngine) updateStrength(sig *types.Signal, in *chanlun.SignalInput) {
	// 获取/创建强度历史
	hist := e.strengthHist[sig.SignalID]
	if hist == nil {
		hist = &strengthHistory{}
		e.strengthHist[sig.SignalID] = hist
	}

	// 因子 1: divergenceRatioTrend（PRD §8.5）
	// 从背驰信号中获取最新的 MACD 面积比
	divTrendScore := e.calcDivergenceTrendScore(sig, in, hist)

	// 因子 2: consecutiveConfirmations（连续确认次数）
	hist.confirmCount++
	if hist.confirmCount > 100 {
		hist.confirmCount = 100 // 上限保护
	}
	confirmationScore := float64(hist.confirmCount) / 5.0
	if confirmationScore > 1.0 {
		confirmationScore = 1.0
	}

	// 因子 3: pullbackDepthRatio（仅二买/三买/二卖/三卖适用）
	pullbackScore := e.calcPullbackScore(sig, in)

	// 因子 4: sublevelSignalEvolving（次级别信号是否同步演化）
	sublevelScore := e.calcSublevelScore(sig, in)

	// 加权合成（默认权重均分 0.25）
	const w1, w2, w3, w4 = 0.25, 0.25, 0.25, 0.25
	strength := w1*divTrendScore + w2*confirmationScore + w3*pullbackScore + w4*sublevelScore

	// 更新 signal
	sig.Strength = strength
	sig.Evidence.StrengthFactors = types.StrengthFactors{
		DivergenceRatioTrend:     divergenceTrendToString(divTrendScore),
		ConsecutiveConfirmations: hist.confirmCount,
		PullbackDepthRatio:       calcPullbackDepthRatio(sig, in),
		SublevelSignalEvolving:   sublevelScore > 0.5,
	}

	e.logger.Debug("strength 更新",
		"signalId", sig.SignalID,
		"strength", strength,
		"divTrend", divTrendScore,
		"confirmCount", hist.confirmCount,
		"pullback", pullbackScore,
		"sublevel", sublevelScore,
	)
}

// calcDivergenceTrendScore 计算背驰面积比的收敛趋势得分。
func (e *SignalEngine) calcDivergenceTrendScore(sig *types.Signal, in *chanlun.SignalInput, hist *strengthHistory) float64 {
	// 从输入中查找与信号同类型的背驰
	var latestRatio float64
	found := false
	switch sig.Type {
	case types.SignalBuy1, types.SignalBuy2, types.SignalBuy3:
		if d := latestConfirmedDivergenceByType(in.Divergences, "bottomDivergence"); d != nil {
			latestRatio = d.Ratio
			found = true
		}
	case types.SignalSell1, types.SignalSell2, types.SignalSell3:
		if d := latestConfirmedDivergenceByType(in.Divergences, "topDivergence"); d != nil {
			latestRatio = d.Ratio
			found = true
		}
	}

	if !found && sig.Evidence.Divergence != nil {
		latestRatio = sig.Evidence.Divergence.Ratio
	} else if !found {
		return 0.5 // 无法判定，返回中等值
	}

	// 记录到历史
	hist.ratioHistory = append(hist.ratioHistory, latestRatio)
	if len(hist.ratioHistory) > 10 {
		hist.ratioHistory = hist.ratioHistory[len(hist.ratioHistory)-10:]
	}

	// 根据历史趋势判定收敛/发散/震荡
	if len(hist.ratioHistory) < 3 {
		return 0.5 // 样本不足，中等
	}

	// 检查最近 N 个值的趋势
	recent := hist.ratioHistory
	firstHalf := avg(recent[:len(recent)/2])
	secondHalf := avg(recent[len(recent)/2:])

	diff := firstHalf - secondHalf
	threshold := 0.02 // 2% 变化视为有趋势

	if diff > threshold {
		// 早期比后期大 → 持续缩小 → converging
		return 1.0
	} else if diff < -threshold {
		// 早期比后期小 → 持续扩大 → diverging
		return 0.0
	}
	return 0.5 // oscillating
}

// hasPivotZoneTarget 判断信号是否有中枢级别的 target（一买/二买/一卖/二卖）。
// 这些信号用回调深度来衡量。
func (e *SignalEngine) hasPivotZoneTarget(sig *types.Signal) bool {
	switch sig.Type {
	case types.SignalBuy1, types.SignalBuy2, types.SignalSell1, types.SignalSell2:
		return true
	default:
		return false
	}
}

// calcPullbackScore 计算回调深度得分。
//
// 对于有中枢 target 的信号（一买/二买/一卖/二卖），
// pullbackDepthRatio = 当前价格到 target 的距离 / target 到失效位的距离。
// 浅回调 → 高分（接近确认）。
func (e *SignalEngine) calcPullbackScore(sig *types.Signal, in *chanlun.SignalInput) float64 {
	if !e.hasPivotZoneTarget(sig) {
		// 三买/三卖无 target，无法计算回调深度
		return 0.5
	}

	ratio := calcPullbackDepthRatio(sig, in)
	// 浅回调 = 高分：score = 1 - ratio
	score := 1.0 - ratio
	if score < 0 {
		score = 0
	}
	if score > 1.0 {
		score = 1.0
	}
	return score
}

// calcSublevelScore 计算次级别信号同步演化得分。
func (e *SignalEngine) calcSublevelScore(sig *types.Signal, in *chanlun.SignalInput) float64 {
	// 检查是否有次级别的同向信号正在演化
	// 简化实现：如果背驰信号已确认但本信号尚未 confirmed，视为次级别在演化
	if sig.State == types.SignalCandidate {
		// 检查输入中是否有新确认的背驰
		switch sig.Type {
		case types.SignalBuy1, types.SignalBuy2, types.SignalBuy3:
			if d := latestConfirmedDivergenceByType(in.Divergences, "bottomDivergence"); d != nil {
				return 1.0
			}
		case types.SignalSell1, types.SignalSell2, types.SignalSell3:
			if d := latestConfirmedDivergenceByType(in.Divergences, "topDivergence"); d != nil {
				return 1.0
			}
		}
	}
	return 0.0
}

// calcPullbackDepthRatio 计算回调深度比例（PRD §8.5）。
func calcPullbackDepthRatio(sig *types.Signal, in *chanlun.SignalInput) float64 {
	if sig.Targets.TargetPrice == nil {
		return 0.5
	}
	target := *sig.Targets.TargetPrice
	invalidation := sig.Targets.InvalidationPrice

	// 计算当前价格相对于 target 和 invalidation 的位置
	var latestPrice float64
	if len(in.Strokes) > 0 {
		latestPrice = in.Strokes[len(in.Strokes)-1].EndPrice
	} else {
		latestPrice = sig.Price
	}

	totalRange := abs(target - invalidation)
	if totalRange < 0.0001 {
		return 0.5
	}

	currentDist := abs(latestPrice - invalidation)
	ratio := currentDist / totalRange
	if ratio > 1.0 {
		ratio = 1.0
	}
	return ratio
}

// divergenceTrendToString 将趋势得分转为字符串标签。
func divergenceTrendToString(score float64) string {
	if score > 0.7 {
		return "converging"
	} else if score < 0.3 {
		return "diverging"
	}
	return "oscillating"
}

// avg 计算 float64 切片的平均值。
func avg(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

// abs 返回 float64 的绝对值。
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
