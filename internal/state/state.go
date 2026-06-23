// Package m7_state 状态存储（M7）。
//
// 职责（PRD §5/§12.3）：
//   - 进程内状态：结构树当前版本、信号历史、双轨状态、消费 offset
//   - 不含业务逻辑
//   - 不做持久化（持久化归 M0）
//   - 被所有模块读写
package state

import (
	"sync"

	"trade/internal/types"
)

// Store M7 进程内状态存储。
type Store struct {
	mu sync.RWMutex

	// 各 symbol 的 Redis 消费 offset
	redisOffsets map[string]string // symbol → offset

	// 各 symbol 的结构版本状态
	structureVersions map[string]map[types.Level]string // symbol → level → versionId

	// 各 symbol 的信号
	signals map[string][]*types.Signal // symbol → []Signal

	// 各 symbol 的双轨状态
	dualTrack map[string]*types.DualTrackState // symbol → DualTrackState
}

// New 创建状态存储。
func New() *Store {
	return &Store{
		redisOffsets:      make(map[string]string),
		structureVersions: make(map[string]map[types.Level]string),
		signals:           make(map[string][]*types.Signal),
		dualTrack:         make(map[string]*types.DualTrackState),
	}
}

// SetRedisOffset 设置指定 symbol 的 Redis 消费 offset。
func (s *Store) SetRedisOffset(symbol, offset string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.redisOffsets[symbol] = offset
}

// GetRedisOffset 获取指定 symbol 的 Redis 消费 offset。
func (s *Store) GetRedisOffset(symbol string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	offset, ok := s.redisOffsets[symbol]
	return offset, ok
}

// AllRedisOffsets 返回所有 symbol 的 Redis offset 快照。
func (s *Store) AllRedisOffsets() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.redisOffsets))
	for k, v := range s.redisOffsets {
		out[k] = v
	}
	return out
}

// SetDualTrack 设置指定 symbol 的双轨状态。
func (s *Store) SetDualTrack(symbol string, state *types.DualTrackState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dualTrack[symbol] = state
}

// GetDualTrack 获取指定 symbol 的双轨状态。
func (s *Store) GetDualTrack(symbol string) *types.DualTrackState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dualTrack[symbol]
}

// AddSignal 添加信号到存储。
func (s *Store) AddSignal(signal *types.Signal) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.signals[signal.Symbol] = append(s.signals[signal.Symbol], signal)
}

// GetSignals 获取指定 symbol 的所有信号。
func (s *Store) GetSignals(symbol string) []*types.Signal {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*types.Signal, len(s.signals[symbol]))
	copy(out, s.signals[symbol])
	return out
}
