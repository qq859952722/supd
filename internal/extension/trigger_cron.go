package extension

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/watch"
)

// CronTrigger cron 触发器
// REQ-D-004: type: on_schedule — 定时触发 + retry_on_failure
type CronTrigger struct {
	scheduler *CronScheduler
	// retryAttempts 记录每个扩展 action 的当前重试次数
	// key = "extName:actionID"
	retryAttempts map[string]int
	// retryCtx/retryCancel 用于取消所有待执行的重试 goroutine（热重载时调用 ClearRetryState）
	retryCtx     context.Context
	retryCancel  context.CancelFunc
	mu           sync.Mutex // B-05-001 修复：保护 retryAttempts 的并发访问
}

// NewCronTrigger 创建 cron 触发器
// REQ-D-004: 初始化 cron 触发器
func NewCronTrigger(scheduler *CronScheduler) *CronTrigger {
	ctx, cancel := context.WithCancel(context.Background())
	return &CronTrigger{
		scheduler:     scheduler,
		retryAttempts: make(map[string]int),
		retryCtx:      ctx,
		retryCancel:   cancel,
	}
}

// ClearRetryState 取消所有待执行的重试 goroutine 并清理重试计数
// 热重载时由 CronScheduler.ClearAllJobs 调用，避免待执行的重试用旧 discovery 执行
func (ct *CronTrigger) ClearRetryState() {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	if ct.retryCancel != nil {
		ct.retryCancel() // 取消所有持有 retryCtx 的待执行 goroutine
	}
	ct.retryAttempts = make(map[string]int)
	ct.retryCtx, ct.retryCancel = context.WithCancel(context.Background())
}

// HandleResult 处理 cron 触发的执行结果
// REQ-D-004, REQ-F-020: retry_on_failure — 失败后每次重试生成新的 run_id，
// 在任务历史中标记为重试，max_retries 用尽后不再重试
func (ct *CronTrigger) HandleResult(ctx context.Context, result *RunResult, retryConfig *RetryConfig, discovery *watch.DiscoveryResult) {
	if result == nil {
		return
	}

	// 只处理失败的情况
	if result.IsSuccess() {
		// 成功则重置重试计数
		key := result.ExtensionName + ":" + result.ActionID
		ct.mu.Lock()
		delete(ct.retryAttempts, key)
		ct.mu.Unlock()
		return
	}

	// C-05-003 修复：使用 ShouldRetry 判断是否应重试
	key := result.ExtensionName + ":" + result.ActionID
	ct.mu.Lock()
	attempts := ct.retryAttempts[key]

	if !ShouldRetry(result.State, retryConfig, attempts) {
		ct.mu.Unlock()
		// 重试次数用尽时清理计数并记录日志
		if attempts > 0 && retryConfig != nil && attempts >= retryConfig.MaxRetries {
			slog.Warn("cron retry: max retries exhausted",
				"extension", result.ExtensionName,
				"action", result.ActionID,
				"attempts", attempts,
				"max_retries", retryConfig.MaxRetries,
			)
			ct.mu.Lock()
			delete(ct.retryAttempts, key)
			ct.mu.Unlock()
		}
		return
	}

	// 增加重试计数
	ct.retryAttempts[key] = attempts + 1
	// 在锁内读取 retryCtx 快照，避免与 ClearRetryState 的写入竞态
	retryCtx := ct.retryCtx
	ct.mu.Unlock()

	// REQ-D-004: 失败后每次重试生成新的 run_id
	// 按配置的间隔延迟后重试
	interval := time.Duration(retryConfig.IntervalMinutes) * time.Minute
	slog.Info("cron retry: scheduling retry",
		"extension", result.ExtensionName,
		"action", result.ActionID,
		"attempt", attempts+1,
		"max_retries", retryConfig.MaxRetries,
		"interval", interval,
		"original_run_id", result.RunID,
	)

	// 使用 retryCtx 调度重试（热重载时 ClearRetryState 可取消待执行的重试）
	go func() {
		select {
		case <-retryCtx.Done():
			return
		case <-time.After(interval):
			ct.executeRetry(retryCtx, result.ExtensionName, result.ActionID, retryConfig, discovery, attempts+1)
		}
	}()
}

// executeRetry 执行重试
// REQ-D-004: 每次重试生成新的 run_id，在任务历史中标记为重试
// A-04-001 修复：cron retry 走 Dispatcher 路径，应用并发策略和 env 合并
// A-04-003 修复：TriggerSource 使用规格枚举值 "schedule"（非 "schedule_retry"）
// retryConfig 传入以在重试失败后继续调度下一轮重试，形成 max_retries 次重试链
func (ct *CronTrigger) executeRetry(ctx context.Context, extName, actionID string, retryConfig *RetryConfig, discovery *watch.DiscoveryResult, attempt int) {
	extEntry, svcName, err := findExtensionByName(discovery, extName)
	if err != nil {
		slog.Error("cron retry: extension not found", "extension", extName, "error", err)
		return
	}

	resolvedActionID, actionArgs := FindActionByID(extEntry.Meta, actionID)
	workDir := buildWorkDir(ct.scheduler.dispatcher.baseDir, extEntry)

	// REQ-D-004: 每次重试生成新的 run_id（由 Executor.Execute 内部 uuid.New() 生成）
	tc := TriggerContext{
		EventType:     "on_schedule",
		TriggerSource: "schedule", // A-04-003 修复：使用规格枚举值，不使用 "schedule_retry"
		ActionID:      resolvedActionID,
		ActionArgs:    actionArgs,
		ServiceName:   svcName,
		WorkDir:       workDir,
	}

	slog.Info("cron retry: executing",
		"extension", extName,
		"action", actionID,
		"attempt", attempt,
		"new_run_id_prefix", uuid.New().String()[:8], // 仅用于日志，实际 run_id 由 executor 生成
	)

	// A-04-001 修复：走 dispatcher.executeWithConcurrency 路径，应用并发策略和 env 合并
	// A-04-004 修复：hardLimitSeconds 由 dispatcher 内部管理，不再硬编码
	result, err := ct.scheduler.dispatcher.executeWithConcurrency(ctx, extEntry.Meta, tc, resolvedActionID, nil)
	if err != nil {
		slog.Error("cron retry: execution failed", "extension", extName, "error", err)
		return
	}

	// 记录重试结果到 TaskManager（与 jobFunc 一致，使前端可见重试历史）
	if result != nil {
		ct.scheduler.mu.Lock()
		taskMgr := ct.scheduler.taskMgr
		ct.scheduler.mu.Unlock()
		if taskMgr != nil {
			taskMgr.RecordRun(result)
		}
		slog.Info("cron retry: result",
			"extension", extName,
			"action", actionID,
			"attempt", attempt,
			"run_id", result.RunID,
			"state", result.State,
		)
	}

	// 重试链：重试仍失败时继续调度下一轮，直到 max_retries 用尽
	if result != nil && result.State != TaskSuccess && retryConfig != nil {
		ct.HandleResult(ctx, result, retryConfig, discovery)
	}
}

// resolveScheduleActionID 解析 on_schedule 触发器的 actionID
// REQ-D-004: action 为空时使用第一个 action，若无 action 则使用 "run"
func resolveScheduleActionID(meta *config.ExtensionMeta, scheduleAction string) string {
	if scheduleAction != "" {
		return scheduleAction
	}
	if len(meta.Actions) > 0 {
		return meta.Actions[0].ID
	}
	return "run"
}

// RegisterSchedule 从扩展配置中注册 cron 调度
// REQ-D-004: 遍历扩展的 on_schedule triggers，为每个调度注册 cron job
func (ct *CronTrigger) RegisterSchedule(extEntry *watch.ExtensionEntry, discovery *watch.DiscoveryResult) error {
	if extEntry.Meta.Enabled == nil || !*extEntry.Meta.Enabled {
		return nil
	}

	for _, schedule := range extEntry.Meta.Triggers.OnSchedule {
		if schedule.Cron == "" {
			continue
		}

		actionID := resolveScheduleActionID(extEntry.Meta, schedule.Action)

		// REQ-D-004: 验证 action 引用存在
		if !validateActionID(extEntry.Meta, actionID) {
			return fmt.Errorf("extension %s: cron action %s not found in actions", extEntry.Name, actionID)
		}

		err := ct.scheduler.AddJob(extEntry.Name, actionID, schedule.Cron, ToRetryConfig(schedule.RetryOnFailure), discovery)
		if err != nil {
			return fmt.Errorf("extension %s: register cron job failed: %w", extEntry.Name, err)
		}
	}

	return nil
}

// UnregisterSchedule 移除扩展的所有 cron 调度
// REQ-D-004: 移除指定扩展的所有 on_schedule cron jobs
func (ct *CronTrigger) UnregisterSchedule(extEntry *watch.ExtensionEntry) {
	for _, schedule := range extEntry.Meta.Triggers.OnSchedule {
		actionID := resolveScheduleActionID(extEntry.Meta, schedule.Action)
		ct.scheduler.RemoveJob(extEntry.Name, actionID)
	}

	// 清理重试计数（B-05-001 修复：加锁保护）
	ct.mu.Lock()
	defer ct.mu.Unlock()
	for _, schedule := range extEntry.Meta.Triggers.OnSchedule {
		actionID := resolveScheduleActionID(extEntry.Meta, schedule.Action)
		delete(ct.retryAttempts, extEntry.Name+":"+actionID)
	}
}

// GetRetryAttempts 获取指定扩展 action 的当前重试次数
func (ct *CronTrigger) GetRetryAttempts(extName, actionID string) int {
	key := extName + ":" + actionID
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return ct.retryAttempts[key]
}
