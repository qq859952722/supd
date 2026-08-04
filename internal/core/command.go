package core

import (
	"path/filepath"
	"strings"

	"github.com/supdorg/supd/internal/config"
)

// ResolveCommandPath 解析命令数组中的可执行文件路径。
// 绝对路径保持绝对；包含路径分隔符的相对路径基于配置所属根目录解析；
// 不含分隔符的命令名保留给 PATH 查找。
func ResolveCommandPath(root string, command []string) []string {
	if len(command) == 0 || command[0] == "" {
		return command
	}
	if !filepath.IsAbs(command[0]) && !strings.ContainsAny(command[0], `/\\`) {
		return command
	}
	resolved := append([]string(nil), command...)
	resolved[0] = config.ResolvePath(root, command[0])
	return resolved
}
