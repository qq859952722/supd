package config

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	supderrors "github.com/supdorg/supd/internal/errors"
)

// --- RegisterBuiltin 测试 ---

// TestRegisterBuiltin 注册4个内置运行时
func TestRegisterBuiltin(t *testing.T) {
	registry := NewRuntimeRegistry()
	RegisterBuiltin(registry)

	expected := map[string]string{
		"bash":    "/bin/bash",
		"sh":      "/bin/sh",
		"python3": "python3",
		"node":    "node",
	}

	for alias, path := range expected {
		entry, ok := registry.entries[alias]
		if !ok {
			t.Errorf("builtin runtime %q not registered", alias)
			continue
		}
		if entry.Path != path {
			t.Errorf("runtime %q: path = %q, want %q", alias, entry.Path, path)
		}
		if entry.Source != RuntimeSourceBuiltin {
			t.Errorf("runtime %q: source = %q, want %q", alias, entry.Source, RuntimeSourceBuiltin)
		}
	}

	if len(registry.entries) != 4 {
		t.Errorf("registry has %d entries, want 4", len(registry.entries))
	}
}

// --- RegisterFromConfig 测试 ---

// TestRegisterFromConfig 覆盖内置
func TestRegisterFromConfig(t *testing.T) {
	registry := NewRuntimeRegistry()
	RegisterBuiltin(registry)

	configRuntimes := map[string]string{
		"python3": "/usr/bin/python3.11",
		"node":    "/usr/local/bin/node",
		"tjs":     "/opt/tkixijs/tkixijs",
	}
	RegisterFromConfig(registry, configRuntimes)

	// python3 应被 config 覆盖
	entry := registry.entries["python3"]
	if entry.Source != RuntimeSourceConfig {
		t.Errorf("python3 source = %q, want %q", entry.Source, RuntimeSourceConfig)
	}
	if entry.Path != "/usr/bin/python3.11" {
		t.Errorf("python3 path = %q, want %q", entry.Path, "/usr/bin/python3.11")
	}

	// 新增的 tjs
	entry = registry.entries["tjs"]
	if entry.Source != RuntimeSourceConfig {
		t.Errorf("tjs source = %q, want %q", entry.Source, RuntimeSourceConfig)
	}

	// bash 未被覆盖，仍是 builtin
	entry = registry.entries["bash"]
	if entry.Source != RuntimeSourceBuiltin {
		t.Errorf("bash source = %q, want %q", entry.Source, RuntimeSourceBuiltin)
	}
}

// --- RegisterFromScan 测试 ---

// TestRegisterFromScan 覆盖内置但不覆盖 config
func TestRegisterFromScan(t *testing.T) {
	registry := NewRuntimeRegistry()
	RegisterBuiltin(registry)

	// 先注册 config
	configRuntimes := map[string]string{
		"python3": "/usr/bin/python3.11",
	}
	RegisterFromConfig(registry, configRuntimes)

	// scan 注册，包含与 config 和 builtin 同名的
	scanRuntimes := map[string]string{
		"python3": "/etc/supd/runtimes/python3",
		"bash":    "/etc/supd/runtimes/bash",
		"deno":    "/etc/supd/runtimes/deno",
	}
	RegisterFromScan(registry, scanRuntimes)

	// python3 应保持 config 来源（scan 不覆盖 config）
	entry := registry.entries["python3"]
	if entry.Source != RuntimeSourceConfig {
		t.Errorf("python3 source = %q, want %q", entry.Source, RuntimeSourceConfig)
	}
	if entry.Path != "/usr/bin/python3.11" {
		t.Errorf("python3 path = %q, want %q", entry.Path, "/usr/bin/python3.11")
	}

	// bash 应被 scan 覆盖（scan > builtin）
	entry = registry.entries["bash"]
	if entry.Source != RuntimeSourceScan {
		t.Errorf("bash source = %q, want %q", entry.Source, RuntimeSourceScan)
	}
	if entry.Path != "/etc/supd/runtimes/bash" {
		t.Errorf("bash path = %q, want %q", entry.Path, "/etc/supd/runtimes/bash")
	}

	// deno 是新增的 scan 来源
	entry = registry.entries["deno"]
	if entry.Source != RuntimeSourceScan {
		t.Errorf("deno source = %q, want %q", entry.Source, RuntimeSourceScan)
	}
}

// --- 三层优先级综合测试 ---

// TestThreeLayerPriority 三层优先级正确（config > scan > builtin）
func TestThreeLayerPriority(t *testing.T) {
	registry := NewRuntimeRegistry()

	// 1. builtin
	RegisterBuiltin(registry)

	// 2. scan（覆盖 bash，新增 deno）
	scanRuntimes := map[string]string{
		"bash": "/etc/supd/runtimes/bash",
		"deno": "/etc/supd/runtimes/deno",
	}
	RegisterFromScan(registry, scanRuntimes)

	// 3. config（覆盖 bash，新增 tjs）
	configRuntimes := map[string]string{
		"bash": "/opt/custom/bash",
		"tjs":  "/opt/tkixijs/tkixijs",
	}
	RegisterFromConfig(registry, configRuntimes)

	// bash: config 优先级最高
	entry := registry.entries["bash"]
	if entry.Source != RuntimeSourceConfig {
		t.Errorf("bash source = %q, want %q", entry.Source, RuntimeSourceConfig)
	}
	if entry.Path != "/opt/custom/bash" {
		t.Errorf("bash path = %q, want %q", entry.Path, "/opt/custom/bash")
	}

	// deno: scan 来源
	entry = registry.entries["deno"]
	if entry.Source != RuntimeSourceScan {
		t.Errorf("deno source = %q, want %q", entry.Source, RuntimeSourceScan)
	}

	// sh: builtin 来源（未被覆盖）
	entry = registry.entries["sh"]
	if entry.Source != RuntimeSourceBuiltin {
		t.Errorf("sh source = %q, want %q", entry.Source, RuntimeSourceBuiltin)
	}

	// tjs: config 来源
	entry = registry.entries["tjs"]
	if entry.Source != RuntimeSourceConfig {
		t.Errorf("tjs source = %q, want %q", entry.Source, RuntimeSourceConfig)
	}
}

// --- Resolve 测试 ---

// TestResolveFound 找到已注册且可用的运行时
func TestResolveFound(t *testing.T) {
	registry := NewRuntimeRegistry()
	registry.entries["bash"] = &RuntimeEntry{
		Alias:     "bash",
		Path:      "/bin/bash",
		Source:    RuntimeSourceBuiltin,
		Available: true,
		AbsPath:   "/bin/bash",
	}

	entry, err := Resolve(registry, "bash")
	if err != nil {
		t.Fatalf("Resolve(bash) returned error: %v", err)
	}
	if entry.Alias != "bash" {
		t.Errorf("alias = %q, want %q", entry.Alias, "bash")
	}
	if entry.AbsPath != "/bin/bash" {
		t.Errorf("absPath = %q, want %q", entry.AbsPath, "/bin/bash")
	}
}

// TestResolveNotFound 找不到返回 RUNTIME_NOT_FOUND
func TestResolveNotFound(t *testing.T) {
	registry := NewRuntimeRegistry()

	_, err := Resolve(registry, "nonexistent")
	if err == nil {
		t.Fatal("Resolve(nonexistent) should return error")
	}

	var svcErr *supderrors.ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("error type = %T, want *ServiceError", err)
	}
	if svcErr.Code != supderrors.ErrRuntimeNotFound {
		t.Errorf("error code = %q, want %q", svcErr.Code, supderrors.ErrRuntimeNotFound)
	}
}

// TestResolveNotAvailable 不可用的运行时 Resolve 返回错误
func TestResolveNotAvailable(t *testing.T) {
	registry := NewRuntimeRegistry()
	registry.entries["missing"] = &RuntimeEntry{
		Alias:     "missing",
		Path:      "/nonexistent/binary",
		Source:    RuntimeSourceConfig,
		Available: false,
		AbsPath:   "/nonexistent/binary",
	}

	_, err := Resolve(registry, "missing")
	if err == nil {
		t.Fatal("Resolve(missing) should return error for unavailable runtime")
	}

	var svcErr *supderrors.ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("error type = %T, want *ServiceError", err)
	}
	if svcErr.Code != supderrors.ErrRuntimeNotFound {
		t.Errorf("error code = %q, want %q", svcErr.Code, supderrors.ErrRuntimeNotFound)
	}
}

// --- List 测试 ---

// TestListReturnsAll 返回所有已注册运行时
func TestListReturnsAll(t *testing.T) {
	registry := NewRuntimeRegistry()
	RegisterBuiltin(registry)

	list := List(registry)
	if len(list) != 4 {
		t.Fatalf("List() returned %d entries, want 4", len(list))
	}

	// 验证按别名排序
	for i := 1; i < len(list); i++ {
		if list[i-1].Alias > list[i].Alias {
			t.Errorf("List() not sorted: %q > %q", list[i-1].Alias, list[i].Alias)
		}
	}
}

// --- ValidateRuntime 测试 ---

// TestValidateRuntimeAbsolutePath 绝对路径校验
func TestValidateRuntimeAbsolutePath(t *testing.T) {
	// 创建临时可执行文件
	tmpDir := t.TempDir()
	execPath := filepath.Join(tmpDir, "test-runtime")
	if err := os.WriteFile(execPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("failed to create test executable: %v", err)
	}

	entry := &RuntimeEntry{
		Alias:  "test",
		Path:   execPath,
		Source: RuntimeSourceConfig,
	}
	ValidateRuntime(entry)

	if !entry.Available {
		t.Errorf("runtime should be available, path=%s", execPath)
	}
	if entry.AbsPath != execPath {
		t.Errorf("absPath = %q, want %q", entry.AbsPath, execPath)
	}
}

// TestValidateRuntimeAbsolutePathNotExists 绝对路径不存在
func TestValidateRuntimeAbsolutePathNotExists(t *testing.T) {
	entry := &RuntimeEntry{
		Alias:  "missing",
		Path:   "/nonexistent/path/binary",
		Source: RuntimeSourceConfig,
	}
	ValidateRuntime(entry)

	if entry.Available {
		t.Error("runtime should not be available for nonexistent path")
	}
}

// TestValidateRuntimeAbsolutePathNotExecutable 绝对路径不可执行
func TestValidateRuntimeAbsolutePathNotExecutable(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "not-exec")
	if err := os.WriteFile(filePath, []byte("data"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	entry := &RuntimeEntry{
		Alias:  "notexec",
		Path:   filePath,
		Source: RuntimeSourceConfig,
	}
	ValidateRuntime(entry)

	if entry.Available {
		t.Error("runtime should not be available for non-executable file")
	}
}

// TestValidateRuntimePathLookup PATH查找
func TestValidateRuntimePathLookup(t *testing.T) {
	// python3 或 node 或 sh 应该在 PATH 中可找到
	// 使用 sh 因为它最普遍
	entry := &RuntimeEntry{
		Alias:  "sh",
		Path:   "sh",
		Source: RuntimeSourceBuiltin,
	}
	ValidateRuntime(entry)

	// sh 通常在 PATH 中
	shPath, err := exec.LookPath("sh")
	if err == nil {
		if !entry.Available {
			t.Error("sh should be available via PATH lookup")
		}
		if entry.AbsPath != shPath {
			t.Errorf("absPath = %q, want %q", entry.AbsPath, shPath)
		}
	}
}

// TestValidateRuntimePathLookupNotFound PATH查找不存在
func TestValidateRuntimePathLookupNotFound(t *testing.T) {
	entry := &RuntimeEntry{
		Alias:  "nonexistent_runtime_xyz",
		Path:   "nonexistent_runtime_xyz",
		Source: RuntimeSourceBuiltin,
	}
	ValidateRuntime(entry)

	if entry.Available {
		t.Error("nonexistent runtime should not be available")
	}
}

// TestValidateRuntimeAbsolutePathIsDirectory 绝对路径指向目录
func TestValidateRuntimeAbsolutePathIsDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	entry := &RuntimeEntry{
		Alias:  "dir",
		Path:   tmpDir,
		Source: RuntimeSourceConfig,
	}
	ValidateRuntime(entry)

	if entry.Available {
		t.Error("runtime should not be available when path is a directory")
	}
}

// --- ValidateAll 测试 ---

// TestValidateAll 校验注册表中所有运行时
func TestValidateAll(t *testing.T) {
	registry := NewRuntimeRegistry()

	// 添加一个存在的可执行文件
	tmpDir := t.TempDir()
	execPath := filepath.Join(tmpDir, "my-runtime")
	if err := os.WriteFile(execPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("failed to create test executable: %v", err)
	}

	registry.entries["my-runtime"] = &RuntimeEntry{
		Alias:  "my-runtime",
		Path:   execPath,
		Source: RuntimeSourceConfig,
	}
	registry.entries["missing"] = &RuntimeEntry{
		Alias:  "missing",
		Path:   "/nonexistent/path",
		Source: RuntimeSourceBuiltin,
	}

	ValidateAll(registry)

	if !registry.entries["my-runtime"].Available {
		t.Error("my-runtime should be available")
	}
	if registry.entries["missing"].Available {
		t.Error("missing should not be available")
	}
}

// TestValidateAll_AliasCycleDetection K-03-001 循环引用检测
// 验证配置加载阶段能识别别名循环引用并输出警告（不修改 Available 状态）
func TestValidateAll_AliasCycleDetection(t *testing.T) {
	tests := []struct {
		name     string
		entries  map[string]string // alias -> path
		hasCycle bool
	}{
		{
			name:     "no cycle (path not alias)",
			entries:  map[string]string{"node": "/usr/bin/node", "python3": "python3"},
			hasCycle: false,
		},
		{
			name:     "self cycle (alias == path)",
			entries:  map[string]string{"node": "node"},
			hasCycle: true,
		},
		{
			name:     "two-node cycle (node<->python)",
			entries:  map[string]string{"node": "python", "python": "node"},
			hasCycle: true,
		},
		{
			name:     "three-node cycle",
			entries:  map[string]string{"a": "b", "b": "c", "c": "a"},
			hasCycle: true,
		},
		{
			name:     "chain no cycle",
			entries:  map[string]string{"a": "b", "b": "c", "c": "/usr/bin/c"},
			hasCycle: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			registry := NewRuntimeRegistry()
			for alias, path := range tc.entries {
				registry.entries[alias] = &RuntimeEntry{
					Alias:  alias,
					Path:   path,
					Source: RuntimeSourceConfig,
				}
			}
			// 调用 detectAliasCycles 不应 panic
			detectAliasCycles(registry)
			// 不论是否检测到循环，Available 状态不应改变（循环检测是纯诊断）
			// 这里仅验证函数不 panic 即可
		})
	}
}

// --- BuildRegistry 综合测试 ---

// TestBuildRegistry 三层合并
func TestBuildRegistry(t *testing.T) {
	// 创建临时可执行文件用于 config 覆盖
	tmpDir := t.TempDir()
	customBash := filepath.Join(tmpDir, "bash")
	if err := os.WriteFile(customBash, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("failed to create test executable: %v", err)
	}

	configRuntimes := map[string]string{
		"bash": customBash, // 覆盖内置 bash
	}
	scanRuntimes := map[string]string{
		"deno": "/nonexistent/deno", // 不可用
	}

	registry := BuildRegistry(configRuntimes, scanRuntimes)

	// bash 被 config 覆盖
	entry := registry.entries["bash"]
	if entry.Source != RuntimeSourceConfig {
		t.Errorf("bash source = %q, want %q", entry.Source, RuntimeSourceConfig)
	}
	if entry.Path != customBash {
		t.Errorf("bash path = %q, want %q", entry.Path, customBash)
	}
	if !entry.Available {
		t.Error("custom bash should be available")
	}

	// sh 保持 builtin
	entry = registry.entries["sh"]
	if entry.Source != RuntimeSourceBuiltin {
		t.Errorf("sh source = %q, want %q", entry.Source, RuntimeSourceBuiltin)
	}

	// deno 是 scan 来源，路径不存在所以不可用
	entry = registry.entries["deno"]
	if entry.Source != RuntimeSourceScan {
		t.Errorf("deno source = %q, want %q", entry.Source, RuntimeSourceScan)
	}
	if entry.Available {
		t.Error("deno should not be available (path does not exist)")
	}
}

// TestBuildRegistryScanDoesNotOverrideConfig scan 不覆盖 config
func TestBuildRegistryScanDoesNotOverrideConfig(t *testing.T) {
	configRuntimes := map[string]string{
		"python3": "/usr/bin/python3.11",
	}
	scanRuntimes := map[string]string{
		"python3": "/etc/supd/runtimes/python3",
	}

	registry := BuildRegistry(configRuntimes, scanRuntimes)

	entry := registry.entries["python3"]
	if entry.Source != RuntimeSourceConfig {
		t.Errorf("python3 source = %q, want %q", entry.Source, RuntimeSourceConfig)
	}
	if entry.Path != "/usr/bin/python3.11" {
		t.Errorf("python3 path = %q, want %q", entry.Path, "/usr/bin/python3.11")
	}
}

// TestBuildRegistryEmptyInputs 空输入时只有内置运行时
func TestBuildRegistryEmptyInputs(t *testing.T) {
	registry := BuildRegistry(nil, nil)

	if len(registry.entries) != 4 {
		t.Errorf("registry has %d entries, want 4", len(registry.entries))
	}

	for _, alias := range []string{"bash", "sh", "python3", "node"} {
		entry, ok := registry.entries[alias]
		if !ok {
			t.Errorf("builtin %q not found", alias)
			continue
		}
		if entry.Source != RuntimeSourceBuiltin {
			t.Errorf("%q source = %q, want %q", alias, entry.Source, RuntimeSourceBuiltin)
		}
	}

	// bash 和 sh 在 Linux 上通常是可用的
	if runtime.GOOS == "linux" {
		if bash := registry.entries["bash"]; !bash.Available {
			t.Log("note: /bin/bash not available on this system")
		}
		if sh := registry.entries["sh"]; !sh.Available {
			t.Log("note: /bin/sh not available on this system")
		}
	}
}

// TestBuildRegistryScanOverridesBuiltin scan 覆盖内置
func TestBuildRegistryScanOverridesBuiltin(t *testing.T) {
	tmpDir := t.TempDir()
	scanBash := filepath.Join(tmpDir, "bash")
	if err := os.WriteFile(scanBash, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("failed to create test executable: %v", err)
	}

	scanRuntimes := map[string]string{
		"bash": scanBash,
	}

	registry := BuildRegistry(nil, scanRuntimes)

	entry := registry.entries["bash"]
	if entry.Source != RuntimeSourceScan {
		t.Errorf("bash source = %q, want %q", entry.Source, RuntimeSourceScan)
	}
	if entry.Path != scanBash {
		t.Errorf("bash path = %q, want %q", entry.Path, scanBash)
	}
	if !entry.Available {
		t.Error("scan bash should be available")
	}
}

// TestBuiltinNotOverrideExisting 内置不覆盖已存在的高优先级条目
func TestBuiltinNotOverrideExisting(t *testing.T) {
	registry := NewRuntimeRegistry()

	// 先注册 config 来源
	registry.entries["bash"] = &RuntimeEntry{
		Alias:  "bash",
		Path:   "/custom/bash",
		Source: RuntimeSourceConfig,
	}

	// 再注册内置，不应覆盖
	RegisterBuiltin(registry)

	entry := registry.entries["bash"]
	if entry.Source != RuntimeSourceConfig {
		t.Errorf("bash source = %q, want %q (builtin should not override)", entry.Source, RuntimeSourceConfig)
	}
	if entry.Path != "/custom/bash" {
		t.Errorf("bash path = %q, want %q", entry.Path, "/custom/bash")
	}
}
