package api

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/core"
	"github.com/supdorg/supd/internal/watch"
)

// newTestOperator 构造一个最小可用的 CoreServiceOperator（不启动真实进程）
func newTestOperator(logDir string) *CoreServiceOperator {
	return &CoreServiceOperator{
		ProcessMgr:   core.NewProcessManager(),
		StateMachines: make(map[string]*core.StateMachine),
		Discovery:    &watch.DiscoveryResult{Services: map[string]*watch.ServiceEntry{}},
		Config:       &config.Config{},
		LogDir:       logDir,
	}
}

func svcEntry(name, configPath string) *watch.ServiceEntry {
	return &watch.ServiceEntry{
		Name:       name,
		ConfigPath: configPath,
		Config:     &config.ServiceConfig{Name: name},
	}
}

// TestStartService_NotFound 服务不存在应返回错误
func TestStartService_NotFound(t *testing.T) {
	o := newTestOperator(t.TempDir())
	if err := o.StartService("nope"); err == nil {
		t.Fatal("expected error for nonexistent service")
	}
}

// TestStartService_NoStateMachine 有服务但无状态机应返回错误
func TestStartService_NoStateMachine(t *testing.T) {
	o := newTestOperator(t.TempDir())
	o.Discovery.Services["svc"] = svcEntry("svc", "/tmp/svc.yaml")
	if err := o.StartService("svc"); err == nil {
		t.Fatal("expected error for missing state machine")
	}
}

// TestStartService_AlreadyRunning 已在运行（up/ready/starting）应拒绝重复启动
func TestStartService_AlreadyRunning(t *testing.T) {
	o := newTestOperator(t.TempDir())
	o.Discovery.Services["svc"] = svcEntry("svc", "/tmp/svc.yaml")
	sm := core.NewStateMachine()
	defer sm.Close()
	sm.ResetTo(core.StateUp)
	o.StateMachines["svc"] = sm

	if err := o.StartService("svc"); err == nil {
		t.Fatal("expected error for already running service")
	}
}

// TestStartService_AlreadyStarting 处于 starting 也应拒绝
func TestStartService_AlreadyStarting(t *testing.T) {
	o := newTestOperator(t.TempDir())
	o.Discovery.Services["svc"] = svcEntry("svc", "/tmp/svc.yaml")
	sm := core.NewStateMachine()
	defer sm.Close()
	sm.ResetTo(core.StateStarting)
	o.StateMachines["svc"] = sm

	if err := o.StartService("svc"); err == nil {
		t.Fatal("expected error for already starting service")
	}
}

// TestStopService_NoProcess 进程不存在且无退避 cancel 时应安全返回 nil
func TestStopService_NoProcess(t *testing.T) {
	o := newTestOperator(t.TempDir())
	o.Discovery.Services["svc"] = svcEntry("svc", "/tmp/svc.yaml")
	sm := core.NewStateMachine()
	defer sm.Close()
	o.StateMachines["svc"] = sm

	if err := o.StopService("svc"); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

// TestStopService_NoProcessWithCancelFunc 无进程但在退避等待时应取消 cancel 并转 down
func TestStopService_NoProcessWithCancelFunc(t *testing.T) {
	o := newTestOperator(t.TempDir())
	o.Discovery.Services["svc"] = svcEntry("svc", "/tmp/svc.yaml")
	sm := core.NewStateMachine()
	defer sm.Close()
	sm.ResetTo(core.StateStarting)
	o.StateMachines["svc"] = sm

	called := make(chan struct{}, 1)
	o.SetCancelFuncs(map[string]context.CancelFunc{
		"svc": func() { called <- struct{}{} },
	})

	if err := o.StopService("svc"); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	select {
	case <-called:
	default:
		t.Fatal("expected cancel func to be called for backoff-waiting service")
	}
	if sm.Current() != core.StateDown {
		t.Fatalf("expected state down after stop during backoff, got %s", sm.Current())
	}
}

// TestForceStopService_NoProcess 无进程无退避时应安全返回 nil
func TestForceStopService_NoProcess(t *testing.T) {
	o := newTestOperator(t.TempDir())
	o.Discovery.Services["svc"] = svcEntry("svc", "/tmp/svc.yaml")
	sm := core.NewStateMachine()
	defer sm.Close()
	o.StateMachines["svc"] = sm

	if err := o.ForceStopService("svc"); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

// TestRestartService_NotFound RestartService 在 Stop 成功后 Start 失败应转发错误
func TestRestartService_NotFound(t *testing.T) {
	o := newTestOperator(t.TempDir())
	if err := o.RestartService("nope"); err == nil {
		t.Fatal("expected wrapped error from StartService")
	}
}

// TestClearFailedState_NotFound 无状态机应报错
func TestClearFailedState_NotFound(t *testing.T) {
	o := newTestOperator(t.TempDir())
	if err := o.ClearFailedState("nope"); err == nil {
		t.Fatal("expected error for missing state machine")
	}
}

// TestClearFailedState_NotFailed 非 failed 状态应报错
func TestClearFailedState_NotFailed(t *testing.T) {
	o := newTestOperator(t.TempDir())
	sm := core.NewStateMachine()
	defer sm.Close()
	sm.ResetTo(core.StateUp)
	o.StateMachines["svc"] = sm

	if err := o.ClearFailedState("svc"); err == nil {
		t.Fatal("expected error for non-failed state")
	}
}

// TestClearFailedState_FailedToPending failed 状态应重置为 pending
func TestClearFailedState_FailedToPending(t *testing.T) {
	o := newTestOperator(t.TempDir())
	sm := core.NewStateMachine()
	defer sm.Close()
	sm.ResetTo(core.StateFailed)
	o.StateMachines["svc"] = sm

	if err := o.ClearFailedState("svc"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if sm.Current() != core.StatePending {
		t.Fatalf("expected pending after clear, got %s", sm.Current())
	}
}

// TestSendSignal_NoProcess 无进程时应返回错误
func TestSendSignal_NoProcess(t *testing.T) {
	o := newTestOperator(t.TempDir())
	if err := o.SendSignal("nope", syscall.SIGTERM); err == nil {
		t.Fatal("expected error for nonexistent process")
	}
}

// TestSetDiscovery 热重载时更新 Discovery 引用
func TestSetDiscovery(t *testing.T) {
	o := newTestOperator(t.TempDir())
	d := &watch.DiscoveryResult{Services: map[string]*watch.ServiceEntry{}}
	o.SetDiscovery(d)
	if o.Discovery != d {
		t.Fatal("Discovery should be updated by SetDiscovery")
	}
	// nil 防御
	o.SetDiscovery(nil) // 不应 panic 或更改
	if o.Discovery != d {
		t.Fatal("SetDiscovery(nil) should be a no-op")
	}
}

// TestUpdateRestartEngines 热重载更新引擎配置不 panic 且保留引擎
func TestUpdateRestartEngines(t *testing.T) {
	o := newTestOperator(t.TempDir())
	o.RestartEngines = make(map[string]*core.RestartEngine)
	svcConfig := &config.ServiceConfig{Name: "svc"}
	o.RestartEngines["svc"] = core.BuildRestartEngine(o.Config, svcConfig)
	disc := &watch.DiscoveryResult{
		Services: map[string]*watch.ServiceEntry{
			"svc": {Name: "svc", Config: svcConfig},
		},
	}
	o.UpdateRestartEngines(o.Config, disc)
	if _, ok := o.RestartEngines["svc"]; !ok {
		t.Fatal("engine should be retained after UpdateRestartEngines")
	}
}

// TestWriteServiceLog_Fallback 无 ServiceLogger 时回退写入日志文件
// 日志路径为 <LogDir>/services/<name>/current
func TestWriteServiceLog_Fallback(t *testing.T) {
	logDir := t.TempDir()
	o := newTestOperator(logDir)
	o.writeServiceLog("svc-x", "error", "boom")

	logPath := filepath.Join(logDir, "services", "svc-x", "current")
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("expected fallback log file at %s: %v", logPath, err)
	}
}
