package extension

import (
	"sync"
	"time"
)

// Debouncer 扩展执行的 trailing debounce 定时器
// REQ-F-018, 2.2.7: debounce:Ns — trailing debounce，每次触发重设计时器，
// 计时到期后执行最后一次触发的 action
// 注意：此 Debouncer 与 watch 包的 Debouncer 不同，
// watch 的是文件变更事件防抖，此处的是扩展执行防抖
// B-05-001 修复：引入 epoch 序号防止旧 timer 的 callback 错误清空新 timer
type Debouncer struct {
	interval time.Duration
	timer    *time.Timer
	pending  func()
	epoch    uint64 // 每次 Reset 自增，timer callback 仅在 epoch 匹配时执行+清空
	mu       sync.Mutex
}

// NewDebouncer 创建扩展执行防抖器
// REQ-F-018, 2.2.7: interval 为防抖时长（如 5s）
func NewDebouncer(interval time.Duration) *Debouncer {
	return &Debouncer{
		interval: interval,
	}
}

// Reset 重置防抖定时器，设置到期后执行的函数
// REQ-F-018, 2.2.7: 每次触发重设计时器，N秒内无新触发后执行最后一次触发的 action
// B-05-001 修复：epoch 模式 — 旧 timer 触发时 epoch 不匹配则跳过，不会 clobber 新 timer
func (d *Debouncer) Reset(fn func()) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.pending = fn
	d.epoch++

	if d.timer != nil {
		d.timer.Stop()
	}
	currentEpoch := d.epoch
	d.timer = time.AfterFunc(d.interval, func() {
		d.mu.Lock()
		// 仅当 epoch 仍匹配当前 Reset 时才执行并清空
		if d.epoch != currentEpoch {
			d.mu.Unlock()
			return
		}
		pending := d.pending
		d.pending = nil
		d.timer = nil
		d.mu.Unlock()

		if pending != nil {
			pending()
		}
	})
}

// Stop 停止防抖定时器
// REQ-F-018: 停止后不再执行待防抖的函数
func (d *Debouncer) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.epoch++ // 防止已排队的 timer 触发执行
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	d.pending = nil
}
