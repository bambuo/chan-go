// Package levels 提供层级独立的缠论流程运行器。
package levels

import (
	"time"

	"github.com/shopspring/decimal"

	"trade/internal/types"
)

// TrendPatternToKline 将一个已完成的走势类型转换为合成 K 线，
// 供下一级 Pipeline 作为输入。
func TrendPatternToKline(symbol string, tp types.TrendPattern) *types.Kline {
	now := time.Now().UnixMilli()
	return &types.Kline{
		Symbol:    symbol,
		Open:      decimal.NewFromFloat(tp.StartPrice),
		High:      decimal.NewFromFloat(tp.High),
		Low:       decimal.NewFromFloat(tp.Low),
		Close:     decimal.NewFromFloat(tp.EndPrice),
		OpenTime:  now,
		CloseTime: now + 1,
		IsClosed:  true,
	}
}
