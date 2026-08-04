package core

import (
	"path/filepath"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/watch"
)

// ResolveWorkdir 将服务配置的 workdir 解析为绝对路径，供进程启动和 readiness 检查共用。
//
// 解析规则：
//   - workdir 为空 → 返回服务根目录（service.yaml 所在目录，即 filepath.Dir(svcEntry.ConfigPath)）
//   - workdir 为绝对路径 → filepath.Clean 后原样返回
//   - workdir 为相对路径 → 基于服务根目录拼接后 Clean
//
// 路径穿越安全性：filepath.Join/Clean 会规范化 ".." 但不阻止逃逸到服务根目录之外。
// 这是允许的——服务进程 CWD 即使逃逸也受 baseDir 边界约束（文件操作 API 有 baseDir 边界校验）。
// 此处只负责把 workdir 解析成可执行的绝对路径，不做安全策略。
//
// 此前 4 处重复实现（bootstrap.startService / supervisor.doRestartProcess /
// service_operator.startServiceLocked / RunReadiness 回调）行为一致，
// 统一抽取为单点函数避免逻辑漂移。
func ResolveWorkdir(svcConfig *config.ServiceConfig, svcEntry *watch.ServiceEntry) string {
	root := filepath.Dir(svcEntry.ConfigPath)
	if svcConfig.Workdir == "" {
		return root
	}
	if filepath.IsAbs(svcConfig.Workdir) {
		return filepath.Clean(svcConfig.Workdir)
	}
	return filepath.Join(root, svcConfig.Workdir)
}
