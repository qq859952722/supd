package core

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/supdorg/supd/internal/config"
)

// BuildServiceProcessEnv 构建服务进程的环境变量切片。
//
// 规格 §2.2.4 环境变量层级合并 — 服务进程使用前 3 层（扩展私有 env 仅扩展执行时注入）：
//  1. supd 自身环境变量（os.Environ()，作为底层不被清除）
//  2. 全局 env 文件（按 cfg.EnvFiles 顺序加载，后者覆盖前者）
//  3. 服务 env（services/<svc>/env.yaml）
//
// enabled:false 的变量不注入；同名变量后者覆盖前者（包括 enabled 状态）。
//
// REQ-D-006: env_files 不影响已运行服务；新启动的服务用新 env
// REQ-F-027: env.yaml（服务）→ 需重启服务生效
//
// 公开供 api 包（CoreServiceOperator）在 API 启动/重启服务时复用同一 env 构造逻辑。
func BuildServiceProcessEnv(baseDir, serviceName string, envFiles []string) []string {
	env := os.Environ()

	// 收集 env.yaml 层（全局 + 服务）
	var layers []*config.EnvFile

	// Layer 2: 全局 env 文件（按 cfg.EnvFiles 顺序加载，后者覆盖前者）
	for _, relPath := range envFiles {
		if relPath == "" {
			continue
		}
		p := relPath
		if !filepath.IsAbs(p) {
			p = filepath.Join(baseDir, relPath)
		}
		ef, err := config.LoadEnv(p)
		if err != nil {
			// 文件不存在是正常情况（用户可能未创建该 env 文件），静默跳过
			// 解析失败等其他错误记录警告，便于用户诊断
			if !errors.Is(err, os.ErrNotExist) {
				slog.Warn("load global env file failed", "path", p, "error", err)
			}
			continue
		}
		layers = append(layers, ef)
	}

	// Layer 3: 服务 env（services/<svc>/env.yaml）
	if serviceName != "" {
		svcEnvPath := filepath.Join(baseDir, "services", serviceName, "env.yaml")
		ef, err := config.LoadEnv(svcEnvPath)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				slog.Warn("load service env file failed",
					"path", svcEnvPath, "service", serviceName, "error", err)
			}
		} else {
			layers = append(layers, ef)
		}
	}

	// 无 env.yaml 层时直接返回 os.Environ()（保持原顺序，避免无谓的拷贝）
	if len(layers) == 0 {
		return env
	}

	// 合并 env.yaml 层，注入 enabled 变量
	merged := config.MergeEnv(layers...)
	injected := config.ToInjectEnv(merged)

	if len(injected) == 0 {
		return env
	}

	// 索引 os.Environ() 中已存在的 key（用于就地覆盖，保留原顺序）
	existingIdx := make(map[string]int, len(env))
	for i, kv := range env {
		if k, _, ok := strings.Cut(kv, "="); ok {
			existingIdx[k] = i
		}
	}

	// env.yaml 注入的变量覆盖同名既有变量；新变量追加到末尾
	for k, v := range injected {
		kv := k + "=" + v
		if i, ok := existingIdx[k]; ok {
			env[i] = kv
		} else {
			env = append(env, kv)
		}
	}

	return env
}
