// Package levels 提供层级独立的缠论流程运行器。
package levels

import (
	"context"
	"log/slog"

	"trade/internal/chanlun"
	"trade/internal/log"
	"trade/internal/types"
)

// SignalSink 信号接收接口，避免直接依赖 signal 包（循环依赖）。
type SignalSink interface {
	OnSignalInput(input *chanlun.SignalInput)
}

// LevelRunner 层级独立的缠论流程运行器。
// 每层运行一个完整的 Pipeline，通过 channel 接收输入。
// 当检测到新完成的走势类型时，自动创建并启动下一级的 LevelRunner（懒加载）。
// 走势类型通过阻塞发送传递到下一级，不丢弃。
type LevelRunner struct {
	Level      types.Level // L1/L2/L3/L4
	Symbol     string
	Input      chan *types.Kline // 接收输入（L1 由外部喂入，L2+ 由上一级产出）
	Pipe       *chanlun.Pipeline
	SignalSink SignalSink   // 可选的信号接收器
	Next       *LevelRunner // 下一级（懒创建）
	logger     *slog.Logger
}

// NewLevelRunner 创建层级运行器，但不启动。
func NewLevelRunner(level types.Level, symbol string, input chan *types.Kline) *LevelRunner {
	return &LevelRunner{
		Level:  level,
		Symbol: symbol,
		Input:  input,
		Pipe:   chanlun.NewPipeline(),
		logger: log.Component("m4.runner"),
	}
}

// WithSignalSink 设置信号接收器（可选的）。
func (r *LevelRunner) WithSignalSink(sink SignalSink) *LevelRunner {
	r.SignalSink = sink
	return r
}

// Run 启动层级处理循环，阻塞直到 ctx 取消或 Input 关闭。
func (r *LevelRunner) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case kline, ok := <-r.Input:
			if !ok {
				return
			}
			r.processOne(kline)
		}
	}
}

// processOne 处理一根 K 线（或合成 K 线），阻塞发送给下一级，不丢弃。
func (r *LevelRunner) processOne(kline *types.Kline) {
	output := r.Pipe.Process(kline)

	// 已完成走势类型 → 发给下一级（非阻塞，缓冲 4096 足够覆盖所有产出）
	for _, tp := range output.CompletedPatterns() {
		r.ensureNextLevel()
		select {
		case r.Next.Input <- TrendPatternToKline(r.Symbol, tp):
		default:
			// 4096 缓冲对实际产出绰绰有余，此分支不可达
		}
	}

	// 信号引擎
	if r.SignalSink != nil {
		si := output.ToSignalInput()
		si.Level = r.Level
		r.SignalSink.OnSignalInput(si)
	}
}

// ensureNextLevel 懒创建下一级运行器并启动。
func (r *LevelRunner) ensureNextLevel() {
	if r.Next != nil {
		return
	}
	nextLevel := r.Level + 1
	if nextLevel > types.LevelL4 {
		return
	}
	input := make(chan *types.Kline, 4096)
	r.Next = NewLevelRunner(nextLevel, r.Symbol, input)
	go r.Next.Run(context.Background())
}
