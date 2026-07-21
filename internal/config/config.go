// Package config 实现 supd 配置解析与校验。
// REQ-C-009: 配置管理使用标准库 + yaml.v4，禁止使用 viper
// REQ-D-006: config.yaml 全局主配置
package config

import (
	"fmt"
	"os"
)

// Settings 全局设置
// REQ-D-006: config.yaml 全局主配置
type Settings struct {
	HTTPListen                     string   `yaml:"http_listen" json:"http_listen"`
	AuthMode                       string   `yaml:"auth_mode" json:"auth_mode"`
	AuthToken                      string   `yaml:"auth_token" json:"auth_token,omitempty"`
	LocalNetworks                  []string `yaml:"local_networks" json:"local_networks"`
	LogMaxSizeMB                   int      `yaml:"log_max_size_mb" json:"log_max_size_mb"`
	LogMaxFiles                    int      `yaml:"log_max_files" json:"log_max_files"`
	LogLevel                       string   `yaml:"log_level" json:"log_level"`
	ShutdownGraceSeconds           int      `yaml:"shutdown_grace_seconds" json:"shutdown_grace_seconds"`
	ExtensionDefaultTimeoutSeconds int      `yaml:"extension_default_timeout_seconds" json:"extension_default_timeout_seconds"`
	ExtensionHardLimitSeconds      int      `yaml:"extension_hard_limit_seconds" json:"extension_hard_limit_seconds"`
	RunHistoryRetentionSeconds     int      `yaml:"run_history_retention_seconds" json:"run_history_retention_seconds"`
	FileHistoryVersions            int      `yaml:"file_history_versions" json:"file_history_versions"`
	MaxUploadSizeMB                int      `yaml:"max_upload_size_mb" json:"max_upload_size_mb"`
}

// Config 全局配置
// REQ-D-006: config.yaml
type Config struct {
	Settings      Settings          `yaml:"settings"`
	EnvFiles      []string          `yaml:"env_files"`
	ExtensionDirs []string          `yaml:"extension_dirs"`
	Runtimes      map[string]string `yaml:"runtimes"`
	Defaults      DefaultRestart    `yaml:"defaults"`
}

// DefaultRestart 全局默认重启策略
type DefaultRestart struct {
	Restart RestartConfig `yaml:"restart" json:"restart"`
}

// RestartConfig 重启策略配置
type RestartConfig struct {
	Policy            string `yaml:"policy" json:"policy"`
	BackoffMs         int    `yaml:"backoff_ms" json:"backoff_ms"`
	MaxBackoffMs      int    `yaml:"max_backoff_ms" json:"max_backoff_ms"`
	Multiplier        int    `yaml:"multiplier" json:"multiplier"`
	MaxRetries        int    `yaml:"max_retries" json:"max_retries"`
	ResetAfterSeconds int    `yaml:"reset_after_seconds" json:"reset_after_seconds"`
}

// LoadConfig 从文件加载配置，填充默认值并校验
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &Config{}
	// REQ-E-003: 使用安全YAML解析（深度100层+别名50）
	if err := SafeUnmarshal(data, cfg, DefaultSafeYAMLOptions); err != nil {
		// C-03-001 修复：错误消息包含文件路径
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	SetDefaults(cfg)
	if err := ValidateConfig(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
