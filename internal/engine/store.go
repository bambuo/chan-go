package engine

import (
	"context"
	"fmt"
	"sync"

	"github.com/redis/go-redis/v9"
)

// RedisEntry 表示待写入 Redis ZSET 的条目。
type RedisEntry struct {
	Score float64
	Value string
}

// NewRedisEntry 创建一个 Redis 条目。
func NewRedisEntry(score float64, value string) *RedisEntry {
	return &RedisEntry{Score: score, Value: value}
}

// ResultStore 是管线结果持久化到 Redis 的存储层。
// 支持启用/禁用两种模式，禁用模式在 Redis 不可用时优雅降级。
type ResultStore struct {
	client  *redis.Client
	enabled bool
	mu      sync.Mutex
	ctx     context.Context
}

// NewResultStore 创建一个启用的结果存储器。
func NewResultStore(client *redis.Client) *ResultStore {
	return &ResultStore{
		client:  client,
		enabled: true,
		ctx:     context.Background(),
	}
}

// NewDisabledResultStore 创建一个禁用的结果存储器（Redis 不可用时使用）。
func NewDisabledResultStore() *ResultStore {
	return &ResultStore{
		client:  nil,
		enabled: false,
	}
}

// SaveOne 写入单条结果到 Redis ZSET，按实体类型分 key。
// Key 格式: trade:{SYMBOL}:L{depth}:{entityType}
func (s *ResultStore) SaveOne(symbol string, depth int, entityType string, entry *RedisEntry) {
	if !s.enabled {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	key := fmt.Sprintf("trade:%s:L%d:%s", symbol, depth, entityType)
	if err := s.client.ZAdd(s.ctx, key, redis.Z{
		Score:  entry.Score,
		Member: entry.Value,
	}).Err(); err != nil {
		// 静默处理——写入失败不影响管线处理
	}
}

// SaveBatch 批量写入各类实体到 Redis ZSET。
// entries 的 key 为 entityType，value 为条目列表。
func (s *ResultStore) SaveBatch(symbol string, depth int, entries map[string][]*RedisEntry) {
	if !s.enabled || len(entries) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	for entityType, items := range entries {
		if len(items) == 0 {
			continue
		}
		key := fmt.Sprintf("trade:%s:L%d:%s", symbol, depth, entityType)
		for _, item := range items {
			if err := s.client.ZAdd(s.ctx, key, redis.Z{
				Score:  item.Score,
				Member: item.Value,
			}).Err(); err != nil {
				// 静默处理
			}
		}
	}
}

// Close 释放资源。
func (s *ResultStore) Close() {
	if s.enabled && s.client != nil {
		_ = s.client.Close()
	}
}
