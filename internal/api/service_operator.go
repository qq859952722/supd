package api

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/core"
	"github.com/supdorg/supd/internal/extension"
	"github.com/supdorg/supd/internal/logging"
	"github.com/supdorg/supd/internal/watch"
)

// --- ServiceOperator 适配器 ---

type CoreServiceOperator struct {
	ProcessMgr    *core.ProcessManager
	StateMachines map[string]*core.StateMachine
	Discovery     *watch.DiscoveryResult
	Config        *config.Config
	BaseDir       string
	LogDir        string
	// REQ-D-004: service_lifecycle 触发器（由 run.go 注入）
	ServiceLifecycleTrigger *extension.ServiceLifecycleTrigger
	// 服务历史记录
	HistoryStore *ServiceHistoryStore
	// 服务日志器（API启动的服务进程日志捕获）
	loggers   map[string]*logging.ServiceLogger
	loggersMu sync.Mutex
	// stateMachinesMu 保护 StateMachines map 的并发访问
	// N-04-001 修复：热重载并发安全
	stateMachinesMu sync.RWMutex
	// 事件发布器（发布 service_died/service_exited 等事件）
	EventPublisher core.EventPublisher
	// 重启引擎 map（与 CoreStateProvider 共享同一实例）
	RestartEngines   map[string]*core.RestartEngine
	restartEnginesMu sync.Mutex
	// 服务 supervisor 的 cancel context（用于停止退避等待中的服务）
	cancelFuncs   map[string]context.CancelFunc
	cancelFuncsMu sync.Mutex
}

// SetCancelFuncs 设置从 Bootstrap 传递的 cancel context map
// 用于 bootstrap 启动的服务，在 API 层需要能停止退避等待中的服务
func (o *CoreServiceOperator) SetCancelFuncs(cancelFuncs map[string]context.CancelFunc) {
	o.cancelFuncsMu.Lock()
	defer o.cancelFuncsMu.Unlock()
	if o.cancelFuncs == nil {
		o.cancelFuncs = make(map[string]context.CancelFunc)
	}
	for k, v := range cancelFuncs {
		o.cancelFuncs[k] = v
	}
}

// SetDiscovery 热重载时更新 Discovery 引用
// N-04-001 修复：providers 持有 Discovery 指针值拷贝，reload 后需要显式更新
func (o *CoreServiceOperator) SetDiscovery(d *watch.DiscoveryResult) {
	if o == nil || d == nil {
		return
	}
	o.stateMachinesMu.Lock()
	defer o.stateMachinesMu.Unlock()
	o.Discovery = d
}

func (o *CoreServiceOperator) StartService(name string) error {
	o.stateMachinesMu.RLock()
	svcEntry, ok := o.Discovery.Services[name]
	if !ok {
		o.stateMachinesMu.RUnlock()
		return fmt.Errorf("service %s not found", name)
	}

	sm, ok := o.StateMachines[name]
	o.stateMachinesMu.RUnlock()
	if !ok {
		return fmt.Errorf("state machine for %s not found", name)
	}

	// 防止重复启动：服务已在运行（up/ready/starting）时直接返回错误
	// 避免重复 fork 进程导致端口冲突 + 旧进程孤儿 + supervisor 竞争
	if st := sm.Current(); st == core.StateUp || st == core.StateReady || st == core.StateStarting {
		return fmt.Errorf("service %s is already running (state: %s)", name, st)
	}

	svcConfig := svcEntry.Config

	// 构建命令（runtime 解析）
	// REQ-F-028, REQ-F-029: runtime 别名解析
	command := svcConfig.Command
	if svcConfig.Runtime != "" {
		registry := config.BuildRegistry(o.Config.Runtimes, o.Discovery.Runtimes)
		if rt, err := config.Resolve(registry, svcConfig.Runtime); err == nil && rt.Available {
			command = append([]string{rt.AbsPath}, command...)
		}
	}

	env := os.Environ()
	workdir := svcConfig.Workdir
	if workdir == "" {
		workdir = filepath.Dir(svcEntry.ConfigPath)
	}

	// REQ-D-004: service_lifecycle:pre_start — 服务启动前触发
	if o.ServiceLifecycleTrigger != nil {
		o.ServiceLifecycleTrigger.OnPreStart(context.Background(), name)
	}

	// 状态转移:
	// - pending → starting: 通过 depends_ready（简化原则：依赖管理只做提示，手动启动时直接触发）
	// - down/failed → starting: 通过 manual_start
	currentState := sm.Current()
	if currentState == core.StatePending {
		sm.Transition(core.EventDependsReady)
	} else {
		sm.Transition(core.EventManualStart)
	}

	// fd_notify readiness 需在 StartProcess 前创建 checker，
	// 以便通过 cmd.ExtraFiles 将管道写端传递给子进程（fd=3）
	var preChecker core.ReadinessChecker
	var extraFiles []*os.File
	if svcConfig.Readiness != nil && svcConfig.Readiness.Type == "fd_notify" {
		nc, cerr := core.NewNotifyChecker(svcConfig.Readiness)
		if cerr != nil {
			sm.Transition(core.EventMaxRetries)
			o.writeServiceLog(name, "error", fmt.Sprintf("fd_notify checker create failed: %s", cerr))
			return fmt.Errorf("readiness fd_notify for %s: %w", name, cerr)
		}
		preChecker = nc
		extraFiles = []*os.File{nc.WriterFd()}
	}

	// 启动子进程
	// REQ-F-023, §2.2.13: 通过 StartServiceProcess 解析 user 字段，
	// user 不存在或非 root 切换其他用户时返回 *ServiceError 拒绝启动
	proc, err := core.StartServiceProcess(name, command, env, workdir, svcConfig.User, svcEntry.ConfigPath, extraFiles...)
	if err != nil {
		if preChecker != nil {
			preChecker.Close()
		}
		sm.Transition(core.EventMaxRetries)
		// 启动失败原因写入服务日志，用户可通过日志页面查看
		// N-04-USER-CRED: ServiceError 会被 fmt.Errorf %w 包裹，service_ops.go 通过 errors.As 识别并映射 HTTP 422
		slog.Error("failed to start service",
			"service", name,
			"user", svcConfig.User,
			"config_path", svcEntry.ConfigPath,
			"error", err)
		o.writeServiceLog(name, "error", fmt.Sprintf("start failed: %s", err))
		return fmt.Errorf("start process: %w", err)
	}

	// fd_notify: StartProcess 成功后关闭 supd 侧的管道写端
	// 子进程关闭写端后，supd 读端能收到 EOF
	// C-01-001 修复：CloseWriter 错误记录日志，便于诊断管道异常
	if nc, ok := preChecker.(*core.NotifyChecker); ok {
		if err := nc.CloseWriter(); err != nil {
			slog.Warn("close notify pipe writer failed", "service", name, "error", err)
		}
	}

	// 状态转移: starting → up
	sm.Transition(core.EventProcessStarted)

	o.ProcessMgr.Register(name, proc)

	// REQ-F-010: 创建并启动服务日志器，捕获进程 stdout/stderr
	// N-G-01 修复：传入 logging.max_size_mb / max_files 配置，使日志轮转生效
	logBaseDir := filepath.Join(o.LogDir, "services")
	maxSizeMB, maxFiles := 0, 0
	if cfg := svcConfig.Logging; cfg != nil {
		maxSizeMB, maxFiles = cfg.MaxSizeMB, cfg.MaxFiles
	}
	svcLogger, loggerErr := logging.NewServiceLogger(name, logBaseDir, maxSizeMB, maxFiles)
	if loggerErr != nil {
		slog.Error("create service logger failed", "service", name, "error", loggerErr)
	} else {
		svcLogger.Start(proc.StdoutPipe(), proc.StderrPipe())
		o.loggersMu.Lock()
		// 关闭旧 logger（重启场景）
		if old, ok := o.loggers[name]; ok {
			old.Close()
		}
		if o.loggers == nil {
			o.loggers = make(map[string]*logging.ServiceLogger)
		}
		o.loggers[name] = svcLogger
		o.loggersMu.Unlock()
	}

	// REQ-F-009: readiness 检查（异步执行，不阻塞 API 响应）
	if svcConfig.Readiness != nil {
		if preChecker != nil {
			// fd_notify: 使用在 StartProcess 前创建的 checker
			go o.runReadinessCheck(context.Background(), name, svcConfig.Readiness, sm, proc, preChecker)
		} else {
			checker, cerr := core.NewReadinessChecker(svcConfig.Readiness, workdir)
			if cerr != nil {
				slog.Error("create readiness checker failed", "service", name, "error", cerr)
				sm.Transition(core.EventReadinessTimeout)
			} else {
				go o.runReadinessCheck(context.Background(), name, svcConfig.Readiness, sm, proc, checker)
			}
		}
	}

	// 记录服务启动历史
	if o.HistoryStore != nil {
		o.HistoryStore.RecordStart(name, proc.PID())
	}

	// 修复：API 启动的服务需要 supervisor goroutine 监控进程退出
	// 否则进程崩溃后状态机永远停在 up，不触发重启/failed/事件
	engine := core.BuildRestartEngine(o.Config, svcConfig)
	engine.RecordStart()
	o.restartEnginesMu.Lock()
	if o.RestartEngines == nil {
		o.RestartEngines = make(map[string]*core.RestartEngine)
	}
	o.RestartEngines[name] = engine
	o.restartEnginesMu.Unlock()

	// 创建 cancel context 用于停止退避等待中的服务
	svcCtx, svcCancel := context.WithCancel(context.Background())
	o.cancelFuncsMu.Lock()
	if o.cancelFuncs == nil {
		o.cancelFuncs = make(map[string]context.CancelFunc)
	}
	o.cancelFuncs[name] = svcCancel
	o.cancelFuncsMu.Unlock()

	go o.superviseService(svcCtx, name, svcEntry, sm, proc, engine)

	return nil
}

// runReadinessCheck 异步执行就绪检查，通过则转 ready，超时则转 failed
func (o *CoreServiceOperator) runReadinessCheck(ctx context.Context, name string, readinessCfg *config.ReadinessConfig, sm *core.StateMachine, proc *core.Process, checker core.ReadinessChecker) {
	defer checker.Close()

	interval := time.Duration(readinessCfg.IntervalSeconds) * time.Second
	if interval <= 0 {
		interval = time.Second
	}
	timeout := time.Duration(readinessCfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := checker.Check(checkCtx); err == nil {
				// REQ-F-004: up→ready，readiness 检查通过
				sm.Transition(core.EventReadinessPassed)
				// REQ-D-004: service_lifecycle:post_ready
				if o.ServiceLifecycleTrigger != nil {
					o.ServiceLifecycleTrigger.OnPostReady(context.Background(), name, proc.PID())
				}
				return
			}
		case <-proc.Done():
			slog.Warn("process exited during readiness check", "service", name)
			return
		case <-checkCtx.Done():
			// REQ-F-004: up→failed，readiness 检查超时
			slog.Error("readiness check timeout", "service", name)
			sm.Transition(core.EventReadinessTimeout)
			// C-01-001 修复：KillProcessGroup 错误需要记录，便于运维诊断（readiness 超时路径）
			if killErr := proc.KillProcessGroup(); killErr != nil {
				slog.Warn("kill process group after readiness timeout failed", "service", name, "error", killErr)
			}
			return
		case <-ctx.Done():
			return
		}
	}
}

// superviseService 监督 API 启动的服务进程，处理退出后的自动重启
// 修复：API 启动的服务缺少进程退出监控，导致进程崩溃后状态机永远停在 up
// 逻辑与 core.Bootstrap.superviseService 保持一致
func (o *CoreServiceOperator) superviseService(ctx context.Context, name string, svcEntry *watch.ServiceEntry, sm *core.StateMachine, proc *core.Process, engine *core.RestartEngine) {
	// supervisor 退出时清理 cancelFuncs
	defer func() {
		o.cancelFuncsMu.Lock()
		delete(o.cancelFuncs, name)
		o.cancelFuncsMu.Unlock()
	}()

	exitCode, signaled, sig := proc.Wait()

	slog.Debug("process exited (api-started)",
		"service", name,
		"exitCode", exitCode,
		"signaled", signaled,
		"signal", sig)

	// 进程退出信息写入服务日志，用户可通过日志页面查看退出原因
	if signaled {
		o.writeServiceLog(name, "warn", fmt.Sprintf("process killed by signal: %s (exit code: %d)", sig, exitCode))
	} else if exitCode != 0 {
		o.writeServiceLog(name, "warn", fmt.Sprintf("process exited with code: %d", exitCode))
	} else {
		// 正常退出（code=0），但对于服务进程来说通常是异常的
		o.writeServiceLog(name, "warn", "process exited unexpectedly (exit code: 0)")
	}

	// REQ-2.9.7: 进程退出时发布 service_died/service_exited 事件
	if o.EventPublisher != nil {
		eventType := "service_exited"
		if signaled {
			eventType = "service_died"
		}
		o.EventPublisher.Publish(eventType, map[string]any{
			"service":   name,
			"exit_code": exitCode,
			"signaled":  signaled,
			"signal":    int(sig),
		})
	}

	// REQ-D-004: service_lifecycle:on_failure — 服务失败后触发
	// 手动停止不算 failure（REQ-D-004, 2.1.4）
	// N-A-02 修复：对齐 bootstrap.go 的检查，同时排除 StateDown（手动停止完成后状态）
	currentState := sm.Current()
	if o.ServiceLifecycleTrigger != nil && currentState != core.StateStopping && currentState != core.StateDown {
		if exitCode != 0 || signaled {
			sigInt := 0
			if signaled {
				sigInt = int(sig)
			}
			o.ServiceLifecycleTrigger.OnFailure(ctx, name, exitCode, sigInt, engine.Retries())
		}
	}

	// 如果服务已不在运行状态（手动停止、已失败等），不重启
	if currentState != core.StateUp && currentState != core.StateReady && currentState != core.StateStarting {
		return
	}

	// REQ-F-008: 检查重启策略
	engine.ResetIfNeeded()
	if !engine.ShouldRestart(exitCode, signaled, sig) {
		if engine.Policy() == core.RestartNever {
			sm.Transition(core.EventMaxRetries) // → failed
		} else {
			// on-failure + 正常退出：进入 down
			sm.ResetTo(core.StateDown)
		}
		return
	}

	if engine.MaxRetriesReached() {
		sm.Transition(core.EventMaxRetries)
		return
	}

	// 重启允许
	engine.IncrementRetries()
	if _, ok := sm.Transition(core.EventRestartAllowed); !ok {
		return
	}

	delay := engine.BackoffDuration()
	slog.Info("restarting service after unexpected exit (api-started)",
		"service", name,
		"attempt", engine.Retries(),
		"delay", delay)

	// 退避等待：可被停止请求中断
	select {
	case <-time.After(delay):
		// 正常等待结束，继续重启
	case <-ctx.Done():
		// 被停止请求中断，直接转 down
		slog.Info("restart backoff aborted by stop request (api-started)", "service", name)
		// 状态转换可能已由 StopService 执行，此处幂等处理
		if sm.Current() == core.StateStarting {
			sm.Transition(core.EventStopRequested) // starting → stopping
			sm.Transition(core.EventBackoffAbort)  // stopping → down
		}
		// 清理 cancelFuncs
		o.cancelFuncsMu.Lock()
		delete(o.cancelFuncs, name)
		o.cancelFuncsMu.Unlock()
		return
	}

	// 退避等待结束后重新检查状态，防止等待期间状态已被外部改变（如手动停止）
	if sm.Current() != core.StateStarting {
		slog.Info("service state changed during backoff, skip restart", "service", name, "state", sm.Current())
		o.cancelFuncsMu.Lock()
		delete(o.cancelFuncs, name)
		o.cancelFuncsMu.Unlock()
		return
	}

	svcConfig := svcEntry.Config

	// 构建命令（runtime 解析）
	command := svcConfig.Command
	if svcConfig.Runtime != "" {
		registry := config.BuildRegistry(o.Config.Runtimes, o.Discovery.Runtimes)
		if rt, err := config.Resolve(registry, svcConfig.Runtime); err == nil && rt.Available {
			command = append([]string{rt.AbsPath}, command...)
		}
	}

	env := os.Environ()
	workdir := svcConfig.Workdir
	if workdir == "" {
		workdir = filepath.Dir(svcEntry.ConfigPath)
	}

	// fd_notify readiness 需在 StartProcess 前创建 checker
	var preChecker core.ReadinessChecker
	var extraFiles []*os.File
	if svcConfig.Readiness != nil && svcConfig.Readiness.Type == "fd_notify" {
		checker, cerr := core.NewReadinessChecker(svcConfig.Readiness, workdir)
		if cerr != nil {
			slog.Error("readiness fd_notify for restart", "service", name, "error", cerr)
			sm.Transition(core.EventMaxRetries)
			return
		}
		preChecker = checker
		if nc, ok := preChecker.(interface{ WriterFd() *os.File }); ok {
			extraFiles = []*os.File{nc.WriterFd()}
		}
	}

	// 启动新进程
	// REQ-F-023, §2.2.13: 通过 StartServiceProcess 解析 user 字段，
	// user 不存在或非 root 切换其他用户时返回 *ServiceError 拒绝启动
	newProc, err := core.StartServiceProcess(name, command, env, workdir, svcEntry.Config.User, svcEntry.ConfigPath, extraFiles...)
	if err != nil {
		if preChecker != nil {
			preChecker.Close()
		}
		slog.Error("failed to restart service",
			"service", name,
			"user", svcEntry.Config.User,
			"config_path", svcEntry.ConfigPath,
			"error", err)
		// 启动失败原因写入服务日志，用户可通过日志页面查看
		o.writeServiceLog(name, "error", fmt.Sprintf("restart failed: %s", err))
		sm.Transition(core.EventMaxRetries)
		return
	}

	// fd_notify: 关闭 supd 侧的管道写端
	// C-01-001 修复：CloseWriter 错误记录日志，便于诊断管道异常
	if nc, ok := preChecker.(interface{ CloseWriter() error }); ok {
		if err := nc.CloseWriter(); err != nil {
			slog.Warn("close notify pipe writer failed", "service", name, "error", err)
		}
	}

	// 状态转移: starting → up
	sm.Transition(core.EventProcessStarted)
	engine.RecordStart()

	// 更新进程管理器
	o.ProcessMgr.Register(name, newProc)

	// 创建新日志器（关闭旧 logger 避免 fd 泄漏）
	// N-G-01 修复：传入 logging 配置使轮转生效
	logBaseDir := filepath.Join(o.LogDir, "services")
	maxSizeMB, maxFiles := 0, 0
	if cfg := svcEntry.Config.Logging; cfg != nil {
		maxSizeMB, maxFiles = cfg.MaxSizeMB, cfg.MaxFiles
	}
	o.loggersMu.Lock()
	if oldLogger, ok := o.loggers[name]; ok && oldLogger != nil {
		// C-01-DISCARD-001 修复：记录 Close 错误而非丢弃，便于排查 fd 泄漏
		if closeErr := oldLogger.Close(); closeErr != nil {
			slog.Warn("close old service logger failed on restart",
				"service", name, "error", closeErr)
		}
	}
	newLogger, loggerErr := logging.NewServiceLogger(name, logBaseDir, maxSizeMB, maxFiles)
	if loggerErr != nil {
		slog.Error("create service logger failed on restart", "service", name, "error", loggerErr)
	} else {
		newLogger.Start(newProc.StdoutPipe(), newProc.StderrPipe())
		o.loggers[name] = newLogger
	}
	o.loggersMu.Unlock()

	// 记录重启历史
	if o.HistoryStore != nil {
		o.HistoryStore.RecordStart(name, newProc.PID())
	}

	// 为新进程启动 supervisor goroutine（使用新的 cancel context）
	newCtx, newCancel := context.WithCancel(context.Background())
	o.cancelFuncsMu.Lock()
	o.cancelFuncs[name] = newCancel
	o.cancelFuncsMu.Unlock()
	go o.superviseService(newCtx, name, svcEntry, sm, newProc, engine)

	// 异步执行 readiness 检查
	if svcConfig.Readiness != nil {
		if preChecker != nil {
			// fd_notify: 使用 preChecker
			go o.runReadinessCheck(newCtx, name, svcConfig.Readiness, sm, newProc, preChecker)
		} else {
			checker, cerr := core.NewReadinessChecker(svcConfig.Readiness, workdir)
			if cerr != nil {
				slog.Error("create readiness checker failed on restart", "service", name, "error", cerr)
				sm.Transition(core.EventReadinessTimeout)
			} else {
				go o.runReadinessCheck(newCtx, name, svcConfig.Readiness, sm, newProc, checker)
			}
		}
	}
}

func (o *CoreServiceOperator) StopService(name string) error {
	proc, ok := o.ProcessMgr.Get(name)
	if !ok {
		// 进程不存在：服务可能处于退避等待（starting 状态，无活跃进程）
		// 调用 cancel 中断退避等待，状态转换由 superviseService 的 ctx.Done() 分支处理
		o.cancelFuncsMu.Lock()
		if cancel, exists := o.cancelFuncs[name]; exists {
			cancel()
			delete(o.cancelFuncs, name)
		}
		o.cancelFuncsMu.Unlock()

		// 如果服务仍在 starting 状态（supervisor 还没来得及处理 ctx.Done），
		// 直接执行状态转换
		o.stateMachinesMu.RLock()
		sm, hasSM := o.StateMachines[name]
		o.stateMachinesMu.RUnlock()
		if hasSM && sm.Current() == core.StateStarting {
			sm.Transition(core.EventStopRequested) // starting → stopping
			sm.Transition(core.EventBackoffAbort)  // stopping → down
		}
		return nil
	}

	// 取消 superviseService 的 context，避免 OLD goroutine 在 RestartService 时
	// 与新启动的 supervisor 竞争（旧 goroutine 检测到 proc.Wait() 返回后可能误触发重启）
	o.cancelFuncsMu.Lock()
	if cancel, exists := o.cancelFuncs[name]; exists {
		cancel()
		delete(o.cancelFuncs, name)
	}
	o.cancelFuncsMu.Unlock()

	o.stateMachinesMu.RLock()
	sm, hasSM := o.StateMachines[name]
	if hasSM {
		sm.Transition(core.EventStopRequested)
	}

	stopCfg := core.DefaultStopConfig()
	if svcEntry, ok := o.Discovery.Services[name]; ok && svcEntry.Config != nil && svcEntry.Config.Stop != nil {
		if svcEntry.Config.Stop.GraceSeconds > 0 {
			stopCfg.GraceSeconds = svcEntry.Config.Stop.GraceSeconds
		}
		if svcEntry.Config.Stop.TimeoutSeconds > 0 {
			stopCfg.TimeoutSeconds = svcEntry.Config.Stop.TimeoutSeconds
		}
	}
	o.stateMachinesMu.RUnlock()

	// REQ-D-004: service_lifecycle:pre_stop — 服务停止前触发
	var runPreStop func() error
	if o.ServiceLifecycleTrigger != nil {
		svcName := name
		svcPID := proc.PID()
		trigger := o.ServiceLifecycleTrigger
		runPreStop = func() error {
			trigger.OnPreStop(context.Background(), svcName, svcPID)
			return nil
		}
	}

	stopResult, err := core.StopService(context.Background(), proc, stopCfg, runPreStop)
	o.ProcessMgr.Unregister(name)
	o.closeLogger(name)

	// 记录服务停止历史
	if o.HistoryStore != nil {
		duration := int64(time.Since(proc.StartTime()).Seconds())
		reason := "manual_stop"
		if err != nil {
			reason = "stop_error"
		}
		o.HistoryStore.RecordStop(name, proc.PID(), stopResult.ExitCode, duration, reason)
	}

	if hasSM && sm.Current() == core.StateStopping {
		sm.Transition(core.EventProcessExited)
	}

	return err
}

// closeLogger 关闭并清理服务日志器（进程退出后调用）
func (o *CoreServiceOperator) closeLogger(name string) {
	o.loggersMu.Lock()
	if logger, ok := o.loggers[name]; ok {
		delete(o.loggers, name)
		o.loggersMu.Unlock()
		logger.Wait() // 等待 goroutine 退出（进程已退出，EOF 触发，不会阻塞）
		// C-01-001 修复：记录 Close 错误（如磁盘满导致 Flush 失败，便于运维感知日志丢失）
		if err := logger.Close(); err != nil {
			slog.Warn("close service logger failed", "service", name, "error", err)
		}
		return
	}
	o.loggersMu.Unlock()
}

// writeServiceLog 向服务日志文件写入自定义消息（如启动失败原因）
// 即使没有 ServiceLogger 实例，也直接写入日志文件
func (o *CoreServiceOperator) writeServiceLog(name string, level, message string) {
	o.loggersMu.Lock()
	if logger, ok := o.loggers[name]; ok {
		logger.WriteLine(level, message)
		o.loggersMu.Unlock()
		return
	}
	o.loggersMu.Unlock()

	// 没有 ServiceLogger 实例，直接写入日志文件
	// N-G-01 修复：使用默认轮转参数（writeServiceLog 无 svcConfig 上下文）
	logBaseDir := filepath.Join(o.LogDir, "services")
	logger, err := logging.NewServiceLogger(name, logBaseDir, 0, 0)
	if err != nil {
		slog.Error("create service logger for error message failed", "service", name, "error", err)
		return
	}
	logger.WriteLine(level, message)
	// C-01-001 修复：fallback 路径也记录 Close 错误
	if err := logger.Close(); err != nil {
		slog.Error("close fallback service logger failed", "service", name, "error", err)
	}
}

func (o *CoreServiceOperator) RestartService(name string) error {
	if err := o.StopService(name); err != nil {
		return fmt.Errorf("stop failed: %w", err)
	}
	return o.StartService(name)
}

func (o *CoreServiceOperator) SendSignal(name string, signal syscall.Signal) error {
	return o.ProcessMgr.SendSignal(name, signal)
}

// ForceStopService 强制停止服务（SIGKILL）
func (o *CoreServiceOperator) ForceStopService(name string) error {
	proc, ok := o.ProcessMgr.Get(name)
	if !ok {
		// 进程不存在：服务可能处于退避等待，与 StopService 同样处理
		o.cancelFuncsMu.Lock()
		if cancel, exists := o.cancelFuncs[name]; exists {
			cancel()
			delete(o.cancelFuncs, name)
		}
		o.cancelFuncsMu.Unlock()

		o.stateMachinesMu.RLock()
		sm, hasSM := o.StateMachines[name]
		o.stateMachinesMu.RUnlock()
		if hasSM && sm.Current() == core.StateStarting {
			sm.Transition(core.EventStopRequested)
			sm.Transition(core.EventBackoffAbort)
		}
		return nil
	}

	// 取消 superviseService 的 context，避免 OLD goroutine 在重启时与新 supervisor 竞争
	o.cancelFuncsMu.Lock()
	if cancel, exists := o.cancelFuncs[name]; exists {
		cancel()
		delete(o.cancelFuncs, name)
	}
	o.cancelFuncsMu.Unlock()

	o.stateMachinesMu.RLock()
	sm, hasSM := o.StateMachines[name]
	if hasSM {
		sm.Transition(core.EventStopRequested)
	}
	o.stateMachinesMu.RUnlock()

	// 向进程组发送 SIGKILL
	_ = syscall.Kill(-proc.PID(), syscall.SIGKILL)

	// 等待进程退出
	<-proc.Done()

	o.ProcessMgr.Unregister(name)
	o.closeLogger(name)

	// 记录强制停止历史
	if o.HistoryStore != nil {
		duration := int64(time.Since(proc.StartTime()).Seconds())
		o.HistoryStore.RecordStop(name, proc.PID(), -1, duration, "force_killed")
	}

	if hasSM && sm.Current() == core.StateStopping {
		sm.Transition(core.EventProcessExited)
	}

	return nil
}

// ClearFailedState 清除失败状态，重置为 pending
func (o *CoreServiceOperator) ClearFailedState(name string) error {
	o.stateMachinesMu.RLock()
	sm, ok := o.StateMachines[name]
	o.stateMachinesMu.RUnlock()
	if !ok {
		return fmt.Errorf("state machine for %s not found", name)
	}
	if sm.Current() != core.StateFailed {
		return fmt.Errorf("service %s is not in failed state (current: %s)", name, sm.Current())
	}
	sm.ResetTo(core.StatePending)
	return nil
}
