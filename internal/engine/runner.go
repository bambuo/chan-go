package engine

import (
	"context"
	"sync"

	"trade/internal/chanlun"
	"trade/internal/logger"
)

// LevelRunner 是级别运行器。
//
// 每个级别一个独立协程，通过缓冲 channel 连接。
// L1 由 App 注入原始 K 线，
// L2+ 由 L(N-1) 完成趋势时按需创建并注入趋势 K 线。
//
// 每个级别内部：
//
//	输入队列 → Pipeline 流式处理 → OutputPipe → Redis
type LevelRunner struct {
	depth      int
	symbol     string
	pipeline   *chanlun.Pipeline
	outputPipe *OutputPipe
	input      chan *chanlun.KLine // 缓冲 128
	nextLevel  *LevelRunner
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	log        *logger.Logger // 当前级别的日志器（含 module/L）
	baseLog    *logger.Logger // 原始日志器（不含级别字段，用于创建子级）
}

// NewLevelRunner 创建一个级别运行器。
func NewLevelRunner(depth int, symbol string, store *ResultStore, log *logger.Logger) *LevelRunner {
	return &LevelRunner{
		depth:      depth,
		symbol:     symbol,
		pipeline:   chanlun.NewPipeline(),
		outputPipe: NewOutputPipe(symbol, depth, store),
		input:      make(chan *chanlun.KLine, 128),
		nextLevel:  nil,
		log:        log.With("module", "level_runner", "L", depth),
		baseLog:    log,
	}
}

// Start 在独立协程中启动运行器的主循环。
func (r *LevelRunner) Start(ctx context.Context) {
	ctx, r.cancel = context.WithCancel(ctx)
	r.wg.Add(1)
	go r.runLoop(ctx)
	r.log.Info("级别运行器已启动")
}

// Stop 停止运行器及其所有子级别。
func (r *LevelRunner) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
	r.outputPipe.Stop()
	r.wg.Wait()
	if r.nextLevel != nil {
		r.nextLevel.Stop()
	}
	r.log.Info("级别运行器已停止")
}

// Enqueue 向输入队列投递 K 线（线程安全）。
func (r *LevelRunner) Enqueue(kline *chanlun.KLine) {
	select {
	case r.input <- kline:
	default:
		// 队列满时丢弃——背压由上游控制
	}
}

// GetDepth 返回当前深度。
func (r *LevelRunner) GetDepth() int {
	return r.depth
}

// runLoop 是运行器主循环。
func (r *LevelRunner) runLoop(ctx context.Context) {
	defer r.wg.Done()
	r.log.Info("开始处理")

	for {
		select {
		case <-ctx.Done():
			r.log.Info("级别运行器收到停止信号")
			return
		case kline, ok := <-r.input:
			if !ok {
				return
			}

			// 流式处理（内部已推入 OutputPipe）
			result := r.pipeline.Process(kline, r.outputPipe)

			// 检查完成趋势 → 级别升级
			if result.HasCompletedTrend {
				trendKline := r.buildTrendKline(result)
				next := r.getOrCreateNextLevel(ctx)
				next.Enqueue(trendKline)
			}
		}
	}
}

// getOrCreateNextLevel 按需创建下一级运行器。
func (r *LevelRunner) getOrCreateNextLevel(ctx context.Context) *LevelRunner {
	if r.nextLevel == nil {
		r.nextLevel = NewLevelRunner(r.depth+1, r.symbol, r.outputPipe.store, r.baseLog)
		r.nextLevel.Start(ctx)
		r.log.Info("级别升级")
	}
	return r.nextLevel
}

// buildTrendKline 从完成走势构造趋势 K 线。
func (r *LevelRunner) buildTrendKline(result *chanlun.ProcessResult) *chanlun.KLine {
	high := result.TrendHigh
	low := result.TrendLow
	if high < low {
		high, low = low, high
	}
	return &chanlun.KLine{
		Symbol:    result.Symbol,
		OpenTime:  result.Time,
		CloseTime: result.Time,
		Open:      high,
		High:      high,
		Low:       low,
		Close:     low,
		Volume:    0,
		IsClosed:  true,
	}
}
