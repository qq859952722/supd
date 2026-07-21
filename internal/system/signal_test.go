package system

import (
	"syscall"
	"testing"
	"time"
)

func TestNewSignalHandler(t *testing.T) {
	h := NewSignalHandler()
	if h == nil {
		t.Fatal("NewSignalHandler() returned nil")
	}
}

func TestSignalHandler_StartStop(t *testing.T) {
	h := NewSignalHandler()
	h.Start()

	// 发送 SIGHUP 触发热重载
	err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP)
	if err != nil {
		t.Fatalf("failed to send SIGHUP: %v", err)
	}

	select {
	case sig := <-h.WaitReload():
		if sig != syscall.SIGHUP {
			t.Errorf("expected SIGHUP, got %v", sig)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for SIGHUP")
	}

	h.Stop()
}

func TestSignalHandler_ShutdownSignal(t *testing.T) {
	h := NewSignalHandler()
	h.Start()

	// 发送 SIGTERM 触发优雅退出
	err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
	if err != nil {
		t.Fatalf("failed to send SIGTERM: %v", err)
	}

	select {
	case sig := <-h.WaitShutdown():
		if sig != syscall.SIGTERM {
			t.Errorf("expected SIGTERM, got %v", sig)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for SIGTERM")
	}

	h.Stop()
}

func TestSignalHandler_MultipleReload(t *testing.T) {
	h := NewSignalHandler()
	h.Start()
	defer h.Stop()

	// 发送多次 SIGHUP
	for i := 0; i < 3; i++ {
		err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP)
		if err != nil {
			t.Fatalf("failed to send SIGHUP: %v", err)
		}
	}

	// 由于 channel buffer=1，可能只收到部分信号
	// 至少应该能收到一个
	select {
	case <-h.WaitReload():
		// 成功收到至少一个 SIGHUP
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for SIGHUP")
	}
}

func TestSignalHandler_StopIdempotent(t *testing.T) {
	h := NewSignalHandler()
	h.Start()

	// 多次 Stop 不应 panic
	h.Stop()
	h.Stop()
}

func TestSignalHandler_ChannelsNonBlocking(t *testing.T) {
	h := NewSignalHandler()

	// 未 Start 时，channel 不应收到信号
	select {
	case <-h.WaitShutdown():
		t.Fatal("unexpected signal on shutdown channel before Start()")
	case <-h.WaitReload():
		t.Fatal("unexpected signal on reload channel before Start()")
	case <-time.After(100 * time.Millisecond):
		// 预期：没有信号
	}
}

func TestSignalHandler_SIGINT(t *testing.T) {
	h := NewSignalHandler()
	h.Start()

	err := syscall.Kill(syscall.Getpid(), syscall.SIGINT)
	if err != nil {
		t.Fatalf("failed to send SIGINT: %v", err)
	}

	select {
	case sig := <-h.WaitShutdown():
		if sig != syscall.SIGINT {
			t.Errorf("expected SIGINT, got %v", sig)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for SIGINT")
	}

	h.Stop()
}

func TestSignalHandler_ReloadAndShutdownSeparate(t *testing.T) {
	h := NewSignalHandler()
	h.Start()
	defer h.Stop()

	// 发送 SIGHUP
	err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP)
	if err != nil {
		t.Fatalf("failed to send SIGHUP: %v", err)
	}

	select {
	case <-h.WaitReload():
		// SIGHUP 只出现在 reload channel
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for SIGHUP on reload channel")
	}

	// 确保 SIGHUP 不会出现在 shutdown channel
	select {
	case sig := <-h.WaitShutdown():
		t.Errorf("SIGHUP should not appear on shutdown channel, got %v", sig)
	case <-time.After(100 * time.Millisecond):
		// 预期：shutdown channel 无信号
	}
}

// TestZombieReaper_StartStop 验证 ZombieReaper 启动和停止
func TestZombieReaper_StartStop(t *testing.T) {
	r := NewZombieReaper()
	if r == nil {
		t.Fatal("NewZombieReaper() returned nil")
	}
	r.Start()

	// Stop 应该幂等且不阻塞
	r.Stop()
	r.Stop() // 重复 Stop 不应 panic
}

// TestZombieReaper_ReapsChildProcess 验证 ZombieReaper 能回收子进程
func TestZombieReaper_ReapsChildProcess(t *testing.T) {
	r := NewZombieReaper()
	r.Start()
	defer r.Stop()

	// 创建一个立即退出的子进程
	pid, err := syscall.ForkExec("/bin/true", []string{"/bin/true"}, nil)
	if err != nil {
		t.Skipf("ForkExec failed (may not have permission): %v", err)
	}

	// 等待子进程退出（变僵尸）
	time.Sleep(100 * time.Millisecond)

	// 触发 SIGCHLD 让 ZombieReaper 回收
	// 即使不显式触发，10s ticker 也会兜底，但测试中用 SIGCHLD 更快
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGCHLD); err != nil {
		t.Logf("send SIGCHLD failed: %v", err)
	}

	// 等待回收（最多 2 秒）
	deadline := time.After(2 * time.Second)
	for {
		// kill(pid, 0) 返回 ESRCH 表示进程不存在（已被回收）
		if err := syscall.Kill(pid, 0); err != nil {
			break // 进程已被回收
		}
		select {
		case <-deadline:
			t.Fatalf("zombie process %d not reaped within 2s", pid)
		case <-time.After(50 * time.Millisecond):
		}
	}
}
