// Package types — 本文件：特征序列与线段类型定义（特征序列算法.md / 线段划分算法.md）
package types

// FeatureKLine 特征序列K线，由笔构建（特征序列算法.md）。
type FeatureKLine struct {
	High      float64       // 高点
	Low       float64       // 低点
	Stroke    interface{}   // 来源笔（chanlun.stroke 指针，运行时由具体类型转换）
	Direction ChanDirection // 方向
	Contained bool          // 是否被前一根包含（包含处理用）
	Index     int           // 在特征序列中的索引
}

// FeatureSeqType 特征序列类型（特征序列算法.md）。
type FeatureSeqType int

const (
	FeatureSeqPrimary   FeatureSeqType = 1 // 第一特征序列：包含处理方向与线段方向相反
	FeatureSeqSecondary FeatureSeqType = 2 // 第二种情况的第二特征序列：包含处理方向与线段方向一致
)

// FeatureSeqFractal 特征序列分型检测结果。
type FeatureSeqFractal struct {
	HasTop    bool // 是否出现顶分型
	TopIndex  int  // 顶分型中间元素索引
	HasBottom bool // 是否出现底分型
	BottomIdx int  // 底分型中间元素索引
}

// Segment 线段。
type Segment struct {
	Index       int           // 线段序号
	Direction   ChanDirection // 线段方向
	Strokes     []interface{} // 构成笔列表（chanlun.stroke 指针）
	StartStroke interface{}   // 第一笔
	EndStroke   interface{}   // 最后一笔
	StartPrice  float64       // 起点价格
	EndPrice    float64       // 终点价格
	High        float64       // 区间最高
	Low         float64       // 区间最低
	Confirmed   bool          // 是否已完成
}

// SegmentEvent 线段变更事件。
type SegmentEvent struct {
	Symbol     string
	SegmentIdx int
	Direction  ChanDirection
	EventType  string // "created" / "extended" / "completed"
}
