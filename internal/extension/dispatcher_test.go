package extension

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/watch"
)

// helper: 创建启用的 bool 指针
func boolPtr(v bool) *bool {
	return &v
}

// helper: 创建带 on_demand 触发器的扩展
func testExtWithOnDemand(name, entry string) *watch.ExtensionEntry {
	configPath := filepath.Join(filepath.Dir(entry), "meta.yaml")
	return &watch.ExtensionEntry{
		Name:        name,
		ConfigPath:  configPath,
		Meta: &config.ExtensionMeta{
			Name:           name,
			Enabled:        boolPtr(true),
			Entry:          entry,
			TimeoutSeconds: 600, // REQ-2.2.3: 默认600秒
			Triggers: config.Triggers{
				OnDemand: boolPtr(true),
			},
			Actions: []config.Action{
				{ID: "run", Label: "Run", ButtonStyle: "default"},
			},
		},
	}
}

// helper: 创建带 service_lifecycle 触发器的扩展
func testExtWithServiceLifecycle(name, entry, event, action string) *watch.ExtensionEntry {
	configPath := filepath.Join(filepath.Dir(entry), "meta.yaml")
	return &watch.ExtensionEntry{
		Name:        name,
		ConfigPath:  configPath,
		Meta: &config.ExtensionMeta{
			Name:           name,
			Enabled:        boolPtr(true),
			Entry:          entry,
			TimeoutSeconds: 600, // REQ-2.2.3: 默认600秒
			Triggers: config.Triggers{
				ServiceLifecycle: []config.TriggerServiceLifecycle{
					{Event: event, Action: action},
				},
			},
			Actions: []config.Action{
				{ID: "run", Label: "Run", ButtonStyle: "default"},
				{ID: action, Label: action, ButtonStyle: "default"},
			},
		},
	}
}

// helper: 创建带 supd_lifecycle 触发器的扩展
func testExtWithSupdLifecycle(name, entry, event, action string) *watch.ExtensionEntry {
	configPath := filepath.Join(filepath.Dir(entry), "meta.yaml")
	return &watch.ExtensionEntry{
		Name:        name,
		ConfigPath:  configPath,
		Meta: &config.ExtensionMeta{
			Name:           name,
			Enabled:        boolPtr(true),
			Entry:          entry,
			TimeoutSeconds: 600, // REQ-2.2.3: 默认600秒
			Triggers: config.Triggers{
				SupdLifecycle: []config.TriggerSupdLifecycle{
					{Event: event, Action: action},
				},
			},
			Actions: []config.Action{
				{ID: "run", Label: "Run", ButtonStyle: "default"},
				{ID: action, Label: action, ButtonStyle: "default"},
			},
		},
	}
}

// helper: 创建带 on_schedule 触发器的扩展
func testExtWithSchedule(name, entry, cron, action string) *watch.ExtensionEntry {
	configPath := filepath.Join(filepath.Dir(entry), "meta.yaml")
	return &watch.ExtensionEntry{
		Name:        name,
		ConfigPath:  configPath,
		Meta: &config.ExtensionMeta{
			Name:           name,
			Enabled:        boolPtr(true),
			Entry:          entry,
			TimeoutSeconds: 600, // REQ-2.2.3: 默认600秒
			Triggers: config.Triggers{
				OnSchedule: []config.TriggerSchedule{
					{Cron: cron, Action: action},
				},
			},
			Actions: []config.Action{
				{ID: "run", Label: "Run", ButtonStyle: "default"},
				{ID: action, Label: action, ButtonStyle: "default"},
			},
		},
	}
}

// TestBuildWorkDirGlobal 测试全局扩展工作目录构造
// 工作目录为扩展自身目录（meta.yaml 所在目录）
func TestBuildWorkDirGlobal(t *testing.T) {
	tmpDir := t.TempDir()
	extDir := filepath.Join(tmpDir, "extensions", "my-ext")
	metaPath := filepath.Join(extDir, "meta.yaml")
	if err := os.MkdirAll(extDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metaPath, []byte("name: my-ext"), 0644); err != nil {
		t.Fatal(err)
	}

	extEntry := &watch.ExtensionEntry{
		Name:        "my-ext",
		ConfigPath:  metaPath,
		ServiceName: "",
	}
	workDir := buildWorkDir(tmpDir, extEntry)

	// 工作目录应为扩展目录本身
	if workDir != extDir {
		t.Errorf("expected workDir %s, got %s", extDir, workDir)
	}

	// 验证 script_tmp 临时目录已创建
	scriptTmp := filepath.Join(tmpDir, "script_tmp", "global+my-ext")
	if _, err := os.Stat(scriptTmp); os.IsNotExist(err) {
		t.Errorf("script_tmp dir was not created: %s", scriptTmp)
	}
}

// TestBuildWorkDirService 测试服务级扩展工作目录构造
// 工作目录为扩展自身目录（meta.yaml 所在目录）
func TestBuildWorkDirService(t *testing.T) {
	tmpDir := t.TempDir()
	extDir := filepath.Join(tmpDir, "services", "my-service", "extensions", "my-ext")
	metaPath := filepath.Join(extDir, "meta.yaml")
	if err := os.MkdirAll(extDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metaPath, []byte("name: my-ext"), 0644); err != nil {
		t.Fatal(err)
	}

	extEntry := &watch.ExtensionEntry{
		Name:        "my-ext",
		ConfigPath:  metaPath,
		ServiceName: "my-service",
	}
	workDir := buildWorkDir(tmpDir, extEntry)

	// 工作目录应为扩展目录本身
	if workDir != extDir {
		t.Errorf("expected workDir %s, got %s", extDir, workDir)
	}

	// 验证 script_tmp 临时目录已创建
	scriptTmp := filepath.Join(tmpDir, "script_tmp", "my-service+my-ext")
	if _, err := os.Stat(scriptTmp); os.IsNotExist(err) {
		t.Errorf("script_tmp dir was not created: %s", scriptTmp)
	}
}

// TestBuildWorkDirNoAutoCleanup 测试程序不自动清理 script_tmp
// 程序不自动清理 script_tmp/，扩展运行前程序不清空该目录
func TestBuildWorkDirNoAutoCleanup(t *testing.T) {
	tmpDir := t.TempDir()
	extDir := filepath.Join(tmpDir, "services", "svc1", "extensions", "ext1")
	metaPath := filepath.Join(extDir, "meta.yaml")
	if err := os.MkdirAll(extDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metaPath, []byte("name: ext1"), 0644); err != nil {
		t.Fatal(err)
	}

	extEntry := &watch.ExtensionEntry{
		Name:        "ext1",
		ConfigPath:  metaPath,
		ServiceName: "svc1",
	}
	workDir := buildWorkDir(tmpDir, extEntry)

	// 在 script_tmp 中创建临时文件
	scriptTmp := filepath.Join(tmpDir, "script_tmp", "svc1+ext1")
	tmpFile := filepath.Join(scriptTmp, "temp.txt")
	if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	// 再次调用 buildWorkDir，之前的文件应该仍然存在
	workDir2 := buildWorkDir(tmpDir, extEntry)
	if workDir2 != workDir {
		t.Errorf("expected same workDir, got %s vs %s", workDir, workDir2)
	}

	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("temp file should still exist after rebuild, but got error: %v", err)
	}
	if string(data) != "test" {
		t.Errorf("temp file content should be preserved, got %s", string(data))
	}
}

// TestFindMatchingExtensionsOnDemand 测试 on_demand 触发匹配
// REQ-F-022: 遍历 DiscoveryResult 中的扩展匹配 EventType
func TestFindMatchingExtensionsOnDemand(t *testing.T) {
	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"alpha": testExtWithOnDemand("alpha", "/bin/true"),
			"beta":  testExtWithOnDemand("beta", "/bin/true"),
		},
		Services: map[string]*watch.ServiceEntry{
			"svc1": {
				Name: "svc1",
				Extensions: map[string]*watch.ExtensionEntry{
					"svc-ext": testExtWithOnDemand("svc-ext", "/bin/true"),
				},
			},
		},
	}

	req := DispatchRequest{
		EventType:   "on_demand",
		Discovery:   discovery,
		TriggerUser: "test-user",
	}

	matched := findMatchingExtensions(req)
	if len(matched) != 3 {
		t.Fatalf("expected 3 matched extensions, got %d", len(matched))
	}

	// 验证全局扩展匹配
	names := make(map[string]bool)
	for _, m := range matched {
		names[m.extEntry.Name] = true
	}
	if !names["alpha"] || !names["beta"] || !names["svc-ext"] {
		t.Errorf("expected alpha, beta, svc-ext matched, got %v", names)
	}
}

// TestFindMatchingExtensionsServiceLifecycle 测试 service_lifecycle 触发匹配
// REQ-F-022: 匹配 EventType+Phase
func TestFindMatchingExtensionsServiceLifecycle(t *testing.T) {
	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"global-ext": testExtWithServiceLifecycle("global-ext", "/bin/true", "pre_start", "init"),
		},
		Services: map[string]*watch.ServiceEntry{
			"svc1": {
				Name: "svc1",
				Extensions: map[string]*watch.ExtensionEntry{
					"svc-ext": testExtWithServiceLifecycle("svc-ext", "/bin/true", "pre_start", "setup"),
				},
			},
		},
	}

	req := DispatchRequest{
		EventType:   "service_lifecycle",
		Phase:       "pre_start",
		Discovery:   discovery,
		TriggerUser: "system",
	}

	matched := findMatchingExtensions(req)
	if len(matched) != 2 {
		t.Fatalf("expected 2 matched extensions, got %d", len(matched))
	}
}

// TestFindMatchingExtensionsNoMatch 测试无匹配扩展时返回空
// REQ-F-022: 无匹配扩展时返回空
func TestFindMatchingExtensionsNoMatch(t *testing.T) {
	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"ext1": testExtWithServiceLifecycle("ext1", "/bin/true", "pre_start", "init"),
		},
		Services: map[string]*watch.ServiceEntry{},
	}

	req := DispatchRequest{
		EventType:   "service_lifecycle",
		Phase:       "post_ready", // ext1 只匹配 pre_start
		Discovery:   discovery,
		TriggerUser: "system",
	}

	matched := findMatchingExtensions(req)
	if len(matched) != 0 {
		t.Errorf("expected 0 matched extensions, got %d", len(matched))
	}
}

// TestFindMatchingExtensionsDisabled 测试禁用的扩展不匹配
// REQ-F-022: Enabled 为 false 的扩展不参与匹配
func TestFindMatchingExtensionsDisabled(t *testing.T) {
	ext := testExtWithOnDemand("disabled-ext", "/bin/true")
	ext.Meta.Enabled = boolPtr(false)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"disabled-ext": ext,
		},
		Services: map[string]*watch.ServiceEntry{},
	}

	req := DispatchRequest{
		EventType:   "on_demand",
		Discovery:   discovery,
		TriggerUser: "test-user",
	}

	matched := findMatchingExtensions(req)
	if len(matched) != 0 {
		t.Errorf("expected 0 matched for disabled extension, got %d", len(matched))
	}
}

// TestDispatchAlphabeticalOrder 测试按字母序串行执行
// REQ-F-022: 同类型内部按目录名字母序串行执行
func TestDispatchAlphabeticalOrder(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	// 创建记录执行顺序的脚本
	markerFile := filepath.Join(tmpDir, "order.txt")
	createTestScript(t, tmpDir, "alpha.sh", "echo alpha >> "+markerFile)
	createTestScript(t, tmpDir, "beta.sh", "echo beta >> "+markerFile)
	createTestScript(t, tmpDir, "gamma.sh", "echo gamma >> "+markerFile)

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"gamma": testExtWithOnDemand("gamma", filepath.Join(tmpDir, "gamma.sh")),
			"alpha": testExtWithOnDemand("alpha", filepath.Join(tmpDir, "alpha.sh")),
			"beta":  testExtWithOnDemand("beta", filepath.Join(tmpDir, "beta.sh")),
		},
		Services: map[string]*watch.ServiceEntry{},
	}

	req := DispatchRequest{
		EventType:   "on_demand",
		Discovery:   discovery,
		TriggerUser: "test-user",
	}

	results := dispatcher.Dispatch(context.Background(), req)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// 读取执行顺序
	data, err := os.ReadFile(markerFile)
	if err != nil {
		t.Fatalf("failed to read marker file: %v", err)
	}

	content := string(data)
	// 验证字母序：alpha 在 beta 之前，beta 在 gamma 之前
	expected := "alpha\nbeta\ngamma\n"
	if content != expected {
		t.Errorf("expected execution order:\n%s\ngot:\n%s", expected, content)
	}
}

// TestDispatchGlobalBeforeService 测试先全局后服务级
// REQ-F-022: 同一服务内先串行执行全局扩展，再串行执行服务级扩展
func TestDispatchGlobalBeforeService(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	markerFile := filepath.Join(tmpDir, "order.txt")
	createTestScript(t, tmpDir, "global-ext.sh", "echo global >> "+markerFile)
	createTestScript(t, tmpDir, "svc-ext.sh", "echo service >> "+markerFile)

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	// 全局扩展匹配 service_lifecycle 的 pre_start
	globalExt := testExtWithServiceLifecycle("global-ext", filepath.Join(tmpDir, "global-ext.sh"), "pre_start", "init")
	svcExt := testExtWithServiceLifecycle("svc-ext", filepath.Join(tmpDir, "svc-ext.sh"), "pre_start", "setup")

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"global-ext": globalExt,
		},
		Services: map[string]*watch.ServiceEntry{
			"my-service": {
				Name:       "my-service",
				Extensions: map[string]*watch.ExtensionEntry{"svc-ext": svcExt},
			},
		},
	}

	req := DispatchRequest{
		EventType:   "service_lifecycle",
		Phase:       "pre_start",
		Discovery:   discovery,
		TriggerUser: "system",
	}

	results := dispatcher.Dispatch(context.Background(), req)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// 读取执行顺序
	data, err := os.ReadFile(markerFile)
	if err != nil {
		t.Fatalf("failed to read marker file: %v", err)
	}

	content := string(data)
	expected := "global\nservice\n"
	if content != expected {
		t.Errorf("expected global before service:\n%s\ngot:\n%s", expected, content)
	}
}

// TestDispatchDifferentServicesParallel 测试不同服务可并行
// REQ-F-022: 以服务为粒度独立执行，不同服务的扩展可并行
func TestDispatchDifferentServicesParallel(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	// 创建带延时的脚本，记录开始时间
	svc1Start := filepath.Join(tmpDir, "svc1_start.txt")
	svc2Start := filepath.Join(tmpDir, "svc2_start.txt")
	createTestScript(t, tmpDir, "svc1-ext.sh", "date +%s%N > "+svc1Start+"\nsleep 0.2")
	createTestScript(t, tmpDir, "svc2-ext.sh", "date +%s%N > "+svc2Start+"\nsleep 0.2")

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{},
		Services: map[string]*watch.ServiceEntry{
			"svc1": {
				Name: "svc1",
				Extensions: map[string]*watch.ExtensionEntry{
					"svc1-ext": testExtWithOnDemand("svc1-ext", filepath.Join(tmpDir, "svc1-ext.sh")),
				},
			},
			"svc2": {
				Name: "svc2",
				Extensions: map[string]*watch.ExtensionEntry{
					"svc2-ext": testExtWithOnDemand("svc2-ext", filepath.Join(tmpDir, "svc2-ext.sh")),
				},
			},
		},
	}

	req := DispatchRequest{
		EventType:   "on_demand",
		Discovery:   discovery,
		TriggerUser: "test-user",
	}

	start := time.Now()
	results := dispatcher.Dispatch(context.Background(), req)
	elapsed := time.Since(start)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// 如果串行执行需要约 400ms+，并行只需约 200ms+
	// 允许一定误差，如果执行时间 > 350ms 则可能是串行的
	if elapsed > 350*time.Millisecond {
		t.Errorf("services may not be running in parallel, elapsed: %v", elapsed)
	}
}

// TestDispatchFailureDoesNotBlockNext 测试前失败不影响后执行
// REQ-F-022: 前一个失败不影响后一个执行
func TestDispatchFailureDoesNotBlockNext(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	// 第一个脚本会失败，第二个会成功
	createTestScript(t, tmpDir, "fail.sh", "exit 1")
	createTestScript(t, tmpDir, "success.sh", "echo ok")

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"alpha-fail":    testExtWithOnDemand("alpha-fail", filepath.Join(tmpDir, "fail.sh")),
			"beta-success":  testExtWithOnDemand("beta-success", filepath.Join(tmpDir, "success.sh")),
		},
		Services: map[string]*watch.ServiceEntry{},
	}

	req := DispatchRequest{
		EventType:   "on_demand",
		Discovery:   discovery,
		TriggerUser: "test-user",
	}

	results := dispatcher.Dispatch(context.Background(), req)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// 验证第一个失败，第二个成功
	// 按字母序：alpha-fail 先执行
	if results[0].ExtensionName != "alpha-fail" {
		t.Errorf("expected first result from alpha-fail, got %s", results[0].ExtensionName)
	}
	if results[0].State != TaskFailed {
		t.Errorf("expected alpha-fail to be failed, got %s", results[0].State)
	}

	if results[1].ExtensionName != "beta-success" {
		t.Errorf("expected second result from beta-success, got %s", results[1].ExtensionName)
	}
	if results[1].State != TaskSuccess {
		t.Errorf("expected beta-success to be success, got %s", results[1].State)
	}
}

// TestDispatchNoMatchReturnsEmpty 测试无匹配扩展时返回空
// REQ-F-022: 无匹配扩展时返回空
func TestDispatchNoMatchReturnsEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"ext1": testExtWithServiceLifecycle("ext1", "/bin/true", "pre_start", "init"),
		},
		Services: map[string]*watch.ServiceEntry{},
	}

	req := DispatchRequest{
		EventType:   "on_demand", // ext1 没有 on_demand 触发器
		Discovery:   discovery,
		TriggerUser: "test-user",
	}

	results := dispatcher.Dispatch(context.Background(), req)
	if len(results) != 0 {
		t.Errorf("expected 0 results for no match, got %d", len(results))
	}
}

// TestDispatchWorkDirCreated 测试工作目录创建
// REQ-F-022: 执行扩展时工作目录自动创建
func TestDispatchWorkDirCreated(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	// 脚本输出工作目录
	scriptContent := "pwd"
	scriptPath := createTestScript(t, tmpDir, "workdir_test.sh", scriptContent)

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"my-ext": testExtWithOnDemand("my-ext", scriptPath),
		},
		Services: map[string]*watch.ServiceEntry{},
	}

	req := DispatchRequest{
		EventType:   "on_demand",
		Discovery:   discovery,
		TriggerUser: "test-user",
	}

	results := dispatcher.Dispatch(context.Background(), req)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].State != TaskSuccess {
		t.Errorf("expected success, got %s", results[0].State)
	}

	// 验证 script_tmp 临时目录已创建（工作目录现在为扩展目录本身）
	expectedScriptTmp := filepath.Join(tmpDir, "script_tmp", "global+my-ext")
	if _, err := os.Stat(expectedScriptTmp); os.IsNotExist(err) {
		t.Errorf("script_tmp dir was not created: %s", expectedScriptTmp)
	}
}

// TestDispatchWorkDirForServiceExtension 测试服务级扩展工作目录
func TestDispatchWorkDirForServiceExtension(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	scriptPath := createTestScript(t, tmpDir, "svc_ext.sh", "echo ok")

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	svcExt := testExtWithOnDemand("my-ext", scriptPath)
	svcExt.ServiceName = "my-service" // 服务级扩展需设置 ServiceName

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{},
		Services: map[string]*watch.ServiceEntry{
			"my-service": {
				Name: "my-service",
				Extensions: map[string]*watch.ExtensionEntry{
					"my-ext": svcExt,
				},
			},
		},
	}

	req := DispatchRequest{
		EventType:   "on_demand",
		Discovery:   discovery,
		TriggerUser: "test-user",
	}

	results := dispatcher.Dispatch(context.Background(), req)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].State != TaskSuccess {
		t.Errorf("expected success, got %s", results[0].State)
	}

	// 验证 script_tmp 临时目录已创建（工作目录现在为扩展目录本身）
	expectedScriptTmp := filepath.Join(tmpDir, "script_tmp", "my-service+my-ext")
	if _, err := os.Stat(expectedScriptTmp); os.IsNotExist(err) {
		t.Errorf("service extension script_tmp dir was not created: %s", expectedScriptTmp)
	}
}

// TestDispatchSupdLifecycle 测试 supd_lifecycle 触发
// REQ-F-022: supd_lifecycle 事件匹配
func TestDispatchSupdLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	scriptPath := createTestScript(t, tmpDir, "supd_ext.sh", "echo supd_ready")

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"supd-ext": testExtWithSupdLifecycle("supd-ext", scriptPath, "post_ready", "on_ready"),
		},
		Services: map[string]*watch.ServiceEntry{},
	}

	req := DispatchRequest{
		EventType:   "supd_lifecycle",
		Phase:       "post_ready",
		Discovery:   discovery,
		TriggerUser: "system",
	}

	results := dispatcher.Dispatch(context.Background(), req)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].State != TaskSuccess {
		t.Errorf("expected success, got %s", results[0].State)
	}
}

// TestDispatchSchedule 测试 on_schedule 触发
// REQ-F-022: on_schedule 事件匹配
func TestDispatchSchedule(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	scriptPath := createTestScript(t, tmpDir, "schedule_ext.sh", "echo scheduled")

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"cron-ext": testExtWithSchedule("cron-ext", scriptPath, "*/5 * * * *", "check"),
		},
		Services: map[string]*watch.ServiceEntry{},
	}

	req := DispatchRequest{
		EventType:   "on_schedule",
		Discovery:   discovery,
		TriggerUser: "scheduler",
	}

	results := dispatcher.Dispatch(context.Background(), req)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].State != TaskSuccess {
		t.Errorf("expected success, got %s", results[0].State)
	}
}

// TestDispatchMixedGlobalAndServiceOrder 测试全局+服务级混合排序
// REQ-F-022: 同一服务内先全局扩展（字母序），后服务级扩展（字母序）
func TestDispatchMixedGlobalAndServiceOrder(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	markerFile := filepath.Join(tmpDir, "order.txt")
	createTestScript(t, tmpDir, "g1.sh", "echo g1 >> "+markerFile)
	createTestScript(t, tmpDir, "g2.sh", "echo g2 >> "+markerFile)
	createTestScript(t, tmpDir, "s1.sh", "echo s1 >> "+markerFile)
	createTestScript(t, tmpDir, "s2.sh", "echo s2 >> "+markerFile)

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	// 故意让 g2 字母序在 g1 之后，验证排序
	// 全局扩展：z-global（字母序靠后），a-global（字母序靠前）
	// 服务级扩展：z-svc（字母序靠后），a-svc（字母序靠前）
	zGlobal := testExtWithServiceLifecycle("z-global", filepath.Join(tmpDir, "g1.sh"), "pre_start", "init")
	aGlobal := testExtWithServiceLifecycle("a-global", filepath.Join(tmpDir, "g2.sh"), "pre_start", "init")
	zSvc := testExtWithServiceLifecycle("z-svc", filepath.Join(tmpDir, "s1.sh"), "pre_start", "setup")
	aSvc := testExtWithServiceLifecycle("a-svc", filepath.Join(tmpDir, "s2.sh"), "pre_start", "setup")

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"z-global": zGlobal,
			"a-global": aGlobal,
		},
		Services: map[string]*watch.ServiceEntry{
			"svc1": {
				Name: "svc1",
				Extensions: map[string]*watch.ExtensionEntry{
					"z-svc": zSvc,
					"a-svc": aSvc,
				},
			},
		},
	}

	req := DispatchRequest{
		EventType:   "service_lifecycle",
		Phase:       "pre_start",
		Discovery:   discovery,
		TriggerUser: "system",
	}

	results := dispatcher.Dispatch(context.Background(), req)
	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}

	// 验证执行顺序：a-global, z-global, a-svc, z-svc
	data, err := os.ReadFile(markerFile)
	if err != nil {
		t.Fatalf("failed to read marker file: %v", err)
	}

	content := string(data)
	expected := "g2\ng1\ns2\ns1\n" // a-global 用 g2.sh, z-global 用 g1.sh, etc.
	if content != expected {
		t.Errorf("expected execution order:\n%s\ngot:\n%s", expected, content)
	}

	// 验证结果顺序
	expectedNames := []string{"a-global", "z-global", "a-svc", "z-svc"}
	for i, r := range results {
		if r.ExtensionName != expectedNames[i] {
			t.Errorf("result[%d]: expected %s, got %s", i, expectedNames[i], r.ExtensionName)
		}
	}
}

// TestDispatchConcurrentServiceGroups 测试不同服务组并发执行
// REQ-F-022: 不同服务的扩展可并行
func TestDispatchConcurrentServiceGroups(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	// 使用 marker 文件记录并发情况
	markerFile := filepath.Join(tmpDir, "concurrent.txt")

	// 两个脚本各自写入开始时间，等待一小段时间，然后写入结束时间
	createTestScript(t, tmpDir, "svc1-ext.sh",
		"echo svc1-start >> "+markerFile+"\nsleep 0.2\necho svc1-end >> "+markerFile)
	createTestScript(t, tmpDir, "svc2-ext.sh",
		"echo svc2-start >> "+markerFile+"\nsleep 0.2\necho svc2-end >> "+markerFile)

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{},
		Services: map[string]*watch.ServiceEntry{
			"svc1": {
				Name: "svc1",
				Extensions: map[string]*watch.ExtensionEntry{
					"svc1-ext": testExtWithOnDemand("svc1-ext", filepath.Join(tmpDir, "svc1-ext.sh")),
				},
			},
			"svc2": {
				Name: "svc2",
				Extensions: map[string]*watch.ExtensionEntry{
					"svc2-ext": testExtWithOnDemand("svc2-ext", filepath.Join(tmpDir, "svc2-ext.sh")),
				},
			},
		},
	}

	req := DispatchRequest{
		EventType:   "on_demand",
		Discovery:   discovery,
		TriggerUser: "test-user",
	}

	results := dispatcher.Dispatch(context.Background(), req)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// 两个都应该是成功的
	for _, r := range results {
		if r.State != TaskSuccess {
			t.Errorf("expected success for %s, got %s", r.ExtensionName, r.State)
		}
	}
}

// TestDispatchAllResultsReturned 测试所有结果都被返回
// REQ-F-022: Dispatch 返回所有 RunResult
func TestDispatchAllResultsReturned(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	scriptPath := createTestScript(t, tmpDir, "simple.sh", "echo hello")

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"ext1": testExtWithOnDemand("ext1", scriptPath),
			"ext2": testExtWithOnDemand("ext2", scriptPath),
		},
		Services: map[string]*watch.ServiceEntry{
			"svc1": {
				Name: "svc1",
				Extensions: map[string]*watch.ExtensionEntry{
					"svc-ext1": testExtWithOnDemand("svc-ext1", scriptPath),
				},
			},
		},
	}

	req := DispatchRequest{
		EventType:   "on_demand",
		Discovery:   discovery,
		TriggerUser: "test-user",
	}

	results := dispatcher.Dispatch(context.Background(), req)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// 验证每个结果都有 runID
	for _, r := range results {
		if r.RunID == "" {
			t.Error("expected non-empty run ID")
		}
	}
}

// TestGroupByService 测试按服务分组
// REQ-F-022: 以服务为粒度独立执行，全局扩展包含在每个服务组中
func TestGroupByService(t *testing.T) {
	matched := []matchedExtension{
		{serviceName: "", actionID: "run"},
		{serviceName: "svc1", actionID: "run"},
		{serviceName: "svc1", actionID: "check"},
		{serviceName: "svc2", actionID: "run"},
	}

	groups := groupByService(matched)

	// 全局扩展包含在有服务级扩展的每个服务组中
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups (svc1, svc2), got %d", len(groups))
	}
	// svc1 组：1个全局 + 2个服务级 = 3
	if len(groups["svc1"]) != 3 {
		t.Errorf("expected 3 svc1 group extensions, got %d", len(groups["svc1"]))
	}
	// svc2 组：1个全局 + 1个服务级 = 2
	if len(groups["svc2"]) != 2 {
		t.Errorf("expected 2 svc2 group extensions, got %d", len(groups["svc2"]))
	}
}

// TestGroupByServiceGlobalOnly 测试仅有全局扩展时的分组
// REQ-F-022: 无服务级扩展时，全局扩展形成独立组
func TestGroupByServiceGlobalOnly(t *testing.T) {
	matched := []matchedExtension{
		{serviceName: "", actionID: "run"},
		{serviceName: "", actionID: "check"},
	}

	groups := groupByService(matched)

	if len(groups) != 1 {
		t.Fatalf("expected 1 group (global only), got %d", len(groups))
	}
	if len(groups[""]) != 2 {
		t.Errorf("expected 2 global extensions, got %d", len(groups[""]))
	}
}

// TestFindActionByID 测试 action 查找
// REQ-F-022: 根据 actionID 查找 action
func TestFindActionByID(t *testing.T) {
	meta := &config.ExtensionMeta{
		Actions: []config.Action{
			{ID: "run", Label: "Run", Args: []string{"--verbose"}},
			{ID: "check", Label: "Check", Args: nil},
		},
	}

	// 查找存在的 action
	id, args := FindActionByID(meta, "check")
	if id != "check" {
		t.Errorf("expected check, got %s", id)
	}
	if args != nil {
		t.Errorf("expected nil args, got %v", args)
	}

	// 查找存在的 action with args
	id, args = FindActionByID(meta, "run")
	if id != "run" {
		t.Errorf("expected run, got %s", id)
	}
	if len(args) != 1 || args[0] != "--verbose" {
		t.Errorf("expected [--verbose], got %v", args)
	}

	// 空ID返回第一个action
	id, _ = FindActionByID(meta, "")
	if id != "run" {
		t.Errorf("expected first action run, got %s", id)
	}
}

// TestDispatchContextCancel 测试 Dispatch 支持 context 取消
// REQ-F-022: context 取消时任务应被取消
func TestDispatchContextCancel(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	scriptPath := createTestScript(t, tmpDir, "long.sh", "sleep 60")

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"long-ext": testExtWithOnDemand("long-ext", scriptPath),
		},
		Services: map[string]*watch.ServiceEntry{},
	}

	ctx, cancel := context.WithCancel(context.Background())

	req := DispatchRequest{
		EventType:   "on_demand",
		Discovery:   discovery,
		TriggerUser: "test-user",
	}

	resultCh := make(chan []*RunResult, 1)
	go func() {
		resultCh <- dispatcher.Dispatch(ctx, req)
	}()

	// 等待一小段时间后取消 context
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case results := <-resultCh:
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		// context 取消后任务应为 canceled 或 timeout
		if results[0].State != TaskCanceled && results[0].State != TaskTimeout {
			t.Errorf("expected canceled or timeout, got %s", results[0].State)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("test timed out")
	}
}

// TestDispatchParallelServiceGroupsConcurrency 测试并行服务组的并发安全性
// REQ-F-022: 不同服务可并行，使用 mutex 保护结果收集
func TestDispatchParallelServiceGroupsConcurrency(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	scriptPath := createTestScript(t, tmpDir, "simple.sh", "echo ok")

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	// 创建多个服务
	services := make(map[string]*watch.ServiceEntry)
	for i := 0; i < 5; i++ {
		svcName := fmt.Sprintf("svc%d", i)
		services[svcName] = &watch.ServiceEntry{
			Name: svcName,
			Extensions: map[string]*watch.ExtensionEntry{
				svcName + "-ext": testExtWithOnDemand(svcName+"-ext", scriptPath),
			},
		}
	}

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{},
		Services:   services,
	}

	req := DispatchRequest{
		EventType:   "on_demand",
		Discovery:   discovery,
		TriggerUser: "test-user",
	}

	results := dispatcher.Dispatch(context.Background(), req)
	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}

	for _, r := range results {
		if r.State != TaskSuccess {
			t.Errorf("expected success for %s, got %s", r.ExtensionName, r.State)
		}
	}
}

// TestDispatchSerialWithinService 测试同一服务内串行执行
// REQ-F-022: 同一服务内串行执行
func TestDispatchSerialWithinService(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	markerFile := filepath.Join(tmpDir, "serial_order.txt")
	createTestScript(t, tmpDir, "ext1.sh", "echo ext1 >> "+markerFile)
	createTestScript(t, tmpDir, "ext2.sh", "echo ext2 >> "+markerFile)

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{},
		Services: map[string]*watch.ServiceEntry{
			"svc1": {
				Name: "svc1",
				Extensions: map[string]*watch.ExtensionEntry{
					"alpha-ext": testExtWithOnDemand("alpha-ext", filepath.Join(tmpDir, "ext1.sh")),
					"beta-ext":  testExtWithOnDemand("beta-ext", filepath.Join(tmpDir, "ext2.sh")),
				},
			},
		},
	}

	req := DispatchRequest{
		EventType:   "on_demand",
		Discovery:   discovery,
		TriggerUser: "test-user",
	}

	results := dispatcher.Dispatch(context.Background(), req)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// 验证串行执行（字母序）
	data, err := os.ReadFile(markerFile)
	if err != nil {
		t.Fatalf("failed to read marker file: %v", err)
	}

	content := string(data)
	expected := "ext1\next2\n" // alpha-ext 先于 beta-ext
	if content != expected {
		t.Errorf("expected serial execution order:\n%s\ngot:\n%s", expected, content)
	}
}

// TestDispatchWorkDirPreserved 测试工作目录在不同运行间不被清理
// REQ-F-022: 扩展运行前程序不清空该目录
func TestDispatchWorkDirPreserved(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	scriptPath := createTestScript(t, tmpDir, "simple.sh", "echo ok")

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"my-ext": testExtWithOnDemand("my-ext", scriptPath),
		},
		Services: map[string]*watch.ServiceEntry{},
	}

	req := DispatchRequest{
		EventType:   "on_demand",
		Discovery:   discovery,
		TriggerUser: "test-user",
	}

	// 第一次执行
	dispatcher.Dispatch(context.Background(), req)

	// 在 script_tmp 临时目录中创建临时文件
	scriptTmp := filepath.Join(tmpDir, "script_tmp", "global+my-ext")
	tmpFile := filepath.Join(scriptTmp, "preserved.txt")
	os.WriteFile(tmpFile, []byte("preserved"), 0644)

	// 第二次执行
	dispatcher.Dispatch(context.Background(), req)

	// 验证临时文件仍然存在
	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("file should be preserved between runs: %v", err)
	}
	if string(data) != "preserved" {
		t.Errorf("file content should be preserved, got %s", string(data))
	}
}

// TestDispatchMultipleFailures 测试多个连续失败不影响后续执行
// REQ-F-022: 前一个失败不影响后一个执行
func TestDispatchMultipleFailures(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	createTestScript(t, tmpDir, "fail1.sh", "exit 1")
	createTestScript(t, tmpDir, "fail2.sh", "exit 2")
	createTestScript(t, tmpDir, "success.sh", "echo ok")

	executor := NewExecutor(logDir, tmpDir)
	dispatcher := NewDispatcher(executor, tmpDir, logDir, 1800)

	discovery := &watch.DiscoveryResult{
		GlobalExts: map[string]*watch.ExtensionEntry{
			"a-fail1":   testExtWithOnDemand("a-fail1", filepath.Join(tmpDir, "fail1.sh")),
			"b-fail2":   testExtWithOnDemand("b-fail2", filepath.Join(tmpDir, "fail2.sh")),
			"c-success": testExtWithOnDemand("c-success", filepath.Join(tmpDir, "success.sh")),
		},
		Services: map[string]*watch.ServiceEntry{},
	}

	req := DispatchRequest{
		EventType:   "on_demand",
		Discovery:   discovery,
		TriggerUser: "test-user",
	}

	results := dispatcher.Dispatch(context.Background(), req)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	if results[0].State != TaskFailed {
		t.Errorf("a-fail1 expected failed, got %s", results[0].State)
	}
	if results[1].State != TaskFailed {
		t.Errorf("b-fail2 expected failed, got %s", results[1].State)
	}
	if results[2].State != TaskSuccess {
		t.Errorf("c-success expected success, got %s", results[2].State)
	}
}
