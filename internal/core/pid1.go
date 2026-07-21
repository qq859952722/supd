package core

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// IsPID1 检查当前进程是否为 PID 1
// REQ-F-012: 当 supd 作为 PID 1 运行时（如在 Docker 容器中），
// 必须设置 PR_SET_CHILD_SUBREAPER 以回收孤儿进程
func IsPID1() bool {
	return os.Getpid() == 1
}

// SetupPID1Mode 设置 PID1 子进程回收模式
// REQ-F-012: 调用 PR_SET_CHILD_SUBREAPER，使 supd 可以回收孤儿进程
// 当 supd 作为 PID 1 运行时（Docker 容器场景），子进程的子进程
// 在子进程退出后会成为孤儿进程，设置 CHILD_SUBREAPER 后
// 内核会将这些孤儿进程重新挂载到 supd 进程下，确保可以被 wait 回收
func SetupPID1Mode() error {
	// PR_SET_CHILD_SUBREAPER = 36 (Linux)
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("PR_SET_CHILD_SUBREAPER: %w", err)
	}
	return nil
}

// SetupPID1IfNeeded 检查是否为 PID1，如果是则设置 subreaper
// noPID1 参数对应 --no-pid1 命令行参数，为 true 时跳过设置
// REQ-F-012: 提供 --no-pid1 参数禁用此行为
func SetupPID1IfNeeded(noPID1 bool) error {
	if noPID1 {
		return nil
	}
	if !IsPID1() {
		return nil
	}
	return SetupPID1Mode()
}
