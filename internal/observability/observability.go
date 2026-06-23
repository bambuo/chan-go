// Package m10_observability 可观测层（M10）。
//
// 职责（PRD §14）：
//   - metrics（延迟、信号质量、漂移、业务计数）
//   - audit log（K 线拒绝、信号生命周期）
//   - 告警（假信号率突增、recast 率突增、K 线断流）
//
// Metrics 使用包级单例 DefaultMetrics，模块可跨包直接调用：
//
//	observability.M.RecordSignalCreated(symbol, sigType, level)
package observability

import (
	"log/slog"
	"time"

	"trade/internal/log"
	"trade/internal/types"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// M 是包级 metrics 实例，供全系统调用。
var M = NewMetricsCollector()

// MetricsCollector metrics 采集器。
type MetricsCollector struct {
	logger *slog.Logger

	// 计算延迟
	m2ComputeDuration   *prometheus.HistogramVec
	m4ComputeDuration   *prometheus.HistogramVec
	// 信号质量
	signalCreated       *prometheus.CounterVec
	signalConfirmed     *prometheus.CounterVec
	signalInvalidated   *prometheus.CounterVec
	signalRecast        *prometheus.CounterVec
	falseSignalRate     *prometheus.GaugeVec
	// 漂移
	levelRecastCount    *prometheus.CounterVec
	dualTrackDivergence *prometheus.GaugeVec
	// 业务
	activeKlineDelay    *prometheus.GaugeVec
	klineRejected       *prometheus.CounterVec
	klineGap            *prometheus.CounterVec
}

// NewMetricsCollector 创建 metrics 采集器。
func NewMetricsCollector() *MetricsCollector {
	m := &MetricsCollector{
		logger: log.Component("m10.metrics"),
		m2ComputeDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "chan_m2_compute_duration_ms",
				Help:    "M2 缠论核心计算耗时（毫秒）",
				Buckets: []float64{1, 5, 10, 50, 100, 500, 1000},
			},
			[]string{"level", "scenario"},
		),
		m4ComputeDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "chan_m4_level_compute_duration_ms",
				Help:    "M4 级别递归计算耗时（毫秒）",
				Buckets: []float64{1, 5, 10, 50, 100, 500, 1000},
			},
			[]string{"level"},
		),
		signalCreated: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "chan_signals_created_total",
				Help: "信号创建总数",
			},
			[]string{"symbol", "type", "level"},
		),
		signalConfirmed: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "chan_signals_confirmed_total",
				Help: "信号确认总数",
			},
			[]string{"symbol", "type"},
		),
		signalInvalidated: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "chan_signals_invalidated_total",
				Help: "信号失效总数",
			},
			[]string{"symbol", "type"},
		),
		signalRecast: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "chan_signals_recast_total",
				Help: "信号 recast 总数",
			},
			[]string{"symbol"},
		),
		falseSignalRate: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "chan_false_signal_rate",
				Help: "假信号率（invalidated / candidate）",
			},
			[]string{"symbol"},
		),
		levelRecastCount: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "chan_level_recast_total",
				Help: "级别漂移事件总数",
			},
			[]string{"symbol", "level"},
		),
		dualTrackDivergence: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "chan_dual_track_divergence",
				Help: "双轨分歧（1=分歧中，0=收敛）",
			},
			[]string{"symbol"},
		),
		activeKlineDelay: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "chan_kline_delay_seconds",
				Help: "最新 K 线距当前时间的延迟（秒）",
			},
			[]string{"symbol"},
		),
		klineRejected: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "chan_kline_rejected_total",
				Help: "被拒绝的 K 线总数",
			},
			[]string{"symbol", "reason"},
		),
		klineGap: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "chan_kline_gap_total",
				Help: "K 线缺口总数",
			},
			[]string{"symbol"},
		),
	}
	return m
}

// RecordM2Duration 记录 M2 计算耗时。
func (m *MetricsCollector) RecordM2Duration(level types.Level, scenario string, duration time.Duration) {
	m.m2ComputeDuration.WithLabelValues(level.String(), scenario).Observe(float64(duration.Milliseconds()))
}

// RecordM4Duration 记录 M4 计算耗时。
func (m *MetricsCollector) RecordM4Duration(level types.Level, duration time.Duration) {
	m.m4ComputeDuration.WithLabelValues(level.String()).Observe(float64(duration.Milliseconds()))
}

// RecordSignalCreated 记录信号创建。
func (m *MetricsCollector) RecordSignalCreated(symbol string, signalType types.SignalType, level types.Level) {
	m.signalCreated.WithLabelValues(symbol, string(signalType), level.String()).Inc()
}

// RecordSignalConfirmed 记录信号确认。
func (m *MetricsCollector) RecordSignalConfirmed(symbol string, signalType types.SignalType) {
	m.signalConfirmed.WithLabelValues(symbol, string(signalType)).Inc()
}

// RecordSignalInvalidated 记录信号失效。
func (m *MetricsCollector) RecordSignalInvalidated(symbol string, signalType types.SignalType) {
	m.signalInvalidated.WithLabelValues(symbol, string(signalType)).Inc()
}

// RecordLevelRecast 记录级别漂移。
func (m *MetricsCollector) RecordLevelRecast(symbol string, level types.Level) {
	m.levelRecastCount.WithLabelValues(symbol, level.String()).Inc()
}

// RecordKlineDelay 记录 K 线延迟。
func (m *MetricsCollector) RecordKlineDelay(symbol string, delaySeconds float64) {
	m.activeKlineDelay.WithLabelValues(symbol).Set(delaySeconds)
}

// RecordKlineRejected 记录 K 线被拒绝。
func (m *MetricsCollector) RecordKlineRejected(symbol string, reason string) {
	m.klineRejected.WithLabelValues(symbol, reason).Inc()
}

// RecordKlineGap 记录 K 线缺口。
func (m *MetricsCollector) RecordKlineGap(symbol string) {
	m.klineGap.WithLabelValues(symbol).Inc()
}
