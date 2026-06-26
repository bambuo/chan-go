// Package app 负责系统组装与生命周期管理。
package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// ChanKLineStore 负责缠论 K 线的 Redis 持久化。
type ChanKLineStore struct {
	rdb    *redis.Client
	symbol string
}

// NewChanKLineStore 创建一个新的缠论 K 线存储实例。
func NewChanKLineStore(rdb *redis.Client, symbol string) *ChanKLineStore {
	return &ChanKLineStore{
		rdb:    rdb,
		symbol: symbol,
	}
}

// key 返回合并 K 线 Redis key：trade:kline:chan:{symbol}
func (s *ChanKLineStore) key() string {
	return fmt.Sprintf("trade:kline:chan:%s", s.symbol)
}

// fractalKey 返回分型 Redis key：trade:kline:{symbol}:fractal
func (s *ChanKLineStore) fractalKey() string {
	return fmt.Sprintf("trade:kline:%s:fractal", s.symbol)
}

// SaveFractal 将已确认分型的缠论 K 线写入 Redis ZSET。
func (s *ChanKLineStore) SaveFractal(ctx context.Context, kline *ChanKLine) error {
	data, err := json.Marshal(kline)
	if err != nil {
		return fmt.Errorf("序列化失败: %w", err)
	}

	return s.rdb.ZAdd(ctx, s.fractalKey(), redis.Z{
		Score:  float64(kline.Timestamp),
		Member: data,
	}).Err()
}
func (s *ChanKLineStore) Save(ctx context.Context, lines []*ChanKLine) error {
	if len(lines) == 0 {
		return nil
	}

	// 添加到 Redis ZSET
	for _, line := range lines {
		data, err := json.Marshal(line)
		if err != nil {
			return fmt.Errorf("序列化失败: %w", err)
		}

		if err := s.rdb.ZAdd(ctx, s.key(), redis.Z{
			Score:  float64(line.Timestamp),
			Member: data,
		}).Err(); err != nil {
			return fmt.Errorf("写入 Redis 失败: %w", err)
		}
	}

	return nil
}

// Load 从 Redis ZSET 加载最新的 N 条缠论 K 线（按时间戳降序）。
func (s *ChanKLineStore) Load(ctx context.Context, count int64) ([]*ChanKLine, error) {
	// 获取最新的 count 条数据（按 score 降序）
	data, err := s.rdb.ZRangeArgs(ctx, redis.ZRangeArgs{
		Key:   s.key(),
		Start: 0,
		Stop:  count - 1,
		Rev:   true,
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("读取 Redis 失败: %w", err)
	}

	var result []*ChanKLine
	for _, item := range data {
		var kline ChanKLine
		if err := json.Unmarshal([]byte(item), &kline); err != nil {
			return nil, fmt.Errorf("反序列化失败: %w", err)
		}
		result = append(result, &kline)
	}

	return result, nil
}

// Clear 清空 Redis 中的缠论 K 线数据。
func (s *ChanKLineStore) Clear(ctx context.Context) error {
	return s.rdb.Del(ctx, s.key()).Err()
}

// ClearFractal 清空分型数据。
func (s *ChanKLineStore) ClearFractal(ctx context.Context) error {
	return s.rdb.Del(ctx, s.fractalKey()).Err()
}
