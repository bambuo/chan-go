// Package types — 本文件：事件类型定义（PRD §11.2）
package types

// EventType 系统事件类型分类。
type EventType string

// 事件类型常量（按模块来源分组）。
const (
	// === M1 输入网关事件 ===
	EventKlineReceived EventType = "kline.received" // 合法 K 线到达
	EventKlineRejected EventType = "kline.rejected" // K 线被拒绝（audit）
	EventKlineGap      EventType = "kline.gap"      // K 线缺口标记

	// === M3 结构树事件 ===
	EventStructureVersionChanged EventType = "structure.versionChanged"

	// === M4 级别事件 ===
	EventLevelRecast EventType = "level.recast" // 级别漂移

	// === M5 信号事件 ===
	EventSignalCreated     EventType = "signal.created"
	EventSignalStateChanged EventType = "signal.stateChanged"
	EventSignalInvalidated EventType = "signal.invalidated"
	EventSignalRecast      EventType = "signal.recast"

	// === M6 共振事件 ===
	EventResonanceTriggered EventType = "resonance.triggered"

	// === M0 快照事件 ===
	EventSnapshotTaken EventType = "snapshot.taken"

	// === M10 可观测事件 ===
	EventHealthDegraded EventType = "health.degraded"
	EventHealthRecovered EventType = "health.recovered"
)

// Event 通用的总线事件载体。
type Event struct {
	Type    EventType   `json:"type"`
	Payload interface{} `json:"payload"`
	Symbol  string      `json:"symbol,omitempty"`
	TS      int64       `json:"ts,omitempty"`
}

// EventHandler 是通用事件处理回调。
type EventHandler func(Event)

// 以下是各事件的负载类型定义。

// KlineReceivedPayload M1 输入网关事件负载。
type KlineReceivedPayload struct {
	Kline *Kline `json:"kline"`
}

// KlineRejectedPayload K 线被拒绝的审计信息（PRD §14.5.1）。
type KlineRejectedPayload struct {
	Kline  *Kline `json:"kline"`
	Reason string `json:"reason"`
}

// KlineGapPayload K 线缺口信息。
type KlineGapPayload struct {
	Symbol        string `json:"symbol"`
	LastTS        int64  `json:"lastTs"`
	CurrentTS     int64  `json:"currentTs"`
	GapDurationMs int64  `json:"gapDurationMs"`
}

// SignalEventPayload 信号事件的通用负载。
type SignalEventPayload struct {
	Signal      *Signal `json:"signal"`
	OldSignal   *Signal `json:"oldSignal,omitempty"`   // stateChanged 时的旧信号
	RecastFrom  *Signal `json:"recastFrom,omitempty"`  // recast 时的原信号
}

// ResonanceEventPayload 共振事件负载。
type ResonanceEventPayload struct {
	Signal      *Signal `json:"signal"`
	Resonance   Resonance `json:"resonance"`
}

// StructureVersionPayload 结构版本变更事件负载。
type StructureVersionPayload struct {
	Symbol       string           `json:"symbol"`
	OldVersion   *StructureVersion `json:"oldVersion,omitempty"`
	NewVersion   StructureVersion  `json:"newVersion"`
	Level        Level            `json:"level"`
}
