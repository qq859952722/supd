package core

import (
	"context"
	"syscall"
	"testing"
	"time"
)

// TestDefaultStopConfig_Boundary10sAnd60s
// L-02-001: 规格 §2.1.4 — stop 默认 grace=10s, timeout=60s。
// 锚定默认值双字段，防止未来误改：
//   - GraceSeconds 必须 = 10（SIGTERM 后等待 graceful 退出的窗口）
//   - TimeoutSeconds 必须 = 60（覆盖 pre_stop+SIGTERM+等待 的总超时）
//
// 现有 TestDefaultStopConfig (stop_test.go:268) 已断言这两个值，
// 此处作为 L-02-001 边界专项测试的明确入口，附加"不可被 0/负数污染"的回归。
func TestDefaultStopConfig_Boundary10sAnd60s(t *testing.T) {
	cfg := DefaultStopConfig()
	if cfg.GraceSeconds != 10 {
		t.Errorf("DefaultStopConfig GraceSeconds = %d, want 10 (REQ-2.1.4)", cfg.GraceSeconds)
	}
	if cfg.TimeoutSeconds != 60 {
		t.Errorf("DefaultStopConfig TimeoutSeconds = %d, want 60 (REQ-2.1.4)", cfg.TimeoutSeconds)
	}
	// 默认值必须满足 grace < timeout 的隐含约束（grace 在 timeout 内）
	if cfg.GraceSeconds >= cfg.TimeoutSeconds {
		t.Errorf("invariant violated: GraceSeconds(%d) >= TimeoutSeconds(%d)", cfg.GraceSeconds, cfg.TimeoutSeconds)
	}
}

// TestShutdownCoordinator_MissingStopConfigFallsBackToDefault10s60s
// L-02-001: 规格 §2.1.4 — ShutdownCoordinator.stopSingleService 在 stopConfigs 缺失条目时
// 回退到 DefaultStopConfig()（即 10s grace / 60s timeout）。
// 验证路径：依赖图中存在服务、状态机处于 up，但 stopCfgs 未注册条目。
// 关机流程应使用 DefaultStopConfig 成功停止服务（说明 10s/60s 兜底生效）。
func TestShutdownCoordinator_MissingStopConfigFallsBackToDefault10s60s(t *testing.T) {
	pm := NewProcessManager()
	dg := NewDependencyGraph()
	sms := make(map[string]*StateMachine)
	// 关键：stopCfgs 故意不注册 "svc1"，触发 DefaultStopConfig 回退路径
	stopCfgs := make(map[string]StopConfig)

	dg.AddService("svc1", nil)
	sm := NewStateMachine()
	defer sm.Close()
	sms["svc1"] = sm

	// 启动一个会响应 SIGTERM 的真实进程
	proc, err := StartProcess("svc1", []string{"sleep", "60"}, nil, "", nil)
	if err != nil {
		t.Fatalf("StartProcess failed: %v", err)
	}
	pm.Register("svc1", proc)

	// 状态机转移到 up
	sm.Transition(EventDependsReady)
	sm.Transition(EventProcessStarted)

	sc := NewShutdownCoordinator(pm, dg, sms, stopCfgs, 30)
	err = sc.GracefulShutdown(context.Background())
	if err != nil {
		t.Fatalf("expected no error using DefaultStopConfig fallback, got %v", err)
	}

	// 验证服务被成功停止（DefaultStopConfig 10s grace 在 SIGTERM 后让进程退出）
	if sm.Current() != StateDown {
		t.Errorf("expected state down, got %s", sm.Current())
	}
	if _, ok := pm.Get("svc1"); ok {
		t.Error("svc1 should be unregistered after shutdown")
	}
}

// TestShutdownCoordinator_UserStopConfigOverridesDefault
// L-02-001: 规格 §2.1.4 — 用户显式配置可覆盖默认 grace/timeout。
// 验证：用户配置 GraceSeconds=1, TimeoutSeconds=2（短于默认 10/60），
// 在 SIGTERM 被忽略的进程上应触发 timeout=2s 的 SIGKILL，
// 而非使用默认的 60s（否则测试会运行 60s 才结束）。
func TestShutdownCoordinator_UserStopConfigOverridesDefault(t *testing.T) {
	pm := NewProcessManager()
	dg := NewDependencyGraph()
	sms := make(map[string]*StateMachine)
	stopCfgs := make(map[string]StopConfig)

	dg.AddService("svc1", nil)
	sm := NewStateMachine()
	defer sm.Close()
	sms["svc1"] = sm

	// 用户配置：grace=1s, timeout=2s（明显短于默认 10s/60s）
	stopCfgs["svc1"] = StopConfig{GraceSeconds: 1, TimeoutSeconds: 2}

	// 启动一个忽略 SIGTERM 的进程，使其必须依赖 timeout→SIGKILL 路径
	proc := startIgnoreTermProcess(t, "svc1")
	pm.Register("svc1", proc)

	sm.Transition(EventDependsReady)
	sm.Transition(EventProcessStarted)

	sc := NewShutdownCoordinator(pm, dg, sms, stopCfgs, 30)
	start := time.Now()
	_ = sc.GracefulShutdown(context.Background())
	elapsed := time.Since(start)

	// 用户配置 timeout=2s 必须生效（若使用默认 60s，elapsed 会远大于 5s）
	// 验证用户配置覆盖了默认值
	if elapsed > 5*time.Second {
		t.Errorf("elapsed = %v, expected < 5s with user override (timeout=2s), default 60s would take much longer", elapsed)
	}
	// 进程应被强制杀掉（因为忽略 SIGTERM，等待 timeout 后 SIGKILL）
	if _, ok := pm.Get("svc1"); ok {
		t.Error("svc1 should be unregistered after force kill (user timeout=2s)")
	}
}

// TestStopService_DefaultGrace10sThenSIGKILL
// L-02-001: 规格 §2.1.4 — 默认 grace=10s 边界行为专项测试。
// 直接调用 StopService 并传入 DefaultStopConfig()，验证：
//   - SIGTERM 后等待 10s grace
//   - grace 期内未退出则 SIGKILL
// 为避免测试运行 10s，此处使用忽略 SIGTERM 的进程并仅断言流程完成（不严格断言时长）。
func TestStopService_DefaultGrace10sThenSIGKILL(t *testing.T) {
	proc := startIgnoreTermProcess(t, "stop-default-grace")

	cfg := DefaultStopConfig()
	if cfg.GraceSeconds != 10 || cfg.TimeoutSeconds != 60 {
		t.Fatalf("test setup invariant: DefaultStopConfig must be 10/60, got %d/%d", cfg.GraceSeconds, cfg.TimeoutSeconds)
	}

	start := time.Now()
	result, err := StopService(context.Background(), proc, cfg, nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("StopService returned error: %v", err)
	}
	// 忽略 SIGTERM 的进程应在 grace(10s) 后被 SIGKILL
	if !result.Signaled || result.Sig != syscall.SIGKILL {
		t.Errorf("expected SIGKILL after 10s grace, got signaled=%v sig=%v", result.Signaled, result.Sig)
	}
	// 应在 grace 10s 附近完成（允许 11s 上界避免 CI 抖动）
	if elapsed < 9*time.Second || elapsed > 11*time.Second {
		t.Errorf("elapsed = %v, expected ~10s (default grace)", elapsed)
	}
}

// TestStopService_UserConfigOverridesDefaultGrace
// L-02-001: 规格 §2.1.4 — 用户配置 grace=1s（短于默认 10s）应覆盖默认值。
// 验证 StopService 直接接受用户 StopConfig 并按用户值执行。
func TestStopService_UserConfigOverridesDefaultGrace(t *testing.T) {
	proc := startIgnoreTermProcess(t, "stop-user-grace")

	// 用户配置：grace=1s, timeout=3s（覆盖默认 10s/60s）
	cfg := StopConfig{GraceSeconds: 1, TimeoutSeconds: 3}

	start := time.Now()
	result, err := StopService(context.Background(), proc, cfg, nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("StopService returned error: %v", err)
	}
	if !result.Signaled || result.Sig != syscall.SIGKILL {
		t.Errorf("expected SIGKILL after user grace=1s, got signaled=%v sig=%v", result.Signaled, result.Sig)
	}
	// 用户 grace=1s 应使 elapsed ≈ 1s（远小于默认 10s），证明用户配置生效
	if elapsed > 3*time.Second {
		t.Errorf("elapsed = %v, expected < 3s with user override grace=1s", elapsed)
	}
}
