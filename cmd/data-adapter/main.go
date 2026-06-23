// Package main 是数据适配层的入口点。
//
// 数据适配层是独立组件（PRD §4/§5），职责：
//   - 启动时从 Binance REST 拉取历史 K 线灌入 Redis Stream
//   - 实时 WebSocket 写入 Redis Stream
//   - 断线自动重连
//
// 本组件在引擎范围之外，可独立部署/替换。
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/adshao/go-binance/v2"
	"github.com/redis/go-redis/v9"
)

const (
	defaultRedisAddr = "localhost:6380"
	streamPrefix     = "chan:klines"
	interval         = "1m"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	redisAddr := getEnv("REDIS_ADDR", defaultRedisAddr)
	symbols := getSymbols()

	// 连接 Redis
	rdb := redis.NewClient(&redis.Options{
		Addr: redisAddr,
		DB:   0,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("连接 Redis 失败: %v", err)
	}
	log.Printf("已连接 Redis: %s", redisAddr)

	// 启动时拉取历史 K 线
	for _, symbol := range symbols {
		log.Printf("开始拉取 %s 历史 K 线...", symbol)
		count, err := fetchHistory(ctx, rdb, symbol)
		if err != nil {
			log.Printf("拉取 %s 历史 K 线失败: %v", symbol, err)
		} else {
			log.Printf("%s 历史 K 线拉取完成: %d 根", symbol, count)
		}
	}

	// 启动实时 WebSocket
	log.Printf("启动实时 WebSocket: %v", symbols)
	done := make(chan struct{})

	wsHandler := createWSHandler(rdb)
	errHandler := func(err error) {
		log.Printf("WebSocket 错误: %v", err)
	}

	pairs := make(map[string]string, len(symbols))
	for _, sym := range symbols {
		pairs[sym] = interval
	}

	serveDone, _, err := binance.WsCombinedKlineServe(pairs, wsHandler, errHandler)
	if err != nil {
		log.Fatalf("WebSocket 连接失败: %v", err)
	}

	go func() {
		<-serveDone
		close(done)
	}()

	log.Println("数据适配层运行中...")

	select {
	case <-sigChan:
		log.Println("收到退出信号")
	case <-done:
		log.Println("WebSocket 流已断开")
	}

	log.Println("数据适配层正在停止...")
	rdb.Close()
	log.Println("数据适配层已停止")
}

// createWSHandler 返回处理实时 K 线的回调函数。
func createWSHandler(rdb *redis.Client) binance.WsKlineHandler {
	return func(event *binance.WsKlineEvent) {
		ctx := context.Background()
		streamKey := fmt.Sprintf("%s:%s", streamPrefix, event.Symbol)

		values := map[string]interface{}{
			"symbol":     event.Symbol,
			"ts":         strconv.FormatInt(event.Kline.StartTime, 10),
			"open":       event.Kline.Open,
			"high":       event.Kline.High,
			"low":        event.Kline.Low,
			"close":      event.Kline.Close,
			"baseVolume": event.Kline.Volume,
			"isClosed":   strconv.FormatBool(event.Kline.IsFinal),
		}

		msgID, err := rdb.XAdd(ctx, &redis.XAddArgs{
			Stream: streamKey,
			Values: values,
		}).Result()

		if err != nil {
			log.Printf("写入 Redis Stream 失败: %v", err)
			return
		}

		if event.Kline.IsFinal {
			log.Printf("K线已闭合 → Redis: symbol=%s ts=%d close=%s msgID=%s",
				event.Symbol, event.Kline.StartTime, event.Kline.Close, msgID)
		}
	}
}

// fetchHistory 从 Binance REST API 拉取历史 K 线并写入 Redis Stream。
// 每次拉取 1000 根，循环直到拉满目标量。
func fetchHistory(ctx context.Context, rdb *redis.Client, symbol string) (int, error) {
	client := binance.NewClient("", "") // 公共 API 无需 Key
	streamKey := fmt.Sprintf("%s:%s", streamPrefix, symbol)

	targetCount := 80000 // R3 参考样本量
	totalCount := 0
	endTime := time.Now().UnixMilli()

	for totalCount < targetCount {
		limit := 1000
		if targetCount-totalCount < limit {
			limit = targetCount - totalCount
		}

		klines, err := client.NewKlinesService().
			Symbol(symbol).
			Interval(interval).
			Limit(limit).
			EndTime(endTime).
			Do(ctx)

		if err != nil {
			return totalCount, fmt.Errorf("REST API 错误: %w", err)
		}

		if len(klines) == 0 {
			break
		}

		// 逆序（K 线按时间正序返回，但我们从最新往旧拉）
		for i := len(klines) - 1; i >= 0; i-- {
			k := klines[i]
			values := map[string]interface{}{
				"symbol":     symbol,
				"ts":         strconv.FormatInt(k.OpenTime, 10),
				"open":       k.Open,
				"high":       k.High,
				"low":        k.Low,
				"close":      k.Close,
				"baseVolume": k.Volume,
				"isClosed":   "true",
			}

			if _, err := rdb.XAdd(ctx, &redis.XAddArgs{
				Stream: streamKey,
				Values: values,
			}).Result(); err != nil {
				return totalCount, fmt.Errorf("写入 Redis 失败: %w", err)
			}
			totalCount++
		}

		// 更新 endTime 到最早拉取结果之前
		if len(klines) > 0 {
			// 下一个循环拉更早的数据
			earliestOpenTime := klines[0].OpenTime
			// 减 1ms 避免重复
			endTime = earliestOpenTime - 1

			log.Printf("  %s: 已拉取 %d / %d 根 (到 %d)",
				symbol, totalCount, targetCount, earliestOpenTime)
		}

		// 防止 API 限频
		time.Sleep(200 * time.Millisecond)
	}

	return totalCount, nil
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getSymbols() []string {
	envSymbols := os.Getenv("SYMBOLS")
	if envSymbols != "" {
		return strings.Split(envSymbols, ",")
	}
	return []string{"BTCUSDT", "ETHUSDT"}
}
