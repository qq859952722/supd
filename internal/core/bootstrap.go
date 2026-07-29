package core

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/logging"
	"github.com/supdorg/supd/internal/watch"
)

// BootstrapConfig 启动配置
// REQ-F-033, REQ-F-034: supd 启动流程配置
type BootstrapConfig struct {
	ConfigPath     string         // config.yaml 路径
	BaseDir        string         // /etc/supd/
	LogDir         string         // /var/log/supd/
	NoPID1         bool           // --no-pid1 标志
	HTTPListen     string         // 覆盖 http_listen
	LogLevel       string         // 覆盖 log_level（--log-level 标志）
	EventPublisher EventPublisher // REQ-2.9.7: 事件发布器
	LifecycleLocks *LifecycleLocks

	// REQ-F-028: 运行时配置来源
	Runtimes           map[string]string // config.yaml 声明的运行时别名映射
	DiscoveredRuntimes map[string]string // 扫描发现的运行时别名映射

	// REQ-D-004: 生命周期触发回调（由 run.go 注入）
	// service_lifecycle:pre_start — 服务启动前触发
	OnServicePreStart func(ctx context.Context, serviceName string)
	// service_lifecycle:post_ready — 服务进入 ready 状态后触发
	OnServicePostReady func(ctx context.Context, serviceName string, servicePID int)
	// service_lifecycle:on_failure — 服务失败后触发（手动停止不算 failure）
	// A-05-001 修复：增加 restartCount 参数，传递 RestartEngine.Retries() 实际重启次数
	// 规格 §2.2.5: on_failure 时 SUPD_SERVICE_PID 为进程退出前的 PID
	OnServiceFailure func(ctx context.Context, serviceName string, exitCode, signal, restartCount, servicePID int)
	// supd_lifecycle:pre_start — supd 启动前触发（Step 9，在 startAutostartServices 之前）
	OnSupdPreStart func(ctx context.Context)
}

// BootstrapResult 启动结果
// REQ-F-033, REQ-F-034: 11步启动流程结果
type BootstrapResult struct {
	Config          *config.Config
	Discovery       *watch.DiscoveryResult
	DepGraph        *DependencyGraph
	StateMachines   map[string]*StateMachine
	ProcessMgr      *ProcessManager
	Watcher         *watch.Watcher
	Loggers         map[string]*logging.ServiceLogger
	RuntimeRegistry *config.RuntimeRegistry       // REQ-F-028: 运行时注册表
	RestartEngines  map[string]*RestartEngine     // REQ-F-008: 每服务重启策略引擎
	CancelFuncs     map[string]context.CancelFunc // supervisor goroutine 的 cancel context
	Errors          []error                       // 非致命错误（单个服务配置错误等）
}

// Bootstrap 启动管理器
// REQ-F-033, REQ-F-034: supd 启动流程
type Bootstrap struct {
	cfg    BootstrapConfig
	result *BootstrapResult
	done   chan struct{} // 启动完成信号
	stopCh chan struct{} // 启动中收到停止信号
	// B-01-RACE 修复：保护 BootstrapResult 的 Loggers/RestartEngines/CancelFuncs maps
	// 主 goroutine 在 startAutostartServices 中写入，supervisor goroutine 在
	// writeServiceLog/restart 路径中读取或写入，需要 mutex 防止数据竞态
	loggersMu        sync.RWMutex
	restartEnginesMu sync.RWMutex
	cancelFuncsMu    sync.RWMutex
}

// NewBootstrap 创建启动管理器
// REQ-F-033, REQ-F-034
func NewBootstrap(cfg BootstrapConfig) *Bootstrap {
	return &Bootstrap{
		cfg:    cfg,
		done:   make(chan struct{}),
		stopCh: make(chan struct{}),
	}
}

func logStartupConfigurationHints(baseDir string) {
	slog.Info("ALLID 自动创建/修正用户",
		"format", "name[:uid[:gid]],多个条目用逗号分隔",
		"examples", "ALLID=abd:1000:1000,claw:1001 或 ALLID=abd,claw",
		"behavior", "省略 gid 时等于 uid；仅用户名时由系统分配 uid/gid；需以 root 运行")
	slog.Info("全局环境变量 YAML 写法",
		"path", filepath.Join(baseDir, "env", "00-base.yaml"),
		"config", "config.yaml 的 env_files 需引用该文件",
		"example", `env: { ALLID: { value: "abd:1000:1000,claw:1001", enabled: true, hint: "启动前创建或修正用户" } }`,
		"note", "必须包含 env 包装层，变量必须使用 value/enabled/hint 结构，不能写成 ALLID: value")
}

// Run 执行11步启动流程
// REQ-F-033: supd 启动流程（11步）
// REQ-F-034: 零状态启动，不持久化
// 返回 BootstrapResult（即使出错也返回部分结果供调用方清理资源）
func (b *Bootstrap) Run(ctx context.Context) (*BootstrapResult, error) {
	b.result = &BootstrapResult{
		StateMachines:  make(map[string]*StateMachine),
		Loggers:        make(map[string]*logging.ServiceLogger),
		RestartEngines: make(map[string]*RestartEngine),
		CancelFuncs:    make(map[string]context.CancelFunc),
	}
	result := b.result

	// Step 0: 清理上次 supd 异常退出（如 SIGKILL）遗留的孤儿进程
	// 通过扫描 PID 文件识别仍存活的子进程并 SIGKILL，避免端口/锁文件冲突
	if reaped := ReapOrphans(b.cfg.BaseDir); len(reaped) > 0 {
		slog.Info("已清理上次 supd 异常退出遗留的孤儿进程", "services", reaped)
	}

	// Step 1: 读取 config.yaml
	// REQ-F-033: 解析失败则拒绝启动
	cfg, err := config.LoadConfig(b.cfg.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("step 1 load config: %w", err)
	}
	result.Config = cfg
	if b.cfg.HTTPListen != "" {
		cfg.Settings.HTTPListen = b.cfg.HTTPListen
	}
	if b.cfg.LogLevel != "" {
		cfg.Settings.LogLevel = b.cfg.LogLevel
	}

	// Step 2: 初始化日志系统
	// REQ-F-033: log/slog + 文件输出
	// P-03-1/O-03-1/O-03-2 修复：slog 默认 handler 同时写入 supd.log 和 stderr，并应用 log_level
	logLevel := ""
	if cfg != nil {
		logLevel = cfg.Settings.LogLevel
	}
	if err := logging.InitSupdLoggerWithLevel(b.cfg.LogDir, logLevel); err != nil {
		return nil, fmt.Errorf("step 2 init supd logger: %w", err)
	}
	logStartupConfigurationHints(b.cfg.BaseDir)

	// Step 3: 运行时注册表在 Step 4 后构建（依赖 Discovery 结果）

	// Step 4: 扫描配置
	// REQ-F-033: services/ + extensions/ + env/ + 服务级 extensions/
	disc := watch.NewDiscovery(b.cfg.BaseDir, b.cfg.LogDir)
	result.Discovery = disc.Scan()

	// 收集发现过程中的非致命错误
	for _, discErr := range result.Discovery.Errors {
		result.Errors = append(result.Errors,
			fmt.Errorf("discovery %s: %s", discErr.Path, discErr.Message))
	}

	// REQ-F-028: 构建运行时注册表（三层来源：config > scan > builtin）
	// REQ-F-029: 运行时可用性校验
	configRuntimes := cfg.Runtimes
	scanRuntimes := result.Discovery.Runtimes
	registry := config.BuildRegistry(configRuntimes, scanRuntimes)
	result.RuntimeRegistry = registry

	// Step 5: 构建依赖图 + 循环检测（含自引用检测）
	// REQ-F-033: 构建依赖图 + 循环检测
	depGraph := NewDependencyGraph()
	result.DepGraph = depGraph
	for name, svc := range result.Discovery.Services {
		depGraph.AddService(name, svc.Config.DependsOn)
	}
	if cycle := depGraph.DetectCycle(); len(cycle) > 0 {
		result.Errors = append(result.Errors,
			fmt.Errorf("dependency cycle detected: %v", cycle))
	}
	if selfRef := depGraph.DetectSelfReference(); len(selfRef) > 0 {
		result.Errors = append(result.Errors,
			fmt.Errorf("self-reference detected: %v", selfRef))
	}

	// Step 6: 启动 fsnotify 监听
	// REQ-F-033: fsnotify 初始化失败 → 拒绝启动
	watcher, err := watch.NewWatcher(b.cfg.BaseDir)
	if err != nil {
		return result, fmt.Errorf("step 6 init fsnotify watcher: %w", err)
	}
	watcher.Start()
	result.Watcher = watcher

	// Step 7: 启动 cron 调度器（加载 on_schedule 触发器）
	// 由 run.go 中启动 CronScheduler

	// Step 8: 启动 HTTP 服务器
	// 由 run.go 中启动 HTTP 服务器

	// 创建进程管理器
	result.ProcessMgr = NewProcessManager()
	// 启用 PID 文件记录：Register 时写，Unregister 时删
	// 下次 supd 启动时 ReapOrphans 通过 PID 文件识别并清理孤儿进程
	result.ProcessMgr.SetPIDFileDir(b.cfg.BaseDir)

	// 为所有已发现的服务创建状态机
	for name, svcEntry := range result.Discovery.Services {
		sm := NewStateMachine()
		sm.SetName(name)
		if b.cfg.EventPublisher != nil {
			sm.SetPublisher(b.cfg.EventPublisher)
		}
		if svcEntry.Config != nil && !isAutostart(svcEntry.Config) {
			sm.ResetTo(StateDown)
		}
		result.StateMachines[name] = sm
	}

	if err := ctx.Err(); err != nil {
		return result, err
	}

	// Step 9: 触发 supd_lifecycle:pre_start 扩展
	// REQ-D-004, 2.8.1: supd 启动第9步，在启动 autostart 服务之前
	// I-04-002 修复：移除上方重复的 Step 9 注释，步骤顺序统一为 9 → 10
	if b.cfg.OnSupdPreStart != nil {
		b.cfg.OnSupdPreStart(ctx)
	}

	// Step 10: 按 autostart + 依赖图启动服务（同层并行，跨层等待 ready）
	// REQ-F-033: 同层并行，跨层等待 ready
	if err := b.startAutostartServices(ctx); err != nil {
		return result, fmt.Errorf("step 10 start autostart services: %w", err)
	}

	// Step 11: 所有 autostart=true 的服务进入终态后触发 supd_lifecycle:post_ready
	// 由 run.go 中触发

	close(b.done)
	return result, nil
}

// startAutostartServices 按 autostart + 依赖图启动服务
// REQ-F-033: 同层并行，跨层等待 ready；服务配置错误或循环依赖→跳过
// REQ-F-034: autostart=true 按依赖图拉起，autostart=false 保持 down
func (b *Bootstrap) startAutostartServices(ctx context.Context) error {
	result := b.result

	// 构建配置错误服务集合（discovery 阶段解析失败的服务不在 Services map 中，
	// 但如果服务目录存在且解析失败，需要标记跳过）
	configErrServices := make(map[string]bool)
	for _, discErr := range result.Discovery.Errors {
		for name, svc := range result.Discovery.Services {
			if discErr.Path == svc.ConfigPath {
				configErrServices[name] = true
			}
		}
	}

	// 构建循环依赖服务集合
	cycleServices := make(map[string]bool)
	if cycle := result.DepGraph.DetectCycle(); len(cycle) > 0 {
		for _, name := range cycle {
			cycleServices[name] = true
		}
	}
	if selfRef := result.DepGraph.DetectSelfReference(); len(selfRef) > 0 {
		for _, name := range selfRef {
			cycleServices[name] = true
		}
	}

	// K-01-002 修复：检测依赖了不存在服务的服务，保持 pending 状态（规格 §2.1.2 第222行）
	// missingDepServices 集合在 autostart 筛选和 cleanGraph 构建时跳过，
	// 这些服务的状态机保持初始 pending 状态
	missingDepServices := make(map[string]bool)
	knownServices := make(map[string]bool, len(result.Discovery.Services))
	for name := range result.Discovery.Services {
		knownServices[name] = true
	}
	for svc, deps := range result.DepGraph.MissingDependencies(knownServices) {
		missingDepServices[svc] = true
		slog.Warn("service has missing dependencies, keeping pending",
			"service", svc, "missing", deps)
	}

	// 尝试拓扑排序
	layers, _ := result.DepGraph.TopologicalSort()

	// 如果存在循环依赖，TopologicalSort 返回 nil layers
	// 需要创建去除循环服务的干净图重新排序
	if layers == nil {
		cleanGraph := NewDependencyGraph()
		for name, svc := range result.Discovery.Services {
			if cycleServices[name] || configErrServices[name] || missingDepServices[name] {
				continue
			}
			// 过滤掉对循环服务的依赖
			var deps []string
			for _, dep := range svc.Config.DependsOn {
				if !cycleServices[dep] && !configErrServices[dep] {
					deps = append(deps, dep)
				}
			}
			cleanGraph.AddService(name, deps)
		}
		var newCycle []string
		layers, newCycle = cleanGraph.TopologicalSort()
		if len(newCycle) > 0 || layers == nil {
			// 仍然存在循环，无法启动任何服务
			slog.Warn("unresolvable dependency cycle, no services started")
			return nil
		}
	}

	// 按层启动服务
	for _, layer := range layers {
		// 筛选本层中 autostart=true 且无配置错误的服务
		var autostartSvcs []string
		for _, name := range layer {
			if configErrServices[name] || missingDepServices[name] {
				continue
			}
			svcEntry, ok := result.Discovery.Services[name]
			if !ok || svcEntry.Config == nil {
				continue
			}
			// REQ-F-034: autostart=false 的服务保持 down，不启动
			if !isAutostart(svcEntry.Config) {
				continue
			}
			autostartSvcs = append(autostartSvcs, name)
		}

		if len(autostartSvcs) == 0 {
			continue
		}

		// 同层并行启动
		type startResult struct {
			name       string
			err        error
			proc       *Process
			logger     *logging.ServiceLogger
			engine     *RestartEngine
			cancelFunc context.CancelFunc
		}
		resultCh := make(chan startResult, len(autostartSvcs))

		for _, name := range autostartSvcs {
			go func(n string) {
				svcEntry := result.Discovery.Services[n]
				proc, logger, engine, cancelFunc, err := b.startService(ctx, n, svcEntry)
				resultCh <- startResult{name: n, err: err, proc: proc, logger: logger, engine: engine, cancelFunc: cancelFunc}
			}(name)
		}

		// 等待本层所有服务到达终态后再启动下一层
		// REQ-F-033: 跨层等待 ready
		for i := 0; i < len(autostartSvcs); i++ {
			select {
			case sr := <-resultCh:
				// 在主 goroutine 中写入所有 map，避免 concurrent map writes
				// B-01-RACE 修复：写 map 时加锁，防止与 supervisor goroutine 读取竞态
				if sr.proc != nil {
					result.ProcessMgr.Register(sr.name, sr.proc)
				}
				if sr.logger != nil {
					b.loggersMu.Lock()
					result.Loggers[sr.name] = sr.logger
					b.loggersMu.Unlock()
				}
				if sr.engine != nil {
					b.restartEnginesMu.Lock()
					result.RestartEngines[sr.name] = sr.engine
					b.restartEnginesMu.Unlock()
				}
				if sr.cancelFunc != nil {
					b.cancelFuncsMu.Lock()
					result.CancelFuncs[sr.name] = sr.cancelFunc
					b.cancelFuncsMu.Unlock()
				}
				if sr.err != nil {
					slog.Error("failed to start service",
						"service", sr.name, "error", sr.err)
				}
			case <-ctx.Done():
				return ctx.Err()
			case <-b.stopCh:
				// REQ-F-033: 启动中收到 SIGTERM/SIGINT 时，立即停止拉起剩余服务
				return fmt.Errorf("startup interrupted by signal")
			}
		}
	}

	return nil
}

// startService 启动单个服务
// REQ-F-033: 创建 Process → Logger → StateMachine → Readiness
// 返回 (进程, 日志器, 重启引擎, cancelFunc, 错误)，进程/日志器/引擎/cancelFunc 在主 goroutine 中注册到 map
func (b *Bootstrap) startService(ctx context.Context, name string, svcEntry *watch.ServiceEntry) (*Process, *logging.ServiceLogger, *RestartEngine, context.CancelFunc, error) {
	unlock := b.cfg.LifecycleLocks.Lock(name)
	defer unlock()
	result := b.result
	svcConfig := svcEntry.Config
	sm := result.StateMachines[name]

	// 构建命令（runtime 解析）
	// REQ-F-028, REQ-F-029: runtime 别名解析
	command := svcConfig.Command
	if svcConfig.Runtime != "" {
		rt, err := config.Resolve(b.result.RuntimeRegistry, svcConfig.Runtime)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("service %s runtime %q: %w", name, svcConfig.Runtime, err)
		}
		command = append([]string{rt.AbsPath}, command...)
	}

	// 构建环境变量
	// 规格 §2.2.4: 服务进程合并 3 层 env（supd 自身 env → 全局 env 文件 → 服务 env）
	// BUG 修复: 此前仅用 os.Environ()，未加载 services/<svc>/env.yaml，违反规格 §2.2.4
	env := BuildServiceProcessEnv(b.cfg.BaseDir, name, b.result.Config.EnvFiles)

	// 构建工作目录
	workdir := svcConfig.Workdir
	if workdir == "" {
		workdir = filepath.Dir(svcEntry.ConfigPath)
	}

	// 状态转移: pending → starting
	// REQ-F-004: 规则1 pending→starting，所有 depends_on 服务进入 ready 后触发
	sm.Transition(EventDependsReady)

	// REQ-D-004: service_lifecycle:pre_start — 服务启动前触发
	if b.cfg.OnServicePreStart != nil {
		b.cfg.OnServicePreStart(ctx, name)
	}

	// A-03-002 修复：fd_notify readiness 需在 StartProcess 前创建 checker，
	// 以便通过 cmd.ExtraFiles 将管道写端传递给子进程（fd=3）
	var preChecker ReadinessChecker
	var extraFiles []*os.File
	if svcConfig.Readiness != nil && svcConfig.Readiness.Type == "fd_notify" {
		nc, cerr := NewNotifyChecker(svcConfig.Readiness)
		if cerr != nil {
			sm.Transition(EventMaxRetries)
			return nil, nil, nil, nil, fmt.Errorf("readiness fd_notify for %s: %w", name, cerr)
		}
		preChecker = nc
		extraFiles = []*os.File{nc.WriterFd()}
	}

	// 启动子进程
	// REQ-F-006: fork+exec+进程组
	// REQ-F-023, §2.2.13: 通过 StartServiceProcess 解析身份配置（user 或 uid 模式），
	// 身份解析失败或非 root 切换其他用户时返回 *ServiceError 拒绝启动
	proc, err := StartServiceProcess(name, command, env, workdir, svcConfig.ToCredentialSpec(), svcEntry.ConfigPath, extraFiles...)
	if err != nil {
		// 进程启动失败，转移至 failed
		if preChecker != nil {
			preChecker.Close()
		}
		// N-04-USER-CRED 修复：用户身份解析失败时写入服务日志便于诊断
		b.writeServiceLog(name, "error", fmt.Sprintf("start failed: %s", err))
		slog.Error("failed to start service",
			"service", name,
			"user", svcConfig.User,
			"config_path", svcEntry.ConfigPath,
			"error", err)
		sm.Transition(EventMaxRetries)
		return nil, nil, nil, nil, fmt.Errorf("start process: %w", err)
	}

	// A-03-002 修复：StartProcess 成功后关闭 supd 侧的管道写端
	// 子进程关闭写端后，supd 读端能收到 EOF
	// C-01-001 修复：CloseWriter 错误记录日志，便于诊断管道异常
	if nc, ok := preChecker.(*NotifyChecker); ok {
		if err := nc.CloseWriter(); err != nil {
			slog.Warn("close notify pipe writer failed", "service", name, "error", err)
		}
	}

	// 状态转移: starting → up
	// REQ-F-004: 规则2 starting→up，进程启动后立即进入
	sm.Transition(EventProcessStarted)

	// 创建并启动服务日志器
	// REQ-F-010: per-service logger goroutine
	// N-G-01 修复：传入 logging.max_size_mb / max_files 配置，使日志轮转生效
	var svcLogger *logging.ServiceLogger
	logBaseDir := filepath.Join(b.cfg.LogDir, "services")
	maxSizeMB, maxFiles := 0, 0
	if cfg := svcEntry.Config.Logging; cfg != nil {
		maxSizeMB, maxFiles = cfg.MaxSizeMB, cfg.MaxFiles
	}
	svcLogger, err = logging.NewServiceLogger(name, logBaseDir, maxSizeMB, maxFiles)
	if err != nil {
		slog.Error("create service logger failed",
			"service", name, "error", err)
	} else {
		svcLogger.Start(proc.StdoutPipe(), proc.StderrPipe())
	}

	// REQ-F-008: 创建重启策略引擎
	// 引擎返回给主 goroutine 注册到 map，避免并发写 map
	engine := BuildRestartEngine(b.result.Config, svcConfig)
	if svcConfig.Readiness == nil {
		engine.RecordStart()
	}

	// 启动 supervisor goroutine：等待进程退出+自动重启
	// REQ-F-006: 每个服务一个专属 Wait goroutine（避免僵尸进程）
	// REQ-F-008: 进程意外退出时按 restart policy 自动重启
	// 使用 cancel context 支持停止退避等待中的服务
	svcCtx, svcCancel := context.WithCancel(context.Background())
	go b.superviseService(svcCtx, name, svcEntry, sm, proc, engine)

	// readiness 检查
	if svcConfig.Readiness != nil {
		err := b.checkReadiness(ctx, name, svcConfig.Readiness, sm, proc, engine, preChecker, workdir, env)
		return proc, svcLogger, engine, svcCancel, err
	}

	// 无 readiness 配置，服务在 up 状态为终态
	return proc, svcLogger, engine, svcCancel, nil
}

// checkReadiness 执行 readiness 检查，等待服务就绪或超时
// REQ-F-009: 4种readiness检查类型
// REQ-F-033: readiness 通过则 Transition(StateReady)
// A-03-002 修复：preChecker 用于 fd_notify 类型，需在 StartProcess 前创建以传递写端 fd
// workdir 为服务目录，传递给 script 类型 checker 以解析相对路径
// env 为服务进程环境变量，传递给 script 类型 checker 以继承服务 env（规格 §2.2.3）
func (b *Bootstrap) checkReadiness(
	ctx context.Context,
	name string,
	readinessCfg *config.ReadinessConfig,
	sm *StateMachine,
	proc *Process,
	engine *RestartEngine,
	preChecker ReadinessChecker,
	workdir string,
	env []string,
) error {
	var checker ReadinessChecker
	if preChecker != nil {
		// A-03-002 修复：fd_notify 的 checker 已在 StartProcess 前创建
		checker = preChecker
	} else {
		var err error
		checker, err = NewReadinessChecker(readinessCfg, workdir, env)
		if err != nil {
			sm.Transition(EventReadinessTimeout)
			return fmt.Errorf("readiness checker for %s: %w", name, err)
		}
	}
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
			// REQ-F-004: 规则3 up→ready，readiness检查通过
			sm.Transition(EventReadinessPassed)
			engine.RecordStart()
			if b.cfg.OnServicePostReady != nil {
				b.cfg.OnServicePostReady(ctx, name, proc.PID())
			}
			return nil
		}
		select {
		case <-ticker.C:
			continue
		case <-proc.Done():
			// 进程在 readiness 检查期间退出
			// 不做状态转移，由 supervisor goroutine 处理重启逻辑
			return fmt.Errorf("process exited during readiness check for service %s", name)
		case <-checkCtx.Done():
			// readiness 检查超时
			// REQ-F-004: 规则6 up→failed，readiness检查超时
			// C-04-001 修复：超时后必须终止进程，避免进程继续运行+supervisor goroutine 泄漏
			sm.Transition(EventReadinessTimeout)
			// C-04-001: 记录 KillProcessGroup 错误，便于运维诊断僵尸进程
			if err := proc.KillProcessGroup(); err != nil {
				slog.Warn("KillProcessGroup failed after readiness timeout", "service", name, "error", err)
			}
			return fmt.Errorf("readiness check timeout for service %s", name)
		case <-ctx.Done():
			return ctx.Err()
		case <-b.stopCh:
			return fmt.Errorf("startup interrupted by signal")
		}
	}
}

// StopStartup 启动中收到信号时调用，停止拉起剩余服务
// REQ-F-033: 启动中收到 SIGTERM/SIGINT 时，立即停止拉起剩余服务
func (b *Bootstrap) StopStartup() {
	select {
	case <-b.stopCh:
		// 已经停止
	default:
		close(b.stopCh)
	}
}

// Cleanup 清理 Bootstrap 已分配的资源（启动失败时由调用方调用）。
//
// 背景：Bootstrap.Run 在 startAutostartServices 阶段可能已拉起部分服务，
// 每个 autostart 服务对应一个 supervisor goroutine（阻塞在 proc.Wait 或退避等待），
// 以及一个运行中的子进程。若直接返回而不清理，会导致：
//   - supervisor goroutine 泄漏：进程未退出则 proc.Wait 永久阻塞；退避中的 goroutine
//     也因无进程退出信号而无法收尾。
//   - 孤儿进程：子进程脱离 supd 管理继续运行，占用端口/PID 文件/锁文件。
//
// 清理顺序（关键）：
//  1. 先取消所有 supervisor 的 context（cancelFuncsMu 保护）→ 中断退避等待，
//     使后续 doBackoff 立即返回 false，goroutine 不再尝试重启。
//  2. 再杀死所有已注册进程组 → 使 supervisor 的 proc.Wait() 返回，
//     goroutine 走完退出路径后因 ctx 已取消而退出（doBackoff 返回 false）。
//     Unregister 同时删除 PID 文件，避免下次启动时 ReapOrphans 误杀。
//  3. 停止 watcher（关闭 fsnotify + debouncer，回收 forwardEvents goroutine）。
//  4. 关闭所有服务日志器（loggersMu 保护，防止与 supervisor 退出路径中
//     writeServiceLog 并发读写 Loggers map）。
//
// 说明：writeServiceLog 取得 logger 引用后释放读锁再 WriteLine，存在与 Close
// 并发的窄窗口；WriteLine 向已关闭的 RotatingLogWriter 写入仅静默丢弃（bufio 缓冲，
// 不 panic），对失败清理路径可接受。
func (b *Bootstrap) Cleanup() {
	result := b.result
	if result == nil {
		return
	}

	// 1. 取消所有 supervisor goroutine 的 context，中断退避等待。
	b.cancelFuncsMu.Lock()
	for _, cancel := range result.CancelFuncs {
		if cancel != nil {
			cancel()
		}
	}
	b.cancelFuncsMu.Unlock()

	// 2. 杀死所有已注册进程组，使 supervisor 的 proc.Wait() 返回并走退出路径。
	//    ctx 已取消 → doBackoff 返回 false → goroutine 不重启、直接退出。
	if result.ProcessMgr != nil {
		for _, name := range result.ProcessMgr.List() {
			if err := result.ProcessMgr.KillProcessGroup(name); err != nil {
				slog.Warn("cleanup: kill process group failed", "service", name, "error", err)
			}
			// Unregister 同时删除 PID 文件，避免下次启动 ReapOrphans 误杀已退出的服务。
			result.ProcessMgr.Unregister(name)
		}
	}

	// 3. 停止 watcher（关闭 fsnotify + debouncer）。
	if result.Watcher != nil {
		result.Watcher.Stop()
	}

	// 4. 关闭所有服务日志器（加写锁，防止与 supervisor 退出路径的 writeServiceLog
	//    并发读写 Loggers map）。
	b.loggersMu.Lock()
	for _, logger := range result.Loggers {
		if logger != nil {
			_ = logger.Close()
		}
	}
	b.loggersMu.Unlock()
}

// isAutostart 判断服务是否自动启动
// REQ-F-034: autostart 为 nil 或 true → true，false → false
func isAutostart(svc *config.ServiceConfig) bool {
	return svc.Autostart == nil || *svc.Autostart
}

// BuildRestartEngine 根据服务配置和全局默认值构建重启策略引擎
// REQ-F-008: 服务级 restart 配置覆盖全局默认
// 导出以供 API 路径（CoreServiceOperator）复用
func BuildRestartEngine(cfg *config.Config, svcConfig *config.ServiceConfig) *RestartEngine {
	// 全局默认值
	var policy RestartPolicy = RestartAlways
	backoffMs := 1000
	maxBackoffMs := 30000
	multiplier := 2
	maxRetries := 0 // 0 = 不限制
	resetAfterSeconds := 300

	// 应用全局默认值
	if cfg != nil {
		d := cfg.Defaults.Restart
		if d.Policy != "" {
			policy = RestartPolicy(d.Policy)
		}
		if d.BackoffMs != 0 {
			backoffMs = d.BackoffMs
		}
		if d.MaxBackoffMs != 0 {
			maxBackoffMs = d.MaxBackoffMs
		}
		if d.Multiplier != 0 {
			multiplier = d.Multiplier
		}
		if d.MaxRetries != 0 {
			maxRetries = d.MaxRetries
		}
		if d.ResetAfterSeconds != 0 {
			resetAfterSeconds = d.ResetAfterSeconds
		}
	}

	// 服务级配置覆盖全局默认
	if svcConfig.Restart != nil {
		r := svcConfig.Restart
		if r.Policy != "" {
			policy = RestartPolicy(r.Policy)
		}
		if r.BackoffMs != 0 {
			backoffMs = r.BackoffMs
		}
		if r.MaxBackoffMs != 0 {
			maxBackoffMs = r.MaxBackoffMs
		}
		if r.Multiplier != 0 {
			multiplier = r.Multiplier
		}
		if r.MaxRetries != 0 {
			maxRetries = r.MaxRetries
		}
		if r.ResetAfterSeconds != 0 {
			resetAfterSeconds = r.ResetAfterSeconds
		}
	}

	return NewRestartEngine(policy, backoffMs, maxBackoffMs, multiplier, maxRetries, resetAfterSeconds)
}

// writeServiceLog 向服务日志文件写入自定义消息
func (b *Bootstrap) writeServiceLog(name string, level, message string) {
	// B-01-RACE 修复：读 Loggers map 时加读锁，防止与 startAutostartServices 写入竞态
	b.loggersMu.RLock()
	logger, ok := b.result.Loggers[name]
	b.loggersMu.RUnlock()
	if ok {
		logger.WriteLine(level, message)
		return
	}
	// 没有 ServiceLogger 实例，直接写入日志文件
	// N-G-01 修复：使用默认轮转参数（writeServiceLog 无 svcConfig 上下文）
	logBaseDir := filepath.Join(b.cfg.LogDir, "services")
	logger, err := logging.NewServiceLogger(name, logBaseDir, 0, 0)
	if err != nil {
		slog.Error("create service logger for error message failed", "service", name, "error", err)
		return
	}
	logger.WriteLine(level, message)
	logger.Close()
}

// superviseService 监督单个服务的进程，处理退出后的自动重启
// REQ-F-006: 每个服务一个专属 Wait goroutine（避免僵尸进程）
// REQ-F-008: 进程意外退出时按 restart policy 自动重启
// M-04-001/TD-003 修复：抽取共享 RunSupervisor，此处为薄包装
func (b *Bootstrap) superviseService(ctx context.Context, name string, svcEntry *watch.ServiceEntry, sm *StateMachine, proc *Process, engine *RestartEngine) {
	callbacks := SupervisorCallbacks{
		AcquireLifecycleLock: b.cfg.LifecycleLocks.Lock,
		WriteServiceLog:      b.writeServiceLog,
		PublishEvent:         b.cfg.EventPublisher,
		OnFailure: func(fCtx context.Context, n string, exitCode, signal, restartCount, pid int) {
			if b.cfg.OnServiceFailure != nil {
				// 原行为：用 context.Background()，OnFailure 不受 supervisor ctx 影响
				b.cfg.OnServiceFailure(context.Background(), n, exitCode, signal, restartCount, pid)
			}
		},
		RuntimeRegistry: b.result.RuntimeRegistry,
		BuildEnv: func(n string) []string {
			return BuildServiceProcessEnv(b.cfg.BaseDir, n, b.result.Config.EnvFiles)
		},
		RegisterProcess: func(n string, p *Process) {
			b.result.ProcessMgr.Register(n, p)
		},
		RebuildLogger:        b.rebuildLogger,
		RecordRestartHistory: nil, // bootstrap 不记录历史
		SpawnNextSupervisor: func(n string, entry *watch.ServiceEntry, s *StateMachine,
			p *Process, e *RestartEngine) context.Context {
			newCtx, newCancel := context.WithCancel(context.Background())
			b.cancelFuncsMu.Lock()
			b.result.CancelFuncs[n] = newCancel
			b.cancelFuncsMu.Unlock()
			go b.superviseService(newCtx, n, entry, s, p, e)
			// 返回旧 ctx：bootstrap 的 readiness 用 supervisor 自己的 ctx（与原行为一致）
			return ctx
		},
		RunReadiness: func(rCtx context.Context, n string, entry *watch.ServiceEntry,
			s *StateMachine, p *Process, pre ReadinessChecker) error {
			workdir := entry.Config.Workdir
			if workdir == "" {
				workdir = filepath.Dir(entry.ConfigPath)
			}
			env := BuildServiceProcessEnv(b.cfg.BaseDir, n, b.result.Config.EnvFiles)
			// 返回 error，供 RunSupervisor 区分进程退出 vs 超时
			return b.checkReadiness(rCtx, n, entry.Config.Readiness, s, p, engine, pre, workdir, env)
		},
		Source: "cli",
	}

	RunSupervisor(ctx, name, svcEntry, sm, proc, engine, callbacks)
}

// rebuildLogger 关闭旧日志器，创建并启动新日志器
// M-04-001 修复：从 superviseService 中提取为独立方法
func (b *Bootstrap) rebuildLogger(name string, newProc *Process, maxSizeMB, maxFiles int) {
	logBaseDir := filepath.Join(b.cfg.LogDir, "services")
	b.loggersMu.Lock()
	defer b.loggersMu.Unlock()
	if oldLogger, ok := b.result.Loggers[name]; ok && oldLogger != nil {
		if closeErr := oldLogger.Close(); closeErr != nil {
			slog.Warn("close old service logger failed on restart",
				"service", name, "error", closeErr)
		}
	}
	newLogger, loggerErr := logging.NewServiceLogger(name, logBaseDir, maxSizeMB, maxFiles)
	if loggerErr != nil {
		slog.Error("create service logger failed on restart",
			"service", name, "error", loggerErr)
	} else {
		newLogger.Start(newProc.StdoutPipe(), newProc.StderrPipe())
		b.result.Loggers[name] = newLogger
	}
}
