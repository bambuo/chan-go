// Package types — 本文件：健康检查响应类型（PRD §11.1 /v1/health）
package types

// HealthStatus 引擎健康状态。
type HealthStatus string

const (
	HealthReady     HealthStatus = "ready"
	HealthDegraded  HealthStatus = "degraded"
	HealthRecovering HealthStatus = "recovering"
)

// HealthResponse /v1/health 的响应格式。
type HealthResponse struct {
	Status      HealthStatus            `json:"status"`
	Version     string                  `json:"version"`
	UptimeSec   int64                   `json:"uptimeSec"`
	SymbolStats map[string]SymbolHealth `json:"symbols,omitempty"`
}

// SymbolHealth 单个 symbol 的健康信息。
type SymbolHealth struct {
	KlineDelaySec  float64            `json:"klineDelaySec"`
	ActiveSignals  int                `json:"activeSignals"`
	StructureDepth map[Level]int      `json:"structureDepth"` // level → 元素数
	DualTrackSync  bool               `json:"dualTrackSync"`
	RedisOffset    string             `json:"redisOffset"`
}
