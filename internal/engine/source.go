package engine

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"trade/internal/chanlun"
	"trade/internal/logger"
)

// DataSource 是从 Redis Stream 消费 K 线数据的数据源。
// 每个交易对一个独立协程消费 trade:kline:{symbol} 流。
type DataSource struct {
	bus           *GenericBus
	client        *redis.Client
	symbols       []string
	streamPrefix  string
	consumerGroup string
	consumerName  string
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	log           *logger.Logger
}

// NewDataSource 创建一个新的数据源。
func NewDataSource(bus *GenericBus, client *redis.Client, symbols []string, streamPrefix, group string, log *logger.Logger) *DataSource {
	return &DataSource{
		bus:           bus,
		client:        client,
		symbols:       symbols,
		streamPrefix:  streamPrefix,
		consumerGroup: group,
		consumerName:  "chan-go-1",
		log:           log.With("module", "datasource"),
	}
}

// Start 启动所有 symbol 的数据消费协程。
func (ds *DataSource) Start(ctx context.Context) {
	ctx, ds.cancel = context.WithCancel(ctx)

	for _, sym := range ds.symbols {
		streamKey := ds.streamKey(sym)
		err := ds.client.XGroupCreateMkStream(ctx, streamKey, ds.consumerGroup, "$").Err()
		if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
			ds.log.Warn("创建消费组失败", "stream", streamKey, "error", err)
		}

		symCopy := sym
		ds.wg.Add(1)
		go ds.consumeLoop(ctx, symCopy)
	}
	ds.log.Info("数据源已启动", "symbols", ds.symbols)
}

// Stop 停止所有消费协程。
func (ds *DataSource) Stop() {
	if ds.cancel != nil {
		ds.cancel()
	}
	ds.wg.Wait()
	ds.log.Info("数据源已停止")
}

func (ds *DataSource) streamKey(symbol string) string {
	return ds.streamPrefix + ":" + symbol
}

// consumeLoop 是单个 symbol 的消费循环。
func (ds *DataSource) consumeLoop(ctx context.Context, symbol string) {
	defer ds.wg.Done()
	streamKey := ds.streamKey(symbol)
	log := ds.log.With("stream", streamKey)

	log.Info("开始消费 Stream")

	for {
		select {
		case <-ctx.Done():
			log.Info("消费协程已停止")
			return
		default:
		}

		results, err := ds.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    ds.consumerGroup,
			Consumer: ds.consumerName,
			Streams:  []string{streamKey, ">"},
			Count:    10,
			Block:    2 * time.Second,
		}).Result()

		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if strings.Contains(err.Error(), "NOGROUP") {
				log.Warn("消费组不存在，尝试重建")
				_ = ds.client.XGroupCreateMkStream(ctx, streamKey, ds.consumerGroup, "$").Err()
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}

		for _, result := range results {
			for _, msg := range result.Messages {
				kline := ds.parseKLine(msg, symbol)
				if kline != nil {
					ds.bus.Publish(Event{
						Type: EventKlineReceived,
						Data: kline,
					})
					if kline.IsClosed {
						ds.bus.Publish(Event{
							Type: EventKlineClosed,
							Data: kline,
						})
					}
				}
				_ = ds.client.XAck(ctx, streamKey, ds.consumerGroup, msg.ID)
			}
		}
	}
}

// parseKLine 从 Redis Stream 消息中解析 K 线数据。
func (ds *DataSource) parseKLine(msg redis.XMessage, symbol string) *chanlun.KLine {
	getStr := func(key string) string {
		v, ok := msg.Values[key]
		if !ok {
			return ""
		}
		s, _ := v.(string)
		return s
	}

	getFloat := func(key string) float64 {
		s := getStr(key)
		if s == "" {
			return 0
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0
		}
		return v
	}

	getInt := func(key string) int64 {
		s := getStr(key)
		if s == "" {
			return 0
		}
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0
		}
		return v
	}

	openTime := getInt("openTime")
	if openTime == 0 {
		openTime = getInt("ts")
	}
	volume := getFloat("volume")
	if volume == 0 {
		volume = getFloat("baseVolume")
	}
	isClosed := getStr("isClosed") == "true"

	return &chanlun.KLine{
		Symbol:    symbol,
		OpenTime:  openTime,
		CloseTime: getInt("closeTime"),
		Open:      getFloat("open"),
		High:      getFloat("high"),
		Low:       getFloat("low"),
		Close:     getFloat("close"),
		Volume:    volume,
		IsClosed:  isClosed,
	}
}
