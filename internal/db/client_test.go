package db

import (
	"context"
	"testing"
	"time"

	"trade/internal/types"

	"github.com/shopspring/decimal"
)

func newTestDB(t *testing.T) (*Client, context.Context) {
	t.Helper()

	ctx := context.Background()
	dbPath := "file:" + t.TempDir() + "/test.db?cache=shared&_journal_mode=WAL"

	client, err := NewClient(ctx, dbPath)
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}

	t.Cleanup(func() {
		client.Close()
	})

	return client, ctx
}

func closedKline(symbol string, openTime int64) *types.Kline {
	return &types.Kline{
		Symbol:    symbol,
		Open:      decimal.NewFromFloat(50000),
		High:      decimal.NewFromFloat(51000),
		Low:       decimal.NewFromFloat(49000),
		Close:     decimal.NewFromFloat(50500),
		Volume:    decimal.NewFromFloat(100),
		OpenTime:  openTime,
		CloseTime: openTime + 60000,
		IsClosed:  true,
	}
}

// TestInsertAndQueryKline 验证基本的插入和查询。
func TestInsertAndQueryKline(t *testing.T) {
	client, ctx := newTestDB(t)

	k := closedKline("BTCUSDT", time.Now().UnixMilli())

	if err := client.InsertKline(ctx, k); err != nil {
		t.Fatalf("插入失败: %v", err)
	}

	klines, err := client.QueryKlines(ctx, "BTCUSDT", k.OpenTime-1000, k.OpenTime+1000, 10)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(klines) != 1 {
		t.Fatalf("期望1条K线，实际 %d", len(klines))
	}
	if klines[0].Open.InexactFloat64() != 50000 {
		t.Errorf("期望open=50000，实际 %.1f", klines[0].Open.InexactFloat64())
	}
	if klines[0].High.InexactFloat64() != 51000 {
		t.Errorf("期望high=51000，实际 %.1f", klines[0].High.InexactFloat64())
	}
	if klines[0].Low.InexactFloat64() != 49000 {
		t.Errorf("期望low=49000，实际 %.1f", klines[0].Low.InexactFloat64())
	}
	if klines[0].Close.InexactFloat64() != 50500 {
		t.Errorf("期望close=50500，实际 %.1f", klines[0].Close.InexactFloat64())
	}
	if !klines[0].IsClosed {
		t.Error("查询结果应有IsClosed=true")
	}
}

// TestInsertDuplicateKline 验证通过INSERT OR IGNORE去重。
func TestInsertDuplicateKline(t *testing.T) {
	client, ctx := newTestDB(t)

	k := closedKline("BTCUSDT", 1000)

	if err := client.InsertKline(ctx, k); err != nil {
		t.Fatalf("首次插入失败: %v", err)
	}

	// 插入重复数据。
	if err := client.InsertKline(ctx, k); err != nil {
		t.Fatalf("第二次插入不应报错: %v", err)
	}

	klines, err := client.QueryKlines(ctx, "BTCUSDT", 0, 2000, 10)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(klines) != 1 {
		t.Fatalf("去重后期望1条K线，实际 %d", len(klines))
	}
}

// TestInsertUnclosedKline 验证未闭合的K线被拒绝。
func TestInsertUnclosedKline(t *testing.T) {
	client, ctx := newTestDB(t)

	k := &types.Kline{
		Symbol:   "BTCUSDT",
		Open:     decimal.NewFromFloat(50000),
		High:     decimal.NewFromFloat(51000),
		Low:      decimal.NewFromFloat(49000),
		Close:    decimal.NewFromFloat(50500),
		OpenTime: 1000,
		IsClosed: false,
	}

	err := client.InsertKline(ctx, k)
	if err == nil {
		t.Fatal("未闭合K线应报错")
	}
}

// TestInsertMultipleSymbols 验证不同交易对的数据正确存储。
func TestInsertMultipleSymbols(t *testing.T) {
	client, ctx := newTestDB(t)

	klines := []*types.Kline{
		closedKline("BTCUSDT", 1000),
		closedKline("ETHUSDT", 1000),
		closedKline("BTCUSDT", 2000),
	}

	for _, k := range klines {
		if err := client.InsertKline(ctx, k); err != nil {
			t.Fatalf("插入失败: %v", err)
		}
	}

	btcKlines, err := client.QueryKlines(ctx, "BTCUSDT", 0, 3000, 10)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(btcKlines) != 2 {
		t.Fatalf("期望2条BTC K线，实际 %d", len(btcKlines))
	}
}

// TestQueryLimit 验证查询的limit参数。
func TestQueryLimit(t *testing.T) {
	client, ctx := newTestDB(t)

	for i := 0; i < 10; i++ {
		k := closedKline("BTCUSDT", int64(1000+i*60000))
		if err := client.InsertKline(ctx, k); err != nil {
			t.Fatalf("插入失败: %v", err)
		}
	}

	klines, err := client.QueryKlines(ctx, "BTCUSDT", 0, 1<<62, 3)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(klines) > 3 {
		t.Fatalf("期望最多3条K线，实际 %d", len(klines))
	}
}

// TestAutoMigration 验证数据库在创建时自动初始化。
func TestAutoMigration(t *testing.T) {
	client, ctx := newTestDB(t)

	// 执行简单查询验证表存在。
	var count int
	err := client.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM klines").Scan(&count)
	if err != nil {
		t.Fatalf("迁移后表应存在: %v", err)
	}
}

// TestEventBusHandlerIntegration 验证事件总线处理器集成。
func TestEventBusHandlerIntegration(t *testing.T) {
	client, ctx := newTestDB(t)

	handler := client.InsertClosedKlineHandler(ctx)

	// 测试错误事件类型。
	handler(types.KlineEvent{
		Type:  types.EventKlineRealtime,
		Kline: closedKline("BTCUSDT", 1000),
	})

	klines, _ := client.QueryKlines(ctx, "BTCUSDT", 0, 2000, 10)
	if len(klines) != 0 {
		t.Error("实时事件不应触发插入")
	}

	// 测试正确事件类型。
	handler(types.KlineEvent{
		Type:  types.EventKlineClosed,
		Kline: closedKline("BTCUSDT", 1000),
	})

	klines, _ = client.QueryKlines(ctx, "BTCUSDT", 0, 2000, 10)
	if len(klines) != 1 {
		t.Error("闭合事件应触发插入")
	}

	// 测试nil Kline（不应panic）。
	handler(types.KlineEvent{Type: types.EventKlineClosed})
}
