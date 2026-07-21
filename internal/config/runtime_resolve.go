package config

import (
	"fmt"
	"log/slog"
	"sort"

	"github.com/supdorg/supd/internal/errors"
)

// RegisterBuiltin 注册4个内置默认运行时。
// REQ-F-028: 内置默认 — bash=/bin/bash, sh=/bin/sh, python3=PATH查找, node=PATH查找
// 优先级最低，被 config 和 scan 覆盖
func RegisterBuiltin(registry *RuntimeRegistry) {
	builtins := map[string]string{
		"bash":    "/bin/bash",
		"sh":      "/bin/sh",
		"python3": "python3", // PATH 查找
		"node":    "node",    // PATH 查找
	}
	for alias, path := range builtins {
		// 内置优先级最低，如果已存在更高优先级的条目则跳过
		if _, exists := registry.entries[alias]; exists {
			continue
		}
		registry.entries[alias] = &RuntimeEntry{
			Alias:  alias,
			Path:   path,
			Source: RuntimeSourceBuiltin,
		}
	}
}

// RegisterFromConfig 从 config.yaml 的 runtimes 配置注册运行时。
// REQ-F-028: config 来源优先级最高，覆盖同名内置和 scan 条目
func RegisterFromConfig(registry *RuntimeRegistry, runtimes map[string]string) {
	for alias, path := range runtimes {
		registry.entries[alias] = &RuntimeEntry{
			Alias:  alias,
			Path:   path,
			Source: RuntimeSourceConfig,
		}
	}
}

// RegisterFromScan 从 runtimes/ 目录扫描结果注册运行时。
// REQ-F-028: scan 来源优先级2，覆盖内置但不覆盖 config
func RegisterFromScan(registry *RuntimeRegistry, runtimes map[string]string) {
	for alias, path := range runtimes {
		// scan 不覆盖 config 来源的条目
		if existing, ok := registry.entries[alias]; ok && existing.Source == RuntimeSourceConfig {
			continue
		}
		registry.entries[alias] = &RuntimeEntry{
			Alias:  alias,
			Path:   path,
			Source: RuntimeSourceScan,
		}
	}
}

// Resolve 解析运行时别名，返回对应的运行时条目。
// REQ-F-028: 查找 entries[alias]
// REQ-F-029: 不可用的运行时返回 RUNTIME_NOT_FOUND 错误
//
// K-03-001 说明：本函数不递归解析 Path 字段。
// 即使用户配置 runtimes: { node: "python", python: "node" }（互相引用），
// Resolve("node") 也只会返回 Path="python" 的条目，不会递归调用 Resolve("python")，
// 因此不会发生栈溢出。扩展执行时 entry.Path 作为命令名直接传给 exec.Command，
// 由 exec 在 PATH 中查找可执行文件；若未找到则启动失败，不会循环。
//
// 循环引用检测在 ValidateAll 中执行（detectAliasCycles），
// 配置加载阶段就会输出警告，便于用户修正错误配置。
func Resolve(registry *RuntimeRegistry, alias string) (*RuntimeEntry, error) {
	entry, ok := registry.entries[alias]
	if !ok {
		// K-03-001: 记录警告日志，说明 runtime 不可用将降级为直接执行
		slog.Warn("runtime not found in registry, falling back to direct execution",
			"alias", alias,
		)
		// REQ-F-029: 别名不存在 → RUNTIME_NOT_FOUND
		return nil, errors.NewServiceError(
			errors.ErrRuntimeNotFound,
			fmt.Sprintf("runtime %q not found in registry", alias),
		).WithDetails(map[string]any{
			"alias": alias,
		})
	}
	if !entry.Available {
		// K-03-001: 记录警告日志，说明 runtime 不可用将降级为直接执行
		slog.Warn("runtime is not available, falling back to direct execution",
			"alias", alias,
			"path", entry.Path,
			"source", string(entry.Source),
		)
		// REQ-F-029: 运行时不可用 → RUNTIME_NOT_FOUND
		return nil, errors.NewServiceError(
			errors.ErrRuntimeNotFound,
			fmt.Sprintf("runtime %q is not available (path: %s)", alias, entry.Path),
		).WithDetails(map[string]any{
			"alias":  alias,
			"path":   entry.Path,
			"source": string(entry.Source),
		})
	}
	return entry, nil
}

// List 列出所有已注册运行时，按别名排序。
// REQ-F-028: 前端下拉选项展示所有已注册运行时
func List(registry *RuntimeRegistry) []*RuntimeEntry {
	result := make([]*RuntimeEntry, 0, len(registry.entries))
	for _, entry := range registry.entries {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Alias < result[j].Alias
	})
	return result
}

// BuildRegistry 便捷函数：按三层优先级构建完整的运行时注册表。
// REQ-F-028: 优先级 config > scan > builtin
// REQ-F-029: 构建完成后校验所有运行时可用性
func BuildRegistry(configRuntimes map[string]string, scanRuntimes map[string]string) *RuntimeRegistry {
	registry := NewRuntimeRegistry()

	// 第1层：注册内置默认（最低优先级）
	RegisterBuiltin(registry)

	// 第2层：注册 scan 来源（覆盖内置，不覆盖 config）
	RegisterFromScan(registry, scanRuntimes)

	// 第3层：注册 config 来源（最高优先级，覆盖一切）
	RegisterFromConfig(registry, configRuntimes)

	// 校验所有运行时可用性
	ValidateAll(registry)

	return registry
}
