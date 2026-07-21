package api

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/supdorg/supd/internal/errors"
)

// PathValidator 文件路径校验器。
// 保留 baseDir 边界检查（防路径穿越），baseDir 下所有文件自由访问。
// extraAllowed 路径（如日志目录）为只读。
type PathValidator struct {
	baseDir      string   // 基础目录，默认 /etc/supd/
	extraAllowed []string // 额外允许的绝对路径前缀（如日志目录，只读）
}

// NewPathValidator 创建路径校验器。
func NewPathValidator(baseDir string) *PathValidator {
	if baseDir == "" {
		baseDir = "/etc/supd/"
	}
	if !strings.HasSuffix(baseDir, "/") {
		baseDir += "/"
	}

	return &PathValidator{
		baseDir: baseDir,
	}
}

// AddAllowedPath 添加额外允许的绝对路径前缀
// 用于将日志目录等外部路径加入白名单，设为只读
func (v *PathValidator) AddAllowedPath(absPath string) {
	absPath = filepath.Clean(absPath)
	if !strings.HasSuffix(absPath, "/") {
		absPath += "/"
	}
	v.extraAllowed = append(v.extraAllowed, absPath)
}

// Validate 校验路径是否在允许范围内。
// 保留 baseDir 边界检查（防路径穿越），baseDir 下所有文件自由访问。
// 返回清理后的绝对路径或错误。
func (v *PathValidator) Validate(requestedPath string) (string, error) {
	// 1. 如果是相对路径，基于 baseDir 转绝对路径
	var absPath string
	if filepath.IsAbs(requestedPath) {
		absPath = filepath.Clean(requestedPath)
	} else {
		absPath = filepath.Clean(filepath.Join(v.baseDir, requestedPath))
	}

	// 2. 检查路径中不含 ".."
	// H-03-004 评估说明：strings.Contains(requestedPath, "..") 是宽松检查，
	// 会误拒 foo..bar.txt 等合法文件名。但审计建议的逐段检查会破坏现有
	// URL 编码攻击向量测试（services/..%2Fetc%2Fpasswd 等）。
	// 综合 AGENTS.md "最小化修改" 原则和测试覆盖完整性，保留原检查。
	// baseDir 边界检查 + EvalSymlinks 已提供主防御，此检查为额外保险。
	if strings.Contains(requestedPath, "..") {
		return "", errors.NewServiceError(errors.ErrInvalidRequest,
			fmt.Sprintf("path must not contain '..': %s", requestedPath))
	}

	// 3. 检查路径在 baseDir 下，或在额外允许的绝对路径前缀内
	if !strings.HasPrefix(absPath, v.baseDir) {
		for _, prefix := range v.extraAllowed {
			if strings.HasPrefix(absPath, prefix) {
				// 符号链接检查
				if resolved, err := filepath.EvalSymlinks(absPath); err == nil && resolved != absPath {
					if !strings.HasPrefix(resolved, prefix) {
						return "", errors.NewServiceError(errors.ErrFileAccessDenied,
							fmt.Sprintf("symlink resolves outside allowed directory: %s -> %s", requestedPath, resolved))
					}
					absPath = resolved
				}
				return absPath, nil
			}
		}
		return "", errors.NewServiceError(errors.ErrFileAccessDenied,
			fmt.Sprintf("path is outside allowed base directory: %s", requestedPath))
	}

	// H-03-003 修复：拒绝访问根目录的 config.yaml（含 auth_token 等敏感信息）
	// 仅精确匹配 baseDir/config.yaml，不影响 services/*/config.yaml 等
	rootConfigPath := filepath.Join(v.baseDir, "config.yaml")
	if absPath == rootConfigPath {
		return "", errors.NewServiceError(errors.ErrFileAccessDenied,
			"access to root config.yaml is forbidden")
	}

	// 4. 符号链接检查：防止 symlink 逃逸 baseDir
	if resolved, err := filepath.EvalSymlinks(absPath); err == nil && resolved != absPath {
		if !strings.HasPrefix(resolved, v.baseDir) {
			return "", errors.NewServiceError(errors.ErrFileAccessDenied,
				fmt.Sprintf("symlink resolves outside allowed base directory: %s -> %s", requestedPath, resolved))
		}
		absPath = resolved
	}

	return absPath, nil
}

// IsReadOnly 检查路径是否为只读。
// extraAllowed 路径（如日志目录）为只读
func (v *PathValidator) IsReadOnly(absPath string) bool {
	for _, prefix := range v.extraAllowed {
		if strings.HasPrefix(absPath, prefix) {
			return true
		}
	}
	return false
}

// ValidateWritePath 校验写路径（排除只读路径）。
func (v *PathValidator) ValidateWritePath(requestedPath string) (string, error) {
	absPath, err := v.Validate(requestedPath)
	if err != nil {
		return "", err
	}

	if v.IsReadOnly(absPath) {
		return "", errors.NewServiceError(errors.ErrFileAccessDenied,
			fmt.Sprintf("path is read-only: %s", requestedPath))
	}

	return absPath, nil
}
