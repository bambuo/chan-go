// Package types 定义信号分析系统的共享数据类型。
//
// 本文件：级别体系 — PRD §6 级别体系
package types

// Level 表示缠论递归级别（PRD §6.1）。
type Level int

const (
	LevelL0 Level = 0 // L0 = 1分钟K线（数据源层，不参与递归）
	LevelL1 Level = 1 // L1 = 笔/线段/中枢/走势类型（基础层）
	LevelL2 Level = 2 // L2 = L1走势 → L2笔递归
	LevelL3 Level = 3 // L3 = L2走势 → L3笔递归
	LevelL4 Level = 4 // L4 = L3走势 → L4笔递归（最高层）
)

func (l Level) String() string {
	switch l {
	case LevelL0:
		return "L0"
	case LevelL1:
		return "L1"
	case LevelL2:
		return "L2"
	case LevelL3:
		return "L3"
	case LevelL4:
		return "L4"
	default:
		return "L?"
	}
}

// LevelTimeHint 每个级别动态统计的时间跨度参考（PRD §6.3）。
// 仅供机器人风控参考，不改变级别定义。
type LevelTimeHint struct {
	AvgDurationSec float64 `json:"avgDurationSec"` // 历史平均
	P10DurationSec float64 `json:"p10DurationSec"` // 10分位
	P90DurationSec float64 `json:"p90DurationSec"` // 90分位
	SampleCount    int     `json:"sampleCount"`    // 统计样本数
}

// StructureDepth 级别结构深度统计（PRD §6.3）。
type StructureDepth struct {
	AvgL2ZoushiPerL3Bi float64 `json:"avgL2ZoushiPerL3Bi"` // 平均由几个L2走势构成
}

// LevelRecastEvent 级别漂移事件（PRD §10.3）。
type LevelRecastEvent struct {
	Symbol      string       `json:"symbol"`
	OldLevel    Level        `json:"oldLevel"`
	NewLevel    Level        `json:"newLevel"`
	AffectedSignalIDs []string `json:"affectedSignalIds"`
	TS          int64        `json:"ts"`
}
