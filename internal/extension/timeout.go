package extension

import (
	"context"
	"log/slog"
	"sync"
	"syscall"
	"time"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/core"
)

// TimeoutConfig 超时配置
// REQ-F-019, 2.2.8: 三层防线 — 扩展timeout → SIGTERM(5s) → SIGKILL；硬上限直接SIGKILL
type TimeoutConfig struct {
	// ExtensionTimeout 扩展 meta.yaml 的 timeout_seconds（默认600秒）
	ExtensionTimeout int
	// HardLimitSeconds config.yaml 的 extension_hard_limit_seconds（默认1800秒）
	HardLimitSeconds int
}

// DefaultTimeoutConfig 返回默认超时配置
// REQ-F-019: timeout 默认 600 秒，hard limit 默认 1800 秒
// O-05-001/O-05-002 修复：使用 config 常量替代字面量 1800/600
func DefaultTimeoutConfig() TimeoutConfig {
	return TimeoutConfig{
		ExtensionTimeout: config.DefaultExtensionTimeoutSeconds,
		HardLimitSeconds: config.ExtensionHardLimitSeconds,
	}
}

// TimeoutGuard 超时守护
// REQ-F-019, 2.2.8: 超时三层防线实现
type TimeoutGuard struct {
	config     TimeoutConfig
	startTime  time.Time
	process    *core.Process
	timedOut   bool
	hardKilled bool
	done       chan struct{}
	stopCh     chan struct{}
	mu         sync.Mutex
}

// NewTimeoutGuard 创建超时守护
// REQ-F-019: 接收超时配置和需要守护的进程
func NewTimeoutGuard(config TimeoutConfig, process *core.Process) *TimeoutGuard {
	return &TimeoutGuard{
		config:   config,
		process:  process,
		done:     make(chan struct{}),
		stopCh:   make(chan struct{}, 1),
		startTime: time.Now(),
	}
}

// Start 启动超时监控
// REQ-F-019, 2.2.8: 三层防线
//  1. extension timeout 到期 → SIGTERM → 等5秒 → SIGKILL
//  2. hard limit 到期 → 直接 SIGKILL
//  3. 两者取先到者
func (g *TimeoutGuard) Start(ctx context.Context) {
	// REQ-F-019: timeout_seconds 默认 600，若为 0 则使用默认值
	// O-05-002 修复：使用 config.DefaultExtensionTimeoutSeconds 常量替代字面量 600
	extTimeout := g.config.ExtensionTimeout
	if extTimeout <= 0 {
		extTimeout = config.DefaultExtensionTimeoutSeconds
	}
	hardLimit := g.config.HardLimitSeconds
	if hardLimit <= 0 {
		// O-05-001 修复：使用 config.ExtensionHardLimitSeconds 常量替代字面量 1800
		hardLimit = config.ExtensionHardLimitSeconds
	}

	extDur := time.Duration(extTimeout) * time.Second
	hardDur := time.Duration(hardLimit) * time.Second

	go func() {
		defer close(g.done)

		extTimer := time.NewTimer(extDur)
		defer extTimer.Stop()

		hardTimer := time.NewTimer(hardDur)
		defer hardTimer.Stop()

		// 5秒等待定时器（SIGTERM后等待进程退出）
		var graceTimer *time.Timer
		defer func() {
			if graceTimer != nil {
				graceTimer.Stop()
			}
		}()

		sigtermSent := false

		for {
			select {
			case <-ctx.Done():
				// 外部取消（如用户取消），停止监控
				return

			case <-g.stopCh:
				// 正常完成，停止监控
				return

			case <-extTimer.C:
				// 第一层防线：extension timeout 到期
				g.mu.Lock()
				g.timedOut = true
				g.mu.Unlock()

				slog.Warn("extension timeout reached, sending SIGTERM",
					"timeout_seconds", g.config.ExtensionTimeout,
				)

				// REQ-F-019: 发送 SIGTERM 给整个进程组
				if err := g.process.SendSignal(syscall.SIGTERM); err != nil {
					slog.Warn("failed to send SIGTERM", "error", err)
				}
				sigtermSent = true

				// REQ-F-019: 等待5秒
				graceTimer = time.NewTimer(5 * time.Second)

			case <-hardTimer.C:
				// 第二层防线：hard limit 到期 → 直接 SIGKILL
				slog.Warn("extension hard limit reached, sending SIGKILL",
					"hard_limit_seconds", g.config.HardLimitSeconds,
				)

				g.mu.Lock()
				g.timedOut = true
				g.hardKilled = true
				g.mu.Unlock()

				if err := g.process.KillProcessGroup(); err != nil {
					slog.Warn("failed to send SIGKILL for hard limit", "error", err)
				}
				return

			case <-func() <-chan time.Time {
				if graceTimer != nil {
					return graceTimer.C
				}
				// 返回一个永不触发的 channel
				return make(chan time.Time)
			}():
				// SIGTERM 后5秒等待到期，进程仍未退出 → SIGKILL
				if sigtermSent {
					slog.Warn("process still alive 5s after SIGTERM, sending SIGKILL")

					g.mu.Lock()
					g.hardKilled = true
					g.mu.Unlock()

					if err := g.process.KillProcessGroup(); err != nil {
						slog.Warn("failed to send SIGKILL", "error", err)
					}
					return
				}
			}
		}
	}()
}

// Check 检查超时状态
// REQ-F-019: 返回是否超时、是否被 SIGKILL
func (g *TimeoutGuard) Check() (timedOut bool, hardKilled bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.timedOut, g.hardKilled
}

// Stop 停止超时监控（进程正常退出时调用）
func (g *TimeoutGuard) Stop() {
	select {
	case g.stopCh <- struct{}{}:
	default:
	}
}

// Elapsed 返回已运行时长
func (g *TimeoutGuard) Elapsed() time.Duration {
	return time.Since(g.startTime)
}

// Wait 等待监控 goroutine 退出
func (g *TimeoutGuard) Wait() {
	<-g.done
}
