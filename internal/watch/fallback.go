package watch

import "fmt"

// FallbackWatcher fsnotify失败时的降级方案
// REQ-E-007: fsnotify失败→拒绝启动/禁用热重载
// 不使用interface抽象层（B-05），FallbackWatcher是独立的降级状态对象
type FallbackWatcher struct {
	enabled bool
	reason  string
}

// NewFallbackWatcher 创建降级watcher
// REQ-E-007: fsnotify失败时创建降级watcher，热重载不可用
func NewFallbackWatcher(err error) *FallbackWatcher {
	return &FallbackWatcher{
		enabled: false,
		reason:  fmt.Sprintf("fsnotify unavailable: %v", err),
	}
}

// Enabled 返回热重载是否可用
func (f *FallbackWatcher) Enabled() bool {
	return f.enabled
}

// Reason 返回禁用原因
func (f *FallbackWatcher) Reason() string {
	return f.reason
}

// NewSafeWatcher 安全创建watcher，失败时返回降级方案
// REQ-E-007: fsnotify失败→拒绝启动/禁用热重载
// failFast: true=拒绝启动（返回错误），false=降级（返回*Watcher=nil, *FallbackWatcher非nil）
func NewSafeWatcher(baseDir string, failFast bool) (*Watcher, *FallbackWatcher, error) {
	w, err := NewWatcher(baseDir)
	if err != nil {
		if failFast {
			// REQ-E-007: 拒绝启动
			return nil, nil, fmt.Errorf("cannot initialize file watcher (hot-reload disabled): %w", err)
		}
		// 降级：返回FallbackWatcher，热重载不可用但不阻止启动
		return nil, NewFallbackWatcher(err), nil
	}
	return w, nil, nil
}
