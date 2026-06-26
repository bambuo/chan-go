package chanlun

// ──────────────────────────────────────────────
// 包含处理（流式）
// RingBuffer 固定容量 3，自动淘汰最旧
// ──────────────────────────────────────────────

// indexedKline 是带全局索引的合并元素。
type indexedKline struct {
	kline *ChanKLine
	index int64
}

// ContainProcessor 是 K 线包含处理器。
// 流式接口：每输入一根原始 K 线，输出新产生的合并 K 线（可能为 nil）。
// RingBuffer 容量 3，保留最后 3 根用于方向判定和分型检测。
type ContainProcessor struct {
	tail    *Ring[*indexedKline] // 容量 3
	counter int64
}

// NewContainProcessor 创建一个包含处理器。
func NewContainProcessor() *ContainProcessor {
	return &ContainProcessor{
		tail: NewRing[*indexedKline](3),
	}
}

// Process 处理一根原始 K 线，执行包含合并。
// 返回新产生的合并 K 线（nil 表示当前 K 线被合并入上一根）。
func (p *ContainProcessor) Process(kline *KLine) *ChanKLine {
	idx := p.counter
	p.counter++

	// 转为临时切片做包含合并
	list := p.tail.ToSlice()
	list = append(list, &indexedKline{
		kline: &ChanKLine{
			Time: kline.OpenTime,
			High: kline.High,
			Low:  kline.Low,
		},
		index: idx,
	})

	p.resolveContainment(list)

	// 写回 RingBuffer（保留最后 3 个）
	p.tail.Clear()
	start := 0
	if len(list) > 3 {
		start = len(list) - 3
	}
	for i := start; i < len(list); i++ {
		p.tail.Append(list[i])
	}

	if p.tail.Len() > 0 {
		if last, ok := p.tail.Last(); ok {
			return last.kline.Clone()
		}
	}
	return nil
}

// GetLastWithOffset 返回最后 N 个合并 K 线及其在全局序列中的起始偏移。
func (p *ContainProcessor) GetLastWithOffset(n int) ([]ChanKLine, int64) {
	list := p.tail.ToSlice()
	var result []ChanKLine
	start := 0
	if len(list) > n {
		start = len(list) - n
	}
	var offset int64
	if len(list) > 0 {
		offset = list[start].index
	}
	for i := start; i < len(list); i++ {
		result = append(result, *list[i].kline)
	}
	return result, offset
}

// GetLatest 返回最后一根合并 K 线。
func (p *ContainProcessor) GetLatest() *ChanKLine {
	if last, ok := p.tail.Last(); ok {
		return last.kline.Clone()
	}
	return nil
}

// Reset 清空处理器状态。
func (p *ContainProcessor) Reset() {
	p.tail.Clear()
	p.counter = 0
}

// ── 内部 ──

// resolveContainment 执行包含合并逻辑。
// 从尾部检测包含关系，如果存在则合并并递归检查。
func (p *ContainProcessor) resolveContainment(list []*indexedKline) {
	for len(list) >= 2 {
		a := list[len(list)-2].kline
		b := list[len(list)-1].kline
		if !a.Contains(b) && !b.Contains(a) {
			break
		}
		// 确定合并方向
		trend := p.determineTrend(list, len(list)-2)
		merged := &ChanKLine{
			Time:  a.Time,
			High:  a.High,
			Low:   a.Low,
			Index: a.Index,
		}
		if trend == DirectionUp {
			merged.MergeUp(b)
		} else {
			merged.MergeDown(b)
		}
		// 移除 a 和 b，加入合并结果
		list = list[:len(list)-2]
		list = append(list, &indexedKline{kline: merged, index: a.Index})
	}
}

// determineTrend 根据列表中指定位置之前的元素判断当前趋势方向。
func (p *ContainProcessor) determineTrend(list []*indexedKline, idx int) Direction {
	if idx > 0 && idx < len(list) {
		prev := list[idx-1].kline
		curr := list[idx].kline
		if curr.IsAbove(prev) {
			return DirectionUp
		}
		if curr.IsBelow(prev) {
			return DirectionDown
		}
	}
	return DirectionUp
}
