package watch

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// createTestDir 创建临时测试目录结构
func createTestDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "supd-discovery-test-")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	return dir
}

// validServiceYAML 返回一个合法的 service.yaml 内容
func validServiceYAML(name string) string {
	return "name: " + name + "\nversion: \"1.0\"\ncommand: [\"/usr/bin/echo\", \"hello\"]\n"
}

// validExtensionYAML 返回一个合法的 meta.yaml 内容
func validExtensionYAML(name string) string {
	return "name: " + name + "\nversion: \"1.0\"\nentry: run.sh\n"
}

// validEnvYAML 返回一个合法的 env.yaml 内容
func validEnvYAML() string {
	return "env:\n  FOO:\n    value: bar\n"
}

func TestScan_EmptyDirectory(t *testing.T) {
	// REQ-F-025: 空目录扫描返回空结果
	dir := createTestDir(t)
	defer os.RemoveAll(dir)

	d := NewDiscovery(dir, filepath.Join(dir, "log"))
	result := d.Scan()

	if len(result.Services) != 0 {
		t.Errorf("expected 0 services, got %d", len(result.Services))
	}
	if len(result.GlobalExts) != 0 {
		t.Errorf("expected 0 global extensions, got %d", len(result.GlobalExts))
	}
	if len(result.Runtimes) != 0 {
		t.Errorf("expected 0 runtimes, got %d", len(result.Runtimes))
	}
	if len(result.Errors) != 0 {
		t.Errorf("expected 0 errors, got %d", len(result.Errors))
	}
}

func TestScan_NonexistentBaseDir(t *testing.T) {
	// REQ-F-025: 基础目录不存在时返回空结果
	d := NewDiscovery("/nonexistent/supd", "/nonexistent/log")
	result := d.Scan()

	if len(result.Services) != 0 {
		t.Errorf("expected 0 services, got %d", len(result.Services))
	}
	if len(result.GlobalExts) != 0 {
		t.Errorf("expected 0 global extensions, got %d", len(result.GlobalExts))
	}
	if len(result.Runtimes) != 0 {
		t.Errorf("expected 0 runtimes, got %d", len(result.Runtimes))
	}
	if len(result.Errors) != 0 {
		t.Errorf("expected 0 errors, got %d", len(result.Errors))
	}
}

func TestDiscoverServices_Normal(t *testing.T) {
	// REQ-F-025: 正常服务发现（3个服务）
	dir := createTestDir(t)
	defer os.RemoveAll(dir)

	servicesDir := filepath.Join(dir, "services")
	for _, name := range []string{"web", "api", "db"} {
		svcDir := filepath.Join(servicesDir, name)
		if err := os.MkdirAll(svcDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(svcDir, "service.yaml"), []byte(validServiceYAML(name)), 0644); err != nil {
			t.Fatal(err)
		}
	}

	d := NewDiscovery(dir, filepath.Join(dir, "log"))
	result := d.Scan()

	if len(result.Services) != 3 {
		t.Fatalf("expected 3 services, got %d", len(result.Services))
	}

	for _, name := range []string{"web", "api", "db"} {
		svc, ok := result.Services[name]
		if !ok {
			t.Errorf("service %q not found", name)
			continue
		}
		if svc.Name != name {
			t.Errorf("expected name %q, got %q", name, svc.Name)
		}
		if svc.ConfigPath != filepath.Join(dir, "services", name, "service.yaml") {
			t.Errorf("unexpected ConfigPath: %s", svc.ConfigPath)
		}
		if svc.Config == nil {
			t.Errorf("service %q Config is nil", name)
		}
		if svc.Config.Name != name {
			t.Errorf("expected config name %q, got %q", name, svc.Config.Name)
		}
	}
}

func TestDiscoverServices_MissingServiceYAML(t *testing.T) {
	// REQ-F-025: 缺少 service.yaml 的目录被跳过并记录错误
	dir := createTestDir(t)
	defer os.RemoveAll(dir)

	servicesDir := filepath.Join(dir, "services")

	// 正常服务
	goodDir := filepath.Join(servicesDir, "good")
	if err := os.MkdirAll(goodDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(goodDir, "service.yaml"), []byte(validServiceYAML("good")), 0644); err != nil {
		t.Fatal(err)
	}

	// 缺少 service.yaml 的目录
	badDir := filepath.Join(servicesDir, "bad")
	if err := os.MkdirAll(badDir, 0755); err != nil {
		t.Fatal(err)
	}

	d := NewDiscovery(dir, filepath.Join(dir, "log"))
	result := d.Scan()

	if len(result.Services) != 1 {
		t.Errorf("expected 1 service, got %d", len(result.Services))
	}
	if _, ok := result.Services["good"]; !ok {
		t.Error("service 'good' not found")
	}

	// 应该有1个错误
	found := false
	for _, e := range result.Errors {
		if e.Path == badDir && e.Message == "missing service.yaml" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error for missing service.yaml in %s, got errors: %+v", badDir, result.Errors)
	}
}

func TestDiscoverServices_InvalidServiceYAML(t *testing.T) {
	// REQ-F-025: 解析失败的配置记录到 Errors
	dir := createTestDir(t)
	defer os.RemoveAll(dir)

	servicesDir := filepath.Join(dir, "services")
	svcDir := filepath.Join(servicesDir, "broken")
	if err := os.MkdirAll(svcDir, 0755); err != nil {
		t.Fatal(err)
	}
	// 无效 YAML 内容（缺少必填字段）
	if err := os.WriteFile(filepath.Join(svcDir, "service.yaml"), []byte("invalid: yaml\n"), 0644); err != nil {
		t.Fatal(err)
	}

	d := NewDiscovery(dir, filepath.Join(dir, "log"))
	result := d.Scan()

	if len(result.Services) != 0 {
		t.Errorf("expected 0 services, got %d", len(result.Services))
	}
	if len(result.Errors) == 0 {
		t.Error("expected at least one error for invalid service.yaml")
	}
}

func TestDiscoverServices_WithEnvYAML(t *testing.T) {
	// REQ-F-025: env.yaml 可选（不强制要求）
	dir := createTestDir(t)
	defer os.RemoveAll(dir)

	servicesDir := filepath.Join(dir, "services")

	// 服务带 env.yaml
	withEnv := filepath.Join(servicesDir, "withenv")
	if err := os.MkdirAll(withEnv, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(withEnv, "service.yaml"), []byte(validServiceYAML("withenv")), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(withEnv, "env.yaml"), []byte(validEnvYAML()), 0644); err != nil {
		t.Fatal(err)
	}

	// 服务不带 env.yaml
	withoutEnv := filepath.Join(servicesDir, "withoutenv")
	if err := os.MkdirAll(withoutEnv, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(withoutEnv, "service.yaml"), []byte(validServiceYAML("withoutenv")), 0644); err != nil {
		t.Fatal(err)
	}

	d := NewDiscovery(dir, filepath.Join(dir, "log"))
	result := d.Scan()

	if len(result.Services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(result.Services))
	}

	svcWith := result.Services["withenv"]
	if svcWith.EnvPath == "" {
		t.Error("expected EnvPath to be set for 'withenv'")
	}

	svcWithout := result.Services["withoutenv"]
	if svcWithout.EnvPath != "" {
		t.Errorf("expected EnvPath to be empty for 'withoutenv', got %q", svcWithout.EnvPath)
	}
}

func TestDiscoverServices_FilesInServicesDir(t *testing.T) {
	// REQ-F-025: services/ 下的普通文件应被忽略，只扫描子目录
	dir := createTestDir(t)
	defer os.RemoveAll(dir)

	servicesDir := filepath.Join(dir, "services")
	if err := os.MkdirAll(servicesDir, 0755); err != nil {
		t.Fatal(err)
	}
	// 在 services/ 下创建一个普通文件
	if err := os.WriteFile(filepath.Join(servicesDir, "README.md"), []byte("ignore me"), 0644); err != nil {
		t.Fatal(err)
	}

	d := NewDiscovery(dir, filepath.Join(dir, "log"))
	result := d.Scan()

	if len(result.Services) != 0 {
		t.Errorf("expected 0 services (files should be ignored), got %d", len(result.Services))
	}
	if len(result.Errors) != 0 {
		t.Errorf("expected 0 errors, got %d", len(result.Errors))
	}
}

func TestDiscoverGlobalExtensions_Normal(t *testing.T) {
	// REQ-F-025: 全局扩展发现
	dir := createTestDir(t)
	defer os.RemoveAll(dir)

	extDir := filepath.Join(dir, "extensions")
	for _, name := range []string{"backup", "cleanup"} {
		subDir := filepath.Join(extDir, name)
		if err := os.MkdirAll(subDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(subDir, "meta.yaml"), []byte(validExtensionYAML(name)), 0644); err != nil {
			t.Fatal(err)
		}
	}

	d := NewDiscovery(dir, filepath.Join(dir, "log"))
	result := d.Scan()

	if len(result.GlobalExts) != 2 {
		t.Fatalf("expected 2 global extensions, got %d", len(result.GlobalExts))
	}

	for _, name := range []string{"backup", "cleanup"} {
		ext, ok := result.GlobalExts[name]
		if !ok {
			t.Errorf("extension %q not found", name)
			continue
		}
		if ext.Name != name {
			t.Errorf("expected name %q, got %q", name, ext.Name)
		}
		if ext.ServiceName != "" {
			t.Errorf("global extension should have empty ServiceName, got %q", ext.ServiceName)
		}
		if ext.Meta == nil {
			t.Errorf("extension %q Meta is nil", name)
		}
	}
}

func TestDiscoverGlobalExtensions_MissingMetaYAML(t *testing.T) {
	// REQ-F-025: 缺少 meta.yaml 的扩展目录被跳过
	dir := createTestDir(t)
	defer os.RemoveAll(dir)

	extDir := filepath.Join(dir, "extensions")

	// 正常扩展
	goodDir := filepath.Join(extDir, "good-ext")
	if err := os.MkdirAll(goodDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(goodDir, "meta.yaml"), []byte(validExtensionYAML("good-ext")), 0644); err != nil {
		t.Fatal(err)
	}

	// 缺少 meta.yaml
	badDir := filepath.Join(extDir, "bad-ext")
	if err := os.MkdirAll(badDir, 0755); err != nil {
		t.Fatal(err)
	}

	d := NewDiscovery(dir, filepath.Join(dir, "log"))
	result := d.Scan()

	if len(result.GlobalExts) != 1 {
		t.Errorf("expected 1 global extension, got %d", len(result.GlobalExts))
	}

	found := false
	for _, e := range result.Errors {
		if e.Path == badDir && e.Message == "missing meta.yaml" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error for missing meta.yaml in %s", badDir)
	}
}

func TestDiscoverGlobalExtensions_WithEnvYAML(t *testing.T) {
	// REQ-F-025: 扩展级 env.yaml 可选
	dir := createTestDir(t)
	defer os.RemoveAll(dir)

	extDir := filepath.Join(dir, "extensions")

	withEnv := filepath.Join(extDir, "with-env")
	if err := os.MkdirAll(withEnv, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(withEnv, "meta.yaml"), []byte(validExtensionYAML("with-env")), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(withEnv, "env.yaml"), []byte(validEnvYAML()), 0644); err != nil {
		t.Fatal(err)
	}

	withoutEnv := filepath.Join(extDir, "without-env")
	if err := os.MkdirAll(withoutEnv, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(withoutEnv, "meta.yaml"), []byte(validExtensionYAML("without-env")), 0644); err != nil {
		t.Fatal(err)
	}

	d := NewDiscovery(dir, filepath.Join(dir, "log"))
	result := d.Scan()

	if len(result.GlobalExts) != 2 {
		t.Fatalf("expected 2 extensions, got %d", len(result.GlobalExts))
	}

	extWith := result.GlobalExts["with-env"]
	if extWith.EnvPath == "" {
		t.Error("expected EnvPath to be set")
	}

	extWithout := result.GlobalExts["without-env"]
	if extWithout.EnvPath != "" {
		t.Errorf("expected EnvPath to be empty, got %q", extWithout.EnvPath)
	}
}

func TestDiscoverRuntimes_Normal(t *testing.T) {
	// REQ-F-025: 运行时发现（文件和非文件混合）
	dir := createTestDir(t)
	defer os.RemoveAll(dir)

	runtimesDir := filepath.Join(dir, "runtimes")
	if err := os.MkdirAll(runtimesDir, 0755); err != nil {
		t.Fatal(err)
	}

	// 创建运行时文件
	if err := os.WriteFile(filepath.Join(runtimesDir, "bun"), []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimesDir, "deno"), []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatal(err)
	}
	// 创建一个子目录（应被忽略）
	if err := os.MkdirAll(filepath.Join(runtimesDir, "not-a-runtime"), 0755); err != nil {
		t.Fatal(err)
	}

	d := NewDiscovery(dir, filepath.Join(dir, "log"))
	result := d.Scan()

	if len(result.Runtimes) != 2 {
		t.Fatalf("expected 2 runtimes, got %d", len(result.Runtimes))
	}

	expected := map[string]string{
		"bun":  filepath.Join(runtimesDir, "bun"),
		"deno": filepath.Join(runtimesDir, "deno"),
	}
	for name, expPath := range expected {
		got, ok := result.Runtimes[name]
		if !ok {
			t.Errorf("runtime %q not found", name)
			continue
		}
		if got != expPath {
			t.Errorf("runtime %q: expected path %q, got %q", name, expPath, got)
		}
	}

	// 目录不应出现在 runtimes 中
	if _, ok := result.Runtimes["not-a-runtime"]; ok {
		t.Error("directories should not appear as runtimes")
	}
}

func TestDiscoverServiceExtensions_Normal(t *testing.T) {
	// REQ-F-025: 服务级扩展发现
	dir := createTestDir(t)
	defer os.RemoveAll(dir)

	servicesDir := filepath.Join(dir, "services")
	svcDir := filepath.Join(servicesDir, "web")
	svcExtDir := filepath.Join(svcDir, "extensions")

	if err := os.MkdirAll(svcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svcDir, "service.yaml"), []byte(validServiceYAML("web")), 0644); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"logger", "monitor"} {
		extSubDir := filepath.Join(svcExtDir, name)
		if err := os.MkdirAll(extSubDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(extSubDir, "meta.yaml"), []byte(validExtensionYAML(name)), 0644); err != nil {
			t.Fatal(err)
		}
	}

	d := NewDiscovery(dir, filepath.Join(dir, "log"))
	result := d.Scan()

	if len(result.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(result.Services))
	}
	svc := result.Services["web"]
	if len(svc.Extensions) != 2 {
		t.Fatalf("expected 2 service extensions, got %d", len(svc.Extensions))
	}

	for _, name := range []string{"logger", "monitor"} {
		ext, ok := svc.Extensions[name]
		if !ok {
			t.Errorf("extension %q not found", name)
			continue
		}
		if ext.ServiceName != "web" {
			t.Errorf("expected ServiceName 'web', got %q", ext.ServiceName)
		}
		if ext.Meta == nil {
			t.Errorf("extension %q Meta is nil", name)
		}
	}
}

func TestDiscoverServiceExtensions_MissingMetaYAML(t *testing.T) {
	// REQ-F-025: 缺少 meta.yaml 的服务级扩展目录被跳过
	dir := createTestDir(t)
	defer os.RemoveAll(dir)

	servicesDir := filepath.Join(dir, "services")
	svcDir := filepath.Join(servicesDir, "web")
	svcExtDir := filepath.Join(svcDir, "extensions")

	if err := os.MkdirAll(svcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svcDir, "service.yaml"), []byte(validServiceYAML("web")), 0644); err != nil {
		t.Fatal(err)
	}

	// 正常扩展
	goodExtDir := filepath.Join(svcExtDir, "good")
	if err := os.MkdirAll(goodExtDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(goodExtDir, "meta.yaml"), []byte(validExtensionYAML("good")), 0644); err != nil {
		t.Fatal(err)
	}

	// 缺少 meta.yaml
	badExtDir := filepath.Join(svcExtDir, "bad")
	if err := os.MkdirAll(badExtDir, 0755); err != nil {
		t.Fatal(err)
	}

	d := NewDiscovery(dir, filepath.Join(dir, "log"))
	result := d.Scan()

	svc := result.Services["web"]
	if len(svc.Extensions) != 1 {
		t.Errorf("expected 1 service extension, got %d", len(svc.Extensions))
	}

	found := false
	for _, e := range result.Errors {
		if e.Path == badExtDir && e.Message == "missing meta.yaml" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error for missing meta.yaml in %s", badExtDir)
	}
}

func TestDiscoverServiceExtensions_WithEnvYAML(t *testing.T) {
	// REQ-F-025: 服务级扩展的 env.yaml 可选
	dir := createTestDir(t)
	defer os.RemoveAll(dir)

	servicesDir := filepath.Join(dir, "services")
	svcDir := filepath.Join(servicesDir, "web")
	svcExtDir := filepath.Join(svcDir, "extensions")

	if err := os.MkdirAll(svcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svcDir, "service.yaml"), []byte(validServiceYAML("web")), 0644); err != nil {
		t.Fatal(err)
	}

	// 带 env.yaml 的扩展
	withEnv := filepath.Join(svcExtDir, "with-env")
	if err := os.MkdirAll(withEnv, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(withEnv, "meta.yaml"), []byte(validExtensionYAML("with-env")), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(withEnv, "env.yaml"), []byte(validEnvYAML()), 0644); err != nil {
		t.Fatal(err)
	}

	// 不带 env.yaml 的扩展
	withoutEnv := filepath.Join(svcExtDir, "without-env")
	if err := os.MkdirAll(withoutEnv, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(withoutEnv, "meta.yaml"), []byte(validExtensionYAML("without-env")), 0644); err != nil {
		t.Fatal(err)
	}

	d := NewDiscovery(dir, filepath.Join(dir, "log"))
	result := d.Scan()

	svc := result.Services["web"]
	if len(svc.Extensions) != 2 {
		t.Fatalf("expected 2 extensions, got %d", len(svc.Extensions))
	}

	extWith := svc.Extensions["with-env"]
	if extWith.EnvPath == "" {
		t.Error("expected EnvPath to be set for 'with-env'")
	}

	extWithout := svc.Extensions["without-env"]
	if extWithout.EnvPath != "" {
		t.Errorf("expected EnvPath to be empty for 'without-env', got %q", extWithout.EnvPath)
	}
}

func TestDiscoverServiceExtensions_InvalidMetaYAML(t *testing.T) {
	// REQ-F-025: 解析失败的扩展配置记录到 Errors
	dir := createTestDir(t)
	defer os.RemoveAll(dir)

	servicesDir := filepath.Join(dir, "services")
	svcDir := filepath.Join(servicesDir, "web")
	svcExtDir := filepath.Join(svcDir, "extensions")

	if err := os.MkdirAll(svcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svcDir, "service.yaml"), []byte(validServiceYAML("web")), 0644); err != nil {
		t.Fatal(err)
	}

	// 无效的 meta.yaml
	brokenDir := filepath.Join(svcExtDir, "broken")
	if err := os.MkdirAll(brokenDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(brokenDir, "meta.yaml"), []byte("invalid: yaml\n"), 0644); err != nil {
		t.Fatal(err)
	}

	d := NewDiscovery(dir, filepath.Join(dir, "log"))
	result := d.Scan()

	svc := result.Services["web"]
	if len(svc.Extensions) != 0 {
		t.Errorf("expected 0 extensions, got %d", len(svc.Extensions))
	}

	if len(result.Errors) == 0 {
		t.Error("expected at least one error for invalid meta.yaml")
	}
}

func TestScan_FullDiscovery(t *testing.T) {
	// REQ-F-025: 全量扫描测试（所有5种发现规则组合）
	dir := createTestDir(t)
	defer os.RemoveAll(dir)

	// 创建服务
	servicesDir := filepath.Join(dir, "services")
	for _, name := range []string{"web", "api"} {
		svcDir := filepath.Join(servicesDir, name)
		if err := os.MkdirAll(svcDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(svcDir, "service.yaml"), []byte(validServiceYAML(name)), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// 服务级扩展
	webExtDir := filepath.Join(servicesDir, "web", "extensions", "logger")
	if err := os.MkdirAll(webExtDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(webExtDir, "meta.yaml"), []byte(validExtensionYAML("logger")), 0644); err != nil {
		t.Fatal(err)
	}

	// 全局扩展
	extDir := filepath.Join(dir, "extensions", "backup")
	if err := os.MkdirAll(extDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extDir, "meta.yaml"), []byte(validExtensionYAML("backup")), 0644); err != nil {
		t.Fatal(err)
	}

	// 运行时
	runtimesDir := filepath.Join(dir, "runtimes")
	if err := os.MkdirAll(runtimesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimesDir, "bun"), []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatal(err)
	}

	// 环境变量
	envDir := filepath.Join(dir, "env")
	if err := os.MkdirAll(envDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(envDir, "00-base.yaml"), []byte(validEnvYAML()), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(envDir, "01-extra.yaml"), []byte(validEnvYAML()), 0644); err != nil {
		t.Fatal(err)
	}

	d := NewDiscovery(dir, filepath.Join(dir, "log"))
	result := d.Scan()

	// 验证服务
	if len(result.Services) != 2 {
		t.Errorf("expected 2 services, got %d", len(result.Services))
	}

	// 验证服务级扩展
	webSvc := result.Services["web"]
	if len(webSvc.Extensions) != 1 {
		t.Errorf("expected 1 extension for 'web', got %d", len(webSvc.Extensions))
	}
	if ext, ok := webSvc.Extensions["logger"]; !ok {
		t.Error("extension 'logger' not found in web service")
	} else if ext.ServiceName != "web" {
		t.Errorf("expected ServiceName 'web', got %q", ext.ServiceName)
	}

	// 验证全局扩展
	if len(result.GlobalExts) != 1 {
		t.Errorf("expected 1 global extension, got %d", len(result.GlobalExts))
	}
	if ext, ok := result.GlobalExts["backup"]; !ok {
		t.Error("global extension 'backup' not found")
	} else if ext.ServiceName != "" {
		t.Errorf("global extension should have empty ServiceName, got %q", ext.ServiceName)
	}

	// 验证运行时
	if len(result.Runtimes) != 1 {
		t.Errorf("expected 1 runtime, got %d", len(result.Runtimes))
	}

	// 验证环境变量
	envFiles := d.DiscoverEnvFiles()
	if len(envFiles) != 2 {
		t.Errorf("expected 2 env files, got %d", len(envFiles))
	}
	// 验证排序
	expected := []string{
		filepath.Join(envDir, "00-base.yaml"),
		filepath.Join(envDir, "01-extra.yaml"),
	}
	sort.Strings(expected)
	for i, exp := range expected {
		if envFiles[i] != exp {
			t.Errorf("env file[%d]: expected %q, got %q", i, exp, envFiles[i])
		}
	}
}

func TestDiscoverEnvFiles_Empty(t *testing.T) {
	// REQ-F-025: env/ 目录不存在时返回空
	dir := createTestDir(t)
	defer os.RemoveAll(dir)

	d := NewDiscovery(dir, filepath.Join(dir, "log"))
	envFiles := d.DiscoverEnvFiles()

	if len(envFiles) != 0 {
		t.Errorf("expected 0 env files, got %d", len(envFiles))
	}
}

func TestDiscoverEnvFiles_OnlyYAML(t *testing.T) {
	// REQ-F-025: 只返回 *.yaml 文件
	dir := createTestDir(t)
	defer os.RemoveAll(dir)

	envDir := filepath.Join(dir, "env")
	if err := os.MkdirAll(envDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(envDir, "00-base.yaml"), []byte(validEnvYAML()), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(envDir, "README.txt"), []byte("ignore"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(envDir, "config.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	d := NewDiscovery(dir, filepath.Join(dir, "log"))
	envFiles := d.DiscoverEnvFiles()

	if len(envFiles) != 1 {
		t.Errorf("expected 1 env file, got %d", len(envFiles))
	}
	if len(envFiles) > 0 && envFiles[0] != filepath.Join(envDir, "00-base.yaml") {
		t.Errorf("expected %q, got %q", filepath.Join(envDir, "00-base.yaml"), envFiles[0])
	}
}

func TestScan_PartialFailure(t *testing.T) {
	// REQ-F-025: 单个服务/扩展解析失败不影响其他
	dir := createTestDir(t)
	defer os.RemoveAll(dir)

	servicesDir := filepath.Join(dir, "services")

	// 正常服务
	goodDir := filepath.Join(servicesDir, "good")
	if err := os.MkdirAll(goodDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(goodDir, "service.yaml"), []byte(validServiceYAML("good")), 0644); err != nil {
		t.Fatal(err)
	}

	// 缺少 service.yaml
	missingDir := filepath.Join(servicesDir, "missing")
	if err := os.MkdirAll(missingDir, 0755); err != nil {
		t.Fatal(err)
	}

	// 无效 service.yaml
	brokenDir := filepath.Join(servicesDir, "broken")
	if err := os.MkdirAll(brokenDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(brokenDir, "service.yaml"), []byte("bad: data\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// 全局扩展——正常
	extDir := filepath.Join(dir, "extensions", "ok-ext")
	if err := os.MkdirAll(extDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extDir, "meta.yaml"), []byte(validExtensionYAML("ok-ext")), 0644); err != nil {
		t.Fatal(err)
	}

	d := NewDiscovery(dir, filepath.Join(dir, "log"))
	result := d.Scan()

	// 只有 good 服务成功
	if len(result.Services) != 1 {
		t.Errorf("expected 1 service, got %d", len(result.Services))
	}
	if _, ok := result.Services["good"]; !ok {
		t.Error("service 'good' not found")
	}

	// 全局扩展不受影响
	if len(result.GlobalExts) != 1 {
		t.Errorf("expected 1 global extension, got %d", len(result.GlobalExts))
	}

	// 应该有2个错误（missing + broken）
	if len(result.Errors) < 2 {
		t.Errorf("expected at least 2 errors, got %d: %+v", len(result.Errors), result.Errors)
	}
}

func TestScan_ServiceWithoutExtensionsDir(t *testing.T) {
	// REQ-F-025: 服务目录下没有 extensions/ 子目录是正常的
	dir := createTestDir(t)
	defer os.RemoveAll(dir)

	servicesDir := filepath.Join(dir, "services")
	svcDir := filepath.Join(servicesDir, "simple")
	if err := os.MkdirAll(svcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svcDir, "service.yaml"), []byte(validServiceYAML("simple")), 0644); err != nil {
		t.Fatal(err)
	}

	d := NewDiscovery(dir, filepath.Join(dir, "log"))
	result := d.Scan()

	if len(result.Services) != 1 {
		t.Errorf("expected 1 service, got %d", len(result.Services))
	}
	svc := result.Services["simple"]
	if len(svc.Extensions) != 0 {
		t.Errorf("expected 0 extensions, got %d", len(svc.Extensions))
	}
	if len(result.Errors) != 0 {
		t.Errorf("expected 0 errors, got %d: %+v", len(result.Errors), result.Errors)
	}
}

// TestIsBackupDir 验证 isBackupDir 对各种目录名的判断
// BUG 修复：删除扩展/服务时生成 .bak.<timestamp> 备份目录，扫描时应跳过
func TestIsBackupDir(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"foo.bak.20260723-100440", true},     // 标准备份名
		{"foo.bak.bak.20260723-100426", true}, // 双重备份（重复删除）
		{"qbittorrent-updater", false},        // 合法扩展名
		{"supd-startup-hook", false},          // 合法扩展名
		{"", false},                           // 空字符串
		{"mybak", false},                      // 含 bak 子串但不含点
		{"a.b.c", false},                      // 含点但不含 bak
	}
	for _, c := range cases {
		if got := isBackupDir(c.name); got != c.want {
			t.Errorf("isBackupDir(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestDiscoverGlobalExtensions_SkipsBackupDirs 验证全局扩展扫描跳过 .bak 备份目录
// BUG：DeleteExtension 把目录 rename 为 .bak.<timestamp>，扫描器不过滤会重复显示已删扩展
func TestDiscoverGlobalExtensions_SkipsBackupDirs(t *testing.T) {
	dir := createTestDir(t)
	defer os.RemoveAll(dir)

	extDir := filepath.Join(dir, "extensions")
	// 正常扩展 + 备份目录（两者 meta.yaml 都合法，模拟真实删除场景）
	for _, dirName := range []string{"my-ext", "my-ext.bak.20260723-100440"} {
		subDir := filepath.Join(extDir, dirName)
		if err := os.MkdirAll(subDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(subDir, "meta.yaml"), []byte(validExtensionYAML("my-ext")), 0644); err != nil {
			t.Fatal(err)
		}
	}

	d := NewDiscovery(dir, filepath.Join(dir, "log"))
	result := d.Scan()

	if len(result.GlobalExts) != 1 {
		t.Fatalf("expected 1 global extension (backup should be skipped), got %d", len(result.GlobalExts))
	}
	if _, ok := result.GlobalExts["my-ext"]; !ok {
		t.Errorf("normal extension my-ext not found")
	}
	if _, ok := result.GlobalExts["my-ext.bak.20260723-100440"]; ok {
		t.Errorf("backup directory should not be scanned as extension")
	}
}

// TestDiscoverServiceExtensions_SkipsBackupDirs 验证服务级扩展扫描跳过 .bak 备份目录
func TestDiscoverServiceExtensions_SkipsBackupDirs(t *testing.T) {
	dir := createTestDir(t)
	defer os.RemoveAll(dir)

	svcExtDir := filepath.Join(dir, "services", "foo", "extensions")
	for _, dirName := range []string{"svc-ext", "svc-ext.bak.20260723-100440"} {
		subDir := filepath.Join(svcExtDir, dirName)
		if err := os.MkdirAll(subDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(subDir, "meta.yaml"), []byte(validExtensionYAML("svc-ext")), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// 服务本身
	svcDir := filepath.Join(dir, "services", "foo")
	if err := os.WriteFile(filepath.Join(svcDir, "service.yaml"), []byte(validServiceYAML("foo")), 0644); err != nil {
		t.Fatal(err)
	}

	d := NewDiscovery(dir, filepath.Join(dir, "log"))
	result := d.Scan()

	svc, ok := result.Services["foo"]
	if !ok {
		t.Fatalf("service foo not found")
	}
	if len(svc.Extensions) != 1 {
		t.Fatalf("expected 1 service extension (backup should be skipped), got %d", len(svc.Extensions))
	}
	if _, ok := svc.Extensions["svc-ext"]; !ok {
		t.Errorf("normal service extension svc-ext not found")
	}
	if _, ok := svc.Extensions["svc-ext.bak.20260723-100440"]; ok {
		t.Errorf("backup directory should not be scanned as service extension")
	}
}

// TestDiscoverServices_SkipsBackupDirs 验证服务扫描跳过 .bak 备份目录
func TestDiscoverServices_SkipsBackupDirs(t *testing.T) {
	dir := createTestDir(t)
	defer os.RemoveAll(dir)

	servicesDir := filepath.Join(dir, "services")
	for _, dirName := range []string{"real-svc", "real-svc.bak.20260723-100440"} {
		svcDir := filepath.Join(servicesDir, dirName)
		if err := os.MkdirAll(svcDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(svcDir, "service.yaml"), []byte(validServiceYAML("real-svc")), 0644); err != nil {
			t.Fatal(err)
		}
	}

	d := NewDiscovery(dir, filepath.Join(dir, "log"))
	result := d.Scan()

	if len(result.Services) != 1 {
		t.Fatalf("expected 1 service (backup should be skipped), got %d", len(result.Services))
	}
	if _, ok := result.Services["real-svc"]; !ok {
		t.Errorf("normal service real-svc not found")
	}
	if _, ok := result.Services["real-svc.bak.20260723-100440"]; ok {
		t.Errorf("backup directory should not be scanned as service")
	}
}
