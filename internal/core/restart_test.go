package core

import (
	"syscall"
	"testing"
	"time"
)

func newTestEngine(policy RestartPolicy) *RestartEngine {
	return NewRestartEngine(policy, 1000, 30000, 2, 0, 300)
}

func TestShouldRestart_Always(t *testing.T) {
	e := newTestEngine(RestartAlways)

	tests := []struct {
		name     string
		exitCode int
		signaled bool
		sig      syscall.Signal
		want     bool
	}{
		{"exit 0", 0, false, 0, true},
		{"exit 1", 1, false, 0, true},
		{"exit 137", 137, false, 0, true},
		{"killed by SIGKILL", 0, true, syscall.SIGKILL, true},
		{"killed by SIGTERM", 0, true, syscall.SIGTERM, true},
		{"killed by SIGINT", 0, true, syscall.SIGINT, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := e.ShouldRestart(tt.exitCode, tt.signaled, tt.sig); got != tt.want {
				t.Errorf("ShouldRestart() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldRestart_OnFailure(t *testing.T) {
	e := newTestEngine(RestartOnFailure)

	tests := []struct {
		name     string
		exitCode int
		signaled bool
		sig      syscall.Signal
		want     bool
	}{
		{"exit 0 not signaled", 0, false, 0, false},
		{"exit 1", 1, false, 0, true},
		{"exit 137", 137, false, 0, true},
		{"killed by SIGKILL (not SIGTERM/SIGINT)", 0, true, syscall.SIGKILL, true},
		{"killed by SIGUSR1", 0, true, syscall.SIGUSR1, true},
		{"killed by SIGTERM (manual stop)", 0, true, syscall.SIGTERM, false},
		{"killed by SIGINT (manual stop)", 0, true, syscall.SIGINT, false},
		{"not signaled exit 0", 0, false, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := e.ShouldRestart(tt.exitCode, tt.signaled, tt.sig); got != tt.want {
				t.Errorf("ShouldRestart() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldRestart_Never(t *testing.T) {
	e := newTestEngine(RestartNever)

	tests := []struct {
		name     string
		exitCode int
		signaled bool
		sig      syscall.Signal
		want     bool
	}{
		{"exit 0", 0, false, 0, false},
		{"exit 1", 1, false, 0, false},
		{"killed by SIGKILL", 0, true, syscall.SIGKILL, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := e.ShouldRestart(tt.exitCode, tt.signaled, tt.sig); got != tt.want {
				t.Errorf("ShouldRestart() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBackoffDuration(t *testing.T) {
	// backoff_ms=1000, multiplier=2, max_backoff_ms=30000
	// retries=1: 1000 * 2^0 = 1000ms
	// retries=2: 1000 * 2^1 = 2000ms
	// retries=3: 1000 * 2^2 = 4000ms
	// retries=4: 1000 * 2^3 = 8000ms
	// retries=5: min(1000*2^4, 30000) = 16000ms
	// retries=6: min(1000*2^5, 30000) = 30000ms (capped)

	e := NewRestartEngine(RestartAlways, 1000, 30000, 2, 0, 300)

	// retries=0 → 0
	if d := e.BackoffDuration(); d != 0 {
		t.Errorf("retries=0: BackoffDuration() = %v, want 0", d)
	}

	e.IncrementRetries() // retries=1
	if d := e.BackoffDuration(); d != 1000*time.Millisecond {
		t.Errorf("retries=1: BackoffDuration() = %v, want 1000ms", d)
	}

	e.IncrementRetries() // retries=2
	if d := e.BackoffDuration(); d != 2000*time.Millisecond {
		t.Errorf("retries=2: BackoffDuration() = %v, want 2000ms", d)
	}

	e.IncrementRetries() // retries=3
	if d := e.BackoffDuration(); d != 4000*time.Millisecond {
		t.Errorf("retries=3: BackoffDuration() = %v, want 4000ms", d)
	}

	e.IncrementRetries() // retries=4
	if d := e.BackoffDuration(); d != 8000*time.Millisecond {
		t.Errorf("retries=4: BackoffDuration() = %v, want 8000ms", d)
	}

	e.IncrementRetries() // retries=5
	if d := e.BackoffDuration(); d != 16000*time.Millisecond {
		t.Errorf("retries=5: BackoffDuration() = %v, want 16000ms", d)
	}

	e.IncrementRetries() // retries=6 → capped at maxBackoffMs
	if d := e.BackoffDuration(); d != 30000*time.Millisecond {
		t.Errorf("retries=6: BackoffDuration() = %v, want 30000ms (capped)", d)
	}
}

func TestBackoffDuration_DifferentMultiplier(t *testing.T) {
	// backoff_ms=500, multiplier=3, max_backoff_ms=60000
	// retries=1: 500 * 3^0 = 500ms
	// retries=2: 500 * 3^1 = 1500ms
	// retries=3: 500 * 3^2 = 4500ms

	e := NewRestartEngine(RestartAlways, 500, 60000, 3, 0, 300)

	e.IncrementRetries() // retries=1
	if d := e.BackoffDuration(); d != 500*time.Millisecond {
		t.Errorf("retries=1: BackoffDuration() = %v, want 500ms", d)
	}

	e.IncrementRetries() // retries=2
	if d := e.BackoffDuration(); d != 1500*time.Millisecond {
		t.Errorf("retries=2: BackoffDuration() = %v, want 1500ms", d)
	}

	e.IncrementRetries() // retries=3
	if d := e.BackoffDuration(); d != 4500*time.Millisecond {
		t.Errorf("retries=3: BackoffDuration() = %v, want 4500ms", d)
	}
}

func TestMaxRetries_ZeroMeansUnlimited(t *testing.T) {
	e := NewRestartEngine(RestartAlways, 1000, 30000, 2, 0, 300)

	for i := 0; i < 100; i++ {
		e.IncrementRetries()
	}
	if e.MaxRetriesReached() {
		t.Errorf("max_retries=0 should mean unlimited, but MaxRetriesReached()=true after 100 retries")
	}
}

func TestMaxRetries_LimitReached(t *testing.T) {
	e := NewRestartEngine(RestartAlways, 1000, 30000, 2, 3, 300)

	if e.MaxRetriesReached() {
		t.Errorf("MaxRetriesReached()=true before any retries")
	}

	e.IncrementRetries() // retries=1
	if e.MaxRetriesReached() {
		t.Errorf("MaxRetriesReached()=true after 1 retry, max=3")
	}

	e.IncrementRetries() // retries=2
	if e.MaxRetriesReached() {
		t.Errorf("MaxRetriesReached()=true after 2 retries, max=3")
	}

	e.IncrementRetries() // retries=3
	if !e.MaxRetriesReached() {
		t.Errorf("MaxRetriesReached()=false after 3 retries, max=3")
	}
}

func TestResetIfNeeded(t *testing.T) {
	e := NewRestartEngine(RestartAlways, 1000, 30000, 2, 0, 2) // reset_after_seconds=2

	e.IncrementRetries()
	e.IncrementRetries()
	e.IncrementRetries()
	if e.Retries() != 3 {
		t.Fatalf("Retries() = %d, want 3", e.Retries())
	}

	// RecordStart sets lastStartTime to now
	e.RecordStart()

	// Not yet elapsed → no reset
	e.ResetIfNeeded()
	if e.Retries() != 3 {
		t.Errorf("ResetIfNeeded() reset retries too early, got %d, want 3", e.Retries())
	}

	// Wait for reset_after_seconds to elapse
	time.Sleep(2100 * time.Millisecond)

	e.ResetIfNeeded()
	if e.Retries() != 0 {
		t.Errorf("ResetIfNeeded() did not reset after reset_after_seconds, got %d, want 0", e.Retries())
	}
}

func TestResetIfNeeded_ZeroResetAfter(t *testing.T) {
	e := NewRestartEngine(RestartAlways, 1000, 30000, 2, 0, 0) // reset_after_seconds=0

	e.IncrementRetries()
	e.RecordStart()
	time.Sleep(100 * time.Millisecond)

	e.ResetIfNeeded()
	if e.Retries() != 1 {
		t.Errorf("reset_after_seconds=0 should not reset, got %d, want 1", e.Retries())
	}
}

func TestResetIfNeeded_NoStartRecorded(t *testing.T) {
	e := NewRestartEngine(RestartAlways, 1000, 30000, 2, 0, 300)

	e.IncrementRetries()
	e.ResetIfNeeded()
	if e.Retries() != 1 {
		t.Errorf("ResetIfNeeded() should not reset when no start recorded, got %d, want 1", e.Retries())
	}
}

func TestRetries(t *testing.T) {
	e := newTestEngine(RestartAlways)

	if e.Retries() != 0 {
		t.Errorf("initial Retries() = %d, want 0", e.Retries())
	}

	e.IncrementRetries()
	if e.Retries() != 1 {
		t.Errorf("after 1 increment: Retries() = %d, want 1", e.Retries())
	}

	e.IncrementRetries()
	e.IncrementRetries()
	if e.Retries() != 3 {
		t.Errorf("after 3 increments: Retries() = %d, want 3", e.Retries())
	}
}

// TestSyncConfigFrom_UpdatesConfigAndPreservesRetries 验证 SyncConfigFrom 更新配置字段且保留 retries 计数
// 规格 §2.4.3: restart 配置变更"立即生效"，热重载时原地更新 engine 配置
func TestSyncConfigFrom_UpdatesConfigAndPreservesRetries(t *testing.T) {
	// 初始 engine: always, max_retries=0(无限), backoff=1000, multiplier=2
	e := NewRestartEngine(RestartAlways, 1000, 30000, 2, 0, 300)
	e.IncrementRetries()
	e.IncrementRetries()
	e.IncrementRetries()
	if e.Retries() != 3 {
		t.Fatalf("setup: Retries() = %d, want 3", e.Retries())
	}

	// 热重载后的新配置: on-failure, max_retries=5, backoff=500, multiplier=3
	fresh := NewRestartEngine(RestartOnFailure, 500, 60000, 3, 5, 600)
	e.SyncConfigFrom(fresh)

	// retries 计数应保留
	if e.Retries() != 3 {
		t.Errorf("after sync: Retries() = %d, want 3 (preserved)", e.Retries())
	}

	// maxRetries 已更新为 5，3 < 5 → 未达上限
	if e.MaxRetriesReached() {
		t.Errorf("after sync: MaxRetriesReached()=true, want false (retries=3 < max=5)")
	}

	// backoff 已更新: retries=3, backoff=500, multiplier=3 → 500*3^2 = 4500ms
	if d := e.BackoffDuration(); d != 4500*time.Millisecond {
		t.Errorf("after sync: BackoffDuration() = %v, want 4500ms (backoff=500*3^2)", d)
	}

	// policy 已更新为 on-failure
	if e.Policy() != RestartOnFailure {
		t.Errorf("after sync: Policy() = %v, want on-failure", e.Policy())
	}
}

// TestSyncConfigFrom_NilOther 验证 nil other 不 panic
func TestSyncConfigFrom_NilOther(t *testing.T) {
	e := newTestEngine(RestartAlways)
	e.IncrementRetries()

	// 不应 panic
	e.SyncConfigFrom(nil)

	if e.Retries() != 1 {
		t.Errorf("after nil sync: Retries() = %d, want 1 (unchanged)", e.Retries())
	}
	if e.MaxRetriesReached() {
		t.Errorf("after nil sync: MaxRetriesReached()=true, want false (max=0 unlimited)")
	}
}

// TestSyncConfigFrom_HotReloadMaxRetries 模拟用户报告的场景：
// 服务在无限重试（max_retries=0）时，用户改全局配置 max_retries=5，
// 热重载后 MaxRetriesReached 应反映新上限，使重试中的服务停止。
func TestSyncConfigFrom_HotReloadMaxRetries(t *testing.T) {
	// 初始: max_retries=0（无限），服务已重试 100 次仍未停止
	e := NewRestartEngine(RestartAlways, 1000, 30000, 2, 0, 300)
	for i := 0; i < 100; i++ {
		e.IncrementRetries()
	}
	if e.MaxRetriesReached() {
		t.Fatal("setup: max_retries=0 should be unlimited, but MaxRetriesReached()=true")
	}

	// 用户修改全局配置 max_retries=5，热重载后更新 engine
	fresh := NewRestartEngine(RestartAlways, 1000, 30000, 2, 5, 300)
	e.SyncConfigFrom(fresh)

	// 100 >= 5 → 现在应达到上限，重试中的服务下次决策将停止
	if !e.MaxRetriesReached() {
		t.Errorf("after hot reload: MaxRetriesReached()=false, want true (retries=100 >= max=5)")
	}
}
