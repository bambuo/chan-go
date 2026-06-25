// Package app 负责系统组装与生命周期管理。
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	defaultGroup    = "chan-engine"
	defaultConsumer = "server-1"
)

// Engine 是信号分析引擎，管理 Redis Stream 消费生命周期。
type Engine struct {
	rdb      *redis.Client
	symbols  []string
	group    string
	consumer string
	wg       sync.WaitGroup
}

// New 创建一个引擎实例。
func New(rdb *redis.Client, symbols []string) *Engine {
	return &Engine{
		rdb:      rdb,
		symbols:  symbols,
		group:    defaultGroup,
		consumer: defaultConsumer,
	}
}

// Start 为每个交易对创建消费组并启动流消费 goroutine。
// ctx 被取消时所有消费者自动退出。
func (e *Engine) Start(ctx context.Context) error {
	for _, sym := range e.symbols {
		streamKey := fmt.Sprintf("trade:kline:%s", sym)
		if err := e.createGroup(ctx, streamKey); err != nil {
			return fmt.Errorf("创建消费组失败 [%s]: %w", streamKey, err)
		}
		e.wg.Add(1)
		go e.consume(ctx, streamKey)
	}
	return nil
}

// Shutdown 等待所有消费者 goroutine 退出。
func (e *Engine) Shutdown() {
	e.wg.Wait()
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
// Block 设 1 秒超时，确保关闭时能及时感知 ctx 取消。
func (e *Engine) consume(ctx context.Context, stream string) {
	defer e.wg.Done()
	slog.Info("开始监听流", "stream", stream)

	for {
		if ctx.Err() != nil {
			slog.Info("流消费已停止", "stream", stream)
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
				slog.Info("流消费已停止", "stream", stream)
				return
			}
			if strings.Contains(err.Error(), "NOGROUP") {
				slog.Warn("消费组不存在，尝试重建", "stream", stream)
				if e := e.createGroup(ctx, stream); e != nil {
					slog.Error("重建消费组失败", "stream", stream, "error", e)
				}
				continue
			}
			slog.Error("读取 Stream 失败", "stream", stream, "error", err)
			continue
		}

		for _, result := range msgs {
			for _, msg := range result.Messages {
				slog.Info("收到 K 线",
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
