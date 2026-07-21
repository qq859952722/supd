package extension

import (
	"fmt"
	"time"
)

// TriggerContext 触发上下文
// REQ-F-016, 2.2.5: 14个SUPD_*上下文环境变量的数据来源
type TriggerContext struct {
	// EventType 事件类型：on_demand/on_schedule/service_lifecycle/supd_lifecycle
	EventType string
	// TriggerSource 触发源：webui/cli/schedule/service_lifecycle/supd_lifecycle
	TriggerSource string
	// TriggerUser 触发者标识
	TriggerUser string
	// Phase 阶段：pre_start/post_ready/on_failure/pre_stop（lifecycle 触发时）
	Phase string
	// ServiceName service_lifecycle 时的服务名
	ServiceName string
	// ServiceDir 服务目录绝对路径（服务级扩展时注入为 SUPD_SERVICE_DIR）
	ServiceDir string
	// ServiceUser 服务级扩展触发时，对应服务的 user 字段值
	// REQ-F-023, 2.2.13: 服务级扩展默认 run_as = 服务的 user 字段值
	ServiceUser string
	// ServicePID 服务 PID
	ServicePID int
	// ServiceExitCode on_failure 时的退出码
	ServiceExitCode int
	// ServiceSignal on_failure 时的信号
	ServiceSignal int
	// RestartCount 服务重启次数
	RestartCount int
	// ActionID action id
	ActionID string
	// ActionArgs action 的 args
	ActionArgs []string
	// WorkDir 工作目录
	// REQ-F-022: 扩展执行时的工作目录，非空时优先使用；为空时使用 Executor.baseDir
	WorkDir string
	// RunID 预先生成的 run_id，非空时 Execute 使用此值，为空时自动生成
	// 用于异步执行场景：调用方预生成 run_id 并记录到 TaskManager，Execute 使用同一 run_id
	RunID string
}

// BuildSupdEnv 构造 SUPD_* 上下文环境变量
// REQ-F-016, 2.2.5: 13个变量，按适用场景注入
// 所有扩展：SUPD_EVENT/SUPD_TRIGGER_SOURCE/SUPD_TRIGGER_TIME/SUPD_TRIGGER_USER/SUPD_RUN_ID/SUPD_EXTENSION_NAME/SUPD_ACTION
// lifecycle 触发时：额外注入 SUPD_PHASE
// service_lifecycle 时：额外注入 SUPD_SERVICE/SUPD_SERVICE_PID
// on_failure 时：额外注入 SUPD_SERVICE_EXIT_CODE/SUPD_SERVICE_SIGNAL/SUPD_SERVICE_RESTART_COUNT
// 全局扩展在 service_lifecycle 触发时：注入 SUPD_SERVICE
func BuildSupdEnv(runID, extName string, tc TriggerContext) []string {
	env := make([]string, 0, 13)

	// 所有场景都注入的变量
	env = append(env,
		fmt.Sprintf("SUPD_EVENT=%s", tc.EventType),
		fmt.Sprintf("SUPD_TRIGGER_SOURCE=%s", tc.TriggerSource),
		fmt.Sprintf("SUPD_TRIGGER_TIME=%s", time.Now().Format(time.RFC3339)),
		fmt.Sprintf("SUPD_TRIGGER_USER=%s", tc.TriggerUser),
		fmt.Sprintf("SUPD_RUN_ID=%s", runID),
		fmt.Sprintf("SUPD_EXTENSION_NAME=%s", extName),
		fmt.Sprintf("SUPD_ACTION=%s", tc.ActionID),
	)

	// lifecycle 触发时额外注入 SUPD_PHASE
	if tc.EventType == "service_lifecycle" || tc.EventType == "supd_lifecycle" {
		env = append(env, fmt.Sprintf("SUPD_PHASE=%s", tc.Phase))
	}

	// service_lifecycle 时注入 SUPD_SERVICE/SUPD_SERVICE_PID
	// REQ-D-004, 2.2.5: pre_start 时 ServicePID 为 0（进程尚未启动），输出空字符串
	if tc.EventType == "service_lifecycle" {
		env = append(env, fmt.Sprintf("SUPD_SERVICE=%s", tc.ServiceName))
		if tc.ServicePID == 0 {
			env = append(env, "SUPD_SERVICE_PID=")
		} else {
			env = append(env, fmt.Sprintf("SUPD_SERVICE_PID=%d", tc.ServicePID))
		}
	}

	// 服务级扩展（有 ServiceName）注入 SUPD_SERVICE_DIR，便于扩展定位服务目录
	// 详见 docs/devlog/deviations.md DEV-009（规格外第14个环境变量，实用增强）
	if tc.ServiceName != "" && tc.ServiceDir != "" {
		env = append(env, fmt.Sprintf("SUPD_SERVICE_DIR=%s", tc.ServiceDir))
	}

	// on_failure 时注入 SUPD_SERVICE_EXIT_CODE/SUPD_SERVICE_SIGNAL/SUPD_SERVICE_RESTART_COUNT
	if tc.Phase == "on_failure" {
		env = append(env, fmt.Sprintf("SUPD_SERVICE_EXIT_CODE=%d", tc.ServiceExitCode))
		env = append(env, fmt.Sprintf("SUPD_SERVICE_SIGNAL=%d", tc.ServiceSignal))
		env = append(env, fmt.Sprintf("SUPD_SERVICE_RESTART_COUNT=%d", tc.RestartCount))
	}

	return env
}

// BuildCommand 构造扩展执行命令
// REQ-F-016, 2.2.5 第3步: <runtime 路径> entry args... 或 entry args...
// runtime 为空时直接 entry args...，runtime 不为空时 runtimePath entry args...
func BuildCommand(runtime string, runtimePath string, entry string, args []string) []string {
	var cmd []string

	if runtime != "" {
		cmd = append(cmd, runtimePath)
	}

	cmd = append(cmd, entry)
	cmd = append(cmd, args...)

	return cmd
}
