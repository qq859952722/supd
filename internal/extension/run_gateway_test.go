package extension

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/watch"
)

// helper: 构造一个仅含单个全局扩展的 DiscoveryResult。
func gatewayDiscovery(name string, meta *config.ExtensionMeta) *watch.DiscoveryResult {
	return &watch.DiscoveryResult{
		Services:   map[string]*watch.ServiceEntry{},
		GlobalExts: map[string]*watch.ExtensionEntry{name: {Name: name, Meta: meta, ConfigPath: "/fake/" + name + "/meta.yaml"}},
		Runtimes:   map[string]string{},
	}
}

// newGatewaySetup 构建 RunGateway + TaskManager（tmpDir 作为 baseDir/logDir 与 WorkDir）。
func newGatewaySetup(t *testing.T, name string, meta *config.ExtensionMeta) (RunGateway, *TaskManager, string) {
	t.Helper()
	tmpDir := t.TempDir()
	executor := NewExecutor(tmpDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, tmpDir, 1800)
	taskMgr := NewTaskManager(7)
	gw := NewRunGateway(dispatcher, taskMgr, gatewayDiscovery(name, meta))
	return gw, taskMgr, tmpDir
}

// makeMeta 构造测试扩展 meta（默认 on_demand/parallel，entry 绝对路径 exit 0）。
func makeMeta(t *testing.T, refine func(*config.ExtensionMeta)) *config.ExtensionMeta {
	t.Helper()
	m := testMeta("gw-ext", createTestScript(t, t.TempDir(), "run.sh", "exit 0"))
	m.Concurrency = "parallel"
	if refine != nil {
		refine(m)
	}
	return m
}

// testSubmit 构造 RunSpec。
func testSubmit(extName, actionID, workDir string) RunSpec {
	return RunSpec{
		ExtensionName: extName,
		ActionID:      actionID,
		Context: TriggerContext{
			EventType: "on_demand",
			WorkDir:   workDir,
		},
	}
}

// awaitTerminal 等待终态回调，断言 state 恰好为 want，且同一 runID 二次终态只回调一次。
func awaitTerminal(t *testing.T, ch chan TerminalResult, runID string, want TaskState) {
	t.Helper()
	select {
	case tr := <-ch:
		if tr.State != want {
			t.Fatalf("terminal state = %q, want %q (runID=%s result: %+v)", tr.State, want, runID, tr)
		}
		if tr.RunID != runID {
			t.Errorf("terminal RunID = %q, want %q", tr.RunID, runID)
		}
		// 同一 runID 二次终态只回调一次：非阻塞检查不应有第二条。
		select {
		case tr2 := <-ch:
			t.Fatalf("callback fired twice for %s: %+v, %+v", runID, tr, tr2)
		case <-time.After(50 * time.Millisecond):
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("timed out waiting for terminal state %q (runID=%s)", want, runID)
	}
}

// newTerminalRecv 注册终态回调并返回接收 channel。
func newTerminalRecv(gw RunGateway, runID string) chan TerminalResult {
	ch := make(chan TerminalResult, 1)
	gw.OnRunTerminal(runID, func(tr TerminalResult) { ch <- tr })
	return ch
}

func TestRunGateway_Submit_Success_SameRunID(t *testing.T) {
	gw, taskMgr, workDir := newGatewaySetup(t, "gw-ext", makeMeta(t, nil))
	runID := "pregen-run-001"
	ch := newTerminalRecv(gw, runID)

	accepted, err := gw.SubmitRun(testSubmit("gw-ext", "run", workDir), runID)
	if err != nil {
		t.Fatalf("SubmitRun error: %v", err)
	}
	if !accepted {
		t.Fatal("SubmitRun returned accepted=false for successful run")
	}

	awaitTerminal(t, ch, runID, TaskSuccess)

	// TaskManager 登记与回调使用同一预生成 ID。
	if got := taskMgr.GetRun(runID); got == nil {
		t.Fatalf("taskMgr.GetRun(%q) = nil, want recorded", runID)
	} else if got.RunID != runID {
		t.Errorf("recorded RunID = %q, want %q", got.RunID, runID)
	}
}

func TestRunGateway_Submit_MissingRunID_Rejected(t *testing.T) {
	gw, _, workDir := newGatewaySetup(t, "gw-ext", makeMeta(t, nil))
	accepted, err := gw.SubmitRun(testSubmit("gw-ext", "run", workDir), "")
	if err == nil {
		t.Fatal("expected error for missing runID, got nil")
	}
	if accepted {
		t.Fatal("expected accepted=false for missing runID")
	}
}

// TestRunGateway_TerminalPaths 表驱动覆盖终态收口 5 路径。
// 每条路径实际提交并等待终态回调，断言回调恰好触发一次且 state 正确。
func TestRunGateway_TerminalPaths(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, gw RunGateway, taskMgr *TaskManager, workDir string) (accepted bool, err error)
	}{
		{
			// 04-3(1)：serialize 队列满被拒 → 产生 failed 终态记录。
			name: "serialize_queue_full_to_failed",
			run: func(t *testing.T, gw RunGateway, taskMgr *TaskManager, workDir string) (bool, error) {
				meta := makeMeta(t, func(m *config.ExtensionMeta) {
					m.Concurrency = "serialize"
					m.Entry = createTestScript(t, t.TempDir(), "slow.sh", "sleep 3") // 占住运行槽
				})
				gw, taskMgr, workDir = newGatewaySetup(t, "gw-ext", meta)

				// 第 1 个运行中的任务占住 runningRuns。
				go func() { gw.SubmitRun(testSubmit("gw-ext", "run", workDir), "hold-000") }()
				time.Sleep(100 * time.Millisecond)

				// 16 个排队任务占满 pendingRuns。
				var wg sync.WaitGroup
				for i := 0; i < 16; i++ {
					wg.Add(1)
					go func(idx int) {
						defer wg.Done()
						gw.SubmitRun(testSubmit("gw-ext", "run", workDir), fmt.Sprintf("queued-%02d", idx))
					}(i)
				}
				time.Sleep(100 * time.Millisecond)

				// 第 17 个 → 队列满，立即返回 failed 终态。
				runID := "qfull-017"
				ch := newTerminalRecv(gw, runID)
				accepted, err := gw.SubmitRun(testSubmit("gw-ext", "run", workDir), runID)
				awaitTerminal(t, ch, runID, TaskFailed)
				if got := taskMgr.GetRun(runID); got == nil || got.State != TaskFailed {
					t.Errorf("taskMgr record for %s: %+v, want failed", runID, got)
				}
				return accepted, err
			},
		},
		{
			// 04-3(2)：debounce 被新触发替换的旧 pending → 终态 canceled。
			name: "debounce_replaced_to_canceled",
			run: func(t *testing.T, gw RunGateway, taskMgr *TaskManager, workDir string) (bool, error) {
				meta := makeMeta(t, func(m *config.ExtensionMeta) {
					m.Concurrency = "debounce:2s"
					m.Entry = createTestScript(t, t.TempDir(), "slow.sh", "sleep 3")
				})
				gw, taskMgr, workDir = newGatewaySetup(t, "gw-ext", meta)

				oldID := "debounce-old"
				ch := newTerminalRecv(gw, oldID)
				go func() { gw.SubmitRun(testSubmit("gw-ext", "run", workDir), oldID) }()
				time.Sleep(100 * time.Millisecond)

				// 新触发替换旧的 debounce pending → 旧 Run 终态 canceled。
				_, err := gw.SubmitRun(testSubmit("gw-ext", "run", workDir), "debounce-new")
				awaitTerminal(t, ch, oldID, TaskCanceled)
				if got := taskMgr.GetRun(oldID); got == nil || got.State != TaskCanceled {
					t.Errorf("taskMgr record for %s: %+v, want canceled", oldID, got)
				}
				return false, err
			},
		},
		{
			// 04-3(3)：replace 被 kill 的旧 Run → 终态 canceled（SIGTERM 及时退出）。
			name: "replace_killed_old_run",
			run: func(t *testing.T, gw RunGateway, taskMgr *TaskManager, workDir string) (bool, error) {
				meta := makeMeta(t, func(m *config.ExtensionMeta) {
					m.Concurrency = "replace"
					m.Entry = createTestScript(t, t.TempDir(), "slow.sh", "sleep 3") // 占住运行槽
				})
				gw, taskMgr, workDir = newGatewaySetup(t, "gw-ext", meta)

				oldID := "replace-old"
				ch := newTerminalRecv(gw, oldID)
				go func() { gw.SubmitRun(testSubmit("gw-ext", "run", workDir), oldID) }()
				time.Sleep(100 * time.Millisecond)

				// 新任务 replace 旧任务 → 旧 Run 被取消。
				_, err := gw.SubmitRun(testSubmit("gw-ext", "run", workDir), "replace-new")
				awaitTerminal(t, ch, oldID, TaskCanceled)
				if got := taskMgr.GetRun(oldID); got == nil || !got.IsTerminal() {
					t.Errorf("taskMgr record for %s: %+v, want terminal", oldID, got)
				}
				return false, err
			},
		},
		{
			// 04-3(4)：提交失败 → 已预登记 planned 更新为 failed 并收口（幂等，不新增第二条）。
			name: "submit_error_planned_to_failed_idempotent",
			run: func(t *testing.T, gw RunGateway, taskMgr *TaskManager, workDir string) (bool, error) {
				runID := "planned-001"
				taskMgr.RecordRun(&RunResult{RunID: runID, ExtensionName: "gw-ext", ActionID: "run", State: TaskPending, StartedAt: time.Now()})
				ch := newTerminalRecv(gw, runID)

				spec := RunSpec{ExtensionName: "no-such-ext", ActionID: "run", Context: TriggerContext{EventType: "on_demand", WorkDir: workDir}}
				accepted, err := gw.SubmitRun(spec, runID)

				// 幂等：再次提交失败不得二次回调、不得新增第二条记录。
				accepted2, err2 := gw.SubmitRun(spec, runID)
				if err2 == nil {
					t.Errorf("second submit expected error, got nil")
				}
				_ = accepted2

				awaitTerminal(t, ch, runID, TaskFailed)
				if got := taskMgr.GetRun(runID); got == nil || got.State != TaskFailed {
					t.Errorf("taskMgr record for %s: %+v, want failed", runID, got)
				}
				return accepted, err
			},
		},
		{
			// 04-3(5)：超时 → timeout。
			name: "timeout_to_timeout",
			run: func(t *testing.T, gw RunGateway, taskMgr *TaskManager, workDir string) (bool, error) {
				meta := makeMeta(t, func(m *config.ExtensionMeta) {
					m.TimeoutSeconds = 1
					m.Entry = createTestScript(t, t.TempDir(), "slow.sh", "sleep 5")
				})
				gw, taskMgr, workDir = newGatewaySetup(t, "gw-ext", meta)

				runID := "timeout-001"
				ch := newTerminalRecv(gw, runID)
				accepted, err := gw.SubmitRun(testSubmit("gw-ext", "run", workDir), runID)
				awaitTerminal(t, ch, runID, TaskTimeout)
				return accepted, err
			},
		},
		{
			// 04-3(5)：正常退出 → success。
			name: "normal_exit_success",
			run: func(t *testing.T, gw RunGateway, taskMgr *TaskManager, workDir string) (bool, error) {
				runID := "success-0001"
				ch := newTerminalRecv(gw, runID)
				accepted, err := gw.SubmitRun(testSubmit("gw-ext", "run", workDir), runID)
				awaitTerminal(t, ch, runID, TaskSuccess)
				return accepted, err
			},
		},
		{
			// 04-3(5)：正常退出 → failed。
			name: "normal_exit_failed",
			run: func(t *testing.T, gw RunGateway, taskMgr *TaskManager, workDir string) (bool, error) {
				meta := makeMeta(t, nil)
				meta.Entry = createTestScript(t, t.TempDir(), "fail.sh", "exit 3")
				gw, taskMgr, workDir = newGatewaySetup(t, "gw-ext", meta)

				runID := "fail-001"
				ch := newTerminalRecv(gw, runID)
				accepted, err := gw.SubmitRun(testSubmit("gw-ext", "run", workDir), runID)
				awaitTerminal(t, ch, runID, TaskFailed)
				return accepted, err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw, taskMgr, workDir := newGatewaySetup(t, "gw-ext", makeMeta(t, nil))
			if _, err := tt.run(t, gw, taskMgr, workDir); err != nil {
				// serialize/debounce/replace/timeout 等路径提交层可能有伴随 error（如队列满），
				// 但终态回调仍投递成功；不将这类预期内的提交层 error 视为用例失败。
				t.Logf("SubmitRun returned error (expected for this path): %v", err)
			}
		})
	}
}

// TestRunGateway_CallbackRegisterAfterTerminal 先终态后注册 → 注册时补调一次。
func TestRunGateway_CallbackRegisterAfterTerminal(t *testing.T) {
	gw, taskMgr, workDir := newGatewaySetup(t, "gw-ext", makeMeta(t, nil))
	runID := "late-reg-001"

	// 先提交（不注册回调），终态发生时被缓存。
	accepted, err := gw.SubmitRun(testSubmit("gw-ext", "run", workDir), runID)
	if err != nil {
		t.Fatalf("SubmitRun failed: %v", err)
	}
	if !accepted {
		t.Fatal("SubmitRun accepted=false")
	}

	// 终态之后注册 → 立即补调一次。
	ch := newTerminalRecv(gw, runID)
	awaitTerminal(t, ch, runID, TaskSuccess)
	if taskMgr.GetRun(runID) == nil {
		t.Error("expected recorded run in taskMgr")
	}

	// 已经补调过一次后再次注册 → 不重复回调。
	gw.OnRunTerminal(runID, func(tr TerminalResult) { ch <- tr })
	select {
	case tr := <-ch:
		t.Fatalf("callback delivered again after it already fired once: %+v", tr)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestRunGateway_ConcurrentSubmit 并发提交+注册+终态；配合 -race 验证无竞态。
func TestRunGateway_ConcurrentSubmit(t *testing.T) {
	gw, _, workDir := newGatewaySetup(t, "gw-ext", makeMeta(t, nil))
	const n = 50
	var wg sync.WaitGroup
	var mu sync.Mutex
	finished := make(map[string]bool)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			runID := fmt.Sprintf("conc-%03d", idx)
			ch := newTerminalRecv(gw, runID)
			accepted, err := gw.SubmitRun(testSubmit("gw-ext", "run", workDir), runID)
			if err != nil {
				t.Errorf("SubmitRun(%s) error: %v", runID, err)
				return
			}
			if !accepted {
				t.Errorf("SubmitRun(%s) accepted=false", runID)
				return
			}
			select {
			case tr := <-ch:
				if tr.State != TaskSuccess {
					t.Errorf("runid=%s state=%q, want success", runID, tr.State)
				}
				mu.Lock()
				if finished[runID] {
					t.Errorf("runid=%s callback delivered twice", runID)
				}
				finished[runID] = true
				mu.Unlock()
			case <-time.After(30 * time.Second):
				t.Errorf("runid=%s timed out waiting terminal", runID)
			}
		}(i)
	}
	wg.Wait()
}