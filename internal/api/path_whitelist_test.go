package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/supdorg/supd/internal/errors"
)

func TestPathValidator_ServicesDir(t *testing.T) {
	v := NewPathValidator("/etc/supd")

	absPath, err := v.Validate("services/myservice/service.yaml")
	if err != nil {
		t.Fatalf("expected services/ to be accessible, got error: %v", err)
	}
	expected := filepath.Join("/etc/supd", "services/myservice/service.yaml")
	if absPath != expected {
		t.Errorf("expected %s, got %s", expected, absPath)
	}
}

func TestPathValidator_ExtensionsDir(t *testing.T) {
	v := NewPathValidator("/etc/supd")

	absPath, err := v.Validate("extensions/myext/extension.yaml")
	if err != nil {
		t.Fatalf("expected extensions/ to be accessible, got error: %v", err)
	}
	expected := filepath.Join("/etc/supd", "extensions/myext/extension.yaml")
	if absPath != expected {
		t.Errorf("expected %s, got %s", expected, absPath)
	}
}

func TestPathValidator_EnvDir(t *testing.T) {
	v := NewPathValidator("/etc/supd")

	absPath, err := v.Validate("env/myenv/env.yaml")
	if err != nil {
		t.Fatalf("expected env/ to be accessible, got error: %v", err)
	}
	expected := filepath.Join("/etc/supd", "env/myenv/env.yaml")
	if absPath != expected {
		t.Errorf("expected %s, got %s", expected, absPath)
	}
}

func TestPathValidator_AssetsDir(t *testing.T) {
	v := NewPathValidator("/etc/supd")

	absPath, err := v.Validate("assets/icon.png")
	if err != nil {
		t.Fatalf("expected assets/ to be accessible, got error: %v", err)
	}
	expected := filepath.Join("/etc/supd", "assets/icon.png")
	if absPath != expected {
		t.Errorf("expected %s, got %s", expected, absPath)
	}
}

func TestPathValidator_ConfigYaml(t *testing.T) {
	v := NewPathValidator("/etc/supd")

	// H-03-003 修复：根目录 config.yaml 含 auth_token 等敏感信息，禁止通过文件 API 访问
	_, err := v.Validate("config.yaml")
	if err == nil {
		t.Fatal("expected config.yaml to be blocked, got nil error")
	}
	svcErr, ok := err.(*errors.ServiceError)
	if !ok {
		t.Fatalf("expected *errors.ServiceError, got %T", err)
	}
	if svcErr.Code != errors.ErrFileAccessDenied {
		t.Errorf("expected error code %s, got %s", errors.ErrFileAccessDenied, svcErr.Code)
	}

	// 子目录中的 config.yaml 不应被阻止（如 services/*/config.yaml）
	absPath, err := v.Validate("services/my-service/config.yaml")
	if err != nil {
		t.Fatalf("expected services/my-service/config.yaml to be accessible, got error: %v", err)
	}
	expected := filepath.Join("/etc/supd", "services", "my-service", "config.yaml")
	if absPath != expected {
		t.Errorf("expected %s, got %s", expected, absPath)
	}
}

func TestPathValidator_LogDirBlocked(t *testing.T) {
	v := NewPathValidator("/etc/supd")

	_, err := v.Validate("/var/log/supd/test.log")
	if err == nil {
		t.Fatal("expected /var/log/supd/ to be blocked, got nil error")
	}
	// 验证错误码
	svcErr, ok := err.(*errors.ServiceError)
	if !ok {
		t.Fatalf("expected *errors.ServiceError, got %T", err)
	}
	if svcErr.Code != errors.ErrFileAccessDenied {
		t.Errorf("expected error code %s, got %s", errors.ErrFileAccessDenied, svcErr.Code)
	}
}

func TestPathValidator_VarLibBlocked(t *testing.T) {
	v := NewPathValidator("/etc/supd")

	_, err := v.Validate("/var/lib/supd/state.json")
	if err == nil {
		t.Fatal("expected /var/lib/supd/ to be blocked, got nil error")
	}
	svcErr, ok := err.(*errors.ServiceError)
	if !ok {
		t.Fatalf("expected *errors.ServiceError, got %T", err)
	}
	if svcErr.Code != errors.ErrFileAccessDenied {
		t.Errorf("expected error code %s, got %s", errors.ErrFileAccessDenied, svcErr.Code)
	}
}

func TestPathValidator_PathTraversal(t *testing.T) {
	v := NewPathValidator("/etc/supd")

	_, err := v.Validate("../../../etc/passwd")
	if err == nil {
		t.Fatal("expected path traversal to be blocked, got nil error")
	}
	svcErr, ok := err.(*errors.ServiceError)
	if !ok {
		t.Fatalf("expected *errors.ServiceError, got %T", err)
	}
	if svcErr.Code != errors.ErrInvalidRequest {
		t.Errorf("expected error code %s, got %s", errors.ErrInvalidRequest, svcErr.Code)
	}
}

func TestPathValidator_DoubleDot(t *testing.T) {
	v := NewPathValidator("/etc/supd")

	testCases := []string{
		"services/..",
		"services/../config.yaml",
		"./../etc/passwd",
	}

	for _, tc := range testCases {
		_, err := v.Validate(tc)
		if err == nil {
			t.Errorf("expected path with '..' to be blocked: %s", tc)
		} else {
			svcErr, ok := err.(*errors.ServiceError)
			if !ok {
				t.Errorf("expected *errors.ServiceError for %s, got %T", tc, err)
				continue
			}
			if svcErr.Code != errors.ErrInvalidRequest {
				t.Errorf("expected error code %s for %s, got %s", errors.ErrInvalidRequest, tc, svcErr.Code)
			}
		}
	}
}

func TestPathValidator_AbsolutePath(t *testing.T) {
	v := NewPathValidator("/etc/supd")

	absPath, err := v.Validate("/etc/supd/services/myservice/service.yaml")
	if err != nil {
		t.Fatalf("expected absolute path in whitelist to be accessible, got error: %v", err)
	}
	expected := "/etc/supd/services/myservice/service.yaml"
	if absPath != expected {
		t.Errorf("expected %s, got %s", expected, absPath)
	}
}

func TestPathValidator_Subdirectory(t *testing.T) {
	v := NewPathValidator("/etc/supd")

	absPath, err := v.Validate("services/myservice/deep/nested/file.conf")
	if err != nil {
		t.Fatalf("expected subdirectory of whitelisted dir to be accessible, got error: %v", err)
	}
	expected := filepath.Join("/etc/supd", "services/myservice/deep/nested/file.conf")
	if absPath != expected {
		t.Errorf("expected %s, got %s", expected, absPath)
	}
}

// TestPathValidator_RuntimesWritable 验证 runtimes/ 目录现在可读可写
// L-01-001 修复：白名单简化后 runtimes/ 不再是只读，baseDir 下所有目录自由访问
func TestPathValidator_RuntimesWritable(t *testing.T) {
	v := NewPathValidator("/etc/supd")

	// runtimes/ 可读
	absPath, err := v.Validate("runtimes/node/config.json")
	if err != nil {
		t.Fatalf("expected runtimes/ to be readable, got error: %v", err)
	}
	expected := filepath.Join("/etc/supd", "runtimes/node/config.json")
	if absPath != expected {
		t.Errorf("expected %s, got %s", expected, absPath)
	}

	// runtimes/ 可写（不是只读）
	if v.IsReadOnly(absPath) {
		t.Error("expected runtimes/ to be writable, not read-only")
	}

	// 写入校验通过
	if _, err := v.ValidateWritePath("runtimes/node/config.json"); err != nil {
		t.Errorf("expected runtimes/ write to be allowed, got error: %v", err)
	}
}

// TestPathValidator_ExtraAllowedReadOnly 验证 extraAllowed 路径仍为只读
// 日志目录等外部路径通过 AddAllowedPath 加入，应被 IsReadOnly 识别
func TestPathValidator_ExtraAllowedReadOnly(t *testing.T) {
	v := NewPathValidator("/etc/supd")
	v.AddAllowedPath("/var/log/supd")

	// /var/log/supd/ 下可读
	absPath, err := v.Validate("/var/log/supd/supd.log")
	if err != nil {
		t.Fatalf("expected /var/log/supd/ to be readable via extraAllowed, got error: %v", err)
	}

	// IsReadOnly 应返回 true
	if !v.IsReadOnly(absPath) {
		t.Error("expected /var/log/supd/ to be read-only")
	}

	// 写入应被拒绝
	if _, err := v.ValidateWritePath("/var/log/supd/supd.log"); err == nil {
		t.Fatal("expected write to /var/log/supd/ to be blocked (read-only)")
	}
}

func TestPathValidator_IsReadOnly_WritablePath(t *testing.T) {
	v := NewPathValidator("/etc/supd")

	absPath, err := v.Validate("services/myservice/service.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.IsReadOnly(absPath) {
		t.Error("expected services/ to be writable, not read-only")
	}
}

func TestPathValidator_EmptyBaseDir(t *testing.T) {
	v := NewPathValidator("")

	absPath, err := v.Validate("services/myservice/service.yaml")
	if err != nil {
		t.Fatalf("expected default base dir to work, got error: %v", err)
	}
	expected := filepath.Join("/etc/supd", "services/myservice/service.yaml")
	if absPath != expected {
		t.Errorf("expected %s, got %s", expected, absPath)
	}
}

// TestPathValidator_URLEncodedPathTraversal 测试URL编码的路径穿越
// HTTP框架（chi/Go标准库）会自动解码URL查询参数，因此
// PathValidator接收到的路径已经是解码后的。此测试验证
// 解码后的路径穿越仍被正确拦截。
func TestPathValidator_URLEncodedPathTraversal(t *testing.T) {
	v := NewPathValidator("/etc/supd")

	// 模拟HTTP框架URL解码后的路径穿越变体
	testCases := []struct {
		name        string
		path        string // 模拟URL解码后传入Validate的路径
		shouldBlock bool
	}{
		{
			name:        "decoded %2E%2E%2F = ../",
			path:        "services/../../../etc/passwd",
			shouldBlock: true,
		},
		{
			name:        "decoded %2e%2e%2f = ../",
			path:        "../etc/passwd",
			shouldBlock: true,
		},
		{
			name:        "decoded %2E%2E/ = ../",
			path:        "services/../../etc/passwd",
			shouldBlock: true,
		},
		{
			name:        "mixed encoding ..%2F",
			path:        "services/..%2Fetc%2Fpasswd",
			shouldBlock: true, // contains ".."
		},
		{
			name:        "dot-dot with backslash",
			path:        "services\\..\\..\\etc\\passwd",
			shouldBlock: true, // contains ".."
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := v.Validate(tc.path)
			if tc.shouldBlock && err == nil {
				t.Errorf("expected path to be blocked: %s", tc.path)
			}
			if !tc.shouldBlock && err != nil {
				t.Errorf("expected path to be allowed: %s, got error: %v", tc.path, err)
			}
		})
	}
}

// TestPathValidator_DoubleEncodedPathTraversal 测试双重URL编码的路径
// HTTP框架只做一次URL解码，双重编码的%2E%2E%2F不会被解码为../。
// 这些路径不会构成真实的路径穿越，因为：
// 1. 文件系统不会将 %2E%2E%2F 解释为 ../
// 2. filepath.Clean 不会处理它们
// 3. 它们落在 baseDir 下（路径前缀匹配通过）
// 所以这些路径会被允许，文件名将包含字面 % 字符
func TestPathValidator_DoubleEncodedPathTraversal(t *testing.T) {
	v := NewPathValidator("/etc/supd")

	// 双重编码：框架解码一次后仍为 %2E%2E%2F，不是 ..
	// 这些路径不构成真实穿越，且落在 baseDir 下，应被允许
	testCases := []struct {
		name        string
		path        string // 模拟框架解码一次后传入Validate的路径
		shouldBlock bool
		reason      string
	}{
		{
			name:        "double encoded %252E%252E%252F → %2E%2E%2F after decode",
			path:        "%2E%2E%2Fetc%2Fpasswd",
			shouldBlock: false, // 字面 % 字符，落在 baseDir 下，无害
			reason:      "literal % chars, no traversal",
		},
		{
			name:        "double encoded lowercase %252e%252e%252f → %2e%2e%2f after decode",
			path:        "%2e%2e%2fetc%2fpasswd",
			shouldBlock: false,
			reason:      "literal % chars, no traversal",
		},
		{
			name:        "partial double encoded %252E%252E/ → %2E%2E/ after decode",
			path:        "%2E%2E/etc/passwd",
			shouldBlock: false,
			reason:      "literal % chars, no traversal",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := v.Validate(tc.path)
			if tc.shouldBlock && err == nil {
				t.Errorf("expected path to be blocked (%s): %s", tc.reason, tc.path)
			}
			if !tc.shouldBlock && err != nil {
				t.Errorf("expected path to be allowed (%s): %s, got error: %v", tc.reason, tc.path, err)
			}
		})
	}
}

// TestPathValidator_NullByteInjection D-04-001: 验证 null byte 注入攻击被正确拒绝。
// null byte (\x00) 在某些 C 库文件系统交互中会截断路径，可能导致路径校验被绕过。
// Go 的 filepath.Clean 会保留 null byte，但 strings.Contains(path, "..") 检查仍能捕获
// 包含 ".." 的恶意路径。本测试覆盖多种 null byte 注入向量。
func TestPathValidator_NullByteInjection(t *testing.T) {
	v := NewPathValidator("/etc/supd")

	testCases := []struct {
		name        string
		path        string
		shouldBlock bool
		reason      string
	}{
		{
			name:        "null byte before traversal",
			path:        "services\x00/../../../etc/passwd",
			shouldBlock: true,
			reason:      "contains '..' after null byte, should be blocked by .. check",
		},
		{
			name:        "null byte between dots",
			path:        "services/.\x00./../etc/passwd",
			shouldBlock: true,
			reason:      "contains '..' (with embedded null), should be blocked",
		},
		{
			name:        "null byte in middle of path",
			path:        "services/myservice\x00/service.yaml",
			shouldBlock: false,
			reason:      "null byte in filename, no '..', falls under baseDir (filesystem will reject on access)",
		},
		{
			name:        "URL encoded null byte %00",
			path:        "services/%00../../../etc/passwd",
			shouldBlock: true,
			reason:      "contains '..' after URL-encoded null byte",
		},
		{
			name:        "null byte at end",
			path:        "services/myservice/service.yaml\x00",
			shouldBlock: false,
			reason:      "trailing null byte, no '..', falls under baseDir (filesystem rejects)",
		},
		{
			name:        "null byte followed by absolute path",
			path:        "services\x00/etc/passwd",
			shouldBlock: false,
			reason:      "null byte in filename, no '..', path resolves under baseDir",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := v.Validate(tc.path)
			if tc.shouldBlock && err == nil {
				t.Errorf("expected path to be blocked (%s): %q", tc.reason, tc.path)
			}
			if !tc.shouldBlock && err != nil {
				t.Errorf("expected path to be allowed (%s): %q, got error: %v", tc.reason, tc.path, err)
			}
		})
	}
}

// TestPathValidator_SymlinkEscape D-04-002: 验证符号链接逃逸攻击被正确拒绝。
// 创建实际的符号链接指向 baseDir 外的文件，验证 PathValidator 的 symlink 检查有效。
func TestPathValidator_SymlinkEscape(t *testing.T) {
	// 创建临时 baseDir
	baseDir := t.TempDir()
	servicesDir := filepath.Join(baseDir, "services", "myservice")
	if err := os.MkdirAll(servicesDir, 0755); err != nil {
		t.Fatalf("mkdir services dir: %v", err)
	}

	// 在 baseDir 外创建目标文件
	targetFile := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(targetFile, []byte("secret"), 0644); err != nil {
		t.Fatalf("write target file: %v", err)
	}

	// 在 services/myservice/ 下创建符号链接指向外部文件
	linkyPath := filepath.Join(servicesDir, "linky")
	if err := os.Symlink(targetFile, linkyPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	v := NewPathValidator(baseDir)

	// 通过相对路径访问符号链接，应被拒绝（解析后逃逸 baseDir）
	_, err := v.Validate("services/myservice/linky")
	if err == nil {
		t.Errorf("expected symlink escape to be blocked, but Validate allowed access")
	} else {
		// 验证返回的是 FileAccessDenied 错误
		se, ok := err.(*errors.ServiceError)
		if !ok || se.Code != errors.ErrFileAccessDenied {
			t.Errorf("expected ErrFileAccessDenied, got %v", err)
		}
	}
}

// TestPathValidator_SymlinkWithinBaseDir D-04-002 补充: 验证 baseDir 内的符号链接被允许。
// 符号链接指向 baseDir 内的另一个文件，应被允许访问。
func TestPathValidator_SymlinkWithinBaseDir(t *testing.T) {
	baseDir := t.TempDir()
	servicesDir := filepath.Join(baseDir, "services", "myservice")
	if err := os.MkdirAll(servicesDir, 0755); err != nil {
		t.Fatalf("mkdir services dir: %v", err)
	}

	// 在 baseDir 内创建目标文件
	targetFile := filepath.Join(servicesDir, "real.yaml")
	if err := os.WriteFile(targetFile, []byte("data"), 0644); err != nil {
		t.Fatalf("write target file: %v", err)
	}

	// 在同一目录创建符号链接指向 real.yaml
	linkyPath := filepath.Join(servicesDir, "linky.yaml")
	if err := os.Symlink("real.yaml", linkyPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	v := NewPathValidator(baseDir)

	// 访问符号链接，应被允许（解析后仍在 baseDir 内）
	absPath, err := v.Validate("services/myservice/linky.yaml")
	if err != nil {
		t.Errorf("expected symlink within baseDir to be allowed, got error: %v", err)
	}
	// 验证返回的是解析后的真实路径
	expectedResolved, _ := filepath.EvalSymlinks(linkyPath)
	if absPath != expectedResolved {
		t.Errorf("expected resolved path %s, got %s", expectedResolved, absPath)
	}
}

// TestPathValidator_NonExistentSymlink D-04-002 补充: 验证不存在的路径（新文件创建场景）。
// filepath.EvalSymlinks 对不存在的路径返回 error，代码走 err != nil 分支跳过 symlink 检查。
// 这是设计上的选择：新文件创建时无法预先检查 symlink，但 filepath.Join/Clean + ".." 检查
// 仍能防止路径穿越。本测试验证不存在的路径能被正常校验通过。
func TestPathValidator_NonExistentSymlink(t *testing.T) {
	baseDir := t.TempDir()
	v := NewPathValidator(baseDir)

	// 不存在的新文件路径，应被允许（用于创建新文件）
	absPath, err := v.Validate("services/newsvc/newfile.yaml")
	if err != nil {
		t.Errorf("expected non-existent path to be allowed for creation, got error: %v", err)
	}
	expected := filepath.Clean(filepath.Join(baseDir, "services/newsvc/newfile.yaml"))
	if absPath != expected {
		t.Errorf("expected path %s, got %s", expected, absPath)
	}
}
