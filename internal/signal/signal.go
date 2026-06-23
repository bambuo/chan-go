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

// SignalEngine 信号引擎。
type SignalEngine struct {
	logger *slog.Logger

	mu sync.RWMutex

	bus *eventbus.GenericBus // 用于发布信号事件（M6 共振引擎订阅）

	activeSignals map[string]*types.Signal
	anchorIndex   map[string]*types.Signal

	lastBuy1s  map[string]*types.Signal
	lastSell1s map[string]*types.Signal
}

// New 创建信号引擎。
// bus 可选，若提供则信号创建/变更时会发布事件供共振引擎消费。
func New(bus *eventbus.GenericBus) *SignalEngine {
	return &SignalEngine{
		logger:        log.Component("m5.signal"),
		bus:           bus,
		activeSignals: make(map[string]*types.Signal),
		anchorIndex:   make(map[string]*types.Signal),
		lastBuy1s:     make(map[string]*types.Signal),
		lastSell1s:    make(map[string]*types.Signal),
	}
}

// OnSignalInput 由 M3Bridge 在每次管道输出变更时调用。
func (e *SignalEngine) OnSignalInput(input *chanlun.SignalInput) {
	if input == nil {
		return
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
//	 - 确认：自一买后的回调笔完成（方向从下转上）且 low > 一买价格
//	 - 失效：最新笔 low ≤ 一买价格
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
//	 - 确认：自一卖后的回调笔完成且 high < 一卖价格
//	 - 失效：最新笔 high ≥ 一卖价格
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
//   - 失效：回升笔高点升破 ZD
func (e *SignalEngine) evaluateSell3Transition(sig *types.Signal, latest chanlun.StrokeInfo, in *chanlun.SignalInput) types.SignalState {
	for _, d := range in.Divergences {
		if d.Type == "topDivergence" && d.Confirmed {
			return types.SignalConfirmed
		}
	}
	if latest.Direction == types.DirectionUp && latest.EndPrice >= sig.Targets.InvalidationPrice {
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

// ====== 辅助 ======

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
	if div.Price2 >= lastPZ.ZD {
		return
	}

	anchorKey := fmt.Sprintf("%s|%s|BUY_1|div_%d", symbol, types.LevelL1, div.Stroke2Idx)
	if _, exists := e.anchorIndex[anchorKey]; exists {
		return
	}

	now := time.Now().UnixMilli()
	sig := &types.Signal{
		SignalID:    fmt.Sprintf("%s_BUY_1_%d", symbol, now),
		Symbol:      symbol,
		TS:          now,
		Type:        types.SignalBuy1,
		Level:       types.LevelL1,
		Price:       div.Price2,
		State:       types.SignalCandidate,
		Provisional: true,
		Confidence:  calcDivergenceConfidence(div, tp),
		Anchor: types.SignalAnchor{
			Kind:                  "divergenceSegment",
			DivergenceLineage:     fmt.Sprintf("L_%s_bi_%d", symbol, div.Stroke2Idx),
			PreviousStrokeLineage: fmt.Sprintf("L_%s_bi_%d", symbol, div.Stroke1Idx),
		},
		Evidence: types.Evidence{
			TrendDirection: "down",
			PivotZoneCount: len(tp.PivotZoneIDs),
			Divergence: &types.DivergenceEvidence{
				Method:       "priceRange",
				CurrentArea:  div.Strength2,
				PreviousArea: div.Strength1,
				Ratio:        div.Ratio,
				Threshold:    0.95,
			},
		},
		Targets: calcBuy1Targets(div.Price2, lastPZ),
	}

	e.addSignal(sig, anchorKey)
	e.lastBuy1s[symbol] = sig
	e.logger.Info("一买信号", "symbol", symbol, "price", div.Price2, "pivotZoneCount", len(tp.PivotZoneIDs), "divergenceRatio", div.Ratio)
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
	if div.Price2 <= lastPZ.ZG {
		return
	}

	anchorKey := fmt.Sprintf("%s|%s|SELL_1|div_%d", symbol, types.LevelL1, div.Stroke2Idx)
	if _, exists := e.anchorIndex[anchorKey]; exists {
		return
	}

	now := time.Now().UnixMilli()
	sig := &types.Signal{
		SignalID:    fmt.Sprintf("%s_SELL_1_%d", symbol, now),
		Symbol:      symbol,
		TS:          now,
		Type:        types.SignalSell1,
		Level:       types.LevelL1,
		Price:       div.Price2,
		State:       types.SignalCandidate,
		Provisional: true,
		Confidence:  calcDivergenceConfidence(div, tp),
		Anchor: types.SignalAnchor{
			Kind:                  "divergenceSegment",
			DivergenceLineage:     fmt.Sprintf("L_%s_bi_%d", symbol, div.Stroke2Idx),
			PreviousStrokeLineage: fmt.Sprintf("L_%s_bi_%d", symbol, div.Stroke1Idx),
		},
		Evidence: types.Evidence{
			TrendDirection: "up",
			PivotZoneCount: len(tp.PivotZoneIDs),
			Divergence: &types.DivergenceEvidence{
				Method:       "priceRange",
				CurrentArea:  div.Strength2,
				PreviousArea: div.Strength1,
				Ratio:        div.Ratio,
				Threshold:    0.95,
			},
		},
		Targets: calcSell1Targets(div.Price2, lastPZ),
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

	anchorKey := fmt.Sprintf("%s|%s|BUY_2|dep_%s", symbol, types.LevelL1, buy1.SignalID)
	if _, exists := e.anchorIndex[anchorKey]; exists {
		return
	}

	now := time.Now().UnixMilli()
	sig := &types.Signal{
		SignalID:    fmt.Sprintf("%s_BUY_2_%d", symbol, now),
		Symbol:      symbol,
		TS:          now,
		Type:        types.SignalBuy2,
		Level:       types.LevelL1,
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
			TargetPrice:       &rebound.EndPrice,
			TargetSource:      "reboundHigh",
			InvalidationPrice: buy1.Price,
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

	anchorKey := fmt.Sprintf("%s|%s|SELL_2|dep_%s", symbol, types.LevelL1, sell1.SignalID)
	if _, exists := e.anchorIndex[anchorKey]; exists {
		return
	}

	now := time.Now().UnixMilli()
	sig := &types.Signal{
		SignalID:    fmt.Sprintf("%s_SELL_2_%d", symbol, now),
		Symbol:      symbol,
		TS:          now,
		Type:        types.SignalSell2,
		Level:       types.LevelL1,
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
			TargetPrice:       &rebound.EndPrice,
			InvalidationPrice: sell1.Price,
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
		if pullback.Low >= pz.ZG {
			continue
		}
		anchorKey := fmt.Sprintf("%s|%s|BUY_3|pz_%d_pb_%d", symbol, types.LevelL1, pz.Index, pullback.Index)
		if _, exists := e.anchorIndex[anchorKey]; exists {
			continue
		}

		now := time.Now().UnixMilli()
		sig := &types.Signal{
			SignalID:    fmt.Sprintf("%s_BUY_3_%d", symbol, now),
			Symbol:      symbol,
			TS:          now,
			Type:        types.SignalBuy3,
			Level:       types.LevelL1,
			Price:       pullback.EndPrice,
			State:       types.SignalCandidate,
			Provisional: true,
			Confidence:  0.65,
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
				TargetPrice:       &leaveStroke.EndPrice,
				InvalidationPrice: pullback.Low,
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
		if pullback.High <= pz.ZD {
			continue
		}
		anchorKey := fmt.Sprintf("%s|%s|SELL_3|pz_%d_pb_%d", symbol, types.LevelL1, pz.Index, pullback.Index)
		if _, exists := e.anchorIndex[anchorKey]; exists {
			continue
		}

		now := time.Now().UnixMilli()
		sig := &types.Signal{
			SignalID:    fmt.Sprintf("%s_SELL_3_%d", symbol, now),
			Symbol:      symbol,
			TS:          now,
			Type:        types.SignalSell3,
			Level:       types.LevelL1,
			Price:       pullback.EndPrice,
			State:       types.SignalCandidate,
			Provisional: true,
			Confidence:  0.65,
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
				TargetPrice:       &leaveStroke.EndPrice,
				InvalidationPrice: pullback.High,
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
	base := 0.5
	if div.Ratio < 0.5 {
		base += 0.3
	} else if div.Ratio < 0.7 {
		base += 0.2
	} else if div.Ratio < 0.9 {
		base += 0.1
	}
	if len(tp.PivotZoneIDs) >= 3 {
		base += 0.1
	}
	if base > 1.0 {
		base = 1.0
	}
	return base
}

func calcBuy1Targets(price float64, lastPZ *chanlun.PivotZoneInfo) types.SignalTargets {
	target := lastPZ.ZD
	return types.SignalTargets{
		TargetPrice:       &target,
		TargetSource:      "lastPivotZoneZD",
		InvalidationPrice: price * 0.95,
	}
}

func calcSell1Targets(price float64, lastPZ *chanlun.PivotZoneInfo) types.SignalTargets {
	target := lastPZ.ZG
	return types.SignalTargets{
		TargetPrice:       &target,
		TargetSource:      "lastPivotZoneZG",
		InvalidationPrice: price * 1.05,
	}
}
