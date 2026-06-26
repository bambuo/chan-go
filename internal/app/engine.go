// Package app 负责系统组装与生命周期管理。
package app

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"trade/internal/logger"
)

const (
	defaultGroup    = "chan-engine"
	defaultConsumer = "server-1"
	scanInterval    = 5 * time.Second
)

// Engine 是信号分析引擎，自动发现并消费所有 trade:kline:* 流。
type Engine struct {
	log      *logger.Logger
	rdb      *redis.Client
	group    string
	consumer string
	interval time.Duration

	mu        sync.Mutex
	active    map[string]struct{}
	pipelines map[string]*Pipeline
	wg        sync.WaitGroup
}

// New 创建一个引擎实例。
func New(rdb *redis.Client, log *logger.Logger) *Engine {
	return &Engine{
		log:       log,
		rdb:       rdb,
		group:     defaultGroup,
		consumer:  defaultConsumer,
		interval:  scanInterval,
		active:    make(map[string]struct{}),
		pipelines: make(map[string]*Pipeline),
	}
}

// Start 启动引擎：立即扫描发现流并启动定期发现循环。
func (e *Engine) Start(ctx context.Context) {
	e.wg.Add(1)
	go e.discoverLoop(ctx)
}

// Shutdown 等待所有消费者 goroutine 退出。
func (e *Engine) Shutdown() {
	e.wg.Wait()
}

// discoverLoop 持续扫描新流并启动消费。
func (e *Engine) discoverLoop(ctx context.Context) {
	defer e.wg.Done()

	e.discover(ctx)
	e.log.Info("引擎已启动，持续扫描 trade:kline:* 流")

	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.discover(ctx)
		}
	}
}

// discover 扫描 trade:kline:* 模式，为新发现的流创建消费组并启动处理管线。
func (e *Engine) discover(ctx context.Context) {
	var cursor uint64
	for {
		// keys 是匹配模式的 Redis key 列表，格式如 []string{"trade:kline:BTCUSDT", ...}
		// nextCursor 是下一轮扫描的游标，为 0 时表示扫描结束
		keys, nextCursor, err := e.rdb.Scan(ctx, cursor, "trade:kline:*", 100).Result()
		if err != nil {
			e.log.Error("扫描 Stream 失败", "error", err)
			return
		}

		for _, key := range keys {
			e.mu.Lock()
			_, exists := e.active[key]
			if !exists {
				e.active[key] = struct{}{}
				e.mu.Unlock()

				if err := e.createGroup(ctx, key); err != nil {
					e.log.Error("创建消费组失败", "stream", key, "error", err)
					continue
				}

				// 从流名称中提取交易对符号
				symbol := extractSymbol(key)
				if symbol == "" {
					e.log.Error("无法从流名称中提取交易对符号", "stream", key)
					continue
				}

				// 创建并启动处理管线
				pipeline := NewPipeline(symbol, key, e.rdb, e.log, e.group, e.consumer)
				e.pipelines[key] = pipeline
				e.wg.Add(1)
				go pipeline.Start(ctx, &e.wg)
				e.log.Info("发现新流，已启动处理管线", "stream", key, "symbol", symbol)
			} else {
				e.mu.Unlock()
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
}

// extractSymbol 从流名称中提取交易对符号。
// 流名称格式：trade:kline:BTCUSDT
func extractSymbol(stream string) string {
	parts := strings.Split(stream, ":")
	if len(parts) >= 3 {
		return parts[2]
	}
	return ""
}

// createGroup 创建 Redis Stream 消费组，已存在则忽略。
func (e *Engine) createGroup(ctx context.Context, stream string) error {
	err := e.rdb.XGroupCreateMkStream(ctx, stream, e.group, "$").Err()
	if err != nil && strings.Contains(err.Error(), "BUSYGROUP") {
		return nil
	}
	return err
}
