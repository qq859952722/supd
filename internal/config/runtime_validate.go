package config

import (
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

// ValidateRuntime 校验单个运行时的可用性。
// REQ-F-029: 绝对路径检查文件存在 + 可执行；PATH 查找用 exec.LookPath
// REQ-F-030: 运行时路径不注入 PATH，仅用于构造命令
func ValidateRuntime(entry *RuntimeEntry) {
	if strings.HasPrefix(entry.Path, "/") {
		// 绝对路径：检查文件存在 + 是普通文件 + 可执行位
		validateAbsolutePath(entry)
	} else {
		// 非绝对路径：PATH 查找
		validatePathLookup(entry)
	}
}

// validateAbsolutePath 校验绝对路径运行时的可用性。
// REQ-F-029: 绝对路径的运行时：检查文件存在 + 可执行
func validateAbsolutePath(entry *RuntimeEntry) {
	info, err := os.Stat(entry.Path)
	if err != nil {
		entry.Available = false
		entry.AbsPath = entry.Path
		return
	}

	// 必须是普通文件
	if !info.Mode().IsRegular() {
		entry.Available = false
		entry.AbsPath = entry.Path
		return
	}

	// 检查可执行位
	if info.Mode().Perm()&0111 == 0 {
		entry.Available = false
		entry.AbsPath = entry.Path
		return
	}

	entry.Available = true
	entry.AbsPath = entry.Path
}

// validatePathLookup 通过 PATH 查找运行时。
// REQ-F-029: PATH 查找的运行时（如 python3）：exec.LookPath 查找
func validatePathLookup(entry *RuntimeEntry) {
	absPath, err := exec.LookPath(entry.Path)
	if err != nil {
		entry.Available = false
		entry.AbsPath = entry.Path
		return
	}
	entry.Available = true
	entry.AbsPath = absPath
}

// ValidateAll 校验注册表中所有运行时的可用性。
// REQ-F-029: 不可用的运行时标记 Available: false
// K-03-001: 不可用的运行时在配置加载阶段输出明确警告；
// 同时检测运行时别名循环引用（如 node→python→node）并输出警告。
// 注意：当前 Resolve 函数不递归解析 Path，循环引用不会导致栈溢出，
// 但循环引用属于配置错误，应在加载阶段告警以便用户修正。
func ValidateAll(registry *RuntimeRegistry) {
	for _, entry := range registry.entries {
		ValidateRuntime(entry)
		if !entry.Available {
			slog.Warn("runtime is not available, services using this runtime will fall back to direct command execution",
				"alias", entry.Alias,
				"path", entry.Path,
				"source", string(entry.Source),
			)
		}
	}
	detectAliasCycles(registry)
}

// detectAliasCycles 检测运行时别名循环引用。
// 若 entry.Path 恰好等于另一个别名（或自身），则视为别名引用链，
// 沿链追踪；若形成环则输出警告。该方法仅用于诊断，不修改 Available 状态。
// 示例：runtimes: { node: "python", python: "node" } 会触发 node→python→node 循环警告。
// O-05-001 修复：跳过 builtin 来源的 entry，避免对内置 python3/node 等产生误报
// （builtin entry 的 Path 是绝对路径如 /usr/bin/python3，不应被识别为别名引用）
func detectAliasCycles(registry *RuntimeRegistry) {
	for alias, entry := range registry.entries {
		// 跳过 builtin 来源（路径是绝对路径，不会是别名引用）
		if entry.Source == RuntimeSourceBuiltin {
			continue
		}
		visited := map[string]bool{alias: true}
		cur := entry
		for {
			next, ok := registry.entries[cur.Path]
			if !ok {
				break // Path 不指向任何已知别名，非循环
			}
			// 跳过 builtin 作为下一跳（避免 builtin 路径被误识别为别名）
			if next.Source == RuntimeSourceBuiltin {
				break
			}
			if visited[cur.Path] {
				// 检测到循环：构造链路信息便于用户定位
				slog.Warn("runtime alias cycle detected, please review runtime configuration",
					"alias", alias,
					"path", entry.Path,
					"cycle_node", cur.Path,
				)
				break
			}
			visited[cur.Path] = true
			cur = next
		}
	}
}
