package chanlun

import "fmt"

// ──────────────────────────────────────────────
// Redis 输出记录结构体
// 所有记录自带时间戳，不依赖 ZSET score
// ──────────────────────────────────────────────

// ChanKlineRecord 是合并 K 线记录。
type ChanKlineRecord struct {
	Time int64   `json:"time"`
	High float64 `json:"high"`
	Low  float64 `json:"low"`
}

func (r *ChanKlineRecord) ToJSON() string {
	return fmt.Sprintf(`{"time":%d,"high":%f,"low":%f}`, r.Time, r.High, r.Low)
}

// FractalRecord 是分型记录。
type FractalRecord struct {
	Time  int64   `json:"time"`
	FType string  `json:"type"`
	High  float64 `json:"high"`
	Low   float64 `json:"low"`
	Index int64   `json:"index"`
}

func (r *FractalRecord) ToJSON() string {
	return fmt.Sprintf(`{"time":%d,"type":"%s","high":%f,"low":%f,"index":%d}`,
		r.Time, r.FType, r.High, r.Low, r.Index)
}

// StrokeRecord 是笔记录。
type StrokeRecord struct {
	Time       int64   `json:"time"`
	StartTime  int64   `json:"startTime"`
	EndTime    int64   `json:"endTime"`
	Direction  string  `json:"direction"`
	StartIndex int64   `json:"startIndex"`
	EndIndex   int64   `json:"endIndex"`
	StartPrice float64 `json:"startPrice"`
	EndPrice   float64 `json:"endPrice"`
	High       float64 `json:"high"`
	Low        float64 `json:"low"`
}

func (r *StrokeRecord) ToJSON() string {
	return fmt.Sprintf(`{"time":%d,"startTime":%d,"endTime":%d,"direction":"%s","startIndex":%d,"endIndex":%d,"startPrice":%f,"endPrice":%f,"high":%f,"low":%f}`,
		r.Time, r.StartTime, r.EndTime, r.Direction, r.StartIndex, r.EndIndex, r.StartPrice, r.EndPrice, r.High, r.Low)
}

// SegmentRecord 是线段记录。
type SegmentRecord struct {
	Time       int64   `json:"time"`
	StartTime  int64   `json:"startTime"`
	EndTime    int64   `json:"endTime"`
	StartPrice float64 `json:"startPrice"`
	EndPrice   float64 `json:"endPrice"`
	Direction  string  `json:"direction"`
	StartIndex int64   `json:"startIndex"`
	EndIndex   int64   `json:"endIndex"`
}

func (r *SegmentRecord) ToJSON() string {
	return fmt.Sprintf(`{"time":%d,"startTime":%d,"endTime":%d,"startPrice":%f,"endPrice":%f,"direction":"%s","startIndex":%d,"endIndex":%d}`,
		r.Time, r.StartTime, r.EndTime, r.StartPrice, r.EndPrice, r.Direction, r.StartIndex, r.EndIndex)
}

// PivotZoneRecord 是中枢记录。
type PivotZoneRecord struct {
	Time       int64   `json:"time"`
	StartTime  int64   `json:"startTime"`
	EndTime    int64   `json:"endTime"`
	StartPrice float64 `json:"startPrice"`
	EndPrice   float64 `json:"endPrice"`
	ZG         float64 `json:"zg"`
	ZD         float64 `json:"zd"`
	StartIndex int64   `json:"startIndex"`
	EndIndex   int64   `json:"endIndex"`
	Direction  string  `json:"direction"`
	Completed  bool    `json:"completed"`
}

func (r *PivotZoneRecord) ToJSON() string {
	return fmt.Sprintf(`{"time":%d,"startTime":%d,"endTime":%d,"startPrice":%f,"endPrice":%f,"zg":%f,"zd":%f,"startIndex":%d,"endIndex":%d,"direction":"%s","completed":%t}`,
		r.Time, r.StartTime, r.EndTime, r.StartPrice, r.EndPrice, r.ZG, r.ZD, r.StartIndex, r.EndIndex, r.Direction, r.Completed)
}

// TrendPatternRecord 是走势类型记录。
type TrendPatternRecord struct {
	Time       int64   `json:"time"`
	StartTime  int64   `json:"startTime"`
	EndTime    int64   `json:"endTime"`
	StartPrice float64 `json:"startPrice"`
	EndPrice   float64 `json:"endPrice"`
	Direction  string  `json:"direction"`
	Completed  bool    `json:"completed"`
	ZonesCount int     `json:"zonesCount"`
}

func (r *TrendPatternRecord) ToJSON() string {
	return fmt.Sprintf(`{"time":%d,"startTime":%d,"endTime":%d,"startPrice":%f,"endPrice":%f,"direction":"%s","completed":%t,"zonesCount":%d}`,
		r.Time, r.StartTime, r.EndTime, r.StartPrice, r.EndPrice, r.Direction, r.Completed, r.ZonesCount)
}

// DivergenceRecord 是背驰记录。
type DivergenceRecord struct {
	Time      int64   `json:"time"`
	Price     float64 `json:"price"`
	DType     string  `json:"type"`
	Ratio     float64 `json:"ratio"`
	Confirmed bool    `json:"confirmed"`
}

func (r *DivergenceRecord) ToJSON() string {
	return fmt.Sprintf(`{"time":%d,"price":%f,"type":"%s","ratio":%f,"confirmed":%t}`,
		r.Time, r.Price, r.DType, r.Ratio, r.Confirmed)
}

// SignalRecord 是买卖点信号记录。
type SignalRecord struct {
	Time     int64   `json:"time"`
	SType    string  `json:"type"`
	Price    float64 `json:"price"`
	Strength float64 `json:"strength"`
}

func (r *SignalRecord) ToJSON() string {
	return fmt.Sprintf(`{"time":%d,"type":"%s","price":%f,"strength":%f}`,
		r.Time, r.SType, r.Price, r.Strength)
}
