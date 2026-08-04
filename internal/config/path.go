package config

import "path/filepath"

// ResolvePath 将配置中的路径解析为绝对路径。
// 绝对路径仅清理；相对路径以所属配置层级的根目录为基准。
func ResolvePath(root, path string) string {
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(root, path))
}
