package chanlun

// Ring 是一个泛型环形缓冲区，固定容量，新数据自动覆盖最旧数据。
type Ring[T any] struct {
	data  []T
	cap   int
	head  int
	count int
	total int64
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

// Append 追加一个元素，若已满则覆盖最旧数据，并返回被覆盖的元素。
func (r *Ring[T]) Append(v T) (evicted T, ok bool) {
	if r.count == r.cap {
		evicted = r.data[r.head]
		ok = true
	}
	r.data[r.head] = v
	r.head = (r.head + 1) % r.cap
	if r.count < r.cap {
		r.count++
	}
	r.total++
	return
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

// ToSlice 导出全部元素为切片（从旧到新）。
func (r *Ring[T]) ToSlice() []T {
	result := make([]T, 0, r.count)
	for i := 0; i < r.count; i++ {
		v, _ := r.At(i)
		result = append(result, v)
	}
	return result
}

// LastN 返回最近最多 n 个元素（从旧到新）。
func (r *Ring[T]) LastN(n int) []T {
	if n <= 0 {
		return nil
	}
	start := 0
	if r.count > n {
		start = r.count - n
	}
	result := make([]T, 0, r.count-start)
	for i := start; i < r.count; i++ {
		v, _ := r.At(i)
		result = append(result, v)
	}
	return result
}

// Clear 清空缓冲区。
func (r *Ring[T]) Clear() {
	var zero T
	for i := 0; i < r.count; i++ {
		r.data[(r.head+i)%r.cap] = zero
	}
	r.head = 0
	r.count = 0
}

// First 返回第一个元素（最旧），若为空返回零值和 false。
func (r *Ring[T]) First() (T, bool) {
	if r.count == 0 {
		var zero T
		return zero, false
	}
	return r.data[r.head], true
}
