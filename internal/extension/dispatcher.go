package extension

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/core"
	"github.com/supdorg/supd/internal/watch"
)

// DispatchRequest 调度请求
// REQ-F-022: 触发执行顺序与工作目录
// REQ-D-004: service_lifecycle/supd_lifecycle 触发时携带上下文字段
type DispatchRequest struct {
	// EventType 事件类型：on_demand/on_schedule/service_lifecycle/supd_lifecycle
	EventType string
	// Phase lifecycle 时的 phase（如 pre_start、post_ready、on_failure、pre_stop、pre_shutdown）
	Phase string
	// ServiceName 触发的服务名（service_lifecycle时，用于过滤只匹配该服务的扩展）
	ServiceName string
	// ServicePID 服务 PID（service_lifecycle 的 post_ready/pre_stop/on_failure 时）
	// REQ-D-004, 2.2.5: pre_start 时为 0（进程尚未启动），post_ready/pre_stop 时为实际 PID，
	// on_failure 时为进程退出前的 PID（proc.PID() 在进程退出后仍返回创建时存储的 PID）
	ServicePID int
	// ServiceExitCode on_failure 时的退出码
	// REQ-D-004, 2.2.5: SUPD_SERVICE_EXIT_CODE
	ServiceExitCode int
	// ServiceSignal on_failure 时的信号
	// REQ-D-004, 2.2.5: SUPD_SERVICE_SIGNAL
	ServiceSignal int
	// RestartCount 服务重启次数
	// REQ-D-004, 2.2.5: SUPD_SERVICE_RESTART_COUNT
	RestartCount int
	// Discovery 当前发现结果
	Discovery *watch.DiscoveryResult
	// TriggerUser 触发者
	TriggerUser string
}

// Dispatcher 触发调度器
// REQ-F-022: 根据触发事件调度扩展执行，管理执行顺序和工作目录
type Dispatcher struct {
	executor         *Executor
	baseDir          string
	logDir           string
	hardLimitSeconds int                 // REQ-F-019: config.yaml 的 extension_hard_limit_seconds
	concurrencyMgr   *ConcurrencyManager // REQ-F-018: 并发策略管理器
	eventPublisher   core.EventPublisher // P-03-001 修复：事件发布器
}

// NewDispatcher 创建触发调度器
// REQ-F-022: 初始化调度器
func NewDispatcher(executor *Executor, baseDir, logDir string, hardLimitSeconds int) *Dispatcher {
	return &Dispatcher{
		executor:         executor,
		baseDir:          baseDir,
		logDir:           logDir,
		hardLimitSeconds: hardLimitSeconds,
		concurrencyMgr:   NewConcurrencyManager(), // B-05-001 修复：接入 ConcurrencyManager
	}
}

// SetEventPublisher 注入事件发布器
// P-03-001 修复：用于发布扩展执行相关事件
func (d *Dispatcher) SetEventPublisher(publisher core.EventPublisher) {
	d.eventPublisher = publisher
}

// GetConcurrencyManager 返回内部 ConcurrencyManager
// N-03-001 修复：供 TaskManager 关联使用
func (d *Dispatcher) GetConcurrencyManager() *ConcurrencyManager {
	return d.concurrencyMgr
}

// CleanupRemovedExtensions 清理被删除扩展的 ConcurrencyManager tracker
// B-05-002: 热重载删除扩展时调用，避免 trackers 残留导致内存泄漏
// 对比 old/new DiscoveryResult，找出被删除的全局扩展和服务级扩展，调用 RemoveExtension
func (d *Dispatcher) CleanupRemovedExtensions(old, new *watch.DiscoveryResult) {
	if old == nil || new == nil {
		return
	}
	// 全局扩展
	for extName := range old.GlobalExts {
		if _, exists := new.GlobalExts[extName]; !exists {
			d.concurrencyMgr.RemoveExtension(extName)
		}
	}
	// 服务级扩展
	for svcName, oldSvc := range old.Services {
		newSvc, exists := new.Services[svcName]
		if !exists {
			// 整个服务被删除，清理其所有扩展
			for extName := range oldSvc.Extensions {
				d.concurrencyMgr.RemoveExtension(extName)
			}
		} else {
			// 服务存在，检查扩展是否被删除
			for extName := range oldSvc.Extensions {
				if _, exists := newSvc.Extensions[extName]; !exists {
					d.concurrencyMgr.RemoveExtension(extName)
				}
			}
		}
	}
}

// ExecuteOnDemand 执行单个 on_demand 扩展调用
// A-03-001 修复：on_demand API/CLI 入口需走 ConcurrencyManager 路径，
// 应用 concurrency 策略和 4 层 env 合并（与 service_lifecycle/supd_lifecycle/on_schedule 一致）
// progressCb 由调用方传入，用于实时更新 TaskManager 中的进度
func (d *Dispatcher) ExecuteOnDemand(ctx context.Context, meta *config.ExtensionMeta, tc TriggerContext, actionID string, progressCb ProgressCallback) (*RunResult, error) {
	return d.executeWithConcurrency(ctx, meta, tc, actionID, progressCb)
}

// matchedExtension 匹配到的扩展
// REQ-F-022: 触发匹配结果
type matchedExtension struct {
	extEntry    *watch.ExtensionEntry
	actionID    string
	actionArgs  []string
	serviceName string // 空=全局扩展
	// serviceUser 服务级扩展匹配时携带的服务 user 字段值，
	// 用于扩展执行时 ResolveRunAs 的服务级 run_as 继承（REQ-F-023, §2.2.13）
	// 全局扩展此字段为空 → ResolveRunAs 回退到 supd 启动用户
	serviceUser string
}

// Dispatch 触发调度
// REQ-F-022: 根据事件类型和阶段找到匹配扩展，按执行顺序触发
// 执行顺序规则：
//   - 以服务为粒度独立执行：不同服务的扩展可并行
//   - 同一服务内：先串行执行全局扩展（字母序），再串行执行服务级扩展（字母序）
//   - 前一个失败不影响后一个执行
func (d *Dispatcher) Dispatch(ctx context.Context, req DispatchRequest) []*RunResult {
	// Step 1: 找到所有匹配的扩展
	matched := findMatchingExtensions(req)
	if len(matched) == 0 {
		return nil
	}

	// Step 2: 按服务分组
	serviceGroups := groupByService(matched)

	// Step 3: 不同服务的组可并行执行
	var mu sync.Mutex
	var wg sync.WaitGroup
	var allResults []*RunResult

	for svcName, exts := range serviceGroups {
		wg.Add(1)
		go func(svc string, extensions []matchedExtension) {
			defer wg.Done()
			results := d.executeForService(ctx, svc, extensions, req)
			mu.Lock()
			allResults = append(allResults, results...)
			mu.Unlock()
		}(svcName, exts)
	}

	wg.Wait()
	return allResults
}

// findMatchingExtensions 找到所有匹配当前事件的扩展
// REQ-F-022: 遍历 DiscoveryResult 中的所有扩展的 triggers，匹配 EventType+Phase
// REQ-D-004: service_lifecycle 时仅匹配指定服务的服务级扩展
// REQ-F-023, §2.2.13: 服务级扩展匹配时携带 svcEntry.Config.User，供执行时 ResolveRunAs 继承
func findMatchingExtensions(req DispatchRequest) []matchedExtension {
	var matched []matchedExtension

	// 遍历全局扩展（serviceUser 为空 → ResolveRunAs 回退到 supd 启动用户）
	for _, extEntry := range req.Discovery.GlobalExts {
		if m := matchExtension(extEntry, req, "", ""); m != nil {
			matched = append(matched, *m)
		}
	}

	// 遍历服务级扩展（serviceUser = svcEntry.Config.User）
	for _, svcEntry := range req.Discovery.Services {
		// REQ-D-004: service_lifecycle 时仅匹配触发该事件的服务
		if req.EventType == "service_lifecycle" && req.ServiceName != "" && svcEntry.Name != req.ServiceName {
			continue
		}
		// 防御性：Config 可能在异常 DiscoveryResult 中为 nil（如测试桩或部分加载）
		var serviceUser string
		if svcEntry.Config != nil {
			serviceUser = svcEntry.Config.User
		}
		for _, extEntry := range svcEntry.Extensions {
			if m := matchExtension(extEntry, req, svcEntry.Name, serviceUser); m != nil {
				matched = append(matched, *m)
			}
		}
	}

	return matched
}

// matchExtension 检查单个扩展是否匹配当前触发事件
// REQ-F-022: 根据 EventType+Phase 匹配扩展的 triggers 定义
// REQ-F-023: serviceName/serviceUser 描述扩展所属服务的上下文，供执行时身份继承
func matchExtension(extEntry *watch.ExtensionEntry, req DispatchRequest, serviceName, serviceUser string) *matchedExtension {
	meta := extEntry.Meta
	if meta.Enabled == nil || !*meta.Enabled {
		return nil
	}

	switch req.EventType {
	case "on_demand":
		if meta.Triggers.OnDemand != nil && *meta.Triggers.OnDemand {
			actionID, actionArgs := FindActionByID(meta, "")
			return &matchedExtension{
				extEntry:    extEntry,
				actionID:    actionID,
				actionArgs:  actionArgs,
				serviceName: serviceName,
				serviceUser: serviceUser,
			}
		}
	case "on_schedule":
		for _, schedule := range meta.Triggers.OnSchedule {
			if schedule.Cron != "" {
				actionID := schedule.Action
				var actionArgs []string
				if actionID == "" {
					actionID, actionArgs = FindActionByID(meta, "")
				} else {
					_, actionArgs = FindActionByID(meta, actionID)
				}
				return &matchedExtension{
					extEntry:    extEntry,
					actionID:    actionID,
					actionArgs:  actionArgs,
					serviceName: serviceName,
					serviceUser: serviceUser,
				}
			}
		}
	case "service_lifecycle":
		for _, lc := range meta.Triggers.ServiceLifecycle {
			if lc.Event == req.Phase {
				actionID := lc.Action
				var actionArgs []string
				if actionID == "" {
					actionID, actionArgs = FindActionByID(meta, "")
				} else {
					_, actionArgs = FindActionByID(meta, actionID)
				}
				return &matchedExtension{
					extEntry:    extEntry,
					actionID:    actionID,
					actionArgs:  actionArgs,
					serviceName: serviceName,
					serviceUser: serviceUser,
				}
			}
		}
	case "supd_lifecycle":
		for _, lc := range meta.Triggers.SupdLifecycle {
			if lc.Event == req.Phase {
				actionID := lc.Action
				var actionArgs []string
				if actionID == "" {
					actionID, actionArgs = FindActionByID(meta, "")
				} else {
					_, actionArgs = FindActionByID(meta, actionID)
				}
				return &matchedExtension{
					extEntry:    extEntry,
					actionID:    actionID,
					actionArgs:  actionArgs,
					serviceName: serviceName,
					serviceUser: serviceUser,
				}
			}
		}
	}

	return nil
}

// FindActionByID 查找 action
// REQ-F-022: 根据 actionID 查找 action，id 为空时返回第一个 action
func FindActionByID(meta *config.ExtensionMeta, actionID string) (string, []string) {
	if actionID != "" {
		for _, a := range meta.Actions {
			if a.ID == actionID {
				return a.ID, a.Args
			}
		}
	}
	// 返回第一个 action
	if len(meta.Actions) > 0 {
		return meta.Actions[0].ID, meta.Actions[0].Args
	}
	return "run", nil
}

// groupByService 按服务名分组
// REQ-F-022: 以服务为粒度独立执行，全局扩展包含在拥有匹配扩展的服务组中
// 同一服务内：先全局扩展（字母序），后服务级扩展（字母序）
// 无服务级扩展时，全局扩展形成独立组
func groupByService(matched []matchedExtension) map[string][]matchedExtension {
	var globalExts, serviceExts []matchedExtension
	for _, m := range matched {
		if m.serviceName == "" {
			globalExts = append(globalExts, m)
		} else {
			serviceExts = append(serviceExts, m)
		}
	}

	groups := make(map[string][]matchedExtension)

	// 服务级扩展按服务名分组
	for _, m := range serviceExts {
		groups[m.serviceName] = append(groups[m.serviceName], m)
	}

	// 全局扩展的处理：
	// 如果有服务级扩展，全局扩展包含在每个服务组中
	// 如果没有服务级扩展，全局扩展形成独立组（serviceName 为空）
	if len(globalExts) > 0 {
		if len(groups) > 0 {
			// 全局扩展包含在每个服务组中
			// executeForService 会负责排序：先全局后服务级
			for svcName := range groups {
				groups[svcName] = append(globalExts, groups[svcName]...)
			}
		} else {
			// 无服务级扩展，全局扩展独立组
			groups[""] = append(groups[""], globalExts...)
		}
	}

	return groups
}

// buildWorkDir 构建扩展工作目录
// 工作目录为扩展自身目录（meta.yaml 所在目录），这样 entry 中的相对路径（如 run.sh）可以正确定位
// 同时创建 script_tmp 子目录供扩展脚本存放临时文件（通过 SUPD_SCRIPT_TMP 环境变量访问）
func buildWorkDir(baseDir string, extEntry *watch.ExtensionEntry) string {
	extDir := filepath.Dir(extEntry.ConfigPath)

	// 创建 script_tmp 临时目录，供扩展脚本写入临时文件
	var dirName string
	if extEntry.ServiceName == "" {
		dirName = "global+" + extEntry.Name
	} else {
		dirName = extEntry.ServiceName + "+" + extEntry.Name
	}
	scriptTmp := filepath.Join(baseDir, "script_tmp", dirName)
	if err := os.MkdirAll(scriptTmp, 0755); err != nil {
		slog.Warn("create extension script_tmp dir failed", "dir", scriptTmp, "extension", extEntry.Name, "service", extEntry.ServiceName, "error", err)
	}

	return extDir
}

// executeForService 以服务为粒度执行扩展
// REQ-F-022: 先全局扩展（字母序），后服务级扩展（字母序），串行执行，前失败不影响后
func (d *Dispatcher) executeForService(ctx context.Context, serviceName string, exts []matchedExtension, req DispatchRequest) []*RunResult {
	// 分离全局扩展和服务级扩展
	var globalExts, serviceExts []matchedExtension
	for _, ext := range exts {
		if ext.serviceName == "" {
			globalExts = append(globalExts, ext)
		} else {
			serviceExts = append(serviceExts, ext)
		}
	}

	// REQ-F-022: 同类型内部按目录名字母序串行执行
	sort.Slice(globalExts, func(i, j int) bool {
		return globalExts[i].extEntry.Name < globalExts[j].extEntry.Name
	})
	sort.Slice(serviceExts, func(i, j int) bool {
		return serviceExts[i].extEntry.Name < serviceExts[j].extEntry.Name
	})

	// REQ-F-022: 先串行执行全局扩展，再串行执行服务级扩展
	ordered := make([]matchedExtension, 0, len(globalExts)+len(serviceExts))
	ordered = append(ordered, globalExts...)
	ordered = append(ordered, serviceExts...)

	var results []*RunResult
	for _, ext := range ordered {
		// REQ-F-022: 构建工作目录
		workDir := buildWorkDir(d.baseDir, ext.extEntry)

		// 构建触发上下文
		// REQ-D-004, 2.2.5: 全局扩展在 service_lifecycle 触发时，SUPD_SERVICE 注入触发源服务名
		svcName := ext.serviceName
		if svcName == "" && req.EventType == "service_lifecycle" && req.ServiceName != "" {
			svcName = req.ServiceName
		}
		// REQ-F-023, §2.2.13 line 677: 全局扩展默认 run_as = supd 启动用户（不继承任何服务身份）
		// 即使被 service_lifecycle 触发，全局扩展的 ServiceUser 也必须为空，
		// 让 ResolveRunAs 走全局分支继承 supd 用户。
		// 服务级扩展的 serviceUser 在 findMatchingExtensions 中已填充为 svcEntry.Config.User。
		tc := TriggerContext{
			EventType:       req.EventType,
			TriggerSource:   req.EventType,
			TriggerUser:     req.TriggerUser,
			Phase:           req.Phase,
			ServiceName:     svcName,
			ServiceUser:     ext.serviceUser,
			ServicePID:      req.ServicePID,
			ServiceExitCode: req.ServiceExitCode,
			ServiceSignal:   req.ServiceSignal,
			RestartCount:    req.RestartCount,
			ActionID:        ext.actionID,
			ActionArgs:      ext.actionArgs,
			WorkDir:         workDir,
		}

		// REQ-F-022: 前一个失败不影响后一个执行
		// B-05-001 修复：通过 ConcurrencyManager 执行，应用 concurrency 策略
		result, _ := d.executeWithConcurrency(ctx, ext.extEntry.Meta, tc, ext.actionID, nil)
		if result != nil {
			results = append(results, result)
		}
	}

	return results
}

// executeWithConcurrency 通过 ConcurrencyManager 执行扩展，应用并发策略
// B-05-001 修复：将 ConcurrencyManager 接入生产代码
// REQ-F-018, 2.2.7: 同一 action 多次触发的行为由 concurrency 字段控制
// J-02-001 修复：接入 REQ-F-015 4层 env 合并逻辑
// A-03-001 修复：添加 progressCb 参数，支持 on_demand 调用的实时进度回调
func (d *Dispatcher) executeWithConcurrency(ctx context.Context, meta *config.ExtensionMeta, tc TriggerContext, actionID string, progressCb ProgressCallback) (*RunResult, error) {
	// 解析并发策略（默认 replace）
	concurrencyStr := meta.Concurrency
	if concurrencyStr == "" {
		concurrencyStr = "replace"
	}
	cfg, parseErr := ParseConcurrency(concurrencyStr)
	if parseErr != nil {
		// 配置校验本应拦截，此处为运行时兜底：记录警告并降级为 replace
		slog.Warn("invalid concurrency value, falling back to replace",
			"extension", meta.Name, "concurrency", concurrencyStr, "err", parseErr)
		cfg = ConcurrencyConfig{Policy: PolicyReplace}
	}

	// 获取该扩展+action 的追踪器
	tracker := d.concurrencyMgr.GetTracker(meta.Name, actionID, cfg.Policy, cfg.DebounceMs)

	// 生成 runID（与 executor.Execute 内部逻辑一致）
	runID := tc.RunID
	if runID == "" {
		runID = uuid.New().String()
		tc.RunID = runID
	}

	// J-02-001: 构建合并后的环境变量（REQ-F-015 4层合并）
	mergedEnvSlice := d.buildMergedEnv(tc.ServiceName, meta.Name)

	// 通过 tracker 执行，应用并发策略（replace/serialize/parallel/debounce）
	// 传入 ctx 作为父 context，确保外部取消能传播到扩展任务

	// P-03-001 修复：发布 extension_started 事件
	if d.eventPublisher != nil {
		d.eventPublisher.Publish("extension_started", map[string]any{
			"run_id":       runID,
			"extension":    meta.Name,
			"action":       actionID,
			"trigger_type": tc.EventType,
			"service":      tc.ServiceName,
		})
	}

	result, err := tracker.Apply(ctx, runID, func(childCtx context.Context) (*RunResult, error) {
		return d.executor.Execute(childCtx, meta, tc, mergedEnvSlice, d.hardLimitSeconds, progressCb)
	})

	// P-03-001 修复：根据执行结果发布对应事件
	if d.eventPublisher != nil && result != nil {
		eventType := "extension_completed"
		switch result.State {
		case TaskFailed:
			eventType = "extension_failed"
		case TaskCanceled:
			eventType = "extension_canceled"
		case TaskTimeout:
			eventType = "extension_timeout"
		}
		d.eventPublisher.Publish(eventType, map[string]any{
			"run_id":    result.RunID,
			"extension": result.ExtensionName,
			"action":    result.ActionID,
			"state":     string(result.State),
			"exit_code": result.ExitCode,
		})
	}

	// C-05-005 修复：接入 failure_handler，记录扩展执行失败
	if err == nil && result != nil && !result.IsSuccess() {
		HandleExtensionFailure(result, slog.Default())
	}
	return result, err
}

// buildMergedEnv 构建合并后的环境变量切片
// J-02-001 修复：接入 REQ-F-015 的 4 层 env 合并逻辑
// 合并顺序（后者覆盖前者）：
//  1. 全局 env 文件（env/*.yaml，按文件名字母序）
//  2. 全局扩展私有 env（extensions/<ext>/env.yaml）
//  3. 服务 env（services/<svc>/env.yaml）
//  4. 服务级扩展私有 env（services/<svc>/extensions/<ext>/env.yaml）
func (d *Dispatcher) buildMergedEnv(serviceName, extName string) []string {
	var layers []*config.EnvFile

	// Layer 1: 全局 env 文件（env/*.yaml，按文件名字母序）
	globalEnvDir := filepath.Join(d.baseDir, "env")
	if entries, err := os.ReadDir(globalEnvDir); err == nil {
		// 按文件名字母序加载
		var paths []string
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
				continue
			}
			paths = append(paths, filepath.Join(globalEnvDir, name))
		}
		sort.Strings(paths)
		for _, p := range paths {
			// C-01-01 修复：env.yaml 解析失败时记录警告，便于用户诊断
			ef, err := config.LoadEnv(p)
			if err != nil {
				slog.Warn("load env file failed", "path", p, "error", err)
				continue
			}
			layers = append(layers, ef)
		}
	}

	// Layer 2: 全局扩展私有 env（extensions/<ext>/env.yaml）
	if extName != "" {
		globalExtEnvPath := filepath.Join(d.baseDir, "extensions", extName, "env.yaml")
		// C-01-01 修复：env.yaml 解析失败时记录警告
		ef, err := config.LoadEnv(globalExtEnvPath)
		if err != nil {
			slog.Warn("load env file failed", "path", globalExtEnvPath, "error", err)
		} else {
			layers = append(layers, ef)
		}
	}

	// Layer 3: 服务 env（services/<svc>/env.yaml）
	if serviceName != "" {
		svcEnvPath := filepath.Join(d.baseDir, "services", serviceName, "env.yaml")
		// C-01-01 修复：env.yaml 解析失败时记录警告
		ef, err := config.LoadEnv(svcEnvPath)
		if err != nil {
			slog.Warn("load env file failed", "path", svcEnvPath, "error", err)
		} else {
			layers = append(layers, ef)
		}
	}

	// Layer 4: 服务级扩展私有 env（services/<svc>/extensions/<ext>/env.yaml）
	if serviceName != "" && extName != "" {
		svcExtEnvPath := filepath.Join(d.baseDir, "services", serviceName, "extensions", extName, "env.yaml")
		// C-01-01 修复：env.yaml 解析失败时记录警告
		ef, err := config.LoadEnv(svcExtEnvPath)
		if err != nil {
			slog.Warn("load env file failed", "path", svcExtEnvPath, "error", err)
		} else {
			layers = append(layers, ef)
		}
	}

	if len(layers) == 0 {
		return nil
	}

	// 合并并过滤 enabled=false 的变量
	merged := config.MergeEnv(layers...)
	injected := config.ToInjectEnv(merged)

	// 转为 "KEY=VALUE" 格式
	result := make([]string, 0, len(injected))
	for k, v := range injected {
		result = append(result, k+"="+v)
	}
	return result
}
