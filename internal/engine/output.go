package engine

import (
	"sync"
)

// entryWithType 是带类型标记的 Redis 条目。
type entryWithType struct {
	entityType string
	entry      *RedisEntry
}

// OutputPipe 是 FIFO → Redis 输出管道。
//
// Pipeline 每步产出即时 push（满则阻塞——自然背压），
// 后台写协程持续 drain 队列，写入 Redis。
// 每类实体独立 Redis key。
type OutputPipe struct {
	queue  chan *entryWithType // 缓冲 1024，满时阻塞
	store  *ResultStore
	symbol string
	depth  int
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewOutputPipe 创建一个输出管道，启动后台写协程。
func NewOutputPipe(symbol string, depth int, store *ResultStore) *OutputPipe {
	p := &OutputPipe{
		queue:  make(chan *entryWithType, 1024),
		store:  store,
		symbol: symbol,
		depth:  depth,
		stopCh: make(chan struct{}),
	}
	p.wg.Add(1)
	go p.writeLoop()
	return p
}

// Push 推入一个条目到输出队列。队列满时阻塞（产生背压到 Pipeline）。
func (p *OutputPipe) Push(entityType string, score float64, value string) {
	p.queue <- &entryWithType{entityType: entityType, entry: NewRedisEntry(score, value)}
}

// TryPush 非阻塞入队，队列满时返回 false。
func (p *OutputPipe) TryPush(entityType string, entry *RedisEntry) bool {
	select {
	case p.queue <- &entryWithType{entityType: entityType, entry: entry}:
		return true
	default:
		return false
	}
}

// Stop 停止后台写协程。
func (p *OutputPipe) Stop() {
	close(p.stopCh)
	p.wg.Wait()
}

// writeLoop 是后台写协程，持续从队列读取并写入 Redis。
func (p *OutputPipe) writeLoop() {
	defer p.wg.Done()
	for {
		select {
		case <-p.stopCh:
			// 关闭前尽量排空队列
			for len(p.queue) > 0 {
				item := <-p.queue
				p.store.SaveOne(p.symbol, p.depth, item.entityType, item.entry)
			}
			return
		case item := <-p.queue:
			p.store.SaveOne(p.symbol, p.depth, item.entityType, item.entry)
		}
	}
}
