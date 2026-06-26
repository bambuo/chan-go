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

// key 返回 Redis key：trade:kline:chan:{symbol}
func (s *ChanKLineStore) key() string {
	return fmt.Sprintf("trade:kline:chan:%s", s.symbol)
}

// Save 将缠论 K 线列表追加到 Redis List。
func (s *ChanKLineStore) Save(ctx context.Context, lines []*ChanKLine) error {
	if len(lines) == 0 {
		return nil
	}

	// 序列化为 JSON
	data, err := json.Marshal(lines)
	if err != nil {
		return fmt.Errorf("序列化失败: %w", err)
	}

	// 追加到 Redis List
	if err := s.rdb.RPush(ctx, s.key(), data).Err(); err != nil {
		return fmt.Errorf("写入 Redis 失败: %w", err)
	}

	return nil
}

// Load 从 Redis List 加载最新的 N 条缠论 K 线。
func (s *ChanKLineStore) Load(ctx context.Context, count int64) ([]*ChanKLine, error) {
	// 获取最新的 count 条数据
	data, err := s.rdb.LRange(ctx, s.key(), -count, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("读取 Redis 失败: %w", err)
	}

	var result []*ChanKLine
	for _, item := range data {
		var lines []*ChanKLine
		if err := json.Unmarshal([]byte(item), &lines); err != nil {
			return nil, fmt.Errorf("反序列化失败: %w", err)
		}
		result = append(result, lines...)
	}

	return result, nil
}

// Clear 清空 Redis 中的缠论 K 线数据。
func (s *ChanKLineStore) Clear(ctx context.Context) error {
	return s.rdb.Del(ctx, s.key()).Err()
}
