package core

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestRestartMaxRetries_ReachesFailedState 验证 max_retries 达到后状态机进入 failed。
// A-02-002: 端到端测试 — 构造 always-fail 服务（`false` 命令，退出码 1），
// 配置 restart.policy=always + max_retries=3 + 极短退避（10ms），
// 验证状态机在 3 次重试后到达 StateFailed。
//
// 规格 §2.1.5 规则6/8：进程死亡且达到 max_retries → failed
// 规格 §2.1.1 状态机：up + max_retries → failed（规则6），starting + max_retries → failed（规则8）
func TestRestartMaxRetries_ReachesFailedState(t *testing.T) {
	// 确保 `false` 命令可用（Linux coreutils 标准命令，退出码 1）
	if _, err := exec.LookPath("false"); err != nil {
		t.Skipf("false command not available: %v", err)
	}

	baseDir, logDir := setupTestDir(t)

	// always-fail 服务：`false` 命令立即退出码 1
	// backoff_ms=10 使退避极短，避免测试长时间等待
	// max_retries=3 限制重试次数
	writeService(t, baseDir, "always-fail", `name: always-fail
version: "1.0"
command:
  - "false"
restart:
  policy: always
  backoff_ms: 10
  max_backoff_ms: 100
  multiplier: 2
  max_retries: 3
  reset_after_seconds: 300
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

	sm, ok := result.StateMachines["always-fail"]
	if !ok {
		t.Fatal("always-fail state machine not found")
	}

	// 轮询等待状态到达 failed（3 次重试 + 退避，总计应 < 5s）
	deadline := time.Now().Add(10 * time.Second)
	var finalState ServiceState
	for time.Now().Before(deadline) {
		finalState = sm.Current()
		if finalState == StateFailed {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if finalState != StateFailed {
		t.Errorf("always-fail should reach failed after 3 retries, got state = %s", finalState)
	}

	// 验证重试次数：engine.Retries() 应该 == 3
	engine, ok := result.RestartEngines["always-fail"]
	if !ok {
		t.Fatal("always-fail restart engine not found")
	}
	if retries := engine.Retries(); retries != 3 {
		t.Errorf("retries count = %d, want 3 (max_retries)", retries)
	}
}

// TestRestartOnFailure_ExitCodeZero_NoRestart 验证 on-failure 策略在退出码 0 时不重启。
// A-02-002: 端到端测试 — 构造 `true` 命令服务（退出码 0），配置 on-failure 策略，
// 验证进程退出后不触发重启，状态进入 down（规格 DEV-010：on-failure+exit 0 → down）。
//
// 规格 §2.1.5：on-failure 触发条件：退出码非 0 或被信号杀死（非 SIGTERM/SIGINT）
// 规格 §2.1.1 状态机：up + 正常退出（不应重启）→ ResetTo(StateDown)（DEV-010 变通方案）
func TestRestartOnFailure_ExitCodeZero_NoRestart(t *testing.T) {
	// 确保 `true` 命令可用（Linux coreutils 标准命令，退出码 0）
	if _, err := exec.LookPath("true"); err != nil {
		t.Skipf("true command not available: %v", err)
	}

	baseDir, logDir := setupTestDir(t)

	// 正常退出服务：`true` 命令立即退出码 0
	writeService(t, baseDir, "exit-zero", `name: exit-zero
version: "1.0"
command:
  - "true"
restart:
  policy: on-failure
  backoff_ms: 10
  max_backoff_ms: 100
  multiplier: 2
  max_retries: 3
  reset_after_seconds: 300
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

	sm, ok := result.StateMachines["exit-zero"]
	if !ok {
		t.Fatal("exit-zero state machine not found")
	}

	// 轮询等待状态到达 down（进程退出后不重启，直接 down）
	deadline := time.Now().Add(5 * time.Second)
	var finalState ServiceState
	for time.Now().Before(deadline) {
		finalState = sm.Current()
		if finalState == StateDown {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if finalState != StateDown {
		t.Errorf("exit-zero with on-failure should reach down (not failed, not restarted), got state = %s", finalState)
	}

	// 验证未触发重启：retries 应为 0
	engine, ok := result.RestartEngines["exit-zero"]
	if !ok {
		t.Fatal("exit-zero restart engine not found")
	}
	if retries := engine.Retries(); retries != 0 {
		t.Errorf("retries count = %d, want 0 (on-failure + exit 0 should not restart)", retries)
	}
}

// TestRestartOnFailure_ExitCodeNonZero_RestartsAndFails 验证 on-failure 策略在退出码非 0 时重启并最终 failed。
// A-02-002: 端到端测试 — 构造 `false` 命令服务（退出码 1），配置 on-failure 策略 + max_retries=3，
// 验证状态机在 3 次重试后到达 StateFailed（与 always 策略行为一致，因为退出码非 0）。
func TestRestartOnFailure_ExitCodeNonZero_RestartsAndFails(t *testing.T) {
	if _, err := exec.LookPath("false"); err != nil {
		t.Skipf("false command not available: %v", err)
	}

	baseDir, logDir := setupTestDir(t)

	writeService(t, baseDir, "on-fail-svc", `name: on-fail-svc
version: "1.0"
command:
  - "false"
restart:
  policy: on-failure
  backoff_ms: 10
  max_backoff_ms: 100
  multiplier: 2
  max_retries: 3
  reset_after_seconds: 300
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

	sm, ok := result.StateMachines["on-fail-svc"]
	if !ok {
		t.Fatal("on-fail-svc state machine not found")
	}

	// 轮询等待状态到达 failed
	deadline := time.Now().Add(10 * time.Second)
	var finalState ServiceState
	for time.Now().Before(deadline) {
		finalState = sm.Current()
		if finalState == StateFailed {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if finalState != StateFailed {
		t.Errorf("on-fail-svc should reach failed after 3 retries (exit code 1), got state = %s", finalState)
	}

	engine, ok := result.RestartEngines["on-fail-svc"]
	if !ok {
		t.Fatal("on-fail-svc restart engine not found")
	}
	if retries := engine.Retries(); retries != 3 {
		t.Errorf("retries count = %d, want 3 (max_retries)", retries)
	}
}
