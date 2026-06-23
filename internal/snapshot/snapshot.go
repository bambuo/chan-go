// Package m0_snapshot 快照层（M0）。
//
// 职责（PRD §12.3）：
//   - 定期（5min）+ 结构变更事件触发，序列化进程状态为 JSON
//   - 含 Redis 消费 offset（重启可恢复）
//   - 保留策略：最近 24 个 + 每天 1 个长期归档
//
// 快照不存全部历史版本（历史版本由 M3 结构树单独管理）。
//
// 并发安全：引擎是单线程顺序消费 Redis Stream，快照时 M7 不会被写入
// （PRD §12.3 B1 串行化队列机制）。
package snapshot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"trade/internal/types"
)

// Snapshot 一次完整的状态快照。
type Snapshot struct {
	Symbol      string                   `json:"symbol"`
	Timestamp   int64                    `json:"timestamp"`
	Version     int                      `json:"version"` // schema 版本，用于兼容性校验
	RedisOffset string                   `json:"redisOffset"`
	DualTrack   *types.DualTrackState    `json:"dualTrack,omitempty"`
	Signals     []*types.Signal          `json:"signals,omitempty"`
	VersionMap  map[string]types.StructureVersion `json:"versionMap,omitempty"`
}

// Config M0 快照层配置。
type Config struct {
	SnapshotDir string // 快照存储目录
	RetainCount int    // 保留最近快照数
}

// Manager 管理快照的创建、恢复和清理。
type Manager struct {
	cfg       Config
	snapshots []string // 最近快照文件名列表（按时间升序）
	sync.RWMutex
}

// New 创建快照层实例。
func New(cfg Config) *Manager {
	return &Manager{
		cfg:       cfg,
		snapshots: make([]string, 0),
	}
}

// Take 执行一次快照（PRD §12.3 串行化：当前 K 线处理完毕后调用）。
func (m *Manager) Take(state *Snapshot) error {
	m.Lock()
	defer m.Unlock()

	state.Timestamp = time.Now().UnixMilli()
	state.Version = 1

	filename := fmt.Sprintf("%d.json", state.Timestamp)
	path := filepath.Join(m.dir(state.Symbol), filename)

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("快照序列化: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("创建快照目录: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("写入快照文件: %w", err)
	}

	// 更新 latest 指针。
	latestPath := filepath.Join(m.dir(state.Symbol), "latest.json")
	if err := os.WriteFile(latestPath, data, 0644); err != nil {
		return fmt.Errorf("写入 latest 指针: %w", err)
	}

	m.snapshots = append(m.snapshots, filename)
	m.prune(state.Symbol)

	return nil
}

// RestoreLatest 从最新快照恢复状态。
// 返回 (快照, 是否存在)。
func (m *Manager) RestoreLatest(symbol string) (*Snapshot, bool) {
	path := filepath.Join(m.dir(symbol), "latest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}

	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, false
	}
	return &snap, true
}

// RestorePrevious 从上一个有效快照恢复（PRD §14.5.3 降级链）。
func (m *Manager) RestorePrevious(symbol string) (*Snapshot, bool) {
	m.RLock()
	defer m.RUnlock()

	dir := m.dir(symbol)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, false
	}

	// 找最新的非 latest.json 文件。
	var files []string
	for _, e := range entries {
		if !e.IsDir() && e.Name() != "latest.json" && filepath.Ext(e.Name()) == ".json" {
			files = append(files, e.Name())
		}
	}
	if len(files) == 0 {
		return nil, false
	}
	sort.Strings(files)

	// 从最新到最旧依次尝试。
	for i := len(files) - 1; i >= 0; i-- {
		path := filepath.Join(dir, files[i])
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var snap Snapshot
		if err := json.Unmarshal(data, &snap); err != nil {
			continue
		}
		if snap.RedisOffset == "" {
			continue
		}
		return &snap, true
	}
	return nil, false
}

// ListSnapshots 返回指定 symbol 的可用快照列表（时间戳）。
func (m *Manager) ListSnapshots(symbol string) ([]int64, error) {
	dir := m.dir(symbol)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var timestamps []int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if e.Name() == "latest.json" {
			continue
		}
		name := e.Name()
		if len(name) < 5 || name[len(name)-5:] != ".json" {
			continue
		}
		var ts int64
		if _, err := fmt.Sscanf(name, "%d.json", &ts); err == nil {
			timestamps = append(timestamps, ts)
		}
	}
	sort.Slice(timestamps, func(i, j int) bool {
		return timestamps[i] < timestamps[j]
	})
	return timestamps, nil
}

// dir 返回指定 symbol 的快照目录。
func (m *Manager) dir(symbol string) string {
	return filepath.Join(m.cfg.SnapshotDir, symbol)
}

// prune 清理超出保留数量的快照。
func (m *Manager) prune(symbol string) {
	if len(m.snapshots) <= m.cfg.RetainCount {
		return
	}
	// 保留最新的 RetainCount 个。
	keep := m.snapshots[len(m.snapshots)-m.cfg.RetainCount:]
	for _, old := range m.snapshots[:len(m.snapshots)-m.cfg.RetainCount] {
		path := filepath.Join(m.dir(symbol), old)
		os.Remove(path)
	}
	m.snapshots = keep
}
