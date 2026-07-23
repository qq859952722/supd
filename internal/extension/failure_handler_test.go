package extension

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/supdorg/supd/internal/config"
)

// TestToRetryConfig 测试 RetryOnFailureConfig → RetryConfig 转换
// REQ-D-004: retry_on_failure 配置解析
func TestToRetryConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.RetryOnFailureConfig
		want *RetryConfig
	}{
		{
			name: "nil config returns nil",
			cfg:  nil,
			want: nil,
		},
		{
			name: "MaxRetries=0 returns nil",
			cfg:  &config.RetryOnFailureConfig{MaxRetries: 0, IntervalMinutes: 5},
			want: nil,
		},
		{
			name: "negative MaxRetries returns nil",
			cfg:  &config.RetryOnFailureConfig{MaxRetries: -1, IntervalMinutes: 5},
			want: nil,
		},
		{
			name: "valid config",
			cfg:  &config.RetryOnFailureConfig{MaxRetries: 3, IntervalMinutes: 5},
			want: &RetryConfig{MaxRetries: 3, IntervalMinutes: 5},
		},
		{
			name: "IntervalMinutes=0 defaults to 1",
			cfg:  &config.RetryOnFailureConfig{MaxRetries: 2, IntervalMinutes: 0},
			want: &RetryConfig{MaxRetries: 2, IntervalMinutes: 1},
		},
		{
			name: "negative IntervalMinutes defaults to 1",
			cfg:  &config.RetryOnFailureConfig{MaxRetries: 2, IntervalMinutes: -5},
			want: &RetryConfig{MaxRetries: 2, IntervalMinutes: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToRetryConfig(tt.cfg)
			if tt.want == nil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected %+v, got nil", tt.want)
			}
			if got.MaxRetries != tt.want.MaxRetries {
				t.Errorf("MaxRetries = %d, want %d", got.MaxRetries, tt.want.MaxRetries)
			}
			if got.IntervalMinutes != tt.want.IntervalMinutes {
				t.Errorf("IntervalMinutes = %d, want %d", got.IntervalMinutes, tt.want.IntervalMinutes)
			}
		})
	}
}

// TestTriggerSchedule_RetryOnFailureParsing 测试 YAML 解析 retry_on_failure 字段
// REQ-D-004: on_schedule 的 retry_on_failure 配置
func TestTriggerSchedule_RetryOnFailureParsing(t *testing.T) {
	tmpDir := t.TempDir()
	metaPath := filepath.Join(tmpDir, "meta.yaml")

	yamlData := []byte(`
name: test-ext
version: "1.0"
runtime: bash
entry: run.sh
actions:
  - id: backup
    label: 备份
triggers:
  on_schedule:
    - cron: "0 3 * * *"
      action: backup
      retry_on_failure:
        max_retries: 3
        interval_minutes: 5
`)
	if err := os.WriteFile(metaPath, yamlData, 0644); err != nil {
		t.Fatal(err)
	}

	meta, err := config.LoadExtension(metaPath)
	if err != nil {
		t.Fatalf("LoadExtension failed: %v", err)
	}

	if len(meta.Triggers.OnSchedule) != 1 {
		t.Fatalf("expected 1 on_schedule trigger, got %d", len(meta.Triggers.OnSchedule))
	}

	schedule := meta.Triggers.OnSchedule[0]
	if schedule.RetryOnFailure == nil {
		t.Fatal("expected RetryOnFailure to be non-nil")
	}
	if schedule.RetryOnFailure.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", schedule.RetryOnFailure.MaxRetries)
	}
	if schedule.RetryOnFailure.IntervalMinutes != 5 {
		t.Errorf("IntervalMinutes = %d, want 5", schedule.RetryOnFailure.IntervalMinutes)
	}

	// 验证转换
	retryCfg := ToRetryConfig(schedule.RetryOnFailure)
	if retryCfg == nil {
		t.Fatal("expected non-nil RetryConfig")
	}
	if retryCfg.MaxRetries != 3 {
		t.Errorf("RetryConfig.MaxRetries = %d, want 3", retryCfg.MaxRetries)
	}
	if retryCfg.IntervalMinutes != 5 {
		t.Errorf("RetryConfig.IntervalMinutes = %d, want 5", retryCfg.IntervalMinutes)
	}
}

// TestTriggerSchedule_NoRetryOnFailure 测试未配置 retry_on_failure 时字段为 nil
func TestTriggerSchedule_NoRetryOnFailure(t *testing.T) {
	tmpDir := t.TempDir()
	metaPath := filepath.Join(tmpDir, "meta.yaml")

	yamlData := []byte(`
name: test-ext
version: "1.0"
runtime: bash
entry: run.sh
actions:
  - id: backup
    label: 备份
triggers:
  on_schedule:
    - cron: "0 3 * * *"
      action: backup
`)
	if err := os.WriteFile(metaPath, yamlData, 0644); err != nil {
		t.Fatal(err)
	}

	meta, err := config.LoadExtension(metaPath)
	if err != nil {
		t.Fatalf("LoadExtension failed: %v", err)
	}

	schedule := meta.Triggers.OnSchedule[0]
	if schedule.RetryOnFailure != nil {
		t.Fatalf("expected RetryOnFailure to be nil, got %+v", schedule.RetryOnFailure)
	}

	// 未配置时应返回 nil（不重试）
	if ToRetryConfig(schedule.RetryOnFailure) != nil {
		t.Fatal("expected nil RetryConfig when RetryOnFailure is nil")
	}
}
