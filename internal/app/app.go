// Package app 封装系统核心组件与应用生命周期。
package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"trade/internal/binance"
	"trade/internal/chanlun"
	"trade/internal/config"
	"trade/internal/db"
	"trade/internal/eventbus"
	"trade/internal/log"
	"trade/internal/types"
)

// App 是缠论分析系统的主控对象，封装所有组件与生命周期。
type App struct {
	cfg    config.Config
	logger *slog.Logger

	bus *eventbus.Bus
	db  *db.Client
	ws  *binance.WSClient

	containProc *chanlun.ContainProcessor
	fractalProc *chanlun.FractalProcessor

	// 订阅ID，关闭时需要取消注册。
	dbSubID      int64
	chanlunSubID int64

	// 生命周期控制。
	cancel context.CancelFunc
}

// New 根据配置构建 App，依次初始化所有子系统。
// 返回的 App 尚未运行，需调用 Run 方法。
func New(cfg config.Config) (*App, error) {
	log.Init(log.Config{
		Level:     cfg.LogLevel,
		JSON:      cfg.LogJSON,
		AddSource: true,
		Output:    os.Stdout,
	})
	logger := log.Component("main")
	logger.Info("启动缠论分析系统", "config", fmt.Sprintf("%+v", cfg))

	// 确保数据目录存在。
	if err := os.MkdirAll(filepath.Dir(cfg.DBFile()), 0755); err != nil {
		return nil, fmt.Errorf("创建数据目录: %w", err)
	}

	// 初始化事件总线。
	bus := eventbus.New()

	// 初始化数据库。
	dbClient, err := db.NewClient(context.Background(), cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("初始化数据库: %w", err)
	}

	// 订阅数据库处理器。
	dbSubID := bus.Subscribe(types.EventKlineClosed, dbClient.InsertClosedKlineHandler(context.Background()))

	// 初始化缠论分析器。
	containProc := chanlun.NewContainProcessor()
	fractalProc := chanlun.NewFractalProcessor()

	// 订阅实时K线事件 — 缠论分析。
	chanlunSubID := bus.Subscribe(types.EventKlineRealtime, func(evt types.KlineEvent) {
		elements := containProc.Process(evt.Kline)
		if len(elements) > 0 {
			last := elements[len(elements)-1]
			fractals := fractalProc.Process(last)
			if len(fractals) > 0 && fractals[len(fractals)-1].Confirmed {
				logger.Debug("识别到分型",
					"type", fractals[len(fractals)-1].Type,
					"high", fractals[len(fractals)-1].High,
					"low", fractals[len(fractals)-1].Low,
				)
			}
		}
	})

	// K 线闭合时更新缠论状态。
	bus.Subscribe(types.EventKlineClosed, func(evt types.KlineEvent) {
		elements := containProc.Process(evt.Kline)
		if len(elements) > 0 {
			fractalProc.Process(elements[len(elements)-1])
		}
	})

	// 创建 WebSocket 客户端。
	wsClient := binance.NewWSClient(cfg.Symbols, cfg.Interval, bus)

	return &App{
		cfg:          cfg,
		logger:       logger,
		bus:          bus,
		db:           dbClient,
		ws:           wsClient,
		containProc:  containProc,
		fractalProc:  fractalProc,
		dbSubID:      dbSubID,
		chanlunSubID: chanlunSubID,
	}, nil
}

// Run 启动系统主循环，阻塞直到信号(SIGINT/SIGTERM)或内部异常退出。
func (a *App) Run(ctx context.Context) error {
	ctx, a.cancel = context.WithCancel(ctx)
	defer a.cancel()

	// 监听系统信号，触发优雅关闭。
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sigChan:
			a.logger.Info("收到信号，正在关闭...")
		case <-ctx.Done():
		}
		a.Shutdown()
	}()

	a.logger.Info("系统初始化完成，启动WebSocket流",
		"symbols", a.cfg.Symbols,
		"interval", a.cfg.Interval,
	)

	if err := a.ws.Start(); err != nil {
		return err
	}

	a.logger.Info("系统已停止")
	return nil
}

// Shutdown 优雅关闭所有子系统。
func (a *App) Shutdown() {
	a.logger.Info("正在关闭子系统...")

	// 取消注册事件订阅。
	a.bus.Unsubscribe(types.EventKlineRealtime, a.chanlunSubID)
	a.bus.Unsubscribe(types.EventKlineClosed, a.dbSubID)

	// 停止 WebSocket 客户端。
	a.ws.Stop()

	// 关闭数据库连接。
	if err := a.db.Close(); err != nil {
		a.logger.Error("关闭数据库失败", "error", err)
	}

	if a.cancel != nil {
		a.cancel()
	}

	a.logger.Info("系统已关闭")
}

// Bus 暴露事件总线（用于测试或扩展）。
func (a *App) Bus() *eventbus.Bus {
	return a.bus
}

// DB 暴露数据库客户端（用于测试或扩展）。
func (a *App) DB() *db.Client {
	return a.db
}
