package logging

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LogWriter 日志文件写入器
// 自动创建目录、文件写互斥
// REQ-C-003: 日志文件写互斥为唯一允许的 mutex
// G-01-001 修复：添加 bufio 缓冲，减少 write syscall，定期 Flush
type LogWriter struct {
	mu        sync.Mutex
	file      *os.File
	buf       *bufio.Writer
	path      string
	stopCh    chan struct{}
	flushDone chan struct{}
}

// NewLogWriter 创建日志写入器，自动创建目录
func NewLogWriter(path string) (*LogWriter, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}

	w := &LogWriter{
		file:      f,
		buf:       bufio.NewWriterSize(f, 64*1024), // G-01-001: 64KB 缓冲区
		path:      path,
		stopCh:    make(chan struct{}),
		flushDone: make(chan struct{}),
	}
	// G-01-001: 启动定期 Flush goroutine（每 5 秒）
	go w.flushLoop()
	return w, nil
}

// flushLoop 定期刷新缓冲区到文件
// G-01-001 修复：减少 write syscall，定期 Flush 确保日志不丢失
// C-01-002 修复：Flush 失败时写入 stderr（最后手段，不通过 log 包避免递归）
func (w *LogWriter) flushLoop() {
	defer close(w.flushDone)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			w.mu.Lock()
			if err := w.buf.Flush(); err != nil {
				// 不通过 log/slog 避免递归；写 stderr 是最后手段
				fmt.Fprintf(os.Stderr, "supd: log flush failed for %s: %v\n", w.path, err)
			}
			w.mu.Unlock()
		case <-w.stopCh:
			return
		}
	}
}

// Write 写入一行日志（互斥保护）
// REQ-C-003: 日志文件写互斥
// G-01-001: 写入 bufio 缓冲区，由定期 Flush 或缓冲区满时自动刷入文件
func (w *LogWriter) Write(line []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(line)
}

// Close 关闭日志文件
// G-01-001: 停止定期 Flush goroutine，Flush 剩余缓冲数据后关闭文件
// C-01-002 修复：检查 Flush 错误，避免丢失最后一批日志行（最多 64KB）
func (w *LogWriter) Close() error {
	// 先停止定期 Flush goroutine（不持有 mutex，避免死锁）
	close(w.stopCh)
	<-w.flushDone

	w.mu.Lock()
	defer w.mu.Unlock()
	// Flush 剩余缓冲数据，失败时优先返回 Flush 错误（文件仍尝试关闭）
	if err := w.buf.Flush(); err != nil {
		// C-01-001 修复：文件 Close 失败记录到 stderr fallback
		if cerr := w.file.Close(); cerr != nil {
			stderrWarn("log writer close after flush failed failed", cerr)
		}
		return fmt.Errorf("flush log buffer: %w", err)
	}
	return w.file.Close()
}

// Sync 刷盘
// G-01-001: 先 Flush bufio 缓冲区，再 Sync 文件
func (w *LogWriter) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.buf.Flush(); err != nil {
		return err
	}
	return w.file.Sync()
}

// Path 返回日志文件路径
func (w *LogWriter) Path() string {
	return w.path
}

// stderrWarn 是 logging 包内部的错误记录函数。
// C-01-001 修复：logging 包内部不应调用 slog（slog.Default handler 可能写入 supdLogger，
// 导致 Write → slog.Warn → Write 递归）。直接写 stderr 是最后手段。
// 调用方应在错误不影响主流程时使用（仅用于诊断），不改变原有控制流。
func stderrWarn(msg string, err error) {
	fmt.Fprintf(os.Stderr, "supd: logging internal: %s: %v\n", msg, err)
}
