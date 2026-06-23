// Package chanlun — M2 → M3 桥接层。
//
// M3Bridge 将 PipelineOutput（缠论处理结果）转换为 M3 结构树版本，
// 实现 K 线流入 → 结构版本化的完整链路。
package chanlun

import (
	"fmt"
	"log/slog"
	"time"

	"trade/internal/log"
	"trade/internal/observability"
	"trade/internal/structure"
	"trade/internal/types"
)

// M3 Bridge 日志组件名。
const bridgeComponent = "chanlun.bridge"

// DebugWriter 是结构调试输出的抽象接口。
type DebugWriter interface {
	Write(output *PipelineOutput) error
}

// M3Bridge 连接 chanlun Pipeline 与 M3 结构树。
type M3Bridge struct {
	pipeline    *Pipeline
	tree        *structure.Tree
	logger      *slog.Logger
	signalSink  SignalSink  // 可选：信号引擎的接收器
	debugWriter DebugWriter // 可选：结构调试输出器
}

// SignalSink 信号引擎接收器接口，由 signal.SignalEngine 实现。
type SignalSink interface {
	OnSignalInput(input *SignalInput)
}

// NewM3Bridge 创建桥接器。
func NewM3Bridge(pipeline *Pipeline, tree *structure.Tree) *M3Bridge {
	return &M3Bridge{
		pipeline: pipeline,
		tree:     tree,
		logger:   log.Component(bridgeComponent),
	}
}

// WithSignalSink 设置信号引擎接收器。
func (b *M3Bridge) WithSignalSink(sink SignalSink) *M3Bridge {
	b.signalSink = sink
	return b
}

// WithDebugWriter 设置结构调试输出器。
// 启用后每次 OnKline 处理完毕会向 debugWriter 写入结构快照。
func (b *M3Bridge) WithDebugWriter(w DebugWriter) *M3Bridge {
	b.debugWriter = w
	return b
}

// OnKline 处理一根 K 线：管道处理 → M3 提交。
// 返回 (是否产生了新版本, 版本ID, 错误)。
func (b *M3Bridge) OnKline(kline *types.Kline) (bool, string, error) {
	start := time.Now()

	// 1. 管道处理
	output := b.pipeline.Process(kline)

	// 1b. 调试输出：将当前管道状态写入文件（始终写入，便于观察无变更时的状态）
	if b.debugWriter != nil {
		if err := b.debugWriter.Write(output); err != nil {
			b.logger.Warn("调试结构输出失败", "symbol", output.Symbol, "error", err)
		}
	}

	// 2. 无变更则跳过
	if !output.HasChange || len(output.AllFractals) == 0 {
		return false, "", nil
	}

	// 3. 构建 L1 级别结构
	state := b.buildLevelStructure(output)

	// 4. 计算 diff
	prev := b.tree.GetLatestStructure(output.Symbol, types.LevelL1)
	diff := buildVersionDiff(prev, state, output)

	// 5. 提交新版本
	versionID := b.tree.Commit(output.Symbol, types.LevelL1, state, diff)

	// 6. 注册本次新增元素的 lineage
	b.registerElements(output)

	observability.M.RecordM2Duration(types.LevelL1, "pipeline", time.Since(start))

	b.logger.Debug("M3 版本提交",
		"symbol", output.Symbol,
		"versionId", versionID,
		"totalKlines", output.TotalKlines,
		"elements", len(output.AllElements),
		"fractals", len(output.AllFractals),
	)

	// 7. 触发信号识别
	if b.signalSink != nil {
		b.signalSink.OnSignalInput(output.ToSignalInput())
	}

	return true, versionID, nil
}

// buildLevelStructure 从 PipelineOutput 构建 L1 LevelStructure。
func (b *M3Bridge) buildLevelStructure(output *PipelineOutput) *types.DualTrackState {
	now := time.Now().UnixMilli()

	// 从 StrokeProcessor 获取确认笔，转换为 M3 的 types.Stroke
	var bis []types.Stroke
	for i, algoStroke := range output.Strokes {
		elementID := fmt.Sprintf("%s_bi_%d@latest", output.Symbol, i)
		lineageID := fmt.Sprintf("L_%s_bi_%d", output.Symbol, i)

		var direction types.ChanDirection
		if algoStroke.Start.FractalType == types.FractalBottom {
			direction = types.DirectionUp
		} else {
			direction = types.DirectionDown
		}

		bis = append(bis, types.Stroke{
			StructureElement: types.StructureElement{
				ID:          elementID,
				ElementType: types.ElementTypeStroke,
				LineageID:   lineageID,
				ValidFromTS: algoStroke.Start.OpenTime,
			},
			StartFractalID: fmt.Sprintf("%s_f_%d", output.Symbol, i),
			Direction:      direction,
			StartPrice:     algoStroke.StartPrice,
			EndPrice:       algoStroke.EndPrice,
			High:           algoStroke.High,
			Low:            algoStroke.Low,
		})
	}

	levelStruct := types.LevelStructure{
		Level:         types.LevelL1,
		Strokes:       bis,
		PivotZones:    PivotZonesToTypes(output.PivotZones),
		TrendPatterns: TrendPatternsToTypes(output.TrendPatterns),
		Provisional:   true,
	}

	// 双轨：当前仅实时轨，确认轨与实时轨一致
	return &types.DualTrackState{
		Provisional: levelStruct,
		Confirmed:   levelStruct,
		InSync:      true,
		DriftSince:  now,
	}
}

// registerElements 注册新元素的 lineage。
func (b *M3Bridge) registerElements(output *PipelineOutput) {
	ts := time.Now().UnixMilli()

	for i := range output.AllElements {
		elementID := fmt.Sprintf("%s_ck_%d", output.Symbol, i)
		lineageID := fmt.Sprintf("L_%s_ck_%d", output.Symbol, i)
		b.tree.RegisterElement(elementID, lineageID, types.ElementTypeMergedKLine, ts)
	}

	for i := range output.AllFractals {
		elementID := fmt.Sprintf("%s_f_%d", output.Symbol, i)
		lineageID := fmt.Sprintf("L_%s_f_%d", output.Symbol, i)
		b.tree.RegisterElement(elementID, lineageID, types.ElementTypeFractal, ts)
	}

	for i := range output.Strokes {
		elementID := fmt.Sprintf("%s_bi_%d", output.Symbol, i)
		lineageID := fmt.Sprintf("L_%s_bi_%d", output.Symbol, i)
		b.tree.RegisterElement(elementID, lineageID, types.ElementTypeStroke, ts)
	}

	for i := range output.PivotZones {
		elementID := fmt.Sprintf("%s_zs_%d", output.Symbol, i)
		lineageID := fmt.Sprintf("L_%s_zs_%d", output.Symbol, i)
		b.tree.RegisterElement(elementID, lineageID, types.ElementTypePivotZone, ts)
	}
}

// buildVersionDiff 计算新版本与上一版本的差异。
func buildVersionDiff(prev *types.LevelStructure, state *types.DualTrackState, output *PipelineOutput) *types.VersionDiff {
	if prev == nil {
		diff := &types.VersionDiff{
			RemovedElementIDs: nil,
			AddedElementIDs:   []string{},
			AffectedWindowTS:  []int64{},
		}
		for i := 0; i < len(output.AllFractals); i++ {
			diff.AddedElementIDs = append(diff.AddedElementIDs,
				fmt.Sprintf("%s_f_%d", output.Symbol, i))
		}
		for i := 0; i < len(output.Strokes); i++ {
			diff.AddedElementIDs = append(diff.AddedElementIDs,
				fmt.Sprintf("%s_bi_%d", output.Symbol, i))
		}
		for i := 0; i < len(output.PivotZones); i++ {
			diff.AddedElementIDs = append(diff.AddedElementIDs,
				fmt.Sprintf("%s_zs_%d", output.Symbol, i))
		}
		return diff
	}

	// 当前仅尾部延伸（场景 A），无回溯修正
	diff := &types.VersionDiff{
		RemovedElementIDs: nil,
		AddedElementIDs:   []string{},
		AffectedWindowTS:  []int64{output.LastOpenTime},
	}

	// 新增的分型
	prevFractalCount := len(prev.Strokes)
	for i := prevFractalCount; i < len(output.AllFractals); i++ {
		diff.AddedElementIDs = append(diff.AddedElementIDs,
			fmt.Sprintf("%s_f_%d", output.Symbol, i))
	}

	// 新增的笔
	// prev.Strokes 在 bridge 创建时用的是 output.Strokes 转换的，此处直接比较长度
	// 但 prev.Strokes 是 types.Stroke 类型（M3），output.Strokes 是 chanlun.stroke 类型（算法内部）
	// 长度比较即可
	prevStrokeCount := 0
	if prev.Strokes != nil {
		prevStrokeCount = len(prev.Strokes)
	}
	for i := prevStrokeCount; i < len(output.Strokes); i++ {
		diff.AddedElementIDs = append(diff.AddedElementIDs,
			fmt.Sprintf("%s_bi_%d", output.Symbol, i))
	}

	// 新增的中枢
	prevPivotZoneCount := 0
	if prev.PivotZones != nil {
		prevPivotZoneCount = len(prev.PivotZones)
	}
	for i := prevPivotZoneCount; i < len(output.PivotZones); i++ {
		diff.AddedElementIDs = append(diff.AddedElementIDs,
			fmt.Sprintf("%s_zs_%d", output.Symbol, i))
	}

	return diff
}
