// Package app 负责系统组装与生命周期管理。
package app

// Ring 是一个泛型环形缓冲区，固定容量，新数据自动覆盖最旧数据。
type Ring[T any] struct {
	data  []T   // 存储槽
	cap   int   // 容量（固定）
	head  int   // 下一次写入位置
	count int   // 当前元素数量
	total int64 // 已写入总数（用于计算全局索引）
}

// NewRing 创建一个指定容量的环形缓冲区。
func NewRing[T any](cap int) *Ring[T] {
	return &Ring[T]{
		data: make([]T, cap),
		cap:  cap,
	}
}

// Cap 返回缓冲区容量。
func (r *Ring[T]) Cap() int {
	return r.cap
}

// Len 返回当前元素数量。
func (r *Ring[T]) Len() int {
	return r.count
}

// Total 返回已写入的元素总数。
func (r *Ring[T]) Total() int64 {
	return r.total
}

// Append 追加一个元素，若已满则覆盖最旧数据。
func (r *Ring[T]) Append(v T) {
	r.data[r.head] = v
	r.head = (r.head + 1) % r.cap
	if r.count < r.cap {
		r.count++
	}
	r.total++
}

// Last 返回最后一个元素（最新写入），若为空返回零值和 false。
func (r *Ring[T]) Last() (T, bool) {
	if r.count == 0 {
		var zero T
		return zero, false
	}
	idx := (r.head - 1 + r.cap) % r.cap
	return r.data[idx], true
}

// At 按逻辑顺序获取元素，0 表示最旧，Len()-1 表示最新。
func (r *Ring[T]) At(i int) (T, bool) {
	if i < 0 || i >= r.count {
		var zero T
		return zero, false
	}
	// 最旧元素的位置
	oldest := (r.head - r.count + r.cap) % r.cap
	idx := (oldest + i) % r.cap
	return r.data[idx], true
}

// ForEach 按从旧到新顺序遍历所有元素。
func (r *Ring[T]) ForEach(fn func(i int, v T) bool) {
	for i := 0; i < r.count; i++ {
		v, _ := r.At(i)
		if !fn(i, v) {
			break
		}
	}
}
