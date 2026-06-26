// Package app 负责系统组装与生命周期管理。
package app

// FractalDetector 是分型识别器，负责从非包含合并 K 线序列中识别顶底分型。
// 输入源必须为经过包含处理后的非包含 K 线序列。
type FractalDetector struct {
	ring *Ring[*ChanKLine] // 容量 3，滑动窗口
}

// NewFractalDetector 创建一个新的分型识别器。
func NewFractalDetector() *FractalDetector {
	return &FractalDetector{
		ring: NewRing[*ChanKLine](3), // 分型判定需要连续三根合并 K 线
	}
}

// Feed 输入一根新的非包含合并 K 线，触发分型检测。
func (fd *FractalDetector) Feed(chanLine *ChanKLine) {
	fd.ring.Append(chanLine)

	// 数量不足 3 根时无法形成分型
	if fd.ring.Len() < 3 {
		return
	}

	fd.detect()
}

// detect 检测最近三根合并 K 线是否形成分型，标记中间 K 线。
// 根据缠论原文，分型一经识别即立即确认。
func (fd *FractalDetector) detect() {
	left, _ := fd.ring.At(0)
	mid, _ := fd.ring.At(1)
	right, _ := fd.ring.At(2)

	// 顶分型：中间高 > 左右高 且 中间低 > 左右低
	if mid.High > left.High && mid.High > right.High &&
		mid.Low > left.Low && mid.Low > right.Low {
		mid.Fractal = FractalTop
		return
	}

	// 底分型：中间低 < 左右低 且 中间高 < 左右高
	if mid.Low < left.Low && mid.Low < right.Low &&
		mid.High < left.High && mid.High < right.High {
		mid.Fractal = FractalBottom
		return
	}
}
