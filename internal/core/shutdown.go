package core

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// ShutdownCoordinator 优雅退出协调器
// REQ-F-032: supd退出流程（6步：pre_shutdown→停止接受请求→反序停服务→等扩展任务→关HTTP→退出）
// REQ-F-047: shutdown_grace_seconds控制关机流程上限，超时强杀
type ShutdownCoordinator struct {
	processManager *ProcessManager
	depGraph       *DependencyGraph
	stateMachines  map[string]*StateMachine // 服务名→状态机
	stopConfigs    map[string]StopConfig    // 服务名→停止配置
	graceSeconds   int                     // shutdown_grace_seconds（默认30秒）
	shutdownCh     chan struct{}            // 关机请求信号channel，close表示已请求关机
	shutdownOnce   sync.Once               // 确保只触发一次关机

	// REQ-D-004: 生命周期触发回调（由 run.go 注入）
	// supd_lifecycle:pre_shutdown — supd 退出第1步触发
	runPreShutdown func(ctx context.Context) error
	// service_lifecycle:pre_stop — 服务停止前触发，返回 runPreStop 闭包
	preStopHook func(serviceName string, servicePID int) func() error
}

// NewShutdownCoordinator 创建优雅退出协调器
// REQ-F-032: 优雅退出协调器
// REQ-F-047: shutdown_grace_seconds默认30秒
func NewShutdownCoordinator(pm *ProcessManager, dg *DependencyGraph, sms map[string]*StateMachine, stopCfgs map[string]StopConfig, graceSeconds int) *ShutdownCoordinator {
	if graceSeconds <= 0 {
		graceSeconds = 30 // REQ-F-047: shutdown_grace_seconds默认30秒（数值锁定）
	}
	return &ShutdownCoordinator{
		processManager: pm,
		depGraph:       dg,
		stateMachines:  sms,
		stopConfigs:    stopCfgs,
		graceSeconds:   graceSeconds,
		shutdownCh:     make(chan struct{}),
	}
}

// SetPreShutdownHook 设置 supd_lifecycle:pre_shutdown 回调
// REQ-D-004, 2.8.1: supd 退出第1步触发
func (sc *ShutdownCoordinator) SetPreShutdownHook(hook func(ctx context.Context) error) {
	sc.runPreShutdown = hook
}

// SetPreStopHook 设置 service_lifecycle:pre_stop 回调
// REQ-D-004, 2.1.4: 服务停止前触发
func (sc *ShutdownCoordinator) SetPreStopHook(hook func(serviceName string, servicePID int) func() error) {
	sc.preStopHook = hook
}

// GracefulShutdown 执行优雅关机流程
// REQ-F-032: supd退出流程（6步）
// REQ-F-047: shutdown_grace_seconds控制整个关机流程上限，超时→强制SIGKILL所有剩余进程
func (sc *ShutdownCoordinator) GracefulShutdown(ctx context.Context) error {
	// 标记关机请求
	sc.shutdownOnce.Do(func() {
		close(sc.shutdownCh)
	})

	// REQ-F-047: 如果ctx没有deadline，创建shutdown_grace_seconds的context
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(sc.graceSeconds)*time.Second)
		defer cancel()
	}

	// 使用channel收集结果，避免共享状态（REQ-C-003: goroutine间用channel通信）
	resultCh := make(chan error, 1)
	go func() {
		resultCh <- sc.executeShutdown(ctx)
	}()

	select {
	case err := <-resultCh:
		return err
	case <-ctx.Done():
		// REQ-F-047: 关机超时→强制SIGKILL所有剩余进程
		slog.Warn("shutdown: grace period exceeded, force killing all remaining processes")
		sc.forceKillAll()
		return ctx.Err()
	}
}

// executeShutdown 执行关机的6个步骤
// REQ-F-032: 6步关机流程
func (sc *ShutdownCoordinator) executeShutdown(ctx context.Context) error {
	// Step 1: 触发supd_lifecycle:pre_shutdown扩展
	// REQ-D-004, 2.8.1: supd 退出第1步，触发 pre_shutdown 扩展
	if sc.runPreShutdown != nil {
		if err := sc.runPreShutdown(ctx); err != nil {
			slog.Warn("shutdown: pre_shutdown hook error", "error", err)
		}
	}

	// Step 2: 标记"停止接受新请求"
	// 由 run.go 中处理

	// Step 3: 按依赖反序停止所有运行中的服务
	// REQ-F-032: 每个服务执行完整停止流程，含pre_stop扩展，受各自的stop.timeout_seconds约束
	if err := sc.stopServicesInOrder(ctx); err != nil {
		slog.Warn("shutdown: error stopping services", "error", err)
	}

	// Step 4: 等待运行中扩展任务结束（最多30秒）
	// 由 run.go 中等待扩展任务结束
	// REQ-F-032: 等待扩展任务最多30秒

	// Step 5: 关闭HTTP服务器
	// 由 run.go 中关闭

	// Step 6: 退出
	return nil
}

// stopServicesInOrder 按依赖反序停止服务
// REQ-F-032: 按依赖反序停止所有运行中的服务
// 同层并行停止，同层全部停止后才停止下一层
func (sc *ShutdownCoordinator) stopServicesInOrder(ctx context.Context) error {
	// REQ-F-032: 按依赖反序层级停止
	layers := sc.depGraph.ReverseLayers()
	if len(layers) == 0 {
		return nil
	}

	var firstErr error
	for _, layer := range layers {
		if err := sc.stopServiceLayer(ctx, layer); err != nil && firstErr == nil {
			firstErr = err
		}
		// 即使某层出错也继续处理下一层
	}
	return firstErr
}

// stopServiceLayer 并行停止同层服务
// REQ-F-032: 同层服务并行停止，同层全部停止后才停止下一层
// 同层并行停止的总时长 = max(各服务stop.timeout_seconds)
func (sc *ShutdownCoordinator) stopServiceLayer(ctx context.Context, layer []string) error {
	// 过滤出需要停止的服务（正在运行中的）
	var runningServices []string
	for _, name := range layer {
		if sm, ok := sc.stateMachines[name]; ok {
			state := sm.Current()
			// REQ-F-032: 只停止运行中的服务
			if state == StateUp || state == StateReady || state == StateStarting {
				runningServices = append(runningServices, name)
			}
		}
	}

	if len(runningServices) == 0 {
		return nil
	}

	// REQ-F-032: 同层并行停止的总时长 = max(各服务stop.timeout_seconds)
	var maxTimeout int
	for _, name := range runningServices {
		cfg, ok := sc.stopConfigs[name]
		if !ok {
			cfg = DefaultStopConfig()
		}
		if cfg.TimeoutSeconds > maxTimeout {
			maxTimeout = cfg.TimeoutSeconds
		}
	}

	// 创建层级超时context（如果父context的deadline更短则自动继承）
	layerCtx, cancel := context.WithTimeout(ctx, time.Duration(maxTimeout)*time.Second)
	defer cancel()

	// REQ-F-032: 同层并行停止，每个服务一个goroutine
	errCh := make(chan error, len(runningServices))
	var wg sync.WaitGroup

	for _, name := range runningServices {
		wg.Add(1)
		go func(serviceName string) {
			defer wg.Done()
			errCh <- sc.stopSingleService(layerCtx, serviceName)
		}(name)
	}

	// 等待所有服务停止完成
	go func() {
		wg.Wait()
		close(errCh)
	}()

	// 收集错误
	var firstErr error
	for err := range errCh {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// stopSingleService 停止单个服务
// REQ-F-032: 每个服务执行完整停止流程：pre_stop扩展→SIGTERM→等grace_seconds→SIGKILL
// 状态变更：stopping → down
// A-07-001 修复：starting 状态服务在关机时使用 ResetTo(StateDown) 绕过状态机规则
func (sc *ShutdownCoordinator) stopSingleService(ctx context.Context, name string) error {
	sm, ok := sc.stateMachines[name]
	if !ok {
		return fmt.Errorf("shutdown: state machine not found for service %s", name)
	}

	// REQ-F-032: 状态变更为stopping
	// A-07-001 修复：注释错误已修正 — 状态机允许 starting→stopping（规则4）
	// 关机时 starting 状态服务也应走正常 stopping 流程
	currentState := sm.Current()
	switch currentState {
	case StateUp, StateReady, StateStarting:
		// 规则4: up/ready/starting → stopping，通过 EventStopRequested 触发
		// A-07-001 修复：starting 状态使用 Transition 而非 ResetTo(StateDown) 绕过
		if _, transitioned := sm.Transition(EventStopRequested); !transitioned {
			slog.Warn("shutdown: failed to transition to stopping",
				"service", name, "current_state", currentState)
		}
	default:
		// pending/stopping/down/failed 等状态：跳过状态转移
	}

	// 获取进程
	proc, ok := sc.processManager.Get(name)
	if !ok {
		// 进程不存在，可能已退出
		if sm.Current() == StateStopping {
			sm.Transition(EventProcessExited)
		}
		return nil
	}

	// 获取停止配置
	cfg, ok := sc.stopConfigs[name]
	if !ok {
		cfg = DefaultStopConfig()
	}

	// REQ-F-032: 调用StopService执行完整停止流程（含pre_stop扩展）
	// REQ-D-004: service_lifecycle:pre_stop — 服务停止前触发
	var runPreStop func() error
	if sc.preStopHook != nil {
		runPreStop = sc.preStopHook(name, proc.PID())
	}
	_, err := StopService(ctx, proc, cfg, runPreStop)

	// 从ProcessManager注销
	sc.processManager.Unregister(name)

	// REQ-F-032: 状态变更为down
	if sm.Current() == StateStopping {
		sm.Transition(EventProcessExited)
	}

	return err
}

// forceKillAll 强制SIGKILL所有剩余进程
// REQ-F-047: 关机超时→强制SIGKILL所有剩余进程，supd立即退出
func (sc *ShutdownCoordinator) forceKillAll() {
	names := sc.processManager.List()
	for _, name := range names {
		if err := sc.processManager.KillProcessGroup(name); err != nil {
			slog.Warn("shutdown: force kill failed", "service", name, "error", err)
		}
		sc.processManager.Unregister(name)
	}
}

// ShutdownRequested 检查是否已请求关机
// REQ-F-032: 启动中收到SIGTERM/SIGINT时，检查此标志以决定是否继续拉起服务
func (sc *ShutdownCoordinator) ShutdownRequested() bool {
	select {
	case <-sc.shutdownCh:
		return true
	default:
		return false
	}
}
