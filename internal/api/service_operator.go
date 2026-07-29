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
	cancelFuncs           map[string]context.CancelFunc
	cancelFuncsMu         sync.Mutex
	LifecycleLocks        *core.LifecycleLocks
	DependencyCoordinator *core.DependencyCoordinator
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

// CanAutoRecover 判断服务是否可被依赖恢复协调器自动拉起。
// 条件：autostart 启用 + 当前处于 pending + 所有依赖均已进入可依赖终态
// （有 readiness 的依赖须 ready，无 readiness 的依赖须 up）。
// 作为 DependencyCoordinator 的 canStart 回调使用。
func (o *CoreServiceOperator) CanAutoRecover(name string) bool {
	o.stateMachinesMu.RLock()
	defer o.stateMachinesMu.RUnlock()
	svc, ok := o.Discovery.Services[name]
	if !ok || svc.Config == nil || !isServiceAutostart(svc.Config) {
		return false
	}
	sm, ok := o.StateMachines[name]
	if !ok || sm.Current() != core.StatePending {
		return false
	}
	for _, dep := range svc.Config.DependsOn {
		depEntry, exists := o.Discovery.Services[dep]
		depSM, hasSM := o.StateMachines[dep]
		if !exists || !hasSM || depEntry.Config == nil {
			return false
		}
		state := depSM.Current()
		if depEntry.Config.Readiness != nil {
			if state != core.StateReady {
				return false
			}
		} else if state != core.StateUp {
			return false
		}
	}
	return true
}

// isServiceAutostart 判断服务是否启用自动启动（nil 视为 true，与规格默认值一致）。
func isServiceAutostart(svc *config.ServiceConfig) bool {
	return svc.Autostart == nil || *svc.Autostart
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

// UpdateRestartEngines 热重载"立即生效"：用最新全局/服务配置原地更新所有 RestartEngine。
// 规格 §2.4.3: service.yaml:restart 与 config.yaml:defaults.restart 变更均"立即生效"。
// 保留各 engine 的 retries/lastStartTime 运行时状态，仅更新 policy/backoff/maxRetries 等配置字段。
// 例如服务在无限重试（max_retries=0）时，用户改为 max_retries=5 后下次 MaxRetriesReached 即为 true。
func (o *CoreServiceOperator) UpdateRestartEngines(cfg *config.Config, discovery *watch.DiscoveryResult) {
	if o == nil || cfg == nil || discovery == nil {
		return
	}
	o.restartEnginesMu.Lock()
	defer o.restartEnginesMu.Unlock()
	for name, engine := range o.RestartEngines {
		svcEntry, ok := discovery.Services[name]
		if !ok {
			continue // 服务已被删除，跳过（其 engine 会在服务清理时移除）
		}
		// 用最新配置构建临时 engine，再原地将配置同步到现有 engine
		freshEngine := core.BuildRestartEngine(cfg, svcEntry.Config)
		engine.SyncConfigFrom(freshEngine)
	}
}

// StartService 启动服务。先获取按服务名粒度的生命周期锁，
// 与关机路径、自动重启路径互斥，避免并发 start/stop/restart 导致状态机错乱。
func (o *CoreServiceOperator) StartService(name string) error {
	unlock := o.LifecycleLocks.Lock(name)
	defer unlock()
	return o.startServiceLocked(name)
}

// startServiceLocked 已持有生命周期锁的内部启动实现。
// RestartService 在同一把锁内依次调用 stopServiceLocked + startServiceLocked，避免重入死锁。
func (o *CoreServiceOperator) startServiceLocked(name string) error {
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
		rt, err := config.Resolve(registry, svcConfig.Runtime)
		if err != nil {
			return fmt.Errorf("service %s runtime %q: %w", name, svcConfig.Runtime, err)
		}
		command = append([]string{rt.AbsPath}, command...)
	}

	// 规格 §2.2.4: 服务进程合并 3 层 env（与 bootstrap.startService 一致）
	// BUG 修复: 此前仅用 os.Environ()，未加载 services/<svc>/env.yaml，违反规格 §2.2.4
	env := core.BuildServiceProcessEnv(o.BaseDir, name, o.Config.EnvFiles)
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
	// REQ-F-023, §2.2.13: 通过 StartServiceProcess 解析身份配置（user 或 uid 模式），
	// 身份解析失败或非 root 切换其他用户时返回 *ServiceError 拒绝启动
	proc, err := core.StartServiceProcess(name, command, env, workdir, svcConfig.ToCredentialSpec(), svcEntry.ConfigPath, extraFiles...)
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

	engine := core.BuildRestartEngine(o.Config, svcConfig)
	if svcConfig.Readiness == nil {
		engine.RecordStart()
	}
	o.restartEnginesMu.Lock()
	if o.RestartEngines == nil {
		o.RestartEngines = make(map[string]*core.RestartEngine)
	}
	o.RestartEngines[name] = engine
	o.restartEnginesMu.Unlock()

	// REQ-F-009: readiness 检查（异步执行，不阻塞 API 响应）
	if svcConfig.Readiness != nil {
		if preChecker != nil {
			// fd_notify: 使用在 StartProcess 前创建的 checker
			go o.runReadinessCheck(context.Background(), name, svcConfig.Readiness, sm, proc, engine, preChecker)
		} else {
			checker, cerr := core.NewReadinessChecker(svcConfig.Readiness, workdir, env)
			if cerr != nil {
				slog.Error("create readiness checker failed", "service", name, "error", cerr)
				sm.Transition(core.EventReadinessTimeout)
			} else {
				go o.runReadinessCheck(context.Background(), name, svcConfig.Readiness, sm, proc, engine, checker)
			}
		}
	}

	// 记录服务启动历史
	if o.HistoryStore != nil {
		o.HistoryStore.RecordStart(name, proc.PID())
	}

	if svcConfig.Readiness == nil && o.DependencyCoordinator != nil {
		o.DependencyCoordinator.OnServiceDependable(context.Background(), name)
	}

	// API 启动的服务同样启动 supervisor，监控退出并应用重启策略。
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
func (o *CoreServiceOperator) runReadinessCheck(ctx context.Context, name string, readinessCfg *config.ReadinessConfig, sm *core.StateMachine, proc *core.Process, engine *core.RestartEngine, checker core.ReadinessChecker) {
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
		if err := checker.Check(checkCtx); err == nil {
			// REQ-F-004: up→ready，readiness 检查通过
			if _, ok := sm.Transition(core.EventReadinessPassed); ok {
				engine.RecordStart()
				if o.DependencyCoordinator != nil {
					o.DependencyCoordinator.OnServiceDependable(ctx, name)
				}
				// REQ-D-004: service_lifecycle:post_ready
				if o.ServiceLifecycleTrigger != nil {
					o.ServiceLifecycleTrigger.OnPostReady(context.Background(), name, proc.PID())
				}
			}
			return
		}
		select {
		case <-ticker.C:
			continue
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
// M-04-001/TD-003 修复：抽取共享 RunSupervisor，此处为薄包装
func (o *CoreServiceOperator) superviseService(ctx context.Context, name string, svcEntry *watch.ServiceEntry, sm *core.StateMachine, proc *core.Process, engine *core.RestartEngine) {
	// supervisor 退出时清理 cancelFuncs（仅此一处，不再三重删除）
	defer func() {
		o.cancelFuncsMu.Lock()
		delete(o.cancelFuncs, name)
		o.cancelFuncsMu.Unlock()
	}()

	// 有意策略变更：一次性构建 RuntimeRegistry（与 bootstrap 一致），而非原每次重建
	// 实际场景中 runtime discovery 在运行期间变化极罕见
	registry := config.BuildRegistry(o.Config.Runtimes, o.Discovery.Runtimes)

	callbacks := core.SupervisorCallbacks{
		AcquireLifecycleLock: o.LifecycleLocks.Lock,
		WriteServiceLog:      o.writeServiceLog,
		PublishEvent:         o.EventPublisher,
		OnFailure: func(fCtx context.Context, n string, exitCode, signal, restartCount, pid int) {
			if o.ServiceLifecycleTrigger != nil {
				// 原行为：传 supervisor ctx（OnFailure 受 StopService cancel 控制）
				o.ServiceLifecycleTrigger.OnFailure(fCtx, n, exitCode, signal, restartCount, pid)
			}
		},
		RuntimeRegistry: registry,
		BuildEnv: func(n string) []string {
			return core.BuildServiceProcessEnv(o.BaseDir, n, o.Config.EnvFiles)
		},
		RegisterProcess: func(n string, p *core.Process) {
			o.ProcessMgr.Register(n, p)
		},
		RebuildLogger: o.rebuildLogger,
		RecordRestartHistory: func(n string, pid int) {
			if o.HistoryStore != nil {
				o.HistoryStore.RecordStart(n, pid)
			}
		},
		SpawnNextSupervisor: func(n string, entry *watch.ServiceEntry, s *core.StateMachine,
			p *core.Process, e *core.RestartEngine) context.Context {
			newCtx, newCancel := context.WithCancel(context.Background())
			o.cancelFuncsMu.Lock()
			o.cancelFuncs[n] = newCancel
			o.cancelFuncsMu.Unlock()
			go o.superviseService(newCtx, n, entry, s, p, e)
			// 返回 newCtx：service_operator 的 readiness 受 StopService cancelFuncs 控制（与原行为一致）
			return newCtx
		},
		OnDependable: func(depCtx context.Context, n string) {
			if o.DependencyCoordinator != nil {
				o.DependencyCoordinator.OnServiceDependable(depCtx, n)
			}
		},
		RunReadiness: func(rCtx context.Context, n string, entry *watch.ServiceEntry,
			s *core.StateMachine, p *core.Process, pre core.ReadinessChecker) error {
			// 异步模式：返回 nil（无同步 error，RunSupervisor 不处理 newProc.Done）
			if pre != nil {
				go o.runReadinessCheck(rCtx, n, entry.Config.Readiness, s, p, engine, pre)
			} else {
				workdir := entry.Config.Workdir
				if workdir == "" {
					workdir = filepath.Dir(entry.ConfigPath)
				}
				env := core.BuildServiceProcessEnv(o.BaseDir, n, o.Config.EnvFiles)
				checker, cerr := core.NewReadinessChecker(entry.Config.Readiness, workdir, env)
				if cerr != nil {
					slog.Error("create readiness checker failed on restart", "service", n, "error", cerr)
					s.Transition(core.EventReadinessTimeout)
				} else {
					go o.runReadinessCheck(rCtx, n, entry.Config.Readiness, s, p, engine, checker)
				}
			}
			return nil
		},
		Source: "api",
	}

	core.RunSupervisor(ctx, name, svcEntry, sm, proc, engine, callbacks)
}

// rebuildLogger 关闭旧日志器，创建并启动新日志器
// M-04-001 修复：从 superviseService 中提取为独立方法
func (o *CoreServiceOperator) rebuildLogger(name string, newProc *core.Process, maxSizeMB, maxFiles int) {
	logBaseDir := filepath.Join(o.LogDir, "services")
	o.loggersMu.Lock()
	defer o.loggersMu.Unlock()
	if oldLogger, ok := o.loggers[name]; ok && oldLogger != nil {
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
}

// StopService 停止服务。先获取生命周期锁再委托 stopServiceLocked。
func (o *CoreServiceOperator) StopService(name string) error {
	unlock := o.LifecycleLocks.Lock(name)
	defer unlock()
	return o.stopServiceLocked(name)
}

// stopServiceLocked 已持有生命周期锁的内部停止实现，供 RestartService 复用。
func (o *CoreServiceOperator) stopServiceLocked(name string) error {
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

// RestartService 重启服务。在同一把生命周期锁内依次 stop→start，
// 复用 *Locked 内部方法避免重入死锁，保证重启期间不被其他 start/stop 并发干扰。
func (o *CoreServiceOperator) RestartService(name string) error {
	unlock := o.LifecycleLocks.Lock(name)
	defer unlock()
	if err := o.stopServiceLocked(name); err != nil {
		return fmt.Errorf("stop failed: %w", err)
	}
	return o.startServiceLocked(name)
}

func (o *CoreServiceOperator) SendSignal(name string, signal syscall.Signal) error {
	return o.ProcessMgr.SendSignal(name, signal)
}

// ForceStopService 强制停止服务（SIGKILL）
func (o *CoreServiceOperator) ForceStopService(name string) error {
	unlock := o.LifecycleLocks.Lock(name)
	defer unlock()

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
	unlock := o.LifecycleLocks.Lock(name)
	defer unlock()

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
