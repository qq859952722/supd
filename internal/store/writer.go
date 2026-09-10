package store

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"time"
)

// writerQueueCapacity 有界队列容量（硬编码 1024）。
const writerQueueCapacity = 1024

var errStoreClosed = errors.New("store writer closed")

// command 写命令。run 在 writer 单 goroutine 内顺序执行（每命令一个事务）；
// result 非空表示权威写命令需等待结果；bumpSeq 表示成功后是否推进全局序列。
type command struct {
	run     func(db *sql.DB) error
	result  chan error
	bumpSeq bool
}

// writer 单 writer + 有界队列。
// 单 goroutine 顺序处理每条命令（每命令一个事务）；TryEnqueue 非阻塞；
// Submit 等待结果；Close 排空队列（带 deadline）。
type writer struct {
	db       *sql.DB
	owner    *Store
	queue    chan *command // 有界，容量 1024（硬编码）
	done     chan struct{} // Close 时关闭，标记停止接收新命令
	stopDone chan struct{} // writer goroutine 排空完成

	closed  atomic.Bool
	dropped atomic.Int64
}

func newWriter(db *sql.DB, owner *Store) *writer {
	w := &writer{
		db:       db,
		owner:    owner,
		queue:    make(chan *command, writerQueueCapacity),
		done:     make(chan struct{}),
		stopDone: make(chan struct{}),
	}
	go w.run()
	return w
}

func (w *writer) run() {
	defer close(w.stopDone)
	for {
		select {
		case c := <-w.queue:
			w.execute(c)
		case <-w.done:
			// 排空剩余命令后退出。
			for {
				select {
				case c := <-w.queue:
					w.execute(c)
				default:
					return
				}
			}
		}
	}
}

func (w *writer) execute(c *command) {
	err := c.run(w.db)
	if c.result != nil {
		c.result <- err
	}
	if err != nil {
		w.owner.recordFailure(err)
		return
	}
	w.owner.recordSuccess()
	if c.bumpSeq {
		w.owner.seq.Add(1)
		w.owner.signalChange()
	}
}

// tryEnqueue 非阻塞入队。满时返回 false（丢弃 + 计数），不阻塞调用方。
// Close 竞态说明：发送成功后复查 closed——若 writer 已进入关停排空，命令可能
// 滞留队列不被执行，按 false 上报避免"假成功"。残余窗口（复查通过后 writer
// 恰好退出）仅存在于 Close 与 TryEnqueue 精确重叠的微秒级瞬间，关机时最多
// 影响 1 条通知，家用场景可接受。
func (w *writer) tryEnqueue(c *command) bool {
	if w.closed.Load() {
		return false
	}
	select {
	case w.queue <- c:
	default:
		w.dropped.Add(1)
		return false
	}
	if w.closed.Load() {
		return false
	}
	return true
}

// submit 将权威写命令送入队列并等待执行结果。
// 队列满时阻塞等待（不静默丢弃）；context 取消/队列关闭/事务失败均返回错误。
func (w *writer) submit(ctx context.Context, c *command) error {
	if w.closed.Load() {
		return errStoreClosed
	}
	select {
	case w.queue <- c:
	case <-ctx.Done():
		return ctx.Err()
	case <-w.done:
		return errStoreClosed
	}
	select {
	case err := <-c.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-w.done:
		return errStoreClosed
	}
}

// close 停止接收新命令并排空队列（带 deadline，超时记录剩余丢弃数）。
// 幂等。
func (w *writer) close(timeout time.Duration) {
	if !w.closed.CompareAndSwap(false, true) {
		return
	}
	close(w.done)
	select {
	case <-w.stopDone:
	case <-time.After(timeout):
		// 超时：记录仍未处理的剩余命令。
		w.dropped.Add(int64(len(w.queue)))
	}
}

// droppedCount 返回被丢弃（队列满或超时未处理）的命令数。
func (w *writer) droppedCount() int64 { return w.dropped.Load() }
