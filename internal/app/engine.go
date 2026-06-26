// Package app 负责系统组装与生命周期管理。
package app

import (
	"context"
	"errors"
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

	mu     sync.Mutex
	active map[string]struct{}
	wg     sync.WaitGroup
}

// New 创建一个引擎实例。
func New(rdb *redis.Client, log *logger.Logger) *Engine {
	return &Engine{
		log:      log,
		rdb:      rdb,
		group:    defaultGroup,
		consumer: defaultConsumer,
		interval: scanInterval,
		active:   make(map[string]struct{}),
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

// discover 扫描 trade:kline:* 模式，为新发现的流创建消费组并启动消费。
func (e *Engine) discover(ctx context.Context) {
	var cursor uint64
	for {
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
				e.wg.Add(1)
				go e.consume(ctx, key)
				e.log.Info("发现新流，已启动消费", "stream", key)
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

// createGroup 创建 Redis Stream 消费组，已存在则忽略。
func (e *Engine) createGroup(ctx context.Context, stream string) error {
	err := e.rdb.XGroupCreateMkStream(ctx, stream, e.group, "$").Err()
	if err != nil && strings.Contains(err.Error(), "BUSYGROUP") {
		return nil
	}
	return err
}

// consume 持续从指定 Stream 读取并处理 K 线消息。
func (e *Engine) consume(ctx context.Context, stream string) {
	defer e.wg.Done()
	e.log.Info("开始监听流", "stream", stream)

	for {
		if ctx.Err() != nil {
			e.log.Info("流消费已停止", "stream", stream)
			return
		}

		msgs, err := e.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    e.group,
			Consumer: e.consumer,
			Streams:  []string{stream, ">"},
			Count:    10,
			Block:    time.Second,
		}).Result()
		if err != nil {
			if errors.Is(err, context.Canceled) {
				e.log.Info("流消费已停止", "stream", stream)
				return
			}
			if errors.Is(err, redis.Nil) {
				continue
			}
			if strings.Contains(err.Error(), "NOGROUP") {
				e.log.Warn("消费组不存在，尝试重建", "stream", stream)
				if cerr := e.createGroup(ctx, stream); cerr != nil {
					e.log.Error("重建消费组失败", "stream", stream, "error", cerr)
				}
				continue
			}
			e.log.Error("读取 Stream 失败", "stream", stream, "error", err)
			continue
		}

		for _, result := range msgs {
			for _, msg := range result.Messages {
				e.log.Info("收到 K 线",
					"stream", stream,
					"id", msg.ID,
					"symbol", msg.Values["symbol"],
					"open", msg.Values["open"],
					"high", msg.Values["high"],
					"low", msg.Values["low"],
					"close", msg.Values["close"],
					"ts", msg.Values["ts"],
				)
			}
		}
	}
}
