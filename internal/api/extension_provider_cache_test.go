package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/watch"
)

// TestUpdateServiceExtensionRefreshesDiscoveryCache 验证 UpdateExtension 写文件后
// 立即更新 Discovery 内存缓存，使后续 GetExtension 返回新值（无需等 watcher rescan）。
// 修复前的 bug：PUT 写入文件成功，但 GET 仍返回 Discovery 缓存的旧 Meta，
// 表现为"编辑保存后关闭再打开仍是旧值"（watcher rescan 有 500ms 防抖延迟）。
func TestUpdateServiceExtensionRefreshesDiscoveryCache(t *testing.T) {
	dir := t.TempDir()

	// 服务级扩展需要 service.yaml 才能被 Discovery 识别（discoverServices 要求）
	svcDir := filepath.Join(dir, "services", "mysvc")
	if err := os.MkdirAll(svcDir, 0755); err != nil {
		t.Fatal(err)
	}
	svcYaml := "name: mysvc\nversion: \"1.0.0\"\ncommand:\n  - sleep\n  - \"1000\"\n"
	if err := os.WriteFile(filepath.Join(svcDir, "service.yaml"), []byte(svcYaml), 0644); err != nil {
		t.Fatal(err)
	}

	// 创建服务级扩展 meta.yaml
	extDir := filepath.Join(svcDir, "extensions", "myext")
	if err := os.MkdirAll(extDir, 0755); err != nil {
		t.Fatal(err)
	}
	origMeta := "name: myext\nversion: \"1.0.0\"\ndescription: original\n" +
		"runtime: bash\nentry: run.sh\ntimeout_seconds: 600\nenabled: true\n" +
		"concurrency: replace\ntriggers:\n  on_demand: true\n"
	if err := os.WriteFile(filepath.Join(extDir, "meta.yaml"), []byte(origMeta), 0644); err != nil {
		t.Fatal(err)
	}

	// 扫描构造 Discovery（不启动 watcher，仅一次性扫描）
	d := watch.NewDiscovery(dir, filepath.Join(dir, "logs"))
	result := d.Scan()
	p := &CoreExtensionProvider{Discovery: result, BaseDir: dir}

	// 初始 description 应为 original
	info, ok := p.GetExtension("myext")
	if !ok {
		t.Fatal("expected extension found before update")
	}
	if info.Description != "original" {
		t.Fatalf("before update: description = %q, want %q", info.Description, "original")
	}

	// UpdateExtension 修改 description
	enabled := true
	newMeta := &config.ExtensionMeta{
		Name:           "myext",
		Version:        "1.0.0",
		Description:    "updated",
		Runtime:        "bash",
		Entry:          "run.sh",
		TimeoutSeconds: 600,
		Enabled:        &enabled,
		Concurrency:    "replace",
	}
	if err := p.UpdateExtension("myext", newMeta, "mysvc"); err != nil {
		t.Fatalf("UpdateExtension failed: %v", err)
	}

	// 立即 GetExtension — 修复后应返回新 description，无需等 watcher rescan
	info2, ok := p.GetExtension("myext")
	if !ok {
		t.Fatal("expected extension found after update")
	}
	if info2.Description != "updated" {
		t.Fatalf("after update: description = %q, want %q (Discovery cache not refreshed)",
			info2.Description, "updated")
	}
}

// TestUpdateGlobalExtensionRefreshesDiscoveryCache 验证全局扩展的缓存刷新（refreshDiscoveryMeta 的 GlobalExts 分支）。
func TestUpdateGlobalExtensionRefreshesDiscoveryCache(t *testing.T) {
	dir := t.TempDir()

	extDir := filepath.Join(dir, "extensions", "gext")
	if err := os.MkdirAll(extDir, 0755); err != nil {
		t.Fatal(err)
	}
	origMeta := "name: gext\nversion: \"1.0.0\"\ndescription: original\n" +
		"runtime: bash\nentry: run.sh\ntimeout_seconds: 600\nenabled: true\n" +
		"concurrency: replace\ntriggers:\n  on_demand: true\n"
	if err := os.WriteFile(filepath.Join(extDir, "meta.yaml"), []byte(origMeta), 0644); err != nil {
		t.Fatal(err)
	}

	d := watch.NewDiscovery(dir, filepath.Join(dir, "logs"))
	result := d.Scan()
	p := &CoreExtensionProvider{Discovery: result, BaseDir: dir}

	info, ok := p.GetExtension("gext")
	if !ok {
		t.Fatal("expected global extension found before update")
	}
	if info.Description != "original" {
		t.Fatalf("before update: description = %q, want %q", info.Description, "original")
	}

	enabled := true
	newMeta := &config.ExtensionMeta{
		Name:           "gext",
		Version:        "1.0.0",
		Description:    "updated-global",
		Runtime:        "bash",
		Entry:          "run.sh",
		TimeoutSeconds: 600,
		Enabled:        &enabled,
		Concurrency:    "replace",
	}
	// service="" 表示全局扩展
	if err := p.UpdateExtension("gext", newMeta, ""); err != nil {
		t.Fatalf("UpdateExtension failed: %v", err)
	}

	info2, ok := p.GetExtension("gext")
	if !ok {
		t.Fatal("expected global extension found after update")
	}
	if info2.Description != "updated-global" {
		t.Fatalf("after update: description = %q, want %q (Discovery cache not refreshed)",
			info2.Description, "updated-global")
	}
}
