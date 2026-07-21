package api

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v4"

	"github.com/google/uuid"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/errors"
	"github.com/supdorg/supd/internal/extension"
	"github.com/supdorg/supd/internal/watch"
)

// --- ExtensionProvider 适配器 ---

type CoreExtensionProvider struct {
	Discovery  *watch.DiscoveryResult
	Executor   *extension.Executor
	Dispatcher *extension.Dispatcher // A-03-001 修复：on_demand 调用走 Dispatcher 路径，应用并发策略和 env 合并
	TaskMgr    *extension.TaskManager
	BaseDir    string
	// B-04-001 修复：on_demand 异步执行的根 context，由 run.go 的 app context 派生
	// supd 退出时此 context 被取消，避免扩展进程孤儿化
	AppCtx context.Context
}

// SetDiscovery 热重载时更新 Discovery 引用
// N-04-001 修复：providers 持有 Discovery 指针值拷贝，reload 后需要显式更新
func (p *CoreExtensionProvider) SetDiscovery(d *watch.DiscoveryResult) {
	if p == nil || d == nil {
		return
	}
	p.Discovery = d
}

func (p *CoreExtensionProvider) ListExtensions() []ExtensionInfo {
	var exts []ExtensionInfo
	// D-05-02 修复：Discovery 为 nil 时返回空列表（热重载期间可能短暂为 nil）
	if p.Discovery == nil {
		return exts
	}

	// 全局扩展
	for name, extEntry := range p.Discovery.GlobalExts {
		exts = append(exts, p.extEntryToInfo(name, extEntry, ""))
	}

	// 服务级扩展
	for _, svcEntry := range p.Discovery.Services {
		for extName, extEntry := range svcEntry.Extensions {
			exts = append(exts, p.extEntryToInfo(extName, extEntry, svcEntry.Name))
		}
	}

	return exts
}

func (p *CoreExtensionProvider) extEntryToInfo(name string, extEntry *watch.ExtensionEntry, serviceName string) ExtensionInfo {
	// D-05-02 修复：extEntry 或 Meta 为 nil 时返回最小化 info，避免 nil 解引用 panic
	if extEntry == nil || extEntry.Meta == nil {
		return ExtensionInfo{
			Name:    name,
			Service: serviceName,
			Enabled: false,
		}
	}
	meta := extEntry.Meta
	enabled := meta.Enabled != nil && *meta.Enabled
	displayState := string(extension.GetDisplayState(meta))
	triggerType := extension.GetTriggerType(meta)

	info := ExtensionInfo{
		Name:         name,
		Version:      meta.Version,
		Description:  meta.Description,
		Enabled:      enabled,
		DisplayState: displayState,
		TriggerType:  triggerType,
		Service:      serviceName,
		Meta:         meta,
		ConfigPath:   extEntry.ConfigPath,
		EnvPath:      extEntry.EnvPath,
	}

	// REQ-2.2.14: 扩展列表展示运行历史统计
	// 从 TaskManager 获取该扩展的任务历史并聚合统计
	if p.TaskMgr != nil {
		runs := p.TaskMgr.ListRuns(extension.RunFilter{
			ExtensionName: name,
			ServiceName:   serviceName,
		})
		info.RunCount = len(runs)
		var lastRun *extension.RunResult
		for i := range runs {
			r := runs[i]
			if r.State == extension.TaskSuccess {
				info.SuccessCount++
			} else if r.State == extension.TaskFailed || r.State == extension.TaskTimeout ||
				r.State == extension.TaskKilled {
				info.FailCount++
			}
			if lastRun == nil || r.StartedAt.After(lastRun.StartedAt) {
				lastRun = r
			}
		}
		if lastRun != nil {
			info.LastRunAt = lastRun.StartedAt.Format("2006-01-02T15:04:05Z07:00")
			info.LastStatus = string(lastRun.State)
		}
	}

	return info
}

func (p *CoreExtensionProvider) GetExtension(name string) (*ExtensionInfo, bool) {
	// D-05-1 修复：Discovery 为 nil 时返回 nil, false（热重载期间可能短暂为 nil）
	if p.Discovery == nil {
		return nil, false
	}
	// 先搜索全局扩展
	if extEntry, ok := p.Discovery.GlobalExts[name]; ok {
		info := p.extEntryToInfo(name, extEntry, "")
		return &info, true
	}
	// 再搜索服务级扩展
	for _, svcEntry := range p.Discovery.Services {
		if extEntry, ok := svcEntry.Extensions[name]; ok {
			info := p.extEntryToInfo(name, extEntry, svcEntry.Name)
			return &info, true
		}
	}
	return nil, false
}

func (p *CoreExtensionProvider) CreateExtension(meta *config.ExtensionMeta, service string) error {
	var dir string
	if service != "" {
		dir = filepath.Join(p.BaseDir, "services", service, "extensions", meta.Name)
	} else {
		dir = filepath.Join(p.BaseDir, "extensions", meta.Name)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := yaml.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal extension meta: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "meta.yaml"), data, 0644)
}

func (p *CoreExtensionProvider) UpdateExtension(name string, meta *config.ExtensionMeta, service string) error {
	info, ok := p.GetExtension(name)
	if !ok {
		return fmt.Errorf("extension %s not found", name)
	}

	configPath := info.ConfigPath
	// service 参数在 UpdateExtension 中不使用（configPath 已定位文件），
	// 保留参数以与 CreateExtension 接口一致

	data, err := yaml.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal extension meta: %w", err)
	}
	return os.WriteFile(configPath, data, 0644)
}

func (p *CoreExtensionProvider) DeleteExtension(name string, service string) error {
	info, ok := p.GetExtension(name)
	if !ok {
		// N-03-DELETE-404 修复：返回 ErrExtensionNotFound 以便 handler 映射为 404
		return errors.NewServiceError(errors.ErrExtensionNotFound,
			fmt.Sprintf("extension %s not found", name))
	}

	dir := filepath.Dir(info.ConfigPath)

	// N-03-1 修复：删除前备份到 .bak.<timestamp>/，与导入流程一致
	backupDir := dir + ".bak." + time.Now().Format("20060102-150405")
	if err := os.Rename(dir, backupDir); err != nil {
		return fmt.Errorf("failed to backup extension before delete: %w", err)
	}

	// 备份成功后，异步清理旧的备份目录（保留最近 5 个）
	go cleanupOldBackups(filepath.Dir(dir), filepath.Base(dir)+".bak.")

	return nil
}

// cleanupOldBackups 保留最近 maxKeep 个备份目录，删除更早的
func cleanupOldBackups(parentDir, prefix string) {
	entries, err := os.ReadDir(parentDir)
	if err != nil {
		return
	}
	var backups []os.DirEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), prefix) {
			backups = append(backups, e)
		}
	}
	// 按名称排序（时间戳保证字典序=时间序）
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Name() > backups[j].Name()
	})
	// 删除超出保留数量的旧备份
	// C-01-005/I-03-002 修复：RemoveAll 错误记录日志，便于诊断备份清理失败
	maxKeep := 5
	for i := maxKeep; i < len(backups); i++ {
		if rmErr := os.RemoveAll(filepath.Join(parentDir, backups[i].Name())); rmErr != nil {
			slog.Warn("remove old backup failed", "file", backups[i].Name(), "error", rmErr)
		}
	}
}

func (p *CoreExtensionProvider) SaveExtensionEnv(name string, envData *config.EnvFile, service string) error {
	info, ok := p.GetExtension(name)
	if !ok {
		return fmt.Errorf("extension %s not found", name)
	}

	envPath := info.EnvPath
	if envPath == "" {
		dir := filepath.Dir(info.ConfigPath)
		envPath = filepath.Join(dir, "env.yaml")
	}

	data, err := yaml.Marshal(envData)
	if err != nil {
		return fmt.Errorf("marshal env data: %w", err)
	}
	return os.WriteFile(envPath, data, 0644)
}

func (p *CoreExtensionProvider) RunExtension(ctx context.Context, name string, actionID string, service string, dryRun bool) (*extension.RunResult, error) {
	if p.Executor == nil {
		return nil, fmt.Errorf("extension executor not configured")
	}

	info, ok := p.GetExtension(name)
	if !ok {
		return nil, fmt.Errorf("extension %s not found", name)
	}

	if info.Meta == nil {
		return nil, fmt.Errorf("extension %s has no meta", name)
	}

	resolvedActionID, actionArgs := extension.FindActionByID(info.Meta, actionID)
	svcName := info.Service

	// 预生成 run_id — 异步执行模式：
	// 1. 预记录 TaskRunning 状态到 TaskManager，使 runs API 立即返回此任务
	// 2. goroutine 中执行扩展，通过 onProgress 回调实时更新进度
	// 3. 立即返回预记录结果，前端可通过 runs API 轮询进度和日志
	runID := uuid.New().String()

	// 工作目录为扩展自身目录（meta.yaml 所在目录），这样 entry 中的相对路径（如 run.sh）可以正确定位
	workDir := filepath.Dir(info.ConfigPath)
	serviceDir := ""
	if svcName != "" && p.BaseDir != "" {
		serviceDir = filepath.Join(p.BaseDir, "services", svcName)
	}

	tc := extension.TriggerContext{
		EventType:     "on_demand",
		TriggerSource: "webui",
		ActionID:      resolvedActionID,
		ActionArgs:    actionArgs,
		ServiceName:   svcName,
		ServiceDir:    serviceDir,
		WorkDir:       workDir,
		RunID:         runID,
	}

	// N-03-01 修复：dry_run 模式 — 不 fork 进程，仅记录任务并返回成功
	// REQ-F-037 A.2: dry_run=true 时扩展不产生实际副作用
	// A-03-001 修复：dry_run 不记入正式运行历史（规格 §2.11.9），仅返回结果供调用方即时查看
	if dryRun {
		result := &extension.RunResult{
			RunID:         runID,
			ExtensionName: name,
			ActionID:      resolvedActionID,
			State:         extension.TaskSuccess,
			StartedAt:     time.Now(),
			FinishedAt:    time.Now(),
			TriggerType:   "on_demand",
			ServiceName:   svcName,
			ResultLevel:   "success",
			ResultMsg:     "dry run: no process started",
			Progress:      100,
		}
		slog.Info("extension dry run requested", "extension", name, "action", resolvedActionID, "run_id", runID)
		return result, nil
	}

	// 预记录 TaskRunning 状态
	preliminary := &extension.RunResult{
		RunID:         runID,
		ExtensionName: name,
		ActionID:      resolvedActionID,
		State:         extension.TaskRunning,
		StartedAt:     time.Now(),
		TriggerType:   "on_demand",
		ServiceName:   svcName,
	}
	if p.TaskMgr != nil {
		p.TaskMgr.RecordRun(preliminary)
	}

	// 异步执行扩展 — 使用 AppCtx 派生的 context，不受 HTTP 请求生命周期影响
	// B-04-001 修复：supd 退出时 AppCtx 被取消，可终止运行中的 on_demand 扩展
	go func() {
		bgCtx := p.AppCtx
		if bgCtx == nil {
			// 兼容路径：AppCtx 未注入时回退到 Background（行为同修复前）
			bgCtx = context.Background()
		}

		// 进度回调：实时更新 TaskManager 中的进度
		var onProgress extension.ProgressCallback
		if p.TaskMgr != nil {
			taskMgr := p.TaskMgr
			onProgress = func(progress int, resultMsg string) {
				taskMgr.UpdateProgress(runID, progress, resultMsg)
			}
		}

		// A-03-001 修复：on_demand 调用走 Dispatcher 路径，应用 concurrency 策略和 4 层 env 合并
		// 之前直接调用 p.Executor.Execute 绕过了 ConcurrencyManager，导致并发策略和 env 合并失效
		var result *extension.RunResult
		var err error
		if p.Dispatcher != nil {
			result, err = p.Dispatcher.ExecuteOnDemand(bgCtx, info.Meta, tc, resolvedActionID, onProgress)
		} else {
			// 兼容路径：Dispatcher 未注入时回退到直接执行（不应用并发策略）
			slog.Warn("Dispatcher not injected, on_demand extension bypasses ConcurrencyManager", "extension", name)
			// O-05-001 修复：使用 config.ExtensionHardLimitSeconds 常量替代字面量 1800
			result, err = p.Executor.Execute(bgCtx, info.Meta, tc, nil, config.ExtensionHardLimitSeconds, onProgress)
		}
		if err != nil {
			slog.Error("异步扩展执行失败", "extension", name, "run_id", runID, "error", err)
			if p.TaskMgr != nil && result != nil {
				p.TaskMgr.RecordRun(result)
			}
			return
		}
		// 记录最终结果（同一 run_id，更新预记录）
		// 进度由 onProgress 回调实时更新到 TaskManager，Execute 的 result.Progress 可能为 0
		// 合并 TaskManager 中的最新进度，避免覆盖
		if p.TaskMgr != nil && result != nil {
			if existing := p.TaskMgr.GetRun(runID); existing != nil && existing.Progress > result.Progress {
				result.Progress = existing.Progress
			}
			p.TaskMgr.RecordRun(result)
		}
	}()

	// 立即返回预记录结果，前端通过 runs API 轮询进度
	return preliminary, nil
}

func (p *CoreExtensionProvider) GetExtensionStatus(name string, service string) (map[string]any, error) {
	info, ok := p.GetExtension(name)
	if !ok {
		return nil, fmt.Errorf("extension %s not found", name)
	}
	return map[string]any{
		"name":          info.Name,
		"enabled":       info.Enabled,
		"display_state": info.DisplayState,
	}, nil
}
