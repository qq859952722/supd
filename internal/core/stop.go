package core

import (
	"context"
	"fmt"
	"log/slog"
	"syscall"
	"time"
)

// StopConfig 停止配置
// REQ-F-007: stop.grace_seconds 默认10秒, stop.timeout_seconds 默认60秒
type StopConfig struct {
	GraceSeconds   int
	TimeoutSeconds int
}

// DefaultStopConfig 返回默认停止配置
// REQ-F-007: grace_seconds=10, timeout_seconds=60
func DefaultStopConfig() StopConfig {
	return StopConfig{
		GraceSeconds:   10,
		TimeoutSeconds: 60,
	}
}

// StopResult 停止结果
type StopResult struct {
	ExitCode int
	Signaled bool
	Sig      syscall.Signal
}

// StopService 执行7步停止流程
// REQ-F-007: 停止流程
// Step 1: 状态变更为stopping（由调用方在调用前完成）
// Step 2: 触发pre_stop扩展（通过 runPreStop 回调执行，由调用方注入）
// Step 3: 向进程组发送SIGTERM
// Step 4: 等待grace_seconds
// Step 5: 进程未退出→SIGKILL
// Step 6: 进程退出后状态变更为down（由调用方在调用后完成）
// Step 7: 手动停止不算failure，不触发on_failure扩展（由调用方判定）
//
// 停止流程总时限: timeout_seconds覆盖整个流程（含pre_stop+SIGTERM等待）
// 超时后直接SIGKILL进程组，不再等待
func StopService(ctx context.Context, process *Process, config StopConfig, runPreStop func() error) (StopResult, error) {
	// 创建总超时context
	totalCtx, cancel := context.WithTimeout(ctx, time.Duration(config.TimeoutSeconds)*time.Second)
	defer cancel()

	// 必须在单独goroutine中调用Wait（回收僵尸进程）
	waitCh := make(chan StopResult, 1)
	go func() {
		exitCode, signaled, sig := process.Wait()
		waitCh <- StopResult{ExitCode: exitCode, Signaled: signaled, Sig: sig}
	}()

	// Step 2: pre_stop扩展（通过 runPreStop 回调执行，由调用方注入）
	if runPreStop != nil {
		preStopDone := make(chan error, 1)
		go func() {
			preStopDone <- runPreStop()
		}()
		select {
		case err := <-preStopDone:
			// pre_stop失败不阻塞停止流程，仅记录日志继续
			if err != nil {
				// C-01-002 修复：记录日志而非静默丢弃
				slog.Warn("pre_stop extension failed", "error", err)
			}
		case <-totalCtx.Done():
			// 总超时，pre_stop未完成，直接进入SIGKILL
			// 等待进程退出
			// C-04-002: 记录 KillProcessGroup 错误，与 L105/L117 保持一致的错误处理
			if err := process.KillProcessGroup(); err != nil {
				slog.Warn("KillProcessGroup failed on pre_stop total timeout", "error", err)
			}
			select {
			case r := <-waitCh:
				return r, nil
			case <-ctx.Done():
				return StopResult{}, ctx.Err()
			}
		}
	}

	// Step 3: 向进程组发送SIGTERM
	if err := process.SendSignal(syscall.SIGTERM); err != nil {
		// SIGTERM发送失败（进程可能已退出），尝试获取Wait结果
		select {
		case r := <-waitCh:
			return r, nil
		case <-totalCtx.Done():
			return StopResult{}, fmt.Errorf("stop service: SIGTERM send failed: %w, and total timeout exceeded", err)
		}
	}

	// Step 4+5: 等待进程退出，带grace_seconds和总超时
	graceTimer := time.NewTimer(time.Duration(config.GraceSeconds) * time.Second)
	defer graceTimer.Stop()

	select {
	case r := <-waitCh:
		// 进程在grace期内退出
		return r, nil
	case <-graceTimer.C:
		// Step 5: grace_seconds到期，进程未退出→SIGKILL
		if err := process.KillProcessGroup(); err != nil {
			// C-01-002 修复：SIGKILL也失败，仍然尝试获取Wait结果
			slog.Debug("KillProcessGroup failed after grace timeout", "error", err)
		}
		select {
		case r := <-waitCh:
			return r, nil
		case <-totalCtx.Done():
			return StopResult{}, fmt.Errorf("stop service: total timeout exceeded after SIGKILL")
		}
	case <-totalCtx.Done():
		// 总超时，直接SIGKILL进程组，不再等待
		if err := process.KillProcessGroup(); err != nil {
			slog.Debug("KillProcessGroup failed on total timeout", "error", err)
		}
		select {
		case r := <-waitCh:
			return r, nil
		case <-ctx.Done():
			return StopResult{}, ctx.Err()
		}
	}
}
