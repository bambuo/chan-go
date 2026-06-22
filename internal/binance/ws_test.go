package binance

import (
	"testing"

	"trade/internal/types"

	"github.com/adshao/go-binance/v2"
)

// TestConvert_KlineEvent 验证 SDK WsKlineEvent 到内部 Kline 的转换。
func TestConvert_KlineEvent(t *testing.T) {
	tests := []struct {
		name     string
		input    *binance.WsKlineEvent
		expected *types.Kline
	}{
		{
			name: "闭合K线",
			input: &binance.WsKlineEvent{
				Symbol: "BTCUSDT",
				Event:  "kline",
				Time:   1000000,
				Kline: binance.WsKline{
					StartTime: 1000,
					EndTime:   1060000,
					Symbol:    "BTCUSDT",
					Interval:  "1m",
					Open:      "50000.0",
					Close:     "50500.0",
					High:      "51000.0",
					Low:       "49000.0",
					Volume:    "100.5",
					IsFinal:   true,
				},
			},
			expected: &types.Kline{
				Symbol:    "BTCUSDT",
				OpenTime:  1000,
				CloseTime: 1060000,
				IsClosed:  true,
			},
		},
		{
			name: "未闭合K线",
			input: &binance.WsKlineEvent{
				Symbol: "ETHUSDT",
				Event:  "kline",
				Time:   2000000,
				Kline: binance.WsKline{
					StartTime: 2000,
					EndTime:   2060000,
					Symbol:    "ETHUSDT",
					Interval:  "1m",
					Open:      "3000.0",
					Close:     "3010.5",
					High:      "3020.0",
					Low:       "2990.0",
					Volume:    "500.2",
					IsFinal:   false,
				},
			},
			expected: &types.Kline{
				Symbol:    "ETHUSDT",
				OpenTime:  2000,
				CloseTime: 2060000,
				IsClosed:  false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convert(tt.input)

			if got.Symbol != tt.expected.Symbol {
				t.Errorf("Symbol: 期望 %s, 实际 %s", tt.expected.Symbol, got.Symbol)
			}
			if got.OpenTime != tt.expected.OpenTime {
				t.Errorf("OpenTime: 期望 %d, 实际 %d", tt.expected.OpenTime, got.OpenTime)
			}
			if got.CloseTime != tt.expected.CloseTime {
				t.Errorf("CloseTime: 期望 %d, 实际 %d", tt.expected.CloseTime, got.CloseTime)
			}
			if got.IsClosed != tt.expected.IsClosed {
				t.Errorf("IsClosed: 期望 %v, 实际 %v", tt.expected.IsClosed, got.IsClosed)
			}

			// 验证数值解析正确。
			if got.Open.InexactFloat64() < 1 {
				t.Errorf("Open 未正确解析: %v", got.Open)
			}
			if got.High.InexactFloat64() < 1 {
				t.Errorf("High 未正确解析: %v", got.High)
			}
			if got.Low.InexactFloat64() < 1 {
				t.Errorf("Low 未正确解析: %v", got.Low)
			}
			if got.Close.InexactFloat64() < 1 {
				t.Errorf("Close 未正确解析: %v", got.Close)
			}
		})
	}
}

// TestSafeDecimal 验证字符串到 decimal 的安全转换。
func TestSafeDecimal(t *testing.T) {
	tests := []struct {
		input string
		want  float64
	}{
		{"50000.0", 50000},
		{"0.001", 0.001},
		{"100.5", 100.5},
		{"", 0},
		{"invalid", 0},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := safeDecimal(tt.input)
			if got.InexactFloat64() != tt.want {
				t.Errorf("safeDecimal(%q) = %v, 期望 %v", tt.input, got.InexactFloat64(), tt.want)
			}
		})
	}
}
