package core

import (
	"context"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// TestShutdown_NoServices 无服务时正常退出
// REQ-F-032: 优雅退出流程，无运行中服务时应立即返回
func TestShutdown_NoServices(t *testing.T) {
	pm := NewProcessManager()
	dg := NewDependencyGraph()
	sms := make(map[string]*StateMachine)
	stopCfgs := make(map[string]StopConfig)

	sc := NewShutdownCoordinator(pm, dg, sms, stopCfgs, 30)
	err := sc.GracefulShutdown(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !sc.ShutdownRequested() {
		t.Error("ShutdownRequested() should return true after GracefulShutdown")
	}
}

// TestShutdown_SingleService 单个服务正常停止
// REQ-F-032: 按依赖反序停止所有运行中的服务
func TestShutdown_SingleService(t *testing.T) {
	pm := NewProcessManager()
	dg := NewDependencyGraph()
	sms := make(map[string]*StateMachine)
	stopCfgs := make(map[string]StopConfig)

	dg.AddService("svc1", nil)
	sms["svc1"] = NewStateMachine()
	defer sms["svc1"].Close()
	stopCfgs["svc1"] = StopConfig{GraceSeconds: 5, TimeoutSeconds: 30}

	// 启动一个真实进程
	proc, err := StartProcess("svc1", []string{"sleep", "60"}, nil, "", nil)
	if err != nil {
		t.Fatalf("StartProcess failed: %v", err)
	}
	pm.Register("svc1", proc)

	// 状态机转移到up
	sms["svc1"].Transition(EventDependsReady)
	sms["svc1"].Transition(EventProcessStarted)

	sc := NewShutdownCoordinator(pm, dg, sms, stopCfgs, 30)
	err = sc.GracefulShutdown(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// 验证状态为down
	if sms["svc1"].Current() != StateDown {
		t.Errorf("expected state down, got %s", sms["svc1"].Current())
	}
}

// TestShutdown_ReverseDependencyOrder 多层依赖反序停止
// REQ-F-032: 按依赖反序停止，依赖者先停，被依赖者后停
// A → B → C (C depends on B, B depends on A)
// 停止顺序: C先停, B后停, A最后停
func TestShutdown_ReverseDependencyOrder(t *testing.T) {
	pm := NewProcessManager()
	dg := NewDependencyGraph()
	sms := make(map[string]*StateMachine)
	stopCfgs := make(map[string]StopConfig)

	dg.AddService("A", nil)
	dg.AddService("B", []string{"A"})
	dg.AddService("C", []string{"B"})

	var stopOrderMu sync.Mutex
	var stopOrder []string

	for _, name := range []string{"A", "B", "C"} {
		sm := NewStateMachine()
		sms[name] = sm
		stopCfgs[name] = StopConfig{GraceSeconds: 5, TimeoutSeconds: 30}

		proc, err := StartProcess(name, []string{"sleep", "60"}, nil, "", nil)
		if err != nil {
			t.Fatalf("StartProcess %s failed: %v", name, err)
		}
		pm.Register(name, proc)

		// 状态机转移到ready
		sm.Transition(EventDependsReady)
		sm.Transition(EventProcessStarted)
		sm.Transition(EventReadinessPassed)

		// 订阅状态变更以记录停止顺序
		ch := sm.Subscribe()
		go func(svcName string, subCh <-chan StateTransition) {
			for transition := range subCh {
				if transition.To == StateDown {
					stopOrderMu.Lock()
					stopOrder = append(stopOrder, svcName)
					stopOrderMu.Unlock()
				}
			}
		}(name, ch)
	}

	sc := NewShutdownCoordinator(pm, dg, sms, stopCfgs, 30)
	err := sc.GracefulShutdown(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// 等待所有状态变更通知
	time.Sleep(200 * time.Millisecond)

	stopOrderMu.Lock()
	defer stopOrderMu.Unlock()
	if len(stopOrder) != 3 {
		t.Fatalf("expected 3 stops, got %d: %v", len(stopOrder), stopOrder)
	}

	// 验证停止顺序: C先停, B后停, A最后停
	if stopOrder[0] != "C" {
		t.Errorf("first stopped service = %s, want C", stopOrder[0])
	}
	if stopOrder[1] != "B" {
		t.Errorf("second stopped service = %s, want B", stopOrder[1])
	}
	if stopOrder[2] != "A" {
		t.Errorf("third stopped service = %s, want A", stopOrder[2])
	}

	// 关闭状态机
	for _, sm := range sms {
		sm.Close()
	}
}

// TestShutdown_ParallelLayerStop 同层服务并行停止
// REQ-F-032: 同层服务并行停止，不串行
func TestShutdown_ParallelLayerStop(t *testing.T) {
	pm := NewProcessManager()
	dg := NewDependencyGraph()
	sms := make(map[string]*StateMachine)
	stopCfgs := make(map[string]StopConfig)

	// B1和B2都依赖A，同一层
	dg.AddService("A", nil)
	dg.AddService("B1", []string{"A"})
	dg.AddService("B2", []string{"A"})

	for _, name := range []string{"A", "B1", "B2"} {
		sm := NewStateMachine()
		sms[name] = sm
		stopCfgs[name] = StopConfig{GraceSeconds: 5, TimeoutSeconds: 30}

		proc, err := StartProcess(name, []string{"sleep", "60"}, nil, "", nil)
		if err != nil {
			t.Fatalf("StartProcess %s failed: %v", name, err)
		}
		pm.Register(name, proc)

		sm.Transition(EventDependsReady)
		sm.Transition(EventProcessStarted)
	}

	sc := NewShutdownCoordinator(pm, dg, sms, stopCfgs, 30)

	start := time.Now()
	err := sc.GracefulShutdown(context.Background())
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// 如果B1和B2并行停止，总时间应该接近单个服务停止时间，而不是两倍
	// sleep进程收到SIGTERM后会很快退出，并行停止应该在2秒内完成
	if elapsed > 5*time.Second {
		t.Errorf("parallel layer stop took %v, expected < 5s (services should stop in parallel)", elapsed)
	}

	for _, sm := range sms {
		sm.Close()
	}
}

// TestShutdown_GraceTimeoutForceKill grace超时后强制SIGKILL
// REQ-F-047: shutdown_grace_seconds到期→强制SIGKILL所有剩余进程
func TestShutdown_GraceTimeoutForceKill(t *testing.T) {
	pm := NewProcessManager()
	dg := NewDependencyGraph()
	sms := make(map[string]*StateMachine)
	stopCfgs := make(map[string]StopConfig)

	dg.AddService("svc1", nil)
	sms["svc1"] = NewStateMachine()
	defer sms["svc1"].Close()
	stopCfgs["svc1"] = StopConfig{GraceSeconds: 30, TimeoutSeconds: 60}

	// 启动一个忽略SIGTERM的进程
	proc := startIgnoreTermProcess(t, "svc1")
	pm.Register("svc1", proc)

	sms["svc1"].Transition(EventDependsReady)
	sms["svc1"].Transition(EventProcessStarted)

	// 设置短grace期（2秒）
	sc := NewShutdownCoordinator(pm, dg, sms, stopCfgs, 2)

	start := time.Now()
	err := sc.GracefulShutdown(context.Background())
	elapsed := time.Since(start)

	// 应该返回超时错误
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if err != context.DeadlineExceeded {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}

	// 应该在grace期附近完成（允许一定误差）
	if elapsed > 6*time.Second {
		t.Errorf("shutdown took %v, should be around 2s grace period", elapsed)
	}
}

// TestShutdown_ServiceStopTimeoutContinueNextLayer 服务停止超时后继续下一层
// REQ-F-032: 即使某层服务停止超时，也继续处理下一层
func TestShutdown_ServiceStopTimeoutContinueNextLayer(t *testing.T) {
	pm := NewProcessManager()
	dg := NewDependencyGraph()
	sms := make(map[string]*StateMachine)
	stopCfgs := make(map[string]StopConfig)

	// A → B (B depends on A)
	dg.AddService("A", nil)
	dg.AddService("B", []string{"A"})

	sms["A"] = NewStateMachine()
	defer sms["A"].Close()
	// A的停止超时很短，B的正常
	stopCfgs["A"] = StopConfig{GraceSeconds: 1, TimeoutSeconds: 2}

	sms["B"] = NewStateMachine()
	defer sms["B"].Close()
	stopCfgs["B"] = StopConfig{GraceSeconds: 5, TimeoutSeconds: 30}

	// A用忽略SIGTERM的进程（会超时）
	procA := startIgnoreTermProcess(t, "A")
	pm.Register("A", procA)
	sms["A"].Transition(EventDependsReady)
	sms["A"].Transition(EventProcessStarted)

	// B用正常进程
	procB, err := StartProcess("B", []string{"sleep", "60"}, nil, "", nil)
	if err != nil {
		t.Fatalf("StartProcess B failed: %v", err)
	}
	pm.Register("B", procB)
	sms["B"].Transition(EventDependsReady)
	sms["B"].Transition(EventProcessStarted)

	// grace期足够长以允许A层超时后B也能停止
	sc := NewShutdownCoordinator(pm, dg, sms, stopCfgs, 30)
	_ = sc.GracefulShutdown(context.Background())
	// 即使A层超时，B也应该被停止
	// 可能返回错误（A超时），也可能不返回（因为forceKillAll清除了）

	// 验证B的进程已被注销
	if _, ok := pm.Get("B"); ok {
		t.Error("B should be unregistered after shutdown")
	}
}

// TestShutdown_SignalDuringStartup 启动中收到信号处理
// REQ-F-032: 启动中收到SIGTERM/SIGINT时，立即停止拉起剩余服务
func TestShutdown_SignalDuringStartup(t *testing.T) {
	pm := NewProcessManager()
	dg := NewDependencyGraph()
	sms := make(map[string]*StateMachine)
	stopCfgs := make(map[string]StopConfig)

	dg.AddService("svc1", nil)
	sms["svc1"] = NewStateMachine()
	defer sms["svc1"].Close()
	stopCfgs["svc1"] = StopConfig{GraceSeconds: 5, TimeoutSeconds: 30}

	// 启动进程但状态机留在starting（模拟启动中）
	proc, err := StartProcess("svc1", []string{"sleep", "60"}, nil, "", nil)
	if err != nil {
		t.Fatalf("StartProcess failed: %v", err)
	}
	pm.Register("svc1", proc)

	// 状态机只到starting，还没到up
	sms["svc1"].Transition(EventDependsReady)
	// 不执行 EventProcessStarted，保持starting状态

	sc := NewShutdownCoordinator(pm, dg, sms, stopCfgs, 30)

	// 先请求关机
	go func() {
		time.Sleep(100 * time.Millisecond)
		sc.GracefulShutdown(context.Background())
	}()

	// 模拟启动流程检查ShutdownRequested
	time.Sleep(200 * time.Millisecond)
	if !sc.ShutdownRequested() {
		t.Error("ShutdownRequested() should return true after GracefulShutdown is called")
	}

	// 等待关机完成
	time.Sleep(2 * time.Second)
}

// TestShutdown_ShutdownRequestedFlag ShutdownRequested标志测试
// REQ-F-032: ShutdownRequested()检查是否已请求关机
func TestShutdown_ShutdownRequestedFlag(t *testing.T) {
	pm := NewProcessManager()
	dg := NewDependencyGraph()
	sms := make(map[string]*StateMachine)
	stopCfgs := make(map[string]StopConfig)

	sc := NewShutdownCoordinator(pm, dg, sms, stopCfgs, 30)

	// 初始状态应该未请求关机
	if sc.ShutdownRequested() {
		t.Error("ShutdownRequested() should return false initially")
	}

	// 调用GracefulShutdown后应该返回true
	sc.GracefulShutdown(context.Background())

	if !sc.ShutdownRequested() {
		t.Error("ShutdownRequested() should return true after GracefulShutdown")
	}
}

// TestShutdown_DefaultGraceSeconds 默认grace秒数测试
// REQ-F-047: shutdown_grace_seconds默认30秒
func TestShutdown_DefaultGraceSeconds(t *testing.T) {
	pm := NewProcessManager()
	dg := NewDependencyGraph()
	sms := make(map[string]*StateMachine)
	stopCfgs := make(map[string]StopConfig)

	// 传入0，应该使用默认30秒
	sc := NewShutdownCoordinator(pm, dg, sms, stopCfgs, 0)
	if sc.graceSeconds != 30 {
		t.Errorf("graceSeconds = %d, want 30", sc.graceSeconds)
	}

	// 传入负数，也应该使用默认30秒
	sc2 := NewShutdownCoordinator(pm, dg, sms, stopCfgs, -5)
	if sc2.graceSeconds != 30 {
		t.Errorf("graceSeconds = %d, want 30", sc2.graceSeconds)
	}
}

// TestShutdown_MultipleServicesMultipleLayers 多服务多层依赖测试
// 验证完整的反序停止+并行停止行为
// 依赖图: A(无依赖) → B1,B2(都依赖A) → C(依赖B1)
// 停止顺序: [C] → [B1,B2] → [A]
func TestShutdown_MultipleServicesMultipleLayers(t *testing.T) {
	pm := NewProcessManager()
	dg := NewDependencyGraph()
	sms := make(map[string]*StateMachine)
	stopCfgs := make(map[string]StopConfig)

	dg.AddService("A", nil)
	dg.AddService("B1", []string{"A"})
	dg.AddService("B2", []string{"A"})
	dg.AddService("C", []string{"B1"})

	var stopOrderMu sync.Mutex
	var stopOrder []string
	var layerDoneCount atomic.Int32

	for _, name := range []string{"A", "B1", "B2", "C"} {
		sm := NewStateMachine()
		sms[name] = sm
		stopCfgs[name] = StopConfig{GraceSeconds: 5, TimeoutSeconds: 30}

		proc, err := StartProcess(name, []string{"sleep", "60"}, nil, "", nil)
		if err != nil {
			t.Fatalf("StartProcess %s failed: %v", name, err)
		}
		pm.Register(name, proc)

		sm.Transition(EventDependsReady)
		sm.Transition(EventProcessStarted)
		sm.Transition(EventReadinessPassed)

		// 订阅状态变更以记录停止顺序
		ch := sm.Subscribe()
		go func(svcName string, subCh <-chan StateTransition) {
			for transition := range subCh {
				if transition.To == StateDown {
					stopOrderMu.Lock()
					stopOrder = append(stopOrder, svcName)
					stopOrderMu.Unlock()
					layerDoneCount.Add(1)
				}
			}
		}(name, ch)
	}

	sc := NewShutdownCoordinator(pm, dg, sms, stopCfgs, 30)
	err := sc.GracefulShutdown(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// 等待所有状态变更通知
	timeout := time.After(5 * time.Second)
	for layerDoneCount.Load() < 4 {
		select {
		case <-timeout:
			t.Fatal("timed out waiting for all services to stop")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	stopOrderMu.Lock()
	defer stopOrderMu.Unlock()
	if len(stopOrder) != 4 {
		t.Fatalf("expected 4 stops, got %d: %v", len(stopOrder), stopOrder)
	}

	// 验证停止顺序：C必须在B1之前，B1和B2必须在A之前
	cIdx := indexOf(stopOrder, "C")
	b1Idx := indexOf(stopOrder, "B1")
	b2Idx := indexOf(stopOrder, "B2")
	aIdx := indexOf(stopOrder, "A")

	if cIdx > b1Idx {
		t.Errorf("C (index %d) should stop before B1 (index %d)", cIdx, b1Idx)
	}
	if b1Idx > aIdx {
		t.Errorf("B1 (index %d) should stop before A (index %d)", b1Idx, aIdx)
	}
	if b2Idx > aIdx {
		t.Errorf("B2 (index %d) should stop before A (index %d)", b2Idx, aIdx)
	}

	for _, sm := range sms {
		sm.Close()
	}
}

// TestShutdown_SkipNonRunningServices 跳过非运行状态的服务
// REQ-F-032: 只停止运行中的服务（up/ready/starting），跳过pending/down/failed
func TestShutdown_SkipNonRunningServices(t *testing.T) {
	pm := NewProcessManager()
	dg := NewDependencyGraph()
	sms := make(map[string]*StateMachine)
	stopCfgs := make(map[string]StopConfig)

	// A: pending状态（从未启动）
	dg.AddService("A", nil)
	sms["A"] = NewStateMachine()
	defer sms["A"].Close()
	stopCfgs["A"] = StopConfig{GraceSeconds: 5, TimeoutSeconds: 30}

	// B: down状态
	dg.AddService("B", nil)
	sms["B"] = NewStateMachine()
	defer sms["B"].Close()
	sms["B"].Transition(EventDependsReady)
	sms["B"].Transition(EventProcessStarted)
	sms["B"].Transition(EventStopRequested)
	sms["B"].Transition(EventProcessExited)
	stopCfgs["B"] = StopConfig{GraceSeconds: 5, TimeoutSeconds: 30}

	// C: ready状态（运行中）
	dg.AddService("C", nil)
	sms["C"] = NewStateMachine()
	defer sms["C"].Close()
	stopCfgs["C"] = StopConfig{GraceSeconds: 5, TimeoutSeconds: 30}
	proc, err := StartProcess("C", []string{"sleep", "60"}, nil, "", nil)
	if err != nil {
		t.Fatalf("StartProcess failed: %v", err)
	}
	pm.Register("C", proc)
	sms["C"].Transition(EventDependsReady)
	sms["C"].Transition(EventProcessStarted)
	sms["C"].Transition(EventReadinessPassed)

	sc := NewShutdownCoordinator(pm, dg, sms, stopCfgs, 30)
	err = sc.GracefulShutdown(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// A应该保持pending
	if sms["A"].Current() != StatePending {
		t.Errorf("A should remain pending, got %s", sms["A"].Current())
	}
	// B应该保持down
	if sms["B"].Current() != StateDown {
		t.Errorf("B should remain down, got %s", sms["B"].Current())
	}
	// C应该变为down
	if sms["C"].Current() != StateDown {
		t.Errorf("C should be down, got %s", sms["C"].Current())
	}
}

// TestShutdown_WithContextDeadline ctx已有deadline时使用原deadline
// REQ-F-047: 如果ctx已有deadline，不创建新的timeout
func TestShutdown_WithContextDeadline(t *testing.T) {
	pm := NewProcessManager()
	dg := NewDependencyGraph()
	sms := make(map[string]*StateMachine)
	stopCfgs := make(map[string]StopConfig)

	dg.AddService("svc1", nil)
	sms["svc1"] = NewStateMachine()
	defer sms["svc1"].Close()
	stopCfgs["svc1"] = StopConfig{GraceSeconds: 30, TimeoutSeconds: 60}

	proc := startIgnoreTermProcess(t, "svc1")
	pm.Register("svc1", proc)
	sms["svc1"].Transition(EventDependsReady)
	sms["svc1"].Transition(EventProcessStarted)

	// 使用自定义的短deadline context（1秒）
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	sc := NewShutdownCoordinator(pm, dg, sms, stopCfgs, 30)
	start := time.Now()
	err := sc.GracefulShutdown(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error")
	}
	// 应该在1秒deadline附近完成
	if elapsed > 4*time.Second {
		t.Errorf("shutdown took %v, should respect the 1s context deadline", elapsed)
	}
}

// TestReverseLayers 验证ReverseLayers方法
// REQ-F-032: 按依赖反序返回分层结果
func TestReverseLayers(t *testing.T) {
	dg := NewDependencyGraph()
	dg.AddService("A", nil)
	dg.AddService("B", []string{"A"})
	dg.AddService("C", []string{"B"})

	layers := dg.ReverseLayers()
	if len(layers) != 3 {
		t.Fatalf("expected 3 layers, got %d", len(layers))
	}

	// 第一层（先停）: C
	if len(layers[0]) != 1 || layers[0][0] != "C" {
		t.Errorf("layer 0 = %v, want [C]", layers[0])
	}
	// 第二层: B
	if len(layers[1]) != 1 || layers[1][0] != "B" {
		t.Errorf("layer 1 = %v, want [B]", layers[1])
	}
	// 第三层（最后停）: A
	if len(layers[2]) != 1 || layers[2][0] != "A" {
		t.Errorf("layer 2 = %v, want [A]", layers[2])
	}
}

// TestReverseLayers_Parallel 同层服务应在同一层
func TestReverseLayers_Parallel(t *testing.T) {
	dg := NewDependencyGraph()
	dg.AddService("A", nil)
	dg.AddService("B1", []string{"A"})
	dg.AddService("B2", []string{"A"})

	layers := dg.ReverseLayers()
	if len(layers) != 2 {
		t.Fatalf("expected 2 layers, got %d", len(layers))
	}

	// 第一层（先停）: B1和B2（同层并行停止）
	if len(layers[0]) != 2 {
		t.Errorf("layer 0 has %d services, want 2: %v", len(layers[0]), layers[0])
	}
	// 第二层: A
	if len(layers[1]) != 1 || layers[1][0] != "A" {
		t.Errorf("layer 1 = %v, want [A]", layers[1])
	}
}

// TestReverseLayers_Empty 空依赖图
func TestReverseLayers_Empty(t *testing.T) {
	dg := NewDependencyGraph()
	layers := dg.ReverseLayers()
	if len(layers) != 0 {
		t.Errorf("expected 0 layers for empty graph, got %d", len(layers))
	}
}

// TestShutdown_ForceKillAll forceKillAll方法测试
// REQ-F-047: 强制SIGKILL所有剩余进程
func TestShutdown_ForceKillAll(t *testing.T) {
	pm := NewProcessManager()
	dg := NewDependencyGraph()

	// 注册两个进程
	for _, name := range []string{"svc1", "svc2"} {
		proc, err := StartProcess(name, []string{"sleep", "60"}, nil, "", nil)
		if err != nil {
			t.Fatalf("StartProcess %s failed: %v", name, err)
		}
		pm.Register(name, proc)
	}

	sms := make(map[string]*StateMachine)
	stopCfgs := make(map[string]StopConfig)
	sc := NewShutdownCoordinator(pm, dg, sms, stopCfgs, 30)

	// 直接调用forceKillAll
	sc.forceKillAll()

	// 所有进程应被注销
	if len(pm.List()) != 0 {
		t.Errorf("expected 0 remaining processes, got %d", len(pm.List()))
	}
}

// TestShutdown_DoubleShutdown 双次关机请求测试
// 确保多次调用GracefulShutdown不会panic
func TestShutdown_DoubleShutdown(t *testing.T) {
	pm := NewProcessManager()
	dg := NewDependencyGraph()
	sms := make(map[string]*StateMachine)
	stopCfgs := make(map[string]StopConfig)

	sc := NewShutdownCoordinator(pm, dg, sms, stopCfgs, 30)

	// 第一次关机
	err := sc.GracefulShutdown(context.Background())
	if err != nil {
		t.Fatalf("first shutdown failed: %v", err)
	}

	// 第二次关机应该不会panic，也不会重复关闭channel
	err = sc.GracefulShutdown(context.Background())
	if err != nil {
		t.Fatalf("second shutdown failed: %v", err)
	}

	if !sc.ShutdownRequested() {
		t.Error("ShutdownRequested() should return true")
	}
}

// TestShutdown_LayerTimeoutWithParentContext 层级超时受父context约束
// REQ-F-032: 同层并行停止的总时长 = max(各服务stop.timeout_seconds)
// 但受全局grace context约束
func TestShutdown_LayerTimeoutWithParentContext(t *testing.T) {
	pm := NewProcessManager()
	dg := NewDependencyGraph()
	sms := make(map[string]*StateMachine)
	stopCfgs := make(map[string]StopConfig)

	dg.AddService("svc1", nil)
	sms["svc1"] = NewStateMachine()
	defer sms["svc1"].Close()
	// 配置很长的timeout_seconds，但grace context会先到期
	stopCfgs["svc1"] = StopConfig{GraceSeconds: 30, TimeoutSeconds: 60}

	proc := startIgnoreTermProcess(t, "svc1")
	pm.Register("svc1", proc)
	sms["svc1"].Transition(EventDependsReady)
	sms["svc1"].Transition(EventProcessStarted)

	// 短grace期
	sc := NewShutdownCoordinator(pm, dg, sms, stopCfgs, 2)
	start := time.Now()
	err := sc.GracefulShutdown(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error")
	}
	// 应该在grace期附近完成，而不是等60秒timeout
	if elapsed > 6*time.Second {
		t.Errorf("shutdown took %v, should be around 2s grace period", elapsed)
	}
}

// indexOf 返回字符串在切片中的索引，找不到返回-1
func indexOf(slice []string, item string) int {
	for i, s := range slice {
		if s == item {
			return i
		}
	}
	return -1
}

// TestShutdown_AllServicesDownState 所有服务停止后状态验证
// REQ-F-032: 服务停止后状态应为down
func TestShutdown_AllServicesDownState(t *testing.T) {
	pm := NewProcessManager()
	dg := NewDependencyGraph()
	sms := make(map[string]*StateMachine)
	stopCfgs := make(map[string]StopConfig)

	// 3个独立服务（同层）
	for _, name := range []string{"svc1", "svc2", "svc3"} {
		dg.AddService(name, nil)
		sm := NewStateMachine()
		sms[name] = sm
		stopCfgs[name] = StopConfig{GraceSeconds: 5, TimeoutSeconds: 30}

		proc, err := StartProcess(name, []string{"sleep", "60"}, nil, "", nil)
		if err != nil {
			t.Fatalf("StartProcess %s failed: %v", name, err)
		}
		pm.Register(name, proc)

		sm.Transition(EventDependsReady)
		sm.Transition(EventProcessStarted)
		sm.Transition(EventReadinessPassed)
	}

	sc := NewShutdownCoordinator(pm, dg, sms, stopCfgs, 30)
	err := sc.GracefulShutdown(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// 所有服务应该变为down
	for name, sm := range sms {
		if sm.Current() != StateDown {
			t.Errorf("service %s state = %s, want down", name, sm.Current())
		}
		sm.Close()
	}
}

// TestShutdown_ProcessKilledBySIGKILL 进程收到SIGKILL的验证
// REQ-F-047: 关机超时后强制SIGKILL
func TestShutdown_ProcessKilledBySIGKILL(t *testing.T) {
	pm := NewProcessManager()
	dg := NewDependencyGraph()
	sms := make(map[string]*StateMachine)
	stopCfgs := make(map[string]StopConfig)

	dg.AddService("svc1", nil)
	sms["svc1"] = NewStateMachine()
	defer sms["svc1"].Close()
	stopCfgs["svc1"] = StopConfig{GraceSeconds: 30, TimeoutSeconds: 60}

	// 忽略SIGTERM的进程
	proc := startIgnoreTermProcess(t, "svc1")
	pm.Register("svc1", proc)
	sms["svc1"].Transition(EventDependsReady)
	sms["svc1"].Transition(EventProcessStarted)

	// 短grace期触发forceKillAll
	sc := NewShutdownCoordinator(pm, dg, sms, stopCfgs, 2)
	err := sc.GracefulShutdown(context.Background())

	// 进程应该已被强制杀掉
	// 验证ProcessManager中已无该进程
	if _, ok := pm.Get("svc1"); ok {
		t.Error("svc1 should be unregistered after force kill")
	}

	_ = err // 可能是timeout error
}

// TestShutdown_SkipServiceWithoutStateMachine 无状态机的服务被跳过
func TestShutdown_SkipServiceWithoutStateMachine(t *testing.T) {
	pm := NewProcessManager()
	dg := NewDependencyGraph()
	sms := make(map[string]*StateMachine)
	stopCfgs := make(map[string]StopConfig)

	// 依赖图中有服务但状态机map中没有
	dg.AddService("missing", nil)

	sc := NewShutdownCoordinator(pm, dg, sms, stopCfgs, 30)
	err := sc.GracefulShutdown(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

// TestShutdown_StopServiceWithProcessExited 进程已退出时的处理
// REQ-F-032: 进程不存在时，直接完成状态转移
func TestShutdown_StopServiceWithProcessExited(t *testing.T) {
	pm := NewProcessManager()
	dg := NewDependencyGraph()
	sms := make(map[string]*StateMachine)
	stopCfgs := make(map[string]StopConfig)

	dg.AddService("svc1", nil)
	sm := NewStateMachine()
	sms["svc1"] = sm
	stopCfgs["svc1"] = StopConfig{GraceSeconds: 5, TimeoutSeconds: 30}

	// 启动一个会立即退出的进程
	proc, err := StartProcess("svc1", []string{"true"}, nil, "", nil)
	if err != nil {
		t.Fatalf("StartProcess failed: %v", err)
	}
	pm.Register("svc1", proc)

	sm.Transition(EventDependsReady)
	sm.Transition(EventProcessStarted)

	// 等待进程退出
	time.Sleep(200 * time.Millisecond)

	// 进程已退出但ProcessManager中还注册着
	sc := NewShutdownCoordinator(pm, dg, sms, stopCfgs, 30)
	err = sc.GracefulShutdown(context.Background())
	// 应该不会出错
	_ = err

	sm.Close()
}

// TestListRunning 测试ListRunning方法
// REQ-F-032: 返回所有有运行中进程的服务名
func TestListRunning(t *testing.T) {
	pm := NewProcessManager()

	// 注册一个运行中的进程
	proc1, err := StartProcess("running", []string{"sleep", "60"}, nil, "", nil)
	if err != nil {
		t.Fatalf("StartProcess failed: %v", err)
	}
	pm.Register("running", proc1)

	// 注册一个会立即退出的进程，并启动Wait goroutine
	proc2, err := StartProcess("exiting", []string{"true"}, nil, "", nil)
	if err != nil {
		t.Fatalf("StartProcess failed: %v", err)
	}
	pm.Register("exiting", proc2)
	// 启动Wait goroutine以回收僵尸进程并关闭Done channel
	go proc2.Wait()

	// 等待exiting进程退出
	time.Sleep(200 * time.Millisecond)

	running := pm.ListRunning()
	if len(running) != 1 || running[0] != "running" {
		t.Errorf("ListRunning() = %v, want [running]", running)
	}

	// 清理
	proc1.SendSignal(syscall.SIGTERM)
	time.Sleep(100 * time.Millisecond)
}

// TestShutdownCoordinator_SetPreShutdownHook L-01-003: 验证 SetPreShutdownHook 注入的回调在 shutdown 时被调用。
// REQ-D-004, 2.8.1: supd_lifecycle:pre_shutdown 在 supd 退出第1步触发。
func TestShutdownCoordinator_SetPreShutdownHook(t *testing.T) {
	pm := NewProcessManager()
	dg := NewDependencyGraph()
	sms := make(map[string]*StateMachine)
	stopCfgs := make(map[string]StopConfig)
	sc := NewShutdownCoordinator(pm, dg, sms, stopCfgs, 30)

	called := atomic.Bool{}
	sc.SetPreShutdownHook(func(ctx context.Context) error {
		called.Store(true)
		return nil
	})

	// 触发 shutdown（无服务，立即返回）
	_ = sc.GracefulShutdown(context.Background())

	if !called.Load() {
		t.Error("expected preShutdown hook to be called, but it was not")
	}
}

// TestShutdownCoordinator_SetPreStopHook L-01-003: 验证 SetPreStopHook 注入的回调在 stop service 时被调用。
// REQ-D-004, 2.1.4: service_lifecycle:pre_stop 在服务停止前触发。
func TestShutdownCoordinator_SetPreStopHook(t *testing.T) {
	pm := NewProcessManager()
	dg := NewDependencyGraph()
	sms := make(map[string]*StateMachine)
	stopCfgs := make(map[string]StopConfig)

	// 启动一个真实进程用于测试
	proc, err := StartProcess("test-svc", []string{"sleep", "30"}, nil, "", nil)
	if err != nil {
		t.Fatalf("StartProcess failed: %v", err)
	}
	pm.Register("test-svc", proc)

	dg.AddService("test-svc", nil)
	sms["test-svc"] = NewStateMachine()
	defer sms["test-svc"].Close()
	stopCfgs["test-svc"] = StopConfig{GraceSeconds: 5, TimeoutSeconds: 30}

	// 状态机转移到 up
	sms["test-svc"].Transition(EventDependsReady)
	sms["test-svc"].Transition(EventProcessStarted)

	sc := NewShutdownCoordinator(pm, dg, sms, stopCfgs, 30)

	called := atomic.Bool{}
	sc.SetPreStopHook(func(serviceName string, servicePID int) func() error {
		return func() error {
			if serviceName != "test-svc" {
				t.Errorf("expected serviceName test-svc, got %s", serviceName)
			}
			if servicePID != proc.PID() {
				t.Errorf("expected PID %d, got %d", proc.PID(), servicePID)
			}
			called.Store(true)
			return nil
		}
	})

	// 触发 shutdown
	_ = sc.GracefulShutdown(context.Background())

	if !called.Load() {
		t.Error("expected preStop hook to be called, but it was not")
	}
}
