package logging

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ServiceLogger 每服务一个logger goroutine
// REQ-F-010: per-service logger goroutine
// N-G-01 修复：接入 RotatingLogWriter，使 logging.max_size_mb / max_files 配置生效
type ServiceLogger struct {
	name    string
	baseDir string // e.g. /var/log/supd/services/<svc>/
	writer  *RotatingLogWriter // 接入轮转，替代裸 LogWriter
	done    chan struct{}
	wg      sync.WaitGroup
}

// NewServiceLogger 创建服务日志器
// baseDir 为日志根目录（如 /var/log/supd/services/），name 为服务名
// 日志文件路径为 baseDir/name/current
// maxSizeMB/maxFiles 来自 service.yaml 的 logging 配置，0 时使用默认值 10/5
func NewServiceLogger(name string, baseDir string, maxSizeMB, maxFiles int) (*ServiceLogger, error) {
	dir := filepath.Join(baseDir, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	currentPath := filepath.Join(dir, "current")
	// N-G-01 修复：使用 RotatingLogWriter 替代裸 LogWriter，使日志轮转生效
	rotator, err := NewRotatingLogWriter(currentPath, maxSizeMB, maxFiles)
	if err != nil {
		return nil, err
	}

	return &ServiceLogger{
		name:    name,
		baseDir: dir,
		writer:  rotator,
		done:    make(chan struct{}),
	}, nil
}

// Start 启动logger goroutine，从pipe读取日志行写入文件
// stdout, stderr: 子进程的输出pipe
// REQ-F-010: logger goroutine 从 pipe 读端读取行，写入 current 文件
func (l *ServiceLogger) Start(stdout, stderr io.Reader) {
	if stdout != nil {
		l.wg.Add(1)
		go l.readPipe(stdout)
	}
	if stderr != nil {
		l.wg.Add(1)
		go l.readPipe(stderr)
	}

	// 当所有 pipe 读取 goroutine 退出后，关闭 done channel
	go func() {
		l.wg.Wait()
		close(l.done)
	}()
}

// CloseWriteEnd 通知logger写端已关闭
// 子进程退出后调用，logger收到EOF后退出
// REQ-F-010: 子进程退出后，父进程关闭自身持有的 pipe 写端，logger goroutine 收到 EOF 后退出
// Note: 对于 exec.Cmd 的 StdoutPipe/StderrPipe，子进程退出后 pipe 写端由 OS 自动关闭，
// logger goroutine 自然收到 EOF。此方法用于手动创建 pipe 的场景。
func (l *ServiceLogger) CloseWriteEnd() {
	// 对于 exec.Cmd 的 pipe，子进程退出后 OS 自动关闭写端，无需额外操作。
	// 对于手动创建的 os.Pipe()，调用方应在调用 Wait() 前关闭写端。
}

// Wait 等待logger goroutine退出
func (l *ServiceLogger) Wait() {
	<-l.done
}

// Close 关闭logger
func (l *ServiceLogger) Close() error {
	return l.writer.Close()
}

// WriteLine 向服务日志写入自定义行（如启动失败原因）
// 内容会自动添加时间戳和级别前缀
func (l *ServiceLogger) WriteLine(level, message string) {
	if l.writer == nil {
		return
	}
	timestamp := time.Now().Format(time.RFC3339Nano)
	line := fmt.Sprintf("[%s] [%s] %s\n", timestamp, level, message)
	// C-01-001 修复：WriteLine 是辅助函数，写入失败仅记录到 stderr fallback
	if _, err := l.writer.Write([]byte(line)); err != nil {
		stderrWarn("service log WriteLine failed (ignored)", err)
	}
}

// LogPath 返回日志文件路径
func (l *ServiceLogger) LogPath() string {
	return l.writer.Path()
}

// readPipe 从pipe读取日志行，格式化后写入文件
// REQ-F-010: logger goroutine 从 pipe 读端读取行，写入 current 文件
func (l *ServiceLogger) readPipe(r io.Reader) {
	defer l.wg.Done()

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		formatted := formatLine(line)
		// C-01-001 修复：写入失败仅记录到 stderr fallback，不中断读取循环
		// （RotatingLogWriter 内部已有 DiskFullBuffer 降级处理，但此处直接调用的是 RotatingLogWriter.Write）
		if _, err := l.writer.Write([]byte(formatted + "\n")); err != nil {
			stderrWarn("service log readPipe write failed (ignored)", err)
		}
	}
	// scanner 在 EOF 时正常退出，无需处理 scanner.Err()
}

// formatLine 格式化日志行
// REQ-F-010: 日志行格式：[ISO8601 时间戳] [级别] 原始内容
// 级别由启发式判定：含 ERROR/error → error，WARN/warn → warn，其余 info
func formatLine(line string) string {
	level := detectLevel(line)
	timestamp := time.Now().Format(time.RFC3339Nano)
	return fmt.Sprintf("[%s] [%s] %s", timestamp, level, line)
}

// detectLevel 启发式判定日志级别
// REQ-F-010: 含 ERROR/error → error，WARN/warn → warn，其余 info
func detectLevel(line string) string {
	lower := strings.ToLower(line)
	if strings.Contains(lower, "error") {
		return "error"
	}
	if strings.Contains(lower, "warn") {
		return "warn"
	}
	return "info"
}
