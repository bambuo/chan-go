// Package m8_gateway 输出网关（M8）。
//
// 职责（PRD §11）：
//   - REST 查询（当前信号快照、历史信号、结构树）
//   - WS 推送（实时信号、结构变更、级别 recast）
//   - 鉴权（API Key + HMAC）
//   - 限流
//   - 订阅管理
//
// M8 是引擎的唯一出口。
package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"trade/internal/config"
	"trade/internal/eventbus"
	"trade/internal/log"
	"trade/internal/signal"
	"trade/internal/structure"

	"github.com/gin-gonic/gin"
)

// Gateway M8 输出网关。
type Gateway struct {
	cfg    config.Config
	bus    *eventbus.GenericBus
	logger *slog.Logger

	signalEngine  *signal.SignalEngine
	structureTree *structure.Tree

	router *gin.Engine
	server *http.Server

	wsClients map[string][]*wsClient // symbol → WS 客户端列表
}

type wsClient struct {
	channels []string
	done     chan struct{}
	since    string // 断线重连时的游标
}

// New 创建输出网关。
func New(cfg config.Config, bus *eventbus.GenericBus, signalEngine *signal.SignalEngine, structureTree *structure.Tree) *Gateway {
	g := &Gateway{
		cfg:           cfg,
		bus:           bus,
		logger:        log.Component("m8.gateway"),
		signalEngine:  signalEngine,
		structureTree: structureTree,
		wsClients:     make(map[string][]*wsClient),
	}

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

	g.logger.Info("启动输出网关", "addr", addr)
	return g.server.ListenAndServe()
}

// Stop 优雅关闭。
func (g *Gateway) Stop(ctx context.Context) error {
	g.logger.Info("关闭输出网关")
	return g.server.Shutdown(ctx)
}

// setupRouter 配置 gin 路由（PRD §11.1）。
func (g *Gateway) setupRouter() {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(g.requestLogger())

	// 健康检查
	r.GET("/v1/health", g.handleHealth)

	// 信号 API
	signals := r.Group("/v1/signals")
	{
		signals.GET("/:symbol/current", g.handleCurrentSignals)
		signals.GET("/:symbol", g.handleSignalsHistory)
		signals.GET("/:symbol/:signalId", g.handleSignalDetail)
	}

	// 结构 API
	structure := r.Group("/v1/structure")
	{
		structure.GET("/:symbol", g.handleStructureCurrent)
		structure.GET("/:symbol/history", g.handleStructureHistory)
	}

	// 级别 API
	r.GET("/v1/levels/:symbol", g.handleLevels)

	g.router = r
}

// requestLogger 请求日志中间件。
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

// === REST 处理器（骨架，待填充）===

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

func (g *Gateway) handleSignalsHistory(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "not implemented yet"})
}

func (g *Gateway) handleSignalDetail(c *gin.Context) {
	signalID := c.Param("signalId")
	signal := g.signalEngine.GetSignal(signalID)
	if signal == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "signal not found"})
		return
	}
	c.JSON(http.StatusOK, signal)
}

func (g *Gateway) handleStructureCurrent(c *gin.Context) {
	symbol := c.Param("symbol")
	_ = symbol
	// TODO: 从 M3 获取当前结构
	c.JSON(http.StatusOK, gin.H{"message": "not implemented yet"})
}

func (g *Gateway) handleStructureHistory(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "not implemented yet"})
}

func (g *Gateway) handleLevels(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "not implemented yet"})
}
