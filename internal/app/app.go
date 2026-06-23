// Package app 封装系统核心组件与应用生命周期。
package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	gosignal "os/signal"
	"syscall"
	"time"

	"trade/internal/chanlun"
	"trade/internal/config"
	"trade/internal/eventbus"
	"trade/internal/gateway"
	"trade/internal/ingest"
	"trade/internal/levels"
	"trade/internal/log"
	"trade/internal/resonance"
	signals "trade/internal/signal"
	"trade/internal/snapshot"
	"trade/internal/state"
	"trade/internal/strucdump"
	"trade/internal/structure"
	"trade/internal/types"
)

// App 是信号分析系统的主控对象，封装所有组件与生命周期。
type App struct {
	cfg    config.Config
	logger *slog.Logger

	// === 基础设施 ===
	bus      *eventbus.Bus        // 旧事件总线（数据适配层路径）
	gBus     *eventbus.GenericBus // 通用事件总线（M1~M10 通信）
	appState *state.Store         // M7 状态存储
	snapMgr  *snapshot.Manager    // M0 快照层

	// === M2 缠论核心 ===
	pipeline *chanlun.Pipeline // symbol 级处理管道
	m3Bridge *chanlun.M3Bridge // M2 → M3 桥接器

	// === 模块 ===
	ingestG   *ingest.IngestGateway      // M1 输入网关
	tree      *structure.Tree            // M3 结构树
	lvlBldr   *levels.LevelBuilder       // M4 递归级别
	sigEngine *signals.SignalEngine      // M5 信号引擎
	resEngine *resonance.ResonanceEngine // M6 共振引擎
	gatewayS  *gateway.Gateway           // M8 输出网关

	// 生命周期控制
	cancel    context.CancelFunc
	startTime time.Time
}

// New 根据配置构建 App，依次初始化所有子系统。
func New(cfg config.Config) (*App, error) {
	log.Init(log.Config{
		Level:     cfg.LogLevel,
		JSON:      cfg.LogJSON,
		AddSource: true,
		Output:    os.Stdout,
	})
	logger := log.Component("main")
	logger.Info("启动信号分析系统", "config", fmt.Sprintf("%+v", cfg))

	// 确保快照目录存在
	if err := os.MkdirAll(cfg.SnapshotDir, 0755); err != nil {
		return nil, fmt.Errorf("创建快照目录 %s: %w", cfg.SnapshotDir, err)
	}

	// === 事件总线 ===
	bus := eventbus.New()
	gBus := eventbus.NewGeneric()

	// === M7 状态存储 ===
	appState := state.New()

	// === M0 快照层 ===
	snapMgr := snapshot.New(snapshot.Config{
		SnapshotDir: cfg.SnapshotDir,
		RetainCount: cfg.SnapshotRetain,
	})

	// === M10 Metrics（包级单例 observability.M） ===

	// === M2 缠论核心 Pipeline ===
	pipeline := chanlun.NewPipeline()

	// === M3 结构树 ===
	tree := structure.New(gBus)

	// === M2 → M3 桥接器 ===
	m3Bridge := chanlun.NewM3Bridge(pipeline, tree)

	// 可选：挂载结构调试输出器
	if cfg.DebugStructureDir != "" {
		debugWriter := strucdump.NewWriter(cfg.DebugStructureDir)
		m3Bridge.WithDebugWriter(debugWriter)
		logger.Info("缠论结构调试输出已启用", "dir", cfg.DebugStructureDir)
	}

	// === M4 递归级别 ===
	lvlBldr := levels.New(gBus, tree)

	// === M5 信号引擎 ===
	sigEngine := signals.New(gBus)

	// === M6 共振引擎 ===
	resEngine := resonance.New(gBus, tree)

	// === M1 输入网关 ===
	ingestG := ingest.New(ingest.Config{
		RedisAddr:     cfg.RedisAddr,
		RedisPassword: cfg.RedisPassword,
		RedisDB:       cfg.RedisDB,
		StreamPrefix:  cfg.StreamPrefix,
		ConsumerGroup: cfg.ConsumerGroup,
		Symbols:       cfg.Symbols,
	}, gBus)

	// === M8 输出网关 ===
	gatewayS := gateway.New(cfg, gBus, sigEngine, tree)

	// === 订阅 M1 事件 → M2 Pipeline → M3 结构树 ===
	gBus.Subscribe(types.EventKlineReceived, func(evt types.Event) {
		payload, ok := evt.Payload.(types.KlineReceivedPayload)
		if !ok || payload.Kline == nil {
			return
		}
		committed, versionID, err := m3Bridge.OnKline(payload.Kline)
		if err != nil {
			logger.Error("M3 桥接处理失败",
				"symbol", evt.Symbol,
				"error", err,
			)
			return
		}
		if committed {
			logger.Debug("M3 版本已提交",
				"symbol", evt.Symbol,
				"versionId", versionID,
			)
		}
	})

	return &App{
		cfg:      cfg,
		logger:   logger,
		bus:      bus,
		gBus:     gBus,
		appState: appState,
		snapMgr:  snapMgr,
		// metrics: 包级单例 observability.M
		pipeline:  pipeline,
		m3Bridge:  m3Bridge,
		ingestG:   ingestG,
		tree:      tree,
		lvlBldr:   lvlBldr,
		sigEngine: sigEngine,
		resEngine: resEngine,
		gatewayS:  gatewayS,
		startTime: time.Now(),
	}, nil
}

// Run 启动系统主循环。
func (a *App) Run(ctx context.Context) error {
	ctx, a.cancel = context.WithCancel(ctx)
	defer a.cancel()

	// 监听系统信号
	sigChan := make(chan os.Signal, 1)
	gosignal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 尝试从快照恢复
	a.tryRestoreFromSnapshot()

	// 启动 M1 输入网关（Redis Stream 消费）
	if err := a.ingestG.Start(ctx); err != nil {
		return fmt.Errorf("启动输入网关: %w", err)
	}

	// 启动 M8 输出网关（HTTP 服务，异步）
	go func() {
		if err := a.gatewayS.Start(); err != nil {
			a.logger.Error("输出网关异常退出", "error", err)
		}
	}()

	// 定期快照（PRD §12.3）
	go a.snapshotLoop(ctx)

	a.logger.Info("系统初始化完成",
		"symbols", a.cfg.Symbols,
		"http", fmt.Sprintf("%s:%d", a.cfg.HTTPAddr, a.cfg.HTTPPort),
	)

	// 等待退出信号
	select {
	case <-sigChan:
		a.logger.Info("收到信号，正在关闭...")
	case <-ctx.Done():
	}

	a.Shutdown()
	return nil
}

// Shutdown 优雅关闭所有子系统。
func (a *App) Shutdown() {
	a.logger.Info("正在关闭子系统...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.gatewayS.Stop(shutdownCtx); err != nil {
		a.logger.Error("关闭输出网关失败", "error", err)
	}

	a.ingestG.Stop()
	a.resEngine.Stop()

	if a.cancel != nil {
		a.cancel()
	}
	a.logger.Info("系统已关闭")
}

// tryRestoreFromSnapshot 尝试从快照恢复（PRD §14.5.3）。
func (a *App) tryRestoreFromSnapshot() {
	for _, symbol := range a.cfg.Symbols {
		snap, ok := a.snapMgr.RestoreLatest(symbol)
		if !ok {
			a.logger.Info("无可用快照，从零开始", "symbol", symbol)
			continue
		}

		if snap.RedisOffset != "" {
			a.appState.SetRedisOffset(symbol, snap.RedisOffset)
			a.ingestG.RestoreOffset(symbol, snap.RedisOffset)
		}
		if snap.DualTrack != nil {
			a.appState.SetDualTrack(symbol, snap.DualTrack)
		}
		for _, sig := range snap.Signals {
			a.appState.AddSignal(sig)
		}

		a.logger.Info("从快照恢复", "symbol", symbol, "offset", snap.RedisOffset)
	}
}

// snapshotLoop 定期执行快照。
func (a *App) snapshotLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(a.cfg.SnapshotPeriod) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			a.takeSnapshot()
		case <-ctx.Done():
			return
		}
	}
}

// takeSnapshot 执行所有 symbol 的快照。
func (a *App) takeSnapshot() {
	for _, symbol := range a.cfg.Symbols {
		offset, _ := a.appState.GetRedisOffset(symbol)
		dualTrack := a.appState.GetDualTrack(symbol)
		signalList := a.appState.GetSignals(symbol)

		snap := &snapshot.Snapshot{
			Symbol:      symbol,
			RedisOffset: offset,
			Signals:     signalList,
			DualTrack:   dualTrack,
		}

		if err := a.snapMgr.Take(snap); err != nil {
			a.logger.Error("快照失败", "symbol", symbol, "error", err)
		}
	}
}

// Bus 暴露旧事件总线（测试用）。
func (a *App) Bus() *eventbus.Bus {
	return a.bus
}

// GBus 暴露通用事件总线（测试用）。
func (a *App) GBus() *eventbus.GenericBus {
	return a.gBus
}
