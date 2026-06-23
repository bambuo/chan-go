// Package chanlun 提供缠论核心算法的 symbol 级处理管道（M2 → M3 桥梁）。
//
// Pipeline 整合 ContainProcessor + FractalProcessor 为按 symbol 管理的管道，
// 每根新 K 线经包含处理 → 分型识别，产出结构化结果供 M3 结构树提交。
package chanlun

import (
	"fmt"
	"sync"

	"trade/internal/types"
)

// PipelineOutput 一次管道处理的结果。
type PipelineOutput struct {
	Symbol           string             // 交易对
	NewElements      []*types.ChanKline // 本次新增的非包含元素
	NewFractals      []types.Fractal    // 本次新增的已确认分型
	AllElements      []*types.ChanKline // 当前全部非包含元素
	AllFractals      []types.Fractal    // 当前全部已确认分型
	Strokes          []*stroke          // 当前所有确认笔
	NewStrokes       []*stroke          // 本次新增的确认笔
	Segments         []*segment         // 当前所有线段
	NewSegments      []*segment         // 本次新增的线段
	PivotZones       []*pivotZone       // 当前所有中枢
	NewPivotZones    []*pivotZone       // 本次新增的中枢
	TrendPatterns    []*trendPattern    // 当前所有走势类型
	NewTrendPatterns []*trendPattern    // 本次新增的走势类型
	Divergences      []*divergence      // 当前所有背驰信号
	NewDivergences   []*divergence      // 本次新增的背驰信号
	TotalKlines      int                // 已处理的原始 K 线总数
	LastOpenTime     int64              // 最后处理 K 线的 OpenTime
	HasChange        bool               // 本此是否有实质变更
}

// Pipeline 是 symbol 级别的缠论处理管道。
// 每 symbol 一个独立实例，维护自己的容器-分型状态机。
type Pipeline struct {
	mu      sync.Mutex
	symbols map[string]*symbolState
}

// symbolState 单个 symbol 的处理状态。
type symbolState struct {
	symbol                     string
	contain                    *ContainProcessor      // K线包含处理器
	fractal                    *FractalProcessor      // 分型识别处理器
	stroke                     *StrokeProcessor       // 笔识别处理器
	segment                    *SegmentProcessor      // 线段划分处理器
	pivotZone                  *PivotZoneProcessor    // 中枢识别处理器
	trendPattern               *TrendPatternProcessor // 走势类型分类处理器
	divergence                 *DivergenceProcessor   // 背驰判定处理器
	totalKlines                int                    // 已处理的原始 K 线总数
	lastOpenTime               int64                  // 最后处理 K 线的 OpenTime
	lastCommittedElementN      int                    // 上次提交到 M3 时的非包含元素数
	lastCommittedFractalN      int                    // 上次提交到 M3 时的已确认分型数
	lastCommittedStrokeN       int                    // 上次提交到 M3 时的笔数
	lastCommittedSegN          int                    // 上次提交到 M3 时的线段数
	lastCommittedPivotZoneN    int                    // 上次提交到 M3 时的中枢数
	lastCommittedTrendPatternN int                    // 上次提交到 M3 时的走势类型数
	lastCommittedDivergenceN   int                    // 上次提交到 M3 时的背驰信号数
}

// NewPipeline 创建新的管道。
func NewPipeline() *Pipeline {
	return &Pipeline{
		symbols: make(map[string]*symbolState),
	}
}

// GetOrCreate 获取或创建指定 symbol 的管道状态。
func (p *Pipeline) GetOrCreate(symbol string) *symbolState {
	p.mu.Lock()
	defer p.mu.Unlock()

	if s, ok := p.symbols[symbol]; ok {
		return s
	}
	s := &symbolState{
		symbol:       symbol,
		contain:      NewContainProcessor(),
		fractal:      NewFractalProcessor(),
		stroke:       NewStrokeProcessor(),
		segment:      NewSegmentProcessor(),
		pivotZone:    NewPivotZoneProcessor(),
		trendPattern: NewTrendPatternProcessor(),
		divergence:   NewDivergenceProcessor(),
	}
	p.symbols[symbol] = s
	return s
}

// Reset 重置指定 symbol 的处理状态。
func (p *Pipeline) Reset(symbol string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if s, ok := p.symbols[symbol]; ok {
		s.contain.Reset()
		s.fractal.Reset()
		s.stroke.Reset(symbol)
		s.segment.Reset(symbol)
		s.pivotZone.Reset(symbol)
		s.trendPattern.Reset(symbol)
		s.divergence.Reset(symbol)
		s.totalKlines = 0
		s.lastOpenTime = 0
		s.lastCommittedElementN = 0
		s.lastCommittedFractalN = 0
		s.lastCommittedStrokeN = 0
		s.lastCommittedSegN = 0
		s.lastCommittedPivotZoneN = 0
		s.lastCommittedTrendPatternN = 0
		s.lastCommittedDivergenceN = 0
	}
}

// Process 处理一根原始 K 线，返回管道输出。
//
// 处理步骤：
//  1. 包含处理：原始 K 线 → 非包含元素序列
//  2. 增量分型识别：新非包含元素 → 分型列表
//  3. 计算增量变更：新元素 + 新分型
//  4. 返回 PipelineOutput
//
// goroutine 安全。
func (p *Pipeline) Process(raw *types.Kline) *PipelineOutput {
	state := p.GetOrCreate(raw.Symbol)
	return state.process(raw)
}

// symbolState.process 处理单根 K 线。
func (s *symbolState) process(raw *types.Kline) *PipelineOutput {
	s.contain.Process(raw)
	s.totalKlines++

	// 获取当前非包含元素
	elements := s.contain.Elements()
	s.lastOpenTime = raw.OpenTime

	// 获取新增的非包含元素
	oldN := s.lastCommittedElementN
	newElements := []*types.ChanKline{}
	if len(elements) > oldN {
		newElements = elements[oldN:]
	}

	// 增量分型识别：用新的非包含元素逐个输入分型处理器
	for _, elem := range newElements {
		s.fractal.Process(elem)
	}

	// 获取当前所有已确认分型
	allFractals := s.fractal.Fractals()

	// 增量笔识别：用所有当前元素触发笔状态机
	// 不能只传 newElements——分型识别在 FractalProcessor 中延迟设置 FractalType，
	// 历史元素可能在当前循环中才第一次被标记为分型
	for _, elem := range elements {
		_ = s.stroke.Process(s.symbol, elem, allFractals)
	}

	// 获取新增的已确认分型
	oldFractalN := s.lastCommittedFractalN
	newFractals := []types.Fractal{}
	if len(allFractals) > oldFractalN {
		newFractals = allFractals[oldFractalN:]
	}

	// 获取当前所有确认笔
	allStrokes := s.stroke.Strokes(raw.Symbol)
	oldStrokeN := s.lastCommittedStrokeN
	newStrokes := []*stroke{}
	if len(allStrokes) > oldStrokeN {
		newStrokes = allStrokes[oldStrokeN:]
	}

	// 增量线段识别：用新确认的笔触发线段状态机
	if len(newStrokes) > 0 {
		s.segment.Process(s.symbol, allStrokes)
	}

	// 获取当前所有线段
	allSegments := s.segment.CurrentSegments(s.symbol)
	oldSegN := s.lastCommittedSegN
	newSegments := []*segment{}
	if len(allSegments) > oldSegN {
		newSegments = allSegments[oldSegN:]
	}

	// 增量中枢识别：用新确认的笔触发中枢状态机
	if len(newStrokes) > 0 {
		s.pivotZone.Process(s.symbol, allStrokes)
	}

	// 获取当前所有中枢
	allPivotZones := s.pivotZone.Load(s.symbol)
	oldPivotZoneN := s.lastCommittedPivotZoneN
	newPivotZones := []*pivotZone{}
	if len(allPivotZones) > oldPivotZoneN {
		newPivotZones = allPivotZones[oldPivotZoneN:]
	}

	// 走势类型分类：基于中枢序列
	if len(newPivotZones) > 0 {
		s.trendPattern.Process(s.symbol, allStrokes, allPivotZones)
	}

	// 获取所有走势类型
	allTrendPatterns := s.trendPattern.Load(s.symbol)
	oldTrendPatternN := s.lastCommittedTrendPatternN
	newTrendPatterns := []*trendPattern{}
	if len(allTrendPatterns) > oldTrendPatternN {
		newTrendPatterns = allTrendPatterns[oldTrendPatternN:]
	}

	// 背驰判定：基于笔序列
	if len(newStrokes) > 0 {
		s.divergence.Process(s.symbol, allStrokes)
	}

	// 获取所有背驰信号
	allDivergences := s.divergence.Load(s.symbol)
	oldDivergenceN := s.lastCommittedDivergenceN
	newDivergences := []*divergence{}
	if len(allDivergences) > oldDivergenceN {
		newDivergences = allDivergences[oldDivergenceN:]
	}

	// 更新提交水位
	s.lastCommittedElementN = len(elements)
	s.lastCommittedFractalN = len(allFractals)
	s.lastCommittedStrokeN = len(allStrokes)
	s.lastCommittedSegN = len(allSegments)
	s.lastCommittedPivotZoneN = len(allPivotZones)
	s.lastCommittedTrendPatternN = len(allTrendPatterns)
	s.lastCommittedDivergenceN = len(allDivergences)

	hasChange := len(newElements) > 0 || len(newFractals) > 0 || len(newStrokes) > 0 || len(newSegments) > 0 || len(newPivotZones) > 0 || len(newTrendPatterns) > 0 || len(newDivergences) > 0

	return &PipelineOutput{
		Symbol:           s.symbol,
		NewElements:      newElements,
		NewFractals:      newFractals,
		AllElements:      elements,
		AllFractals:      allFractals,
		Strokes:          allStrokes,
		NewStrokes:       newStrokes,
		Segments:         allSegments,
		NewSegments:      newSegments,
		PivotZones:       allPivotZones,
		NewPivotZones:    newPivotZones,
		TrendPatterns:    allTrendPatterns,
		NewTrendPatterns: newTrendPatterns,
		Divergences:      allDivergences,
		NewDivergences:   newDivergences,
		TotalKlines:      s.totalKlines,
		LastOpenTime:     raw.OpenTime,
		HasChange:        hasChange,
	}
}

// GetState 返回指定 symbol 的当前状态（不含增量信息，只读快照）。
func (p *Pipeline) GetState(symbol string) *PipelineOutput {
	p.mu.Lock()
	defer p.mu.Unlock()

	s, ok := p.symbols[symbol]
	if !ok {
		return &PipelineOutput{Symbol: symbol}
	}

	return &PipelineOutput{
		Symbol:        s.symbol,
		AllElements:   s.contain.Elements(),
		AllFractals:   s.fractal.Fractals(),
		Strokes:       s.stroke.Strokes(symbol),
		Segments:      s.segment.CurrentSegments(symbol),
		PivotZones:    s.pivotZone.Load(symbol),
		TrendPatterns: s.trendPattern.Load(symbol),
		Divergences:   s.divergence.Load(symbol),
		TotalKlines:   s.totalKlines,
		LastOpenTime:  s.lastOpenTime,
	}
}

// elementID 为指定元素生成稳定的版本内 ID。
func (s *symbolState) elementID(elemType string, index int) string {
	return fmt.Sprintf("%s_%s_%d", s.symbol, elemType, index)
}

// lineageID 为指定元素生成稳定的跨版本 lineageId。
func (s *symbolState) lineageID(elemType string, index int) string {
	return fmt.Sprintf("L_%s_%s_%d", s.symbol, elemType, index)
}
