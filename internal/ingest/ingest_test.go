// Package ingest M1 输入网关的端到端集成测试。
//
// 前置条件：Docker Redis 运行在 localhost:6379
// 测试内容：
//   - Redis Stream 写入 K 线 → M1 消费 → eventbus 分发 EventKlineReceived
//   - 非法 K 线（ts 倒退、字段缺失）→ EventKlineRejected
//   - ts 缺口 → EventKlineGap
package ingest

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"trade/internal/eventbus"
	"trade/internal/types"

	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
)

const testRedisAddr = "localhost:6379"

// TestIngestE2E_BasicFlow 验证：写入有效 K 线 → M1 消费 → EventKlineReceived
func TestIngestE2E_BasicFlow(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: testRedisAddr})
	defer rdb.Close()

	symbol := testSymbol(t)
	ctx := context.Background()

	// 启动 M1
	gBus := eventbus.NewGeneric()
	ing := New(Config{
		RedisAddr:     testRedisAddr,
		StreamPrefix:  "test:chan:klines",
		ConsumerGroup: "test-group",
		Symbols:       []string{symbol},
	}, gBus)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := ing.Start(ctx); err != nil {
		t.Fatalf("启动 IngestGateway 失败: %v", err)
	}

	// 订阅 EventKlineReceived
	received := make(chan *types.Kline, 10)
	gBus.Subscribe(types.EventKlineReceived, func(evt types.Event) {
		if p, ok := evt.Payload.(types.KlineReceivedPayload); ok {
			received <- p.Kline
		}
	})

	// 写入一根有效 K 线到 Redis Stream
	streamKey := fmt.Sprintf("test:chan:klines:%s", symbol)
	ts := time.Now().UnixMilli()
	msgID, err := rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: map[string]interface{}{
			"symbol":     symbol,
			"ts":         fmt.Sprintf("%d", ts),
			"open":       "50000.0",
			"high":       "51000.0",
			"low":        "49000.0",
			"close":      "50500.0",
			"baseVolume": "100.5",
			"isClosed":   "true",
		},
	}).Result()
	if err != nil {
		t.Fatalf("写入 Redis Stream 失败: %v", err)
	}
	t.Logf("已写入 Redis: msgID=%s ts=%d", msgID, ts)

	// 等待消费
	select {
	case k := <-received:
		if k.Symbol != symbol {
			t.Errorf("symbol 期望 %s, 实际 %s", symbol, k.Symbol)
		}
		if !k.Open.Equal(decimal.NewFromFloat(50000)) {
			t.Errorf("open 期望 50000, 实际 %s", k.Open)
		}
		if k.OpenTime != ts {
			t.Errorf("OpenTime 期望 %d, 实际 %d", ts, k.OpenTime)
		}
		t.Logf("消费成功: symbol=%s ts=%d open=%s close=%s",
			k.Symbol, k.OpenTime, k.Open, k.Close)
	case <-time.After(5 * time.Second):
		t.Fatal("等待 K 线消费超时（5s）")
	}

	// 验证 offset 已更新
	offset := ing.CurrentOffset(symbol)
	if offset == "" || offset == "$" {
		t.Errorf("offset 应已被更新, 当前=%s", offset)
	}
	t.Logf("消费 offset: %s", offset)
}

// TestIngestE2E_RejectInvalidKline 验证：非法 K 线 → EventKlineRejected
func TestIngestE2E_RejectInvalidKline(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: testRedisAddr})
	defer rdb.Close()

	symbol := testSymbol(t)
	ctx := context.Background()

	gBus := eventbus.NewGeneric()
	ing := New(Config{
		RedisAddr:     testRedisAddr,
		StreamPrefix:  "test:chan:klines",
		ConsumerGroup: "test-group-reject",
		Symbols:       []string{symbol},
	}, gBus)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := ing.Start(ctx); err != nil {
		t.Fatalf("启动 IngestGateway 失败: %v", err)
	}

	// 订阅拒绝事件
	rejected := make(chan types.KlineRejectedPayload, 10)
	gBus.Subscribe(types.EventKlineRejected, func(evt types.Event) {
		if p, ok := evt.Payload.(types.KlineRejectedPayload); ok {
			rejected <- p
		}
	})

	streamKey := fmt.Sprintf("test:chan:klines:%s", symbol)

	// 写入缺少 open 字段的非法消息
	msgID, err := rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: map[string]interface{}{
			"symbol": symbol,
			"ts":     fmt.Sprintf("%d", time.Now().UnixMilli()),
			"high":   "51000.0",
			"low":    "49000.0",
			"close":  "50500.0",
			// 没有 open
		},
	}).Result()
	if err != nil {
		t.Fatalf("写入 Redis Stream 失败: %v", err)
	}
	t.Logf("已写入非法 K 线: %s", msgID)

	select {
	case p := <-rejected:
		if p.Reason == "" {
			t.Error("拒绝原因应非空")
		}
		t.Logf("拒绝成功: reason=%s", p.Reason)
	case <-time.After(5 * time.Second):
		t.Fatal("等待拒绝事件超时（5s）")
	}
}

// TestIngestE2E_TsGap 验证：ts 缺口 → EventKlineGap
func TestIngestE2E_TsGap(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: testRedisAddr})
	defer rdb.Close()

	symbol := testSymbol(t)
	ctx := context.Background()

	gBus := eventbus.NewGeneric()
	ing := New(Config{
		RedisAddr:     testRedisAddr,
		StreamPrefix:  "test:chan:klines",
		ConsumerGroup: "test-group-gap",
		Symbols:       []string{symbol},
	}, gBus)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := ing.Start(ctx); err != nil {
		t.Fatalf("启动 IngestGateway 失败: %v", err)
	}

	// 订阅缺口和接收事件
	gapReceived := make(chan types.KlineGapPayload, 10)
	gBus.Subscribe(types.EventKlineGap, func(evt types.Event) {
		if p, ok := evt.Payload.(types.KlineGapPayload); ok {
			gapReceived <- p
		}
	})

	streamKey := fmt.Sprintf("test:chan:klines:%s", symbol)

	// 确保消费循环已启动
	time.Sleep(200 * time.Millisecond)

	ts1 := time.Now().UnixMilli()
	ts2 := ts1 + 300_000 // 5 分钟后（缺口 4 分钟 > 60s 阈值）

	// 先写入第一根正常 K 线
	_, err := rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: map[string]interface{}{
			"symbol": symbol,
			"ts":     fmt.Sprintf("%d", ts1),
			"open":   "50000", "high": "51000", "low": "49000", "close": "50500",
			"baseVolume": "100", "isClosed": "true",
		},
	}).Result()
	if err != nil {
		t.Fatalf("写入第一根 K 线失败: %v", err)
	}

	time.Sleep(500 * time.Millisecond) // 等待第一根被消费

	// 再写入有缺口的 K 线（ts 跳了 5 分钟）
	_, err = rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: map[string]interface{}{
			"symbol": symbol,
			"ts":     fmt.Sprintf("%d", ts2),
			"open":   "51000", "high": "52000", "low": "50000", "close": "51500",
			"baseVolume": "200", "isClosed": "true",
		},
	}).Result()
	if err != nil {
		t.Fatalf("写入第二根 K 线失败: %v", err)
	}

	select {
	case gap := <-gapReceived:
		if gap.GapDurationMs < 240000 {
			t.Errorf("缺口时长期望 >= 240000ms, 实际 %d", gap.GapDurationMs)
		}
		t.Logf("缺口检测成功: gapMs=%d lastTS=%d currentTS=%d",
			gap.GapDurationMs, gap.LastTS, gap.CurrentTS)
	case <-time.After(10 * time.Second):
		t.Fatal("等待缺口事件超时（10s）")
	}
}

// TestIngestE2E_DynamicSymbol 验证：新 stream 出现 → 自动发现并启动 worker
func TestIngestE2E_DynamicSymbol(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: testRedisAddr})
	defer rdb.Close()

	ctx := context.Background()

	gBus := eventbus.NewGeneric()
	ing := New(Config{
		RedisAddr:     testRedisAddr,
		StreamPrefix:  "test:chan:klines",
		ConsumerGroup: "test-group-dynamic",
		Symbols:       []string{}, // 初始空列表
	}, gBus)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := ing.Start(ctx); err != nil {
		t.Fatalf("启动 IngestGateway 失败: %v", err)
	}

	// 订阅接收事件
	var received atomic.Int32
	gBus.Subscribe(types.EventKlineReceived, func(evt types.Event) {
		received.Add(1)
	})

	// 创建一个新的 symbol stream（引擎动态扫描应该发现它）
	newSymbol := fmt.Sprintf("DYN_%d", time.Now().UnixNano())
	streamKey := fmt.Sprintf("test:chan:klines:%s", newSymbol)

	_, err := rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: map[string]interface{}{
			"symbol": newSymbol,
			"ts":     fmt.Sprintf("%d", time.Now().UnixMilli()),
			"open":   "60000", "high": "61000", "low": "59000", "close": "60500",
			"baseVolume": "50", "isClosed": "true",
		},
	}).Result()
	if err != nil {
		t.Fatalf("写入新 symbol K 线失败: %v", err)
	}

	// SCAN 周期 30s，等 5 秒可能不够
	// 这里先手动触发扫描
	ing.scanNewSymbols(ctx)

	// 再写一根触发消费
	_, err = rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: map[string]interface{}{
			"symbol": newSymbol,
			"ts":     fmt.Sprintf("%d", time.Now().UnixMilli()),
			"open":   "60500", "high": "61500", "low": "59500", "close": "61000",
			"baseVolume": "60", "isClosed": "true",
		},
	}).Result()
	if err != nil {
		t.Fatalf("写入第二根 K 线失败: %v", err)
	}

	time.Sleep(2 * time.Second) // 等待消费

	if received.Load() == 0 {
		t.Error("动态 symbol 未被消费")
	} else {
		t.Logf("动态 symbol 消费成功: %s, 接收 %d 条", newSymbol, received.Load())
	}
}

// TestIngestE2E_TsReject 验证：ts 重复/倒退 → EventKlineRejected
func TestIngestE2E_TsReject(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: testRedisAddr})
	defer rdb.Close()

	symbol := testSymbol(t)
	ctx := context.Background()

	gBus := eventbus.NewGeneric()
	ing := New(Config{
		RedisAddr:     testRedisAddr,
		StreamPrefix:  "test:chan:klines",
		ConsumerGroup: "test-group-ts",
		Symbols:       []string{symbol},
	}, gBus)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := ing.Start(ctx); err != nil {
		t.Fatalf("启动 IngestGateway 失败: %v", err)
	}

	rejected := make(chan types.KlineRejectedPayload, 10)
	gBus.Subscribe(types.EventKlineRejected, func(evt types.Event) {
		if p, ok := evt.Payload.(types.KlineRejectedPayload); ok {
			rejected <- p
		}
	})

	streamKey := fmt.Sprintf("test:chan:klines:%s", symbol)

	// 确保消费循环已启动
	time.Sleep(200 * time.Millisecond)

	ts := time.Now().UnixMilli()

	// 写入第一根
	_, err := rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: map[string]interface{}{
			"symbol": symbol, "ts": fmt.Sprintf("%d", ts),
			"open": "50000", "high": "51000", "low": "49000", "close": "50500",
			"baseVolume": "100", "isClosed": "true",
		},
	}).Result()
	if err != nil {
		t.Fatalf("写入第一根 K 线失败: %v", err)
	}
	time.Sleep(500 * time.Millisecond) // 等待第一根被消费

	// 写入 ts 相同的（重复）
	_, err = rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: map[string]interface{}{
			"symbol": symbol, "ts": fmt.Sprintf("%d", ts),
			"open": "51000", "high": "52000", "low": "50000", "close": "51500",
			"baseVolume": "200", "isClosed": "true",
		},
	}).Result()
	if err != nil {
		t.Fatalf("写入重复 K 线失败: %v", err)
	}

	select {
	case p := <-rejected:
		t.Logf("重复 ts 被拒绝: reason=%s", p.Reason)
	case <-time.After(10 * time.Second):
		t.Fatal("等待重复 ts 拒绝事件超时（10s）")
	}

	// 写入 ts 倒退的
	_, err = rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: map[string]interface{}{
			"symbol": symbol, "ts": fmt.Sprintf("%d", ts-1000),
			"open": "48000", "high": "49000", "low": "47000", "close": "48500",
			"baseVolume": "50", "isClosed": "true",
		},
	}).Result()
	if err != nil {
		t.Fatalf("写入倒退 K 线失败: %v", err)
	}

	select {
	case p := <-rejected:
		t.Logf("倒退 ts 被拒绝: reason=%s", p.Reason)
	case <-time.After(10 * time.Second):
		t.Fatal("等待倒退 ts 拒绝事件超时（10s）")
	}
}

// testSymbol 为每个测试生成唯一 symbol，避免测试间干扰。
func testSymbol(t *testing.T) string {
	return fmt.Sprintf("TEST_%d", time.Now().UnixNano())
}
