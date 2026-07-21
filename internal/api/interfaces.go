package api

import (
	"context"
	"syscall"
	"time"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/core"
	"github.com/supdorg/supd/internal/extension"
)

// REQ-C-003: 在 api 包中定义接口，不修改 core/extension 包
// 这些接口供 Server 持有，实际实现在 cmd/supd 中注入。

// ServiceStateInfo 服务状态信息
type ServiceStateInfo struct {
	Name         string                `json:"name"`
	State        core.ServiceState     `json:"state"`
	PID          int                   `json:"pid,omitempty"`
	Uptime       int64                 `json:"uptime_seconds,omitempty"`
	Config       *config.ServiceConfig `json:"config,omitempty"`
	ConfigPath   string                `json:"config_path,omitempty"`
	Enabled      bool                  `json:"enabled"`
	RestartCount int                   `json:"restart_count"`
}

// StateProvider 提供服务状态查询
type StateProvider interface {
	GetServiceState(name string) (ServiceStateInfo, bool)
	ListServiceStates() map[string]ServiceStateInfo
}

// ServiceOperator 提供服务操作
type ServiceOperator interface {
	StartService(name string) error
	StopService(name string) error
	RestartService(name string) error
	SendSignal(name string, signal syscall.Signal) error
	ForceStopService(name string) error
	ClearFailedState(name string) error
}

// ExtensionInfo 扩展信息摘要
// REQ-2.2.14: 扩展列表展示含状态、运行历史、触发时机
type ExtensionInfo struct {
	Name         string                `json:"name"`
	Version      string                `json:"version"`
	Description  string                `json:"description,omitempty"`
	Enabled      bool                  `json:"enabled"`
	DisplayState string                `json:"display_state"`
	TriggerType  string                `json:"trigger_type"`
	Service      string                `json:"service,omitempty"`
	RunCount     int                   `json:"run_count"`
	SuccessCount int                   `json:"success_count"`
	FailCount    int                   `json:"fail_count"`
	LastRunAt    string                `json:"last_run_at,omitempty"`
	LastStatus   string                `json:"last_status,omitempty"`
	Meta         *config.ExtensionMeta `json:"meta,omitempty"`
	ConfigPath   string                `json:"config_path,omitempty"`
	EnvPath      string                `json:"env_path,omitempty"`
}

// ExtensionProvider 提供扩展信息与操作
type ExtensionProvider interface {
	ListExtensions() []ExtensionInfo
	GetExtension(name string) (*ExtensionInfo, bool)
	CreateExtension(meta *config.ExtensionMeta, service string) error
	UpdateExtension(name string, meta *config.ExtensionMeta, service string) error
	DeleteExtension(name string, service string) error
	SaveExtensionEnv(name string, envData *config.EnvFile, service string) error
	RunExtension(ctx context.Context, name string, actionID string, service string, dryRun bool) (*extension.RunResult, error)
	GetExtensionStatus(name string, service string) (map[string]any, error)
	// ExportExtension、ImportExtension、ConfirmImport 已删除 — 由 handler 层直接操作 archive 包
}

// TaskProvider 提供任务查询与操作
type TaskProvider interface {
	ListRuns(filter extension.RunFilter) []*extension.RunResult
	GetRun(runID string) *extension.RunResult
	CancelRun(runID string) error
	GetRunLogs(runID string, sincePos int64) ([]string, int64, error)
	DeleteRunLogs(runID string) error
	ClearRuns(filter extension.RunFilter) int
}

// CronProvider 提供定时任务信息
type CronProvider interface {
	ListCronEntries() []CronEntryInfo
	ListCronHistory(filter extension.RunFilter) []*extension.RunResult
}

// CronEntryInfo cron 条目信息
type CronEntryInfo struct {
	ExtensionName string `json:"extension_name"`
	ActionID      string `json:"action_id"`
	Schedule      string `json:"schedule"`
	NextRun       string `json:"next_run,omitempty"`
	Service       string `json:"service,omitempty"`
}

// SettingsProvider 提供设置读写
type SettingsProvider interface {
	GetSettings() *config.Settings
	UpdateSettings(settings *config.Settings) error
	GetEnv() (*config.EnvFile, error)
	UpdateEnv(envFile *config.EnvFile) error
	GetRuntimesConfig() map[string]string
	UpdateRuntimesConfig(runtimes map[string]string) error
	// 以下方法用于 Config 级别的字段（不在 Settings 结构体中）
	GetEnvFiles() []string
	UpdateEnvFiles(files []string) error
	GetExtensionDirs() []string
	UpdateExtensionDirs(dirs []string) error
	GetDefaults() config.DefaultRestart
	UpdateDefaults(defaults config.DefaultRestart) error
}

// RuntimeProvider 提供运行时管理
type RuntimeProvider interface {
	ListRuntimes() []RuntimeInfo
	UploadRuntime(name string, data []byte) error
	DeleteRuntime(name string) error
}

// RuntimeInfo 运行时信息
type RuntimeInfo struct {
	Alias     string `json:"alias"`
	Path      string `json:"path"`
	Source    string `json:"source"` // builtin/config/scan
	Available bool   `json:"available"`
}

// SystemProvider 提供系统状态
type SystemProvider interface {
	GetSystemStatus() SystemStatusInfo
}

// SystemStatusInfo 系统状态信息
type SystemStatusInfo struct {
	StartTime   time.Time `json:"start_time"`
	Version     string    `json:"version"`
	Uptime      int64     `json:"uptime_seconds"`
	HTTPListen  string    `json:"http_listen"`
	AuthMode    string    `json:"auth_mode"`
	WorkDir     string    `json:"work_dir"`
	CPU         float64   `json:"cpu_percent"`
	MemoryMB    float64   `json:"memory_mb"`
	DiskTotalMB float64   `json:"disk_total_mb"`
	DiskUsedMB  float64   `json:"disk_used_mb"`
}

// FileProvider 提供文件操作
type FileProvider interface {
	FileTree(basePath string) ([]FileTreeNode, error)
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, content []byte) error
	CreateFile(path string, content []byte) error
	CreateDir(path string) error
	DeleteFile(path string) error
	MoveFile(oldPath, newPath string) error
	FileHistory(path string) ([]FileVersion, error)
	RollbackFile(path string, version int) error
	ValidateFile(path string, content []byte) ([]ValidationError, error)
	SnapshotFile(path string) error
}

// FileTreeNode 文件树节点
type FileTreeNode struct {
	Name     string         `json:"name"`
	Path     string         `json:"path"`
	IsDir    bool           `json:"is_dir"`
	Size     int64          `json:"size,omitempty"` // 文件大小（字节），仅文件节点
	Children []FileTreeNode `json:"children,omitempty"`
}

// FileVersion 文件历史版本
type FileVersion struct {
	Version   int       `json:"version"`
	Timestamp time.Time `json:"timestamp"`
	Size      int64     `json:"size"`
}

// ValidationError YAML 校验错误
type ValidationError struct {
	Message string `json:"message"`
	Line    int    `json:"line,omitempty"`
	Column  int    `json:"column,omitempty"`
}

// AuthProvider 提供认证操作
type AuthProvider interface {
	VerifyToken(token string) bool
}

// LogProvider 提供日志操作
type LogProvider interface {
	GetServiceLogs(serviceName string, sincePos int64) ([]string, int64, error)
	SearchServiceLogs(serviceName string, pattern string, maxLines int) ([]string, error)
}

// WatchProvider 提供文件监听
type WatchProvider interface {
	ReloadConfig() error
	GetDiscovery() *DiscoveryResultInfo
}

// DiscoveryResultInfo 发现结果信息
type DiscoveryResultInfo struct {
	Services   map[string]ServiceDiscoveryInfo   `json:"services"`
	GlobalExts map[string]ExtensionDiscoveryInfo `json:"global_extensions"`
}

// ServiceDiscoveryInfo 服务发现信息
type ServiceDiscoveryInfo struct {
	Name       string                            `json:"name"`
	ConfigPath string                            `json:"config_path"`
	Extensions map[string]ExtensionDiscoveryInfo `json:"extensions"`
}

// ExtensionDiscoveryInfo 扩展发现信息
type ExtensionDiscoveryInfo struct {
	Name        string `json:"name"`
	ConfigPath  string `json:"config_path"`
	ServiceName string `json:"service_name,omitempty"`
}

// ServiceHistoryGetter 提供服务历史查询
type ServiceHistoryGetter interface {
	GetServiceHistory(name string) []HistoryEntry
	GetServiceDeaths(name string) []HistoryEntry
}

// HistoryEntry 历史记录条目
type HistoryEntry struct {
	Time     string `json:"time"`
	PID      int    `json:"pid"`
	ExitCode int    `json:"exit_code"`
	Duration int64  `json:"duration_seconds"`
	Reason   string `json:"reason"`
}
