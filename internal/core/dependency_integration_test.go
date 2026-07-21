package core

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestBootstrap_DependencyChain_StartupOrder 验证 3 个相互依赖服务的启动顺序
// L-04-001: 多服务依赖启动集成测试
//
// 拓扑：svc-a → svc-b → svc-c（svc-c 依赖 svc-b，svc-b 依赖 svc-a）
// 预期：按拓扑顺序串行启动 svc-a → svc-b → svc-c
//
// REQ-F-033: 同层并行，跨层等待 ready
// REQ-F-005: 拓扑排序后按层级启动
//
// 用真实的 ProcessManager + Bootstrap 构造集成场景，
// 用 sleep 命令作为服务进程便于观察时序，
// 通过 OnServicePreStart 回调记录启动顺序与时间戳，
// 同时通过 Process.StartTime() 验证进程启动时间戳单调递增。
func TestBootstrap_DependencyChain_StartupOrder(t *testing.T) {
	baseDir, logDir := setupTestDir(t)

	// svc-a 无依赖（第 0 层）
	writeService(t, baseDir, "svc-a", `name: svc-a
version: "1.0"
command:
  - sleep
  - "60"
`)
	// svc-b 依赖 svc-a（第 1 层）
	writeService(t, baseDir, "svc-b", `name: svc-b
version: "1.0"
command:
  - sleep
  - "60"
depends_on:
  - svc-a
`)
	// svc-c 依赖 svc-b（第 2 层）
	writeService(t, baseDir, "svc-c", `name: svc-c
version: "1.0"
command:
  - sleep
  - "60"
depends_on:
  - svc-b
`)

	// 通过 OnServicePreStart 回调记录启动顺序与时间戳
	// Bootstrap 在 startService 中按拓扑层级调用此回调：
	// 跨层等待本层所有服务到达终态（up/ready/failed）后才调用下一层的回调
	// 链式拓扑每层只有一个服务，因此回调严格按 svc-a → svc-b → svc-c 顺序串行触发
	var mu sync.Mutex
	var startupOrder []string
	startTimestamps := make(map[string]time.Time)

	cfg := BootstrapConfig{
		ConfigPath: filepath.Join(baseDir, "config.yaml"),
		BaseDir:    baseDir,
		LogDir:     logDir,
		OnServicePreStart: func(ctx context.Context, serviceName string) {
			mu.Lock()
			defer mu.Unlock()
			startupOrder = append(startupOrder, serviceName)
			startTimestamps[serviceName] = time.Now()
		},
	}

	b := NewBootstrap(cfg)
	result, err := b.Run(context.Background())
	if err != nil {
		t.Fatalf("Bootstrap.Run() error = %v", err)
	}
	defer cleanupBootstrap(t, result)

	// 验证 1：3 个状态机都已创建并进入 up 状态
	// （无 readiness 配置，进程启动后立即进入 up）
	for _, name := range []string{"svc-a", "svc-b", "svc-c"} {
		sm, ok := result.StateMachines[name]
		if !ok {
			t.Errorf("%s state machine not found", name)
			continue
		}
		if state := sm.Current(); state != StateUp {
			t.Errorf("%s state = %v, want %v", name, state, StateUp)
		}
	}

	// 验证 2：3 个进程都已注册到 ProcessManager
	procs := result.ProcessMgr.List()
	if len(procs) != 3 {
		t.Errorf("expected 3 processes, got %d: %v", len(procs), procs)
	}

	// 验证 3：依赖图拓扑排序为 3 层 [svc-a], [svc-b], [svc-c]
	layers, cycle := result.DepGraph.TopologicalSort()
	if len(cycle) > 0 {
		t.Errorf("unexpected cycle: %v", cycle)
	}
	if len(layers) != 3 {
		t.Fatalf("expected 3 layers, got %d: %v", len(layers), layers)
	}
	if len(layers[0]) != 1 || layers[0][0] != "svc-a" {
		t.Errorf("layer 0 should be [svc-a], got %v", layers[0])
	}
	if len(layers[1]) != 1 || layers[1][0] != "svc-b" {
		t.Errorf("layer 1 should be [svc-b], got %v", layers[1])
	}
	if len(layers[2]) != 1 || layers[2][0] != "svc-c" {
		t.Errorf("layer 2 should be [svc-c], got %v", layers[2])
	}

	// 验证 4：Bootstrap 真实按拓扑顺序启动（关键集成验证）
	// 这是 L-04-001 的核心断言：不仅是拓扑排序结果正确，
	// 而且真实的 Bootstrap 启动流程也按此顺序执行
	mu.Lock()
	defer mu.Unlock()
	if len(startupOrder) != 3 {
		t.Fatalf("expected 3 startup events, got %d: %v", len(startupOrder), startupOrder)
	}
	expectedOrder := []string{"svc-a", "svc-b", "svc-c"}
	for i, name := range expectedOrder {
		if startupOrder[i] != name {
			t.Errorf("startup order[%d] = %s, want %s (full order: %v)",
				i, startupOrder[i], name, startupOrder)
			break
		}
	}

	// 验证 5：OnServicePreStart 回调时间戳单调递增
	// 跨层等待 ready 意味着 svc-b 在 svc-a 进入 up 后才触发 pre_start，svc-c 在 svc-b 进入 up 后才触发
	tA := startTimestamps["svc-a"]
	tB := startTimestamps["svc-b"]
	tC := startTimestamps["svc-c"]
	if tA.IsZero() || tB.IsZero() || tC.IsZero() {
		t.Fatalf("missing start timestamps: a=%v b=%v c=%v", tA, tB, tC)
	}
	if !tB.After(tA) {
		t.Errorf("svc-b pre_start time (%v) should be after svc-a (%v)", tB, tA)
	}
	if !tC.After(tB) {
		t.Errorf("svc-c pre_start time (%v) should be after svc-b (%v)", tC, tB)
	}

	// 验证 6：进程启动时间戳也单调递增（更严格的集成证据）
	// Process.StartTime() 是 OS 层面的进程创建时间，不受 Go 回调时序影响
	procA, ok := result.ProcessMgr.Get("svc-a")
	if !ok {
		t.Fatal("svc-a process not found in ProcessManager")
	}
	procB, ok := result.ProcessMgr.Get("svc-b")
	if !ok {
		t.Fatal("svc-b process not found in ProcessManager")
	}
	procC, ok := result.ProcessMgr.Get("svc-c")
	if !ok {
		t.Fatal("svc-c process not found in ProcessManager")
	}
	if !procB.StartTime().After(procA.StartTime()) {
		t.Errorf("svc-b process StartTime (%v) should be after svc-a (%v)",
			procB.StartTime(), procA.StartTime())
	}
	if !procC.StartTime().After(procB.StartTime()) {
		t.Errorf("svc-c process StartTime (%v) should be after svc-b (%v)",
			procC.StartTime(), procB.StartTime())
	}
}

// TestBootstrap_DependencyChain_DependenciesReady 验证服务启动时依赖服务已就绪
// L-04-001: 验证 Bootstrap 跨层等待 ready 的机制
//
// 拓扑：svc-a → svc-b → svc-c（链式）
// 在 svc-b 的 pre_start 触发时，svc-a 应已处于 up 状态
// 在 svc-c 的 pre_start 触发时，svc-b 应已处于 up 状态
//
// REQ-F-033: 跨层等待 ready（同层并行，跨层等待本层所有服务到达终态后才启动下一层）
// REQ-F-004: 状态机转移规则 starting → up（进程启动后立即进入 up，无 readiness 配置时）
func TestBootstrap_DependencyChain_DependenciesReady(t *testing.T) {
	baseDir, logDir := setupTestDir(t)

	writeService(t, baseDir, "svc-a", `name: svc-a
version: "1.0"
command:
  - sleep
  - "60"
`)
	writeService(t, baseDir, "svc-b", `name: svc-b
version: "1.0"
command:
  - sleep
  - "60"
depends_on:
  - svc-a
`)
	writeService(t, baseDir, "svc-c", `name: svc-c
version: "1.0"
command:
  - sleep
  - "60"
depends_on:
  - svc-b
`)

	// depStateAtStart: 在调用某服务 pre_start 时，被依赖服务的状态
	// 例如 svc-b 启动时，svc-a 的状态
	var mu sync.Mutex
	depStateAtStart := make(map[string]ServiceState)

	// bootstrapRef 通过闭包引用 Bootstrap 实例：
	// Bootstrap.result 在 Run 开始时被赋值（行 89-94），
	// 因此 OnServicePreStart 在 Run 内部被调用时 bootstrapRef.result 已就绪，
	// 可以读取 StateMachines 验证依赖服务状态
	var bootstrapRef *Bootstrap

	cfg := BootstrapConfig{
		ConfigPath: filepath.Join(baseDir, "config.yaml"),
		BaseDir:    baseDir,
		LogDir:     logDir,
		OnServicePreStart: func(ctx context.Context, serviceName string) {
			mu.Lock()
			defer mu.Unlock()
			if bootstrapRef == nil || bootstrapRef.result == nil {
				return
			}
			// OnServicePreStart 在 startService 中被调用，此时本服务 sm 已经从 pending → starting
			// 由于跨层等待 ready，上一层服务应已进入终态（up，无 readiness 配置时）
			switch serviceName {
			case "svc-b":
				if sm, ok := bootstrapRef.result.StateMachines["svc-a"]; ok {
					depStateAtStart["svc-a_at_svc-b_start"] = sm.Current()
				}
			case "svc-c":
				if sm, ok := bootstrapRef.result.StateMachines["svc-b"]; ok {
					depStateAtStart["svc-b_at_svc-c_start"] = sm.Current()
				}
			}
		},
	}

	bootstrapRef = NewBootstrap(cfg)
	result, err := bootstrapRef.Run(context.Background())
	if err != nil {
		t.Fatalf("Bootstrap.Run() error = %v", err)
	}
	defer cleanupBootstrap(t, result)

	mu.Lock()
	defer mu.Unlock()

	// 验证 1：svc-b 启动时 svc-a 已 up
	// svc-a 无 readiness 配置，进程启动后立即进入 up 状态
	if state, ok := depStateAtStart["svc-a_at_svc-b_start"]; ok {
		if state != StateUp {
			t.Errorf("svc-a should be up when svc-b starts, got %v (expected %v)", state, StateUp)
		}
	} else {
		t.Error("svc-a state at svc-b start not recorded (OnServicePreStart was not called for svc-b)")
	}

	// 验证 2：svc-c 启动时 svc-b 已 up
	if state, ok := depStateAtStart["svc-b_at_svc-c_start"]; ok {
		if state != StateUp {
			t.Errorf("svc-b should be up when svc-c starts, got %v (expected %v)", state, StateUp)
		}
	} else {
		t.Error("svc-b state at svc-c start not recorded (OnServicePreStart was not called for svc-c)")
	}
}
