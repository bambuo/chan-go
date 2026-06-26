// Package app 提供缠论信号分析系统的应用协调器。
package app

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	trade_chanlun "trade/internal/chanlun"
	"trade/internal/config"
	"trade/internal/engine"
	"trade/internal/logger"
)

// App 是应用主协调器。
// 负责初始化所有组件、事件订阅、生命周期管理。
type App struct {
	config     *config.Config
	bus        *engine.GenericBus
	l1         *engine.LevelRunner
	dataSource *engine.DataSource
	store      *engine.ResultStore
	log        *logger.Logger
}

// NewApp 创建并初始化应用。
// 初始化顺序：
//  1. 日志
//  2. Redis 连接 → ResultStore（失败则降级）
//  3. 事件总线
//  4. 级别运行器 L1
//  5. 数据源
//  6. 事件订阅
func NewApp(cfg *config.Config) (*App, error) {
	log, err := logger.New()
	if err != nil {
		return nil, fmt.Errorf("初始化日志失败: %w", err)
	}

	// 尝试连接 Redis
	var store *engine.ResultStore
	rdb, err := connectRedis(cfg, log)
	if err != nil {
		log.Warn("Redis 不可用，结果不持久化", "error", err)
		store = engine.NewDisabledResultStore()
	} else {
		store = engine.NewResultStore(rdb)
		log.Info("Redis 连接已建立", "addr", cfg.Redis.Addr)
	}

	bus := engine.NewGenericBus()

	// 主 symbol（L1 的主交易对）
	primarySymbol := "BTCUSDT"
	if len(cfg.Server.Symbols) > 0 {
		primarySymbol = cfg.Server.Symbols[0]
	}

	l1 := engine.NewLevelRunner(1, primarySymbol, store, log)

	dataSource := engine.NewDataSource(bus, rdb, cfg.Server.Symbols,
		cfg.Server.StreamPrefix, cfg.Server.ConsumerGroup, log)

	// 事件订阅
	bus.Subscribe(engine.EventKlineReceived, func(e engine.Event) {
		if kline, ok := e.Data.(*trade_chanlun.KLine); ok {
			l1.Enqueue(kline)
		}
	})

	bus.Subscribe(engine.EventError, func(e engine.Event) {
		if msg, ok := e.Data.(string); ok {
			log.Error(msg)
		}
	})

	a := &App{
		config:     cfg,
		bus:        bus,
		l1:         l1,
		dataSource: dataSource,
		store:      store,
		log:        log,
	}

	return a, nil
}

// Run 启动系统。
func (a *App) Run(ctx context.Context) error {
	a.log.Info("缠论实时分析系统 · Chan Theory Live Analyzer")
	a.log.Info("信号分析引擎启动中...")

	// 启动 L1 级别运行器
	l1Ctx, l1Cancel := context.WithCancel(ctx)
	defer l1Cancel()
	a.l1.Start(l1Ctx)

	// 启动数据源
	a.dataSource.Start(ctx)

	a.log.Info("系统初始化完成", "symbols", a.config.Server.Symbols)
	a.log.Info("监控交易对", "symbols", a.config.Server.Symbols)

	// 阻塞等待 context 取消
	<-ctx.Done()
	a.log.Info("收到关闭信号，正在停止系统...")
	return nil
}

// Shutdown 停止系统。
func (a *App) Shutdown() {
	a.log.Info("正在停止系统...")

	a.dataSource.Stop()
	a.l1.Stop()

	if a.store != nil {
		a.store.Close()
	}

	if a.log != nil {
		_ = a.log.Sync()
	}

	a.log.Info("系统已安全关闭")
}

// connectRedis 连接 Redis。
func connectRedis(cfg *config.Config, log *logger.Logger) (*redis.Client, error) {
	var rdb redis.UniversalClient

	if cfg.Redis.URL != "" {
		opt, err := redis.ParseURL(cfg.Redis.URL)
		if err != nil {
			return nil, fmt.Errorf("解析 Redis URL 失败: %w", err)
		}
		rdb = redis.NewClient(opt)
	} else {
		rdb = redis.NewClient(&redis.Options{
			Addr:         cfg.Redis.Addr,
			Password:     cfg.Redis.Password,
			DB:           cfg.Redis.DB,
			MaxRetries:   cfg.Redis.MaxRetries,
			DialTimeout:  cfg.Redis.DialTimeout,
			ReadTimeout:  cfg.Redis.ReadTimeout,
			WriteTimeout: cfg.Redis.WriteTimeout,
		})
	}

	client := rdb.(*redis.Client)

	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("Redis 连通性检查失败: %w", err)
	}

	return client, nil
}
