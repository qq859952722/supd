package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ExtensionLogConfig 扩展日志配置
// REQ-F-010: 扩展运行日志区分服务级和全局级
type ExtensionLogConfig struct {
	// ExtName 扩展名称
	ExtName string
	// IsServiceLevel 是否为服务级扩展
	IsServiceLevel bool
	// ServiceName 服务名称（仅服务级扩展使用）
	ServiceName string
	// RunID 运行ID（仅全局扩展使用）
	RunID string
	// LogRootDir 日志根目录（如 /var/log/supd）
	LogRootDir string
}

// ExtensionLogger 扩展运行日志器
// REQ-F-010: 服务级扩展输出写入对应服务日志目录，前缀 [ext:<ext-name>]
// 全局扩展输出写入 /var/log/supd/extensions/<ext-name>/<run_id>.log
// 服务级扩展额外写入独立扩展日志文件（extensions/<ext>/<run_id>.log），用于 GetRunLogs 读取
// REQ-C-003: 文件写互斥为唯一允许的 mutex
// G-01 修复：主 writer 接入 RotatingLogWriter，使 §2.2.16 "扩展运行日志上限 10MB 硬编码" 生效
type ExtensionLogger struct {
	mu         sync.Mutex
	writer     *RotatingLogWriter // 主日志 writer（服务级=服务日志，全局级=扩展日志），带 10MB 轮转
	svcWriter  *LogWriter         // 服务级扩展的服务日志 writer（同时写入两个文件；服务日志由 ServiceLogger 负责轮转）
	extName    string
	isSvcLvl   bool
}

// NewExtensionLogger 创建扩展日志器
// REQ-F-010: 服务级扩展前缀 [ext:<ext-name>]，全局扩展写入 extensions/<ext-name>/<run_id>.log
// 服务级扩展同时写入：1) 独立扩展日志文件（用于 GetRunLogs 读取）2) 服务日志文件（带 [ext:xxx] 前缀）
func NewExtensionLogger(cfg ExtensionLogConfig) (*ExtensionLogger, error) {
	var logPath string

	if cfg.IsServiceLevel {
		// 服务级扩展：主 writer 指向独立扩展日志文件（与全局扩展格式一致，用于 GetRunLogs）
		logPath = filepath.Join(cfg.LogRootDir, "extensions", cfg.ExtName, cfg.RunID+".log")
	} else {
		// REQ-F-010: 全局扩展（on_schedule/on_demand 触发）的输出写入
		// /var/log/supd/extensions/<ext-name>/<run_id>.log
		logPath = filepath.Join(cfg.LogRootDir, "extensions", cfg.ExtName, cfg.RunID+".log")
	}

	dir := filepath.Dir(logPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	// G-01 修复：主 writer 接入 RotatingLogWriter，应用 §2.2.16 "扩展运行日志上限 10MB 硬编码"
	// maxSizeMB=10（10MB 硬上限），maxFiles=5（与 NewRotatingLogWriter 默认值一致，保留最近 5 个归档）
	// 保持 O_APPEND 模式（RotatingLogWriter 内部使用 LogWriter，以 O_APPEND 打开，跨 supd 重启保留历史）
	writer, err := NewRotatingLogWriter(logPath, 10, 5)
	if err != nil {
		return nil, err
	}

	logger := &ExtensionLogger{
		writer:   writer,
		extName:  cfg.ExtName,
		isSvcLvl: cfg.IsServiceLevel,
	}

	// 服务级扩展：额外创建服务日志 writer，同时写入服务日志（带 [ext:xxx] 前缀）
	if cfg.IsServiceLevel {
		svcLogPath := filepath.Join(cfg.LogRootDir, "services", cfg.ServiceName, "current")
		svcDir := filepath.Dir(svcLogPath)
		if err := os.MkdirAll(svcDir, 0755); err == nil {
			if svcWriter, err := NewLogWriter(svcLogPath); err == nil {
				logger.svcWriter = svcWriter
			}
		}
	}

	return logger, nil
}

// Write 写入扩展日志行
// REQ-F-010: 服务级扩展前缀 [ext:<ext-name>]
// 服务级扩展同时写入：1) 独立扩展日志（标准格式）2) 服务日志（带 [ext:xxx] 前缀）
// REQ-C-003: 文件写互斥
func (e *ExtensionLogger) Write(line []byte) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// 主 writer 写入独立扩展日志（标准格式，与全局扩展一致）
	formatted := formatLine(string(line)) + "\n"
	n, err := e.writer.Write([]byte(formatted))

	// 服务级扩展：额外写入服务日志（带 [ext:xxx] 前缀）
	if e.isSvcLvl && e.svcWriter != nil {
		timestamp := time.Now().Format(time.RFC3339Nano)
		level := detectLevel(string(line))
		svcFormatted := fmt.Sprintf("[%s] [%s] [ext:%s] %s\n", timestamp, level, e.extName, string(line))
		// C-01-001 修复：副 writer 写入失败不影响主流程，仅记录到 stderr fallback
		if _, err := e.svcWriter.Write([]byte(svcFormatted)); err != nil {
			stderrWarn("extension svc log write failed (ignored)", err)
		}
	}

	return n, err
}

// Close 关闭扩展日志器
func (e *ExtensionLogger) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.svcWriter != nil {
		// C-01-001 修复：副 writer Close 失败不阻塞主 writer 关闭，仅记录到 stderr
		if err := e.svcWriter.Close(); err != nil {
			stderrWarn("extension svc log close failed (ignored)", err)
		}
	}
	return e.writer.Close()
}

// Path 返回日志文件路径
func (e *ExtensionLogger) Path() string {
	return e.writer.Path()
}

// SupdLifecycleLog supd_lifecycle 触发的扩展输出写入 supd.log
// REQ-F-010: supd_lifecycle 触发的扩展输出写入 supd.log
func SupdLifecycleLog(extName string, msg string, logDir string) {
	timestamp := time.Now().Format(time.RFC3339Nano)
	line := fmt.Sprintf("[%s] [info] [ext:%s] %s\n", timestamp, extName, msg)

	// 直接写入 supd.log
	if supdLogger != nil {
		// C-01-001 修复：显式接收错误并记录到 stderr fallback
		if _, err := supdLogger.Write([]byte(line)); err != nil {
			stderrWarn("supd lifecycle log write failed", err)
		}
		return
	}

	// supdLogger 未初始化时，直接写文件
	path := filepath.Join(logDir, "supd.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	// C-01-001 修复：fallback 路径的 Close 失败仅记录到 stderr
	defer func() {
		if cerr := f.Close(); cerr != nil {
			stderrWarn("supd lifecycle log fallback close failed (ignored)", cerr)
		}
	}()
	// C-01-001 修复：显式接收错误并记录到 stderr fallback
	if _, err := f.WriteString(line); err != nil {
		stderrWarn("supd lifecycle log fallback write failed", err)
	}
}
