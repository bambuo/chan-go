// Package m1_ingest 输入网关（M1）。
//
// 职责（PRD §5/§12.1/§12.5）：
//   - 消费 Redis Stream（chan:klines:{symbol}），引擎唯一输入边界
//   - 按 symbol 路由到独立 worker
//   - K 线连续性校验（ts 递增、字段完整性），PRD §14.5.1 R17
//   - 动态 symbol 发现（PRD §12.5），SCAN 定时扫描新 stream
//   - 维护消费 offset，支持快照恢复
//
// 核心原则：引擎无冷启动概念，从第一根 K 线起即正常工作（PRD §12.4 X1）。
package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"trade/internal/eventbus"
	"trade/internal/log"
	"trade/internal/types"

	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
)

// Config M1 输入网关配置。
type Config struct {
	RedisAddr     string   // Redis 地址（如 "localhost:6380"）
	RedisPassword string   // Redis 密码
	RedisDB       int      // Redis DB 编号
	StreamPrefix  string   // Stream 前缀（默认 "chan:klines"）
	ConsumerGroup string   // 消费组名称（默认 "chan-engine"）
	Symbols       []string // 初始 symbol 列表
}

// IngestGateway M1 输入网关。
type IngestGateway struct {
	cfg    Config
	bus    *eventbus.GenericBus
	rdb    *redis.Client
	logger *slog.Logger

	mu              sync.RWMutex
	workers         map[string]*symbolWorker // symbol → worker
	offsets         map[string]string        // symbol → 消费 offset
	lastTS          map[string]int64         // symbol → 上一根 K 线 ts
	pendingSnapshot map[string]bool          // symbol → 待快照标记
}

type symbolWorker struct {
	symbol string
	cancel context.CancelFunc
}

// New 创建输入网关实例。
func New(cfg Config, bus *eventbus.GenericBus) *IngestGateway {
	if cfg.StreamPrefix == "" {
		cfg.StreamPrefix = "chan:klines"
	}
	if cfg.ConsumerGroup == "" {
		cfg.ConsumerGroup = "chan-engine"
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	return &IngestGateway{
		cfg:             cfg,
		bus:             bus,
		rdb:             rdb,
		logger:          log.Component("m1.ingest"),
		workers:         make(map[string]*symbolWorker),
		offsets:         make(map[string]string),
		lastTS:          make(map[string]int64),
		pendingSnapshot: make(map[string]bool),
	}
}

// Start 启动所有 symbol worker + 动态扫描。
func (g *IngestGateway) Start(ctx context.Context) error {
	g.logger.Info("启动输入网关", "symbols", g.cfg.Symbols)

	// 检查 Redis 连接
	if err := g.rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("Redis 连接失败: %w", err)
	}

	// 启动初始 symbol worker
	for _, symbol := range g.cfg.Symbols {
		if err := g.startWorker(ctx, symbol); err != nil {
			g.logger.Error("启动 worker 失败", "symbol", symbol, "error", err)
		}
	}

	// 动态 symbol 扫描（PRD §12.5.1）
	go g.scanLoop(ctx)

	return nil
}

// Stop 优雅停止所有 worker。
func (g *IngestGateway) Stop() {
	g.mu.Lock()
	defer g.mu.Unlock()

	for symbol, w := range g.workers {
		w.cancel()
		delete(g.workers, symbol)
	}
	g.logger.Info("输入网关已停止")
}

// RestoreOffset 从快照恢复指定 symbol 的消费 offset。
// 通过 XGROUP SETID 将消费组游标设置到快照 offset。
func (g *IngestGateway) RestoreOffset(symbol, offset string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// 记录 offset 到内存，消费循环会用 ">" 读取新消息
	g.offsets[symbol] = offset

	// 通过 XGROUP SETID 设置消费组游标
	// 这样后续 ">" 会从快照 offset 之后的新消息开始读取
	streamKey := g.streamKey(symbol)
	err := g.rdb.XGroupSetID(context.Background(), streamKey, g.cfg.ConsumerGroup, offset).Err()
	if err != nil {
		g.logger.Warn("设置消费组游标失败（可能首次恢复时消费组未就绪）",
			"symbol", symbol, "offset", offset, "error", err)
	}

	g.logger.Info("恢复消费 offset", "symbol", symbol, "offset", offset)
}

// CurrentOffset 返回指定 symbol 的当前消费 offset。
func (g *IngestGateway) CurrentOffset(symbol string) string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.offsets[symbol]
}

// MarkPendingSnapshot 标记 symbol 待快照（PRD §12.3 串行化队列）。
func (g *IngestGateway) MarkPendingSnapshot(symbol string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.pendingSnapshot[symbol] = true
}

// ConsumePendingSnapshots 消费所有待快照标记（由 app 在主循环调用）。
func (g *IngestGateway) ConsumePendingSnapshots() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	var symbols []string
	for sym, pending := range g.pendingSnapshot {
		if pending {
			symbols = append(symbols, sym)
			g.pendingSnapshot[sym] = false
		}
	}
	return symbols
}

// startWorker 为指定 symbol 启动消费 goroutine。
func (g *IngestGateway) startWorker(ctx context.Context, symbol string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	// 检查是否已存在
	if _, ok := g.workers[symbol]; ok {
		return nil
	}

	workerCtx, cancel := context.WithCancel(ctx)
	streamKey := g.streamKey(symbol)

	w := &symbolWorker{
		symbol: symbol,
		cancel: cancel,
	}
	g.workers[symbol] = w

	// 确保消费组存在
	if err := g.ensureConsumerGroup(workerCtx, streamKey); err != nil {
		cancel()
		delete(g.workers, symbol)
		return fmt.Errorf("创建消费组 %s: %w", streamKey, err)
	}

	go g.consumeLoop(workerCtx, symbol, streamKey)
	g.logger.Info("启动 symbol worker", "symbol", symbol, "stream", streamKey)
	return nil
}

// stopWorker 停止指定 symbol 的 worker（PRD §12.5.3 symbol 移除）。
func (g *IngestGateway) stopWorker(symbol string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if w, ok := g.workers[symbol]; ok {
		w.cancel()
		delete(g.workers, symbol)
		g.logger.Info("停止 symbol worker", "symbol", symbol)
	}
}

// ensureConsumerGroup 确保 Redis 消费组存在（首次创建或已存在则跳过）。
func (g *IngestGateway) ensureConsumerGroup(ctx context.Context, streamKey string) error {
	err := g.rdb.XGroupCreateMkStream(ctx, streamKey, g.cfg.ConsumerGroup, "0").Err()
	if err != nil && strings.Contains(err.Error(), "BUSYGROUP") {
		// 消费组已存在，这是正常情况
		return nil
	}
	return err
}

// consumeLoop symbol worker 主循环：持续 XREADGROUP 消费。
//
// 关于 offset 关键语义（Redis 文档）：
//
//	XREADGROUP 的 id 参数有两种模式：
//	  ">"     = 读取从未投递给任何消费者的新消息（NEW 模式）
//	  具体 ID  = 读取该消费者已投递但未 ACK 的待处理消息（HISTORY 模式）
//
//	消费循环始终使用 ">"，确保只读新消息。
//	快照恢复时通过 XGROUP SETID 设置消费组游标，"＞" 自然从游标处续消费。
//	不依赖 XACK（§12.1 D2 已锁定：重复消费不影响正确性——M2 是纯函数）。
func (g *IngestGateway) consumeLoop(ctx context.Context, symbol, streamKey string) {
	consumerName := fmt.Sprintf("engine-%d", time.Now().UnixNano())

	g.logger.Info("开始消费", "symbol", symbol, "consumer", consumerName)

	for {
		select {
		case <-ctx.Done():
			g.logger.Info("worker 停止", "symbol", symbol)
			return
		default:
		}

		// XREADGROUP: 始终用 ">" 读取新消息
		streams, err := g.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    g.cfg.ConsumerGroup,
			Consumer: consumerName,
			Streams:  []string{streamKey, ">"},
			Count:    10,
			Block:    2 * time.Second,
		}).Result()

		if err != nil {
			if err == redis.Nil {
				// 超时无新消息，继续
				continue
			}
			g.logger.Error("XREADGROUP 错误", "symbol", symbol, "error", err)
			time.Sleep(time.Second)
			continue
		}

		for _, stream := range streams {
			for _, msg := range stream.Messages {
				g.processMessage(symbol, msg)
				// 仅记录 offset 供快照用，不用于 XREADGROUP 参数
				g.mu.Lock()
				g.offsets[symbol] = msg.ID
				g.mu.Unlock()
			}
		}
	}
}

// restoreStartOffset 确定起始 offset。
//
// XREADGROUP 特殊语义：
//   - ">" = 读取从未投递给任何消费者的新消息（XREADGROUP 专用）
//   - "$" 在 XREADGROUP 中非法（会导致 ERR）
//   - "0" = 读取该消费者已投递但未 ACK 的待处理消息
//
// 快照恢复时通过 XGROUP SETID 设置消费组游标，此处始终返回 ">"。
func (g *IngestGateway) restoreStartOffset(_ string) string {
	return ">"
}

// setGroupCursor 通过 XGROUP SETID 设置消费组游标到指定 offset。
// 用于快照恢复后，从快照记录的 offset 续消费。
func (g *IngestGateway) setGroupCursor(ctx context.Context, symbol, offset string) error {
	streamKey := g.streamKey(symbol)
	err := g.rdb.XGroupSetID(ctx, streamKey, g.cfg.ConsumerGroup, offset).Err()
	if err != nil {
		return fmt.Errorf("设置消费组游标 %s/%s: %w", streamKey, offset, err)
	}
	g.logger.Info("设置消费组游标", "stream", streamKey, "offset", offset)
	return nil
}

// processMessage 处理单条 Redis Stream 消息。
func (g *IngestGateway) processMessage(symbol string, msg redis.XMessage) {
	// 解析消息字段
	kline, err := parseRedisMessage(msg)
	if err != nil {
		g.logger.Error("解析 K 线消息失败", "symbol", symbol, "msgId", msg.ID, "error", err)
		g.publishRejected(symbol, nil, fmt.Sprintf("解析失败: %v", err))
		return
	}

	// K 线校验
	g.mu.RLock()
	lastTS := g.lastTS[symbol]
	g.mu.RUnlock()

	if valid, reason := validateKline(kline, lastTS); !valid {
		g.logger.Warn("K 线校验不通过", "symbol", symbol, "ts", kline.OpenTime, "reason", reason)
		g.publishRejected(symbol, kline, reason)
		return
	}

	// 检查 ts 缺口
	if lastTS > 0 && kline.OpenTime-lastTS > 60000 {
		gap := kline.OpenTime - lastTS
		g.logger.Warn("K 线缺口", "symbol", symbol, "gapMs", gap, "lastTS", lastTS, "currentTS", kline.OpenTime)
		g.bus.Publish(types.Event{
			Type:   types.EventKlineGap,
			Symbol: symbol,
			TS:     kline.OpenTime,
			Payload: types.KlineGapPayload{
				Symbol:        symbol,
				LastTS:        lastTS,
				CurrentTS:     kline.OpenTime,
				GapDurationMs: gap,
			},
		})
	}

	// 更新 lastTS
	g.mu.Lock()
	g.lastTS[symbol] = kline.OpenTime
	g.mu.Unlock()

	// 发布 K 线已接收事件
	g.bus.Publish(types.Event{
		Type:   types.EventKlineReceived,
		Symbol: symbol,
		TS:     kline.OpenTime,
		Payload: types.KlineReceivedPayload{
			Kline: kline,
		},
	})
}

// publishRejected 发布 K 线被拒绝事件（PRD §14.5.1）。
func (g *IngestGateway) publishRejected(symbol string, kline *types.Kline, reason string) {
	g.bus.Publish(types.Event{
		Type:   types.EventKlineRejected,
		Symbol: symbol,
		Payload: types.KlineRejectedPayload{
			Kline:  kline,
			Reason: reason,
		},
	})
}

// scanLoop 定期扫描 Redis 中的新 symbol stream（PRD §12.5.1）。
func (g *IngestGateway) scanLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	g.logger.Info("启动动态 symbol 扫描", "interval", "30s")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			g.scanNewSymbols(ctx)
		}
	}
}

// scanNewSymbols 扫描 Redis 中新的 chan:klines:* stream。
func (g *IngestGateway) scanNewSymbols(ctx context.Context) {
	pattern := g.cfg.StreamPrefix + ":*"
	iter := g.rdb.Scan(ctx, 0, pattern, 100).Iterator()

	for iter.Next(ctx) {
		streamName := iter.Val()
		// 提取 symbol：chan:klines:BTCUSDT → BTCUSDT
		symbol := strings.TrimPrefix(streamName, g.cfg.StreamPrefix+":")

		g.mu.RLock()
		_, exists := g.workers[symbol]
		g.mu.RUnlock()

		if !exists {
			g.logger.Info("发现新 symbol stream", "stream", streamName, "symbol", symbol)
			if err := g.startWorker(ctx, symbol); err != nil {
				g.logger.Error("启动新 symbol worker 失败", "symbol", symbol, "error", err)
			}
		}
	}

	if err := iter.Err(); err != nil {
		g.logger.Error("SCAN 错误", "error", err)
	}
}

// streamKey 返回 Redis Stream key。
func (g *IngestGateway) streamKey(symbol string) string {
	return fmt.Sprintf("%s:%s", g.cfg.StreamPrefix, symbol)
}

// parseRedisMessage 将 Redis Stream 消息解析为 Kline 对象。
func parseRedisMessage(msg redis.XMessage) (*types.Kline, error) {
	values := msg.Values

	k := &types.Kline{}

	// symbol
	if sym, ok := values["symbol"].(string); ok {
		k.Symbol = sym
	} else {
		return nil, fmt.Errorf("缺少 symbol 字段")
	}

	// ts
	if tsStr, ok := values["ts"].(string); ok {
		ts, err := strconv.ParseInt(tsStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("解析 ts 失败: %w", err)
		}
		k.OpenTime = ts
		k.CloseTime = ts + 60000 - 1 // 1m K 线推算 closeTime
	} else if tsNum, ok := values["ts"].(int64); ok {
		k.OpenTime = tsNum
		k.CloseTime = tsNum + 60000 - 1
	} else {
		return nil, fmt.Errorf("缺少或无法解析 ts 字段")
	}

	// OHLC
	k.Open = parseDecimalField(values, "open")
	k.High = parseDecimalField(values, "high")
	k.Low = parseDecimalField(values, "low")
	k.Close = parseDecimalField(values, "close")

	// 可选字段
	if v := parseDecimalField(values, "baseVolume"); !v.IsZero() {
		k.BaseVolume = v
	}
	if v := parseDecimalField(values, "quoteVolume"); !v.IsZero() {
		k.QuoteVolume = v
	}
	if v := parseDecimalField(values, "turnover"); !v.IsZero() {
		k.Turnover = v
	}
	if tcStr, ok := values["tradeCount"].(string); ok {
		if tc, err := strconv.ParseInt(tcStr, 10, 64); err == nil {
			k.TradeCount = tc
		}
	}

	// isClosed
	if closedStr, ok := values["isClosed"].(string); ok {
		k.IsClosed = closedStr == "1" || closedStr == "true"
	}

	return k, nil
}

// parseDecimalField 从 values map 中解析 Decimal 字段。
func parseDecimalField(values map[string]interface{}, key string) decimal.Decimal {
	v, ok := values[key]
	if !ok {
		return decimal.Zero
	}
	switch val := v.(type) {
	case string:
		d, err := decimal.NewFromString(val)
		if err != nil {
			return decimal.Zero
		}
		return d
	case float64:
		return decimal.NewFromFloat(val)
	case int64:
		return decimal.NewFromInt(val)
	default:
		return decimal.Zero
	}
}

// validateKline 校验单根 K 线的合法性（PRD §14.5.1 R17）。
func validateKline(k *types.Kline, lastTS int64) (bool, string) {
	// ts 倒退或重复
	if k.OpenTime <= lastTS && lastTS > 0 {
		return false, fmt.Sprintf("ts 不递增: last=%d, current=%d", lastTS, k.OpenTime)
	}

	// 字段异常
	if k.Open.IsZero() || k.High.IsZero() || k.Low.IsZero() || k.Close.IsZero() {
		return false, "价格字段为零或缺失"
	}

	// high >= low
	if k.High.LessThan(k.Low) {
		return false, "high < low"
	}

	// open/close 在 [low, high] 范围内
	if k.Open.LessThan(k.Low) || k.Open.GreaterThan(k.High) {
		return false, "open 不在 [low, high] 范围内"
	}
	if k.Close.LessThan(k.Low) || k.Close.GreaterThan(k.High) {
		return false, "close 不在 [low, high] 范围内"
	}

	// OpenTime 必须在合理范围内（1970年以后，非未来时间）
	if k.OpenTime < 0 {
		return false, "OpenTime 为负值"
	}
	// 未来 5 分钟内的 K 线允许（数据适配层延迟写入场景）
	now := time.Now().UnixMilli()
	if k.OpenTime > now+5*60*1000 {
		return false, "OpenTime 远超当前时间"
	}

	return true, ""
}
