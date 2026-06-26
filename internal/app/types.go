// Package app 负责系统组装与生命周期管理。
package app

// KLine 表示一根 K 线数据。
type KLine struct {
	// Symbol 是交易对名称，如 BTCUSDT。
	Symbol string `json:"symbol"`
	// Open 是开盘价。
	Open float64 `json:"open"`
	// High 是最高价。
	High float64 `json:"high"`
	// Low 是最低价。
	Low float64 `json:"low"`
	// Close 是收盘价。
	Close float64 `json:"close"`
	// Volume 是成交量。
	Volume float64 `json:"volume"`
	// Timestamp 是 K 线时间戳（毫秒）。
	Timestamp int64 `json:"ts"`
}

// MergedKLine 表示包含处理后的合并 K 线。
type MergedKLine struct {
	// High 是合并后的最高价。
	High float64 `json:"high"`
	// Low 是合并后的最低价。
	Low float64 `json:"low"`
	// Direction 是合并 K 线的方向：1 表示向上，-1 表示向下。
	Direction int `json:"direction"`
	// Fractal 是分型类型：1 表示顶分型，-1 表示底分型，0 表示未知。
	Fractal int `json:"fractal"`
	// Index 是合并 K 线在序列中的索引。
	Index int `json:"index"`
}
