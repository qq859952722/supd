package core

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Process 代表一个被监督的子进程
// REQ-F-006: fork+exec+进程组
// REQ-F-011: 进程管理核心机制
type Process struct {
	name      string
	cmd       *exec.Cmd
	pid       int
	pgid      int
	startTime time.Time    // 进程启动时间
	done      chan struct{} // 进程退出信号
	stdout    io.ReadCloser
	stderr    io.ReadCloser
	waitOnce  sync.Once     // 确保 Wait 只执行一次
	exitCode  int           // 缓存退出码，供多次 Wait 调用使用
	signaled  bool          // 缓存信号标记
	sig       syscall.Signal // 缓存信号
}

// StartProcess 启动子进程
// REQ-F-006: fork+exec，设置 Setpgid=true 创建独立进程组
// REQ-F-011: Stdin=nil 关闭标准输入，禁止服务进程后台化
// REQ-F-023: credential 非 nil 时设置执行身份（含补充组），实现 run_as / user 字段切换
// A-03-002 修复：extraFiles 用于 fd_notify readiness 传递管道写端给子进程（fd=3）
func StartProcess(name string, command []string, env []string, dir string, credential *syscall.Credential, extraFiles ...*os.File) (*Process, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("process %s: command is empty", name)
	}

	cmd := exec.Command(command[0], command[1:]...)
	cmd.Env = env
	cmd.Dir = dir

	// REQ-F-011: 服务 stdin 必须关闭
	cmd.Stdin = nil

	// A-03-002 修复：设置 ExtraFiles，子进程通过 fd 3+ 访问
	// 主要用于 fd_notify readiness：管道写端传递给子进程
	if len(extraFiles) > 0 {
		cmd.ExtraFiles = extraFiles
	}

	// REQ-F-006: Setpgid=true，每个子进程独立进程组
	// REQ-F-023: credential 非 nil 时切换执行身份
	sysProcAttr := &syscall.SysProcAttr{
		Setpgid: true,
	}
	if credential != nil {
		sysProcAttr.Credential = credential
	}
	cmd.SysProcAttr = sysProcAttr

	// 在 Start 之前创建 stdout/stderr pipe
	// 子进程退出后，pipe 写端关闭，logger goroutine 收 EOF 退出
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("process %s: stdout pipe: %w", name, err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("process %s: stderr pipe: %w", name, err)
	}

	// 启动进程
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("process %s: start failed: %w", name, err)
	}

	p := &Process{
		name:   name,
		cmd:    cmd,
		pid:       cmd.Process.Pid,
		pgid:      cmd.Process.Pid, // Setpgid=true 时 PGID == PID
		startTime: time.Now(),
		done:      make(chan struct{}),
		stdout:    stdoutPipe,
		stderr:    stderrPipe,
	}

	return p, nil
}

// Wait 等待进程退出，返回退出状态
// REQ-F-006: 每个服务一个专属 Wait goroutine（避免僵尸进程）
// 安全：多次调用不会 panic，后续调用返回首次结果
func (p *Process) Wait() (exitCode int, signaled bool, sig syscall.Signal) {
	p.waitOnce.Do(func() {
		err := p.cmd.Wait()
		close(p.done)

		if err == nil {
			// 进程正常退出，exit code = 0
			p.exitCode = 0
			return
		}

		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			// 非 ExitError，可能是 I/O 错误等
			p.exitCode = -1
			return
		}

		status, ok := exitErr.Sys().(syscall.WaitStatus)
		if !ok {
			p.exitCode = exitErr.ExitCode()
			return
		}

		if status.Signaled() {
			p.exitCode = -1
			p.signaled = true
			p.sig = status.Signal()
			return
		}

		p.exitCode = status.ExitStatus()
	})
	<-p.done // 确保调用者等待进程实际退出
	return p.exitCode, p.signaled, p.sig
}

// SendSignal 向进程组发送信号
// REQ-F-006: 使用 syscall.Kill(-pgid, sig) 发到整个进程组
func (p *Process) SendSignal(sig syscall.Signal) error {
	// -pgid 表示向整个进程组发送信号
	if err := syscall.Kill(-p.pgid, sig); err != nil {
		return fmt.Errorf("process %s: send signal %d to pgid %d: %w", p.name, sig, p.pgid, err)
	}
	return nil
}

// KillProcessGroup 强制杀死进程组（SIGKILL）
// REQ-F-006: 使用 syscall.Kill(-pgid, SIGKILL) 强制杀死整个进程组
func (p *Process) KillProcessGroup() error {
	if err := syscall.Kill(-p.pgid, syscall.SIGKILL); err != nil {
		return fmt.Errorf("process %s: kill pgid %d: %w", p.name, p.pgid, err)
	}
	return nil
}

// PID 返回进程ID
func (p *Process) PID() int {
	return p.pid
}

// StartTime 返回进程启动时间
func (p *Process) StartTime() time.Time {
	return p.startTime
}

// PGID 返回进程组ID
func (p *Process) PGID() int {
	return p.pgid
}

// Command 返回启动命令行（用于 PID 文件记录和孤儿进程验证）
func (p *Process) Command() []string {
	return p.cmd.Args
}

// Done 返回进程退出channel
func (p *Process) Done() <-chan struct{} {
	return p.done
}

// StdoutPipe 返回stdout pipe
// 子进程退出后，pipe 写端关闭，logger goroutine 收 EOF 退出
func (p *Process) StdoutPipe() io.ReadCloser {
	return p.stdout
}

// StderrPipe 返回stderr pipe
// 子进程退出后，pipe 写端关闭，logger goroutine 收 EOF 退出
func (p *Process) StderrPipe() io.ReadCloser {
	return p.stderr
}
