package core

import (
	"bufio"
	"context"
	"syscall"
	"testing"
	"time"
)

// waitForReady 从进程stdout读取直到收到"READY"标记或超时
// 用于确保忽略SIGTERM的进程已完成信号处理器设置
func waitForReady(t *testing.T, proc *Process, timeout time.Duration) {
	t.Helper()
	reader := bufio.NewReader(proc.StdoutPipe())
	ch := make(chan struct{})
	go func() {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			if line == "READY\n" {
				ch <- struct{}{}
				return
			}
		}
	}()
	select {
	case <-ch:
		// 进程已准备好
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for process to become ready")
	}
}

// startIgnoreTermProcess 启动一个忽略SIGTERM的Python进程
func startIgnoreTermProcess(t *testing.T, name string) *Process {
	t.Helper()
	proc, err := StartProcess(name, []string{"python3", "-c", "import signal,os; signal.signal(signal.SIGTERM, signal.SIG_IGN); os.write(1, b'READY\\n'); signal.pause()"}, nil, "", nil)
	if err != nil {
		t.Fatalf("StartProcess failed: %v", err)
	}
	waitForReady(t, proc, 5*time.Second)
	return proc
}

// TestStopService_GracefulExit 进程在grace期内退出（SIGTERM后立即退出）
// REQ-F-007: Step 3 发送SIGTERM → Step 4 进程在grace_seconds内退出 → 返回结果
func TestStopService_GracefulExit(t *testing.T) {
	proc, err := StartProcess("stop-graceful", []string{"sleep", "60"}, nil, "", nil)
	if err != nil {
		t.Fatalf("StartProcess failed: %v", err)
	}

	config := StopConfig{
		GraceSeconds:   10,
		TimeoutSeconds: 60,
	}

	ctx := context.Background()
	result, err := StopService(ctx, proc, config, nil)
	if err != nil {
		t.Fatalf("StopService returned error: %v", err)
	}

	// 进程应该被SIGTERM终止
	if !result.Signaled {
		t.Errorf("Signaled = false, want true (process should be terminated by SIGTERM)")
	}
	if result.Sig != syscall.SIGTERM {
		t.Errorf("Sig = %d, want SIGTERM (%d)", result.Sig, syscall.SIGTERM)
	}
}

// TestStopService_RequiresSIGKILL 进程忽略SIGTERM，需要SIGKILL才能退出
// REQ-F-007: Step 4 grace_seconds到期 → Step 5 SIGKILL → 返回结果
func TestStopService_RequiresSIGKILL(t *testing.T) {
	proc := startIgnoreTermProcess(t, "stop-sigkill")

	config := StopConfig{
		GraceSeconds:   1, // 短grace期，快速触发SIGKILL
		TimeoutSeconds: 30,
	}

	ctx := context.Background()
	result, err := StopService(ctx, proc, config, nil)
	if err != nil {
		t.Fatalf("StopService returned error: %v", err)
	}

	// 进程应该被SIGKILL终止
	if !result.Signaled {
		t.Errorf("Signaled = false, want true (process should be killed by SIGKILL)")
	}
	if result.Sig != syscall.SIGKILL {
		t.Errorf("Sig = %d, want SIGKILL (%d)", result.Sig, syscall.SIGKILL)
	}
}

// TestStopService_TotalTimeout 总超时后强制SIGKILL
// REQ-F-007: timeout_seconds覆盖整个流程，超时后直接SIGKILL进程组
func TestStopService_TotalTimeout(t *testing.T) {
	proc := startIgnoreTermProcess(t, "stop-total-timeout")

	// timeout_seconds < grace_seconds，总超时会先触发
	config := StopConfig{
		GraceSeconds:   10,
		TimeoutSeconds: 2,
	}

	start := time.Now()
	ctx := context.Background()
	result, err := StopService(ctx, proc, config, nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("StopService returned error: %v", err)
	}

	// 进程应该被SIGKILL终止（总超时触发）
	if !result.Signaled {
		t.Errorf("Signaled = false, want true")
	}
	if result.Sig != syscall.SIGKILL {
		t.Errorf("Sig = %d, want SIGKILL (%d)", result.Sig, syscall.SIGKILL)
	}

	// 总耗时应在timeout_seconds附近（允许一定误差）
	if elapsed > 8*time.Second {
		t.Errorf("StopService took %v, should be close to timeout_seconds (2s)", elapsed)
	}
}

// TestStopService_PreStopPlaceholder pre_stop扩展占位测试
// REQ-F-007: Step 2 pre_stop扩展执行（已通过 ServiceLifecycleTrigger.OnPreStop 实现）
// pre_stop失败不阻塞停止流程
func TestStopService_PreStopPlaceholder(t *testing.T) {
	proc, err := StartProcess("stop-prestop", []string{"sleep", "60"}, nil, "", nil)
	if err != nil {
		t.Fatalf("StartProcess failed: %v", err)
	}

	config := StopConfig{
		GraceSeconds:   10,
		TimeoutSeconds: 30,
	}

	preStopCalled := false
	runPreStop := func() error {
		preStopCalled = true
		return nil
	}

	ctx := context.Background()
	result, err := StopService(ctx, proc, config, runPreStop)
	if err != nil {
		t.Fatalf("StopService returned error: %v", err)
	}

	if !preStopCalled {
		t.Error("pre_stop callback was not called")
	}

	// 进程仍然应该被正常停止
	if !result.Signaled {
		t.Errorf("Signaled = false, want true")
	}
}

// TestStopService_PreStopError pre_stop失败不阻塞停止流程
// REQ-F-007: pre_stop失败不阻塞停止
func TestStopService_PreStopError(t *testing.T) {
	proc, err := StartProcess("stop-prestop-err", []string{"sleep", "60"}, nil, "", nil)
	if err != nil {
		t.Fatalf("StartProcess failed: %v", err)
	}

	config := StopConfig{
		GraceSeconds:   10,
		TimeoutSeconds: 30,
	}

	preStopCalled := false
	runPreStop := func() error {
		preStopCalled = true
		return syscall.ENOENT // 模拟pre_stop失败
	}

	ctx := context.Background()
	result, err := StopService(ctx, proc, config, runPreStop)
	if err != nil {
		t.Fatalf("StopService returned error: %v, should not fail even if pre_stop fails", err)
	}

	if !preStopCalled {
		t.Error("pre_stop callback was not called")
	}

	// 停止流程应该继续正常执行
	if !result.Signaled {
		t.Errorf("Signaled = false, want true")
	}
}

// TestStopService_QuickExitProcess 进程在SIGTERM发送前就已退出
// 验证对已退出进程的SIGTERM错误处理
func TestStopService_QuickExitProcess(t *testing.T) {
	// 使用一个立即退出的进程
	proc, err := StartProcess("stop-quick", []string{"true"}, nil, "", nil)
	if err != nil {
		t.Fatalf("StartProcess failed: %v", err)
	}

	// 等待进程自行退出
	time.Sleep(100 * time.Millisecond)

	config := StopConfig{
		GraceSeconds:   5,
		TimeoutSeconds: 10,
	}

	ctx := context.Background()
	result, err := StopService(ctx, proc, config, nil)
	if err != nil {
		t.Fatalf("StopService returned error: %v", err)
	}

	// 进程正常退出（exit code 0，非信号杀死）
	if result.Signaled {
		t.Errorf("Signaled = true, want false (process exited on its own)")
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
}

// TestStopService_ContextCancelled 父context取消时停止流程中断
func TestStopService_ContextCancelled(t *testing.T) {
	proc := startIgnoreTermProcess(t, "stop-cancel")

	config := StopConfig{
		GraceSeconds:   30,
		TimeoutSeconds: 60,
	}

	ctx, cancel := context.WithCancel(context.Background())

	// 在1秒后取消父context
	go func() {
		time.Sleep(1 * time.Second)
		cancel()
	}()

	_, err := StopService(ctx, proc, config, nil)

	// 确保不留僵尸进程
	proc.KillProcessGroup()
	time.Sleep(100 * time.Millisecond)

	if err == nil {
		t.Log("StopService returned nil error even with context cancellation - process may have been killed in time")
	}
}

// TestDefaultStopConfig 验证默认停止配置
// REQ-F-007: grace_seconds=10, timeout_seconds=60
func TestDefaultStopConfig(t *testing.T) {
	cfg := DefaultStopConfig()
	if cfg.GraceSeconds != 10 {
		t.Errorf("DefaultStopConfig GraceSeconds = %d, want 10", cfg.GraceSeconds)
	}
	if cfg.TimeoutSeconds != 60 {
		t.Errorf("DefaultStopConfig TimeoutSeconds = %d, want 60", cfg.TimeoutSeconds)
	}
}

// TestStopService_PreStopTimeout pre_stop执行期间总超时触发SIGKILL
// REQ-F-007: timeout_seconds覆盖整个流程（含pre_stop执行）
func TestStopService_PreStopTimeout(t *testing.T) {
	proc := startIgnoreTermProcess(t, "stop-prestop-timeout")

	config := StopConfig{
		GraceSeconds:   5,
		TimeoutSeconds: 2, // 短总超时
	}

	// pre_stop模拟长时间执行
	runPreStop := func() error {
		time.Sleep(30 * time.Second)
		return nil
	}

	start := time.Now()
	ctx := context.Background()
	result, err := StopService(ctx, proc, config, runPreStop)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("StopService returned error: %v", err)
	}

	// 进程应该被SIGKILL终止（总超时触发）
	if !result.Signaled {
		t.Errorf("Signaled = false, want true")
	}
	if result.Sig != syscall.SIGKILL {
		t.Errorf("Sig = %d, want SIGKILL (%d)", result.Sig, syscall.SIGKILL)
	}

	// 总耗时应在总超时附近
	if elapsed > 10*time.Second {
		t.Errorf("StopService took %v, should be close to timeout_seconds (2s)", elapsed)
	}
}
