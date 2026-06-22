package binance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"trade/internal/eventbus"
	"trade/internal/types"

	"github.com/gorilla/websocket"
)

// testWSKlinePayload 镜像币安K线负载结构，用于构建测试消息。
type testWSKlinePayload struct {
	EventType string           `json:"e"`
	EventTime int64            `json:"E"`
	Symbol    string           `json:"s"`
	Kline     testWSKlineInner `json:"k"`
}

type testWSKlineInner struct {
	OpenTime  int64   `json:"t"`
	CloseTime int64   `json:"T"`
	Symbol    string  `json:"s"`
	Interval  string  `json:"i"`
	Open      float64 `json:"o"`
	Close     float64 `json:"c"`
	High      float64 `json:"h"`
	Low       float64 `json:"l"`
	Volume    float64 `json:"v"`
	IsClosed  bool    `json:"x"`
}

// testWebSocketServer 创建一个本地WebSocket服务器，模拟币安K线流。
func testWebSocketServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()

	var upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// 使用类型结构体构建负载（避免JSON手写错误）。
		payload := testWSKlinePayload{
			EventType: "kline",
			EventTime: time.Now().UnixMilli(),
			Symbol:    "BTCUSDT",
			Kline: testWSKlineInner{
				OpenTime:  1000,
				CloseTime: 1060000,
				Symbol:    "BTCUSDT",
				Interval:  "1m",
				Open:      50000.0,
				Close:     50500.0,
				High:      51000.0,
				Low:       49000.0,
				Volume:    100.5,
				IsClosed:  true,
			},
		}

		data, _ := json.Marshal(payload)
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			t.Log("写入错误:", err)
			return
		}

		// 短暂保持连接。
		time.Sleep(500 * time.Millisecond)
	}))

	// 将http://...转换为ws://...
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	return server, wsURL
}

// TestWSClient_ConnectAndReceive 验证WebSocket客户端能连接并接收消息。
func TestWSClient_ConnectAndReceive(t *testing.T) {
	server, wsURL := testWebSocketServer(t)
	defer server.Close()

	bus := eventbus.New()
	received := make(chan types.KlineEvent, 1)

	bus.Subscribe(types.EventKlineClosed, func(evt types.KlineEvent) {
		received <- evt
	})

	client := NewWSClient(
		[]string{"btcusdt"},
		"1m",
		bus,
		WithWSURL(wsURL+"/ws"),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go func() {
		_ = client.Start(ctx)
	}()

	select {
	case evt := <-received:
		if evt.Kline.Symbol != "BTCUSDT" {
			t.Errorf("期望BTCUSDT，实际为 %s", evt.Kline.Symbol)
		}
		if evt.Kline.Open.InexactFloat64() != 50000 {
			t.Errorf("期望open=50000，实际为 %.1f", evt.Kline.Open.InexactFloat64())
		}
		if evt.Kline.Close.InexactFloat64() != 50500 {
			t.Errorf("期望close=50500，实际为 %.1f", evt.Kline.Close.InexactFloat64())
		}
		if evt.Kline.High.InexactFloat64() != 51000 {
			t.Errorf("期望high=51000，实际为 %.1f", evt.Kline.High.InexactFloat64())
		}
		if evt.Kline.Low.InexactFloat64() != 49000 {
			t.Errorf("期望low=49000，实际为 %.1f", evt.Kline.Low.InexactFloat64())
		}
		if evt.Kline.Volume.InexactFloat64() != 100.5 {
			t.Errorf("期望volume=100.5，实际为 %.1f", evt.Kline.Volume.InexactFloat64())
		}
		if !evt.Kline.IsClosed {
			t.Error("期望IsClosed=true")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("等待K线事件超时")
	}
}

// TestWSClient_RealtimeAndClosedEvents 验证两种事件类型都能产生。
func TestWSClient_RealtimeAndClosedEvents(t *testing.T) {
	server, wsURL := testWebSocketServer(t)
	defer server.Close()

	bus := eventbus.New()
	var closedCount atomic.Int32

	bus.Subscribe(types.EventKlineClosed, func(evt types.KlineEvent) {
		closedCount.Add(1)
	})

	client := NewWSClient(
		[]string{"btcusdt"},
		"1m",
		bus,
		WithWSURL(wsURL+"/ws"),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = client.Start(ctx)
	}()

	time.Sleep(1 * time.Second)

	if closedCount.Load() == 0 {
		t.Error("期望至少1个闭合事件")
	}
}

// TestCombinedStreamURL 验证组合流URL的构建。
func TestCombinedStreamURL(t *testing.T) {
	bus := eventbus.New()
	client := NewWSClient([]string{"btcusdt", "ethusdt"}, "1m", bus)

	if client.url != defaultBinanceWSURL {
		t.Errorf("期望默认URL，实际为 %s", client.url)
	}
	if len(client.symbols) != 2 {
		t.Errorf("期望2个交易对，实际为 %d", len(client.symbols))
	}
}
