// Package types — 本文件：信号对象定义（PRD §8）
package types

// SignalType 买卖点类型（PRD §8.2）。
type SignalType string

const (
	SignalBuy1  SignalType = "BUY_1"
	SignalBuy2  SignalType = "BUY_2"
	SignalBuy3  SignalType = "BUY_3"
	SignalSell1 SignalType = "SELL_1"
	SignalSell2 SignalType = "SELL_2"
	SignalSell3 SignalType = "SELL_3"
)

// SignalState 信号状态机状态（PRD §8.3）。
type SignalState string

const (
	SignalCandidate   SignalState = "candidate"
	SignalConfirmed   SignalState = "confirmed"
	SignalInvalidated SignalState = "invalidated"
	SignalSuperseded  SignalState = "superseded"
)

// ResonanceKind 共振类型（PRD §9.1）。
type ResonanceKind string

const (
	ResonanceIntervalNesting ResonanceKind = "intervalNesting" // G-2 区间套
	ResonanceCrossLevel      ResonanceKind = "crossLevel"      // G-1 跨层共振
	ResonanceStandalone      ResonanceKind = "standalone"      // 无共振
	ResonanceDirectionOnly   ResonanceKind = "directionOnly"   // A3 方向过滤
)

// Signal 完整信号对象（PRD §8.1）。
type Signal struct {
	SignalID         string         `json:"signalId"`
	Symbol           string         `json:"symbol"`
	TS               int64          `json:"ts"`
	Type             SignalType     `json:"type"`
	Level            Level          `json:"level"`
	LevelTimeHint    *LevelTimeHint `json:"levelTimeHint,omitempty"`
	Price            float64        `json:"price"`
	State            SignalState    `json:"state"`
	Provisional      bool           `json:"provisional"`
	Confidence       float64        `json:"confidence"`
	Strength         float64        `json:"strength"`
	RecastRisk       float64        `json:"recastRisk"`
	Anchor           SignalAnchor   `json:"anchor"`
	Targets          SignalTargets  `json:"targets"`
	Resonance        Resonance      `json:"resonance"`
	StructureVersion string         `json:"structureVersion"`
	Evidence         Evidence       `json:"evidence"`
	Supersedes       []string       `json:"supersedes,omitempty"`
	RecastFrom       *string        `json:"recastFrom,omitempty"`
}

// Clone 返回信号的深拷贝。
func (s *Signal) Clone() *Signal {
	out := *s
	if s.LevelTimeHint != nil {
		h := *s.LevelTimeHint
		out.LevelTimeHint = &h
	}
	if s.Supersedes != nil {
		out.Supersedes = make([]string, len(s.Supersedes))
		copy(out.Supersedes, s.Supersedes)
	}
	if s.RecastFrom != nil {
		v := *s.RecastFrom
		out.RecastFrom = &v
	}
	out.Resonance = *s.Resonance.clone()
	out.Evidence = *s.Evidence.clone()
	return &out
}

// SignalAnchor 信号的结构锚点 — 用于身份判定与去重（PRD §8.6）。
type SignalAnchor struct {
	Kind                  string `json:"kind"`                            // divergenceSegment | dependentBuyPoint | pivotZoneBreakout
	DivergenceLineage     string `json:"divergenceLineage,omitempty"`     // 背驰段 lineageId
	CurrentStrokeLineage  string `json:"currentStrokeLineage,omitempty"`  // 当前笔 lineageId
	PreviousStrokeLineage string `json:"previousStrokeLineage,omitempty"` // 前一笔 lineageId
	DependOnSignalID      string `json:"dependOnSignalId,omitempty"`      // 依赖的一买/一卖信号ID（二买/二卖）
	DependOnPivotZoneID   string `json:"dependOnPivotZoneId,omitempty"`   // 离开的中枢 lineageId（三买/三卖）
	CurrentStrokeID       string `json:"currentStrokeId,omitempty"`       // 当前回调笔 lineageId（二买/二卖/三买/三卖）
}

// SignalTargets 目标位与失效位（PRD §8.7）。
type SignalTargets struct {
	TargetPrice        *float64 `json:"targetPrice,omitempty"`  // 结构性参考位
	TargetSource       string   `json:"targetSource,omitempty"` // target 的结构来源
	InvalidationPrice  float64  `json:"invalidationPrice"`      // 失效价位（必有）
	InvalidationSource string   `json:"invalidationSource"`     // 失效位的结构来源
}

// Resonance 共振信息（PRD §9）。
type Resonance struct {
	Kind            ResonanceKind          `json:"kind"`
	Participants    []ResonanceParticipant `json:"participants,omitempty"`
	DirectionFilter *DirectionFilter       `json:"directionFilter,omitempty"`
}

func (r *Resonance) clone() *Resonance {
	out := *r
	if r.Participants != nil {
		out.Participants = make([]ResonanceParticipant, len(r.Participants))
		copy(out.Participants, r.Participants)
	}
	if r.DirectionFilter != nil {
		f := *r.DirectionFilter
		out.DirectionFilter = &f
	}
	return &out
}

// ResonanceParticipant 共振参与者。
type ResonanceParticipant struct {
	Level    Level      `json:"level"`
	Type     SignalType `json:"type"`
	SignalID string     `json:"signalId"`
}

// DirectionFilter A3 方向过滤结果。
type DirectionFilter struct {
	AlignedLevels []Level `json:"alignedLevels"`
	Aligned       bool    `json:"aligned"`
	Boost         float64 `json:"boost"`
}

// Evidence 信号证据（PRD §8.1 evidence 字段）。
type Evidence struct {
	TrendDirection       string              `json:"trendDirection"`
	PivotZoneCount       int                 `json:"pivotZoneCount"`
	LastPivotZoneID      string              `json:"lastPivotZoneId"`
	LastPivotZoneBelow   bool                `json:"lastPivotZoneBelow"`
	Divergence           *DivergenceEvidence `json:"divergence,omitempty"`
	StrengthFactors      StrengthFactors     `json:"strengthFactors"`
	IntervalNestingChain []NestingLink       `json:"intervalNestingChain,omitempty"`
	HasGap               bool                `json:"hasGap,omitempty"`
}

func (e *Evidence) clone() *Evidence {
	out := *e
	if e.Divergence != nil {
		d := *e.Divergence
		out.Divergence = &d
	}
	if e.IntervalNestingChain != nil {
		out.IntervalNestingChain = make([]NestingLink, len(e.IntervalNestingChain))
		copy(out.IntervalNestingChain, e.IntervalNestingChain)
	}
	return &out
}

// DivergenceEvidence 背驰证据（PRD §8.1）。
type DivergenceEvidence struct {
	Method       string  `json:"method"`
	CurrentArea  float64 `json:"currentArea"`
	PreviousArea float64 `json:"previousArea"`
	Ratio        float64 `json:"ratio"`
	Threshold    float64 `json:"threshold"`
}

// StrengthFactors 强度因子（PRD §8.5）。
type StrengthFactors struct {
	DivergenceRatioTrend     string  `json:"divergenceRatioTrend"`
	ConsecutiveConfirmations int     `json:"consecutiveConfirmations"`
	PullbackDepthRatio       float64 `json:"pullbackDepthRatio"`
	SublevelSignalEvolving   bool    `json:"sublevelSignalEvolving"`
}

// NestingLink 区间套链中的一环。
type NestingLink struct {
	Level               Level `json:"level"`
	InDivergenceSegment bool  `json:"inDivergenceSegment"`
}
