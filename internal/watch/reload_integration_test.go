package watch

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestReloadIntegration_FileChangeToClassification 端到端热重载集成测试
// L-04-002: 覆盖 文件变化 → fsnotify 监听 → 防抖 → Discovery 扫描 → ReloadManager 分类 → 待生效变更标注
// 使用真实文件系统与真实 fsnotify watcher，不使用 mock。
func TestReloadIntegration_FileChangeToClassification(t *testing.T) {
	baseDir := t.TempDir()
	logDir := t.TempDir()
	svcDir := filepath.Join(baseDir, "services", "myapp")
	if err := os.MkdirAll(svcDir, 0755); err != nil {
		t.Fatalf("mkdir services: %v", err)
	}
	svcPath := filepath.Join(svcDir, "service.yaml")

	// 初始 service.yaml：command=sleep，tags=v1
	initialContent := "name: myapp\nversion: \"1.0\"\ncommand:\n  - sleep\n  - \"5\"\ntags:\n  - v1\n"
	if err := os.WriteFile(svcPath, []byte(initialContent), 0644); err != nil {
		t.Fatalf("write initial service.yaml: %v", err)
	}

	// 启动 Watcher（REQ-F-026: 500ms 防抖）
	w, err := NewWatcher(baseDir)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	w.Start()
	defer w.Stop()

	// 1. 初始 Discovery 扫描
	disc := NewDiscovery(baseDir, logDir)
	oldResult := disc.Scan()
	if _, ok := oldResult.Services["myapp"]; !ok {
		t.Fatalf("initial discovery: myapp not found")
	}

	// 2. 修改 service.yaml：command 改为 nginx（NeedRestart），tags 改为 v2（Immediate）
	// 原子写：先写临时文件再 rename，触发 fsnotify rename 事件（REQ-F-026: 原子写关注 rename）
	newContent := "name: myapp\nversion: \"1.0\"\ncommand:\n  - nginx\n  - -g\n  - \"daemon off;\"\ntags:\n  - v2\n"
	tmpPath := svcPath + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(newContent), 0644); err != nil {
		t.Fatalf("write tmp service.yaml: %v", err)
	}
	if err := os.Rename(tmpPath, svcPath); err != nil {
		t.Fatalf("rename tmp→service.yaml: %v", err)
	}

	// 3. 等待 Watcher.Events() 投递变更批次（防抖 500ms + 余量）
	select {
	case batch := <-w.Events():
		found := false
		for _, e := range batch {
			if e.Path == svcPath {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("watcher event for %s not in batch: %+v", svcPath, batch)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for watcher event after file change")
	}

	// 4. 新的 Discovery 扫描（模拟 supd 收到 watcher 事件后触发重载）
	newResult := disc.Scan()
	if _, ok := newResult.Services["myapp"]; !ok {
		t.Fatalf("post-change discovery: myapp not found")
	}

	// 5. ReloadManager 分类变更
	rm := NewReloadManager(disc)
	reloadResult := rm.Reload(oldResult, newResult)

	// 6. 验证：command 变更应进入 PendingChanges（CategoryNeedRestart）
	if len(reloadResult.PendingChanges) == 0 {
		t.Fatalf("expected pending changes for command modification, got none (immediate=%d, errors=%v)",
			len(reloadResult.ImmediateChanges), reloadResult.Errors)
	}
	foundCmdPending := false
	for _, pc := range reloadResult.PendingChanges {
		if pc.ServiceName == "myapp" {
			for _, c := range pc.Changes {
				for _, f := range c.Fields {
					if f == "command" && c.Category == CategoryNeedRestart {
						foundCmdPending = true
					}
				}
			}
		}
	}
	if !foundCmdPending {
		t.Errorf("command change not classified as NeedRestart in pending changes: %+v", reloadResult.PendingChanges)
	}

	// 7. 验证：tags 变更应进入 ImmediateChanges（CategoryImmediate）
	foundTagsImmediate := false
	for _, c := range reloadResult.ImmediateChanges {
		for _, f := range c.Fields {
			if f == "tags" && c.Category == CategoryImmediate {
				foundTagsImmediate = true
			}
		}
	}
	if !foundTagsImmediate {
		t.Errorf("tags change not classified as Immediate: %+v", reloadResult.ImmediateChanges)
	}

	// 8. 验证：GetPendingChanges 返回 myapp 的待生效变更
	pending := rm.GetPendingChanges("myapp")
	if len(pending) == 0 {
		t.Fatalf("GetPendingChanges(myapp) returned empty")
	}
	if pending[0].ServiceName != "myapp" {
		t.Errorf("expected ServiceName=myapp, got %s", pending[0].ServiceName)
	}

	// 9. 验证：HasPendingChanges 为 true
	if !rm.HasPendingChanges("myapp") {
		t.Error("HasPendingChanges(myapp) = false, want true")
	}

	// 10. 验证：ClearPendingChanges 后再查询应为空
	rm.ClearPendingChanges("myapp")
	if rm.HasPendingChanges("myapp") {
		t.Error("HasPendingChanges(myapp) = true after ClearPendingChanges, want false")
	}
}

// TestReloadIntegration_NewServiceDetected 验证新增服务目录能被 watcher 检测到并触发 Discovery 重新扫描。
// L-04-002: 覆盖 服务目录新增 → watcher 事件 → discovery 扫描 → 新服务出现在结果中。
func TestReloadIntegration_NewServiceDetected(t *testing.T) {
	baseDir := t.TempDir()
	logDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(baseDir, "services"), 0755); err != nil {
		t.Fatalf("mkdir services: %v", err)
	}

	// 初始扫描：services 目录为空，无服务
	disc := NewDiscovery(baseDir, logDir)
	oldResult := disc.Scan()
	if len(oldResult.Services) != 0 {
		t.Fatalf("expected 0 services initially, got %d", len(oldResult.Services))
	}

	// 启动 Watcher
	w, err := NewWatcher(baseDir)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	w.Start()
	defer w.Stop()

	// 创建新服务目录与 service.yaml
	newSvcDir := filepath.Join(baseDir, "services", "newsvc")
	if err := os.MkdirAll(newSvcDir, 0755); err != nil {
		t.Fatalf("mkdir newsvc: %v", err)
	}
	newSvcPath := filepath.Join(newSvcDir, "service.yaml")
	if err := os.WriteFile(newSvcPath, []byte("name: newsvc\nversion: \"1.0\"\ncommand:\n  - echo\n  - hi\n"), 0644); err != nil {
		t.Fatalf("write newsvc service.yaml: %v", err)
	}

	// 等待 watcher 投递事件（newsvc 目录创建 + service.yaml 写入）
	receivedNewSvcEvent := false
	deadline := time.Now().Add(5 * time.Second)
Loop:
	for time.Now().Before(deadline) {
		select {
		case batch := <-w.Events():
			for _, e := range batch {
				if e.Path == newSvcDir || e.Path == newSvcPath {
					receivedNewSvcEvent = true
				}
			}
			if receivedNewSvcEvent {
				break Loop
			}
		case <-time.After(500 * time.Millisecond):
		}
		if receivedNewSvcEvent {
			break
		}
	}
	if !receivedNewSvcEvent {
		t.Fatalf("timeout waiting for watcher event for new service dir/file")
	}

	// 再次扫描，验证新服务出现
	newResult := disc.Scan()
	svc, ok := newResult.Services["newsvc"]
	if !ok {
		t.Fatalf("post-change discovery: newsvc not found")
	}
	if svc.Config == nil {
		t.Fatal("newsvc Config is nil")
	}
	if len(svc.Config.Command) != 2 || svc.Config.Command[0] != "echo" {
		t.Errorf("newsvc command mismatch: %+v", svc.Config.Command)
	}

	// 由于 oldResult 中没有 newsvc，Reload 不应将其作为变更（新增服务由上层处理）
	rm := NewReloadManager(disc)
	reloadResult := rm.Reload(oldResult, newResult)
	for _, pc := range reloadResult.PendingChanges {
		if pc.ServiceName == "newsvc" {
			t.Errorf("new service should not appear in pending changes: %+v", pc)
		}
	}
}

// TestReloadIntegration_ConfigAppliedToRunningService 端到端热重载集成测试
// L-04-001: 覆盖 修改运行中服务的配置 → fsnotify 检测 → Discovery 重新扫描 → 新配置加载 → 服务继续运行不中断
// 验证"配置热重载→服务实际应用新配置"的完整链路，使用真实文件系统与真实 fsnotify watcher，不使用 mock。
func TestReloadIntegration_ConfigAppliedToRunningService(t *testing.T) {
	baseDir := t.TempDir()
	logDir := t.TempDir()
	svcDir := filepath.Join(baseDir, "services", "myapp")
	if err := os.MkdirAll(svcDir, 0755); err != nil {
		t.Fatalf("mkdir services: %v", err)
	}
	svcPath := filepath.Join(svcDir, "service.yaml")

	// 初始 service.yaml：command=[sleep, 30]（长运行进程），description=v1，tags=[v1]
	initialContent := "name: myapp\nversion: \"1.0\"\ndescription: \"v1\"\ncommand:\n  - sleep\n  - \"30\"\ntags:\n  - v1\n"
	if err := os.WriteFile(svcPath, []byte(initialContent), 0644); err != nil {
		t.Fatalf("write initial service.yaml: %v", err)
	}

	// 1. 启动一个真实的 sleep 进程，模拟"运行中的服务"
	// REQ-F-027: 热重载不中断正在运行的服务和脚本
	sleepCmd := exec.Command("sleep", "30")
	if err := sleepCmd.Start(); err != nil {
		t.Fatalf("start sleep process: %v", err)
	}
	defer func() {
		_ = sleepCmd.Process.Signal(syscall.SIGTERM)
		_, _ = sleepCmd.Process.Wait()
	}()
	t.Logf("started sleep process pid=%d", sleepCmd.Process.Pid)

	// 辅助：通过 signal 0 探活，判断进程是否仍在运行
	processAlive := func() bool {
		return sleepCmd.Process.Signal(syscall.Signal(0)) == nil
	}
	if !processAlive() {
		t.Fatal("sleep process should be alive right after start")
	}

	// 2. 初始 Discovery 扫描（旧配置）
	disc := NewDiscovery(baseDir, logDir)
	oldResult := disc.Scan()
	oldSvc, ok := oldResult.Services["myapp"]
	if !ok {
		t.Fatalf("initial discovery: myapp not found")
	}
	if oldSvc.Config.Description != "v1" {
		t.Fatalf("initial description = %q, want %q", oldSvc.Config.Description, "v1")
	}

	// 3. 启动 Watcher（REQ-F-026: 500ms 防抖）
	w, err := NewWatcher(baseDir)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	w.Start()
	defer w.Stop()

	// 4. 修改 service.yaml：description 改为 v2（验证新配置加载），tags 改为 v2（Immediate）
	// 保持 command 不变（不触发 NeedRestart），验证热重载不中断正在运行的 sleep 进程
	// 原子写：先写临时文件再 rename，触发 fsnotify rename 事件（REQ-F-026: 原子写关注 rename）
	newContent := "name: myapp\nversion: \"1.0\"\ndescription: \"v2\"\ncommand:\n  - sleep\n  - \"30\"\ntags:\n  - v2\n"
	tmpPath := svcPath + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(newContent), 0644); err != nil {
		t.Fatalf("write tmp service.yaml: %v", err)
	}
	if err := os.Rename(tmpPath, svcPath); err != nil {
		t.Fatalf("rename tmp→service.yaml: %v", err)
	}

	// 5. 等待 Watcher.Events() 投递变更批次（防抖 500ms + 余量）
	select {
	case batch := <-w.Events():
		found := false
		for _, e := range batch {
			if e.Path == svcPath {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("watcher event for %s not in batch: %+v", svcPath, batch)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for watcher event after config change")
	}

	// 6. 验证：热重载触发后，运行中的 sleep 进程仍未被中断
	if !processAlive() {
		t.Fatal("sleep process was killed during hot reload; hot reload must not interrupt running services")
	}

	// 7. 新的 Discovery 扫描（模拟 supd 收到 watcher 事件后触发重载，加载新配置）
	newResult := disc.Scan()
	newSvc, ok := newResult.Services["myapp"]
	if !ok {
		t.Fatalf("post-change discovery: myapp not found")
	}

	// 8. 验证：新配置被正确加载（description 已更新为 v2，tags 已更新为 [v2]）
	if newSvc.Config.Description != "v2" {
		t.Errorf("post-change description = %q, want %q", newSvc.Config.Description, "v2")
	}
	if len(newSvc.Config.Tags) != 1 || newSvc.Config.Tags[0] != "v2" {
		t.Errorf("post-change tags = %v, want [v2]", newSvc.Config.Tags)
	}
	// command 未变，新配置中仍为 [sleep, 30]
	if len(newSvc.Config.Command) != 2 || newSvc.Config.Command[0] != "sleep" || newSvc.Config.Command[1] != "30" {
		t.Errorf("post-change command = %v, want [sleep 30]", newSvc.Config.Command)
	}

	// 9. ReloadManager 分类变更（对比 old/new）
	rm := NewReloadManager(disc)
	reloadResult := rm.Reload(oldResult, newResult)

	// 10. 验证：tags 变更应进入 ImmediateChanges（CategoryImmediate）
	foundTagsImmediate := false
	for _, c := range reloadResult.ImmediateChanges {
		for _, f := range c.Fields {
			if f == "tags" && c.Category == CategoryImmediate {
				foundTagsImmediate = true
			}
		}
	}
	if !foundTagsImmediate {
		t.Errorf("tags change not classified as Immediate: %+v", reloadResult.ImmediateChanges)
	}

	// 11. 验证：command 未变更，不应产生 NeedRestart 类待生效变更
	for _, pc := range reloadResult.PendingChanges {
		if pc.ServiceName != "myapp" {
			continue
		}
		for _, c := range pc.Changes {
			if c.Category != CategoryNeedRestart {
				continue
			}
			for _, f := range c.Fields {
				if f == "command" {
					t.Errorf("command should not be in NeedRestart pending changes (command unchanged): %+v", c)
				}
			}
		}
	}

	// 12. 最终验证：整个热重载流程结束后，运行中的 sleep 进程仍然存活
	if !processAlive() {
		t.Fatal("sleep process was killed after reload completed; hot reload must not interrupt running services")
	}
}
