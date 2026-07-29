package api

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/supdorg/supd/internal/errors"
)

type pathTier int

const (
	tierForbidden pathTier = iota
	tierReadOnly
	tierReadWrite
)

var readWriteSubdirs = map[string]struct{}{
	"services":   {},
	"extensions": {},
	"env":        {},
	"assets":     {},
}

var readOnlySubdirs = map[string]struct{}{
	"runtimes": {},
}

// PathValidator 文件路径校验器。
// 文件 API 仅允许访问显式登记的顶层目录；额外绝对路径只读。
type PathValidator struct {
	baseDir      string
	extraAllowed []string
}

// NewPathValidator 创建路径校验器。
func NewPathValidator(baseDir string) *PathValidator {
	if baseDir == "" {
		baseDir = "/etc/supd"
	}
	abs, err := filepath.Abs(baseDir)
	if err == nil {
		baseDir = abs
	}
	return &PathValidator{baseDir: filepath.Clean(baseDir)}
}

// AddAllowedPath 添加额外允许的只读绝对目录。
func (v *PathValidator) AddAllowedPath(absPath string) {
	abs, err := filepath.Abs(absPath)
	if err != nil {
		return
	}
	v.extraAllowed = append(v.extraAllowed, filepath.Clean(abs))
}

func pathWithin(base, path string) bool {
	rel, err := filepath.Rel(base, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (v *PathValidator) tierOf(absPath string) pathTier {
	absPath = filepath.Clean(absPath)
	if pathWithin(v.baseDir, absPath) {
		rel, err := filepath.Rel(v.baseDir, absPath)
		if err != nil {
			return tierForbidden
		}
		if rel == "." {
			return tierReadWrite
		}
		first := strings.Split(filepath.ToSlash(rel), "/")[0]
		if _, ok := readWriteSubdirs[first]; ok {
			return tierReadWrite
		}
		if _, ok := readOnlySubdirs[first]; ok {
			return tierReadOnly
		}
		return tierForbidden
	}
	for _, allowed := range v.extraAllowed {
		if pathWithin(allowed, absPath) {
			return tierReadOnly
		}
	}
	return tierForbidden
}

// safeResolve 解析目标或最近存在祖先的符号链接，防止不存在目标借父目录 symlink 逃逸。
func (v *PathValidator) safeResolve(absPath string) (string, error) {
	probe := absPath
	var tail []string
	for {
		resolved, err := filepath.EvalSymlinks(probe)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			resolved = filepath.Clean(resolved)
			if v.tierOf(resolved) == tierForbidden {
				return "", errors.NewServiceError(errors.ErrFileAccessDenied,
					fmt.Sprintf("symlink resolves outside allowed paths: %s -> %s", absPath, resolved))
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", errors.NewServiceError(errors.ErrFileAccessDenied,
				fmt.Sprintf("cannot resolve path: %s: %v", absPath, err))
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", errors.NewServiceError(errors.ErrFileAccessDenied,
				fmt.Sprintf("cannot resolve path ancestry: %s", absPath))
		}
		tail = append(tail, filepath.Base(probe))
		probe = parent
	}
}

// Validate 校验读路径并返回解析后的绝对路径。
func (v *PathValidator) Validate(requestedPath string) (string, error) {
	if strings.Contains(requestedPath, "..") {
		return "", errors.NewServiceError(errors.ErrInvalidRequest,
			fmt.Sprintf("path must not contain '..': %s", requestedPath))
	}

	var absPath string
	if filepath.IsAbs(requestedPath) {
		absPath = filepath.Clean(requestedPath)
	} else {
		absPath = filepath.Clean(filepath.Join(v.baseDir, requestedPath))
	}
	if v.tierOf(absPath) == tierForbidden {
		return "", errors.NewServiceError(errors.ErrFileAccessDenied,
			fmt.Sprintf("access to path is forbidden: %s", requestedPath))
	}

	resolved, err := v.safeResolve(absPath)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

// IsReadOnly 检查已解析路径是否只读。
func (v *PathValidator) IsReadOnly(absPath string) bool {
	return v.tierOf(absPath) == tierReadOnly
}

// ValidateWritePath 校验写路径。
func (v *PathValidator) ValidateWritePath(requestedPath string) (string, error) {
	absPath, err := v.Validate(requestedPath)
	if err != nil {
		return "", err
	}
	if v.tierOf(absPath) != tierReadWrite || absPath == v.baseDir {
		return "", errors.NewServiceError(errors.ErrFileAccessDenied,
			fmt.Sprintf("path is read-only or forbidden: %s", requestedPath))
	}
	return absPath, nil
}
