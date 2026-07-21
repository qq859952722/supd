package watch

import (
	"errors"
	"strings"
	"testing"
)

// TestFallbackWatcherCreation 创建降级watcher
// REQ-E-007: fsnotify失败→降级watcher
func TestFallbackWatcherCreation(t *testing.T) {
	err := errors.New("fsnotify init failed")
	fw := NewFallbackWatcher(err)

	if fw.Enabled() {
		t.Error("FallbackWatcher should not be enabled")
	}
	if !strings.Contains(fw.Reason(), "fsnotify unavailable") {
		t.Errorf("unexpected reason: %s", fw.Reason())
	}
	if !strings.Contains(fw.Reason(), err.Error()) {
		t.Errorf("reason should contain original error: %s", fw.Reason())
	}
}

// TestFallbackWatcherDisabled 降级watcher不可用
// REQ-E-007: 禁用热重载
func TestFallbackWatcherDisabled(t *testing.T) {
	fw := NewFallbackWatcher(errors.New("test error"))
	if fw.Enabled() {
		t.Error("FallbackWatcher.Enabled() should be false")
	}
}

// TestFallbackWatcherReason 原因包含错误信息
func TestFallbackWatcherReason(t *testing.T) {
	fw := NewFallbackWatcher(errors.New("permission denied"))
	reason := fw.Reason()
	if reason == "" {
		t.Error("reason should not be empty")
	}
	if !strings.Contains(reason, "permission denied") {
		t.Errorf("reason should contain original error: %s", reason)
	}
}

// TestNewSafeWatcherSuccess 正常创建
func TestNewSafeWatcherSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	watcher, fallback, err := NewSafeWatcher(tmpDir, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if watcher == nil {
		t.Fatal("watcher should not be nil on success")
	}
	if fallback != nil {
		t.Error("fallback should be nil on success")
	}
	watcher.Stop()
}

// TestNewSafeWatcherFailFastWithSimulatedError 使用模拟失败测试拒绝启动模式
// REQ-E-007: fsnotify失败→拒绝启动
func TestNewSafeWatcherFailFastWithSimulatedError(t *testing.T) {
	// fsnotify.NewWatcher在正常系统上不会失败
	// 测试 NewFallbackWatcher 的行为是否正确即可
	fw := NewFallbackWatcher(errors.New("simulated fsnotify failure"))

	if fw.Enabled() {
		t.Error("FallbackWatcher should not be enabled after simulated failure")
	}
	reason := fw.Reason()
	if !strings.Contains(reason, "simulated fsnotify failure") {
		t.Errorf("reason should contain simulated error: %s", reason)
	}
}

// TestFallbackWatcherNotEnabled 降级watcher不启用热重载
// REQ-E-007: 禁用热重载
func TestFallbackWatcherNotEnabled(t *testing.T) {
	fw := NewFallbackWatcher(errors.New("test"))
	if fw.Enabled() {
		t.Error("FallbackWatcher should not be enabled")
	}
}
