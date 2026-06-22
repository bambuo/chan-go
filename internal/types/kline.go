// Package types 定义K线数据和缠论分析的共享数据结构。
package types

import "github.com/shopspring/decimal"

// KlineEventType 事件总线的事件类型分类。
type KlineEventType string

const (
	// EventKlineClosed 币安推送闭合的1分钟K线时触发。
	EventKlineClosed KlineEventType = "kline.closed"
	// EventKlineRealtime 每次K线实时更新（未闭合）时触发。
	EventKlineRealtime KlineEventType = "kline.realtime"
)

// Kline 表示来自币安的标准K线数据点。
type Kline struct {
	Symbol    string          `json:"symbol"`
	Open      decimal.Decimal `json:"open"`
	High      decimal.Decimal `json:"high"`
	Low       decimal.Decimal `json:"low"`
	Close     decimal.Decimal `json:"close"`
	Volume    decimal.Decimal `json:"volume"`
	OpenTime  int64           `json:"open_time"`  // Unix毫秒
	CloseTime int64           `json:"close_time"` // Unix毫秒
	IsClosed  bool            `json:"is_closed"`  // 该K线是否已闭合
}

// Clone 返回Kline的深拷贝。
func (k *Kline) Clone() *Kline {
	return &Kline{
		Symbol:    k.Symbol,
		Open:      k.Open,
		High:      k.High,
		Low:       k.Low,
		Close:     k.Close,
		Volume:    k.Volume,
		OpenTime:  k.OpenTime,
		CloseTime: k.CloseTime,
		IsClosed:  k.IsClosed,
	}
}

// KlineEvent 包装Kline用于事件总线分发。
type KlineEvent struct {
	Type  KlineEventType
	Kline *Kline
}

// ChanDirection 表示包含处理中识别的方向。
type ChanDirection int

const (
	DirectionUp   ChanDirection = 1
	DirectionDown ChanDirection = -1
	DirectionNone ChanDirection = 0
)

func (d ChanDirection) String() string {
	switch d {
	case DirectionUp:
		return "up"
	case DirectionDown:
		return "down"
	default:
		return "none"
	}
}

// ChanKline 经缠论包含处理后的K线元素。
// 同时保存原始数据和算法所需的计算引用。
type ChanKline struct {
	High       float64       // 包含处理后的高点
	Low        float64       // 包含处理后的低点
	RawHigh    float64       // 包含处理前的原始高点
	RawLow     float64       // 包含处理前的原始低点
	OpenTime   int64         // Unix毫秒
	CloseTime  int64         // Unix毫秒
	Direction  ChanDirection // 相对于前一个元素的方向
	Contained  bool          // 该元素是否被前一个元素包含（合并）
	MergedFrom int           // 合并入该元素的原始K线数量（>=1）
}

// FractalType 标识分型类型。
type FractalType int

const (
	FractalNone   FractalType = 0
	FractalTop    FractalType = 1 // 顶分型
	FractalBottom FractalType = 2 // 底分型
)

func (f FractalType) String() string {
	switch f {
	case FractalTop:
		return "top"
	case FractalBottom:
		return "bottom"
	default:
		return "none"
	}
}

// Fractal 表示缠论中的一个分型。
type Fractal struct {
	Type     FractalType `json:"type"`
	Index    int         `json:"index"`     // 在非包含K线序列中的索引
	High     float64     `json:"high"`      // 中间元素的高点
	Low      float64     `json:"low"`       // 中间元素的低点
	OpenTime int64       `json:"open_time"` // 中间元素的时间
	// Confirmed 为true时表示后续K线已确认该分型。
	Confirmed bool `json:"confirmed"`
}
