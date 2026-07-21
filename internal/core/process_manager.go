package core

import (
	"fmt"
	"log/slog"
	"sync"
	"syscall"
)

// ProcessManager 管理所有服务进程
// REQ-F-006: 进程管理
// 使用 sync.RWMutex 保护进程 map 的并发访问（基础设施，非业务状态共享）
type ProcessManager struct {
	mu         sync.RWMutex
	procs      map[string]*Process // service name -> Process
	pidFileDir string              // PID 文件基础目录（baseDir），空则不写 PID 文件
}

// NewProcessManager 创建进程管理器
func NewProcessManager() *ProcessManager {
	return &ProcessManager{
		procs: make(map[string]*Process),
	}
}

// SetPIDFileDir 设置 PID 文件基础目录，启用 PID 文件记录。
// 启用后 Register 时写 PID 文件，Unregister 时删 PID 文件，
// 供下次 supd 启动时 ReapOrphans 识别并清理孤儿进程。
func (pm *ProcessManager) SetPIDFileDir(dir string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.pidFileDir = dir
}

// Register 注册进程
func (pm *ProcessManager) Register(name string, proc *Process) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.procs[name] = proc
	if pm.pidFileDir != "" {
		if err := writePIDFile(pm.pidFileDir, name, proc); err != nil {
			slog.Warn("写入 PID 文件失败", "service", name, "error", err)
		}
	}
}

// Unregister 注销进程
func (pm *ProcessManager) Unregister(name string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.procs, name)
	if pm.pidFileDir != "" {
		removePIDFile(pm.pidFileDir, name)
	}
}

// Get 获取进程
func (pm *ProcessManager) Get(name string) (*Process, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	p, ok := pm.procs[name]
	return p, ok
}

// SendSignal 向指定服务的进程组发送信号
// REQ-F-006: 使用 syscall.Kill(-pgid, sig) 发到整个进程组
func (pm *ProcessManager) SendSignal(name string, sig syscall.Signal) error {
	pm.mu.RLock()
	proc, ok := pm.procs[name]
	pm.mu.RUnlock()

	if !ok {
		return fmt.Errorf("process manager: service %s not found", name)
	}
	return proc.SendSignal(sig)
}

// KillProcessGroup 强制杀死指定服务的进程组
// REQ-F-006: 使用 syscall.Kill(-pgid, SIGKILL)
func (pm *ProcessManager) KillProcessGroup(name string) error {
	pm.mu.RLock()
	proc, ok := pm.procs[name]
	pm.mu.RUnlock()

	if !ok {
		return fmt.Errorf("process manager: service %s not found", name)
	}
	return proc.KillProcessGroup()
}

// List 返回所有已注册的服务名
func (pm *ProcessManager) List() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	names := make([]string, 0, len(pm.procs))
	for name := range pm.procs {
		names = append(names, name)
	}
	return names
}

// ListRunning 返回所有已注册且进程仍在运行的服务名
// REQ-F-032: 用于关机时确定需要停止哪些服务
// 在 ProcessManager 架构中，进程停止后会被 Unregister，
// 因此 List() 返回的即为仍在管理中的服务，等价于运行中的服务。
// 对于已完成 Wait() 的进程，通过 Done() channel 过滤掉已退出的。
func (pm *ProcessManager) ListRunning() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	var names []string
	for name, proc := range pm.procs {
		select {
		case <-proc.Done():
			// Wait() 已被调用且进程已退出，跳过
		default:
			names = append(names, name)
		}
	}
	return names
}
