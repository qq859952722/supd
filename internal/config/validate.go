package config

import (
	"fmt"
	"net"
	"strings"
)

// ValidateConfig 校验配置的合法性
// REQ-D-006, REQ-C-009: 校验规则
func ValidateConfig(cfg *Config) error {
	s := &cfg.Settings

	// auth_mode 必须是三种之一
	// REQ-2.7.1: none | local_skip | always_token
	validAuthModes := map[string]bool{"none": true, "local_skip": true, "always_token": true}
	if !validAuthModes[s.AuthMode] {
		return fmt.Errorf("settings.auth_mode: invalid value %q, must be one of: none, local_skip, always_token", s.AuthMode)
	}

	// auth_mode != none 时 auth_token 必填
	if s.AuthMode != "none" && s.AuthToken == "" {
		return fmt.Errorf("settings.auth_token: required when auth_mode is not 'none'")
	}

	// local_networks 必须是合法的 CIDR
	// K-04-002 修复：使用 net.ParseCIDR 校验每个网段
	for i, n := range s.LocalNetworks {
		if _, _, err := net.ParseCIDR(strings.TrimSpace(n)); err != nil {
			return fmt.Errorf("settings.local_networks[%d]: invalid CIDR %q: %w", i, n, err)
		}
	}

	// log_level 必须是四种之一
	validLogLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLogLevels[s.LogLevel] {
		return fmt.Errorf("settings.log_level: invalid value %q, must be one of: debug, info, warn, error", s.LogLevel)
	}

	// 数值范围校验
	if s.LogMaxSizeMB <= 0 {
		return fmt.Errorf("settings.log_max_size_mb: must be positive, got %d", s.LogMaxSizeMB)
	}
	if s.LogMaxFiles <= 0 {
		return fmt.Errorf("settings.log_max_files: must be positive, got %d", s.LogMaxFiles)
	}
	if s.ShutdownGraceSeconds <= 0 {
		return fmt.Errorf("settings.shutdown_grace_seconds: must be positive, got %d", s.ShutdownGraceSeconds)
	}
	if s.ExtensionDefaultTimeoutSeconds <= 0 {
		return fmt.Errorf("settings.extension_default_timeout_seconds: must be positive, got %d", s.ExtensionDefaultTimeoutSeconds)
	}
	if s.ExtensionHardLimitSeconds <= s.ExtensionDefaultTimeoutSeconds {
		return fmt.Errorf("settings.extension_hard_limit_seconds (%d) must be greater than extension_default_timeout_seconds (%d)", s.ExtensionHardLimitSeconds, s.ExtensionDefaultTimeoutSeconds)
	}
	if s.RunHistoryRetentionSeconds <= 0 {
		return fmt.Errorf("settings.run_history_retention_seconds: must be positive, got %d", s.RunHistoryRetentionSeconds)
	}
	if s.FileHistoryVersions <= 0 {
		return fmt.Errorf("settings.file_history_versions: must be positive, got %d", s.FileHistoryVersions)
	}
	if s.MaxUploadSizeMB <= 0 {
		return fmt.Errorf("settings.max_upload_size_mb: must be positive, got %d", s.MaxUploadSizeMB)
	}

	// restart policy 校验
	d := &cfg.Defaults.Restart
	validPolicies := map[string]bool{"always": true, "on-failure": true, "never": true}
	if !validPolicies[d.Policy] {
		return fmt.Errorf("defaults.restart.policy: invalid value %q, must be one of: always, on-failure, never", d.Policy)
	}
	if d.BackoffMs <= 0 {
		return fmt.Errorf("defaults.restart.backoff_ms: must be positive, got %d", d.BackoffMs)
	}
	if d.MaxBackoffMs < d.BackoffMs {
		return fmt.Errorf("defaults.restart.max_backoff_ms (%d) must be >= backoff_ms (%d)", d.MaxBackoffMs, d.BackoffMs)
	}
	if d.Multiplier <= 0 {
		return fmt.Errorf("defaults.restart.multiplier: must be positive, got %d", d.Multiplier)
	}
	if d.ResetAfterSeconds < 0 {
		return fmt.Errorf("defaults.restart.reset_after_seconds: must be non-negative, got %d", d.ResetAfterSeconds)
	}

	// runtimes 键名校验：不允许空键或包含特殊字符
	for k, v := range cfg.Runtimes {
		if k == "" {
			return fmt.Errorf("runtimes: empty key not allowed")
		}
		if strings.Contains(k, "/") {
			return fmt.Errorf("runtimes: key %q must not contain '/'", k)
		}
		if v == "" {
			return fmt.Errorf("runtimes.%s: path must not be empty", k)
		}
		if !strings.HasPrefix(v, "/") {
			return fmt.Errorf("runtimes.%s: path %q must be absolute", k, v)
		}
	}

	// K-NEW-01 修复：将 config.yaml 的 extension_hard_limit_seconds 注入校验器
	// 使 ValidateExtensionMeta 中的硬上限检查与 config.yaml 配置一致
	SetExtensionHardLimit(s.ExtensionHardLimitSeconds)

	return nil
}
