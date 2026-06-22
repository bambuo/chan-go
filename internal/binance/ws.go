// Package binance 提供订阅币安K线流的客户端，基于 ccxt/go-binance SDK。
package binance

import (
	"log/slog"
	"strconv"

	"trade/internal/eventbus"
	"trade/internal/log"
	"trade/internal/types"

	"github.com/adshao/go-binance/v2"
	"github.com/shopspring/decimal"
)

// WSClient 管理币安K线WebSocket流，将SDK事件转发到事件总线。
type WSClient struct {
	symbols  []string
	interval string
	bus      *eventbus.Bus

	stopC chan struct{} // 通知 SDK 停止。
	doneC chan struct{} // SDK 通知我们已完成。

	logger *slog.Logger
}

// NewWSClient 创建新的WebSocket客户端。
// symbols: 交易对符号（小写），例如 []string{"btcusdt", "ethusdt"}
// interval: K线时间间隔，例如 "1m"
// bus: 事件总线
func NewWSClient(symbols []string, interval string, bus *eventbus.Bus) *WSClient {
	return &WSClient{
		symbols:  symbols,
		interval: interval,
		bus:      bus,
		logger:   log.Component("binance.ws"),
	}
}

// Start 开始订阅K线流，阻塞直到连接断开或被 Stop 终止。
func (c *WSClient) Start() error {
	// 构建 symbol → interval 映射。
	pairs := make(map[string]string, len(c.symbols))
	for _, sym := range c.symbols {
		pairs[sym] = c.interval
	}

	// SDK 内部处理心跳、重连等。
	doneC, stopC, err := binance.WsCombinedKlineServe(
		pairs,
		c.wsHandler(),
		c.errHandler(),
	)
	if err != nil {
		return err
	}

	c.doneC = doneC
	c.stopC = stopC

	c.logger.Info("WebSocket已连接", "symbols", c.symbols, "interval", c.interval)

	// 阻塞直到流结束。
	<-doneC
	return nil
}

// Stop 优雅关闭WebSocket连接。
func (c *WSClient) Stop() {
	c.logger.Info("WebSocket客户端正在停止...")
	if c.stopC != nil {
		close(c.stopC)
	}
}

// wsHandler 返回 SDK K线事件处理器。
func (c *WSClient) wsHandler() binance.WsKlineHandler {
	return func(event *binance.WsKlineEvent) {
		kl := convert(event)
		eventType := types.EventKlineRealtime
		if kl.IsClosed {
			eventType = types.EventKlineClosed
		}

		c.bus.Publish(types.KlineEvent{
			Type:  eventType,
			Kline: kl,
		})

		c.logger.Debug("K线事件已发布",
			"symbol", kl.Symbol,
			"event_type", eventType,
			"close", kl.Close,
		)
	}
}

// errHandler 返回 SDK 错误处理器。
func (c *WSClient) errHandler() binance.ErrHandler {
	return func(err error) {
		c.logger.Error("WebSocket错误", "error", err)
	}
}

// convert 将 SDK 的 WsKlineEvent 转换为内部 Kline 类型。
func convert(event *binance.WsKlineEvent) *types.Kline {
	k := &event.Kline
	return &types.Kline{
		Symbol:    event.Symbol,
		Open:      safeDecimal(k.Open),
		High:      safeDecimal(k.High),
		Low:       safeDecimal(k.Low),
		Close:     safeDecimal(k.Close),
		Volume:    safeDecimal(k.Volume),
		OpenTime:  k.StartTime,
		CloseTime: k.EndTime,
		IsClosed:  k.IsFinal,
	}
}

// safeDecimal 将 SDK 的字符串价格安全转为 decimal.Decimal。
func safeDecimal(s string) decimal.Decimal {
	if s == "" {
		return decimal.Zero
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		// 极端兜底：尝试 float64 解析。
		f, err2 := strconv.ParseFloat(s, 64)
		if err2 != nil {
			return decimal.Zero
		}
		return decimal.NewFromFloat(f)
	}
	return d
}

// 导出 SDK 常量用于文档
const (
	DefaultWSURL = "wss://stream.binance.com:9443"
)
