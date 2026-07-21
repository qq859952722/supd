package extension

import (
	"time"

	"github.com/supdorg/supd/internal/config"
)

// TaskState 任务状态类型
// REQ-F-016, 2.2.10: 7种任务状态，锁定不可新增
type TaskState string

const (
	// TaskPending 已创建但还没启动
	TaskPending TaskState = "pending"
	// TaskRunning 进程在跑
	TaskRunning TaskState = "running"
	// TaskSuccess exit 0 或 ::result:: success 或 ::result:: warning
	TaskSuccess TaskState = "success"
	// TaskFailed exit 非 0 或 ::result:: error
	TaskFailed TaskState = "failed"
	// TaskTimeout 超时被杀
	TaskTimeout TaskState = "timeout"
	// TaskCanceled 用户取消或被新任务 replace
	TaskCanceled TaskState = "canceled"
	// TaskKilled SIGKILL 强杀
	TaskKilled TaskState = "killed"
)

// MaxExtensionLogSize 扩展运行日志上限
// REQ-F-024, 2.2.16: 10MB硬编码（数值锁定）
const MaxExtensionLogSize int64 = 10 * 1024 * 1024

// DefaultTaskRetentionDays 任务历史默认保留天数
// REQ-F-020, 2.2.9: 数值锁定 7 天
// O-05-001 修复：引用 config.DefaultRetentionDays 消除重复定义
const DefaultTaskRetentionDays = config.DefaultRetentionDays

// RunResult 扩展运行结果
// REQ-F-016: 11步流程第11步，判定最终状态并记录
type RunResult struct {
	// RunID 本次运行的唯一标识（UUID）
	RunID string `json:"run_id"`
	// ExtensionName 扩展名
	ExtensionName string `json:"extension_name"`
	// ActionID action id
	ActionID string `json:"action_id"`
	// State 最终任务状态
	State TaskState `json:"state"`
	// ExitCode 进程退出码
	ExitCode int `json:"exit_code"`
	// Progress 进度 0-100（来自 ::progress:: 协议）
	Progress int `json:"progress"`
	// ResultMsg 来自 ::result:: 的消息
	ResultMsg string `json:"result_msg"`
	// ResultLevel 结果级别：success/warning/error
	ResultLevel string `json:"result_level"`
	// StartedAt 启动时间
	StartedAt time.Time `json:"started_at"`
	// FinishedAt 结束时间
	FinishedAt time.Time `json:"finished_at"`
	// TriggerType 触发类型：on_demand/on_schedule/service_lifecycle/supd_lifecycle
	TriggerType string `json:"trigger_type"`
	// ServiceName 服务级扩展所属服务名
	ServiceName string `json:"service_name"`
}

// IsSuccess 判断任务是否成功完成
// REQ-F-016: ::result:: warning 视为成功完成（带告警），不计入失败统计
func (r *RunResult) IsSuccess() bool {
	return r.State == TaskSuccess
}

// IsTerminal 判断任务是否处于终态
// REQ-F-016: success/failed/timeout/canceled/killed 为终态
func (r *RunResult) IsTerminal() bool {
	switch r.State {
	case TaskSuccess, TaskFailed, TaskTimeout, TaskCanceled, TaskKilled:
		return true
	default:
		return false
	}
}

// ProgressCallback 进度回调函数
// Execute() 在 stdout goroutine 解析 ::progress:: / ::result:: 时调用
// 用于异步执行场景：实时更新 TaskManager 中的任务进度
type ProgressCallback func(progress int, resultMsg string)
