package core

import (
	"io"
	"os"
	"syscall"
	"testing"
	"time"
)

// startAndWaitHelper 启动进程并在后台 goroutine 中调用 Wait（模拟僵尸进程回收）
// 返回 Process 和 Wait 结果 channel
func startAndWaitHelper(t *testing.T, name string, command []string) (*Process, <-chan waitResult) {
	t.Helper()
	proc, err := StartProcess(name, command, nil, "", nil)
	if err != nil {
		t.Fatalf("StartProcess failed: %v", err)
	}
	resultCh := make(chan waitResult, 1)
	go func() {
		exitCode, signaled, sig := proc.Wait()
		resultCh <- waitResult{exitCode: exitCode, signaled: signaled, sig: sig}
	}()
	return proc, resultCh
}

// TestStartProcess_Basic 启动子进程并验证基本属性
func TestStartProcess_Basic(t *testing.T) {
	proc, resultCh := startAndWaitHelper(t, "test-sleep", []string{"sleep", "60"})

	// 验证 PID > 0
	if proc.PID() <= 0 {
		t.Errorf("PID = %d, want > 0", proc.PID())
	}

	// 验证 PGID == PID（Setpgid=true 时 PGID 等于 PID）
	if proc.PGID() != proc.PID() {
		t.Errorf("PGID = %d, want %d (same as PID)", proc.PGID(), proc.PID())
	}

	// 验证进程确实在运行
	if err := syscall.Kill(proc.PID(), 0); err != nil {
		t.Errorf("process not running: %v", err)
	}

	// 清理：杀死进程
	if err := proc.KillProcessGroup(); err != nil {
		t.Fatalf("KillProcessGroup failed: %v", err)
	}

	// 等待 Wait goroutine 返回
	select {
	case r := <-resultCh:
		if !r.signaled || r.sig != syscall.SIGKILL {
			t.Errorf("Wait result: signaled=%v sig=%d, want signaled=true sig=SIGKILL", r.signaled, r.sig)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Wait goroutine did not return within timeout")
	}
}

// TestStartProcess_StdinNil 验证 stdin 为 nil
func TestStartProcess_StdinNil(t *testing.T) {
	_, resultCh := startAndWaitHelper(t, "test-echo", []string{"echo", "hello"})

	// echo 立即退出（stdin 已关闭，不会等待输入）
	select {
	case <-resultCh:
		// 预期：echo 命令立即退出
	case <-time.After(5 * time.Second):
		t.Fatal("echo process should have exited immediately (stdin closed)")
	}
}

// TestProcess_Wait_NormalExit 验证 Wait 返回正确的退出码
func TestProcess_Wait_NormalExit(t *testing.T) {
	proc, err := StartProcess("test-exit0", []string{"true"}, nil, "", nil)
	if err != nil {
		t.Fatalf("StartProcess failed: %v", err)
	}

	exitCode, signaled, sig := proc.Wait()
	if exitCode != 0 {
		t.Errorf("exitCode = %d, want 0", exitCode)
	}
	if signaled {
		t.Errorf("signaled = true, want false")
	}
	if sig != 0 {
		t.Errorf("sig = %d, want 0", sig)
	}
}

// TestProcess_Wait_NonZeroExit 验证 Wait 返回非零退出码
func TestProcess_Wait_NonZeroExit(t *testing.T) {
	proc, err := StartProcess("test-exit1", []string{"false"}, nil, "", nil)
	if err != nil {
		t.Fatalf("StartProcess failed: %v", err)
	}

	exitCode, signaled, sig := proc.Wait()
	if exitCode != 1 {
		t.Errorf("exitCode = %d, want 1", exitCode)
	}
	if signaled {
		t.Errorf("signaled = true, want false")
	}
	if sig != 0 {
		t.Errorf("sig = %d, want 0", sig)
	}
}

// TestProcess_Wait_Signaled 验证 Wait 返回被信号杀死的状态
func TestProcess_Wait_Signaled(t *testing.T) {
	proc, err := StartProcess("test-sleep", []string{"sleep", "60"}, nil, "", nil)
	if err != nil {
		t.Fatalf("StartProcess failed: %v", err)
	}

	// 发送 SIGKILL
	if err := proc.KillProcessGroup(); err != nil {
		t.Fatalf("KillProcessGroup failed: %v", err)
	}

	exitCode, signaled, sig := proc.Wait()
	if !signaled {
		t.Errorf("signaled = false, want true")
	}
	if sig != syscall.SIGKILL {
		t.Errorf("sig = %d, want SIGKILL (%d)", sig, syscall.SIGKILL)
	}
	if exitCode != -1 {
		t.Errorf("exitCode = %d, want -1 for signaled process", exitCode)
	}
}

// TestProcess_SendSignal_SIGTERM 验证 SIGTERM 信号发送
func TestProcess_SendSignal_SIGTERM(t *testing.T) {
	proc, err := StartProcess("test-sleep", []string{"sleep", "60"}, nil, "", nil)
	if err != nil {
		t.Fatalf("StartProcess failed: %v", err)
	}

	// 发送 SIGTERM
	if err := proc.SendSignal(syscall.SIGTERM); err != nil {
		t.Fatalf("SendSignal(SIGTERM) failed: %v", err)
	}

	exitCode, signaled, sig := proc.Wait()
	if !signaled {
		t.Errorf("signaled = false, want true")
	}
	if sig != syscall.SIGTERM {
		t.Errorf("sig = %d, want SIGTERM (%d)", sig, syscall.SIGTERM)
	}
	_ = exitCode
}

// TestProcess_DoneChannel 验证 Done channel 在 Wait 返回后关闭
func TestProcess_DoneChannel(t *testing.T) {
	proc, err := StartProcess("test-echo", []string{"echo", "done"}, nil, "", nil)
	if err != nil {
		t.Fatalf("StartProcess failed: %v", err)
	}

	// Wait 关闭 done channel
	proc.Wait()

	select {
	case <-proc.Done():
		// 预期：Done channel 已关闭
	default:
		t.Error("Done channel should be closed after Wait returns")
	}
}

// TestProcess_EmptyCommand 验证空命令返回错误
func TestProcess_EmptyCommand(t *testing.T) {
	_, err := StartProcess("test-empty", []string{}, nil, "", nil)
	if err == nil {
		t.Error("expected error for empty command, got nil")
	}
}

// TestProcess_InvalidCommand 验证不存在的命令返回错误
func TestProcess_InvalidCommand(t *testing.T) {
	_, err := StartProcess("test-invalid", []string{"nonexistent_command_xyz"}, nil, "", nil)
	if err == nil {
		t.Error("expected error for invalid command, got nil")
	}
}

// TestProcess_Setpgid 验证进程组独立于父进程
func TestProcess_Setpgid(t *testing.T) {
	proc, resultCh := startAndWaitHelper(t, "test-setpgid", []string{"sleep", "60"})

	// PGID 应该等于 PID，不等于父进程的 PGID
	parentPGID, _ := syscall.Getpgid(os.Getpid())
	if proc.PGID() == parentPGID {
		t.Errorf("child PGID %d equals parent PGID %d, should be independent", proc.PGID(), parentPGID)
	}
	if proc.PGID() != proc.PID() {
		t.Errorf("PGID %d != PID %d, should be equal with Setpgid=true", proc.PGID(), proc.PID())
	}

	// 清理
	proc.KillProcessGroup()
	<-resultCh
}

// TestProcess_StdoutPipe 验证 stdout pipe 可以读取输出
func TestProcess_StdoutPipe(t *testing.T) {
	proc, err := StartProcess("test-stdout-pipe", []string{"echo", "hello-stdout"}, nil, "", nil)
	if err != nil {
		t.Fatalf("StartProcess failed: %v", err)
	}

	outPipe := proc.StdoutPipe()
	if outPipe == nil {
		t.Fatal("StdoutPipe() returned nil")
	}

	// 读取 stdout
	data, err := io.ReadAll(outPipe)
	if err != nil {
		t.Fatalf("ReadAll stdout pipe: %v", err)
	}

	expected := "hello-stdout\n"
	if string(data) != expected {
		t.Errorf("stdout = %q, want %q", string(data), expected)
	}

	proc.Wait()
}

// TestProcess_StderrPipe 验证 stderr pipe 可以读取输出
func TestProcess_StderrPipe(t *testing.T) {
	// 使用 bash 将输出重定向到 stderr
	proc, err := StartProcess("test-stderr-pipe", []string{"bash", "-c", "echo hello-stderr >&2"}, nil, "", nil)
	if err != nil {
		t.Fatalf("StartProcess failed: %v", err)
	}

	errPipe := proc.StderrPipe()
	if errPipe == nil {
		t.Fatal("StderrPipe() returned nil")
	}

	// 读取 stderr
	data, err := io.ReadAll(errPipe)
	if err != nil {
		t.Fatalf("ReadAll stderr pipe: %v", err)
	}

	expected := "hello-stderr\n"
	if string(data) != expected {
		t.Errorf("stderr = %q, want %q", string(data), expected)
	}

	proc.Wait()
}

// --- ProcessManager 测试 ---

// TestProcessManager_RegisterUnregister 注册和注销进程
func TestProcessManager_RegisterUnregister(t *testing.T) {
	pm := NewProcessManager()

	proc, resultCh := startAndWaitHelper(t, "pm-test", []string{"sleep", "60"})
	defer func() {
		proc.KillProcessGroup()
		<-resultCh
	}()

	// 注册
	pm.Register("svc1", proc)

	// 获取
	got, ok := pm.Get("svc1")
	if !ok {
		t.Fatal("Get(svc1) returned not found")
	}
	if got != proc {
		t.Error("Get(svc1) returned wrong process")
	}

	// 注销
	pm.Unregister("svc1")

	// 再次获取应该失败
	_, ok = pm.Get("svc1")
	if ok {
		t.Error("Get(svc1) should return false after Unregister")
	}
}

// TestProcessManager_GetNotFound 获取不存在的服务
func TestProcessManager_GetNotFound(t *testing.T) {
	pm := NewProcessManager()

	_, ok := pm.Get("nonexistent")
	if ok {
		t.Error("Get(nonexistent) should return false")
	}
}

// TestProcessManager_List 列出所有已注册服务
func TestProcessManager_List(t *testing.T) {
	pm := NewProcessManager()

	proc1, resultCh1 := startAndWaitHelper(t, "pm-list1", []string{"sleep", "60"})
	defer func() {
		proc1.KillProcessGroup()
		<-resultCh1
	}()

	proc2, resultCh2 := startAndWaitHelper(t, "pm-list2", []string{"sleep", "60"})
	defer func() {
		proc2.KillProcessGroup()
		<-resultCh2
	}()

	pm.Register("svc1", proc1)
	pm.Register("svc2", proc2)

	list := pm.List()
	if len(list) != 2 {
		t.Fatalf("List() returned %d items, want 2", len(list))
	}

	found := map[string]bool{}
	for _, name := range list {
		found[name] = true
	}
	if !found["svc1"] || !found["svc2"] {
		t.Errorf("List() = %v, want [svc1, svc2]", list)
	}
}

// TestProcessManager_SendSignal 通过 ProcessManager 发送信号
func TestProcessManager_SendSignal(t *testing.T) {
	pm := NewProcessManager()

	proc, resultCh := startAndWaitHelper(t, "pm-signal", []string{"sleep", "60"})

	pm.Register("svc1", proc)

	// 通过 ProcessManager 发送 SIGTERM
	if err := pm.SendSignal("svc1", syscall.SIGTERM); err != nil {
		t.Fatalf("SendSignal failed: %v", err)
	}

	select {
	case r := <-resultCh:
		if !r.signaled {
			t.Errorf("signaled = false, want true")
		}
		if r.sig != syscall.SIGTERM {
			t.Errorf("sig = %d, want SIGTERM", r.sig)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Wait goroutine did not return within timeout")
	}
}

// TestProcessManager_SendSignal_NotFound 向不存在的服务发送信号
func TestProcessManager_SendSignal_NotFound(t *testing.T) {
	pm := NewProcessManager()

	err := pm.SendSignal("nonexistent", syscall.SIGTERM)
	if err == nil {
		t.Error("expected error for nonexistent service, got nil")
	}
}

// TestProcessManager_KillProcessGroup 通过 ProcessManager 强制杀死进程组
func TestProcessManager_KillProcessGroup(t *testing.T) {
	pm := NewProcessManager()

	proc, resultCh := startAndWaitHelper(t, "pm-kill", []string{"sleep", "60"})

	pm.Register("svc1", proc)

	// 通过 ProcessManager 强制杀死
	if err := pm.KillProcessGroup("svc1"); err != nil {
		t.Fatalf("KillProcessGroup failed: %v", err)
	}

	select {
	case r := <-resultCh:
		if !r.signaled {
			t.Errorf("signaled = false, want true")
		}
		if r.sig != syscall.SIGKILL {
			t.Errorf("sig = %d, want SIGKILL", r.sig)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Wait goroutine did not return within timeout")
	}
}

// TestProcessManager_KillProcessGroup_NotFound 向不存在的服务强杀
func TestProcessManager_KillProcessGroup_NotFound(t *testing.T) {
	pm := NewProcessManager()

	err := pm.KillProcessGroup("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent service, got nil")
	}
}

// TestProcess_WaitInGoroutine 验证 Wait 在 goroutine 中调用（僵尸进程回收场景）
func TestProcess_WaitInGoroutine(t *testing.T) {
	proc, resultCh := startAndWaitHelper(t, "test-goroutine-wait", []string{"sleep", "60"})

	// 发送 SIGTERM
	if err := proc.SendSignal(syscall.SIGTERM); err != nil {
		t.Fatalf("SendSignal failed: %v", err)
	}

	// 等待结果
	select {
	case r := <-resultCh:
		if !r.signaled {
			t.Errorf("signaled = false, want true")
		}
		if r.sig != syscall.SIGTERM {
			t.Errorf("sig = %d, want SIGTERM", r.sig)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Wait goroutine did not return within timeout")
	}

	// Done channel 也应该已关闭
	select {
	case <-proc.Done():
		// 预期
	default:
		t.Error("Done channel should be closed after Wait returns")
	}
}

type waitResult struct {
	exitCode int
	signaled bool
	sig      syscall.Signal
}
