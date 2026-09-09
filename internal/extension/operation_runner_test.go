package extension

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/supdorg/supd/internal/store"
	"github.com/supdorg/supd/internal/watch"
)

// --- 测试脚手架（真 SQLite 临时库 + 真扩展脚本 + RunGateway）---

type opHarness struct {
	runner   *OperationRunner
	store    *store.Store
	registry *OperationRegistry
	disco    *watch.DiscoveryResult
	taskMgr  *TaskManager
	base     string
}

func newOpHarness(t *testing.T) *opHarness {
	base := t.TempDir()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store open: %v", err)
	}
	t.Cleanup(func() { st.Close(5 * time.Second) })
	disco := &watch.DiscoveryResult{
		Services:   map[string]*watch.ServiceEntry{},
		GlobalExts: map[string]*watch.ExtensionEntry{},
		Runtimes:   map[string]string{},
	}
	executor := NewExecutor(base, base)
	dispatcher := NewDispatcher(executor, base, base, 1800)
	taskMgr := NewTaskManager(7)
	gateway := NewRunGateway(dispatcher, taskMgr, disco)
	registry := NewOperationRegistry()
	registry.Rebuild(disco)
	runner := NewOperationRunner(gateway, st, registry)
	return &opHarness{runner: runner, store: st, registry: registry, disco: disco, taskMgr: taskMgr, base: base}
}

// addGlobalRun 添加一个全局扩展 action（声明 opID，脚本由 script 指定）。
func (h *opHarness) addGlobalRun(t *testing.T, extName, opID, script string) {
	dir := filepath.Join(h.base, "g-"+extName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	entry := createTestScript(t, dir, "run.sh", script)
	meta := testMeta(extName, entry)
	meta.Actions[0].Operations = []string{opID}
	h.disco.GlobalExts[extName] = &watch.ExtensionEntry{Name: extName, Meta: meta, ConfigPath: filepath.Join(dir, "meta.yaml")}
}

// addServiceRun 添加一个服务级扩展 action（响应 opID）。
func (h *opHarness) addServiceRun(t *testing.T, svc, extName, opID, script string) {
	dir := filepath.Join(h.base, svc+"-"+extName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	entry := createTestScript(t, dir, "run.sh", script)
	meta := testMeta(extName, entry)
	meta.Actions[0].Operations = []string{opID}
	if h.disco.Services[svc] == nil {
		h.disco.Services[svc] = &watch.ServiceEntry{Name: svc, Extensions: map[string]*watch.ExtensionEntry{}}
	}
	h.disco.Services[svc].Extensions[extName] = &watch.ExtensionEntry{Name: extName, Meta: meta, ConfigPath: filepath.Join(dir, "meta.yaml"), ServiceName: svc}
}

// rebuild 重建注册表，使新增的扩展生效。
func (h *opHarness) rebuild() { h.registry.Rebuild(h.disco) }

func markerScript(marker, logFile string) string {
	return fmt.Sprintf(`echo '%s' >> %q`, marker, logFile)
}

func failScript(marker, logFile string) string {
	return markerScript(marker, logFile) + "\nexit 3"
}

func startEndScript(marker, logFile string) string {
	return fmt.Sprintf(`echo 'S%s' >> %q
sleep 0.3
echo 'E%s' >> %q`, marker, logFile, marker, logFile)
}

// envDumpScript 打印 SUPD_OPERATION 与 SUPD_OPERATION_EXECUTION_ID（用于并发隔离断言）。
func envDumpScript(logFile string) string {
	return fmt.Sprintf(`echo "$SUPD_OPERATION:$SUPD_OPERATION_EXECUTION_ID" >> %q`, logFile)
}

func readLogLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func waitExecFinished(t *testing.T, h *opHarness, execID string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		det, err := h.store.GetExecution(context.Background(), execID)
		if err == nil && det != nil && det.FinishedAt != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("execution %s did not finish in time", execID)
}

// --- 13 项集成测试 ---

// 1. TestTwoPhaseOrder 全局顺序→服务阶段；全局 action 执行次数恰为 1。
func TestTwoPhaseOrder(t *testing.T) {
	h := newOpHarness(t)
	log := filepath.Join(h.base, "order.log")
	h.addGlobalRun(t, "g2-ext", "op1", markerScript("g2", log))
	h.addGlobalRun(t, "g1-ext", "op1", markerScript("g1", log))
	h.addServiceRun(t, "s2", "s2-ext", "op1", markerScript("s2a", log))
	h.addServiceRun(t, "s1", "s1-ext", "op1", markerScript("s1a", log))
	h.rebuild()

	_, err := h.runner.RunSynchronous(context.Background(), "op1", nil)
	if err != nil {
		t.Fatalf("RunSynchronous: %v", err)
	}
	lines := readLogLines(t, log)
	if len(lines) < 4 {
		t.Fatalf("log lines = %d, want >=4; got %v", len(lines), lines)
	}
	// 全局按 extension_name 稳定序：g1-ext → g2-ext。
	if lines[0] != "g1" || lines[1] != "g2" {
		t.Errorf("global order wrong: %v", lines)
	}
	// 全局 action 执行次数恰为 1。
	if count(lines, "g1") != 1 || count(lines, "g2") != 1 {
		t.Errorf("global runs not exactly once: %v", lines)
	}
	// 服务阶段在全局之后。
	if idxOf(lines, "s1a") < idxOf(lines, "g2") || idxOf(lines, "s2a") < idxOf(lines, "g2") {
		t.Errorf("service runs should start after global phase: %v", lines)
	}
}

func count(s []string, v string) int {
	n := 0
	for _, x := range s {
		if x == v {
			n++
		}
	}
	return n
}

func idxOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

// 2. TestGlobalFailureContinues 全局失败 → 后续全局与服务阶段仍执行（方案 A）。
func TestGlobalFailureContinues(t *testing.T) {
	h := newOpHarness(t)
	log := filepath.Join(h.base, "gf.log")
	h.addGlobalRun(t, "g1-ext", "op1", failScript("g1f", log)) // 失败
	h.addGlobalRun(t, "g2-ext", "op1", markerScript("g2", log))
	h.addServiceRun(t, "s1", "s1-ext", "op1", markerScript("s1", log))
	h.rebuild()

	if _, err := h.runner.RunSynchronous(context.Background(), "op1", nil); err != nil {
		t.Fatalf("RunSynchronous: %v", err)
	}
	lines := readLogLines(t, log)
	if count(lines, "g2") != 1 {
		t.Errorf("g2 should still run after g1 failure: %v", lines)
	}
	if idxOf(lines, "s1") < 0 {
		t.Errorf("service phase should run after global failures: %v", lines)
	}
}

// 3. TestServiceFailureIsolated 服务 A 失败不影响服务 B。
func TestServiceFailureIsolated(t *testing.T) {
	h := newOpHarness(t)
	log := filepath.Join(h.base, "sf.log")
	h.addGlobalRun(t, "g-ext", "op1", markerScript("g", log))
	h.addServiceRun(t, "sA", "sA-ext", "op1", failScript("sAf", log))
	h.addServiceRun(t, "sB", "sB-ext", "op1", markerScript("sB", log))
	h.rebuild()

	if _, err := h.runner.RunSynchronous(context.Background(), "op1", nil); err != nil {
		t.Fatalf("RunSynchronous: %v", err)
	}
	lines := readLogLines(t, log)
	if count(lines, "sB") != 1 {
		t.Errorf("service B should run despite service A failure: %v", lines)
	}
}

// 4. TestServicePhaseConcurrency4 8 个服务响应者任一时刻运行中 ≤4（有界并行）。
func TestServicePhaseConcurrency4(t *testing.T) {
	h := newOpHarness(t)
	log := filepath.Join(h.base, "cc.log")
	h.addGlobalRun(t, "g-ext", "op1", markerScript("g", log))
	for i := 0; i < 8; i++ {
		svc := fmt.Sprintf("svc%d", i)
		h.addServiceRun(t, svc, svc+"-ext", "op1", startEndScript(fmt.Sprintf("m%d", i), log))
	}
	h.rebuild()
	start := time.Now()
	if _, err := h.runner.RunSynchronous(context.Background(), "op1", nil); err != nil {
		t.Fatalf("RunSynchronous: %v", err)
	}
	lines := readLogLines(t, log)
	peak := 0
	active := 0
	starts := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "S") {
			active++
			starts++
			if active > peak {
				peak = active
			}
		} else if strings.HasPrefix(l, "E") {
			active--
		}
	}
	if starts != 8 {
		t.Errorf("service starts = %d, want 8", starts)
	}
	if peak > 4 {
		t.Errorf("peak concurrent service runs = %d, want <= 4", peak)
	}
	// 有界并行 4 → 至少 2 批。
	if elapsed := time.Since(start); elapsed < 500*time.Millisecond {
		t.Errorf("elapsed %v, want >= ~2 batches of 300ms", elapsed)
	}
}

// 5. TestSameServiceSerial 同服务 2 响应者按 extension_name→action_id 稳定序串行。
func TestSameServiceSerial(t *testing.T) {
	h := newOpHarness(t)
	log := filepath.Join(h.base, "serial.log")
	h.addGlobalRun(t, "g-ext", "op1", markerScript("g", log))
	h.addServiceRun(t, "s1", "b-ext", "op1", markerScript("sb", log))
	h.addServiceRun(t, "s1", "a-ext", "op1", markerScript("sa", log))
	h.rebuild()

	if _, err := h.runner.RunSynchronous(context.Background(), "op1", nil); err != nil {
		t.Fatalf("RunSynchronous: %v", err)
	}
	lines := readLogLines(t, log)
	// 稳定序 a-ext → b-ext，且同服务内串行（紧邻）。
	if idxOf(lines, "sa") != idxOf(lines, "sb")-1 {
		t.Errorf("same-service responders should run serially in stable order, got %v", lines)
	}
}

// 6. TestNoResponders 无响应者 → Execution + warning 通知 + 立即关闭 Topic。
func TestNoResponders(t *testing.T) {
	h := newOpHarness(t)
	// 直接注入一个无全局运行、无服务响应的操作（同包访问未导出字段）。
	h.registry.mu.Lock()
	h.registry.ops["ghost-op"] = &OperationInfo{ID: "ghost-op", Label: "Ghost", ButtonStyle: "default"}
	h.registry.mu.Unlock()

	outcome, err := h.runner.RunSynchronous(context.Background(), "ghost-op", nil)
	if err != nil {
		t.Fatalf("RunSynchronous: %v", err)
	}
	det, err := h.store.GetExecution(context.Background(), outcome.ExecutionID)
	if err != nil || det == nil {
		t.Fatalf("GetExecution: %v", err)
	}
	if det.FinishedAt == nil {
		t.Error("execution should be finished (no responders)")
	}
	topic, err := h.store.GetTopic(context.Background(), outcome.TopicID)
	if err != nil || topic == nil {
		t.Fatalf("GetTopic: %v", err)
	}
	if topic.ClosedAt == nil {
		t.Error("topic should be closed")
	}
	notifs, err := h.store.ListNotifications(context.Background(), outcome.TopicID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range notifs {
		if n.Level == "warning" && strings.Contains(n.Content, "没有服务或全局扩展响应此操作") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected system warning notification about no responders, got %+v", notifs)
	}
}

// 7. TestConcurrentOperations 两个操作并发 → executionID/topicID 不串、环境变量归属不串。
func TestConcurrentOperations(t *testing.T) {
	h := newOpHarness(t)
	log := filepath.Join(h.base, "conc.log")
	h.addGlobalRun(t, "gA-ext", "opA", envDumpScript(log))
	h.addGlobalRun(t, "gB-ext", "opB", envDumpScript(log))
	h.rebuild()

	var wg sync.WaitGroup
	var o1, o2 *TriggerOutcome
	var e1, e2 error
	wg.Add(2)
	go func() {
		defer wg.Done()
		o1, e1 = h.runner.RunSynchronous(context.Background(), "opA", []byte(`{"a":1}`))
	}()
	go func() {
		defer wg.Done()
		o2, e2 = h.runner.RunSynchronous(context.Background(), "opB", nil)
	}()
	wg.Wait()
	if e1 != nil || e2 != nil {
		t.Fatalf("concurrent runs failed: %v %v", e1, e2)
	}
	if o1.ExecutionID == o2.ExecutionID || o1.TopicID == o2.TopicID {
		t.Error("concurrent executions must have distinct executionID/topicID")
	}
	lines := readLogLines(t, log)
	if len(lines) != 2 {
		t.Fatalf("got %d env dumps, want 2: %v", len(lines), lines)
	}
	// 断言 `opA:execA` 与 `opB:execB` 各出现一次且 execA != execB（归属不串）。
	pairs := map[string]string{}
	for _, l := range lines {
		op, execID, ok := strings.Cut(l, ":")
		if !ok {
			t.Fatalf("bad env dump line: %q", l)
		}
		pairs[op] = execID
	}
	for _, op := range []string{"opA", "opB"} {
		id, ok := pairs[op]
		if !ok || id == "" {
			t.Fatalf("missing env for %s: %v", op, pairs)
		}
		if op == "opA" && id != o1.ExecutionID {
			t.Errorf("opA execution mismatch: got %s want %s", id, o1.ExecutionID)
		}
		if op == "opB" && id != o2.ExecutionID {
			t.Errorf("opB execution mismatch: got %s want %s", id, o2.ExecutionID)
		}
	}
}

// 8. TestParamsValidation 非对象 JSON/超 8KB → 错误（不建 Execution）。
func TestParamsValidation(t *testing.T) {
	h := newOpHarness(t)
	log := filepath.Join(h.base, "pv.log")
	h.addGlobalRun(t, "g-ext", "op1", markerScript("g", log))
	h.rebuild()

	// 非对象（数组）。
	if _, err := h.runner.RunSynchronous(context.Background(), "op1", []byte(`[1,2]`)); err == nil {
		t.Fatal("array params should error")
	}
	// 非 JSON。
	if _, err := h.runner.RunSynchronous(context.Background(), "op1", []byte(`{bad`)); err == nil {
		t.Fatal("invalid JSON should error")
	}
	// 超 8KB。
	big := []byte(`{"x":"` + strings.Repeat("a", operationParamsLimit) + `"}`)
	if _, err := h.runner.RunSynchronous(context.Background(), "op1", big); err == nil {
		t.Fatal("oversized params should error")
	}
	// 三次失败均不建 Execution。
	execs, err := h.store.ListExecutions(context.Background(), 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(execs) != 0 {
		t.Fatalf("invalid param requests created %d executions", len(execs))
	}
}

// 9. TestIdempotencyKey — 见 api 包 handler/provider 测试。

// 10. TestRestartInterrupt 重启恢复：未完成 Execution → interrupted_at 置位、run state 保留、不恢复子进程。
func TestRestartInterrupt(t *testing.T) {
	h := newOpHarness(t)
	svc := "s1"
	execID := uuid.New().String()
	sa := time.Now().UnixMilli()
	if _, err := h.store.CreateExecution(context.Background(), store.CreateExecutionInput{
		ExecutionID: execID, OperationID: "op", OperationLabel: "op", CreatedAt: sa,
		Runs: []store.PlannedRun{
			{RunID: "run-pending", Phase: "global", ServiceName: nil, ExtensionName: "g", ActionID: "op"},
			{RunID: "run-running", Phase: "service", ServiceName: &svc, ExtensionName: "s", ActionID: "op"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	// run-running 模拟正在运行。
	if err := h.store.UpdateRunState(context.Background(), "run-running", store.StateRunning, &sa, nil); err != nil {
		t.Fatal(err)
	}

	// 重启恢复（不恢复子进程：Runner 无恢复逻辑，仅标记中断）。
	if err := h.runner.RecoverInterruptedExecutions(context.Background()); err != nil {
		t.Fatal(err)
	}
	det, err := h.store.GetExecution(context.Background(), execID)
	if err != nil || det == nil {
		t.Fatalf("GetExecution: %v", err)
	}
	if det.InterruptedAt == nil {
		t.Fatal("execution should be marked interrupted")
	}
	// run state 保留原值（pending/running 不变）。
	states := map[string]string{}
	for _, r := range det.Runs {
		states[r.RunID] = r.State
	}
	if states["run-pending"] != store.StatePending {
		t.Errorf("pending run state changed to %q", states["run-pending"])
	}
	if states["run-running"] != store.StateRunning {
		t.Errorf("running run state changed to %q", states["run-running"])
	}
}

// 11. TestHotReloadSnapshot 执行中热重载 → 当前 Execution 用旧快照，新 Execution 用新注册。
func TestHotReloadSnapshot(t *testing.T) {
	h := newOpHarness(t)
	log := filepath.Join(h.base, "hr.log")
	h.addGlobalRun(t, "g-ext", "op1", startEndScript("hr-run", log))
	h.rebuild()

	// 异步触发旧快照执行。
	outcome, err := h.runner.StartExecution(context.Background(), "op1", nil)
	if err != nil {
		t.Fatalf("StartExecution: %v", err)
	}
	// 热重载：清空注册表。
	h.registry.Rebuild(&watch.DiscoveryResult{Services: map[string]*watch.ServiceEntry{}, GlobalExts: map[string]*watch.ExtensionEntry{}})
	// 新触发用新注册 → 操作不存在。
	if _, err := h.runner.RunSynchronous(context.Background(), "op1", nil); err == nil {
		t.Fatal("op1 should not be recognized after hot reload removed it")
	}
	// 旧 Execution 仍按旧快照执行完成（快照冻结）。
	waitExecFinished(t, h, outcome.ExecutionID)
	lines := readLogLines(t, log)
	if count(lines, "Shr-run") != 1 {
		t.Errorf("frozen-snapshot run should still execute once, got %v", lines)
	}
}

// 12. TestConcurrencyPolicyRespected 响应 action 配 replace → 第二次 replace 第一次（(service,ext,action) key）。
func TestConcurrencyPolicyRespected(t *testing.T) {
	h := newOpHarness(t)
	log := filepath.Join(h.base, "rep.log")
	h.addGlobalRun(t, "g-ext", "op1", markerScript("g", log))
	// 服务响应者 concurrency=replace，慢脚本（占住运行槽后等待被替换或完成）。
	h.addServiceRun(t, "s1", "s1-ext", "op1", "sleep 3\necho 'replaced-or-done' >> "+quote(log))
	h.rebuild()
	// 强制 replace 并发策略（testMeta 默认空→replace，此处显式声明以防默认语义变化）。
	h.disco.Services["s1"].Extensions["s1-ext"].Meta.Concurrency = "replace"

	// 第一次异步触发启动服务 run；第二次触发 replace 第一次。
	o1, err1 := h.runner.StartExecution(context.Background(), "op1", nil)
	if err1 != nil {
		t.Fatalf("first StartExecution: %v", err1)
	}
	// 略等第一次服务 run 启动后再触发第二次。
	time.Sleep(200 * time.Millisecond)
	o2, err2 := h.runner.StartExecution(context.Background(), "op1", nil)
	if err2 != nil {
		t.Fatalf("second StartExecution: %v", err2)
	}
	waitExecFinished(t, h, o1.ExecutionID)
	waitExecFinished(t, h, o2.ExecutionID)

	// 首次执行的服务 run 应被第二次 replace 为 canceled/failed 终态。
	d1, _ := h.store.GetExecution(context.Background(), o1.ExecutionID)
	var firstServiceState string
	for _, r := range d1.Runs {
		if r.Phase == "service" {
			firstServiceState = r.State
		}
	}
	if firstServiceState != store.StateCanceled && firstServiceState != store.StateFailed && firstServiceState != store.StateKilled {
		t.Errorf("first service run state = %q, want replaced (canceled/failed/killed)", firstServiceState)
	}
	d2, _ := h.store.GetExecution(context.Background(), o2.ExecutionID)
	var secondServiceState string
	for _, r := range d2.Runs {
		if r.Phase == "service" {
			secondServiceState = r.State
		}
	}
	if secondServiceState != store.StateSuccess {
		t.Errorf("second service run state = %q, want success", secondServiceState)
	}
}

func quote(s string) string { return "\"" + s + "\"" }

// 13. TestDebounceResponderWaitsTerminal 响应 action 配 debounce → 服务阶段等待收口后才完成 Execution。
func TestDebounceResponderWaitsTerminal(t *testing.T) {
	h := newOpHarness(t)
	log := filepath.Join(h.base, "deb.log")
	h.addGlobalRun(t, "g-ext", "op1", markerScript("g", log))
	h.addServiceRun(t, "s1", "s1-ext", "op1", markerScript("deb-run", log))
	h.disco.Services["s1"].Extensions["s1-ext"].Meta.Concurrency = "debounce:1s"
	h.rebuild()

	start := time.Now()
	outcome, err := h.runner.RunSynchronous(context.Background(), "op1", nil)
	if err != nil {
		t.Fatalf("RunSynchronous: %v", err)
	}
	det, err := h.store.GetExecution(context.Background(), outcome.ExecutionID)
	if err != nil || det == nil {
		t.Fatalf("GetExecution: %v", err)
	}
	// 服务阶段收口后才完成 Execution。
	if det.FinishedAt == nil {
		t.Fatal("execution should be finished after debounce settles")
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Errorf("execution finished too early (%v); debounce responder run should be awaited", elapsed)
	}
	var svcStateOK bool
	for _, r := range det.Runs {
		if r.Phase == "service" {
			switch r.State {
			case store.StateSuccess, store.StateCanceled, store.StateFailed:
				svcStateOK = true // debounce 收口后必有终态
			}
		}
	}
	if !svcStateOK {
		t.Errorf("service run did not reach a terminal state recorded: %+v", det.Runs)
	}
}