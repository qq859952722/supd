package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CatchAllLogger 同时写文件和 stderr 的日志器
// REQ-F-010: supd 自身日志写入 /var/log/supd/supd.log，同时写 stderr（systemd journal 可见）
// 未配置独立 logger 的服务输出走 catch-all
// REQ-C-003: 文件写互斥为唯一允许的 mutex
type CatchAllLogger struct {
	mu     sync.Mutex
	writer *LogWriter
	stderr io.Writer
}

// NewCatchAllLogger 创建 CatchAllLogger，写到 path 和 stderr
// REQ-F-010: supd 自身日志写入文件同时写 stderr
func NewCatchAllLogger(path string) (*CatchAllLogger, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	writer, err := NewLogWriter(path)
	if err != nil {
		return nil, err
	}

	return &CatchAllLogger{
		writer: writer,
		stderr: os.Stderr,
	}, nil
}

// Write 同时写文件和 stderr
// REQ-F-010: supd 自身日志写入文件同时写 stderr（systemd journal 可见）
// REQ-C-003: 文件写互斥
func (c *CatchAllLogger) Write(line []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	n, err := c.writer.Write(line)
	if err != nil {
		return n, err
	}

	// 写 stderr 忽略错误（stderr 写入失败不应影响主流程）
	// C-01-001 修复：显式接收错误并记录到 stderr fallback（避免静默丢失诊断信息）
	if _, err := c.stderr.Write(line); err != nil {
		stderrWarn("stderr write failed (ignored)", err)
	}

	return n, nil
}

// Close 关闭日志文件
func (c *CatchAllLogger) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.writer.Close()
}

// Path 返回日志文件路径
func (c *CatchAllLogger) Path() string {
	return c.writer.Path()
}

// supdLogger 全局 supd 自身日志器
// REQ-F-010: supd 自身日志写入 /var/log/supd/supd.log
var supdLogger *CatchAllLogger

// InitSupdLogger 初始化 supd 自身日志
// REQ-F-010: supd 自身日志写入 logDir/supd.log，同时写 stderr
// P-03-1/O-03-1/O-03-2 修复：同时配置 slog 默认 handler，使所有 slog 调用都写入 supd.log
// logLevel: debug/info/warn/error，空字符串默认 info
func InitSupdLogger(logDir string) error {
	path := filepath.Join(logDir, "supd.log")
	logger, err := NewCatchAllLogger(path)
	if err != nil {
		return err
	}
	supdLogger = logger

	// P-03-1/O-03-1 修复：配置 slog 默认 handler，使所有 slog.Info/Warn/Error 调用
	// 都同时写入 supd.log 文件和 stderr，而非仅写 stderr
	handler := slog.NewTextHandler(logger, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	slog.SetDefault(slog.New(handler))

	return nil
}

// InitSupdLoggerWithLevel 初始化 supd 自身日志并应用 log_level 配置
// O-03-2 修复：应用 config.Settings.LogLevel 到 slog，使 debug 日志可按需开启
func InitSupdLoggerWithLevel(logDir, logLevel string) error {
	if err := InitSupdLogger(logDir); err != nil {
		return err
	}

	// O-03-2: 应用 log_level 配置
	if supdLogger != nil {
		var level slog.Level
		switch logLevel {
		case "debug":
			level = slog.LevelDebug
		case "info", "":
			level = slog.LevelInfo
		case "warn", "warning":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		default:
			level = slog.LevelInfo
		}
		handler := slog.NewTextHandler(supdLogger, &slog.HandlerOptions{
			Level: level,
		})
		slog.SetDefault(slog.New(handler))
	}

	return nil
}

// SupdLog 写入 supd 自身日志
// REQ-F-010: supd 自身日志写入 supd.log，格式 [ISO8601 时间戳] [级别] 消息
func SupdLog(msg string) {
	if supdLogger == nil {
		return
	}

	timestamp := time.Now().Format(time.RFC3339Nano)
	line := fmt.Sprintf("[%s] [info] %s\n", timestamp, msg)
	// C-01-001 修复：显式接收错误并记录到 stderr fallback
	if _, err := supdLogger.Write([]byte(line)); err != nil {
		stderrWarn("supd log write failed", err)
	}
}
