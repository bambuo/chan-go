// Package binance 提供用于订阅币安K线流的WebSocket客户端。
package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"trade/internal/eventbus"
	"trade/internal/log"
	"trade/internal/types"

	"github.com/gorilla/websocket"
	"github.com/shopspring/decimal"
)

const (
	// defaultBinanceWSURL 是币安WebSocket市场流的基础端点。
	defaultBinanceWSURL = "wss://stream.binance.com:9443/ws"

	// pingInterval 客户端发送ping帧的间隔。
	pingInterval = 3 * time.Minute

	// pongWait 等待pong响应的最长时间，超过则认为连接已死。
	pongWait = 10 * time.Minute

	// writeWait 控制帧的写入截止时间。
	writeWait = 10 * time.Second

	// reconnectDelay 重连的基础延迟。
	reconnectDelay = 1 * time.Second

	// maxReconnectDelay 指数退避的上限。
	maxReconnectDelay = 30 * time.Second
)

// WSClient 管理币安市场流的WebSocket连接。
type WSClient struct {
	url      string
	symbols  []string
	interval string
	bus      *eventbus.Bus

	conn   *websocket.Conn
	mu     sync.RWMutex
	cancel context.CancelFunc
	wg     sync.WaitGroup

	logger *slog.Logger

	// reconnectCount 用于退避计算。
	reconnectCount int
}

// WSClientOption 配置WSClient的可选项。
type WSClientOption func(*WSClient)

// WithWSURL 设置自定义WebSocket URL。
func WithWSURL(url string) WSClientOption {
	return func(c *WSClient) {
		c.url = url
	}
}

// NewWSClient 创建新的币安WebSocket客户端。
// symbols: 交易对符号（小写），例如 []string{"btcusdt", "ethusdt"}
// interval: K线时间间隔，例如 "1m"
// bus: 用于发布K线事件的事件总线
func NewWSClient(symbols []string, interval string, bus *eventbus.Bus, opts ...WSClientOption) *WSClient {
	c := &WSClient{
		url:      defaultBinanceWSURL,
		symbols:  symbols,
		interval: interval,
		bus:      bus,
		logger:   log.Component("binance.ws"),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Start 建立WebSocket连接并开始接收流数据。
// 阻塞直到context被取消。
func (c *WSClient) Start(ctx context.Context) error {
	ctx, c.cancel = context.WithCancel(ctx)
	defer func() { c.cancel = nil }()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := c.connect(ctx); err != nil {
			c.logger.Error("连接失败", "error", err)
			c.waitReconnect(ctx)
			continue
		}

		c.reconnectCount = 0
		c.wg.Add(1)
		err := c.readPump(ctx)
		c.wg.Done()

		if err != nil {
			c.logger.Error("读取循环异常退出", "error", err)
		} else {
			c.logger.Info("读取循环正常退出")
		}

		// 重连前清理旧连接。
		c.closeConn()

		if ctx.Err() != nil {
			return ctx.Err()
		}

		c.waitReconnect(ctx)
	}
}

// connect 建立WebSocket连接并订阅K线流。
func (c *WSClient) connect(ctx context.Context) error {
	streams := make([]string, 0, len(c.symbols))
	for _, sym := range c.symbols {
		streams = append(streams, fmt.Sprintf("%s@kline_%s", sym, c.interval))
	}

	// 订阅多个流时使用组合流URL。
	url := c.url
	if len(streams) > 1 {
		combinedPath := "/stream?streams="
		for i, s := range streams {
			if i > 0 {
				combinedPath += "/"
			}
			combinedPath += s
		}
		url = "wss://stream.binance.com:9443" + combinedPath
	} else if len(streams) == 1 {
		url = c.url + "/" + streams[0]
	}

	c.logger.Info("正在连接币安WebSocket", "url", url)

	dialer := websocket.DefaultDialer
	conn, _, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		return fmt.Errorf("websocket拨号: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	c.logger.Info("WebSocket已连接", "symbols", c.symbols, "interval", c.interval)
	return nil
}

// readPump 持续从WebSocket连接读取消息。
func (c *WSClient) readPump(ctx context.Context) error {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("连接为空")
	}

	// 配置pong处理器。
	if err := conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		return fmt.Errorf("设置读取截止时间: %w", err)
	}
	conn.SetPongHandler(func(string) error {
		c.mu.RLock()
		defer c.mu.RUnlock()
		if c.conn != nil {
			return c.conn.SetReadDeadline(time.Now().Add(pongWait))
		}
		return nil
	})

	// 启动协程定期发送ping。
	pingTicker := time.NewTicker(pingInterval)
	defer pingTicker.Stop()

	pingDone := make(chan struct{})
	defer close(pingDone)

	go func() {
		for {
			select {
			case <-pingTicker.C:
				c.mu.RLock()
				conn := c.conn
				c.mu.RUnlock()
				if conn != nil {
					if err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(writeWait)); err != nil {
						c.logger.Warn("ping写入失败", "error", err)
						return
					}
				}
			case <-pingDone:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("读取消息: %w", err)
		}

		c.processMessage(message)
	}
}

// processMessage 解析原始WebSocket消息并发布事件。
func (c *WSClient) processMessage(data []byte) {
	// 先尝试解析为组合流包装格式。
	var combined struct {
		Stream string          `json:"stream"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &combined); err == nil && combined.Stream != "" {
		c.handleKlineMessage(combined.Data)
		return
	}

	// 尝试解析为原始流负载。
	c.handleKlineMessage(data)
}

// klinePayload 币安K线WebSocket负载的原始结构。
// 使用 decimal.Decimal 兼容字符串和数字两种JSON格式。
type klinePayload struct {
	EventType string `json:"e"`
	EventTime int64  `json:"E"`
	Symbol    string `json:"s"`
	Kline     struct {
		OpenTime  int64           `json:"t"`
		CloseTime int64           `json:"T"`
		Symbol    string          `json:"s"`
		Interval  string          `json:"i"`
		Open      decimal.Decimal `json:"o"`
		Close     decimal.Decimal `json:"c"`
		High      decimal.Decimal `json:"h"`
		Low       decimal.Decimal `json:"l"`
		Volume    decimal.Decimal `json:"v"`
		IsClosed  bool            `json:"x"`
	} `json:"k"`
}

// handleKlineMessage 解析K线消息并发布事件。
func (c *WSClient) handleKlineMessage(data []byte) {
	var raw klinePayload
	if err := json.Unmarshal(data, &raw); err != nil {
		c.logger.Warn("解析K线负载失败", "error", err)
		return
	}

	if raw.EventType != "kline" {
		return
	}

	kl := &types.Kline{
		Symbol:    raw.Symbol,
		Open:      raw.Kline.Open,
		High:      raw.Kline.High,
		Low:       raw.Kline.Low,
		Close:     raw.Kline.Close,
		Volume:    raw.Kline.Volume,
		OpenTime:  raw.Kline.OpenTime,
		CloseTime: raw.Kline.CloseTime,
		IsClosed:  raw.Kline.IsClosed,
	}

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
		"close_time", kl.CloseTime,
	)
}

// closeConn 安全关闭WebSocket连接。
func (c *WSClient) closeConn() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		_ = c.conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(writeWait),
		)
		c.conn.Close()
		c.conn = nil
	}
}

// waitReconnect 使用指数退避阻塞后重试。
func (c *WSClient) waitReconnect(ctx context.Context) {
	c.reconnectCount++
	delay := time.Duration(c.reconnectCount) * reconnectDelay
	if delay > maxReconnectDelay {
		delay = maxReconnectDelay
	}
	c.logger.Info("正在重连", "attempt", c.reconnectCount, "delay", delay)

	select {
	case <-ctx.Done():
	case <-time.After(delay):
	}
}

// Stop 优雅关闭WebSocket客户端。
func (c *WSClient) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
	c.closeConn()
	c.logger.Info("WebSocket客户端已停止")
}
