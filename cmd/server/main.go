// Package main 是缠论K线分析系统的入口点。
//
// 按顺序初始化所有组件：
//  1. 结构化日志
//  2. 事件总线
//  3. SQLite数据库
//  4. 缠论分析流水线
//  5. 币安WebSocket客户端
//
// 系统订阅币安1分钟K线流，经过缠论包含处理和分型分析，
// 并将闭合K线持久化到SQLite数据库中。
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"trade/internal/binance"
	"trade/internal/chanlun"
	"trade/internal/db"
	"trade/internal/eventbus"
	"trade/internal/log"
	"trade/internal/types"
)

func main() {
	// 1. 初始化结构化日志。
	cfg := log.DefaultConfig()
	cfg.Level = "info"
	cfg.JSON = false
	log.Init(cfg)
	logger := log.Component("main")

	logger.Info("启动缠论分析系统")

	// 2. 创建事件总线。
	bus := eventbus.New()

	// 3. 确保数据目录存在，然后创建数据库客户端并自动迁移。
	if err := os.MkdirAll("data", 0755); err != nil {
		logger.Error("创建数据目录失败", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dbClient, err := db.NewClient(ctx, "file:data/klines.db?cache=shared&_journal_mode=WAL")
	if err != nil {
		logger.Error("初始化数据库失败", "error", err)
		os.Exit(1)
	}
	defer dbClient.Close()

	// 4. 订阅数据库处理器到闭合K线事件。
	dbSubID := bus.Subscribe(types.EventKlineClosed, dbClient.InsertClosedKlineHandler(ctx))

	// 5. 设置缠论分析流水线。
	containProc := chanlun.NewContainProcessor()
	fractalProc := chanlun.NewFractalProcessor()

	// 订阅缠论流水线到实时K线事件。
	chanlunSubID := bus.Subscribe(types.EventKlineRealtime, func(evt types.KlineEvent) {
		// 通过包含算法处理原始K线。
		elements := containProc.Process(evt.Kline)
		_ = elements // 非包含元素可用于进一步分析。

		// 将最后一个非包含元素送入分型分析。
		if len(elements) > 0 {
			lastElem := elements[len(elements)-1]
			fractals := fractalProc.Process(lastElem)
			if len(fractals) > 0 {
				latestFractal := fractals[len(fractals)-1]
				if latestFractal.Confirmed {
					logger.Debug("识别到分型",
						"type", latestFractal.Type,
						"high", latestFractal.High,
						"low", latestFractal.Low,
					)
				}
			}
		}
	})

	// 也订阅闭合事件用于缠论（K线闭合时完成分析）。
	bus.Subscribe(types.EventKlineClosed, func(evt types.KlineEvent) {
		elements := containProc.Process(evt.Kline)
		if len(elements) > 0 {
			lastElem := elements[len(elements)-1]
			fractalProc.Process(lastElem)
		}
	})

	// 6. 创建并启动币安WebSocket客户端。
	wsClient := binance.NewWSClient(
		[]string{"btcusdt", "ethusdt"},
		"1m",
		bus,
	)

	// 处理优雅关闭。
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info("正在关闭...")
		cancel()

		// 清理订阅。
		bus.Unsubscribe(types.EventKlineRealtime, chanlunSubID)
		bus.Unsubscribe(types.EventKlineClosed, dbSubID)

		wsClient.Stop()
	}()

	// 7. 阻塞并接收数据流。
	logger.Info("系统初始化完成，启动WebSocket流",
		"symbols", []string{"btcusdt", "ethusdt"},
		"interval", "1m",
	)

	if err := wsClient.Start(ctx); err != nil && err != context.Canceled {
		logger.Error("WebSocket客户端异常退出", "error", err)
		os.Exit(1)
	}

	logger.Info("系统已停止")
}
