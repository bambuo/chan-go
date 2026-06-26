// Package app 负责系统组装与生命周期管理。
package app

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/redis/go-redis/v9"

	"trade/internal/logger"
)

// Pipeline 是一个交易对的处理管线，负责从 Redis 流中消费 K 线数据并进行缠论分析。
type Pipeline struct {
	symbol   string
	stream   string
	rdb      *redis.Client
	log      *logger.Logger
	group    string
	consumer string // 每个管线唯一的消费者名称

	// 缠论算法状态
	mu          sync.Mutex
	mergedLines []*MergedKLine // 合并 K 线序列
	lastKLine   *KLine         // 上一根 K 线
}

// NewPipeline 创建一个新的处理管线实例。
// 为每个管线生成唯一的消费者名称：{baseConsumer}-{symbol}
func NewPipeline(symbol, stream string, rdb *redis.Client, log *logger.Logger, group, baseConsumer string) *Pipeline {
	consumer := fmt.Sprintf("%s-%s", baseConsumer, symbol)
	return &Pipeline{
		symbol:   symbol,
		stream:   stream,
		rdb:      rdb,
		log:      log.With("symbol", symbol),
		group:    group,
		consumer: consumer,
	}
}

// Start 启动处理管线，开始消费 Redis 流。
func (p *Pipeline) Start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	p.log.Info("处理管线已启动",
		"stream", p.stream,
		"group", p.group,
		"consumer", p.consumer,
	)

	for {
		if ctx.Err() != nil {
			p.log.Info("处理管线已停止")
			return
		}

		msgs, err := p.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    p.group,
			Consumer: p.consumer,
			Streams:  []string{p.stream, ">"},
			Count:    10,
			Block:    0,
		}).Result()
		if err != nil {
			if ctx.Err() != nil {
				p.log.Info("处理管线已停止")
				return
			}
			p.log.Error("读取 Stream 失败", "error", err)
			continue
		}

		for _, result := range msgs {
			for _, msg := range result.Messages {
				if err := p.processMessage(ctx, msg); err != nil {
					p.log.Error("处理消息失败", "id", msg.ID, "error", err)
				}
			}
		}
	}
}

// processMessage 处理单个 Redis 消息。
func (p *Pipeline) processMessage(ctx context.Context, msg redis.XMessage) error {
	// 解析 K 线数据
	kline, err := p.parseKLine(msg)
	if err != nil {
		return fmt.Errorf("解析 K 线失败: %w", err)
	}

	// 处理 K 线
	return p.processKLine(ctx, kline)
}

// parseKLine 从 Redis 消息中解析 K 线数据。
func (p *Pipeline) parseKLine(msg redis.XMessage) (*KLine, error) {
	symbol, ok := msg.Values["symbol"].(string)
	if !ok {
		return nil, fmt.Errorf("缺少 symbol 字段")
	}

	open, err := parseFloat(msg.Values["open"])
	if err != nil {
		return nil, fmt.Errorf("解析 open 失败: %w", err)
	}

	high, err := parseFloat(msg.Values["high"])
	if err != nil {
		return nil, fmt.Errorf("解析 high 失败: %w", err)
	}

	low, err := parseFloat(msg.Values["low"])
	if err != nil {
		return nil, fmt.Errorf("解析 low 失败: %w", err)
	}

	close, err := parseFloat(msg.Values["close"])
	if err != nil {
		return nil, fmt.Errorf("解析 close 失败: %w", err)
	}

	volume, err := parseFloat(msg.Values["volume"])
	if err != nil {
		return nil, fmt.Errorf("解析 volume 失败: %w", err)
	}

	ts, err := parseInt64(msg.Values["ts"])
	if err != nil {
		return nil, fmt.Errorf("解析 ts 失败: %w", err)
	}

	return &KLine{
		Symbol:    symbol,
		Open:      open,
		High:      high,
		Low:       low,
		Close:     close,
		Volume:    volume,
		Timestamp: ts,
	}, nil
}

// processKLine 处理一根 K 线，执行缠论算法。
func (p *Pipeline) processKLine(ctx context.Context, kline *KLine) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 1. 包含处理
	if err := p.processInclusion(kline); err != nil {
		return fmt.Errorf("包含处理失败: %w", err)
	}

	// 2. 分型识别（待实现）
	// 3. 笔识别（待实现）
	// 4. 线段识别（待实现）
	// 5. 中枢识别（待实现）
	// 6. 走势类型识别（待实现）
	// 7. 背驰判定（待实现）
	// 8. 买卖点识别（待实现）

	p.log.Info("处理 K 线完成",
		"open", kline.Open,
		"high", kline.High,
		"low", kline.Low,
		"close", kline.Close,
		"ts", kline.Timestamp,
		"mergedLines", len(p.mergedLines),
	)

	return nil
}

// processInclusion 执行包含处理算法。
// 根据缠论原文，处理 K 线之间的包含关系。
func (p *Pipeline) processInclusion(kline *KLine) error {
	// 如果没有合并 K 线，直接添加第一根
	if len(p.mergedLines) == 0 {
		merged := &MergedKLine{
			High:      kline.High,
			Low:       kline.Low,
			Direction: 1, // 初始方向向上
			Index:     0,
		}
		p.mergedLines = append(p.mergedLines, merged)
		p.lastKLine = kline
		return nil
	}

	// 获取最后一根合并 K 线
	lastMerged := p.mergedLines[len(p.mergedLines)-1]

	// 判断包含关系
	if p.isInclusion(lastMerged, kline) {
		// 存在包含关系，进行合并
		p.mergeKLines(lastMerged, kline)
	} else {
		// 不存在包含关系，直接添加
		merged := &MergedKLine{
			High:      kline.High,
			Low:       kline.Low,
			Direction: lastMerged.Direction, // 保持当前方向
			Index:     len(p.mergedLines),
		}
		p.mergedLines = append(p.mergedLines, merged)

		// 更新方向
		p.updateDirection()
	}

	p.lastKLine = kline
	return nil
}

// isInclusion 判断两根 K 线是否存在包含关系。
// 包含关系：当前高 <= 前一根高 且 当前低 >= 前一根低，或者
// 当前高 >= 前一根高 且 当前低 <= 前一根低
func (p *Pipeline) isInclusion(merged *MergedKLine, kline *KLine) bool {
	return (kline.High <= merged.High && kline.Low >= merged.Low) ||
		(kline.High >= merged.High && kline.Low <= merged.Low)
}

// mergeKLines 合并 K 线。
// 根据方向决定合并方式：
// - 向上：取两边高点中的更高者、低点中的更高者
// - 向下：取两边高点中的更低者、低点中的更低者
func (p *Pipeline) mergeKLines(merged *MergedKLine, kline *KLine) {
	if merged.Direction == 1 {
		// 向上：取高高，低高
		merged.High = max(merged.High, kline.High)
		merged.Low = max(merged.Low, kline.Low)
	} else {
		// 向下：取低高，低低
		merged.High = min(merged.High, kline.High)
		merged.Low = min(merged.Low, kline.Low)
	}
}

// updateDirection 更新方向。
// 以非包含序列中最后两个元素为基准判定方向：
// - 若第二根高点 > 第一根高点 且 第二根低点 > 第一根低点 → 方向更新为向上
// - 若第二根高点 < 第一根高点 且 第二根低点 < 第一根低点 → 方向更新为向下
// - 否则方向保持不变
func (p *Pipeline) updateDirection() {
	if len(p.mergedLines) < 2 {
		return
	}

	current := p.mergedLines[len(p.mergedLines)-1]
	previous := p.mergedLines[len(p.mergedLines)-2]

	if current.High > previous.High && current.Low > previous.Low {
		current.Direction = 1 // 向上
	} else if current.High < previous.High && current.Low < previous.Low {
		current.Direction = -1 // 向下
	} else {
		current.Direction = previous.Direction // 保持前一个方向
	}
}

// max 返回两个浮点数中的较大值。
func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// min 返回两个浮点数中的较小值。
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// parseFloat 从接口值中解析浮点数。
func parseFloat(v interface{}) (float64, error) {
	switch val := v.(type) {
	case string:
		return strconv.ParseFloat(val, 64)
	case float64:
		return val, nil
	case int64:
		return float64(val), nil
	default:
		return 0, fmt.Errorf("无法解析为浮点数: %v", v)
	}
}

// parseInt64 从接口值中解析整数。
func parseInt64(v interface{}) (int64, error) {
	switch val := v.(type) {
	case string:
		return strconv.ParseInt(val, 10, 64)
	case int64:
		return val, nil
	case float64:
		return int64(val), nil
	default:
		return 0, fmt.Errorf("无法解析为整数: %v", v)
	}
}
