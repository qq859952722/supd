package core

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/watch"
)

// SupervisorCallbacks 封装 supervisor 差异依赖，由调用方（bootstrap/api）注入。
// 所有字段均为可选（nil 安全），未注入时静默跳过。
type SupervisorCallbacks struct {
	// WriteServiceLog 写服务日志（进程退出原因、重启失败原因）
	WriteServiceLog func(name, level, message string)

	// PublishEvent 发布 service_died / service_exited 事件
	PublishEvent EventPublisher

	// OnFailure 服务意外失败回调（触发 on_failure 扩展）
	// nil 安全；手动停止时不调用（由 sm.Current() 判断）
	// ctx 由 RunSupervisor 传入（handleProcessExit 传 supervisor 的 ctx）
	// bootstrap 闭包内可用 context.Background()（原行为），service_operator 用传入 ctx（原行为）
	OnFailure func(ctx context.Context, name string, exitCode, signal, restartCount, servicePID int)

	// RuntimeRegistry 运行时注册表（重启时解析 runtime 别名）
	// bootstrap: 预构建（启动时复用）；service_operator: 薄包装顶部一次性构建（有意策略变更）
	RuntimeRegistry *config.RuntimeRegistry

	// BuildEnv 构建服务进程环境变量列表（闭包捕获 baseDir + envFiles）
	BuildEnv func(name string) []string

	// RegisterProcess 重启后向进程管理器注册新进程
	RegisterProcess func(name string, proc *Process)

	// RebuildLogger 关闭旧日志器，创建并启动新日志器
	RebuildLogger func(name string, newProc *Process, maxSizeMB, maxFiles int)

	// RecordRestartHistory 记录重启历史（bootstrap 不需要，置 nil）
	RecordRestartHistory func(name string, pid int)

	// SpawnNextSupervisor 为新进程启动 supervisor goroutine
	// 由调用方提供，负责创建 newCtx、写 cancelFuncs、launch goroutine
	// 返回新进程的 context.Context（用于 readiness 检查）
	// bootstrap 返回旧 ctx（readiness 用 supervisor 自己的 ctx，与原行为一致）
	// service_operator 返回 newCtx（readiness 用新进程的 ctx，与原行为一致：StopService cancelFuncs 同步）
	SpawnNextSupervisor func(name string, svcEntry *watch.ServiceEntry,
		sm *StateMachine, proc *Process, engine *RestartEngine) context.Context

	// RunReadiness 重启后执行 readiness 检查
	// bootstrap: 同步（阻塞），返回 error（checkReadiness 的错误）
	// service_operator: 异步（goroutine），返回 nil（异步模式无同步 error）
	// RunSupervisor 在同步模式下处理 error → newProc.Done() 区分进程退出 vs 超时
	RunReadiness func(ctx context.Context, name string, svcEntry *watch.ServiceEntry,
		sm *StateMachine, proc *Process, preChecker ReadinessChecker) error

	// Source 标识服务来源（"cli"/"api"），用于 slog 日志区分
	Source string
}

// RestartAction 重启决策结果（export 以便测试）
type RestartAction int

const (
	RestartActionWait   RestartAction = iota // 允许重启，进入退避
	RestartActionFailed                       // → failed
	RestartActionDown                         // → down
	RestartActionAbort                        // 状态已被外部接管，静默退出
)

// RunSupervisor 监督单个服务进程：等待退出→决策→重启。
// 在独立 goroutine 中运行（go RunSupervisor(...)）。
// ctx 取消时中断退避等待，转 stopping→down 路径。
func RunSupervisor(
	ctx context.Context,
	name string,
	svcEntry *watch.ServiceEntry,
	sm *StateMachine,
	proc *Process,
	engine *RestartEngine,
	callbacks SupervisorCallbacks,
) {
	// 1. 等待进程退出
	exitCode, signaled, sig := proc.Wait()

	// 1.1 slog.Debug 输出进程退出级别的日志（补全 P2 发现）
	sourceTag := callbacks.Source
	if sourceTag == "api" {
		sourceTag = "api-started"
	}
	slog.Debug("process exited ("+sourceTag+")",
		"service", name,
		"exitCode", exitCode,
		"signaled", signaled,
		"signal", sig)

	// 2. 退出处理（日志+事件+on_failure）
	// 传入 ctx：bootstrap 闭包用 Background()，service_operator 闭包用 ctx（与原行为一致）
	handleProcessExit(ctx, name, exitCode, signaled, sig, sm, engine, proc, callbacks)

	// 3. 检查是否需要重启
	currentState := sm.Current()
	if currentState != StateUp && currentState != StateReady && currentState != StateStarting {
		return
	}

	// 4. 重启策略决策（含 engine/sm 状态副作用，可验证）
	action := DecideRestart(engine, sm, exitCode, signaled, sig)
	switch action {
	case RestartActionFailed:
		sm.Transition(EventMaxRetries)
		return
	case RestartActionDown:
		sm.Transition(EventNormalExit)
		return
	case RestartActionAbort:
		// P1 修复：EventRestartAllowed 状态转移失败（外部已改变状态），静默退出，不再二次触发 EventMaxRetries
		return
	}

	// 5. 退避等待（可被 ctx 中断）
	delay := engine.BackoffDuration()
	slog.Info("restarting service after unexpected exit ("+sourceTag+")",
		"service", name, "attempt", engine.Retries(), "delay", delay)
	if !doBackoff(ctx, sm, name, delay) {
		return // ctx 取消，stopping→down 已处理
	}

	// 6. 退避后再次检查（等待期间可能被手动停止）
	if sm.Current() != StateStarting {
		slog.Info("service state changed during backoff, skip restart ("+sourceTag+")",
			"service", name, "state", sm.Current())
		return
	}

	// 7. 启动新进程+重建日志器
	newProc, preChecker, ok := doRestartProcess(name, svcEntry, sm, engine, callbacks)
	if !ok {
		return // 启动失败，已转 failed
	}

	// 8. 启动新 supervisor → 返回新进程的 context（用于 readiness 检查）
	// bootstrap 返回 ctx（readiness 用旧 supervisor ctx，与原行为一致）
	// service_operator 返回 newCtx（readiness 受 StopService cancelFuncs 控制，与原行为一致）
	readinessCtx := ctx
	if callbacks.SpawnNextSupervisor != nil {
		readinessCtx = callbacks.SpawnNextSupervisor(name, svcEntry, sm, newProc, engine)
	}

	// 9. readiness 检查
	// bootstrap: 同步+error 处理（区分进程退出 vs 超时）
	// service_operator: 异步，返回 nil（无同步 error）
	if svcEntry.Config.Readiness != nil && callbacks.RunReadiness != nil {
		if err := callbacks.RunReadiness(readinessCtx, name, svcEntry, sm, newProc, preChecker); err != nil {
			// 同步模式下 checkReadiness 返回 error
			// 区分"进程在 readiness 期间退出"和"readiness 超时"
			select {
			case <-newProc.Done():
				// 进程在 readiness 检查期间退出 → 新 supervisor 会处理重启，无需额外状态转移
			default:
				// readiness 超时/失败 → RunReadiness 内部（checkReadiness）已处理状态转移至 failed
			}
		}
	}
}

// DecideRestart 基于重启引擎与状态机决定下一步行为
// 含 engine 状态 mutation（ResetIfNeeded + IncrementRetries）和 sm 状态转移（Transition）
// Export 以便 supervisor_test.go 直接测试
func DecideRestart(engine *RestartEngine, sm *StateMachine,
	exitCode int, signaled bool, sig syscall.Signal) RestartAction {
	engine.ResetIfNeeded()
	if !engine.ShouldRestart(exitCode, signaled, sig) {
		// A-02-001 修复：区分 failed vs down
		// never 策略：进程退出 → failed（规格明确定义）
		// on-failure + 正常退出：不算失败 → down
		if engine.Policy() == RestartNever {
			return RestartActionFailed
		}
		return RestartActionDown
	}
	if engine.MaxRetriesReached() {
		return RestartActionFailed
	}
	engine.IncrementRetries()
	if _, ok := sm.Transition(EventRestartAllowed); !ok {
		// P1 修复：状态转移失败（外部已变更状态），返回 Abort 静默终止
		return RestartActionAbort
	}
	return RestartActionWait
}

// handleProcessExit 处理进程退出后的日志、事件、on_failure 回调
func handleProcessExit(ctx context.Context, name string, exitCode int, signaled bool, sig syscall.Signal,
	sm *StateMachine, engine *RestartEngine, proc *Process, cb SupervisorCallbacks) {
	// 写退出日志
	if cb.WriteServiceLog != nil {
		switch {
		case signaled:
			cb.WriteServiceLog(name, "warn",
				fmt.Sprintf("process killed by signal: %s (exit code: %d)", sig, exitCode))
		case exitCode != 0:
			cb.WriteServiceLog(name, "warn",
				fmt.Sprintf("process exited with code: %d", exitCode))
		default:
			cb.WriteServiceLog(name, "warn", "process exited unexpectedly (exit code: 0)")
		}
	}

	// 发布事件
	if cb.PublishEvent != nil {
		eventType := "service_exited"
		if signaled {
			eventType = "service_died"
		}
		cb.PublishEvent.Publish(eventType, map[string]any{
			"service":   name,
			"exit_code": exitCode,
			"signaled":  signaled,
			"signal":    int(sig),
		})
	}

	// A-02-001 修复：区分手动停止与意外崩溃，手动停止不算 failure（规格 §2.1.4）
	// ctx 由 RunSupervisor 传入，闭包内由调用方决定语义：
	//   bootstrap 闭包内部可用 context.Background()（与原行为一致）
	//   service_operator 闭包内部用传入 ctx（与原行为一致：OnFailure 受 supervisor cancel context 控制）
	currentState := sm.Current()
	if cb.OnFailure != nil && currentState != StateStopping && currentState != StateDown {
		if exitCode != 0 || signaled {
			sigInt := 0
			if signaled {
				sigInt = int(sig)
			}
			// A-05-001 修复：传递 engine.Retries()，使 SUPD_SERVICE_RESTART_COUNT 反映实际重启次数
			// 规格 §2.2.5: 传递 proc.PID()（进程退出前的 PID），使 SUPD_SERVICE_PID 可用
			cb.OnFailure(ctx, name, exitCode, sigInt, engine.Retries(), proc.PID())
		}
	}
}

// doBackoff 退避等待（可被 ctx 中断），返回 true 表示等待结束可继续重启
func doBackoff(ctx context.Context, sm *StateMachine, name string, delay time.Duration) bool {
	select {
	case <-time.After(delay):
		return true
	case <-ctx.Done():
		slog.Info("restart backoff aborted by stop request", "service", name)
		// 状态转换可能已由 StopService 执行，此处幂等处理
		if sm.Current() == StateStarting {
			sm.Transition(EventStopRequested) // starting → stopping
			sm.Transition(EventBackoffAbort)  // stopping → down
		}
		return false
	}
}

// doRestartProcess 启动新进程、重建日志器、记录历史
func doRestartProcess(name string, svcEntry *watch.ServiceEntry, sm *StateMachine,
	engine *RestartEngine, cb SupervisorCallbacks) (*Process, ReadinessChecker, bool) {
	svcConfig := svcEntry.Config

	// 构建命令（runtime 解析）
	command := svcConfig.Command
	if svcConfig.Runtime != "" && cb.RuntimeRegistry != nil {
		if rt, err := config.Resolve(cb.RuntimeRegistry, svcConfig.Runtime); err == nil && rt.Available {
			command = append([]string{rt.AbsPath}, command...)
		}
	}

	// 构建 env 和 workdir
	var env []string
	if cb.BuildEnv != nil {
		env = cb.BuildEnv(name)
	}
	workdir := svcConfig.Workdir
	if workdir == "" {
		workdir = filepath.Dir(svcEntry.ConfigPath)
	}

	// A-03-002 修复：fd_notify readiness 需在 StartProcess 前创建 checker
	var preChecker ReadinessChecker
	var extraFiles []*os.File
	if svcConfig.Readiness != nil && svcConfig.Readiness.Type == "fd_notify" {
		nc, cerr := NewNotifyChecker(svcConfig.Readiness)
		if cerr != nil {
			slog.Error("readiness fd_notify for restart", "service", name, "error", cerr)
			sm.Transition(EventMaxRetries)
			return nil, nil, false
		}
		preChecker = nc
		extraFiles = []*os.File{nc.WriterFd()}
	}

	// 启动新进程
	// REQ-F-023, §2.2.13: 通过 StartServiceProcess 解析身份配置（user 或 uid 模式）
	newProc, err := StartServiceProcess(name, command, env, workdir, svcConfig.ToCredentialSpec(), svcEntry.ConfigPath, extraFiles...)
	if err != nil {
		if preChecker != nil {
			preChecker.Close()
		}
		if cb.WriteServiceLog != nil {
			cb.WriteServiceLog(name, "error", fmt.Sprintf("restart failed: %s", err))
		}
		slog.Error("failed to restart service",
			"service", name,
			"user", svcConfig.User,
			"config_path", svcEntry.ConfigPath,
			"error", err)
		sm.Transition(EventMaxRetries)
		return nil, nil, false
	}

	// A-03-002 修复：StartProcess 成功后关闭 supd 侧的管道写端
	// C-01-001 修复：CloseWriter 错误记录日志，便于诊断管道异常
	if nc, ok := preChecker.(*NotifyChecker); ok {
		if err := nc.CloseWriter(); err != nil {
			slog.Warn("close notify pipe writer failed", "service", name, "error", err)
		}
	}

	// 状态转移: starting → up
	sm.Transition(EventProcessStarted)
	engine.RecordStart()

	// 注册新进程
	if cb.RegisterProcess != nil {
		cb.RegisterProcess(name, newProc)
	}

	// 重建日志器
	maxSizeMB, maxFiles := 0, 0
	if cfg := svcConfig.Logging; cfg != nil {
		maxSizeMB, maxFiles = cfg.MaxSizeMB, cfg.MaxFiles
	}
	if cb.RebuildLogger != nil {
		cb.RebuildLogger(name, newProc, maxSizeMB, maxFiles)
	}

	// 历史记录
	if cb.RecordRestartHistory != nil {
		cb.RecordRestartHistory(name, newProc.PID())
	}

	return newProc, preChecker, true
}
