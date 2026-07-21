package system

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// SignalHandler 信号处理器
// REQ-F-012: PID1 模式下的信号注册
// REQ-F-013: SIGHUP → 热重载，SIGTERM/SIGINT → 优雅退出
type SignalHandler struct {
	shutdownCh chan os.Signal
	reloadCh   chan os.Signal
	stopCh     chan struct{}
}

// NewSignalHandler 创建信号处理器
func NewSignalHandler() *SignalHandler {
	return &SignalHandler{
		shutdownCh: make(chan os.Signal, 1),
		reloadCh:   make(chan os.Signal, 1),
		stopCh:     make(chan struct{}),
	}
}

// Start 开始监听信号
// REQ-F-013: 注册 SIGTERM/SIGINT → 优雅退出，SIGHUP → 热重载
func (h *SignalHandler) Start() {
	// SIGTERM/SIGINT → 优雅退出
	signal.Notify(h.shutdownCh, syscall.SIGTERM, syscall.SIGINT)
	// SIGHUP → 热重载
	signal.Notify(h.reloadCh, syscall.SIGHUP)
}

// WaitShutdown 等待关闭信号（SIGTERM 或 SIGINT）
func (h *SignalHandler) WaitShutdown() <-chan os.Signal {
	return h.shutdownCh
}

// WaitReload 等待热重载信号（SIGHUP）
// REQ-F-013: supd 收到 SIGHUP 信号时触发配置热重载
func (h *SignalHandler) WaitReload() <-chan os.Signal {
	return h.reloadCh
}

// Stop 停止信号处理，释放资源
func (h *SignalHandler) Stop() {
	signal.Stop(h.shutdownCh)
	signal.Stop(h.reloadCh)
	select {
	case <-h.stopCh:
		// 已关闭
	default:
		close(h.stopCh)
	}
}

// ZombieReaper 僵尸进程回收器
// REQ-F-012: PID1 模式下，监听 SIGCHLD 并回收孤儿进程
// 当 supd 作为 PID1 运行时，设置了 CHILD_SUBREAPER 后，
// 孤儿进程会被重新挂载到 supd 下，需要通过 wait 回收
//
// 设计要点（PID 1 加固）：
//   - 监听 SIGCHLD 信号触发回收（主路径）
//   - 周期性 ticker 兜底（10s），防止极端情况下 SIGCHLD 丢失导致僵尸积累
//   - reapZombies 用 Wait4(-1, WNOHANG) 循环批量回收所有死亡子进程，
//     与触发信号数量无关
type ZombieReaper struct {
	sigChld chan os.Signal
	stopCh  chan struct{}
	stopped chan struct{}
}

// NewZombieReaper 创建僵尸进程回收器
func NewZombieReaper() *ZombieReaper {
	return &ZombieReaper{
		sigChld: make(chan os.Signal, 1),
		stopCh:  make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

// Start 启动僵尸进程回收 goroutine
// REQ-F-012: PID1 模式下监听 SIGCHLD，并增加 10s 周期 poll 兜底
func (r *ZombieReaper) Start() {
	signal.Notify(r.sigChld, syscall.SIGCHLD)

	go func() {
		defer close(r.stopped)
		// P1-003 加固：10s ticker 作为 SIGCHLD 丢失的兜底
		// 极端情况下信号被其他库截获或 kernel 合并丢失时，仍能定期回收
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-r.sigChld:
				reapZombies()
			case <-ticker.C:
				reapZombies()
			case <-r.stopCh:
				signal.Stop(r.sigChld)
				return
			}
		}
	}()

	slog.Info("僵尸进程回收 goroutine 已启动", "fallback_poll_interval", "10s")
}

// Stop 停止僵尸进程回收 goroutine，释放信号注册
// 幂等：多次调用安全
func (r *ZombieReaper) Stop() {
	select {
	case <-r.stopCh:
		// 已停止
	default:
		close(r.stopCh)
	}
	<-r.stopped
}

// reapZombies 回收所有已结束的子进程（WNOHANG 非阻塞）
func reapZombies() {
	for {
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
		if err != nil || pid <= 0 {
			break
		}
		slog.Debug("回收僵尸进程", "pid", pid, "status", status)
	}
}
