package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/supdorg/supd/internal/api"
	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/core"
	"github.com/supdorg/supd/internal/extension"
	"github.com/supdorg/supd/internal/logging"
	"github.com/supdorg/supd/internal/system"
	"github.com/supdorg/supd/internal/watch"
)

// REQ-F-039: run 命令 — 启动监督器
var (
	runListen   string
	runLogLevel string
	runNoPID1   bool
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "启动 supd 监督器",
	Long:  "启动 supd 进程监督器。若工作目录不存在，自动调用 init 流程。",
	Example: `  # 启动监督器（使用默认工作目录与默认监听 :7979）
  supd run

  # 指定工作目录与监听地址
  supd --workdir /path/to/workdir run --listen :9090

  # 指定日志级别（debug/info/warn/error）
  supd run --log-level debug

  # 作为 systemd 服务运行时禁用 PID1 模式（Docker 容器中不要使用）
  supd run --no-pid1`,
	RunE: runRun,
}

func init() {
	runCmd.Flags().StringVar(&runListen, "listen", "", "指定监听地址 (如 :7979)")
	runCmd.Flags().StringVar(&runLogLevel, "log-level", "", "日志级别 (debug/info/warn/error)")
	runCmd.Flags().BoolVar(&runNoPID1, "no-pid1", false,
		"不启用 PID1 模式（仅适用于 systemd 服务场景，Docker 容器中禁用）")
}

// runRun 执行 run 命令
// REQ-F-039: 启动监督器，若工作目录不存在自动调用 init 流程
func runRun(cmd *cobra.Command, args []string) error {
	dir := getWorkDir()
	cfgPath := getConfigPath()

	// 若工作目录或配置文件不存在，自动调用 init 流程
	// REQ-F-039: 若工作目录不存在，自动调用init流程
	// Docker 场景：/etc/supd 目录可能已由镜像创建（含 runtimes/），但 config.yaml 缺失
	_, dirErr := os.Stat(dir)
	_, cfgErr := os.Stat(cfgPath)
	if os.IsNotExist(dirErr) || os.IsNotExist(cfgErr) {
		if os.IsNotExist(dirErr) {
			infof("工作目录 %s 不存在，自动初始化...", dir)
		} else {
			infof("配置文件 %s 不存在，自动初始化...", cfgPath)
		}
		if err := runInit(cmd, args); err != nil {
			return fmt.Errorf("自动初始化失败: %w", err)
		}
	}

	// 前台运行
	infof("supd 启动中...")
	verbosef("工作目录: %s", dir)
	verbosef("配置文件: %s", cfgPath)

	// Step 1: 加载配置
	// REQ-F-033: 解析失败则拒绝启动
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}
	if runListen != "" {
		cfg.Settings.HTTPListen = runListen
		verbosef("监听地址: %s", runListen)
	}
	if runLogLevel != "" {
		cfg.Settings.LogLevel = runLogLevel
		verbosef("日志级别: %s", runLogLevel)
	}

	// Step 2: PID1 模式
	// REQ-F-012: 作为 PID1 运行时设置 CHILD_SUBREAPER
	if err := core.SetupPID1IfNeeded(runNoPID1); err != nil {
		slog.Warn("PID1 模式设置失败", "error", err)
	}
	// P1-007: 显式记录 PID 1 模式状态，便于 Docker 场景调试
	if core.IsPID1() && !runNoPID1 {
		slog.Info("以 PID 1 模式运行",
			"subreaper", "enabled",
			"zombie_reaper", "enabled",
			"note", "Docker 容器场景")
	} else if runNoPID1 {
		slog.Info("PID 1 模式已禁用（--no-pid1）",
			"note", "适用于 systemd 场景，Docker 容器中不建议使用")
		verbosef("PID1 模式: 已禁用")
	} else {
		slog.Info("以非 PID 1 模式运行",
			"note", "由父进程（如 systemd）管理生命周期")
	}

	// Step 3: 创建 context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Step 4: 运行 Bootstrap（11步启动流程）
	// REQ-F-033, REQ-F-034: supd 启动流程
	logDir := os.Getenv("SUPD_LOG_DIR")
	if logDir == "" {
		logDir = "/var/log/supd"
	}
	// K-04-001 修复：启动期校验 log_dir 存在性/可写性，避免运行时才发现权限问题
	if err := validateLogDir(logDir); err != nil {
		return fmt.Errorf("log_dir %q 校验失败: %w", logDir, err)
	}

	// REQ-2.9.7: 事件环形缓冲200条（在Bootstrap之前创建，以便注入EventPublisher）
	eventRing := api.NewEventRingBuffer(api.DefaultEventRingCapacity)

	// REQ-D-004: 在 Bootstrap 之前创建 Executor/Dispatcher/LifecycleTriggers
	// 以便在服务启动时触发 service_lifecycle 扩展
	hardLimit := cfg.Settings.ExtensionHardLimitSeconds
	if hardLimit <= 0 {
		hardLimit = config.ExtensionHardLimitSeconds // O-05-001 修复：使用常量替代字面量 1800
	}
	executor := extension.NewExecutor(logDir, dir)
	dispatcher := extension.NewDispatcher(executor, dir, logDir, hardLimit)
	// P-03-001 修复：给 dispatcher 注入事件发布器，发布扩展执行相关事件
	dispatcher.SetEventPublisher(eventRing)

	// 预扫描 Discovery，供 LifecycleTrigger 在 Bootstrap 期间使用
	preDiscovery := watch.NewDiscovery(dir, logDir).Scan()
	serviceLifecycleTrigger := extension.NewServiceLifecycleTrigger(dispatcher, preDiscovery)
	supdLifecycleTrigger := extension.NewSupdLifecycleTrigger(dispatcher, preDiscovery)

	// 提前创建 TaskManager 并注入给两个 LifecycleTrigger
	// 关键：必须在 Bootstrap.Run 之前注入，因为 Bootstrap 启动 autostart 服务时会触发
	// service_lifecycle:pre_start/post_ready 和 supd_lifecycle:pre_start/post_ready 回调，
	// 此时需要 taskMgr 已就绪才能记录执行历史
	// 与 CronScheduler.SetTaskManager 保持一致的模式（参考 cron_scheduler.go）
	taskMgr := extension.NewTaskManager(extension.DefaultTaskRetentionDays) // REQ-F-020: 7天保留
	// N-03-001 修复：关联 ConcurrencyManager，使 CancelRun 能实际终止任务
	taskMgr.SetConcurrencyManager(dispatcher.GetConcurrencyManager())
	serviceLifecycleTrigger.SetTaskManager(taskMgr)
	supdLifecycleTrigger.SetTaskManager(taskMgr)

	bsCfg := core.BootstrapConfig{
		ConfigPath:     cfgPath,
		BaseDir:        dir,
		LogDir:         logDir,
		NoPID1:         runNoPID1,
		HTTPListen:     runListen,
		LogLevel:       runLogLevel,
		EventPublisher: eventRing,    // BUG-02: 将 EventRingBuffer 注入 Bootstrap
		Runtimes:       cfg.Runtimes, // REQ-F-028: 运行时配置来源
		// REQ-D-004: 生命周期回调
		OnServicePreStart: func(ctx context.Context, serviceName string) {
			serviceLifecycleTrigger.OnPreStart(ctx, serviceName)
		},
		OnServicePostReady: func(ctx context.Context, serviceName string, servicePID int) {
			serviceLifecycleTrigger.OnPostReady(ctx, serviceName, servicePID)
		},
		OnServiceFailure: func(ctx context.Context, serviceName string, exitCode, signal, restartCount, servicePID int) {
			serviceLifecycleTrigger.OnFailure(ctx, serviceName, exitCode, signal, restartCount, servicePID)
		},
		OnSupdPreStart: func(ctx context.Context) {
			supdLifecycleTrigger.OnPreStart(ctx)
		},
	}
	bootstrap := core.NewBootstrap(bsCfg)
	result, err := bootstrap.Run(ctx)
	if err != nil {
		// Bootstrap 失败，但 result 可能包含部分已启动的资源，需要清理
		if result != nil {
			cleanupResources(ctx, result)
		}
		return fmt.Errorf("启动失败: %w", err)
	}

	// 记录非致命错误
	for _, e := range result.Errors {
		slog.Warn("启动警告", "error", e)
	}

	// REQ-D-004: Bootstrap 完成后，用最终 Discovery 更新触发器
	serviceLifecycleTrigger.SetDiscovery(result.Discovery)
	supdLifecycleTrigger.SetDiscovery(result.Discovery)

	// REQ-D-004, 2.8.1 Step 11: 所有 autostart=true 的服务进入终态后触发 supd_lifecycle:post_ready
	supdLifecycleTrigger.OnPostReady(ctx)

	// Step 5: 创建并启动 HTTP API 服务器
	// REQ-F-033 Step 8: 启动 HTTP 服务器
	apiServer := api.NewServer(cfg)

	// REQ-F-028: 设置 runtime 别名解析所需的三层来源
	executor.SetRuntimes(cfg.Runtimes, result.Discovery.Runtimes)
	cronScheduler := extension.NewCronScheduler(dispatcher)
	// REQ-D-004: 创建 CronTrigger 并注入 CronScheduler，用于 retry_on_failure 处理
	cronTrigger := extension.NewCronTrigger(cronScheduler)
	cronScheduler.SetCronTrigger(cronTrigger)

	// 将 on_schedule 扩展注册到 CronScheduler
	// REQ-D-004: type: on_schedule — 定时触发
	registerCronJobs(cronScheduler, result.Discovery)

	svcOperator := injectProviders(apiServer, result, cfg, dir, logDir, eventRing, executor, cronScheduler, serviceLifecycleTrigger, supdLifecycleTrigger, dispatcher, taskMgr, ctx)
	// REQ-F-002: 前端静态文件嵌入（非dev模式）
	webFS := getWebFS()
	if webFS != nil {
		apiServer.SetupStaticFiles(webFS)
	}
	// 在单独的 goroutine 中启动 HTTP 服务器
	go func() {
		if err := apiServer.Start(); err != nil {
			slog.Error("HTTP 服务器异常退出", "error", err)
			cancel()
		}
	}()

	// REQ-D-004: 启动 cron 调度器
	cronScheduler.Start()

	// P-03-001 修复：启动磁盘空间监控 goroutine，磁盘满时发布 system_resource_warning 事件
	// 规格§2.9.7: 14种事件类型包含 system_resource_warning
	go startDiskSpaceMonitor(ctx, logDir, eventRing)

	// Step 6: 信号处理
	// REQ-F-012: PID1 模式下的信号注册
	// REQ-F-013: SIGHUP → 热重载，SIGTERM/SIGINT → 优雅退出
	sigHandler := system.NewSignalHandler()
	sigHandler.Start()
	defer sigHandler.Stop()

	// REQ-F-012: PID1 模式下启动僵尸进程回收（含 10s 周期 poll 兜底）
	// P1-006: 使用 ZombieReaper 结构体，支持 Stop 方法，与 SignalHandler 设计一致
	if core.IsPID1() && !runNoPID1 {
		reaper := system.NewZombieReaper()
		reaper.Start()
		defer reaper.Stop()
	}

	// 构建 ShutdownCoordinator
	stopConfigs := buildStopConfigs(result)
	graceSeconds := cfg.Settings.ShutdownGraceSeconds
	shutdownCoord := core.NewShutdownCoordinator(
		result.ProcessMgr,
		result.DepGraph,
		result.StateMachines,
		stopConfigs,
		graceSeconds,
	)
	// REQ-D-004: 注入 supd_lifecycle:pre_shutdown 和 service_lifecycle:pre_stop 回调
	slt := serviceLifecycleTrigger
	sltDeref := slt
	shutdownCoord.SetPreShutdownHook(func(ctx context.Context) error {
		supdLifecycleTrigger.OnPreShutdown(ctx)
		return nil
	})
	shutdownCoord.SetPreStopHook(func(serviceName string, servicePID int) func() error {
		trigger := sltDeref
		return func() error {
			trigger.OnPreStop(context.Background(), serviceName, servicePID)
			return nil
		}
	})

	// Watcher 事件处理 goroutine（SIGHUP 热重载）
	// N-04-01/N-04-02: 传入 lifecycle triggers、cron scheduler、event ring 以便热重载后同步更新
	// N-04-001: 传入 apiServer 以便热重载后更新 providers 的 Discovery 引用
	reloadMgr := watch.NewReloadManager(watch.NewDiscovery(dir, logDir))
	go handleWatcherEvents(result.Watcher, reloadMgr, result, dir, logDir,
		serviceLifecycleTrigger, supdLifecycleTrigger, cronScheduler, eventRing, apiServer, dispatcher, svcOperator)

	infof("supd 运行中 (按 Ctrl+C 停止)")

	// Step 7: 等待信号
	for {
		select {
		case <-sigHandler.WaitShutdown():
			infof("收到退出信号，开始优雅退出...")
			goto shutdown
		case <-sigHandler.WaitReload():
			// REQ-F-013: SIGHUP 触发配置热重载
			// N-04-01/N-04-02: 通过 applyReload 统一处理，同步更新 triggers/cron 并发送事件
			// N-04-001: 同时更新 API Server providers 的 Discovery 引用
			applyReload(result, dir, logDir, reloadMgr,
				serviceLifecycleTrigger, supdLifecycleTrigger,
				cronScheduler, eventRing, "sighup", apiServer, dispatcher, svcOperator)
		case <-ctx.Done():
			infof("HTTP 服务器异常，开始退出...")
			goto shutdown
		}
	}

shutdown:

	// Step 8: 优雅退出
	// REQ-F-032: 6步退出流程
	// REQ-F-047: shutdown_grace_seconds 控制关机流程上限
	// P1-002 修复：将整个关机流程纳入单一 shutdown_grace_seconds 预算
	// 规格 §2.8.1: 关机流程总时长 = 服务停止总时长 + 扩展任务等待 30 秒，受此上限约束
	shutdownGraceSeconds := cfg.Settings.ShutdownGraceSeconds
	if shutdownGraceSeconds <= 0 {
		shutdownGraceSeconds = 30
	}
	graceCtx, graceCancel := context.WithTimeout(context.Background(),
		time.Duration(shutdownGraceSeconds)*time.Second)
	defer graceCancel()

	// REQ-D-004: 停止 cron 调度器（受 graceCtx 约束，规格 §2.8.1 单一预算贯穿）
	cronScheduler.Stop(graceCtx)

	// B-04-002 修复：关机流程等待运行中的扩展任务结束（带超时），避免孤儿进程
	// 规格 §2.2.9: supd 退出后所有运行任务清空；这里尽量等待扩展自行退出，超时则强制终止
	// 规格 §2.8.1: 扩展任务等待 30 秒（A-07-001 修复：原 10s 与规格不一致，已对齐为 30s）
	// P1-002: 扩展等待时长受 graceCtx 剩余时间约束，不超过 30s
	if dispatcher.GetConcurrencyManager().HasAnyRunning() {
		infof("等待运行中的扩展任务结束...")
		extTimeout := 30 * time.Second
		if deadline, ok := graceCtx.Deadline(); ok {
			if remaining := time.Until(deadline); remaining < extTimeout {
				extTimeout = remaining
			}
		}
		if still := dispatcher.GetConcurrencyManager().WaitForAllRunning(extTimeout); still > 0 {
			slog.Warn("关机时仍有扩展任务未结束，可能产生孤儿进程", "count", still)
		}
	}

	// GracefulShutdown 复用 graceCtx（不再创建独立 deadline，受全局预算约束）
	if err := shutdownCoord.GracefulShutdown(graceCtx); err != nil {
		slog.Error("优雅退出出错", "error", err)
	}

	// P1-001 修复：HTTP Shutdown 必须有 deadline，防止无界等待
	// 从 graceCtx 派生 5s 超时（如剩余时间不足 5s，使用剩余时间）
	httpStopCtx, httpStopCancel := context.WithTimeout(graceCtx, 5*time.Second)
	defer httpStopCancel()
	if err := apiServer.Stop(httpStopCtx); err != nil {
		slog.Error("关闭 HTTP 服务器出错", "error", err)
	}

	// 停止 watcher
	if result.Watcher != nil {
		result.Watcher.Stop()
	}

	infof("supd 已退出")
	return nil
}

// injectProviders 将 BootstrapResult 中的组件注入到 API Server
// REQ-F-033 Step 8: HTTP 服务器依赖注入
// taskMgr 在调用本函数前已创建并注入给 LifecycleTrigger（确保 Bootstrap 阶段触发的回调能记录历史）
func injectProviders(server *api.Server, result *core.BootstrapResult, cfg *config.Config, baseDir string, logDir string, eventRing *api.EventRingBuffer, executor *extension.Executor, cronScheduler *extension.CronScheduler, serviceLifecycleTrigger *extension.ServiceLifecycleTrigger, supdLifecycleTrigger *extension.SupdLifecycleTrigger, dispatcher *extension.Dispatcher, taskMgr *extension.TaskManager, appCtx context.Context) *api.CoreServiceOperator {
	startTime := time.Now()

	pathValidator := api.NewPathValidator(baseDir)
	// 将日志目录加入白名单（只读），允许文件浏览器查看日志
	if logDir != "" {
		pathValidator.AddAllowedPath(logDir)
	}

	// 将 TaskManager 注入 CronScheduler，使 cron 触发的执行能记录到历史
	// LifecycleTrigger 的注入已在 Bootstrap 之前完成（见上方 runRun 函数）
	cronScheduler.SetTaskManager(taskMgr)

	// 创建服务历史记录存储
	historyStore := api.NewServiceHistoryStore()

	// 创建 CoreServiceOperator，持有引用以设置 cancelFuncs
	svcOperator := &api.CoreServiceOperator{
		ProcessMgr:              result.ProcessMgr,
		StateMachines:           result.StateMachines,
		Discovery:               result.Discovery,
		Config:                  cfg,
		BaseDir:                 baseDir,
		LogDir:                  logDir,
		ServiceLifecycleTrigger: serviceLifecycleTrigger, // REQ-D-004: 注入 service_lifecycle 触发器
		HistoryStore:            historyStore,
		EventPublisher:          eventRing,             // 修复：API 启动的服务需要发布 service_died/exited 事件
		RestartEngines:          result.RestartEngines, // 修复：共享重启引擎 map
	}
	// 从 Bootstrap 传递 cancel context（用于停止退避等待中的服务）
	svcOperator.SetCancelFuncs(result.CancelFuncs)

	server.SetProviders(
		&api.CoreStateProvider{
			StateMachines:  result.StateMachines,
			ProcessMgr:     result.ProcessMgr,
			Discovery:      result.Discovery,
			Config:         cfg,
			StartTime:      startTime,
			RestartEngines: result.RestartEngines, // D-05-001 修复：传入重启引擎以获取实际重启次数
		},
		svcOperator,
		&api.CoreExtensionProvider{
			Discovery:  result.Discovery,
			Executor:   executor,
			Dispatcher: dispatcher, // A-03-001 修复：on_demand 走 Dispatcher 路径
			TaskMgr:    taskMgr,
			BaseDir:    baseDir,
			AppCtx:     appCtx, // B-04-001 修复：注入 app context 供 on_demand 异步执行
		},
		&api.CoreTaskProvider{
			TaskMgr: taskMgr,
			LogDir:  logDir,
		},
		&api.CoreCronProvider{
			CronScheduler: cronScheduler, // REQ-D-004: CronScheduler 已接入
			Discovery:     result.Discovery,
			TaskMgr:       taskMgr,
		},
		&api.ConfigSettingsProvider{
			Config:  cfg,
			BaseDir: baseDir,
		},
		&api.ConfigRuntimeProvider{
			Config:  cfg,
			BaseDir: baseDir,
		},
		&api.CoreSystemProvider{
			Config:    cfg,
			StartTime: startTime,
			BaseDir:   baseDir,
			Version:   Version,
		},
		&api.OsFileProvider{
			BaseDir:       baseDir,
			PathValidator: pathValidator,
			HistoryDir:    filepath.Join(baseDir, "history"),
			LogDir:        logDir,
			MaxVersions:   cfg.Settings.FileHistoryVersions,
		},
		&api.ConfigAuthProvider{
			AuthToken: cfg.Settings.AuthToken,
		},
		&api.FileLogProvider{
			LogDir: logDir,
		},
		&api.CoreWatchProvider{
			Watcher:   result.Watcher,
			Discovery: result.Discovery,
			BaseDir:   baseDir,
			LogDir:    logDir,
		},
		&api.CoreHistoryGetter{
			ProcessMgr:    result.ProcessMgr,
			StateMachines: result.StateMachines,
			Store:         historyStore,
		},
		eventRing,
		pathValidator,
	)
	return svcOperator
}

// buildStopConfigs 从 BootstrapResult 构建服务停止配置映射
func buildStopConfigs(result *core.BootstrapResult) map[string]core.StopConfig {
	stopConfigs := make(map[string]core.StopConfig)
	for name, svc := range result.Discovery.Services {
		if svc.Config == nil || svc.Config.Stop == nil {
			stopConfigs[name] = core.DefaultStopConfig()
			continue
		}
		grace := svc.Config.Stop.GraceSeconds
		timeout := svc.Config.Stop.TimeoutSeconds
		if grace <= 0 {
			grace = 10 // REQ-F-007: 默认10秒
		}
		if timeout <= 0 {
			timeout = 60 // REQ-F-007: 默认60秒
		}
		stopConfigs[name] = core.StopConfig{
			GraceSeconds:   grace,
			TimeoutSeconds: timeout,
		}
	}
	return stopConfigs
}

// cleanupResources 清理 Bootstrap 已分配的资源（启动失败时调用）
func cleanupResources(ctx context.Context, result *core.BootstrapResult) {
	if result.Watcher != nil {
		result.Watcher.Stop()
	}
	// 关闭所有服务日志器
	for name, logger := range result.Loggers {
		if logger != nil {
			logger.Close()
		}
		_ = name
	}
}

// handleWatcherEvents 处理 watcher 事件（配置热重载）
// REQ-F-013: SIGHUP 触发配置热重载
// REQ-F-027: 配置热重载管理
// N-04-01 修复：热重载后同步更新 lifecycle triggers 和 cron scheduler
// N-04-02 修复：发送 config_reloaded 事件
// N-04-001 修复：同时更新 API Server 的 providers Discovery 引用
func handleWatcherEvents(
	watcher *watch.Watcher,
	reloadMgr *watch.ReloadManager,
	result *core.BootstrapResult,
	baseDir, logDir string,
	serviceLifecycleTrigger *extension.ServiceLifecycleTrigger,
	supdLifecycleTrigger *extension.SupdLifecycleTrigger,
	cronScheduler *extension.CronScheduler,
	eventRing *api.EventRingBuffer,
	apiServer *api.Server,
	dispatcher *extension.Dispatcher,
	svcOperator *api.CoreServiceOperator,
) {
	if watcher == nil {
		return
	}
	eventCh := watcher.Events()
	for range eventCh {
		applyReload(result, baseDir, logDir, reloadMgr,
			serviceLifecycleTrigger, supdLifecycleTrigger,
			cronScheduler, eventRing, "watcher", apiServer, dispatcher, svcOperator)
	}
}

// applyReload 应用一次热重载：扫描、对比、更新 Discovery 引用、刷新 triggers/cron、发送事件
// N-04-01/N-04-02 修复：抽出公共逻辑，watcher 事件和 SIGHUP 信号共用
// N-04-001 修复：更新 API Server 的 providers Discovery 引用
func applyReload(
	result *core.BootstrapResult,
	baseDir, logDir string,
	reloadMgr *watch.ReloadManager,
	serviceLifecycleTrigger *extension.ServiceLifecycleTrigger,
	supdLifecycleTrigger *extension.SupdLifecycleTrigger,
	cronScheduler *extension.CronScheduler,
	eventRing *api.EventRingBuffer,
	source string,
	apiServer *api.Server,
	dispatcher *extension.Dispatcher,
	svcOperator *api.CoreServiceOperator,
) {
	slog.Info("检测到配置变更，执行热重载", "source", source)
	oldDiscovery := result.Discovery
	disc := watch.NewDiscovery(baseDir, logDir)
	newDiscovery := disc.Scan()

	// B-05-002: 清理被删除扩展的 ConcurrencyManager tracker，避免内存泄漏
	if dispatcher != nil {
		dispatcher.CleanupRemovedExtensions(oldDiscovery, newDiscovery)
	}

	reloadResult := reloadMgr.Reload(oldDiscovery, newDiscovery)

	// A-06-001 修复：config.yaml 变更分类
	// DiscoveryResult 不携带 *config.Config，ReloadManager.compareConfig 无法在此路径分类；
	// 由调用方独立加载 baseDir/config.yaml，复用 ReloadConfig() 完成分类并合并到主结果
	if result.Config != nil {
		cfgPath := filepath.Join(baseDir, "config.yaml")
		newCfg, cfgErr := config.LoadConfig(cfgPath)
		if cfgErr != nil {
			slog.Warn("config.yaml 加载失败，跳过配置变更分类", "error", cfgErr)
			reloadResult.Errors = append(reloadResult.Errors, fmt.Errorf("config.yaml load failed: %w", cfgErr))
		} else {
			cfgResult := reloadMgr.ReloadConfig(cfgPath, result.Config, newCfg)
			reloadResult.ImmediateChanges = append(reloadResult.ImmediateChanges, cfgResult.ImmediateChanges...)
			reloadResult.PendingChanges = append(reloadResult.PendingChanges, cfgResult.PendingChanges...)
			// 更新 result.Config 为最新，供下次重载对比使用
			result.Config = newCfg
		}
	}

	for _, imm := range reloadResult.ImmediateChanges {
		slog.Info("立即生效变更", "file", imm.File, "fields", imm.Fields)
	}
	for _, pending := range reloadResult.PendingChanges {
		slog.Info("待生效变更", "service", pending.ServiceName, "fields", pending.Changes)
	}
	for _, e := range reloadResult.Errors {
		slog.Warn("热重载错误", "error", e)
	}

	// N-04-01: 更新 DiscoveryResult 引用，并同步刷新 lifecycle triggers 和 cron scheduler
	result.Discovery = newDiscovery
	if serviceLifecycleTrigger != nil {
		serviceLifecycleTrigger.SetDiscovery(newDiscovery)
	}
	if supdLifecycleTrigger != nil {
		supdLifecycleTrigger.SetDiscovery(newDiscovery)
	}
	if cronScheduler != nil {
		// 先清除所有旧 jobs（闭包可能捕获旧 discovery），再用新 discovery 重新注册
		cronScheduler.ClearAllJobs()
		registerCronJobs(cronScheduler, newDiscovery)
	}

	// 规格 §2.4.3: restart 配置变更"立即生效"，原地更新所有 RestartEngine 的配置字段。
	// 使正在重试循环中的服务下次 ShouldRestart/MaxRetriesReached 决策使用最新配置，
	// 例如 max_retries 从 0（无限）改为 5 后，重试中的服务达到上限即停止。
	if svcOperator != nil {
		svcOperator.UpdateRestartEngines(result.Config, newDiscovery)
	}

	// N-04-001 修复：更新 API Server 中各 provider 的 Discovery 引用
	// providers 持有 Discovery 指针值拷贝，reload 后需要显式更新，否则 API 响应仍使用旧 Discovery
	if apiServer != nil {
		apiServer.UpdateDiscovery(newDiscovery)
	}

	// N-04-02: 发送 config_reloaded 事件（规格 §2.9.7）
	if eventRing != nil {
		eventRing.Publish("config_reloaded", map[string]any{
			"source":            source,
			"immediate_changes": len(reloadResult.ImmediateChanges),
			"pending_changes":   len(reloadResult.PendingChanges),
			"errors":            len(reloadResult.Errors),
		})
		// P-03-001 修复：重载有错误时发布 config_reload_failed 事件
		// N-04-001/P-01-001 修复：reloadResult.Errors 是 []error 接口，
		// encoding/json 对 error 接口序列化时仅输出 {}（无导出字段），
		// 导致事件流 UI 看到 errors: [{}]，无法定位具体配置错误。
		// 修复：发布前转换为 []string（调用 .Error()），并附加 path 信息（如有）。
		if len(reloadResult.Errors) > 0 {
			errMsgs := make([]string, 0, len(reloadResult.Errors))
			for _, e := range reloadResult.Errors {
				errMsgs = append(errMsgs, e.Error())
			}
			eventRing.Publish("config_reload_failed", map[string]any{
				"source": source,
				"errors": errMsgs,
			})
		}
	}
}

// registerCronJobs 将发现的 on_schedule 扩展注册到 CronScheduler
// REQ-D-004: type: on_schedule — 定时触发，标准 5 段 cron 表达式
func registerCronJobs(cronScheduler *extension.CronScheduler, discovery *watch.DiscoveryResult) {
	// 全局扩展
	for extName, extEntry := range discovery.GlobalExts {
		if extEntry.Meta == nil || extEntry.Meta.Enabled == nil || !*extEntry.Meta.Enabled {
			continue
		}
		for _, schedule := range extEntry.Meta.Triggers.OnSchedule {
			if schedule.Cron == "" {
				continue
			}
			retryCfg := extension.ToRetryConfig(schedule.RetryOnFailure)
			if err := cronScheduler.AddJob(extName, schedule.Action, schedule.Cron, retryCfg, discovery); err != nil {
				slog.Error("注册 cron 任务失败", "extension", extName, "schedule", schedule.Cron, "error", err)
			}
		}
	}

	// 服务级扩展
	for _, svcEntry := range discovery.Services {
		for extName, extEntry := range svcEntry.Extensions {
			if extEntry.Meta == nil || extEntry.Meta.Enabled == nil || !*extEntry.Meta.Enabled {
				continue
			}
			for _, schedule := range extEntry.Meta.Triggers.OnSchedule {
				if schedule.Cron == "" {
					continue
				}
				retryCfg := extension.ToRetryConfig(schedule.RetryOnFailure)
				if err := cronScheduler.AddJob(extName, schedule.Action, schedule.Cron, retryCfg, discovery); err != nil {
					slog.Error("注册 cron 任务失败", "extension", extName, "schedule", schedule.Cron, "error", err)
				}
			}
		}
	}
}

// startDiskSpaceMonitor 定期检查磁盘空间，磁盘满时发布 system_resource_warning 事件
// P-03-001 修复：规格§2.9.7 要求14种事件类型，system_resource_warning 之前从未发布
// 结合实际使用场景：内网个人应用，检查间隔60秒足够，避免过度设计
func startDiskSpaceMonitor(ctx context.Context, logDir string, publisher *api.EventRingBuffer) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	warningSent := false // 防止重复发送告警

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			diskFull := logging.IsDiskFull(logDir)
			if diskFull && !warningSent {
				slog.Warn("磁盘空间不足，可能影响日志写入", "log_dir", logDir)
				publisher.Publish("system_resource_warning", map[string]any{
					"resource":  "disk",
					"status":    "full",
					"path":      logDir,
					"timestamp": time.Now().Format(time.RFC3339),
				})
				warningSent = true
			} else if !diskFull && warningSent {
				// 磁盘恢复，重置告警状态
				slog.Info("磁盘空间恢复", "log_dir", logDir)
				warningSent = false
			}
		}
	}
}

// validateLogDir 启动期校验 log_dir：创建目录（如不存在）并验证可写性
// K-04-001 修复：避免运行时才发现日志目录不可写导致日志静默丢失
func validateLogDir(logDir string) error {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	// 写入测试文件验证可写性，写完立即删除
	testFile := filepath.Join(logDir, ".supd_writable_test")
	if err := os.WriteFile(testFile, []byte("supd writability test"), 0644); err != nil {
		return fmt.Errorf("目录不可写: %w", err)
	}
	if err := os.Remove(testFile); err != nil {
		return fmt.Errorf("删除测试文件失败: %w", err)
	}
	return nil
}
