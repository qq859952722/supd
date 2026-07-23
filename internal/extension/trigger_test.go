package extension

import (
	"context"
	"testing"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/watch"
)

// TestOnDemandTrigger_Basic 测试 OnDemandTrigger 基本创建
// REQ-D-004: on_demand 触发器基本功能
func TestOnDemandTrigger_Basic(t *testing.T) {
	dispatcher := NewDispatcher(nil, "/tmp/supd", "/var/log/supd", 1800)
	trigger := NewOnDemandTrigger(dispatcher)

	if trigger == nil {
		t.Fatal("NewOnDemandTrigger returned nil")
	}
	if trigger.dispatcher != dispatcher {
		t.Fatal("dispatcher not set correctly")
	}
}

// TestOnDemandTrigger_ExtensionNotFound 测试触发不存在的扩展
// REQ-D-004: 扩展不存在时应返回错误
func TestOnDemandTrigger_ExtensionNotFound(t *testing.T) {
	dispatcher := NewDispatcher(nil, "/tmp/supd", "/var/log/supd", 1800)
	trigger := NewOnDemandTrigger(dispatcher)

	discovery := &watch.DiscoveryResult{
		GlobalExts: make(map[string]*watch.ExtensionEntry),
		Services:   make(map[string]*watch.ServiceEntry),
	}

	_, err := trigger.Trigger(context.Background(), "nonexistent", "", "test-user", "webui", discovery)
	if err == nil {
		t.Fatal("expected error for nonexistent extension")
	}
}

// TestOnDemandTrigger_OnDemandNotEnabled 测试扩展未启用 on_demand
// REQ-D-004: on_demand trigger 未启用时应返回错误
func TestOnDemandTrigger_OnDemandNotEnabled(t *testing.T) {
	dispatcher := NewDispatcher(nil, "/tmp/supd", "/var/log/supd", 1800)
	trigger := NewOnDemandTrigger(dispatcher)

	enabled := false
	meta := &config.ExtensionMeta{
		Name:    "test-ext",
		Enabled: &enabled,
		Triggers: config.Triggers{
			OnDemand: &enabled, // false
		},
	}

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"test-ext": {
				Name: "test-ext",
				Meta: meta,
			},
		},
		Services: make(map[string]*watch.ServiceEntry),
	}

	_, err := trigger.Trigger(context.Background(), "test-ext", "", "test-user", "webui", discovery)
	if err == nil {
		t.Fatal("expected error for on_demand not enabled")
	}
}

// TestOnDemandTrigger_ActionNotFound 测试 action 不存在
// REQ-D-004: triggers.action 引用不存在的 id → 配置错误
func TestOnDemandTrigger_ActionNotFound(t *testing.T) {
	dispatcher := NewDispatcher(nil, "/tmp/supd", "/var/log/supd", 1800)
	trigger := NewOnDemandTrigger(dispatcher)

	enabled := true
	meta := &config.ExtensionMeta{
		Name:    "test-ext",
		Enabled: &enabled,
		Actions: []config.Action{
			{ID: "run", Label: "Run"},
		},
		Triggers: config.Triggers{
			OnDemand: &enabled,
		},
	}

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"test-ext": {
				Name: "test-ext",
				Meta: meta,
			},
		},
		Services: make(map[string]*watch.ServiceEntry),
	}

	_, err := trigger.Trigger(context.Background(), "test-ext", "nonexistent-action", "test-user", "webui", discovery)
	if err == nil {
		t.Fatal("expected error for action not found")
	}
}

// TestFindExtensionByName 测试按名称查找扩展
// REQ-D-004: 先查全局扩展，再查服务级扩展
func TestFindExtensionByName(t *testing.T) {
	enabled := true
	globalMeta := &config.ExtensionMeta{
		Name:    "global-ext",
		Enabled: &enabled,
	}
	svcMeta := &config.ExtensionMeta{
		Name:    "svc-ext",
		Enabled: &enabled,
	}

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"global-ext": {
				Name: "global-ext",
				Meta: globalMeta,
			},
		},
		Services: map[string]*watch.ServiceEntry{
			"myservice": {
				Name: "myservice",
				Extensions: map[string]*watch.ExtensionEntry{
					"svc-ext": {
						Name:        "svc-ext",
						Meta:        svcMeta,
						ServiceName: "myservice",
					},
				},
			},
		},
	}

	// 测试全局扩展
	ext, svcName, err := findExtensionByName(discovery, "global-ext")
	if err != nil {
		t.Fatalf("expected to find global-ext, got error: %v", err)
	}
	if ext.Name != "global-ext" {
		t.Fatalf("expected global-ext, got %s", ext.Name)
	}
	if svcName != "" {
		t.Fatalf("expected empty svcName for global ext, got %s", svcName)
	}

	// 测试服务级扩展
	ext, svcName, err = findExtensionByName(discovery, "svc-ext")
	if err != nil {
		t.Fatalf("expected to find svc-ext, got error: %v", err)
	}
	if ext.Name != "svc-ext" {
		t.Fatalf("expected svc-ext, got %s", ext.Name)
	}
	if svcName != "myservice" {
		t.Fatalf("expected myservice, got %s", svcName)
	}

	// 测试不存在的扩展
	_, _, err = findExtensionByName(discovery, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent extension")
	}
}

// TestValidateActionID 测试 action ID 验证
// REQ-D-004: triggers.action 引用不存在的 id → 配置错误
func TestValidateActionID(t *testing.T) {
	meta := &config.ExtensionMeta{
		Actions: []config.Action{
			{ID: "run", Label: "Run"},
			{ID: "check", Label: "Check"},
		},
	}

	// 空 actionID → 有效（使用第一个 action）
	if !validateActionID(meta, "") {
		t.Fatal("expected empty actionID to be valid")
	}

	// 存在的 action ID
	if !validateActionID(meta, "run") {
		t.Fatal("expected 'run' actionID to be valid")
	}
	if !validateActionID(meta, "check") {
		t.Fatal("expected 'check' actionID to be valid")
	}

	// 不存在的 action ID
	if validateActionID(meta, "nonexistent") {
		t.Fatal("expected 'nonexistent' actionID to be invalid")
	}

	// 无 actions 的 meta
	emptyMeta := &config.ExtensionMeta{}
	if validateActionID(emptyMeta, "") {
		t.Fatal("expected empty meta with empty actionID to be invalid")
	}
}

// TestCronScheduler_Basic 测试 CronScheduler 基本创建
// REQ-D-004: cron 调度器基本功能
func TestCronScheduler_Basic(t *testing.T) {
	dispatcher := NewDispatcher(nil, "/tmp/supd", "/var/log/supd", 1800)
	scheduler := NewCronScheduler(dispatcher)

	if scheduler == nil {
		t.Fatal("NewCronScheduler returned nil")
	}
	if scheduler.dispatcher != dispatcher {
		t.Fatal("dispatcher not set correctly")
	}
}

// TestCronScheduler_AddAndRemoveJob 测试添加和移除 cron job
// REQ-D-004: on_schedule 的 cron 任务管理
func TestCronScheduler_AddAndRemoveJob(t *testing.T) {
	dispatcher := NewDispatcher(nil, "/tmp/supd", "/var/log/supd", 1800)
	scheduler := NewCronScheduler(dispatcher)

	enabled := true
	meta := &config.ExtensionMeta{
		Name:    "cron-ext",
		Enabled: &enabled,
		Actions: []config.Action{
			{ID: "run", Label: "Run"},
		},
		Triggers: config.Triggers{
			OnSchedule: []config.TriggerSchedule{
				{Cron: "0 * * * *", Action: "run"}, // 每小时
			},
		},
	}

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"cron-ext": {
				Name: "cron-ext",
				Meta: meta,
			},
		},
		Services: make(map[string]*watch.ServiceEntry),
	}

	// 添加 job
	err := scheduler.AddJob("cron-ext", "run", "0 * * * *", nil, discovery)
	if err != nil {
		t.Fatalf("AddJob failed: %v", err)
	}

	if !scheduler.HasJob("cron-ext", "run") {
		t.Fatal("expected job to exist after AddJob")
	}

	if scheduler.JobCount() != 1 {
		t.Fatalf("expected 1 job, got %d", scheduler.JobCount())
	}

	// 移除 job
	scheduler.RemoveJob("cron-ext", "run")

	if scheduler.HasJob("cron-ext", "run") {
		t.Fatal("expected job to not exist after RemoveJob")
	}

	if scheduler.JobCount() != 0 {
		t.Fatalf("expected 0 jobs, got %d", scheduler.JobCount())
	}
}

// TestCronScheduler_InvalidCronExpression 测试无效 cron 表达式
// REQ-D-004: 无效的 cron 表达式应返回错误
func TestCronScheduler_InvalidCronExpression(t *testing.T) {
	dispatcher := NewDispatcher(nil, "/tmp/supd", "/var/log/supd", 1800)
	scheduler := NewCronScheduler(dispatcher)

	discovery := &watch.DiscoveryResult{
		GlobalExts: make(map[string]*watch.ExtensionEntry),
		Services:   make(map[string]*watch.ServiceEntry),
	}

	err := scheduler.AddJob("ext", "run", "invalid-cron", nil, discovery)
	if err == nil {
		t.Fatal("expected error for invalid cron expression")
	}
}

// TestCronScheduler_StartAndStop 测试启动和停止
// REQ-D-004: cron 调度器启停
func TestCronScheduler_StartAndStop(t *testing.T) {
	dispatcher := NewDispatcher(nil, "/tmp/supd", "/var/log/supd", 1800)
	scheduler := NewCronScheduler(dispatcher)

	scheduler.Start()
	scheduler.Stop(context.Background())
	// 如果没有 panic，则测试通过
}

// TestCronTrigger_Basic 测试 CronTrigger 基本创建
// REQ-D-004: cron 触发器基本功能
func TestCronTrigger_Basic(t *testing.T) {
	dispatcher := NewDispatcher(nil, "/tmp/supd", "/var/log/supd", 1800)
	scheduler := NewCronScheduler(dispatcher)
	trigger := NewCronTrigger(scheduler)

	if trigger == nil {
		t.Fatal("NewCronTrigger returned nil")
	}
	if trigger.scheduler != scheduler {
		t.Fatal("scheduler not set correctly")
	}
}

// TestCronTrigger_HandleResult_Success 测试成功结果不触发重试
// REQ-D-004, REQ-F-020: 成功时重置重试计数
func TestCronTrigger_HandleResult_Success(t *testing.T) {
	dispatcher := NewDispatcher(nil, "/tmp/supd", "/var/log/supd", 1800)
	scheduler := NewCronScheduler(dispatcher)
	trigger := NewCronTrigger(scheduler)

	discovery := &watch.DiscoveryResult{
		GlobalExts: make(map[string]*watch.ExtensionEntry),
		Services:   make(map[string]*watch.ServiceEntry),
	}

	// 先设置一些重试次数
	trigger.retryAttempts["test-ext:run"] = 2

	retryConfig := &RetryConfig{MaxRetries: 3, IntervalMinutes: 1}
	result := &RunResult{
		ExtensionName: "test-ext",
		ActionID:      "run",
		State:         TaskSuccess,
	}

	trigger.HandleResult(context.Background(), result, retryConfig, discovery)

	// 成功后应重置重试计数
	if trigger.GetRetryAttempts("test-ext", "run") != 0 {
		t.Fatalf("expected retry attempts to be 0 after success, got %d", trigger.GetRetryAttempts("test-ext", "run"))
	}
}

// TestCronTrigger_HandleResult_FailureNoRetry 测试失败但无重试配置
// REQ-D-004: 无 retry_on_failure 配置时不重试
func TestCronTrigger_HandleResult_FailureNoRetry(t *testing.T) {
	dispatcher := NewDispatcher(nil, "/tmp/supd", "/var/log/supd", 1800)
	scheduler := NewCronScheduler(dispatcher)
	trigger := NewCronTrigger(scheduler)

	discovery := &watch.DiscoveryResult{
		GlobalExts: make(map[string]*watch.ExtensionEntry),
		Services:   make(map[string]*watch.ServiceEntry),
	}

	result := &RunResult{
		ExtensionName: "test-ext",
		ActionID:      "run",
		State:         TaskFailed,
	}

	// 无重试配置
	trigger.HandleResult(context.Background(), result, nil, discovery)

	if trigger.GetRetryAttempts("test-ext", "run") != 0 {
		t.Fatalf("expected 0 retry attempts with nil config, got %d", trigger.GetRetryAttempts("test-ext", "run"))
	}

	// MaxRetries 为 0
	trigger.HandleResult(context.Background(), result, &RetryConfig{MaxRetries: 0}, discovery)

	if trigger.GetRetryAttempts("test-ext", "run") != 0 {
		t.Fatalf("expected 0 retry attempts with MaxRetries=0, got %d", trigger.GetRetryAttempts("test-ext", "run"))
	}
}

// TestCronTrigger_HandleResult_MaxRetriesExhausted 测试重试次数用尽
// REQ-D-004: max_retries 用尽后不再重试
func TestCronTrigger_HandleResult_MaxRetriesExhausted(t *testing.T) {
	dispatcher := NewDispatcher(nil, "/tmp/supd", "/var/log/supd", 1800)
	scheduler := NewCronScheduler(dispatcher)
	trigger := NewCronTrigger(scheduler)

	discovery := &watch.DiscoveryResult{
		GlobalExts: make(map[string]*watch.ExtensionEntry),
		Services:   make(map[string]*watch.ServiceEntry),
	}

	retryConfig := &RetryConfig{MaxRetries: 3, IntervalMinutes: 1}
	result := &RunResult{
		ExtensionName: "test-ext",
		ActionID:      "run",
		State:         TaskFailed,
	}

	// 设置已重试 3 次（达到最大）
	trigger.retryAttempts["test-ext:run"] = 3

	trigger.HandleResult(context.Background(), result, retryConfig, discovery)

	// 重试次数用尽后应删除重试计数
	if trigger.GetRetryAttempts("test-ext", "run") != 0 {
		t.Fatalf("expected retry attempts to be 0 after exhausted, got %d", trigger.GetRetryAttempts("test-ext", "run"))
	}
}

// TestCronTrigger_RegisterSchedule 测试注册 cron 调度
// REQ-D-004: 遍历扩展的 on_schedule triggers，为每个调度注册 cron job
func TestCronTrigger_RegisterSchedule(t *testing.T) {
	dispatcher := NewDispatcher(nil, "/tmp/supd", "/var/log/supd", 1800)
	scheduler := NewCronScheduler(dispatcher)
	trigger := NewCronTrigger(scheduler)

	enabled := true
	extEntry := &watch.ExtensionEntry{
		Name: "cron-ext",
		Meta: &config.ExtensionMeta{
			Name:    "cron-ext",
			Enabled: &enabled,
			Actions: []config.Action{
				{ID: "run", Label: "Run"},
				{ID: "check", Label: "Check"},
			},
			Triggers: config.Triggers{
				OnSchedule: []config.TriggerSchedule{
					{Cron: "0 * * * *", Action: "run"},
					{Cron: "0 0 * * *", Action: "check"},
				},
			},
		},
	}

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"cron-ext": extEntry,
		},
		Services: make(map[string]*watch.ServiceEntry),
	}

	err := trigger.RegisterSchedule(extEntry, discovery)
	if err != nil {
		t.Fatalf("RegisterSchedule failed: %v", err)
	}

	if !scheduler.HasJob("cron-ext", "run") {
		t.Fatal("expected 'run' job to be registered")
	}
	if !scheduler.HasJob("cron-ext", "check") {
		t.Fatal("expected 'check' job to be registered")
	}
	if scheduler.JobCount() != 2 {
		t.Fatalf("expected 2 jobs, got %d", scheduler.JobCount())
	}
}

// TestCronTrigger_RegisterSchedule_Disabled 测试禁用扩展不注册 cron
// REQ-D-004: enabled=false 的扩展不注册调度
func TestCronTrigger_RegisterSchedule_Disabled(t *testing.T) {
	dispatcher := NewDispatcher(nil, "/tmp/supd", "/var/log/supd", 1800)
	scheduler := NewCronScheduler(dispatcher)
	trigger := NewCronTrigger(scheduler)

	enabled := false
	extEntry := &watch.ExtensionEntry{
		Name: "disabled-ext",
		Meta: &config.ExtensionMeta{
			Name:    "disabled-ext",
			Enabled: &enabled,
			Triggers: config.Triggers{
				OnSchedule: []config.TriggerSchedule{
					{Cron: "0 * * * *", Action: "run"},
				},
			},
		},
	}

	discovery := &watch.DiscoveryResult{
		GlobalExts: make(map[string]*watch.ExtensionEntry),
		Services:   make(map[string]*watch.ServiceEntry),
	}

	err := trigger.RegisterSchedule(extEntry, discovery)
	if err != nil {
		t.Fatalf("RegisterSchedule for disabled ext should not error: %v", err)
	}

	if scheduler.JobCount() != 0 {
		t.Fatalf("expected 0 jobs for disabled extension, got %d", scheduler.JobCount())
	}
}

// TestCronTrigger_UnregisterSchedule 测试移除调度
// REQ-D-004: 移除指定扩展的所有 on_schedule cron jobs
func TestCronTrigger_UnregisterSchedule(t *testing.T) {
	dispatcher := NewDispatcher(nil, "/tmp/supd", "/var/log/supd", 1800)
	scheduler := NewCronScheduler(dispatcher)
	trigger := NewCronTrigger(scheduler)

	enabled := true
	extEntry := &watch.ExtensionEntry{
		Name: "cron-ext",
		Meta: &config.ExtensionMeta{
			Name:    "cron-ext",
			Enabled: &enabled,
			Actions: []config.Action{
				{ID: "run", Label: "Run"},
			},
			Triggers: config.Triggers{
				OnSchedule: []config.TriggerSchedule{
					{Cron: "0 * * * *", Action: "run"},
				},
			},
		},
	}

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"cron-ext": extEntry,
		},
		Services: make(map[string]*watch.ServiceEntry),
	}

	// 先注册
	err := trigger.RegisterSchedule(extEntry, discovery)
	if err != nil {
		t.Fatalf("RegisterSchedule failed: %v", err)
	}

	if scheduler.JobCount() != 1 {
		t.Fatalf("expected 1 job, got %d", scheduler.JobCount())
	}

	// 设置重试计数
	trigger.retryAttempts["cron-ext:run"] = 2

	// 再移除
	trigger.UnregisterSchedule(extEntry)

	if scheduler.HasJob("cron-ext", "run") {
		t.Fatal("expected job to be removed after UnregisterSchedule")
	}

	// 重试计数应被清理
	if trigger.GetRetryAttempts("cron-ext", "run") != 0 {
		t.Fatalf("expected retry attempts to be 0 after unregister, got %d", trigger.GetRetryAttempts("cron-ext", "run"))
	}
}

// TestCronTrigger_RegisterSchedule_InvalidAction 测试引用不存在的 action
// REQ-D-004: triggers.action 引用不存在的 id → 配置错误
func TestCronTrigger_RegisterSchedule_InvalidAction(t *testing.T) {
	dispatcher := NewDispatcher(nil, "/tmp/supd", "/var/log/supd", 1800)
	scheduler := NewCronScheduler(dispatcher)
	trigger := NewCronTrigger(scheduler)

	enabled := true
	extEntry := &watch.ExtensionEntry{
		Name: "bad-ext",
		Meta: &config.ExtensionMeta{
			Name:    "bad-ext",
			Enabled: &enabled,
			Actions: []config.Action{
				{ID: "run", Label: "Run"},
			},
			Triggers: config.Triggers{
				OnSchedule: []config.TriggerSchedule{
					{Cron: "0 * * * *", Action: "nonexistent"},
				},
			},
		},
	}

	discovery := &watch.DiscoveryResult{
		GlobalExts: make(map[string]*watch.ExtensionEntry),
		Services:   make(map[string]*watch.ServiceEntry),
	}

	err := trigger.RegisterSchedule(extEntry, discovery)
	if err == nil {
		t.Fatal("expected error for invalid action reference")
	}
}
