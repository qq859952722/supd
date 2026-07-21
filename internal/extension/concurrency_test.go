package extension

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- ParseConcurrency 测试 ---

func TestParseConcurrency(t *testing.T) {
	tests := []struct {
		input     string
		policy    ConcurrencyPolicy
		debounce  int
		expectErr bool
	}{
		{"replace", PolicyReplace, 0, false},
		{"serialize", PolicySerialize, 0, false},
		{"parallel", PolicyParallel, 0, false},
		{"debounce:5s", PolicyDebounce, 5000, false},
		{"debounce:1s", PolicyDebounce, 1000, false},
		{"debounce:3600s", PolicyDebounce, 3600000, false},
		{"", PolicyReplace, 0, false},           // 空字符串默认 replace
		{"unknown", PolicyReplace, 0, false},    // 未知格式默认 replace（向后兼容）
		{"debounce:500ms", PolicyReplace, 0, true},  // A-04-001: Nms 格式已移除
		{"debounce:", PolicyReplace, 0, true},   // 缺少 Ns 后缀
		{"debounce:0s", PolicyReplace, 0, true}, // A-04-002: N<1 越界
		{"debounce:-1s", PolicyReplace, 0, true},
		{"debounce:3601s", PolicyReplace, 0, true}, // A-04-002: N>3600 越界
		{"debounce:abcs", PolicyReplace, 0, true},  // N 非整数
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			cfg, err := ParseConcurrency(tt.input)
			if tt.expectErr {
				if err == nil {
					t.Errorf("ParseConcurrency(%q) expected error, got nil", tt.input)
				}
				// 出错时降级为 replace
				if cfg.Policy != PolicyReplace {
					t.Errorf("ParseConcurrency(%q) on error: Policy = %v, want %v", tt.input, cfg.Policy, PolicyReplace)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseConcurrency(%q) unexpected error: %v", tt.input, err)
			}
			if cfg.Policy != tt.policy {
				t.Errorf("ParseConcurrency(%q).Policy = %v, want %v", tt.input, cfg.Policy, tt.policy)
			}
			if cfg.DebounceMs != tt.debounce {
				t.Errorf("ParseConcurrency(%q).DebounceMs = %v, want %v", tt.input, cfg.DebounceMs, tt.debounce)
			}
		})
	}
}

// --- Replace 策略测试 ---

func TestConcurrencyReplace_NoRunningTask(t *testing.T) {
	tracker := NewActionTracker(PolicyReplace, 0)

	var executed int32
	result, err := tracker.Apply(context.Background(), "run-1", func(ctx context.Context) (*RunResult, error) {
		atomic.AddInt32(&executed, 1)
		return &RunResult{RunID: "run-1", State: TaskSuccess}, nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.State != TaskSuccess {
		t.Errorf("result.State = %v, want %v", result.State, TaskSuccess)
	}
	if atomic.LoadInt32(&executed) != 1 {
		t.Errorf("executed = %d, want 1", executed)
	}
	if tracker.HasRunning() {
		t.Error("tracker should have no running tasks after completion")
	}
}

func TestConcurrencyReplace_CancelsRunningTask(t *testing.T) {
	tracker := NewActionTracker(PolicyReplace, 0)

	var task1Started sync.WaitGroup
	task1Started.Add(1)

	var task1Done int32

	// 启动第一个任务（长时间运行）
	go func() {
		tracker.Apply(context.Background(), "run-1", func(ctx context.Context) (*RunResult, error) {
			task1Started.Done()
			<-ctx.Done() // 等待被取消
			atomic.StoreInt32(&task1Done, 1)
			return &RunResult{RunID: "run-1", State: TaskCanceled}, nil
		})
	}()

	task1Started.Wait()
	time.Sleep(10 * time.Millisecond) // 确保任务1已进入执行

	// 启动第二个任务（应该取消第一个）
	result, err := tracker.Apply(context.Background(), "run-2", func(ctx context.Context) (*RunResult, error) {
		return &RunResult{RunID: "run-2", State: TaskSuccess}, nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RunID != "run-2" {
		t.Errorf("result.RunID = %v, want run-2", result.RunID)
	}
	if result.State != TaskSuccess {
		t.Errorf("result.State = %v, want %v", result.State, TaskSuccess)
	}

	// 第一个任务应该已被取消
	if atomic.LoadInt32(&task1Done) != 1 {
		t.Error("task 1 should have been cancelled")
	}
}

// --- Serialize 策略测试 ---

func TestConcurrencySerialize_NoRunningTask(t *testing.T) {
	tracker := NewActionTracker(PolicySerialize, 0)

	result, err := tracker.Apply(context.Background(), "run-1", func(ctx context.Context) (*RunResult, error) {
		return &RunResult{RunID: "run-1", State: TaskSuccess}, nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.State != TaskSuccess {
		t.Errorf("result.State = %v, want %v", result.State, TaskSuccess)
	}
}

func TestConcurrencySerialize_QueuesAndRunsAfterFirst(t *testing.T) {
	tracker := NewActionTracker(PolicySerialize, 0)

	var task1Started sync.WaitGroup
	task1Started.Add(1)
	var task1Unblock = make(chan struct{})

	// 启动第一个任务
	go func() {
		tracker.Apply(context.Background(), "run-1", func(ctx context.Context) (*RunResult, error) {
			task1Started.Done()
			<-task1Unblock // 阻塞直到测试释放
			return &RunResult{RunID: "run-1", State: TaskSuccess}, nil
		})
	}()

	task1Started.Wait()
	time.Sleep(10 * time.Millisecond)

	// 在另一个 goroutine 中启动第二个任务（应该被排队）
	var result2 *RunResult
	var err2 error
	done2 := make(chan struct{})
	go func() {
		result2, err2 = tracker.Apply(context.Background(), "run-2", func(ctx context.Context) (*RunResult, error) {
			return &RunResult{RunID: "run-2", State: TaskSuccess}, nil
		})
		close(done2)
	}()

	time.Sleep(20 * time.Millisecond) // 确保第二个 Apply 已经进入等待

	// 释放第一个任务
	close(task1Unblock)

	// 等待第二个任务完成
	<-done2

	if err2 != nil {
		t.Fatalf("unexpected error: %v", err2)
	}
	if result2.RunID != "run-2" {
		t.Errorf("result2.RunID = %v, want run-2", result2.RunID)
	}
	if result2.State != TaskSuccess {
		t.Errorf("result2.State = %v, want %v", result2.State, TaskSuccess)
	}
}

func TestConcurrencySerialize_SecondPendingCancelsFirst(t *testing.T) {
	tracker := NewActionTracker(PolicySerialize, 0)

	var task1Started sync.WaitGroup
	task1Started.Add(1)
	var task1Unblock = make(chan struct{})

	// 启动第一个任务
	go func() {
		tracker.Apply(context.Background(), "run-1", func(ctx context.Context) (*RunResult, error) {
			task1Started.Done()
			<-task1Unblock
			return &RunResult{RunID: "run-1", State: TaskSuccess}, nil
		})
	}()

	task1Started.Wait()
	time.Sleep(10 * time.Millisecond)

	// 第二个任务被排队
	done2 := make(chan struct{})
	go func() {
		r, _ := tracker.Apply(context.Background(), "run-2", func(ctx context.Context) (*RunResult, error) {
			return &RunResult{RunID: "run-2", State: TaskSuccess}, nil
		})
		_ = r
		close(done2)
	}()

	time.Sleep(10 * time.Millisecond)

	// 第三个任务被排队，应该取消第二个排队任务
	var result3 *RunResult
	var err3 error
	done3 := make(chan struct{})
	go func() {
		result3, err3 = tracker.Apply(context.Background(), "run-3", func(ctx context.Context) (*RunResult, error) {
			return &RunResult{RunID: "run-3", State: TaskSuccess}, nil
		})
		close(done3)
	}()

	time.Sleep(10 * time.Millisecond)

	// 释放第一个任务
	close(task1Unblock)

	// 等待第三个任务完成（第二个应该被取消）
	<-done3

	if err3 != nil {
		t.Fatalf("unexpected error: %v", err3)
	}
	if result3.RunID != "run-3" {
		t.Errorf("result3.RunID = %v, want run-3", result3.RunID)
	}
}

// --- Parallel 策略测试 ---

func TestConcurrencyParallel_RunsImmediately(t *testing.T) {
	tracker := NewActionTracker(PolicyParallel, 0)

	result, err := tracker.Apply(context.Background(), "run-1", func(ctx context.Context) (*RunResult, error) {
		return &RunResult{RunID: "run-1", State: TaskSuccess}, nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.State != TaskSuccess {
		t.Errorf("result.State = %v, want %v", result.State, TaskSuccess)
	}
}

func TestConcurrencyParallel_MultipleParallel(t *testing.T) {
	tracker := NewActionTracker(PolicyParallel, 0)

	var started sync.WaitGroup
	started.Add(3)

	// 并行启动3个任务
	results := make(chan *RunResult, 3)
	for i := 0; i < 3; i++ {
		go func(idx int) {
			r, _ := tracker.Apply(context.Background(), "run-"+string(rune('1'+idx)), func(ctx context.Context) (*RunResult, error) {
				started.Done()
				time.Sleep(50 * time.Millisecond) // 模拟工作
				return &RunResult{RunID: "run-" + string(rune('1'+idx)), State: TaskSuccess}, nil
			})
			results <- r
		}(i)
	}

	started.Wait() // 所有3个任务应该同时启动

	for i := 0; i < 3; i++ {
		r := <-results
		if r.State != TaskSuccess {
			t.Errorf("result %d State = %v, want %v", i, r.State, TaskSuccess)
		}
	}
}

// --- Debounce 策略测试 ---

func TestConcurrencyDebounce_WaitsForTimer(t *testing.T) {
	tracker := NewActionTracker(PolicyDebounce, 100) // 100ms debounce

	start := time.Now()
	result, err := tracker.Apply(context.Background(), "run-1", func(ctx context.Context) (*RunResult, error) {
		return &RunResult{RunID: "run-1", State: TaskSuccess}, nil
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.State != TaskSuccess {
		t.Errorf("result.State = %v, want %v", result.State, TaskSuccess)
	}
	// 应该至少等待 debounce 间隔
	if elapsed < 80*time.Millisecond {
		t.Errorf("elapsed = %v, should be at least ~100ms (debounce interval)", elapsed)
	}
}

func TestConcurrencyDebounce_ResetsTimer(t *testing.T) {
	tracker := NewActionTracker(PolicyDebounce, 100) // 100ms debounce

	var task1Started sync.WaitGroup
	task1Started.Add(1)

	// 启动第一个 debounce 触发（会被第二个取消）
	go func() {
		r, _ := tracker.Apply(context.Background(), "run-1", func(ctx context.Context) (*RunResult, error) {
			return &RunResult{RunID: "run-1", State: TaskSuccess}, nil
		})
		// 应该被取消
		if r.State != TaskCanceled {
			t.Errorf("run-1 should be canceled, got state %v", r.State)
		}
		task1Started.Done()
	}()

	time.Sleep(30 * time.Millisecond) // 在 debounce 间隔内

	// 第二个触发重置计时器
	result, err := tracker.Apply(context.Background(), "run-2", func(ctx context.Context) (*RunResult, error) {
		return &RunResult{RunID: "run-2", State: TaskSuccess}, nil
	})

	task1Started.Wait() // 等待第一个 Apply 返回

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 第二个触发应该成功执行
	if result.RunID != "run-2" {
		t.Errorf("result.RunID = %v, want run-2", result.RunID)
	}
	if result.State != TaskSuccess {
		t.Errorf("result.State = %v, want %v", result.State, TaskSuccess)
	}
}

func TestConcurrencyDebounce_CancelsRunningTask(t *testing.T) {
	// 测试 debounce 到期时如有运行中任务，按 replace 策略取消
	// REQ-F-018, 2.2.7: debounce 计时到期触发执行时，如有运行中的同 action 任务，按该扩展的 concurrency 策略处理（默认 replace）
	tracker := NewActionTracker(PolicyDebounce, 50) // 50ms debounce

	// 触发第一个任务，等 debounce 间隔后开始执行
	var run1Executing sync.WaitGroup
	run1Executing.Add(1)
	var run1Done int32

	go func() {
		tracker.Apply(context.Background(), "run-1", func(ctx context.Context) (*RunResult, error) {
			run1Executing.Done() // 通知 run-1 已开始执行
			<-ctx.Done()         // 等待被取消
			atomic.StoreInt32(&run1Done, 1)
			return &RunResult{RunID: "run-1", State: TaskCanceled}, nil
		})
	}()

	// 等待 run-1 真正开始执行（debounce 间隔之后）
	run1Executing.Wait()

	// 现在 run-1 正在运行，触发 run-2 的 debounce
	result, err := tracker.Apply(context.Background(), "run-2", func(ctx context.Context) (*RunResult, error) {
		return &RunResult{RunID: "run-2", State: TaskSuccess}, nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RunID != "run-2" {
		t.Errorf("result.RunID = %v, want run-2", result.RunID)
	}

	// run-1 应该被取消
	if atomic.LoadInt32(&run1Done) != 1 {
		t.Error("run-1 should have been cancelled")
	}
}

// --- 多 action 之间互不阻塞测试 ---

func TestConcurrencyManager_MultiActionNoBlock(t *testing.T) {
	mgr := NewConcurrencyManager()

	tracker1 := mgr.GetTracker("ext1", "action1", PolicySerialize, 0)
	tracker2 := mgr.GetTracker("ext1", "action2", PolicySerialize, 0)

	if tracker1 == tracker2 {
		t.Error("different actions should have different trackers")
	}

	// 同一扩展不同 action 应该可以并行执行
	var action1Started sync.WaitGroup
	action1Started.Add(1)
	var action1Unblock = make(chan struct{})

	go func() {
		tracker1.Apply(context.Background(), "run-1", func(ctx context.Context) (*RunResult, error) {
			action1Started.Done()
			<-action1Unblock
			return &RunResult{RunID: "run-1", State: TaskSuccess}, nil
		})
	}()

	action1Started.Wait()
	time.Sleep(10 * time.Millisecond)

	// action2 应该能立即执行，不被 action1 阻塞
	result, err := tracker2.Apply(context.Background(), "run-2", func(ctx context.Context) (*RunResult, error) {
		return &RunResult{RunID: "run-2", State: TaskSuccess}, nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.State != TaskSuccess {
		t.Errorf("result.State = %v, want %v", result.State, TaskSuccess)
	}

	close(action1Unblock)
}

func TestConcurrencyManager_GetTrackerReturnsSame(t *testing.T) {
	mgr := NewConcurrencyManager()

	t1 := mgr.GetTracker("ext1", "action1", PolicyReplace, 0)
	t2 := mgr.GetTracker("ext1", "action1", PolicySerialize, 0) // 不同策略也应该返回同一个

	if t1 != t2 {
		t.Error("same extName:actionID should return same tracker")
	}
}

// --- CancelRunning 测试 ---

func TestConcurrencyCancelRunning(t *testing.T) {
	tracker := NewActionTracker(PolicyParallel, 0)

	var started sync.WaitGroup
	started.Add(1)

	go func() {
		tracker.Apply(context.Background(), "run-1", func(ctx context.Context) (*RunResult, error) {
			started.Done()
			<-ctx.Done()
			return &RunResult{RunID: "run-1", State: TaskCanceled}, nil
		})
	}()

	started.Wait()
	time.Sleep(10 * time.Millisecond)

	if !tracker.HasRunning() {
		t.Error("tracker should have running task")
	}

	tracker.CancelRunning("run-1")

	if tracker.HasRunning() {
		t.Error("tracker should have no running tasks after cancel")
	}
}

// --- HasRunning 测试 ---

func TestConcurrencyHasRunning(t *testing.T) {
	tracker := NewActionTracker(PolicyParallel, 0)

	if tracker.HasRunning() {
		t.Error("new tracker should have no running tasks")
	}

	var started sync.WaitGroup
	started.Add(1)
	var unblock = make(chan struct{})

	go func() {
		tracker.Apply(context.Background(), "run-1", func(ctx context.Context) (*RunResult, error) {
			started.Done()
			<-unblock
			return &RunResult{RunID: "run-1", State: TaskSuccess}, nil
		})
	}()

	started.Wait()
	time.Sleep(10 * time.Millisecond)

	if !tracker.HasRunning() {
		t.Error("tracker should have running task")
	}

	close(unblock)
	time.Sleep(20 * time.Millisecond)

	if tracker.HasRunning() {
		t.Error("tracker should have no running tasks after completion")
	}
}

// --- RunCompleted 测试 ---

func TestConcurrencyRunCompleted(t *testing.T) {
	tracker := NewActionTracker(PolicySerialize, 0)

	var task1Started sync.WaitGroup
	task1Started.Add(1)
	var task1Unblock = make(chan struct{})

	// 启动第一个任务
	go func() {
		tracker.Apply(context.Background(), "run-1", func(ctx context.Context) (*RunResult, error) {
			task1Started.Done()
			<-task1Unblock
			return &RunResult{RunID: "run-1", State: TaskSuccess}, nil
		})
	}()

	task1Started.Wait()

	// 排队第二个任务
	var result2 *RunResult
	done2 := make(chan struct{})
	go func() {
		result2, _ = tracker.Apply(context.Background(), "run-2", func(ctx context.Context) (*RunResult, error) {
			return &RunResult{RunID: "run-2", State: TaskSuccess}, nil
		})
		close(done2)
	}()

	time.Sleep(20 * time.Millisecond)

	// 通过 RunCompleted 通知第一个任务完成
	close(task1Unblock)
	time.Sleep(20 * time.Millisecond)

	// 第二个任务应该开始执行
	select {
	case <-done2:
		if result2.RunID != "run-2" {
			t.Errorf("result2.RunID = %v, want run-2", result2.RunID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for run-2 to complete")
	}
}

// --- Debouncer 单元测试 ---

func TestDebouncer_ResetAndFire(t *testing.T) {
	d := NewDebouncer(50 * time.Millisecond)

	var executed int32
	d.Reset(func() {
		atomic.AddInt32(&executed, 1)
	})

	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt32(&executed) != 1 {
		t.Errorf("executed = %d, want 1", executed)
	}
}

func TestDebouncer_ResetResetsTimer(t *testing.T) {
	d := NewDebouncer(50 * time.Millisecond)

	var executed int32
	d.Reset(func() {
		atomic.AddInt32(&executed, 1)
	})

	time.Sleep(30 * time.Millisecond)

	// 重置定时器
	d.Reset(func() {
		atomic.AddInt32(&executed, 10)
	})

	time.Sleep(100 * time.Millisecond)

	// 只有第二次的函数应该被执行
	if atomic.LoadInt32(&executed) != 10 {
		t.Errorf("executed = %d, want 10", executed)
	}
}

func TestDebouncer_Stop(t *testing.T) {
	d := NewDebouncer(50 * time.Millisecond)

	var executed int32
	d.Reset(func() {
		atomic.AddInt32(&executed, 1)
	})

	d.Stop()

	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt32(&executed) != 0 {
		t.Errorf("executed = %d, want 0 (stopped before firing)", executed)
	}
}

func TestDebouncer_MultipleReset(t *testing.T) {
	d := NewDebouncer(30 * time.Millisecond)

	var executed int32

	// 连续多次 Reset，只有最后一次的函数应该被执行
	for i := 0; i < 5; i++ {
		idx := i
		d.Reset(func() {
			atomic.StoreInt32(&executed, int32(idx+1))
		})
		time.Sleep(10 * time.Millisecond) // 每次在间隔内重置
	}

	time.Sleep(60 * time.Millisecond)

	// 应该执行最后一次 Reset 的函数
	if atomic.LoadInt32(&executed) != 5 {
		t.Errorf("executed = %d, want 5", executed)
	}
}

// --- L-01-001 补充：7 个辅助函数单元测试 ---
// 覆盖 Stop / CancelRun / HasAnyRunning / RemoveExtension / WaitForAllRunning / RunCompleted / collectDones

// --- Stop 测试 ---

// TestConcurrencyActionTracker_Stop_Empty_NoPanic 边界：对空 tracker 调用 Stop 应安全返回。
func TestConcurrencyActionTracker_Stop_Empty_NoPanic(t *testing.T) {
	tracker := NewActionTracker(PolicyReplace, 0)
	tracker.Stop() // 无 pending、无 running、无 debouncer，不应 panic
}

// TestConcurrencyActionTracker_Stop_WithSerializePending_CancelsPending 验证 Stop 取消 serialize 排队任务。
func TestConcurrencyActionTracker_Stop_WithSerializePending_CancelsPending(t *testing.T) {
	tracker := NewActionTracker(PolicySerialize, 0)

	var task1Started sync.WaitGroup
	task1Started.Add(1)
	task1Unblock := make(chan struct{})

	// 启动第一个任务（占用 runningRuns 槽位）
	go func() {
		tracker.Apply(context.Background(), "run-1", func(ctx context.Context) (*RunResult, error) {
			task1Started.Done()
			<-task1Unblock
			return &RunResult{RunID: "run-1", State: TaskSuccess}, nil
		})
	}()

	task1Started.Wait()
	time.Sleep(10 * time.Millisecond)

	// 排队第二个任务（应进入 pendingRun）
	var result2 *RunResult
	done2 := make(chan struct{})
	go func() {
		result2, _ = tracker.Apply(context.Background(), "run-2", func(ctx context.Context) (*RunResult, error) {
			return &RunResult{RunID: "run-2", State: TaskSuccess}, nil
		})
		close(done2)
	}()

	time.Sleep(20 * time.Millisecond) // 确保 run-2 已进入 pendingRun

	// Stop 应取消 pendingRun（run-2），向其 resultCh 发送 TaskCanceled
	tracker.Stop()

	select {
	case <-done2:
		if result2 == nil || result2.State != TaskCanceled {
			t.Errorf("run-2 should be canceled by Stop, got result=%v", result2)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for run-2 to be canceled by Stop")
	}

	// 释放 run-1（Stop 不取消运行中任务，需手动释放；Apply cleanup 的 startPendingRunLocked 会因 pendingRun=nil 而 no-op）
	close(task1Unblock)
	time.Sleep(20 * time.Millisecond)
}

// TestConcurrencyActionTracker_Stop_WithDebouncePending_CancelsPending 验证 Stop 取消 debounce 待执行任务。
func TestConcurrencyActionTracker_Stop_WithDebouncePending_CancelsPending(t *testing.T) {
	tracker := NewActionTracker(PolicyDebounce, 2000) // 2s debounce，确保 Stop 前定时器未触发

	var result1 *RunResult
	done1 := make(chan struct{})
	go func() {
		result1, _ = tracker.Apply(context.Background(), "run-1", func(ctx context.Context) (*RunResult, error) {
			return &RunResult{RunID: "run-1", State: TaskSuccess}, nil
		})
		close(done1)
	}()

	time.Sleep(20 * time.Millisecond) // 确保 Apply 已设置 debouncePending

	tracker.Stop() // 应取消 debouncePending

	select {
	case <-done1:
		if result1 == nil || result1.State != TaskCanceled {
			t.Errorf("run-1 should be canceled by Stop, got result=%v", result1)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for run-1 to be canceled by Stop")
	}
}

// TestConcurrencyActionTracker_Stop_WithRunningTask_DoesNotCancel 验证 Stop 不取消运行中任务。
// 规格 B-05-002：热重载不影响运行中服务，让它们自然完成。
func TestConcurrencyActionTracker_Stop_WithRunningTask_DoesNotCancel(t *testing.T) {
	tracker := NewActionTracker(PolicyParallel, 0)

	var task1Started sync.WaitGroup
	task1Started.Add(1)
	task1Unblock := make(chan struct{})

	go func() {
		tracker.Apply(context.Background(), "run-1", func(ctx context.Context) (*RunResult, error) {
			task1Started.Done()
			<-task1Unblock
			return &RunResult{RunID: "run-1", State: TaskSuccess}, nil
		})
	}()

	task1Started.Wait()
	time.Sleep(10 * time.Millisecond)

	if !tracker.HasRunning() {
		t.Fatal("tracker should have running task before Stop")
	}

	tracker.Stop()

	// Stop 后任务应仍在运行（未被取消）
	if !tracker.HasRunning() {
		t.Error("Stop should not cancel running tasks (B-05-002: let them complete naturally)")
	}

	close(task1Unblock)
	time.Sleep(20 * time.Millisecond)

	if tracker.HasRunning() {
		t.Error("tracker should have no running tasks after task completes")
	}
}

// --- ConcurrencyManager.CancelRun 测试 ---

// TestConcurrencyManager_CancelRun_FindsRunAcrossTrackers 验证 CancelRun 能在多个 tracker 中找到并取消任务。
func TestConcurrencyManager_CancelRun_FindsRunAcrossTrackers(t *testing.T) {
	mgr := NewConcurrencyManager()
	tracker := mgr.GetTracker("ext1", "action1", PolicyParallel, 0)

	var started sync.WaitGroup
	started.Add(1)

	go func() {
		tracker.Apply(context.Background(), "run-target", func(ctx context.Context) (*RunResult, error) {
			started.Done()
			<-ctx.Done() // 等待被取消
			return &RunResult{RunID: "run-target", State: TaskCanceled}, nil
		})
	}()

	started.Wait()
	time.Sleep(10 * time.Millisecond)

	// CancelRun 应遍历 trackers 找到 run-target 并取消
	if !mgr.CancelRun("run-target") {
		t.Error("CancelRun should return true for existing runID")
	}

	// 等待 Apply cleanup 完成（删除 runningRuns 中的条目）
	time.Sleep(20 * time.Millisecond)

	if mgr.HasAnyRunning() {
		t.Error("manager should have no running tasks after CancelRun")
	}
}

// TestConcurrencyManager_CancelRun_NonExistentReturnsFalse 边界：未知 runID 返回 false。
func TestConcurrencyManager_CancelRun_NonExistentReturnsFalse(t *testing.T) {
	mgr := NewConcurrencyManager()
	mgr.GetTracker("ext1", "action1", PolicyParallel, 0)

	// 无任何运行中任务时，CancelRun 应返回 false
	if mgr.CancelRun("nonexistent") {
		t.Error("CancelRun should return false for non-existent runID")
	}
}

// TestConcurrencyManager_CancelRun_MultipleTrackersFindsRightOne 验证 CancelRun 在多 tracker 中精确定位。
func TestConcurrencyManager_CancelRun_MultipleTrackersFindsRightOne(t *testing.T) {
	mgr := NewConcurrencyManager()
	trackerA := mgr.GetTracker("ext1", "actionA", PolicyParallel, 0)
	trackerB := mgr.GetTracker("ext1", "actionB", PolicyParallel, 0)

	var startedA, startedB sync.WaitGroup
	startedA.Add(1)
	startedB.Add(1)
	unblockA := make(chan struct{})

	go func() {
		trackerA.Apply(context.Background(), "run-A", func(ctx context.Context) (*RunResult, error) {
			startedA.Done()
			<-unblockA
			return &RunResult{RunID: "run-A", State: TaskSuccess}, nil
		})
	}()

	go func() {
		trackerB.Apply(context.Background(), "run-B", func(ctx context.Context) (*RunResult, error) {
			startedB.Done()
			<-ctx.Done()
			return &RunResult{RunID: "run-B", State: TaskCanceled}, nil
		})
	}()

	startedA.Wait()
	startedB.Wait()
	time.Sleep(10 * time.Millisecond)

	// 仅取消 run-B，run-A 不受影响
	if !mgr.CancelRun("run-B") {
		t.Error("CancelRun should return true for run-B")
	}

	time.Sleep(20 * time.Millisecond)

	// run-A 应仍在运行
	if !trackerA.HasRunning() {
		t.Error("run-A should still be running (not the cancel target)")
	}
	// run-B 应已取消
	if trackerB.HasRunning() {
		t.Error("run-B should be canceled")
	}

	close(unblockA)
	time.Sleep(20 * time.Millisecond)
}

// --- ConcurrencyManager.HasAnyRunning 测试 ---

// TestConcurrencyManager_HasAnyRunning_EmptyReturnsFalse 边界：空 manager 返回 false。
func TestConcurrencyManager_HasAnyRunning_EmptyReturnsFalse(t *testing.T) {
	mgr := NewConcurrencyManager()
	if mgr.HasAnyRunning() {
		t.Error("empty manager should have no running tasks")
	}
}

// TestConcurrencyManager_HasAnyRunning_WithRunningReturnsTrue 验证有运行中任务时返回 true。
func TestConcurrencyManager_HasAnyRunning_WithRunningReturnsTrue(t *testing.T) {
	mgr := NewConcurrencyManager()
	tracker := mgr.GetTracker("ext1", "action1", PolicyParallel, 0)

	var started sync.WaitGroup
	started.Add(1)
	unblock := make(chan struct{})

	go func() {
		tracker.Apply(context.Background(), "run-1", func(ctx context.Context) (*RunResult, error) {
			started.Done()
			<-unblock
			return &RunResult{RunID: "run-1", State: TaskSuccess}, nil
		})
	}()

	started.Wait()
	time.Sleep(10 * time.Millisecond)

	if !mgr.HasAnyRunning() {
		t.Error("manager should report running tasks")
	}

	close(unblock)
	time.Sleep(20 * time.Millisecond)

	if mgr.HasAnyRunning() {
		t.Error("manager should report no running tasks after completion")
	}
}

// TestConcurrencyManager_HasAnyRunning_TrackerExistsButNoRunningReturnsFalse 边界：有 tracker 但无运行中任务。
func TestConcurrencyManager_HasAnyRunning_TrackerExistsButNoRunningReturnsFalse(t *testing.T) {
	mgr := NewConcurrencyManager()
	mgr.GetTracker("ext1", "action1", PolicyParallel, 0)
	mgr.GetTracker("ext2", "action2", PolicySerialize, 0)

	if mgr.HasAnyRunning() {
		t.Error("manager with idle trackers should return false")
	}
}

// --- ConcurrencyManager.RemoveExtension 测试 ---

// TestConcurrencyManager_RemoveExtension_RemovesOnlyMatchingTrackers 验证只移除匹配扩展的 tracker。
func TestConcurrencyManager_RemoveExtension_RemovesOnlyMatchingTrackers(t *testing.T) {
	mgr := NewConcurrencyManager()
	t1 := mgr.GetTracker("ext1", "action1", PolicyParallel, 0)
	t2 := mgr.GetTracker("ext2", "action1", PolicyParallel, 0)

	// 移除 ext2
	mgr.RemoveExtension("ext2")

	// ext2 的 tracker 应已从 map 移除：再次 GetTracker 应返回新 tracker（不同指针）
	t2Again := mgr.GetTracker("ext2", "action1", PolicyParallel, 0)
	if t2Again == t2 {
		t.Error("ext2's tracker should have been removed (expected new tracker instance)")
	}

	// ext1 的 tracker 应保留（同一指针）
	t1Again := mgr.GetTracker("ext1", "action1", PolicyParallel, 0)
	if t1Again != t1 {
		t.Error("ext1's tracker should still be the same instance (not removed)")
	}

	// 幂等性边界：再次移除 ext2 不应 panic
	mgr.RemoveExtension("ext2")
}

// TestConcurrencyManager_RemoveExtension_StopsPendingDebounce 验证 RemoveExtension 调用 Stop 清理 pending。
func TestConcurrencyManager_RemoveExtension_StopsPendingDebounce(t *testing.T) {
	mgr := NewConcurrencyManager()
	tracker := mgr.GetTracker("ext1", "action1", PolicyDebounce, 2000) // 2s debounce

	var result1 *RunResult
	done1 := make(chan struct{})
	go func() {
		result1, _ = tracker.Apply(context.Background(), "run-1", func(ctx context.Context) (*RunResult, error) {
			return &RunResult{RunID: "run-1", State: TaskSuccess}, nil
		})
		close(done1)
	}()

	time.Sleep(20 * time.Millisecond) // 确保 debouncePending 已设置

	// RemoveExtension 应调用 Stop，取消 debouncePending
	mgr.RemoveExtension("ext1")

	select {
	case <-done1:
		if result1 == nil || result1.State != TaskCanceled {
			t.Errorf("run-1 should be canceled by RemoveExtension->Stop, got result=%v", result1)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for debounce pending to be canceled")
	}
}

// --- ConcurrencyManager.WaitForAllRunning 测试 ---

// TestConcurrencyManager_WaitForAllRunning_NoTasks_ReturnsZero 边界：无任务时立即返回 0。
func TestConcurrencyManager_WaitForAllRunning_NoTasks_ReturnsZero(t *testing.T) {
	mgr := NewConcurrencyManager()
	// 边界：无 tracker
	if got := mgr.WaitForAllRunning(100 * time.Millisecond); got != 0 {
		t.Errorf("WaitForAllRunning on empty manager = %d, want 0", got)
	}

	mgr.GetTracker("ext1", "action1", PolicyParallel, 0)
	// 边界：有 tracker 但无运行中任务
	if got := mgr.WaitForAllRunning(100 * time.Millisecond); got != 0 {
		t.Errorf("WaitForAllRunning with idle trackers = %d, want 0", got)
	}
}

// TestConcurrencyManager_WaitForAllRunning_AllComplete_ReturnsZero 验证所有任务在超时内完成时返回 0。
func TestConcurrencyManager_WaitForAllRunning_AllComplete_ReturnsZero(t *testing.T) {
	mgr := NewConcurrencyManager()
	tracker := mgr.GetTracker("ext1", "action1", PolicyParallel, 0)

	var started sync.WaitGroup
	started.Add(1)
	unblock := make(chan struct{})

	go func() {
		tracker.Apply(context.Background(), "run-1", func(ctx context.Context) (*RunResult, error) {
			started.Done()
			<-unblock
			return &RunResult{RunID: "run-1", State: TaskSuccess}, nil
		})
	}()

	started.Wait()
	time.Sleep(10 * time.Millisecond)

	// 在另一个 goroutine 中释放 unblock，让任务在 50ms 后完成
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(unblock)
	}()

	// 给 2 秒超时，任务应在 50ms 后完成
	if got := mgr.WaitForAllRunning(2 * time.Second); got != 0 {
		t.Errorf("WaitForAllRunning = %d, want 0 (all should complete within timeout)", got)
	}
	time.Sleep(20 * time.Millisecond)
}

// TestConcurrencyManager_WaitForAllRunning_TimeoutExceeded_ReturnsRemaining 验证超时后返回剩余任务数。
func TestConcurrencyManager_WaitForAllRunning_TimeoutExceeded_ReturnsRemaining(t *testing.T) {
	mgr := NewConcurrencyManager()
	tracker := mgr.GetTracker("ext1", "action1", PolicyParallel, 0)

	var started sync.WaitGroup
	started.Add(1)
	unblock := make(chan struct{})

	go func() {
		tracker.Apply(context.Background(), "run-1", func(ctx context.Context) (*RunResult, error) {
			started.Done()
			<-unblock // 长时间阻塞
			return &RunResult{RunID: "run-1", State: TaskSuccess}, nil
		})
	}()

	started.Wait()
	time.Sleep(10 * time.Millisecond)

	// 50ms 超时，任务仍在阻塞，应返回 1（剩余 1 个）
	start := time.Now()
	got := mgr.WaitForAllRunning(50 * time.Millisecond)
	elapsed := time.Since(start)

	if got != 1 {
		t.Errorf("WaitForAllRunning = %d, want 1 (1 task still running)", got)
	}
	if elapsed < 40*time.Millisecond {
		t.Errorf("elapsed = %v, should be at least ~50ms (timeout duration)", elapsed)
	}

	// 释放任务，避免泄漏
	close(unblock)
	time.Sleep(20 * time.Millisecond)
}

// --- RunCompleted 测试 ---

// TestConcurrencyActionTracker_RunCompleted_RemovesTaskFromRunning 验证 RunCompleted 从 runningRuns 移除任务。
func TestConcurrencyActionTracker_RunCompleted_RemovesTaskFromRunning(t *testing.T) {
	tracker := NewActionTracker(PolicyParallel, 0)

	var started sync.WaitGroup
	started.Add(1)
	unblock := make(chan struct{})

	go func() {
		tracker.Apply(context.Background(), "run-1", func(ctx context.Context) (*RunResult, error) {
			started.Done()
			<-unblock
			return &RunResult{RunID: "run-1", State: TaskSuccess}, nil
		})
	}()

	started.Wait()
	time.Sleep(10 * time.Millisecond)

	if !tracker.HasRunning() {
		t.Fatal("tracker should have running task before RunCompleted")
	}

	// 调用 RunCompleted 应从 runningRuns 移除任务并关闭 done 通道
	tracker.RunCompleted("run-1", &RunResult{RunID: "run-1", State: TaskSuccess})

	if tracker.HasRunning() {
		t.Error("tracker should have no running tasks after RunCompleted")
	}

	// 释放 execute（Apply cleanup 的 markDone/delete 会 no-op，因 RunCompleted 已清理）
	close(unblock)
	time.Sleep(20 * time.Millisecond)
}

// TestConcurrencyActionTracker_RunCompleted_UnknownRunID_NoOp 边界：未知 runID 时安全返回。
func TestConcurrencyActionTracker_RunCompleted_UnknownRunID_NoOp(t *testing.T) {
	tracker := NewActionTracker(PolicyParallel, 0)

	var started sync.WaitGroup
	started.Add(1)
	unblock := make(chan struct{})

	go func() {
		tracker.Apply(context.Background(), "run-1", func(ctx context.Context) (*RunResult, error) {
			started.Done()
			<-unblock
			return &RunResult{RunID: "run-1", State: TaskSuccess}, nil
		})
	}()

	started.Wait()
	time.Sleep(10 * time.Millisecond)

	// 边界：未知 runID，RunCompleted 应 no-op（不 panic、不影响现有任务）
	tracker.RunCompleted("nonexistent", &RunResult{RunID: "nonexistent", State: TaskSuccess})

	if !tracker.HasRunning() {
		t.Error("run-1 should still be running after RunCompleted on unknown runID")
	}

	close(unblock)
	time.Sleep(20 * time.Millisecond)
}

// TestConcurrencyActionTracker_RunCompleted_SerializeTriggersPending 验证 serialize 策略下 RunCompleted 触发排队任务。
func TestConcurrencyActionTracker_RunCompleted_SerializeTriggersPending(t *testing.T) {
	tracker := NewActionTracker(PolicySerialize, 0)

	var task1Started sync.WaitGroup
	task1Started.Add(1)
	task1Unblock := make(chan struct{})

	// 启动 task1（占用 runningRuns 槽位）
	go func() {
		tracker.Apply(context.Background(), "run-1", func(ctx context.Context) (*RunResult, error) {
			task1Started.Done()
			<-task1Unblock
			return &RunResult{RunID: "run-1", State: TaskSuccess}, nil
		})
	}()

	task1Started.Wait()
	time.Sleep(10 * time.Millisecond)

	// 排队 task2（应进入 pendingRun）
	var result2 *RunResult
	done2 := make(chan struct{})
	go func() {
		result2, _ = tracker.Apply(context.Background(), "run-2", func(ctx context.Context) (*RunResult, error) {
			return &RunResult{RunID: "run-2", State: TaskSuccess}, nil
		})
		close(done2)
	}()

	time.Sleep(20 * time.Millisecond) // 确保 task2 已进入 pendingRun

	// RunCompleted("run-1") 应触发 startPendingRunLocked，启动 task2
	tracker.RunCompleted("run-1", &RunResult{RunID: "run-1", State: TaskSuccess})

	select {
	case <-done2:
		if result2 == nil || result2.RunID != "run-2" || result2.State != TaskSuccess {
			t.Errorf("task2 result = %v, want run-2/success", result2)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for task2 to be triggered by RunCompleted")
	}

	// 释放 task1（其 Apply cleanup 会 no-op，因 RunCompleted 已清理）
	close(task1Unblock)
	time.Sleep(20 * time.Millisecond)
}

// --- collectDones 测试 ---

// TestConcurrencyActionTracker_collectDones_NoRunning_ReturnsEmpty 边界：无运行中任务时返回空切片。
func TestConcurrencyActionTracker_collectDones_NoRunning_ReturnsEmpty(t *testing.T) {
	tracker := NewActionTracker(PolicyParallel, 0)
	dones := tracker.collectDones()
	if len(dones) != 0 {
		t.Errorf("collectDones() returned %d channels, want 0", len(dones))
	}
}

// TestConcurrencyActionTracker_collectDones_WithRunning_ReturnsChannels 验证返回运行中任务的 done 通道。
func TestConcurrencyActionTracker_collectDones_WithRunning_ReturnsChannels(t *testing.T) {
	tracker := NewActionTracker(PolicyParallel, 0)

	var started sync.WaitGroup
	started.Add(1)
	unblock := make(chan struct{})

	go func() {
		tracker.Apply(context.Background(), "run-1", func(ctx context.Context) (*RunResult, error) {
			started.Done()
			<-unblock
			return &RunResult{RunID: "run-1", State: TaskSuccess}, nil
		})
	}()

	started.Wait()
	time.Sleep(10 * time.Millisecond)

	dones := tracker.collectDones()
	if len(dones) != 1 {
		t.Fatalf("collectDones() returned %d channels, want 1", len(dones))
	}

	// 验证通道尚处于 open 状态（任务运行中，done 未关闭）
	select {
	case <-dones[0]:
		t.Error("done channel should not be closed while task is running")
	default:
		// 通道 open 且无数据，符合预期
	}

	close(unblock)
	time.Sleep(20 * time.Millisecond)

	// 任务完成后 done 通道应被关闭（接收应立即返回）
	select {
	case <-dones[0]:
		// 期望：通道已关闭，接收成功
	default:
		t.Error("done channel should be closed after task completes")
	}
}
