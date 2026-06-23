// Package chanlun 实现背驰判定算法（背驰判定算法.md）。
//
// 背驰 = 价格创新高/低的同时，对应的动力（MACD 面积/均线面积）未能同步。
// 当前简化实现：用价格区间 × 成交量作为动力的近似替代。
package chanlun

import (
	"math"
	"sync"

	"trade/internal/types"
)

// divergence 背驰判定结果。
type divergence struct {
	Type       string  // "topDivergence"(顶背驰) / "bottomDivergence"(底背驰)
	Stroke1Idx int     // 前一笔索引
	Stroke2Idx int     // 当前笔索引
	Price1     float64 // 前一个极值
	Price2     float64 // 当前极值
	Strength1  float64 // 前一段强度（MACD面积替代）
	Strength2  float64 // 当前段强度
	Ratio      float64 // 强度比 (Strength2/Strength1)
	Confirmed  bool    // Ratio < 0.95 时确认
}

// divergenceState 背驰判定状态。
type divergenceState struct {
	mu              sync.Mutex
	strokes         []*stroke     // 所有笔
	divergences     []*divergence // 已识别的背驰
	processedStroke int           // 已处理的笔索引
}

// DivergenceProcessor 背驰判定处理器。
type DivergenceProcessor struct {
	states map[string]*divergenceState
	mu     sync.Mutex
}

// NewDivergenceProcessor 创建背驰判定处理器。
func NewDivergenceProcessor() *DivergenceProcessor {
	return &DivergenceProcessor{
		states: make(map[string]*divergenceState),
	}
}

// Process 增量处理笔列表，返回新增的背驰信号。
func (bcp *DivergenceProcessor) Process(symbol string, strokes []*stroke) []*divergence {
	st := bcp.getState(symbol)
	st.mu.Lock()
	defer st.mu.Unlock()

	st.strokes = strokes

	// 比较相邻的同向笔（隔一根反向笔，如第 0 笔与第 2 笔）。
	// 在严格交替的笔序列 [up, down, up, down, ...] 中：
	//   idx=2: strokes[0] vs strokes[2]  (同为 up)
	//   idx=3: strokes[1] vs strokes[3]  (同为 down)
	//   idx=4: strokes[2] vs strokes[4]  (同为 up)
	//   ...
	//
	// 背驰条件：价格创新高/低的同时，强度（MACD 面积替代）未能同步。
	for st.processedStroke+2 < len(strokes) {
		st.processedStroke++
		idx := st.processedStroke
		if idx < 2 {
			continue
		}

		// 获取相邻的两根同向笔（隔一根反向笔）
		s1 := strokes[idx-2]
		s2 := strokes[idx]

		if s1.Direction != s2.Direction {
			continue
		}

		// 检查价格是否创新高（顶背驰）或新低（底背驰）
		var priceHigher bool
		if s2.Direction == types.DirectionUp {
			priceHigher = s2.EndPrice > s1.EndPrice
		} else {
			priceHigher = s2.EndPrice < s1.EndPrice
		}
		if !priceHigher {
			continue
		}

		str1 := strokeStrength(s1)
		str2 := strokeStrength(s2)

		ratio := str2 / str1
		isDiverged := ratio < 0.95

		bcType := "topDivergence"
		if s2.Direction == types.DirectionDown {
			bcType = "bottomDivergence"
		}

		bc := &divergence{
			Type:       bcType,
			Stroke1Idx: idx - 2,
			Stroke2Idx: idx,
			Price1:     s1.EndPrice,
			Price2:     s2.EndPrice,
			Strength1:  str1,
			Strength2:  str2,
			Ratio:      ratio,
			Confirmed:  isDiverged,
		}
		st.divergences = append(st.divergences, bc)
	}

	return st.divergences
}

// ReprocessFrom 从指定笔索引开始重算背驰（PRD §10.4.3 回溯修正）。
func (bcp *DivergenceProcessor) ReprocessFrom(symbol string, fromIdx int) {
	st := bcp.getState(symbol)
	st.mu.Lock()
	defer st.mu.Unlock()

	// 删除从 fromIdx 之后的背驰
	var kept []*divergence
	for _, d := range st.divergences {
		if d.Stroke2Idx < fromIdx {
			kept = append(kept, d)
		}
	}
	st.divergences = kept

	// 重置已处理笔索引，允许重算
	resetIdx := fromIdx - 2
	if resetIdx < 0 {
		resetIdx = 0
	}
	st.processedStroke = resetIdx
}

// Load 返回所有背驰信号。
func (bcp *DivergenceProcessor) Load(symbol string) []*divergence {
	st := bcp.getState(symbol)
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.divergences
}

// Reset 重置。
func (bcp *DivergenceProcessor) Reset(symbol string) {
	bcp.mu.Lock()
	defer bcp.mu.Unlock()
	delete(bcp.states, symbol)
}

// getState 获取或创建指定 symbol 的背驰判定状态。
func (bcp *DivergenceProcessor) getState(symbol string) *divergenceState {
	bcp.mu.Lock()
	defer bcp.mu.Unlock()
	if s, ok := bcp.states[symbol]; ok {
		return s
	}
	s := &divergenceState{}
	bcp.states[symbol] = s
	return s
}

// strokeStrength 计算一根笔的"强度"（近似 MACD 面积）。
func strokeStrength(s *stroke) float64 {
	range_ := math.Abs(s.EndPrice - s.StartPrice)
	return range_ * float64(1)
}

// divergenceToEvidence 转为输出结构。
func divergenceToEvidence(bc *divergence) map[string]interface{} {
	return map[string]interface{}{
		"type":      bc.Type,
		"price1":    bc.Price1,
		"price2":    bc.Price2,
		"ratio":     bc.Ratio,
		"confirmed": bc.Confirmed,
	}
}
