package extension

import (
	"context"
	"testing"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/watch"
)

// TestResolveScheduleActionID 解析 on_schedule 的 actionID 回退规则
func TestResolveScheduleActionID(t *testing.T) {
	meta := &config.ExtensionMeta{
		Actions: []config.Action{{ID: "run"}, {ID: "check"}},
	}
	if got := resolveScheduleActionID(meta, "check"); got != "check" {
		t.Errorf("explicit action: want check, got %s", got)
	}
	if got := resolveScheduleActionID(meta, ""); got != "run" {
		t.Errorf("empty action: want first action run, got %s", got)
	}

	// 无 actions 的 meta：返回默认 "run"
	empty := &config.ExtensionMeta{}
	if got := resolveScheduleActionID(empty, ""); got != "run" {
		t.Errorf("no actions: want default run, got %s", got)
	}
	// 显式指定的 action 即使无对应 actions 也原样返回（校验在 RegisterSchedule 做）
	if got := resolveScheduleActionID(empty, "custom"); got != "custom" {
		t.Errorf("explicit custom: want custom, got %s", got)
	}
}

// TestCronTrigger_ClearRetryState 验证清理重试计数
func TestCronTrigger_ClearRetryState(t *testing.T) {
	disp := NewDispatcher(nil, "", "", 0)
	sched := NewCronScheduler(disp)
	ct := NewCronTrigger(sched)

	ct.retryAttempts["ext:run"] = 5
	ct.ClearRetryState()

	if ct.GetRetryAttempts("ext", "run") != 0 {
		t.Error("expected retry attempts cleared to 0")
	}
}

// TestCronTrigger_HandleResult_Nil 验证 nil 结果安全返回
func TestCronTrigger_HandleResult_Nil(t *testing.T) {
	disp := NewDispatcher(nil, "", "", 0)
	sched := NewCronScheduler(disp)
	ct := NewCronTrigger(sched)

	ct.HandleResult(context.Background(), nil, &RetryConfig{MaxRetries: 3}, &watch.DiscoveryResult{})
	// 无 panic 即通过
}

// TestCronTrigger_HandleResult_SchedulesRetry 验证 ShouldRetry 为真时调度重试 goroutine
// 使用超大 IntervalMinutes，重试 goroutine 在测试期间不会真正触发执行（安全）
func TestCronTrigger_HandleResult_SchedulesRetry(t *testing.T) {
	disp := NewDispatcher(nil, "", "", 0)
	sched := NewCronScheduler(disp)
	ct := NewCronTrigger(sched)

	disc := &watch.DiscoveryResult{}
	retry := &RetryConfig{MaxRetries: 3, IntervalMinutes: 100000}
	res := &RunResult{ExtensionName: "ext", ActionID: "run", State: TaskFailed}

	ct.HandleResult(context.Background(), res, retry, disc)

	// ShouldRetry 返回 true → 重试计数 +1（调度了一个沉睡中的 goroutine）
	if got := ct.GetRetryAttempts("ext", "run"); got != 1 {
		t.Fatalf("expected 1 scheduled retry attempt, got %d", got)
	}
}
