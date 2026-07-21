package watch

import (
	"sync"
	"time"
)

// REQ-F-026: 变更事件，表示一个文件/目录的变更
type ChangeEvent struct {
	Path      string    // 变更的文件/目录路径
	Operation string    // "create"/"write"/"rename"/"remove"
	Timestamp time.Time // 事件时间
}

// REQ-F-026: 防抖器，500ms 内的连续事件合并为一次重载，按文件路径去重
type Debouncer struct {
	interval time.Duration          // 防抖间隔，500ms（REQ-F-026锁定）
	events   chan ChangeEvent       // 输入事件通道
	output   chan []ChangeEvent     // 去重后的事件批次输出
	done     chan struct{}          // 停止信号
	pending  map[string]ChangeEvent // 按路径去重，同一文件只保留最后一次事件
	mu       sync.Mutex             // 保护 pending 的访问
	stopOnce sync.Once              // 确保 Stop 只执行一次
}

// NewDebouncer 创建防抖器
// REQ-F-026: interval 为防抖间隔，调用方传入 500ms（数值锁定）
func NewDebouncer(interval time.Duration) *Debouncer {
	return &Debouncer{
		interval: interval,
		events:   make(chan ChangeEvent, 64),
		output:   make(chan []ChangeEvent, 16),
		done:     make(chan struct{}),
		pending:  make(map[string]ChangeEvent),
	}
}

// Push 推入一个变更事件
// REQ-F-026: 事件进入防抖队列，同一文件路径后到的事件覆盖前面的
func (d *Debouncer) Push(event ChangeEvent) {
	d.events <- event
}

// Events 获取去重后的事件批次通道
// REQ-F-026: 每次 500ms 防抖窗口到期后输出一批去重事件
func (d *Debouncer) Events() <-chan []ChangeEvent {
	return d.output
}

// Start 启动防抖 goroutine
// REQ-F-026: 防抖逻辑——500ms 内连续事件合并，按路径去重
func (d *Debouncer) Start() {
	go d.run()
}

// Stop 停止防抖器
// REQ-F-026: 关闭后不再输出事件
func (d *Debouncer) Stop() {
	d.stopOnce.Do(func() {
		close(d.done)
	})
}

// run 防抖主循环
// REQ-F-026: 收到事件后启动500ms定时器，期间新事件重置定时器；
// 定时器到期时将 pending 中所有事件作为一批发送到 output；
// 同一文件路径只保留最后一次事件
func (d *Debouncer) run() {
	var timer *time.Timer
	var timerC <-chan time.Time

	for {
		select {
		case <-d.done:
			if timer != nil {
				timer.Stop()
			}
			// REQ-F-026: 停止时 flush 剩余事件
			d.flush()
			close(d.output)
			return

		case event, ok := <-d.events:
			if !ok {
				if timer != nil {
					timer.Stop()
				}
				d.flush()
				close(d.output)
				return
			}
			// REQ-F-026: 按路径去重，同一文件只保留最后一次事件
			d.mu.Lock()
			d.pending[event.Path] = event
			d.mu.Unlock()

			// REQ-F-026: 新事件到达时重置定时器
		if timer == nil {
			timer = time.NewTimer(d.interval)
			timerC = timer.C
		} else {
			// B-05-001 修复：按 Go 官方推荐模式 drain channel，避免已到期 timer 的
			// 旧 value 导致下次 select 立即触发提前 flush（防抖窗口失效）
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(d.interval)
		}

		case <-timerC:
			// REQ-F-026: 定时器到期，将 pending 中所有事件作为一批发送
			d.flush()
			timer.Stop()
			timer = nil
			timerC = nil
		}
	}
}

// flush 将 pending 中所有事件作为一批发送到 output
// REQ-F-026: 按文件路径去重后的批次输出
func (d *Debouncer) flush() {
	d.mu.Lock()
	if len(d.pending) == 0 {
		d.mu.Unlock()
		return
	}
	batch := make([]ChangeEvent, 0, len(d.pending))
	for _, event := range d.pending {
		batch = append(batch, event)
	}
	// 清空 pending
	d.pending = make(map[string]ChangeEvent)
	d.mu.Unlock()

	select {
	case d.output <- batch:
	case <-d.done:
	}
}
