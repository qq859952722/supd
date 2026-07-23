package extension

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/supdorg/supd/internal/watch"
)

// CronScheduler cron 调度器封装
// REQ-D-004: type: on_schedule — 定时触发，标准 5 段 cron 表达式
type CronScheduler struct {
	cron         *cron.Cron
	dispatcher   *Dispatcher
	taskMgr      *TaskManager // 用于记录 cron 触发的执行结果
	cronTrigger  *CronTrigger // REQ-D-004: retry_on_failure 处理器
	entries      map[string]cron.EntryID            // key = "extName:actionID"
	retryConfigs map[string]*RetryConfig            // key = "extName:actionID"
	mu           sync.Mutex
}

// NewCronScheduler 创建 cron 调度器
// REQ-D-004: 初始化 cron 调度器
// A-05-001 修复：规格 §2.4 第 14 行和 §2.8.1 第 163 行要求 cron 使用服务器时区（Asia/Shanghai）。
// 原实现 cron.New() 默认使用 time.Local，依赖系统时区配置；
// 改为显式 WithLocation 固定 CST（UTC+8），避免在不同部署环境下时区漂移。
func NewCronScheduler(dispatcher *Dispatcher) *CronScheduler {
	return &CronScheduler{
		cron:         cron.New(cron.WithLocation(time.FixedZone("CST", 8*3600))), // REQ-D-004: 标准 5 段 cron 表达式，固定 Asia/Shanghai 时区
		dispatcher:   dispatcher,
		entries:      make(map[string]cron.EntryID),
		retryConfigs: make(map[string]*RetryConfig),
	}
}

// SetCronTrigger 注入 CronTrigger，用于 retry_on_failure 处理
// REQ-D-004: cron 执行失败后由 CronTrigger.HandleResult 调度重试
func (s *CronScheduler) SetCronTrigger(ct *CronTrigger) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cronTrigger = ct
}

// SetTaskManager 注入 TaskManager，用于记录 cron 触发的执行结果
// B-04-RACE-001 修复：使用 mu 保护 taskMgr 字段，防止与 jobFunc 中的读取竞态
func (s *CronScheduler) SetTaskManager(tm *TaskManager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.taskMgr = tm
}

// AddJob 添加 cron 定时任务
// REQ-D-004: on_schedule 的 schedule 字段为标准 cron 表达式
// extName 和 actionID 用于标识和回调
// retryCfg 为可选的失败重试配置（nil 表示不重试）
func (s *CronScheduler) AddJob(extName, actionID, schedule string, retryCfg *RetryConfig, discovery *watch.DiscoveryResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := extName + ":" + actionID

	// 如果已有同 key 的任务，先移除
	if entryID, ok := s.entries[key]; ok {
		s.cron.Remove(entryID)
		delete(s.entries, key)
	}
	// 存储重试配置（nil 也存，覆盖旧值）
	s.retryConfigs[key] = retryCfg
	// 读取 cronTrigger（在锁内读取，jobFunc 闭包使用快照）
	cronTrigger := s.cronTrigger

	// REQ-D-004: 创建 cron job，到期时触发扩展执行
	jobFunc := func() {
		slog.Info("cron trigger fired", "extension", extName, "action", actionID)

		// P-03-001 修复：发布 cron_triggered 事件
		if s.dispatcher.eventPublisher != nil {
			s.dispatcher.eventPublisher.Publish("cron_triggered", map[string]any{
				"extension": extName,
				"action":    actionID,
				"schedule":  schedule,
			})
		}

		ctx := context.Background()
		extEntry, svcName, err := findExtensionByName(discovery, extName)
		if err != nil {
			slog.Error("cron trigger: extension not found", "extension", extName, "error", err)
			return
		}

		resolvedActionID, actionArgs := FindActionByID(extEntry.Meta, actionID)
		workDir := buildWorkDir(s.dispatcher.baseDir, extEntry)

		tc := TriggerContext{
			EventType:     "on_schedule",
			TriggerSource: "schedule",
			ActionID:      resolvedActionID,
			ActionArgs:    actionArgs,
			ServiceName:   svcName,
			WorkDir:       workDir,
		}

		// B-05-001 修复：通过 ConcurrencyManager 执行，应用 concurrency 策略
	result, err := s.dispatcher.executeWithConcurrency(ctx, extEntry.Meta, tc, resolvedActionID, nil)
	if err != nil {
		slog.Error("cron trigger: execution failed", "extension", extName, "error", err)
		return
	}

	// 记录执行结果到 TaskManager，使前端可以看到 cron 触发的执行历史
	// B-04-RACE-001 修复：读取 taskMgr 字段时加锁，防止与 SetTaskManager 写入竞态
	s.mu.Lock()
	taskMgr := s.taskMgr
	s.mu.Unlock()
	if taskMgr != nil && result != nil {
		taskMgr.RecordRun(result)
	}

	// REQ-D-004: retry_on_failure — 失败后由 CronTrigger.HandleResult 调度重试
		if result != nil && result.State != TaskSuccess {
			slog.Warn("cron trigger: extension failed",
				"extension", extName,
				"action", actionID,
				"state", result.State,
				"run_id", result.RunID,
			)
			// 有重试配置且 cronTrigger 已注入时，触发重试调度
			if retryCfg != nil && cronTrigger != nil {
				cronTrigger.HandleResult(ctx, result, retryCfg, discovery)
			}
		}
	}

	entryID, err := s.cron.AddFunc(schedule, jobFunc)
	if err != nil {
		return fmt.Errorf("cron add job %s/%s: %w", extName, actionID, err)
	}

	s.entries[key] = entryID
	slog.Info("cron job added", "extension", extName, "action", actionID, "schedule", schedule)

	return nil
}

// RemoveJob 移除 cron 定时任务
// REQ-D-004: 移除指定扩展 action 的 cron 任务
func (s *CronScheduler) RemoveJob(extName, actionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := extName + ":" + actionID
	if entryID, ok := s.entries[key]; ok {
		s.cron.Remove(entryID)
		delete(s.entries, key)
		delete(s.retryConfigs, key)
		slog.Info("cron job removed", "extension", extName, "action", actionID)
	}
}

// ClearAllJobs 清除所有 cron 定时任务
// N-04-01 修复：热重载时调用，移除所有旧 jobs，避免闭包捕获旧 discovery
// 同时取消待执行的重试 goroutine 并清理重试计数，避免热重载后用旧 discovery 执行重试
func (s *CronScheduler) ClearAllJobs() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, entryID := range s.entries {
		s.cron.Remove(entryID)
		delete(s.entries, key)
	}
	s.retryConfigs = make(map[string]*RetryConfig)
	// 取消待执行的重试 goroutine + 清理 retryAttempts，避免热重载后残留状态
	if s.cronTrigger != nil {
		s.cronTrigger.ClearRetryState()
	}
	slog.Info("all cron jobs cleared for hot reload")
}

// Start 启动 cron 调度器
// REQ-D-004: 启动 cron 调度
func (s *CronScheduler) Start() {
	s.cron.Start()
	slog.Info("cron scheduler started")
}

// Stop 停止 cron 调度器
// REQ-D-004: 停止 cron 调度
// 规格 §2.8.1: 关机流程单一预算贯穿 — cron stop 受 graceCtx 约束，不无界等待
// ctx 用于限制等待运行中 job 退出的最长时间；超时则记录警告并返回，避免阻塞后续关机步骤
func (s *CronScheduler) Stop(ctx context.Context) {
	stopCtx := s.cron.Stop()
	select {
	case <-stopCtx.Done():
		slog.Info("cron scheduler stopped")
	case <-ctx.Done():
		slog.Warn("cron scheduler stop timed out, some jobs may still be running")
	}
}

// HasJob 检查指定扩展 action 是否有 cron 任务
func (s *CronScheduler) HasJob(extName, actionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := extName + ":" + actionID
	_, ok := s.entries[key]
	return ok
}

// JobCount 返回当前注册的 cron 任务数量
func (s *CronScheduler) JobCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// GetNextRun 返回指定扩展 action 的下次执行时间
// D-05-002 修复：CronEntryInfo.NextRun 需要从 CronScheduler 获取下次执行时间
func (s *CronScheduler) GetNextRun(extName, actionID string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := extName + ":" + actionID
	entryID, ok := s.entries[key]
	if !ok {
		return time.Time{}, false
	}

	for _, entry := range s.cron.Entries() {
		if entry.ID == entryID {
			return entry.Next, true
		}
	}
	return time.Time{}, false
}
