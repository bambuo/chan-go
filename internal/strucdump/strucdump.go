// Package strucdump 提供缠论结构调试输出能力。
//
// 当 --debug-structure-dir 标志启用时，每次 Pipeline 处理完一根 K 线，
// 将当前完整结构快照以 JSON 格式写入指定目录，便于人工审查结构是否正确。
package strucdump

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"trade/internal/chanlun"
	"trade/internal/types"
)

// Snapshot 是可序列化的结构快照，包含缠论全部层级。
type Snapshot struct {
	Symbol        string             `json:"symbol"`
	Timestamp     int64              `json:"timestamp"`
	TotalKlines   int                `json:"totalKlines"`
	LastOpenTime  int64              `json:"lastOpenTime"`
	Elements      []ElementDump      `json:"elements"`
	Fractals      []FractalDump      `json:"fractals"`
	Strokes       []StrokeDump       `json:"strokes"`
	Segments      []SegmentDump      `json:"segments"`
	PivotZones    []PivotZoneDump    `json:"pivotZones"`
	TrendPatterns []TrendPatternDump `json:"trendPatterns"`
	Divergences   []DivergenceDump   `json:"divergences"`
}

// ElementDump 非包含 K 线元素的调试表示。
type ElementDump struct {
	Index       int     `json:"index"`
	High        float64 `json:"high"`
	Low         float64 `json:"low"`
	Direction   string  `json:"direction"`
	Contained   bool    `json:"contained"`
	MergedFrom  int     `json:"mergedFrom"`
	FractalType string  `json:"fractalType,omitempty"`
}

// FractalDump 分型调试表示。
type FractalDump struct {
	Index     int     `json:"index"`
	Type      string  `json:"type"`
	Price     float64 `json:"price"`
	OpenTime  int64   `json:"openTime"`
	Confirmed bool    `json:"confirmed"`
}

// StrokeDump 笔的调试表示。
type StrokeDump struct {
	Index      int     `json:"index"`
	Direction  string  `json:"direction"`
	StartPrice float64 `json:"startPrice"`
	EndPrice   float64 `json:"endPrice"`
	High       float64 `json:"high"`
	Low        float64 `json:"low"`
	Confirmed  bool    `json:"confirmed"`
	Virtual    bool    `json:"virtual"`
}

// SegmentDump 线段的调试表示。
type SegmentDump struct {
	Index      int     `json:"index"`
	Direction  string  `json:"direction"`
	StartPrice float64 `json:"startPrice"`
	EndPrice   float64 `json:"endPrice"`
	Completed  bool    `json:"completed"`
}

// PivotZoneDump 中枢的调试表示。
type PivotZoneDump struct {
	Index       int     `json:"index"`
	ZG          float64 `json:"zg"`
	ZD          float64 `json:"zd"`
	Direction   string  `json:"direction"`
	StrokeCount int     `json:"strokeCount"`
	Completed   bool    `json:"completed"`
}

// TrendPatternDump 走势类型的调试表示。
type TrendPatternDump struct {
	Index      int     `json:"index"`
	Type       string  `json:"type"` // "trend" / "consolidation"
	Direction  string  `json:"direction"`
	StartPrice float64 `json:"startPrice"`
	EndPrice   float64 `json:"endPrice"`
	High       float64 `json:"high"`
	Low        float64 `json:"low"`
	Completed  bool    `json:"completed"`
}

// DivergenceDump 背驰信号的调试表示。
type DivergenceDump struct {
	Type       string  `json:"type"`
	Stroke1Idx int     `json:"stroke1Idx"`
	Stroke2Idx int     `json:"stroke2Idx"`
	Price1     float64 `json:"price1"`
	Price2     float64 `json:"price2"`
	Ratio      float64 `json:"ratio"`
	Confirmed  bool    `json:"confirmed"`
}

// Writer 负责将结构快照写入调试目录。
type Writer struct {
	baseDir string
	mu      sync.Mutex
}

// NewWriter 创建结构调试输出器。
// baseDir 是调试输出的根目录（如 "data/debug"），
// 每个 symbol 会在其下创建子目录。
func NewWriter(baseDir string) *Writer {
	return &Writer{baseDir: baseDir}
}

// Write 将 PipelineOutput 以 JSON 快照写入文件。
// 路径: <baseDir>/<symbol>/<timestamp>.json
// 同时更新 <baseDir>/<symbol>/latest.json
func (w *Writer) Write(output *chanlun.PipelineOutput) error {
	snap := convert(output)
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("strucdump: 序列化失败: %w", err)
	}

	dir := filepath.Join(w.baseDir, output.Symbol)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("strucdump: 创建目录 %s: %w", dir, err)
	}

	// 写入带时间戳的快照文件
	filename := fmt.Sprintf("%d.json", output.LastOpenTime)
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("strucdump: 写入 %s: %w", path, err)
	}

	// 更新 latest.json 指针
	latestPath := filepath.Join(dir, "latest.json")
	if err := os.WriteFile(latestPath, data, 0644); err != nil {
		return fmt.Errorf("strucdump: 写入 %s: %w", latestPath, err)
	}

	return nil
}

// WriteAll 将多个 symbol 的输出批量写入。
func (w *Writer) WriteAll(outputs []*chanlun.PipelineOutput) error {
	for _, out := range outputs {
		if out != nil && out.HasChange {
			if err := w.Write(out); err != nil {
				return err
			}
		}
	}
	return nil
}

// BaseDir 返回调试输出根目录。
func (w *Writer) BaseDir() string { return w.baseDir }

// convert 将 PipelineOutput 转换为可序列化的 Snapshot。
func convert(out *chanlun.PipelineOutput) *Snapshot {
	now := time.Now().UnixMilli()
	snap := &Snapshot{
		Symbol:       out.Symbol,
		Timestamp:    now,
		TotalKlines:  out.TotalKlines,
		LastOpenTime: out.LastOpenTime,
	}

	// 非包含元素
	for i, e := range out.AllElements {
		dir := "none"
		if e.Direction == types.DirectionUp {
			dir = "up"
		} else if e.Direction == types.DirectionDown {
			dir = "down"
		}
		ft := ""
		switch e.FractalType {
		case types.FractalTop:
			ft = "top"
		case types.FractalBottom:
			ft = "bottom"
		}
		snap.Elements = append(snap.Elements, ElementDump{
			Index:       i,
			High:        e.High,
			Low:         e.Low,
			Direction:   dir,
			Contained:   e.Contained,
			MergedFrom:  e.MergedFrom,
			FractalType: ft,
		})
	}

	// 分型
	for i, f := range out.AllFractals {
		ft := "unknown"
		switch f.Type {
		case types.FractalTop:
			ft = "top"
		case types.FractalBottom:
			ft = "bottom"
		}
		price := f.High
		if f.Type == types.FractalBottom {
			price = f.Low
		}
		snap.Fractals = append(snap.Fractals, FractalDump{
			Index:     i,
			Type:      ft,
			Price:     price,
			OpenTime:  f.OpenTime,
			Confirmed: f.Confirmed,
		})
	}

	// 笔
	for _, s := range out.Strokes {
		snap.Strokes = append(snap.Strokes, StrokeDump{
			Index:      s.Index,
			Direction:  s.Direction.String(),
			StartPrice: s.StartPrice,
			EndPrice:   s.EndPrice,
			High:       s.High,
			Low:        s.Low,
			Confirmed:  s.Confirmed,
			Virtual:    s.Virtual,
		})
	}

	// 线段
	for i, seg := range out.Segments {
		snap.Segments = append(snap.Segments, SegmentDump{
			Index:      i,
			Direction:  seg.Direction().String(),
			StartPrice: seg.StartPrice(),
			EndPrice:   seg.EndPrice(),
			Completed:  seg.Completed(),
		})
	}

	// 中枢
	for _, pz := range out.PivotZones {
		snap.PivotZones = append(snap.PivotZones, PivotZoneDump{
			Index:       pz.Index(),
			ZG:          pz.ZG,
			ZD:          pz.ZD,
			Direction:   pz.Direction.String(),
			StrokeCount: pz.StrokeCount(),
			Completed:   pz.Completed,
		})
	}

	// 走势类型
	for _, tp := range out.TrendPatterns {
		snap.TrendPatterns = append(snap.TrendPatterns, TrendPatternDump{
			Index:      tp.Index,
			Type:       tp.Type,
			Direction:  tp.Direction.String(),
			StartPrice: tp.StartPrice,
			EndPrice:   tp.EndPrice,
			High:       tp.High,
			Low:        tp.Low,
			Completed:  tp.Completed,
		})
	}

	// 背驰
	for _, d := range out.Divergences {
		snap.Divergences = append(snap.Divergences, DivergenceDump{
			Type:       d.Type,
			Stroke1Idx: d.Stroke1Idx,
			Stroke2Idx: d.Stroke2Idx,
			Price1:     d.Price1,
			Price2:     d.Price2,
			Ratio:      d.Ratio,
			Confirmed:  d.Confirmed,
		})
	}

	return snap
}
