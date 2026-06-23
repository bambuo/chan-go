// Package m9_backtest 回测引擎（M9）。
//
// 职责（PRD §13）：
//   - 向 Redis Stream 灌历史 K 线
//   - 逐根回放（与生产同代码路径）
//   - 信号统计（胜率/盈亏比/假信号率/recast 率）
//   - 参数标定（§13.4 confidence/strength 权重优化）
package backtest

import (
	"log/slog"
	"time"

	"trade/internal/log"
	"trade/internal/types"
)

// BacktestEngine 回测引擎。
type BacktestEngine struct {
	logger *slog.Logger
}

// Report 回测报告（PRD §13.2）。
type Report struct {
	Symbol    string  `json:"symbol"`
	StartTS   int64   `json:"startTs"`
	EndTS     int64   `json:"endTs"`
	KlineCount int    `json:"klineCount"`

	TotalSignals    int     `json:"totalSignals"`
	WinRate         float64 `json:"winRate"`
	ProfitRatio     float64 `json:"profitRatio"` // 盈亏比
	FalseSignalRate float64 `json:"falseSignalRate"`
	RecastRate      float64 `json:"recastRate"`
	ConfirmDelayP50 float64 `json:"confirmDelayP50"`
	ConfirmDelayP90 float64 `json:"confirmDelayP90"`
	ResonanceGain   float64 `json:"resonanceGain"`
}

// New 创建回测引擎。
func New() *BacktestEngine {
	return &BacktestEngine{
		logger: log.Component("m9.backtest"),
	}
}

// Run 运行回测。
// 将历史 K 线逐根注入到引擎，采集信号后计算统计指标。
func (e *BacktestEngine) Run(symbol string, klines []*types.Kline) (*Report, error) {
	start := time.Now()
	e.logger.Info("开始回测", "symbol", symbol, "klineCount", len(klines))

	// TODO: 实际实现
	// 1. 创建临时引擎实例（M1~M6）
	// 2. 逐根按 ts 顺序注入 K 线
	// 3. 采集所有信号事件
	// 4. 计算统计指标

	_ = start
	return &Report{
		Symbol:    symbol,
		StartTS:   klines[0].OpenTime,
		EndTS:     klines[len(klines)-1].OpenTime,
		KlineCount: len(klines),
	}, nil
}

// Calibrate 运行参数标定（PRD §13.4）。
func (e *BacktestEngine) Calibrate(symbol string, klines []*types.Kline) error {
	// TODO: 实际实现
	// 1. 用初始默认参数跑回测
	// 2. 优化 w1~w4 等参数使排序一致性最大化
	// 3. 在另一段数据验证
	return nil
}
