package extension

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/supdorg/supd/internal/watch"
)

// TestServiceLifecycleTriggerOnPreStart 测试 service_lifecycle:pre_start 触发
// REQ-D-004: 服务启动前触发 pre_start 扩展
func TestServiceLifecycleTriggerOnPreStart(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	scriptPath := createTestScript(t, tmpDir, "pre_start.sh", "echo pre_start")

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"global-ext": testExtWithServiceLifecycle("global-ext", scriptPath, "pre_start", "init"),
		},
		Services: map[string]*watch.ServiceEntry{
			"my-service": {
				Name: "my-service",
				Extensions: map[string]*watch.ExtensionEntry{
					"svc-ext": testExtWithServiceLifecycle("svc-ext", scriptPath, "pre_start", "setup"),
				},
			},
		},
	}

	trigger := NewServiceLifecycleTrigger(dispatcher, discovery)
	results := trigger.OnPreStart(context.Background(), "my-service")

	if len(results) != 2 {
		t.Fatalf("expected 2 results (1 global + 1 service), got %d", len(results))
	}

	for _, r := range results {
		if r.State != TaskSuccess {
			t.Errorf("expected success for %s, got %s", r.ExtensionName, r.State)
		}
		if r.TriggerType != "service_lifecycle" {
			t.Errorf("expected trigger type service_lifecycle, got %s", r.TriggerType)
		}
	}
}

// TestServiceLifecycleTriggerOnPreStartFiltersByService 测试 service_lifecycle 触发仅匹配指定服务
// REQ-D-004: service_lifecycle 时仅触发指定服务的扩展，不触发其他服务的扩展
func TestServiceLifecycleTriggerOnPreStartFiltersByService(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	scriptPath := createTestScript(t, tmpDir, "pre_start.sh", "echo pre_start")

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"global-ext": testExtWithServiceLifecycle("global-ext", scriptPath, "pre_start", "init"),
		},
		Services: map[string]*watch.ServiceEntry{
			"svc-a": {
				Name: "svc-a",
				Extensions: map[string]*watch.ExtensionEntry{
					"svc-a-ext": testExtWithServiceLifecycle("svc-a-ext", scriptPath, "pre_start", "setup"),
				},
			},
			"svc-b": {
				Name: "svc-b",
				Extensions: map[string]*watch.ExtensionEntry{
					"svc-b-ext": testExtWithServiceLifecycle("svc-b-ext", scriptPath, "pre_start", "setup"),
				},
			},
		},
	}

	trigger := NewServiceLifecycleTrigger(dispatcher, discovery)

	// 触发 svc-a 的 pre_start，应该只匹配 global-ext + svc-a-ext
	results := trigger.OnPreStart(context.Background(), "svc-a")

	if len(results) != 2 {
		t.Fatalf("expected 2 results (1 global + 1 svc-a), got %d", len(results))
	}

	names := make(map[string]bool)
	for _, r := range results {
		names[r.ExtensionName] = true
	}
	if !names["global-ext"] {
		t.Error("expected global-ext to be triggered")
	}
	if !names["svc-a-ext"] {
		t.Error("expected svc-a-ext to be triggered")
	}
	if names["svc-b-ext"] {
		t.Error("svc-b-ext should NOT be triggered for svc-a's pre_start")
	}
}

// TestServiceLifecycleTriggerOnPostReady 测试 service_lifecycle:post_ready 触发
// REQ-D-004: 服务进入 ready 状态后触发 post_ready 扩展
func TestServiceLifecycleTriggerOnPostReady(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	scriptPath := createTestScript(t, tmpDir, "post_ready.sh", "echo post_ready")

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"global-ext": testExtWithServiceLifecycle("global-ext", scriptPath, "post_ready", "notify"),
		},
		Services: map[string]*watch.ServiceEntry{
			"my-service": {
				Name: "my-service",
				Extensions: map[string]*watch.ExtensionEntry{
					"svc-ext": testExtWithServiceLifecycle("svc-ext", scriptPath, "post_ready", "on_ready"),
				},
			},
		},
	}

	trigger := NewServiceLifecycleTrigger(dispatcher, discovery)
	results := trigger.OnPostReady(context.Background(), "my-service", 12345)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	for _, r := range results {
		if r.State != TaskSuccess {
			t.Errorf("expected success for %s, got %s", r.ExtensionName, r.State)
		}
	}
}

// TestServiceLifecycleTriggerOnPreStop 测试 service_lifecycle:pre_stop 触发
// REQ-D-004: 服务停止前触发 pre_stop 扩展
func TestServiceLifecycleTriggerOnPreStop(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	scriptPath := createTestScript(t, tmpDir, "pre_stop.sh", "echo pre_stop")

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"global-ext": testExtWithServiceLifecycle("global-ext", scriptPath, "pre_stop", "cleanup"),
		},
		Services: map[string]*watch.ServiceEntry{
			"my-service": {
				Name: "my-service",
				Extensions: map[string]*watch.ExtensionEntry{
					"svc-ext": testExtWithServiceLifecycle("svc-ext", scriptPath, "pre_stop", "teardown"),
				},
			},
		},
	}

	trigger := NewServiceLifecycleTrigger(dispatcher, discovery)
	results := trigger.OnPreStop(context.Background(), "my-service", 12345)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	for _, r := range results {
		if r.State != TaskSuccess {
			t.Errorf("expected success for %s, got %s", r.ExtensionName, r.State)
		}
	}
}

// TestServiceLifecycleTriggerOnFailure 测试 service_lifecycle:on_failure 触发
// REQ-D-004: 服务失败后触发 on_failure 扩展
func TestServiceLifecycleTriggerOnFailure(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	scriptPath := createTestScript(t, tmpDir, "on_failure.sh", "echo on_failure")

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"alert-ext": testExtWithServiceLifecycle("alert-ext", scriptPath, "on_failure", "alert"),
		},
		Services: map[string]*watch.ServiceEntry{
			"my-service": {
				Name: "my-service",
				Extensions: map[string]*watch.ExtensionEntry{
					"svc-ext": testExtWithServiceLifecycle("svc-ext", scriptPath, "on_failure", "handle"),
				},
			},
		},
	}

	trigger := NewServiceLifecycleTrigger(dispatcher, discovery)
	results := trigger.OnFailure(context.Background(), "my-service", 1, 9, 3)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	for _, r := range results {
		if r.State != TaskSuccess {
			t.Errorf("expected success for %s, got %s", r.ExtensionName, r.State)
		}
	}
}

// TestServiceLifecycleTriggerFailureDoesNotBlock 测试扩展失败不阻止服务
// REQ-D-004, 2.2.11: 扩展失败不阻止服务启停
func TestServiceLifecycleTriggerFailureDoesNotBlock(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	failScript := createTestScript(t, tmpDir, "fail.sh", "exit 1")

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"fail-ext": testExtWithServiceLifecycle("fail-ext", failScript, "pre_start", "init"),
		},
		Services: map[string]*watch.ServiceEntry{},
	}

	trigger := NewServiceLifecycleTrigger(dispatcher, discovery)
	results := trigger.OnPreStart(context.Background(), "my-service")

	// 扩展失败后，仍然返回结果（不阻止调用方）
	if len(results) != 1 {
		t.Fatalf("expected 1 result even on failure, got %d", len(results))
	}

	if results[0].State != TaskFailed {
		t.Errorf("expected failed state, got %s", results[0].State)
	}
	// 关键：返回结果本身不阻止服务，调用方可以选择忽略失败
}

// TestServiceLifecycleTriggerNoMatch 测试无匹配扩展时返回空
// REQ-D-004: 没有扩展匹配当前 phase 时返回空结果
func TestServiceLifecycleTriggerNoMatch(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	scriptPath := createTestScript(t, tmpDir, "pre_start.sh", "echo pre_start")

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"ext1": testExtWithServiceLifecycle("ext1", scriptPath, "pre_start", "init"),
		},
		Services: map[string]*watch.ServiceEntry{},
	}

	trigger := NewServiceLifecycleTrigger(dispatcher, discovery)
	// 触发 post_ready，但 ext1 只匹配 pre_start
	results := trigger.OnPostReady(context.Background(), "my-service", 12345)

	if len(results) != 0 {
		t.Errorf("expected 0 results for no match, got %d", len(results))
	}
}

// TestServiceLifecycleTriggerEnvVars 测试 service_lifecycle 触发时的环境变量注入
// REQ-D-004, 2.2.5: 触发时注入 SUPD_SERVICE/SUPD_SERVICE_PID/SUPD_PHASE 等环境变量
func TestServiceLifecycleTriggerEnvVars(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	// 脚本检查环境变量
	envCheckScript := createTestScript(t, tmpDir, "env_check.sh",
		`test "$SUPD_EVENT" = "service_lifecycle" && \
         test "$SUPD_PHASE" = "pre_start" && \
         test "$SUPD_SERVICE" = "my-service" && \
         test "$SUPD_TRIGGER_USER" = "service_lifecycle"`)

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"env-ext": testExtWithServiceLifecycle("env-ext", envCheckScript, "pre_start", "init"),
		},
		Services: map[string]*watch.ServiceEntry{
			"my-service": {
				Name: "my-service",
				Extensions: map[string]*watch.ExtensionEntry{
					"svc-ext": testExtWithServiceLifecycle("svc-ext", envCheckScript, "pre_start", "setup"),
				},
			},
		},
	}

	trigger := NewServiceLifecycleTrigger(dispatcher, discovery)
	results := trigger.OnPreStart(context.Background(), "my-service")

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	for _, r := range results {
		if r.State != TaskSuccess {
			t.Errorf("expected success for %s (env vars should be set), got %s", r.ExtensionName, r.State)
		}
	}
}

// TestServiceLifecycleTriggerOnFailureEnvVars 测试 on_failure 时的环境变量注入
// REQ-D-004, 2.2.5: on_failure 时注入 SUPD_SERVICE_EXIT_CODE/SUPD_SERVICE_SIGNAL/SUPD_SERVICE_RESTART_COUNT
func TestServiceLifecycleTriggerOnFailureEnvVars(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	// 脚本检查 on_failure 环境变量
	envCheckScript := createTestScript(t, tmpDir, "env_check.sh",
		`test "$SUPD_EVENT" = "service_lifecycle" && \
         test "$SUPD_PHASE" = "on_failure" && \
         test "$SUPD_SERVICE" = "my-service" && \
         test "$SUPD_SERVICE_EXIT_CODE" = "1" && \
         test "$SUPD_SERVICE_SIGNAL" = "9" && \
         test "$SUPD_SERVICE_RESTART_COUNT" = "3"`)

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"alert-ext": testExtWithServiceLifecycle("alert-ext", envCheckScript, "on_failure", "alert"),
		},
		Services: map[string]*watch.ServiceEntry{
			"my-service": {
				Name: "my-service",
				Extensions: map[string]*watch.ExtensionEntry{
					"svc-ext": testExtWithServiceLifecycle("svc-ext", envCheckScript, "on_failure", "handle"),
				},
			},
		},
	}

	trigger := NewServiceLifecycleTrigger(dispatcher, discovery)
	results := trigger.OnFailure(context.Background(), "my-service", 1, 9, 3)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	for _, r := range results {
		if r.State != TaskSuccess {
			t.Errorf("expected success for %s (on_failure env vars should be set), got %s", r.ExtensionName, r.State)
		}
	}
}

// TestSupdLifecycleTriggerOnPreStart 测试 supd_lifecycle:pre_start 触发
// REQ-D-004, 2.8.1: supd 启动第9步触发 pre_start 扩展
func TestSupdLifecycleTriggerOnPreStart(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	scriptPath := createTestScript(t, tmpDir, "supd_pre_start.sh", "echo supd_pre_start")

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"supd-init": testExtWithSupdLifecycle("supd-init", scriptPath, "pre_start", "init"),
		},
		Services: map[string]*watch.ServiceEntry{},
	}

	trigger := NewSupdLifecycleTrigger(dispatcher, discovery)
	results := trigger.OnPreStart(context.Background())

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].State != TaskSuccess {
		t.Errorf("expected success, got %s", results[0].State)
	}
	if results[0].TriggerType != "supd_lifecycle" {
		t.Errorf("expected trigger type supd_lifecycle, got %s", results[0].TriggerType)
	}
}

// TestSupdLifecycleTriggerOnPostReady 测试 supd_lifecycle:post_ready 触发
// REQ-D-004, 2.8.1: 所有 autostart=true 的服务进入终态后触发 post_ready
func TestSupdLifecycleTriggerOnPostReady(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	scriptPath := createTestScript(t, tmpDir, "supd_post_ready.sh", "echo supd_post_ready")

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"supd-ready": testExtWithSupdLifecycle("supd-ready", scriptPath, "post_ready", "on_ready"),
		},
		Services: map[string]*watch.ServiceEntry{},
	}

	trigger := NewSupdLifecycleTrigger(dispatcher, discovery)
	results := trigger.OnPostReady(context.Background())

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].State != TaskSuccess {
		t.Errorf("expected success, got %s", results[0].State)
	}
}

// TestSupdLifecycleTriggerOnPreShutdown 测试 supd_lifecycle:pre_shutdown 触发
// REQ-D-004, 2.8.1: supd 退出第1步触发 pre_shutdown
func TestSupdLifecycleTriggerOnPreShutdown(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	scriptPath := createTestScript(t, tmpDir, "supd_pre_shutdown.sh", "echo supd_pre_shutdown")

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"supd-shutdown": testExtWithSupdLifecycle("supd-shutdown", scriptPath, "pre_shutdown", "cleanup"),
		},
		Services: map[string]*watch.ServiceEntry{},
	}

	trigger := NewSupdLifecycleTrigger(dispatcher, discovery)
	results := trigger.OnPreShutdown(context.Background())

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].State != TaskSuccess {
		t.Errorf("expected success, got %s", results[0].State)
	}
}

// TestSupdLifecycleTriggerNoMatch 测试 supd_lifecycle 无匹配扩展时返回空
// REQ-D-004: 没有扩展匹配当前 phase 时返回空结果
func TestSupdLifecycleTriggerNoMatch(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	scriptPath := createTestScript(t, tmpDir, "pre_start.sh", "echo pre_start")

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"ext1": testExtWithSupdLifecycle("ext1", scriptPath, "pre_start", "init"),
		},
		Services: map[string]*watch.ServiceEntry{},
	}

	trigger := NewSupdLifecycleTrigger(dispatcher, discovery)
	// 触发 post_ready，但 ext1 只匹配 pre_start
	results := trigger.OnPostReady(context.Background())

	if len(results) != 0 {
		t.Errorf("expected 0 results for no match, got %d", len(results))
	}
}

// TestSupdLifecycleTriggerEnvVars 测试 supd_lifecycle 触发时的环境变量注入
// REQ-D-004, 2.2.5: supd_lifecycle 时注入 SUPD_EVENT/SUPD_PHASE
func TestSupdLifecycleTriggerEnvVars(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	envCheckScript := createTestScript(t, tmpDir, "env_check.sh",
		`test "$SUPD_EVENT" = "supd_lifecycle" && \
         test "$SUPD_PHASE" = "pre_shutdown" && \
         test "$SUPD_TRIGGER_USER" = "supd_lifecycle"`)

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"shutdown-ext": testExtWithSupdLifecycle("shutdown-ext", envCheckScript, "pre_shutdown", "cleanup"),
		},
		Services: map[string]*watch.ServiceEntry{},
	}

	trigger := NewSupdLifecycleTrigger(dispatcher, discovery)
	results := trigger.OnPreShutdown(context.Background())

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].State != TaskSuccess {
		t.Errorf("expected success (env vars should be set), got %s", results[0].State)
	}
}

// TestSupdLifecycleTriggerAllPhases 测试 supd_lifecycle 所有 3 个 phase
// REQ-D-004, 2.2.3: supd_lifecycle 有 3 个 phase：pre_start/post_ready/pre_shutdown
func TestSupdLifecycleTriggerAllPhases(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	scriptPath := createTestScript(t, tmpDir, "all_phases.sh", "echo ok")

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"pre-start-ext":    testExtWithSupdLifecycle("pre-start-ext", scriptPath, "pre_start", "init"),
			"post-ready-ext":   testExtWithSupdLifecycle("post-ready-ext", scriptPath, "post_ready", "on_ready"),
			"pre-shutdown-ext": testExtWithSupdLifecycle("pre-shutdown-ext", scriptPath, "pre_shutdown", "cleanup"),
		},
		Services: map[string]*watch.ServiceEntry{},
	}

	trigger := NewSupdLifecycleTrigger(dispatcher, discovery)

	// pre_start 只匹配 pre-start-ext
	results := trigger.OnPreStart(context.Background())
	if len(results) != 1 || results[0].ExtensionName != "pre-start-ext" {
		t.Errorf("pre_start: expected 1 result from pre-start-ext, got %v", results)
	}

	// post_ready 只匹配 post-ready-ext
	results = trigger.OnPostReady(context.Background())
	if len(results) != 1 || results[0].ExtensionName != "post-ready-ext" {
		t.Errorf("post_ready: expected 1 result from post-ready-ext, got %v", results)
	}

	// pre_shutdown 只匹配 pre-shutdown-ext
	results = trigger.OnPreShutdown(context.Background())
	if len(results) != 1 || results[0].ExtensionName != "pre-shutdown-ext" {
		t.Errorf("pre_shutdown: expected 1 result from pre-shutdown-ext, got %v", results)
	}
}

// TestServiceLifecycleTriggerAllPhases 测试 service_lifecycle 所有 4 个 phase
// REQ-D-004, 2.2.3: service_lifecycle 有 4 个 phase：pre_start/post_ready/on_failure/pre_stop
func TestServiceLifecycleTriggerAllPhases(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	scriptPath := createTestScript(t, tmpDir, "all_phases.sh", "echo ok")

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"pre-start-ext":  testExtWithServiceLifecycle("pre-start-ext", scriptPath, "pre_start", "init"),
			"post-ready-ext": testExtWithServiceLifecycle("post-ready-ext", scriptPath, "post_ready", "notify"),
			"on-failure-ext": testExtWithServiceLifecycle("on-failure-ext", scriptPath, "on_failure", "alert"),
			"pre-stop-ext":   testExtWithServiceLifecycle("pre-stop-ext", scriptPath, "pre_stop", "cleanup"),
		},
		Services: map[string]*watch.ServiceEntry{},
	}

	trigger := NewServiceLifecycleTrigger(dispatcher, discovery)

	// pre_start 只匹配 pre-start-ext
	results := trigger.OnPreStart(context.Background(), "svc")
	if len(results) != 1 || results[0].ExtensionName != "pre-start-ext" {
		t.Errorf("pre_start: expected 1 result from pre-start-ext, got %v", results)
	}

	// post_ready 只匹配 post-ready-ext
	results = trigger.OnPostReady(context.Background(), "svc", 123)
	if len(results) != 1 || results[0].ExtensionName != "post-ready-ext" {
		t.Errorf("post_ready: expected 1 result from post-ready-ext, got %v", results)
	}

	// on_failure 只匹配 on-failure-ext
	results = trigger.OnFailure(context.Background(), "svc", 1, 9, 2)
	if len(results) != 1 || results[0].ExtensionName != "on-failure-ext" {
		t.Errorf("on_failure: expected 1 result from on-failure-ext, got %v", results)
	}

	// pre_stop 只匹配 pre-stop-ext
	results = trigger.OnPreStop(context.Background(), "svc", 123)
	if len(results) != 1 || results[0].ExtensionName != "pre-stop-ext" {
		t.Errorf("pre_stop: expected 1 result from pre-stop-ext, got %v", results)
	}
}

// TestServiceLifecycleTriggerPreStartPIDIsZero 测试 pre_start 时 PID 为 0
// REQ-D-004, 2.2.5: pre_start 时 SUPD_SERVICE_PID 为空字符串（服务进程尚未启动）
func TestServiceLifecycleTriggerPreStartPIDIsZero(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	// 脚本检查 PID 应为空字符串（规格 §2.2.5: pre_start 时 SUPD_SERVICE_PID 为空）
	envCheckScript := createTestScript(t, tmpDir, "pid_check.sh",
		`test "$SUPD_PHASE" = "pre_start" && test -z "$SUPD_SERVICE_PID"`)

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"pid-ext": testExtWithServiceLifecycle("pid-ext", envCheckScript, "pre_start", "init"),
		},
		Services: map[string]*watch.ServiceEntry{},
	}

	trigger := NewServiceLifecycleTrigger(dispatcher, discovery)
	results := trigger.OnPreStart(context.Background(), "my-service")

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].State != TaskSuccess {
		t.Errorf("expected success, got %s", results[0].State)
	}
}

// TestServiceLifecycleTriggerPostReadyPID 测试 post_ready 时 PID 为实际值
// REQ-D-004, 2.2.5: post_ready 时 SUPD_SERVICE_PID 为服务进程的实际 PID
func TestServiceLifecycleTriggerPostReadyPID(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	envCheckScript := createTestScript(t, tmpDir, "pid_check.sh",
		`test "$SUPD_PHASE" = "post_ready" && test "$SUPD_SERVICE_PID" = "99999"`)

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"pid-ext": testExtWithServiceLifecycle("pid-ext", envCheckScript, "post_ready", "notify"),
		},
		Services: map[string]*watch.ServiceEntry{},
	}

	trigger := NewServiceLifecycleTrigger(dispatcher, discovery)
	results := trigger.OnPostReady(context.Background(), "my-service", 99999)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].State != TaskSuccess {
		t.Errorf("expected success, got %s", results[0].State)
	}
}

// TestServiceLifecycleTriggerContextCancel 测试 lifecycle 触发支持 context 取消
// REQ-D-004: 触发操作支持 context 取消
func TestServiceLifecycleTriggerContextCancel(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	scriptPath := createTestScript(t, tmpDir, "long.sh", "sleep 60")

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"long-ext": testExtWithServiceLifecycle("long-ext", scriptPath, "pre_start", "init"),
		},
		Services: map[string]*watch.ServiceEntry{},
	}

	trigger := NewServiceLifecycleTrigger(dispatcher, discovery)

	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan []*RunResult, 1)
	go func() {
		resultCh <- trigger.OnPreStart(ctx, "my-service")
	}()

	// 等待一小段时间后取消 context
	cancel()

	select {
	case results := <-resultCh:
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		// 任务应被取消
		if results[0].State != TaskCanceled && results[0].State != TaskTimeout {
			t.Errorf("expected canceled or timeout, got %s", results[0].State)
		}
	case <-func() chan struct{} {
		ch := make(chan struct{})
		go func() {
			// 等待较长时间但测试不应该无限等待
			<-resultCh
			ch <- struct{}{}
		}()
		return ch
	}():
	}
}

// TestLifecycleTriggerWriteWorkDir 测试 lifecycle 触发的扩展写入工作目录
// REQ-D-004: 触发时扩展写入 marker 文件验证工作目录正确
func TestLifecycleTriggerWriteWorkDir(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	markerFile := filepath.Join(tmpDir, "marker.txt")
	scriptPath := createTestScript(t, tmpDir, "write_marker.sh",
		"echo marked >> "+markerFile)

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"marker-ext": testExtWithSupdLifecycle("marker-ext", scriptPath, "pre_start", "init"),
		},
		Services: map[string]*watch.ServiceEntry{},
	}

	trigger := NewSupdLifecycleTrigger(dispatcher, discovery)
	results := trigger.OnPreStart(context.Background())

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].State != TaskSuccess {
		t.Errorf("expected success, got %s", results[0].State)
	}

	// 验证 marker 文件被写入
	data, err := os.ReadFile(markerFile)
	if err != nil {
		t.Fatalf("failed to read marker file: %v", err)
	}
	if string(data) != "marked\n" {
		t.Errorf("expected marker content 'marked\\n', got %q", string(data))
	}
}
