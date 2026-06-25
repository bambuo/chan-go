// Package redis 提供 Redis 客户端连接的创建与生命周期管理.
package redis

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"trade/internal/config"
)

// Client 是对 go-redis Client 的轻量封装，提供健康检查和优雅关闭.
type Client struct {
	*redis.Client
}

// NewClient 根据配置创建一个 Redis 客户端并验证连通性.
func NewClient(ctx context.Context, cfg config.RedisConfig) (*Client, error) {
	var rdb redis.UniversalClient

	if cfg.URL != "" {
		opt, err := redis.ParseURL(cfg.URL)
		if err != nil {
			return nil, fmt.Errorf("解析 Redis URL: %w", err)
		}
		rdb = redis.NewClient(opt)
	} else {
		rdb = redis.NewClient(&redis.Options{
			Addr:         cfg.Addr,
			Password:     cfg.Password,
			DB:           cfg.DB,
			MaxRetries:   cfg.MaxRetries,
			DialTimeout:  cfg.DialTimeout,
			ReadTimeout:  cfg.ReadTimeout,
			WriteTimeout: cfg.WriteTimeout,
		})
	}

	client := &Client{rdb.(*redis.Client)}

	// 验证连通性
	if err := client.Ping(ctx); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("Redis 连通性检查失败: %w", err)
	}

	slog.Info("Redis 连接已建立",
		"addr", client.Options().Addr,
		"db", client.Options().DB,
	)
	return client, nil
}

// Ping 检查 Redis 连接是否正常.
func (c *Client) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return c.Client.Ping(ctx).Err()
}

// Close 优雅关闭 Redis 连接.
func (c *Client) Close() error {
	slog.Info("正在关闭 Redis 连接")
	return c.Client.Close()
}
