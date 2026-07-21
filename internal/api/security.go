package api

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/supdorg/supd/internal/errors"
)

// shellMetaChars 需要拦截的shell元字符
// REQ-E-004: 命令注入防护
var shellMetaChars = map[rune]bool{
	';': true,
	'|': true,
	'&': true,
	'$': true,
	'`': true,
	'(': true,
	')': true,
	'{': true,
	'}': true,
	'\n': true,
	'\r': true,
}

// serviceNamePattern 服务名正则
// REQ-D-007: ^[a-z][a-z0-9-]*$ — 与 internal/config/service_validate.go 保持一致
var serviceNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// isValidServiceName 校验服务名是否符合规格 REQ-D-007
// 用于 API 入口防止路径穿越和路由污染
func isValidServiceName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	return serviceNamePattern.MatchString(name)
}

// ValidateEntryPath 校验扩展entry路径安全性
// REQ-E-004: 命令注入防护 — 不含..、路径分隔符之外的非法字符、shell元字符
func ValidateEntryPath(entry string) error {
	if entry == "" {
		return errors.NewServiceError(errors.ErrInvalidRequest, "entry path must not be empty")
	}

	// 不含 ..
	if strings.Contains(entry, "..") {
		return errors.NewServiceError(errors.ErrInvalidRequest,
			fmt.Sprintf("entry path must not contain '..': %s", entry))
	}

	// 不含 shell 元字符
	for _, ch := range entry {
		if shellMetaChars[ch] {
			return errors.NewServiceError(errors.ErrInvalidRequest,
				fmt.Sprintf("entry path contains invalid character '%c': %s", ch, entry))
		}
	}

	// 清理后与原始值一致
	cleaned := filepath.Clean(entry)
	if cleaned != entry {
		return errors.NewServiceError(errors.ErrInvalidRequest,
			fmt.Sprintf("entry path contains redundant path components: %s", entry))
	}

	return nil
}

// SanitizeFilename 清理文件名，移除路径组件
// REQ-E-006: 上传文件安全 — 文件名清理
func SanitizeFilename(name string) string {
	name = filepath.Base(name)
	// 移除前导和尾随的空格和点号（如 "." 或 ".."）
	name = strings.Trim(name, ". ")
	return name
}

// IsPathInBase 检查绝对路径是否在基础目录下
// REQ-E-002: 路径穿越防护
func IsPathInBase(path string, baseDir string) bool {
	if path == "" || baseDir == "" {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return false
	}
	sep := string(filepath.Separator)
	return strings.HasPrefix(absPath, absBase+sep) || absPath == absBase
}

// ValidateRunAsUser 校验 run_as 用户名安全性
// REQ-E-005: 扩展执行安全 — run_as非root限制
func ValidateRunAsUser(runAs string) error {
	// 空字符串表示使用默认用户，合法
	if runAs == "" {
		return nil
	}

	// 不允许 root
	if runAs == "root" {
		return errors.NewServiceError(errors.ErrRuntimeUserNotFound,
			"run_as 'root' is not allowed for security reasons")
	}

	// 不含特殊字符
	for _, ch := range runAs {
		if ch < 32 || ch > 126 {
			return errors.NewServiceError(errors.ErrRuntimeUserNotFound,
				"run_as contains non-printable character")
		}
		switch ch {
		case '/', '\\', ':', ';', '|', '&', '$', '`', '(', ')', '{', '}':
			return errors.NewServiceError(errors.ErrRuntimeUserNotFound,
				fmt.Sprintf("run_as contains invalid character '%c'", ch))
		}
	}

	return nil
}
