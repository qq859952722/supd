package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	// 写一个最小config（local_skip模式需要auth_token）
	minimal := `
settings:
  auth_token: "default-token-for-test"
`
	if err := os.WriteFile(cfgPath, []byte(minimal), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// 验证默认值
	if cfg.Settings.HTTPListen != ":7979" {
		t.Errorf("HTTPListen = %q, want :7979", cfg.Settings.HTTPListen)
	}
	if cfg.Settings.AuthMode != "local_skip" {
		t.Errorf("AuthMode = %q, want local_skip", cfg.Settings.AuthMode)
	}
	if cfg.Settings.LogMaxSizeMB != 10 {
		t.Errorf("LogMaxSizeMB = %d, want 10", cfg.Settings.LogMaxSizeMB)
	}
	if cfg.Settings.LogMaxFiles != 5 {
		t.Errorf("LogMaxFiles = %d, want 5", cfg.Settings.LogMaxFiles)
	}
	if cfg.Settings.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", cfg.Settings.LogLevel)
	}
	if cfg.Settings.ShutdownGraceSeconds != 30 {
		t.Errorf("ShutdownGraceSeconds = %d, want 30", cfg.Settings.ShutdownGraceSeconds)
	}
	if cfg.Settings.ExtensionDefaultTimeoutSeconds != 600 {
		t.Errorf("ExtensionDefaultTimeoutSeconds = %d, want 600", cfg.Settings.ExtensionDefaultTimeoutSeconds)
	}
	if cfg.Settings.ExtensionHardLimitSeconds != 1800 {
		t.Errorf("ExtensionHardLimitSeconds = %d, want 1800", cfg.Settings.ExtensionHardLimitSeconds)
	}
	if cfg.Settings.RunHistoryRetentionSeconds != 604800 {
		t.Errorf("RunHistoryRetentionSeconds = %d, want 604800", cfg.Settings.RunHistoryRetentionSeconds)
	}
	if cfg.Settings.FileHistoryVersions != 50 {
		t.Errorf("FileHistoryVersions = %d, want 50", cfg.Settings.FileHistoryVersions)
	}
	if cfg.Settings.MaxUploadSizeMB != 100 {
		t.Errorf("MaxUploadSizeMB = %d, want 100", cfg.Settings.MaxUploadSizeMB)
	}
	if cfg.Defaults.Restart.Policy != "always" {
		t.Errorf("Restart.Policy = %q, want always", cfg.Defaults.Restart.Policy)
	}
	if cfg.Defaults.Restart.BackoffMs != 1000 {
		t.Errorf("Restart.BackoffMs = %d, want 1000", cfg.Defaults.Restart.BackoffMs)
	}
	if cfg.Defaults.Restart.MaxBackoffMs != 30000 {
		t.Errorf("Restart.MaxBackoffMs = %d, want 30000", cfg.Defaults.Restart.MaxBackoffMs)
	}
	if cfg.Defaults.Restart.Multiplier != 2 {
		t.Errorf("Restart.Multiplier = %d, want 2", cfg.Defaults.Restart.Multiplier)
	}
	if cfg.Defaults.Restart.ResetAfterSeconds != 300 {
		t.Errorf("Restart.ResetAfterSeconds = %d, want 300", cfg.Defaults.Restart.ResetAfterSeconds)
	}
}

func TestLoadConfigFull(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	full := `
settings:
  http_listen: ":9090"
  auth_mode: always_token
  auth_token: "my-secret-token"
  local_networks:
    - "10.0.0.0/8"
  log_max_size_mb: 20
  log_max_files: 10
  log_level: debug
  shutdown_grace_seconds: 60
  extension_default_timeout_seconds: 300
  extension_hard_limit_seconds: 900
  run_history_retention_seconds: 86400
  file_history_versions: 30
  max_upload_size_mb: 50

env_files:
  - env/base.yaml
  - env/prod.yaml

extension_dirs:
  - extensions/
  - extra-ext/

runtimes:
  bun: /usr/local/bin/bun
  python3: /usr/bin/python3.12

defaults:
  restart:
    policy: on-failure
    backoff_ms: 2000
    max_backoff_ms: 60000
    multiplier: 3
    max_retries: 5
    reset_after_seconds: 600
`
	if err := os.WriteFile(cfgPath, []byte(full), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Settings.HTTPListen != ":9090" {
		t.Errorf("HTTPListen = %q, want :9090", cfg.Settings.HTTPListen)
	}
	if cfg.Settings.AuthMode != "always_token" {
		t.Errorf("AuthMode = %q, want always_token", cfg.Settings.AuthMode)
	}
	if len(cfg.EnvFiles) != 2 {
		t.Errorf("len(EnvFiles) = %d, want 2", len(cfg.EnvFiles))
	}
	if cfg.Defaults.Restart.Policy != "on-failure" {
		t.Errorf("Restart.Policy = %q, want on-failure", cfg.Defaults.Restart.Policy)
	}
	if cfg.Defaults.Restart.MaxRetries != 5 {
		t.Errorf("Restart.MaxRetries = %d, want 5", cfg.Defaults.Restart.MaxRetries)
	}
}

func TestValidateInvalidAuthMode(t *testing.T) {
	cfg := &Config{}
	SetDefaults(cfg)
	cfg.Settings.AuthMode = "invalid"
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected error for invalid auth_mode")
	}
}

func TestValidateMissingAuthToken(t *testing.T) {
	cfg := &Config{}
	SetDefaults(cfg)
	cfg.Settings.AuthMode = "always_token"
	cfg.Settings.AuthToken = ""
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected error for missing auth_token with always_token mode")
	}
}

func TestValidateInvalidLogLevel(t *testing.T) {
	cfg := &Config{}
	SetDefaults(cfg)
	cfg.Settings.LogLevel = "verbose"
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected error for invalid log_level")
	}
}

func TestValidateHardLimitTooSmall(t *testing.T) {
	cfg := &Config{}
	SetDefaults(cfg)
	cfg.Settings.ExtensionHardLimitSeconds = 100
	cfg.Settings.ExtensionDefaultTimeoutSeconds = 600
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected error for hard_limit < default_timeout")
	}
}

func TestValidateRuntimeRelativePath(t *testing.T) {
	cfg := &Config{}
	SetDefaults(cfg)
	cfg.Runtimes = map[string]string{
		"test": "relative/path",
	}
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected error for relative runtime path")
	}
}

func TestValidateInvalidRestartPolicy(t *testing.T) {
	cfg := &Config{}
	SetDefaults(cfg)
	cfg.Defaults.Restart.Policy = "sometimes"
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected error for invalid restart policy")
	}
}

func TestLoadConfigNonexistent(t *testing.T) {
	_, err := LoadConfig("/nonexistent/config.yaml")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadConfigInvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{{invalid yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfig(cfgPath)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}
