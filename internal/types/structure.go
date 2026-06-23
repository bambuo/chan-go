// Package types — 本文件：结构树与版本化元素（PRD §10/§10.5）
package types

// StructureVersion 版本化结构树的版本元数据（PRD §10.2）。
type StructureVersion struct {
	VersionID     string       `json:"versionId"`
	ValidFromTS   int64        `json:"validFromTs"`
	SupersededBy  *string      `json:"supersededBy,omitempty"`
	ParentVersion string       `json:"parentVersion,omitempty"` // 前驱版本
	Reason        string       `json:"reason,omitempty"`        // 版本变更原因
	Diff          *VersionDiff `json:"diff,omitempty"`          // 与上一版本的差异
}

// VersionDiff 版本差异记录（PRD §10.4.3 Step 6）。
type VersionDiff struct {
	RemovedElementIDs []string `json:"removedElementIds"`
	AddedElementIDs   []string `json:"addedElementIds"`
	AffectedWindowTS  []int64  `json:"affectedWindowTs"` // [最早变更点, 当前]
}

// ElementLineage 跨版本元素 lineage 映射（PRD §10.5.2）。
type ElementLineage struct {
	LineageID string   `json:"lineageId"`        // 稳定的 lineageId（跨版本不变）
	SameAs    []string `json:"sameAs,omitempty"` // 各版本内的实际元素ID
}

// 结构元素类型枚举
const (
	ElementTypeMergedKLine  = "mergedKLine"
	ElementTypeFractal      = "fractal"
	ElementTypeStroke       = "stroke"          // 笔
	ElementTypeFeatureSeq   = "featureSequence" // 特征序列
	ElementTypeSegment      = "segment"         // 线段
	ElementTypePivotZone    = "pivotZone"       // 中枢
	ElementTypeTrendPattern = "trendPattern"    // 走势类型
)

// StructureElement 结构树中不可变元素的基类。
type StructureElement struct {
	ID          string `json:"id"` // 版本内 ID（如 bi_P5@v4）
	ElementType string `json:"elementType"`
	LineageID   string `json:"lineageId"` // 跨版本稳定 ID
	VersionID   string `json:"versionId"` // 所属结构版本
	ValidFromTS int64  `json:"validFromTs"`
}

// Bi 笔（PRD §6.1 递归定义的核心构件）。
type Stroke struct {
	StructureElement
	StartFractalID string        `json:"startFractalId"`
	EndFractalID   string        `json:"endFractalId"`
	Direction      ChanDirection `json:"direction"`
	StartPrice     float64       `json:"startPrice"`
	EndPrice       float64       `json:"endPrice"`
	High           float64       `json:"high"`
	Low            float64       `json:"low"`
	Virtual        bool          `json:"virtual"` // 虚笔（尚未确认的尾部，PRD §10.4.1）
}

// PivotZone 中枢（PRD 中枢识别算法.md）。
type PivotZone struct {
	StructureElement
	Direction    ChanDirection `json:"direction"`
	ZG           float64       `json:"zg"`           // 中枢上沿（高）
	ZD           float64       `json:"zd"`           // 中枢下沿（低）
	StrokeIDs    []string      `json:"strokeIds"`    // 构成笔的 lineageId 列表
	SegmentCount int           `json:"segmentCount"` // 中枢已延伸段数
	Level        Level         `json:"level"`        // 所在级别
}

// TrendPattern 走势类型（PRD 走势类型分类算法.md）。
type TrendPattern struct {
	StructureElement
	Direction    ChanDirection `json:"direction"`
	PivotZoneIDs []string      `json:"pivotZoneIds"` // 包含的中枢 lineageId 列表
	Type         string        `json:"type"`         // "consolidation"(盘整) / "trend"(趋势)
	Level        Level         `json:"level"`
	Completed    bool          `json:"completed"`           // 是否已结束
	EndReason    string        `json:"endReason,omitempty"` // "divergence" / "reverseBreak"
}

// LevelStructure 单个级别的结构快照。
type LevelStructure struct {
	Level         Level            `json:"level"`
	Version       StructureVersion `json:"version"`
	Strokes       []Stroke         `json:"strokes"`
	PivotZones    []PivotZone      `json:"pivotZones"`
	TrendPatterns []TrendPattern   `json:"trendPatterns"`
	Provisional   bool             `json:"provisional"` // 实时轨=true, 确认轨=false
}

// DualTrackState 双轨状态（PRD §10.1）。
type DualTrackState struct {
	Provisional LevelStructure `json:"provisional"`          // 实时轨
	Confirmed   LevelStructure `json:"confirmed"`            // 确认轨
	InSync      bool           `json:"inSync"`               // 双轨是否收敛
	DriftSince  int64          `json:"driftSince,omitempty"` // 分歧开始时间
}
