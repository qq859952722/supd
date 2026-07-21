package extension

import "log/slog"

// RetryConfig 重试配置
// REQ-F-021: 扩展运行失败可配置重试
// REQ-D-004: on_schedule 的 retry_on_failure 配置，含重试间隔
type RetryConfig struct {
	// MaxRetries 最大重试次数
	MaxRetries int
	// IntervalMinutes 重试间隔（分钟），REQ-D-004: on_schedule retry_on_failure.interval_minutes
	IntervalMinutes int
}

// HandleExtensionFailure 处理扩展运行失败
// REQ-F-021: 扩展运行失败仅记录日志，不影响服务
// REQ-E-005: 扩展失败隔离 — 扩展运行失败不应影响所属服务状态
func HandleExtensionFailure(result *RunResult, logger *slog.Logger) {
	if logger == nil {
		return
	}

	logger.Error("extension run failed",
		"extension", result.ExtensionName,
		"action", result.ActionID,
		"run_id", result.RunID,
		"state", string(result.State),
		"exit_code", result.ExitCode,
		"result_msg", result.ResultMsg,
	)
}

// ShouldRetry 判断是否应该重试
// REQ-F-021: 仅 failed/timeout 状态可重试
// REQ-F-016: ::result:: warning 视为成功，不计失败统计，不触发重试
func ShouldRetry(state TaskState, retryConfig *RetryConfig, currentRetries int) bool {
	if retryConfig == nil {
		return false
	}

	// 仅 failed/timeout 状态可重试
	if state != TaskFailed && state != TaskTimeout {
		return false
	}

	// currentRetries < MaxRetries 时可重试
	if currentRetries >= retryConfig.MaxRetries {
		return false
	}

	return true
}
