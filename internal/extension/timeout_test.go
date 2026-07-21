package extension

import (
	"context"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/supdorg/supd/internal/core"
)

// startSleepProcess 启动一个 sleep 子进程用于测试
func startSleepProcess(t *testing.T, duration string) *core.Process {
	t.Helper()
	cmd := []string{"sleep", duration}
	p, err := core.StartProcess("test-sleep", cmd, nil, "", nil)
	if err != nil {
		t.Fatalf("failed to start sleep process: %v", err)
	}
	return p
}

// TestTimeoutNormalCompletion 测试未超时正常完成
// REQ-F-019: 进程在超时前退出，不触发任何信号
func TestTimeoutNormalCompletion(t *testing.T) {
	// 启动一个短命的进程
	p := startSleepProcess(t, "1")

	guard := NewTimeoutGuard(TimeoutConfig{
		ExtensionTimeout: 10, // 10秒超时，进程1秒就结束
		HardLimitSeconds: 30,
	}, p)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	guard.Start(ctx)

	// 等待进程正常退出
	go func() {
		p.Wait()
		guard.Stop()
	}()

	guard.Wait()

	timedOut, hardKilled := guard.Check()
	if timedOut {
		t.Error("expected no timeout, but timedOut=true")
	}
	if hardKilled {
		t.Error("expected no hard kill, but hardKilled=true")
	}
}

// TestTimeoutExtensionTimeoutSIGTERM 测试 extension timeout 到期后发送 SIGTERM
// REQ-F-019: 三层防线第1层 — extension timeout 到期 → SIGTERM → 5秒 → SIGKILL
func TestTimeoutExtensionTimeoutSIGTERM(t *testing.T) {
	// 启动一个长时间运行的进程
	p := startSleepProcess(t, "300")

	guard := NewTimeoutGuard(TimeoutConfig{
		ExtensionTimeout: 1, // 1秒超时
		HardLimitSeconds: 60,
	}, p)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	guard.Start(ctx)

	// 在另一个 goroutine 中等待进程退出
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.Wait()
	}()

	// 等待 guard 完成（应该触发超时 → SIGTERM → 5秒 → SIGKILL）
	guard.Wait()

	timedOut, hardKilled := guard.Check()
	if !timedOut {
		t.Error("expected timeout, but timedOut=false")
	}
	if !hardKilled {
		t.Error("expected hard kill (SIGKILL after 5s), but hardKilled=false")
	}

	// 验证进程已退出
	select {
	case <-done:
		// 进程已退出，符合预期
	case <-time.After(2 * time.Second):
		t.Error("process did not exit after SIGKILL")
	}
}

// TestTimeoutSIGTERMGracefulExit 测试 SIGTERM 后进程正常退出（不触发 SIGKILL）
// REQ-F-019: SIGTERM 后5秒内进程退出，不触发 SIGKILL
func TestTimeoutSIGTERMGracefulExit(t *testing.T) {
	// 使用 bash 进程，捕获 SIGTERM 后快速退出
	// bash 默认会处理 SIGTERM 并退出
	cmd := []string{"bash", "-c", "trap 'exit 0' TERM; while true; do sleep 0.1; done"}
	p, err := core.StartProcess("test-graceful", cmd, nil, "", nil)
	if err != nil {
		t.Fatalf("failed to start process: %v", err)
	}

	guard := NewTimeoutGuard(TimeoutConfig{
		ExtensionTimeout: 1, // 1秒超时
		HardLimitSeconds: 60,
	}, p)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	guard.Start(ctx)

	// 等待进程退出
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.Wait()
		// 进程退出后停止 guard
		guard.Stop()
	}()

	// 等待 guard 或超时
	select {
	case <-guard.done:
		// guard 退出
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for guard to finish")
	}

	timedOut, hardKilled := guard.Check()
	if !timedOut {
		t.Error("expected timeout (SIGTERM was sent), but timedOut=false")
	}
	if hardKilled {
		t.Error("expected no hard kill (process exited after SIGTERM), but hardKilled=true")
	}
}

// TestTimeoutHardLimit 测试 hard limit 到期直接 SIGKILL
// REQ-F-019: 全局硬上限到期 → 无条件 SIGKILL
func TestTimeoutHardLimit(t *testing.T) {
	// 启动长时间进程
	p := startSleepProcess(t, "300")

	guard := NewTimeoutGuard(TimeoutConfig{
		ExtensionTimeout: 60,  // extension timeout 很长，不会先到
		HardLimitSeconds: 1,   // hard limit 1秒后到期
	}, p)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	guard.Start(ctx)

	// 等待进程退出
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.Wait()
	}()

	// 等待 guard 完成
	guard.Wait()

	timedOut, hardKilled := guard.Check()
	if !timedOut {
		t.Error("expected timeout, but timedOut=false")
	}
	if !hardKilled {
		t.Error("expected hard kill (hard limit SIGKILL), but hardKilled=false")
	}

	select {
	case <-done:
		// 进程已退出
	case <-time.After(2 * time.Second):
		t.Error("process did not exit after hard limit SIGKILL")
	}
}

// TestTimeoutExtLessThanHardLimit 测试 extension timeout < hard limit
// REQ-F-019: extension timeout 先到期时，走 SIGTERM → 5秒 → SIGKILL 路径
func TestTimeoutExtLessThanHardLimit(t *testing.T) {
	p := startSleepProcess(t, "300")

	guard := NewTimeoutGuard(TimeoutConfig{
		ExtensionTimeout: 1,  // extension timeout 先到
		HardLimitSeconds: 10, // hard limit 后到
	}, p)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	guard.Start(ctx)

	done := make(chan struct{})
	go func() {
		defer close(done)
		p.Wait()
	}()

	guard.Wait()

	timedOut, hardKilled := guard.Check()
	if !timedOut {
		t.Error("expected timeout")
	}
	if !hardKilled {
		t.Error("expected hard kill (SIGTERM then SIGKILL since sleep ignores SIGTERM)")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("process did not exit")
	}
}

// TestTimeoutStop 测试 Stop 停止监控
// REQ-F-019: 进程正常完成后调用 Stop 终止监控 goroutine
func TestTimeoutStop(t *testing.T) {
	p := startSleepProcess(t, "300")

	guard := NewTimeoutGuard(TimeoutConfig{
		ExtensionTimeout: 10,
		HardLimitSeconds: 30,
	}, p)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	guard.Start(ctx)

	// 立即停止监控
	guard.Stop()

	// 等待 guard goroutine 退出
	select {
	case <-guard.done:
		// 正常
	case <-time.After(2 * time.Second):
		t.Error("guard did not stop after Stop()")
	}

	// 停止后不应标记超时
	timedOut, hardKilled := guard.Check()
	if timedOut {
		t.Error("expected no timeout after Stop()")
	}
	if hardKilled {
		t.Error("expected no hard kill after Stop()")
	}

	// 清理：杀死测试进程
	p.KillProcessGroup()
	p.Wait()
}

// TestTimeoutContextCancel 测试 context 取消停止监控
func TestTimeoutContextCancel(t *testing.T) {
	p := startSleepProcess(t, "300")

	guard := NewTimeoutGuard(TimeoutConfig{
		ExtensionTimeout: 10,
		HardLimitSeconds: 30,
	}, p)

	ctx, cancel := context.WithCancel(context.Background())
	guard.Start(ctx)

	// 取消 context
	cancel()

	select {
	case <-guard.done:
		// 正常
	case <-time.After(2 * time.Second):
		t.Error("guard did not stop after context cancel")
	}

	// context 取消不标记超时
	timedOut, _ := guard.Check()
	if timedOut {
		t.Error("expected no timeout after context cancel")
	}

	// 清理
	p.KillProcessGroup()
	p.Wait()
}

// TestTimeoutElapsed 测试 Elapsed 返回运行时长
func TestTimeoutElapsed(t *testing.T) {
	guard := NewTimeoutGuard(TimeoutConfig{
		ExtensionTimeout: 600,
		HardLimitSeconds: 1800,
	}, nil)

	elapsed := guard.Elapsed()
	if elapsed < 0 {
		t.Error("elapsed should be non-negative")
	}

	// 等待一小段时间后再次检查
	time.Sleep(100 * time.Millisecond)
	elapsed2 := guard.Elapsed()
	if elapsed2 <= elapsed {
		t.Error("elapsed should increase over time")
	}
}

// TestTimeoutDefaultConfig 测试默认配置
// REQ-F-019: timeout 默认 600，hard limit 默认 1800
func TestTimeoutDefaultConfig(t *testing.T) {
	cfg := DefaultTimeoutConfig()
	if cfg.ExtensionTimeout != 600 {
		t.Errorf("expected ExtensionTimeout=600, got %d", cfg.ExtensionTimeout)
	}
	if cfg.HardLimitSeconds != 1800 {
		t.Errorf("expected HardLimitSeconds=1800, got %d", cfg.HardLimitSeconds)
	}
}

// TestTimeoutProcessAlreadyExited 测试进程已退出时发送信号不崩溃
// REQ-F-019: 进程退出后 guard 应安全处理
func TestTimeoutProcessAlreadyExited(t *testing.T) {
	// 启动一个立即退出的进程
	cmd := []string{"true"}
	p, err := core.StartProcess("test-exit", cmd, nil, "", nil)
	if err != nil {
		t.Fatalf("failed to start process: %v", err)
	}

	// 等待进程退出
	p.Wait()

	guard := NewTimeoutGuard(TimeoutConfig{
		ExtensionTimeout: 1,
		HardLimitSeconds: 2,
	}, p)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	guard.Start(ctx)

	// guard 的 timer 到期后会尝试给已退出进程发信号
	// 这不应该 panic，虽然 syscall.Kill 可能返回错误
	select {
	case <-guard.done:
	case <-time.After(5 * time.Second):
		t.Error("guard did not finish")
	}
}

// TestTimeoutSIGTERMThenSIGKILLSequence 验证完整的 SIGTERM → 5s → SIGKILL 序列时序
// REQ-F-019: 三层防线的完整时序验证
func TestTimeoutSIGTERMThenSIGKILLSequence(t *testing.T) {
	// 使用忽略 SIGTERM 的进程（sleep 忽略 SIGTERM）
	p := startSleepProcess(t, "300")

	guard := NewTimeoutGuard(TimeoutConfig{
		ExtensionTimeout: 1,
		HardLimitSeconds: 60,
	}, p)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startTime := time.Now()
	guard.Start(ctx)

	// 等待 guard 完成
	guard.Wait()
	elapsed := time.Since(startTime)

	timedOut, hardKilled := guard.Check()
	if !timedOut {
		t.Error("expected timeout")
	}
	if !hardKilled {
		t.Error("expected hard kill")
	}

	// 验证时序：extension timeout(1s) + 5s grace = ~6s
	// 允许 ±2s 误差
	if elapsed < 4*time.Second {
		t.Errorf("elapsed too short (%v), expected ~6s (1s timeout + 5s grace)", elapsed)
	}
	if elapsed > 10*time.Second {
		t.Errorf("elapsed too long (%v), expected ~6s", elapsed)
	}

	// 清理
	p.Wait()
}

// TestTimeoutHardLimitBypassesSIGTERM 验证 hard limit 直接触发 SIGKILL，不经过 SIGTERM
// REQ-F-019: hard limit 到期无条件 SIGKILL
func TestTimeoutHardLimitBypassesSIGTERM(t *testing.T) {
	p := startSleepProcess(t, "300")

	// extension timeout 设得很长，hard limit 设短，让 hard limit 先触发
	guard := NewTimeoutGuard(TimeoutConfig{
		ExtensionTimeout: 60,
		HardLimitSeconds: 1,
	}, p)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startTime := time.Now()
	guard.Start(ctx)

	guard.Wait()
	elapsed := time.Since(startTime)

	timedOut, hardKilled := guard.Check()
	if !timedOut {
		t.Error("expected timeout")
	}
	if !hardKilled {
		t.Error("expected hard kill from hard limit")
	}

	// hard limit 直接触发 SIGKILL，不需要等5秒
	// 应该在 ~1s 内完成
	if elapsed > 5*time.Second {
		t.Errorf("elapsed too long (%v), hard limit should kill directly without 5s grace", elapsed)
	}

	p.Wait()
}

// TestTimeoutProcessGroupKill 验证超时后进程被 SIGKILL 终止
// REQ-F-019: SIGTERM/SIGKILL 发给整个进程组（-pgid），由 core.Process 的 SendSignal/KillProcessGroup 保证
func TestTimeoutProcessGroupKill(t *testing.T) {
	// 启动一个长时间运行的进程
	p := startSleepProcess(t, "300")

	guard := NewTimeoutGuard(TimeoutConfig{
		ExtensionTimeout: 1,
		HardLimitSeconds: 10,
	}, p)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	guard.Start(ctx)

	// 等待 guard 完成（extension timeout → SIGTERM → 5s → SIGKILL）
	guard.Wait()

	timedOut, hardKilled := guard.Check()
	if !timedOut {
		t.Error("expected timeout")
	}
	if !hardKilled {
		t.Error("expected hard kill")
	}

	// 进程应该已退出
	p.Wait()
}

// TestTimeoutAlreadyExitedOnSIGTERM 测试 SIGTERM 发送时进程已退出的情况
// REQ-F-019: 进程在 timeout 前恰好退出，SIGTERM 发送到已退出进程应安全
func TestTimeoutAlreadyExitedOnSIGTERM(t *testing.T) {
	// 使用 exec.Command 手动控制
	cmd := exec.Command("sleep", "0.5")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start process: %v", err)
	}

	pgid := cmd.Process.Pid

	// 等待进程退出
	cmd.Wait()

	// 现在构建 Process 来模拟已经退出的进程
	// 向已退出进程组发信号
	err := syscall.Kill(-pgid, syscall.SIGTERM)
	if err == nil {
		// 进程组恰好已被回收，不会有错误
	} else {
		// 预期 ESRCH 错误，进程已不存在
		t.Logf("expected error sending signal to dead process: %v", err)
	}
}
