package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/supdorg/supd/internal/config"
)

// setupTestDir 创建测试所需的临时目录和最小 config.yaml
func setupTestDir(t *testing.T) (baseDir, logDir string) {
	t.Helper()
	baseDir = t.TempDir()
	logDir = t.TempDir()

	// 创建目录结构
	os.MkdirAll(filepath.Join(baseDir, "services"), 0755)
	os.MkdirAll(filepath.Join(logDir, "services"), 0755)

	// 写入最小 config.yaml
	configContent := "settings:\n  auth_mode: none\n"
	if err := os.WriteFile(filepath.Join(baseDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	return baseDir, logDir
}

// writeService 在 baseDir/services/<name>/ 下创建 service.yaml
func writeService(t *testing.T, baseDir, name, content string) {
	t.Helper()
	svcDir := filepath.Join(baseDir, "services", name)
	if err := os.MkdirAll(svcDir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", svcDir, err)
	}
	if err := os.WriteFile(filepath.Join(svcDir, "service.yaml"), []byte(content), 0644); err != nil {
		t.Fatalf("write service.yaml for %s: %v", name, err)
	}
}

// cleanupBootstrap 清理 Bootstrap 启动后的资源（进程、watcher、状态机、日志器）
// L-03-001/M-02-001 修复：必须先取消 supervisor goroutine 的 context，
// 防止 KillProcessGroup 后 supervisor 触发自动重启创建新进程/新日志文件，
// 导致 t.TempDir() 清理时 "directory not empty" 失败。
func cleanupBootstrap(t *testing.T, result *BootstrapResult) {
	t.Helper()
	if result == nil {
		return
	}
	// 1. 先取消所有 supervisor goroutine 的 context，使其在 proc.Wait() 返回后
	//    不进入重启退避等待，直接退出，避免创建新进程或新日志文件
	for _, cancel := range result.CancelFuncs {
		cancel()
	}
	// 2. 杀进程（解除 supervisor goroutine 的 proc.Wait() 阻塞）
	if result.ProcessMgr != nil {
		for _, name := range result.ProcessMgr.List() {
			result.ProcessMgr.KillProcessGroup(name)
		}
	}
	// 3. 等待 supervisor goroutine 处理完取消信号并退出
	//    （proc.Wait() 返回 → 检查 ctx.Done() → 返回）
	time.Sleep(300 * time.Millisecond)
	// 4. 关闭其他资源
	if result.Watcher != nil {
		result.Watcher.Stop()
	}
	for _, sm := range result.StateMachines {
		sm.Close()
	}
	for _, logger := range result.Loggers {
		logger.Close()
	}
}

// TestBootstrap_FullStartup 测试完整启动流程
// REQ-F-033: 11步启动流程
func TestBootstrap_FullStartup(t *testing.T) {
	baseDir, logDir := setupTestDir(t)

	writeService(t, baseDir, "svc1", `name: svc1
version: "1.0"
command:
  - sleep
  - "60"
`)
	writeService(t, baseDir, "svc2", `name: svc2
version: "1.0"
command:
  - sleep
  - "60"
`)

	cfg := BootstrapConfig{
		ConfigPath: filepath.Join(baseDir, "config.yaml"),
		BaseDir:    baseDir,
		LogDir:     logDir,
	}

	b := NewBootstrap(cfg)
	result, err := b.Run(context.Background())
	if err != nil {
		t.Fatalf("Bootstrap.Run() error = %v", err)
	}
	defer cleanupBootstrap(t, result)

	// 验证：2个状态机
	if len(result.StateMachines) != 2 {
		t.Errorf("expected 2 state machines, got %d", len(result.StateMachines))
	}

	// 验证：svc1 在 up 状态（无 readiness 配置）
	if sm, ok := result.StateMachines["svc1"]; ok {
		if state := sm.Current(); state != StateUp {
			t.Errorf("svc1 state = %v, want %v", state, StateUp)
		}
	} else {
		t.Error("svc1 state machine not found")
	}

	// 验证：svc2 在 up 状态
	if sm, ok := result.StateMachines["svc2"]; ok {
		if state := sm.Current(); state != StateUp {
			t.Errorf("svc2 state = %v, want %v", state, StateUp)
		}
	} else {
		t.Error("svc2 state machine not found")
	}

	// 验证：2个进程已注册
	procs := result.ProcessMgr.List()
	if len(procs) != 2 {
		t.Errorf("expected 2 processes, got %d", len(procs))
	}

	// 验证：watcher 已启动
	if result.Watcher == nil {
		t.Error("watcher should not be nil")
	}

	// 验证：config 已加载
	if result.Config == nil {
		t.Error("config should not be nil")
	}

	// 验证：discovery 结果存在
	if result.Discovery == nil {
		t.Error("discovery should not be nil")
	}

	// 验证：依赖图存在
	if result.DepGraph == nil {
		t.Error("dep graph should not be nil")
	}

	// 验证：日志器已创建
	if len(result.Loggers) != 2 {
		t.Errorf("expected 2 loggers, got %d", len(result.Loggers))
	}
}

// TestBootstrap_AutostartFalse 测试 autostart=false 的服务不被启动
// REQ-F-034: autostart=false 的服务保持 down 状态
func TestBootstrap_AutostartFalse(t *testing.T) {
	baseDir, logDir := setupTestDir(t)

	writeService(t, baseDir, "auto-svc", `name: auto-svc
version: "1.0"
command:
  - sleep
  - "60"
`)
	writeService(t, baseDir, "manual-svc", `name: manual-svc
version: "1.0"
autostart: false
command:
  - sleep
  - "60"
`)

	cfg := BootstrapConfig{
		ConfigPath: filepath.Join(baseDir, "config.yaml"),
		BaseDir:    baseDir,
		LogDir:     logDir,
	}

	b := NewBootstrap(cfg)
	result, err := b.Run(context.Background())
	if err != nil {
		t.Fatalf("Bootstrap.Run() error = %v", err)
	}
	defer cleanupBootstrap(t, result)

	// 验证：auto-svc 已启动（up 状态）
	if sm, ok := result.StateMachines["auto-svc"]; ok {
		if state := sm.Current(); state != StateUp {
			t.Errorf("auto-svc state = %v, want %v", state, StateUp)
		}
	} else {
		t.Error("auto-svc state machine not found")
	}

	// 验证：manual-svc 未启动（非 up/ready 状态）
	if sm, ok := result.StateMachines["manual-svc"]; ok {
		if state := sm.Current(); state == StateUp || state == StateReady {
			t.Errorf("manual-svc should not be started, state = %v", state)
		}
	} else {
		t.Error("manual-svc state machine not found")
	}

	// 验证：只有 auto-svc 有进程
	procs := result.ProcessMgr.List()
	if len(procs) != 1 {
		t.Errorf("expected 1 process, got %d: %v", len(procs), procs)
	}
}

// TestBootstrap_DependencyOrder 测试依赖图排序（先启动被依赖的服务）
// REQ-F-033: 按 autostart + 依赖图启动服务（同层并行，跨层等待 ready）
func TestBootstrap_DependencyOrder(t *testing.T) {
	baseDir, logDir := setupTestDir(t)

	writeService(t, baseDir, "base-svc", `name: base-svc
version: "1.0"
command:
  - sleep
  - "60"
`)
	writeService(t, baseDir, "dep-svc", `name: dep-svc
version: "1.0"
command:
  - sleep
  - "60"
depends_on:
  - base-svc
`)

	cfg := BootstrapConfig{
		ConfigPath: filepath.Join(baseDir, "config.yaml"),
		BaseDir:    baseDir,
		LogDir:     logDir,
	}

	b := NewBootstrap(cfg)
	result, err := b.Run(context.Background())
	if err != nil {
		t.Fatalf("Bootstrap.Run() error = %v", err)
	}
	defer cleanupBootstrap(t, result)

	// 验证：两个服务都已启动
	for _, name := range []string{"base-svc", "dep-svc"} {
		if sm, ok := result.StateMachines[name]; ok {
			if state := sm.Current(); state != StateUp {
				t.Errorf("%s state = %v, want %v", name, state, StateUp)
			}
		} else {
			t.Errorf("%s state machine not found", name)
		}
	}

	// 验证：依赖图拓扑排序正确
	layers, cycle := result.DepGraph.TopologicalSort()
	if len(cycle) > 0 {
		t.Errorf("unexpected cycle: %v", cycle)
	}
	if len(layers) < 2 {
		t.Errorf("expected at least 2 layers (base-svc first, dep-svc second), got %d layers", len(layers))
	}
	// base-svc 应在第1层，dep-svc 应在第2层
	foundBase := false
	foundDep := false
	for _, name := range layers[0] {
		if name == "base-svc" {
			foundBase = true
		}
	}
	for _, name := range layers[1] {
		if name == "dep-svc" {
			foundDep = true
		}
	}
	if !foundBase {
		t.Error("base-svc should be in layer 0")
	}
	if !foundDep {
		t.Error("dep-svc should be in layer 1")
	}
}

// TestBootstrap_ConfigError 测试配置错误的服务不影响其他
// REQ-F-033: 单个 service.yaml 解析失败 → 标记该项为"配置错误"状态，不影响其他服务
func TestBootstrap_ConfigError(t *testing.T) {
	baseDir, logDir := setupTestDir(t)

	writeService(t, baseDir, "good-svc", `name: good-svc
version: "1.0"
command:
  - sleep
  - "60"
`)

	// 创建配置错误的服务（无效 YAML）
	badSvcDir := filepath.Join(baseDir, "services", "bad-svc")
	if err := os.MkdirAll(badSvcDir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", badSvcDir, err)
	}
	if err := os.WriteFile(filepath.Join(badSvcDir, "service.yaml"), []byte("invalid: [yaml: content"), 0644); err != nil {
		t.Fatalf("write bad service.yaml: %v", err)
	}

	cfg := BootstrapConfig{
		ConfigPath: filepath.Join(baseDir, "config.yaml"),
		BaseDir:    baseDir,
		LogDir:     logDir,
	}

	b := NewBootstrap(cfg)
	result, err := b.Run(context.Background())
	if err != nil {
		t.Fatalf("Bootstrap.Run() error = %v", err)
	}
	defer cleanupBootstrap(t, result)

	// 验证：good-svc 已启动
	if sm, ok := result.StateMachines["good-svc"]; ok {
		if state := sm.Current(); state != StateUp {
			t.Errorf("good-svc state = %v, want %v", state, StateUp)
		}
	} else {
		t.Error("good-svc state machine not found")
	}

	// 验证：bad-svc 不在状态机中（配置解析失败未被添加到 Services map）
	if _, ok := result.StateMachines["bad-svc"]; ok {
		t.Error("bad-svc should not have a state machine (config parse error)")
	}

	// 验证：存在非致命错误
	if len(result.Errors) == 0 {
		t.Error("expected non-fatal errors for bad-svc config error")
	}
}

// TestBootstrap_CycleDependency 测试循环依赖的服务被跳过
// REQ-F-033: 循环依赖→跳过该服务，不影响其他
func TestBootstrap_CycleDependency(t *testing.T) {
	baseDir, logDir := setupTestDir(t)

	writeService(t, baseDir, "svc-a", `name: svc-a
version: "1.0"
command:
  - sleep
  - "60"
depends_on:
  - svc-b
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
`)

	cfg := BootstrapConfig{
		ConfigPath: filepath.Join(baseDir, "config.yaml"),
		BaseDir:    baseDir,
		LogDir:     logDir,
	}

	b := NewBootstrap(cfg)
	result, err := b.Run(context.Background())
	if err != nil {
		t.Fatalf("Bootstrap.Run() error = %v", err)
	}
	defer cleanupBootstrap(t, result)

	// 验证：svc-a 和 svc-b 未启动（循环依赖）
	for _, name := range []string{"svc-a", "svc-b"} {
		if sm, ok := result.StateMachines[name]; ok {
			if state := sm.Current(); state == StateUp || state == StateReady {
				t.Errorf("%s should not be started (cycle), state = %v", name, state)
			}
		}
	}

	// 验证：svc-c 已启动
	if sm, ok := result.StateMachines["svc-c"]; ok {
		if state := sm.Current(); state != StateUp {
			t.Errorf("svc-c state = %v, want %v", state, StateUp)
		}
	} else {
		t.Error("svc-c state machine not found")
	}

	// 验证：只有 svc-c 有进程
	procs := result.ProcessMgr.List()
	if len(procs) != 1 {
		t.Errorf("expected 1 process, got %d: %v", len(procs), procs)
	}
}

// TestBootstrap_ConfigParseFailure 测试 config.yaml 解析失败拒绝启动
// REQ-F-033: config.yaml 解析失败 → 拒绝启动
func TestBootstrap_ConfigParseFailure(t *testing.T) {
	baseDir := t.TempDir()
	logDir := t.TempDir()

	// 写入无效的 config.yaml
	if err := os.WriteFile(filepath.Join(baseDir, "config.yaml"), []byte("invalid: [yaml"), 0644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	cfg := BootstrapConfig{
		ConfigPath: filepath.Join(baseDir, "config.yaml"),
		BaseDir:    baseDir,
		LogDir:     logDir,
	}

	b := NewBootstrap(cfg)
	result, err := b.Run(context.Background())
	if err == nil {
		t.Error("expected error for invalid config.yaml")
		if result != nil {
			cleanupBootstrap(t, result)
		}
	}
}

// TestBootstrap_StopSignal 测试启动中收到停止信号
// REQ-F-033: 启动中收到 SIGTERM/SIGINT 时，立即停止拉起剩余服务
func TestBootstrap_StopSignal(t *testing.T) {
	baseDir, logDir := setupTestDir(t)

	// 创建一个有 readiness 检查的服务（检查一个无人监听的端口，会持续等待）
	writeService(t, baseDir, "slow-svc", `name: slow-svc
version: "1.0"
command:
  - sleep
  - "60"
readiness:
  type: tcp_check
  port: 59999
  interval_seconds: 1
  timeout_seconds: 30
`)
	// 还有一个无 readiness 的快速服务
	writeService(t, baseDir, "fast-svc", `name: fast-svc
version: "1.0"
command:
  - sleep
  - "60"
`)

	cfg := BootstrapConfig{
		ConfigPath: filepath.Join(baseDir, "config.yaml"),
		BaseDir:    baseDir,
		LogDir:     logDir,
	}

	b := NewBootstrap(cfg)

	doneCh := make(chan struct{})
	var result *BootstrapResult
	var runErr error

	go func() {
		result, runErr = b.Run(context.Background())
		close(doneCh)
	}()

	// 等待 bootstrap 开始服务启动
	time.Sleep(2 * time.Second)
	b.StopStartup()

	// 等待 bootstrap 完成
	select {
	case <-doneCh:
		if runErr == nil {
			t.Error("expected error when startup is interrupted by signal")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("bootstrap didn't complete after stop signal")
	}

	if result != nil {
		cleanupBootstrap(t, result)
	}
}

// TestBootstrap_ContextCancellation 测试 context 取消中断启动
// REQ-F-033: 启动中收到停止信号
func TestBootstrap_ContextCancellation(t *testing.T) {
	baseDir, logDir := setupTestDir(t)

	writeService(t, baseDir, "slow-svc", `name: slow-svc
version: "1.0"
command:
  - sleep
  - "60"
readiness:
  type: tcp_check
  port: 59998
  interval_seconds: 1
  timeout_seconds: 30
`)

	cfg := BootstrapConfig{
		ConfigPath: filepath.Join(baseDir, "config.yaml"),
		BaseDir:    baseDir,
		LogDir:     logDir,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	b := NewBootstrap(cfg)

	doneCh := make(chan struct{})
	var result *BootstrapResult
	var runErr error

	go func() {
		result, runErr = b.Run(ctx)
		close(doneCh)
	}()

	// 等待 bootstrap 开始服务启动
	time.Sleep(2 * time.Second)
	cancel()

	// 等待 bootstrap 完成
	select {
	case <-doneCh:
		if runErr == nil {
			t.Error("expected error when context is cancelled")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("bootstrap didn't complete after context cancellation")
	}

	if result != nil {
		cleanupBootstrap(t, result)
	}
}

// TestBootstrap_IsAutostart 测试 isAutostart 辅助函数
// REQ-F-034: autostart 为 nil 或 true → true，false → false
func TestBootstrap_IsAutostart(t *testing.T) {
	tr := true
	fl := false

	tests := []struct {
		name     string
		autostart *bool
		want     bool
	}{
		{"nil", nil, true},
		{"true", &tr, true},
		{"false", &fl, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &config.ServiceConfig{Autostart: tt.autostart}
			got := isAutostart(svc)
			if got != tt.want {
				t.Errorf("isAutostart() = %v, want %v", got, tt.want)
			}
		})
	}
}
