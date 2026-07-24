package extension

import (
	"context"
	"testing"
	"time"

	"github.com/supdorg/supd/internal/watch"
)

// TestCronScheduler_GetNextRun 验证注册后可查询下次执行时间
// 注意：cron 的 Next 仅在 Start() 后才会计算，故此处先启动调度器
func TestCronScheduler_GetNextRun(t *testing.T) {
	disp := NewDispatcher(nil, "", "", 0)
	sched := NewCronScheduler(disp)

	if err := sched.AddJob("ext", "run", "0 * * * *", nil, &watch.DiscoveryResult{}); err != nil {
		t.Fatalf("AddJob failed: %v", err)
	}
	sched.Start()
	defer sched.Stop(context.Background())

	next, ok := sched.GetNextRun("ext", "run")
	if !ok {
		t.Fatal("expected GetNextRun ok=true")
	}
	if next.IsZero() {
		t.Fatalf("expected a non-zero next run time, got %v", next)
	}
	if !next.After(time.Now().Add(-time.Minute)) {
		t.Fatalf("expected next run time in the near future, got %v", next)
	}

	// 不存在的 job 应返回 false
	if _, ok := sched.GetNextRun("ext", "nope"); ok {
		t.Fatal("expected false for nonexistent job")
	}
}

// TestCronScheduler_ClearAllJobs 验证热重载清空中断旧 jobs
func TestCronScheduler_ClearAllJobs(t *testing.T) {
	disp := NewDispatcher(nil, "", "", 0)
	sched := NewCronScheduler(disp)

	if err := sched.AddJob("ext", "run", "0 * * * *", nil, &watch.DiscoveryResult{}); err != nil {
		t.Fatalf("AddJob failed: %v", err)
	}
	if sched.JobCount() != 1 {
		t.Fatalf("expected 1 job before clear, got %d", sched.JobCount())
	}

	sched.ClearAllJobs()
	if sched.JobCount() != 0 {
		t.Fatalf("expected 0 jobs after clear, got %d", sched.JobCount())
	}
}

// TestCronScheduler_SetCronTrigger 验证注入 CronTrigger 后带重试配置注册 job
func TestCronScheduler_SetCronTrigger(t *testing.T) {
	disp := NewDispatcher(nil, "", "", 0)
	sched := NewCronScheduler(disp)
	ct := NewCronTrigger(sched)
	sched.SetCronTrigger(ct)

	retry := &RetryConfig{MaxRetries: 2, IntervalMinutes: 100000}
	if err := sched.AddJob("ext", "run", "0 * * * *", retry, &watch.DiscoveryResult{}); err != nil {
		t.Fatalf("AddJob with retry config failed: %v", err)
	}
	if !sched.HasJob("ext", "run") {
		t.Fatal("expected job registered with cronTrigger set")
	}
}

// TestCronScheduler_SetTaskManager 验证注入 TaskManager 不 panic
func TestCronScheduler_SetTaskManager(t *testing.T) {
	disp := NewDispatcher(nil, "", "", 0)
	sched := NewCronScheduler(disp)
	tm := NewTaskManager(7)
	sched.SetTaskManager(tm)
	if err := sched.AddJob("ext", "run", "0 * * * *", nil, &watch.DiscoveryResult{}); err != nil {
		t.Fatalf("AddJob after SetTaskManager failed: %v", err)
	}
}
