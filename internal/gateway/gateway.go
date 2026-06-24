// Package m8_gateway 输出网关（M8）。
//
// 职责（PRD §11）：
//   - 主输出通道 — Redis Stream XADD（实时信号、共振、状态变更）
//   - 辅助查询 — REST API（当前信号快照、结构树、级别）
//
// 架构要点：
//   - 输入用 XREADGROUP 消费 K 线 → 输出用 XADD 发布信号，对称设计
//   - REST 只做快照查询（不参与实时推送），实时推送走 Redis Stream
//   - 每次信号变更写入一条 JSON 到 "<OutputStreamPrefix>:<symbol>" stream
//
// M8 是引擎的唯一出口。
package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"trade/internal/config"
	"trade/internal/eventbus"
	"trade/internal/log"
	"trade/internal/signal"
	"trade/internal/structure"
	"trade/internal/types"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// Gateway M8 输出网关。
type Gateway struct {
	cfg    config.Config
	bus    *eventbus.GenericBus
	logger *slog.Logger

	signalEngine  *signal.SignalEngine
	structureTree *structure.Tree

	rdb    *redis.Client
	rdbCtx context.Context

	router *gin.Engine
	server *http.Server

	subIDs []int64 // 事件订阅 ID
}

// New 创建输出网关。
//
// 订阅总线事件并将信号/共振写入 Redis Stream（XADD）。
// 同时提供 REST 接口供外部查询当前状态快照。
func New(cfg config.Config, bus *eventbus.GenericBus, signalEngine *signal.SignalEngine, structureTree *structure.Tree) *Gateway {
	g := &Gateway{
		cfg:           cfg,
		bus:           bus,
		logger:        log.Component("m8.gateway"),
		signalEngine:  signalEngine,
		structureTree: structureTree,
		rdbCtx:        context.Background(),
	}

	// 初始化 Redis 客户端（与 M1 输入网关共享同一 Redis）
	g.rdb = redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	// 订阅事件 → 写入 Redis Stream
	g.subIDs = append(g.subIDs,
		bus.Subscribe(types.EventSignalCreated, g.onSignalCreated),
		bus.Subscribe(types.EventSignalStateChanged, g.onSignalStateChanged),
		bus.Subscribe(types.EventResonanceTriggered, g.onResonanceTriggered),
	)

	g.setupRouter()
	return g
}

// Start 启动 HTTP 服务。
func (g *Gateway) Start() error {
	addr := fmt.Sprintf("%s:%d", g.cfg.HTTPAddr, g.cfg.HTTPPort)
	g.server = &http.Server{
		Addr:         addr,
		Handler:      g.router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	g.logger.Info("启动输出网关",
		"addr", addr,
		"outputStreamPrefix", g.cfg.OutputStreamPrefix,
	)
	return g.server.ListenAndServe()
}

// Stop 优雅关闭。
func (g *Gateway) Stop(ctx context.Context) error {
	g.logger.Info("关闭输出网关")

	for _, id := range g.subIDs {
		g.bus.Unsubscribe(types.EventSignalCreated, id)
		g.bus.Unsubscribe(types.EventSignalStateChanged, id)
		g.bus.Unsubscribe(types.EventResonanceTriggered, id)
	}

	if g.rdb != nil {
		g.rdb.Close()
	}

	return g.server.Shutdown(ctx)
}

// ========================================================================
// 事件处理 → Redis Stream XADD
// ========================================================================

func (g *Gateway) streamKey(symbol string) string {
	return fmt.Sprintf("%s:%s", g.cfg.OutputStreamPrefix, symbol)
}

func (g *Gateway) xaddToStream(symbol string, eventType string, data interface{}) {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		g.logger.Error("序列化输出事件失败", "eventType", eventType, "error", err)
		return
	}

	if err := g.rdb.XAdd(g.rdbCtx, &redis.XAddArgs{
		Stream: g.streamKey(symbol),
		Values: map[string]interface{}{
			"type": eventType,
			"ts":   fmt.Sprintf("%d", time.Now().UnixMilli()),
			"data": string(jsonBytes),
		},
	}).Err(); err != nil {
		g.logger.Error("XADD 输出失败",
			"stream", g.streamKey(symbol),
			"eventType", eventType,
			"error", err,
		)
		return
	}

	g.logger.Debug("输出事件已写入 Redis Stream",
		"stream", g.streamKey(symbol),
		"eventType", eventType,
	)
}

func (g *Gateway) onSignalCreated(evt types.Event) {
	payload, ok := evt.Payload.(types.SignalEventPayload)
	if !ok || payload.Signal == nil {
		return
	}
	g.xaddToStream(evt.Symbol, "signal.created", payload.Signal)
}

func (g *Gateway) onSignalStateChanged(evt types.Event) {
	payload, ok := evt.Payload.(types.SignalEventPayload)
	if !ok || payload.Signal == nil {
		return
	}
	g.xaddToStream(evt.Symbol, "signal.stateChanged", payload.Signal)
}

func (g *Gateway) onResonanceTriggered(evt types.Event) {
	payload, ok := evt.Payload.(types.ResonanceEventPayload)
	if !ok || payload.Signal == nil {
		return
	}
	g.xaddToStream(evt.Symbol, "resonance.triggered", map[string]interface{}{
		"signal":    payload.Signal,
		"resonance": payload.Resonance,
	})
}

// ========================================================================
// REST API（快照查询）
// ========================================================================

func (g *Gateway) setupRouter() {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(g.requestLogger())

	r.GET("/v1/health", g.handleHealth)
	r.GET("/v1/signals/:symbol", g.handleSignals) // 历史信号查询（带过滤）
	r.GET("/v1/signals/:symbol/current", g.handleCurrentSignals)
	r.GET("/v1/signals/:symbol/:signalId", g.handleSignalDetail)
	r.GET("/v1/structure/:symbol", g.handleStructureCurrent)
	r.GET("/v1/structure/:symbol/history", g.handleStructureHistory) // 指定时刻结构
	r.GET("/v1/levels/:symbol", g.handleLevels)

	g.router = r
}

func (g *Gateway) requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		c.Next()
		g.logger.Debug("HTTP 请求",
			"method", c.Request.Method,
			"path", path,
			"status", c.Writer.Status(),
			"latency", time.Since(start),
		)
	}
}

func (g *Gateway) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"version": "1.0.0",
	})
}

func (g *Gateway) handleCurrentSignals(c *gin.Context) {
	symbol := c.Param("symbol")
	signals := g.signalEngine.GetActiveSignals(symbol)
	c.JSON(http.StatusOK, gin.H{
		"symbol":  symbol,
		"signals": signals,
	})
}

// handleSignals 历史信号查询（PRD §11.1）。
// GET /v1/signals/{symbol}?level=L1&state=confirmed&limit=20
func (g *Gateway) handleSignals(c *gin.Context) {
	symbol := c.Param("symbol")

	// 解析可选过滤参数
	var levelFilter types.Level
	if lvl := c.Query("level"); lvl != "" {
		switch lvl {
		case "L1":
			levelFilter = types.LevelL1
		case "L2":
			levelFilter = types.LevelL2
		case "L3":
			levelFilter = types.LevelL3
		case "L4":
			levelFilter = types.LevelL4
		}
	}

	stateFilter := types.SignalState(c.Query("state"))

	limit := 0
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	signals := g.signalEngine.QuerySignals(symbol, levelFilter, stateFilter, limit)
	c.JSON(http.StatusOK, gin.H{
		"symbol":  symbol,
		"signals": signals,
		"filter": map[string]interface{}{
			"level": levelFilter,
			"state": stateFilter,
			"limit": limit,
		},
	})
}

// handleStructureHistory 指定时刻的结构快照（PRD §11.1）。
// GET /v1/structure/{symbol}/history?ts=...
func (g *Gateway) handleStructureHistory(c *gin.Context) {
	symbol := c.Param("symbol")
	tsStr := c.Query("ts")
	if tsStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing ts query parameter"})
		return
	}

	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid ts"})
		return
	}

	// 查询 M3 版本历史 + 当前状态
	result := make(map[string]interface{})
	for _, level := range []types.Level{types.LevelL1, types.LevelL2, types.LevelL3, types.LevelL4} {
		state := g.structureTree.GetCurrentState(symbol, level)
		if state == nil {
			continue
		}
		versions := g.structureTree.GetVersionHistory(symbol, level)
		levelInfo := map[string]interface{}{
			"current": map[string]interface{}{
				"strokes":       len(state.Confirmed.Strokes),
				"pivotZones":    len(state.Confirmed.PivotZones),
				"trendPatterns": len(state.Confirmed.TrendPatterns),
			},
			"versionHistory": versions,
			"requestedTs":    ts,
		}
		result[level.String()] = levelInfo
	}

	if len(result) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "no structure data for symbol"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"symbol": symbol, "levels": result})
}

func (g *Gateway) handleSignalDetail(c *gin.Context) {
	signalID := c.Param("signalId")
	sig := g.signalEngine.GetSignal(signalID)
	if sig == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "signal not found"})
		return
	}
	c.JSON(http.StatusOK, sig)
}

func (g *Gateway) handleStructureCurrent(c *gin.Context) {
	symbol := c.Param("symbol")
	result := make(map[string]*types.DualTrackState)

	for _, level := range []types.Level{types.LevelL1, types.LevelL2, types.LevelL3, types.LevelL4} {
		state := g.structureTree.GetCurrentState(symbol, level)
		if state != nil {
			result[level.String()] = state
		}
	}

	if len(result) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "no structure data for symbol"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"symbol": symbol, "levels": result})
}

func (g *Gateway) handleLevels(c *gin.Context) {
	symbol := c.Param("symbol")
	result := make(map[string]interface{})

	for _, level := range []types.Level{types.LevelL1, types.LevelL2, types.LevelL3, types.LevelL4} {
		state := g.structureTree.GetCurrentState(symbol, level)
		if state == nil {
			continue
		}
		levelInfo := map[string]interface{}{
			"strokes":       len(state.Confirmed.Strokes),
			"pivotZones":    len(state.Confirmed.PivotZones),
			"trendPatterns": len(state.Confirmed.TrendPatterns),
			"inSync":        state.InSync,
		}

		result[level.String()] = levelInfo
	}

	if len(result) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "no level data for symbol"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"symbol": symbol, "levels": result})
}
