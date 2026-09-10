package logging

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/supdorg/supd/internal/notification"
	"github.com/supdorg/supd/internal/stream"
)

// ServiceLogger 每服务一个logger goroutine
// REQ-F-010: per-service logger goroutine
// N-G-01 修复：接入 RotatingLogWriter，使 logging.max_size_mb / max_files 配置生效
type ServiceLogger struct {
	name    string
	baseDir string             // e.g. /var/log/supd/services/<svc>/
	writer  *RotatingLogWriter // 接入轮转，替代裸 LogWriter
	done    chan struct{}
	wg      sync.WaitGroup

	// notifySink 通知接收点（预留接线点，由节点 08 替换为真实路由；本节点为测试 Sink）。
	notifySink notification.NotifySink
}

// notifyLineLimit 服务 stdout/stderr 单行读取字节上限（协议窗口 8KB）。
const notifyLineLimit = 8192

// SetNotifySink 注入通知接收点。服务 stdout 中合法的 ::notify:: 行将会
// 写入日志并尝试非阻塞入队；nil 或队列满时被忽略，不影响日志与服务状态。
func (l *ServiceLogger) SetNotifySink(sink notification.NotifySink) {
	l.notifySink = sink
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
// 设计稿 §六.4：stdout/stderr 分流，仅 stdout 解析 ::notify:: 协议。
func (l *ServiceLogger) Start(stdout, stderr io.Reader) {
	if stdout != nil {
		l.wg.Add(1)
		go l.readStdout(stdout)
	}
	if stderr != nil {
		l.wg.Add(1)
		go l.readStderr(stderr)
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

// readStdout 从 stdout pipe 读取日志行写入文件，并解析 ::notify:: 协议。
// 设计稿 §六.4：stdout 唯一 reader —— 写日志 + 解析 notify。
// 永久排水：超长行按截断普通日志处理，不中断读取；读取错误显式记录，不改变服务状态。
func (l *ServiceLogger) readStdout(r io.Reader) {
	defer l.wg.Done()

	err := stream.ReadLines(r, notifyLineLimit, func(line string) {
		l.writeLogLine(line)

		parsed, isProto := notification.ParseNotifyLine(line)
		if !isProto {
			return // 非协议行：仅日志
		}
		if parsed == nil {
			// 协议行但格式/等级非法：仅日志 + 追加 warning
			l.WriteLine("warn", "invalid ::notify:: line, treated as log: "+line)
			return
		}
		l.enqueueNotify(parsed)
	})
	if err != nil && !isBenignPipeErr(err) {
		stderrWarn(fmt.Sprintf("service %s stdout read error", l.name), err)
	}
}

// readStderr 从 stderr pipe 读取日志行写入文件。只写日志，不解析协议。
// 同样使用排水安全读取：超长行不中断；读取错误显式记录。
func (l *ServiceLogger) readStderr(r io.Reader) {
	defer l.wg.Done()

	err := stream.ReadLines(r, notifyLineLimit, func(line string) {
		l.writeLogLine(line)
	})
	if err != nil && !isBenignPipeErr(err) {
		stderrWarn(fmt.Sprintf("service %s stderr read error", l.name), err)
	}
}

// isBenignPipeErr 判断管道读取错误是否为进程正常退出时的良性终止
// （exec.Cmd 在 Wait/进程退出时会关闭 StdoutPipe/StderrPipe 读端，正在排水的
// Reader 可能读到 os.ErrClosed/io.ErrClosedPipe）。此类错误按正常 EOF 处理，不告警。
func isBenignPipeErr(err error) bool {
	return errors.Is(err, os.ErrClosed) || errors.Is(err, os.ErrInvalid)
}

// writeLogLine 格式化并写入一条日志行（写入失败仅记录到 stderr fallback，不中断读取）。
func (l *ServiceLogger) writeLogLine(line string) {
	formatted := formatLine(line)
	// C-01-001 修复：写入失败仅记录到 stderr fallback，不中断读取循环
	if _, err := l.writer.Write([]byte(formatted + "\n")); err != nil {
		stderrWarn("service log readPipe write failed (ignored)", err)
	}
}

// enqueueNotify 非阻塞提交通知。来源上下文（此处为服务名）由 supd 填充，脚本不可覆盖。
// Sink 为 nil 或队列满时忽略，不影响日志与服务状态。
func (l *ServiceLogger) enqueueNotify(parsed *notification.ParsedNotify) {
	if l.notifySink == nil {
		return
	}
	l.notifySink.TryEnqueue(notification.PendingNotification{
		Level:       parsed.Level,
		Content:     parsed.Content,
		ServiceName: l.name,
	})
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
