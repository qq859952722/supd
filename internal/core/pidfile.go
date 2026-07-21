package core

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// PID 文件机制：记录 supd 管理的子进程，下次启动时识别并清理孤儿进程。
//
// 使用场景：supd 被 SIGKILL 强杀时，graceful shutdown 无机会执行，
// 子进程（在独立进程组中）成为孤儿继续运行。下次 supd 启动时
// 通过扫描 PID 文件识别这些孤儿并 SIGKILL，避免端口/锁文件冲突。
//
// PID 文件位置：<baseDir>/.supd/pids/<service_name>.pid
// 文件格式：JSON，含 PID/PGID/Command/StartTime

// pidFileRecord PID 文件记录
type pidFileRecord struct {
	Name      string   `json:"name"`
	PID       int      `json:"pid"`
	PGID      int      `json:"pgid"`
	Command   []string `json:"command"`
	StartTime int64    `json:"start_time"` // Unix 时间戳（秒）
}

// pidFilePath 返回服务的 PID 文件路径
func pidFilePath(baseDir, name string) string {
	return filepath.Join(baseDir, ".supd", "pids", name+".pid")
}

// writePIDFile 写入服务的 PID 文件（原子写入）
func writePIDFile(baseDir, name string, proc *Process) error {
	dir := filepath.Dir(pidFilePath(baseDir, name))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create pidfile dir: %w", err)
	}

	rec := pidFileRecord{
		Name:      name,
		PID:       proc.PID(),
		PGID:      proc.PGID(),
		Command:   proc.Command(),
		StartTime: proc.StartTime().Unix(),
	}

	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal pidfile: %w", err)
	}

	path := pidFilePath(baseDir, name)
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write pidfile: %w", err)
	}
	return os.Rename(tmpPath, path)
}

// removePIDFile 删除服务的 PID 文件
func removePIDFile(baseDir, name string) {
	_ = os.Remove(pidFilePath(baseDir, name))
}

// ReapOrphans 扫描 PID 文件目录，清理上次 supd 异常退出后遗留的孤儿进程。
// 对仍存活的孤儿进程发送 SIGKILL 到整个进程组，然后删除 PID 文件。
// 返回被清理的服务名列表。
func ReapOrphans(baseDir string) []string {
	dir := filepath.Join(baseDir, ".supd", "pids")
	entries, err := os.ReadDir(dir)
	if err != nil {
		// 目录不存在或无权限，无孤儿可清理
		return nil
	}

	var reaped []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".pid") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".pid")
		path := filepath.Join(dir, entry.Name())

		data, err := os.ReadFile(path)
		if err != nil {
			_ = os.Remove(path)
			continue
		}

		var rec pidFileRecord
		if err := json.Unmarshal(data, &rec); err != nil {
			_ = os.Remove(path)
			continue
		}

		// 检查进程是否存活
		if !processAlive(rec.PID) {
			// 进程已退出，清理过期 PID 文件
			_ = os.Remove(path)
			continue
		}

		// 验证命令行匹配（防止 PID 复用）
		if !commandMatches(rec.PID, rec.Command) {
			// PID 已被其他进程复用，不是我们的孤儿
			_ = os.Remove(path)
			continue
		}

		// 确认是孤儿进程，SIGKILL 整个进程组
		slog.Warn("检测到孤儿进程，正在清理",
			"service", name, "pid", rec.PID, "pgid", rec.PGID)
		if err := syscall.Kill(-rec.PGID, syscall.SIGKILL); err != nil {
			// 进程组可能已不存在，尝试 kill 单个 PID
			_ = syscall.Kill(rec.PID, syscall.SIGKILL)
		}
		reaped = append(reaped, name)
		_ = os.Remove(path)
	}

	return reaped
}

// processAlive 检查 PID 是否存活
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	// kill(pid, 0) 不实际发信号，只检查进程是否存在
	return syscall.Kill(pid, 0) == nil
}

// commandMatches 读取 /proc/<pid>/cmdline 验证命令行是否匹配
// 防止 PID 复用导致误杀无关进程
func commandMatches(pid int, expected []string) bool {
	if len(expected) == 0 {
		return false
	}
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return false
	}
	// /proc/<pid>/cmdline 用 \0 分隔参数
	actual := strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00")
	if len(actual) != len(expected) {
		return false
	}
	for i, arg := range expected {
		if actual[i] != arg {
			return false
		}
	}
	return true
}
