package extension

import (
	"log/slog"

	"github.com/supdorg/supd/internal/config"
)

// RetryConfig 重试配置
// REQ-F-021: 扩展运行失败可配置重试
// REQ-D-004: on_schedule 的 retry_on_failure 配置，含重试间隔
type RetryConfig struct {
	// MaxRetries 最大重试次数
	MaxRetries int
	// IntervalMinutes 重试间隔（分钟），REQ-D-004: on_schedule retry_on_failure.interval_minutes
	IntervalMinutes int
}

// ToRetryConfig 将 config.RetryOnFailureConfig 转换为 extension.RetryConfig
// REQ-D-004: retry_on_failure 配置解析
// 返回 nil 表示不重试（未配置或 MaxRetries <= 0）
func ToRetryConfig(cfg *config.RetryOnFailureConfig) *RetryConfig {
	if cfg == nil || cfg.MaxRetries <= 0 {
		return nil
	}
	interval := cfg.IntervalMinutes
	if interval <= 0 {
		interval = 1 // 默认 1 分钟，避免 0 间隔立即重试
	}
	return &RetryConfig{
		MaxRetries:      cfg.MaxRetries,
		IntervalMinutes: interval,
	}
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
