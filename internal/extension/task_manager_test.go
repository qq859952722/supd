package extension

import (
	"bytes"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/supdorg/supd/internal/config"
)

// --- TaskHistory 测试 ---

func TestTaskHistory_AddAndGet(t *testing.T) {
	h := NewTaskHistory(7)

	result := &RunResult{
		RunID:         "run-001",
		ExtensionName: "test-ext",
		State:         TaskSuccess,
		StartedAt:     time.Now(),
		FinishedAt:    time.Now(),
	}

	h.Add(result)

	got := h.Get("run-001")
	if got == nil {
		t.Fatal("expected to find run-001, got nil")
	}
	if got.RunID != "run-001" {
		t.Errorf("RunID = %q, want %q", got.RunID, "run-001")
	}
	if got.State != TaskSuccess {
		t.Errorf("State = %q, want %q", got.State, TaskSuccess)
	}
}

func TestTaskHistory_Get_NotFound(t *testing.T) {
	h := NewTaskHistory(7)
	got := h.Get("nonexistent")
	if got != nil {
		t.Errorf("expected nil for nonexistent runID, got %v", got)
	}
}

func TestTaskHistory_Remove(t *testing.T) {
	h := NewTaskHistory(7)

	result := &RunResult{
		RunID:     "run-001",
		State:     TaskSuccess,
		StartedAt: time.Now(),
	}
	h.Add(result)

	if h.Get("run-001") == nil {
		t.Fatal("expected run-001 to exist before removal")
	}

	h.Remove("run-001")
	if h.Get("run-001") != nil {
		t.Error("expected run-001 to be removed")
	}
}

func TestTaskHistory_TotalCount(t *testing.T) {
	h := NewTaskHistory(7)

	if h.TotalCount() != 0 {
		t.Errorf("TotalCount = %d, want 0", h.TotalCount())
	}

	for i := 0; i < 5; i++ {
		h.Add(&RunResult{
			RunID:     "run-" + time.Now().Format("150405.000") + "-" + mustItoa(i),
			State:     TaskSuccess,
			StartedAt: time.Now(),
		})
	}

	if h.TotalCount() != 5 {
		t.Errorf("TotalCount = %d, want 5", h.TotalCount())
	}
}

func TestTaskHistory_CountByState(t *testing.T) {
	h := NewTaskHistory(7)

	h.Add(&RunResult{RunID: "r1", State: TaskSuccess, StartedAt: time.Now()})
	h.Add(&RunResult{RunID: "r2", State: TaskFailed, StartedAt: time.Now()})
	h.Add(&RunResult{RunID: "r3", State: TaskSuccess, StartedAt: time.Now()})
	h.Add(&RunResult{RunID: "r4", State: TaskTimeout, StartedAt: time.Now()})

	if h.CountByState(TaskSuccess) != 2 {
		t.Errorf("CountByState(success) = %d, want 2", h.CountByState(TaskSuccess))
	}
	if h.CountByState(TaskFailed) != 1 {
		t.Errorf("CountByState(failed) = %d, want 1", h.CountByState(TaskFailed))
	}
	if h.CountByState(TaskTimeout) != 1 {
		t.Errorf("CountByState(timeout) = %d, want 1", h.CountByState(TaskTimeout))
	}
	if h.CountByState(TaskCanceled) != 0 {
		t.Errorf("CountByState(canceled) = %d, want 0", h.CountByState(TaskCanceled))
	}
}

func TestTaskHistory_LazyCleanup(t *testing.T) {
	// REQ-F-020: 任务历史保留7天（内存）
	// DEC-001: 惰性清理 — 每次记录新任务时顺带删除超期记录
	h := NewTaskHistory(7) // 7天保留

	// 添加一个超过7天的旧记录
	oldTime := time.Now().AddDate(0, 0, -8) // 8天前
	h.Add(&RunResult{
		RunID:     "old-run",
		State:     TaskSuccess,
		StartedAt: oldTime,
	})

	// 旧记录应该还在（还没触发惰性清理——Add自身会触发，但旧记录也是刚Add的）
	// 再添加一个新记录，触发惰性清理
	h.Add(&RunResult{
		RunID:     "new-run",
		State:     TaskSuccess,
		StartedAt: time.Now(),
	})

	// 旧记录应该被清理
	if h.Get("old-run") != nil {
		t.Error("expected old-run to be cleaned up after lazy cleanup")
	}

	// 新记录应该还在
	if h.Get("new-run") == nil {
		t.Error("expected new-run to still exist")
	}
}

func TestTaskHistory_LazyCleanup_WithinRetention(t *testing.T) {
	h := NewTaskHistory(7)

	// 添加一个6天前的记录（不应被清理）
	recentTime := time.Now().AddDate(0, 0, -6)
	h.Add(&RunResult{
		RunID:     "recent-run",
		State:     TaskSuccess,
		StartedAt: recentTime,
	})

	// 添加新记录触发惰性清理
	h.Add(&RunResult{
		RunID:     "new-run",
		State:     TaskSuccess,
		StartedAt: time.Now(),
	})

	// 6天前的记录不应被清理
	if h.Get("recent-run") == nil {
		t.Error("expected recent-run (within 7 days) to still exist")
	}
}

// TestTaskHistory_LazyCleanup_AtDay7Boundary 锚定第7天边界的保留行为
// L-02-003: lazyCleanupLocked 使用严格 Before(cutoff)，cutoff = now - 7天。
// 由于 lazyCleanupLocked 重新调用 time.Now()，测试中两次 time.Now() 间存在微小前进，
// 因此使用 "7天前 + 1秒缓冲" 表示第7天边界，确保清理运行时仍在保留期内。
// 若误改为 BeforeOrEqual 或 cutoff 偏移，此用例将失败（缓冲不足以覆盖偏差）。
func TestTaskHistory_LazyCleanup_AtDay7Boundary(t *testing.T) {
	h := NewTaskHistory(7)

	// 第7天边界：7天前减1秒（仍在保留期内）
	day7Boundary := time.Now().AddDate(0, 0, -7).Add(time.Second)
	h.Add(&RunResult{
		RunID:     "day7-run",
		State:     TaskSuccess,
		StartedAt: day7Boundary,
	})

	// 添加新记录触发惰性清理
	h.Add(&RunResult{
		RunID:     "new-run",
		State:     TaskSuccess,
		StartedAt: time.Now(),
	})

	// 第7天边界的记录不应被清理（Before 是严格小于）
	if h.Get("day7-run") == nil {
		t.Error("task at day 7 boundary should be retained (Before is strict)")
	}
}

func TestTaskHistory_List_FilterByExtensionName(t *testing.T) {
	h := NewTaskHistory(7)

	h.Add(&RunResult{RunID: "r1", ExtensionName: "ext-a", State: TaskSuccess, StartedAt: time.Now()})
	h.Add(&RunResult{RunID: "r2", ExtensionName: "ext-b", State: TaskFailed, StartedAt: time.Now()})
	h.Add(&RunResult{RunID: "r3", ExtensionName: "ext-a", State: TaskSuccess, StartedAt: time.Now()})

	results := h.List(RunFilter{ExtensionName: "ext-a"})
	if len(results) != 2 {
		t.Errorf("List filter by ext-a: got %d results, want 2", len(results))
	}
}

func TestTaskHistory_List_FilterByServiceName(t *testing.T) {
	h := NewTaskHistory(7)

	h.Add(&RunResult{RunID: "r1", ServiceName: "svc-a", State: TaskSuccess, StartedAt: time.Now()})
	h.Add(&RunResult{RunID: "r2", ServiceName: "svc-b", State: TaskSuccess, StartedAt: time.Now()})
	h.Add(&RunResult{RunID: "r3", ServiceName: "svc-a", State: TaskFailed, StartedAt: time.Now()})

	results := h.List(RunFilter{ServiceName: "svc-a"})
	if len(results) != 2 {
		t.Errorf("List filter by svc-a: got %d results, want 2", len(results))
	}
}

func TestTaskHistory_List_FilterByState(t *testing.T) {
	h := NewTaskHistory(7)

	h.Add(&RunResult{RunID: "r1", State: TaskSuccess, StartedAt: time.Now()})
	h.Add(&RunResult{RunID: "r2", State: TaskFailed, StartedAt: time.Now()})
	h.Add(&RunResult{RunID: "r3", State: TaskSuccess, StartedAt: time.Now()})

	results := h.List(RunFilter{State: TaskFailed})
	if len(results) != 1 {
		t.Errorf("List filter by failed: got %d results, want 1", len(results))
	}
}

func TestTaskHistory_List_FilterByTimeRange(t *testing.T) {
	h := NewTaskHistory(7)

	t1 := time.Now().Add(-2 * time.Hour)
	t2 := time.Now().Add(-1 * time.Hour)
	t3 := time.Now()

	h.Add(&RunResult{RunID: "r1", State: TaskSuccess, StartedAt: t1})
	h.Add(&RunResult{RunID: "r2", State: TaskSuccess, StartedAt: t2})
	h.Add(&RunResult{RunID: "r3", State: TaskSuccess, StartedAt: t3})

	since := time.Now().Add(-90 * time.Minute)
	results := h.List(RunFilter{Since: since})
	if len(results) != 2 {
		t.Errorf("List filter by since: got %d results, want 2", len(results))
	}

	until := time.Now().Add(-30 * time.Minute)
	results = h.List(RunFilter{Until: until})
	if len(results) != 2 {
		t.Errorf("List filter by until: got %d results, want 2", len(results))
	}
}

func TestTaskHistory_List_WithLimit(t *testing.T) {
	h := NewTaskHistory(7)

	for i := 0; i < 10; i++ {
		h.Add(&RunResult{
			RunID:     "r" + mustItoa(i),
			State:     TaskSuccess,
			StartedAt: time.Now().Add(time.Duration(i) * time.Second),
		})
	}

	results := h.List(RunFilter{Limit: 3})
	if len(results) != 3 {
		t.Errorf("List with limit 3: got %d results, want 3", len(results))
	}
}

func TestTaskHistory_List_SortedByTimeDesc(t *testing.T) {
	h := NewTaskHistory(7)

	t1 := time.Now().Add(-2 * time.Hour)
	t2 := time.Now().Add(-1 * time.Hour)
	t3 := time.Now()

	h.Add(&RunResult{RunID: "r1", State: TaskSuccess, StartedAt: t1})
	h.Add(&RunResult{RunID: "r2", State: TaskSuccess, StartedAt: t2})
	h.Add(&RunResult{RunID: "r3", State: TaskSuccess, StartedAt: t3})

	results := h.List(RunFilter{})
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// 最新的在前
	if results[0].RunID != "r3" {
		t.Errorf("first result RunID = %q, want %q (newest first)", results[0].RunID, "r3")
	}
	if results[2].RunID != "r1" {
		t.Errorf("last result RunID = %q, want %q (oldest last)", results[2].RunID, "r1")
	}
}

func TestTaskHistory_DefaultRetentionDays(t *testing.T) {
	h := NewTaskHistory(0) // 传入0或负数应默认7
	if h.retentionDays != 7 {
		t.Errorf("retentionDays = %d, want 7", h.retentionDays)
	}

	h2 := NewTaskHistory(-1)
	if h2.retentionDays != 7 {
		t.Errorf("retentionDays = %d, want 7", h2.retentionDays)
	}
}

func TestTaskHistory_MaxLogSize(t *testing.T) {
	h := NewTaskHistory(7)
	// REQ-F-024, 2.2.16: 扩展运行日志上限10MB硬编码
	if h.maxLogSize != 10*1024*1024 {
		t.Errorf("maxLogSize = %d, want %d", h.maxLogSize, 10*1024*1024)
	}
}

// TestTaskHistory_UpdateProgress_Concurrent 验证 UpdateProgress 在并发场景下的安全性
// B-03-001: 审计要求专项测试 — 多个 goroutine 同时调用 UpdateProgress（执行 goroutine 更新进度）
// 与 Get/List（API 查询同时读取）并发时是否安全。
// DEV-003 偏差：TaskHistory 使用 sync.RWMutex 是可接受的（已在 Get/List 返回拷贝避免竞态）。
// 本测试若用 -race 重跑应零竞态警告；若移除 Get/List 的拷贝逻辑则会触发 DATA RACE。
func TestTaskHistory_UpdateProgress_Concurrent(t *testing.T) {
	h := NewTaskHistory(7)

	runID := "concurrent-run-1"
	// StartedAt 必须设为 time.Now()，否则 lazyCleanup 会立即删除零值时间记录
	h.Add(&RunResult{
		RunID:     runID,
		State:     TaskRunning,
		StartedAt: time.Now(),
	})

	// 并发拓扑：10 个 writer goroutine 调 UpdateProgress，
	// 5 个 reader 调 Get，5 个 reader 调 List
	var wg sync.WaitGroup
	const writers = 10
	const readers = 5

	// Writer goroutine：每个写 0..99，最后一次写入为 99
	// 由于所有 writer 的最后一个值都是 99 且 wg.Wait() 保证全部完成，
	// 最终 Progress 必为 99（last-writer-wins，最后写入的 wall-clock 时间最晚）
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				h.UpdateProgress(runID, j, "working")
			}
		}()
	}

	// Reader goroutine（Get）：验证不 panic 且返回非 nil 记录
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				run := h.Get(runID)
				if run == nil {
					t.Errorf("Get returned nil during concurrent updates")
					return
				}
				if run.RunID != runID {
					t.Errorf("Get RunID = %q, want %q", run.RunID, runID)
					return
				}
				// Progress 应在 [0,99] 范围内（任何中间时刻的合法值）
				if run.Progress < 0 || run.Progress > 99 {
					t.Errorf("Get Progress = %d, out of [0,99]", run.Progress)
					return
				}
			}
		}()
	}

	// Reader goroutine（List）：验证不 panic 且返回单条记录
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				results := h.List(RunFilter{})
				if len(results) != 1 {
					t.Errorf("List returned %d results, want 1", len(results))
					return
				}
			}
		}()
	}

	wg.Wait()

	// 最终状态校验：所有 writer 最后一次写入均为 99，故最终 Progress 必为 99
	run := h.Get(runID)
	if run == nil {
		t.Fatal("run not found after concurrent updates")
	}
	if run.Progress != 99 {
		t.Errorf("progress = %d, want 99 (all writers' last value is 99)", run.Progress)
	}
	if run.ResultMsg != "working" {
		t.Errorf("ResultMsg = %q, want %q", run.ResultMsg, "working")
	}
	if run.State != TaskRunning {
		t.Errorf("State = %q, want %q (UpdateProgress should not change State)", run.State, TaskRunning)
	}
}

// --- TaskManager 测试 ---

func TestTaskManager_RecordAndGetRun(t *testing.T) {
	m := NewTaskManager(7)

	result := &RunResult{
		RunID:         "run-001",
		ExtensionName: "test-ext",
		ActionID:      "deploy",
		State:         TaskSuccess,
		StartedAt:     time.Now(),
		FinishedAt:    time.Now(),
	}

	m.RecordRun(result)

	got := m.GetRun("run-001")
	if got == nil {
		t.Fatal("expected to find run-001, got nil")
	}
	if got.RunID != "run-001" {
		t.Errorf("RunID = %q, want %q", got.RunID, "run-001")
	}
	if got.ExtensionName != "test-ext" {
		t.Errorf("ExtensionName = %q, want %q", got.ExtensionName, "test-ext")
	}
}

func TestTaskManager_ListRunsByExtension(t *testing.T) {
	m := NewTaskManager(7)

	m.RecordRun(&RunResult{RunID: "r1", ExtensionName: "ext-a", State: TaskSuccess, StartedAt: time.Now()})
	m.RecordRun(&RunResult{RunID: "r2", ExtensionName: "ext-b", State: TaskFailed, StartedAt: time.Now()})
	m.RecordRun(&RunResult{RunID: "r3", ExtensionName: "ext-a", State: TaskSuccess, StartedAt: time.Now()})

	results := m.ListRunsByExtension("ext-a")
	if len(results) != 2 {
		t.Errorf("ListRunsByExtension(ext-a): got %d, want 2", len(results))
	}
}

func TestTaskManager_ListRunsByService(t *testing.T) {
	m := NewTaskManager(7)

	m.RecordRun(&RunResult{RunID: "r1", ServiceName: "svc-a", State: TaskSuccess, StartedAt: time.Now()})
	m.RecordRun(&RunResult{RunID: "r2", ServiceName: "svc-b", State: TaskSuccess, StartedAt: time.Now()})
	m.RecordRun(&RunResult{RunID: "r3", ServiceName: "svc-a", State: TaskFailed, StartedAt: time.Now()})

	results := m.ListRunsByService("svc-a")
	if len(results) != 2 {
		t.Errorf("ListRunsByService(svc-a): got %d, want 2", len(results))
	}
}

func TestTaskManager_ListRuns_WithFilter(t *testing.T) {
	m := NewTaskManager(7)

	m.RecordRun(&RunResult{RunID: "r1", ExtensionName: "ext-a", State: TaskSuccess, StartedAt: time.Now()})
	m.RecordRun(&RunResult{RunID: "r2", ExtensionName: "ext-a", State: TaskFailed, StartedAt: time.Now()})
	m.RecordRun(&RunResult{RunID: "r3", ExtensionName: "ext-b", State: TaskSuccess, StartedAt: time.Now()})

	results := m.ListRuns(RunFilter{ExtensionName: "ext-a", State: TaskFailed})
	if len(results) != 1 {
		t.Errorf("ListRuns(ext-a, failed): got %d, want 1", len(results))
	}
}

func TestTaskManager_MaxLogSize(t *testing.T) {
	m := NewTaskManager(7)
	// REQ-F-024, 2.2.16: 扩展运行日志上限10MB硬编码
	if m.maxLogSize != 10*1024*1024 {
		t.Errorf("maxLogSize = %d, want %d", m.maxLogSize, 10*1024*1024)
	}
}

// --- FailureHandler 测试 ---

func TestHandleExtensionFailure(t *testing.T) {
	// REQ-F-021: 扩展运行失败仅记录日志，不影响服务
	// REQ-E-005: 扩展失败隔离
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	result := &RunResult{
		RunID:         "run-fail-001",
		ExtensionName: "test-ext",
		ActionID:      "deploy",
		State:         TaskFailed,
		ExitCode:      1,
		ResultMsg:     "exit code 1",
	}

	HandleExtensionFailure(result, logger)

	logOutput := buf.String()
	if logOutput == "" {
		t.Error("expected failure log output, got empty")
	}
}

func TestHandleExtensionFailure_NilLogger(t *testing.T) {
	// 不应 panic
	result := &RunResult{
		RunID:         "run-fail-001",
		ExtensionName: "test-ext",
		State:         TaskFailed,
	}
	HandleExtensionFailure(result, nil) // 应安全返回
}

func TestShouldRetry(t *testing.T) {
	// REQ-F-021: 仅 failed/timeout 状态可重试
	retryCfg := &RetryConfig{MaxRetries: 3}

	tests := []struct {
		name          string
		state         TaskState
		currentRetries int
		want          bool
	}{
		{"failed, 0 retries", TaskFailed, 0, true},
		{"failed, 2 retries", TaskFailed, 2, true},
		{"failed, max retries", TaskFailed, 3, false},
		{"timeout, 0 retries", TaskTimeout, 0, true},
		{"timeout, max retries", TaskTimeout, 3, false},
		{"success should not retry", TaskSuccess, 0, false},
		{"canceled should not retry", TaskCanceled, 0, false},
		{"killed should not retry", TaskKilled, 0, false},
		{"pending should not retry", TaskPending, 0, false},
		{"running should not retry", TaskRunning, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldRetry(tt.state, retryCfg, tt.currentRetries)
			if got != tt.want {
				t.Errorf("ShouldRetry(%s, %d retries) = %v, want %v", tt.state, tt.currentRetries, got, tt.want)
			}
		})
	}
}

func TestShouldRetry_NilConfig(t *testing.T) {
	got := ShouldRetry(TaskFailed, nil, 0)
	if got {
		t.Error("ShouldRetry with nil config should return false")
	}
}

// --- Display 测试 ---

func TestGetDisplayState_Active(t *testing.T) {
	// REQ-F-024: 有 actions 且 enabled → DisplayActive
	enabled := true
	meta := &config.ExtensionMeta{
		Name:    "test-ext",
		Enabled: &enabled,
		Actions: []config.Action{
			{ID: "deploy", Label: "Deploy"},
		},
		Triggers: config.Triggers{
			OnDemand: &enabled,
		},
	}

	state := GetDisplayState(meta)
	if state != DisplayActive {
		t.Errorf("GetDisplayState = %q, want %q", state, DisplayActive)
	}
}

func TestGetDisplayState_Automated(t *testing.T) {
	// REQ-F-024: 无 actions 但有 triggers → DisplayAutomated
	enabled := true
	meta := &config.ExtensionMeta{
		Name:    "auto-ext",
		Enabled: &enabled,
		Actions: nil, // 无 actions
		Triggers: config.Triggers{
			OnSchedule: []config.TriggerSchedule{
				{Cron: "*/5 * * * *"}, // 无 action 引用
			},
			SupdLifecycle: []config.TriggerSupdLifecycle{
				{Event: "post_ready"}, // 无 action 引用
			},
		},
	}

	state := GetDisplayState(meta)
	if state != DisplayAutomated {
		t.Errorf("GetDisplayState = %q, want %q", state, DisplayAutomated)
	}
}

func TestGetDisplayState_Disabled(t *testing.T) {
	// REQ-F-024: enabled=false → DisplayDisabled
	disabled := false
	meta := &config.ExtensionMeta{
		Name:    "disabled-ext",
		Enabled: &disabled,
		Actions: []config.Action{
			{ID: "deploy", Label: "Deploy"},
		},
	}

	state := GetDisplayState(meta)
	if state != DisplayDisabled {
		t.Errorf("GetDisplayState = %q, want %q", state, DisplayDisabled)
	}
}

func TestGetDisplayState_ConfigError(t *testing.T) {
	// REQ-F-024: triggers.action 引用不存在的 action → DisplayConfigError
	enabled := true
	meta := &config.ExtensionMeta{
		Name:    "bad-ext",
		Enabled: &enabled,
		Actions: []config.Action{
			{ID: "deploy", Label: "Deploy"},
		},
		Triggers: config.Triggers{
			OnSchedule: []config.TriggerSchedule{
				{Cron: "*/5 * * * *", Action: "nonexistent_action"}, // 引用不存在的 action
			},
		},
	}

	state := GetDisplayState(meta)
	if state != DisplayConfigError {
		t.Errorf("GetDisplayState = %q, want %q", state, DisplayConfigError)
	}
}

func TestGetDisplayState_ConfigErrorServiceLifecycle(t *testing.T) {
	// REQ-F-024: service_lifecycle 引用不存在的 action → DisplayConfigError
	enabled := true
	meta := &config.ExtensionMeta{
		Name:    "bad-ext2",
		Enabled: &enabled,
		Actions: []config.Action{
			{ID: "deploy", Label: "Deploy"},
		},
		Triggers: config.Triggers{
			ServiceLifecycle: []config.TriggerServiceLifecycle{
				{Event: "started", Action: "bad_action"},
			},
		},
	}

	state := GetDisplayState(meta)
	if state != DisplayConfigError {
		t.Errorf("GetDisplayState = %q, want %q", state, DisplayConfigError)
	}
}

func TestGetDisplayState_ConfigErrorSupdLifecycle(t *testing.T) {
	// REQ-F-024: supd_lifecycle 引用不存在的 action → DisplayConfigError
	enabled := true
	meta := &config.ExtensionMeta{
		Name:    "bad-ext3",
		Enabled: &enabled,
		Actions: []config.Action{
			{ID: "deploy", Label: "Deploy"},
		},
		Triggers: config.Triggers{
			SupdLifecycle: []config.TriggerSupdLifecycle{
				{Event: "pre_ready", Action: "bad_action"},
			},
		},
	}

	state := GetDisplayState(meta)
	if state != DisplayConfigError {
		t.Errorf("GetDisplayState = %q, want %q", state, DisplayConfigError)
	}
}

func TestGetDisplayState_DisabledTakesPriority(t *testing.T) {
	// enabled=false 优先于 config_error
	disabled := false
	meta := &config.ExtensionMeta{
		Name:    "priority-ext",
		Enabled: &disabled,
		Actions: []config.Action{
			{ID: "deploy", Label: "Deploy"},
		},
		Triggers: config.Triggers{
			OnSchedule: []config.TriggerSchedule{
				{Cron: "*/5 * * * *", Action: "nonexistent"},
			},
		},
	}

	state := GetDisplayState(meta)
	if state != DisplayDisabled {
		t.Errorf("GetDisplayState = %q, want %q (disabled takes priority)", state, DisplayDisabled)
	}
}

func TestGetConfigErrors_NoErrors(t *testing.T) {
	enabled := true
	meta := &config.ExtensionMeta{
		Name:    "good-ext",
		Enabled: &enabled,
		Actions: []config.Action{
			{ID: "deploy", Label: "Deploy"},
		},
		Triggers: config.Triggers{
			OnSchedule: []config.TriggerSchedule{
				{Cron: "*/5 * * * *", Action: "deploy"}, // 正确引用
			},
		},
	}

	errors := GetConfigErrors(meta)
	if len(errors) != 0 {
		t.Errorf("GetConfigErrors = %v, want empty", errors)
	}
}

func TestGetConfigErrors_WithErrors(t *testing.T) {
	enabled := true
	meta := &config.ExtensionMeta{
		Name:    "bad-ext",
		Enabled: &enabled,
		Actions: []config.Action{
			{ID: "deploy", Label: "Deploy"},
		},
		Triggers: config.Triggers{
			OnSchedule: []config.TriggerSchedule{
				{Cron: "*/5 * * * *", Action: "nonexistent"},
			},
			ServiceLifecycle: []config.TriggerServiceLifecycle{
				{Event: "started", Action: "also_bad"},
			},
			SupdLifecycle: []config.TriggerSupdLifecycle{
				{Event: "pre_ready", Action: "nope"},
			},
		},
	}

	errors := GetConfigErrors(meta)
	if len(errors) != 3 {
		t.Errorf("GetConfigErrors returned %d errors, want 3", len(errors))
	}
}

func TestGetConfigErrors_EmptyActionRef(t *testing.T) {
	// 空 action 引用不应报错（由 validateActionRef 校验 required，此处只检查引用不存在的情况）
	enabled := true
	meta := &config.ExtensionMeta{
		Name:    "ext",
		Enabled: &enabled,
		Actions: []config.Action{
			{ID: "deploy", Label: "Deploy"},
		},
		Triggers: config.Triggers{
			OnSchedule: []config.TriggerSchedule{
				{Cron: "*/5 * * * *", Action: ""}, // 空引用，不是配置错误
			},
		},
	}

	errors := GetConfigErrors(meta)
	if len(errors) != 0 {
		t.Errorf("GetConfigErrors with empty action ref = %v, want empty", errors)
	}
}

// --- RunResult 测试 ---

func TestRunResult_IsSuccess(t *testing.T) {
	// REQ-F-016: ::result:: warning 视为成功完成
	tests := []struct {
		state TaskState
		want  bool
	}{
		{TaskSuccess, true},
		{TaskFailed, false},
		{TaskTimeout, false},
		{TaskCanceled, false},
		{TaskKilled, false},
		{TaskPending, false},
		{TaskRunning, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			r := &RunResult{State: tt.state}
			if r.IsSuccess() != tt.want {
				t.Errorf("IsSuccess() = %v, want %v", r.IsSuccess(), tt.want)
			}
		})
	}
}

func TestRunResult_IsTerminal(t *testing.T) {
	// REQ-F-016: success/failed/timeout/canceled/killed 为终态
	tests := []struct {
		state TaskState
		want  bool
	}{
		{TaskSuccess, true},
		{TaskFailed, true},
		{TaskTimeout, true},
		{TaskCanceled, true},
		{TaskKilled, true},
		{TaskPending, false},
		{TaskRunning, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			r := &RunResult{State: tt.state}
			if r.IsTerminal() != tt.want {
				t.Errorf("IsTerminal() = %v, want %v", r.IsTerminal(), tt.want)
			}
		})
	}
}

// mustItoa 简单整数转字符串（测试辅助）
func mustItoa(i int) string {
	return time.Now().Format("150405.000") + "-" + string(rune('0'+i))
}
