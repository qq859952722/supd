package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadExtensionMinimal(t *testing.T) {
	tmpDir := t.TempDir()
	metaPath := filepath.Join(tmpDir, "meta.yaml")

	minimal := `
name: my-ext
version: 1.0.0
entry: run.sh
`
	if err := os.WriteFile(metaPath, []byte(minimal), 0644); err != nil {
		t.Fatal(err)
	}

	meta, err := LoadExtension(metaPath)
	if err != nil {
		t.Fatalf("LoadExtension failed: %v", err)
	}

	// 必填字段
	if meta.Name != "my-ext" {
		t.Errorf("Name = %q, want my-ext", meta.Name)
	}
	if meta.Version != "1.0.0" {
		t.Errorf("Version = %q, want 1.0.0", meta.Version)
	}
	if meta.Entry != "run.sh" {
		t.Errorf("Entry = %q, want run.sh", meta.Entry)
	}

	// 默认值
	if meta.Enabled == nil || *meta.Enabled != true {
		t.Error("Enabled = false, want true (default)")
	}
	if meta.TimeoutSeconds != 600 {
		t.Errorf("TimeoutSeconds = %d, want 600 (default)", meta.TimeoutSeconds)
	}
	if meta.Concurrency != "replace" {
		t.Errorf("Concurrency = %q, want replace (default)", meta.Concurrency)
	}
	if meta.UI.ShowLogs == nil || *meta.UI.ShowLogs != true {
		t.Error("UI.ShowLogs = false, want true (default)")
	}
	if meta.UI.ButtonStyle != "default" {
		t.Errorf("UI.ButtonStyle = %q, want default", meta.UI.ButtonStyle)
	}
}

func TestLoadExtensionFull(t *testing.T) {
	tmpDir := t.TempDir()
	metaPath := filepath.Join(tmpDir, "meta.yaml")

	full := `
name: backup
version: 2.1.0
description: Backup utility
enabled: true
runtime: python3
entry: backup.py
timeout_seconds: 300
run_as: backup-user
concurrency: replace

ui:
  show_logs: false
  button_style: primary

actions:
  - id: full-backup
    label: Full Backup
    button_style: danger
    args: ["--full", "--compress"]
  - id: incr-backup
    label: Incremental Backup

triggers:
  on_demand: true
  on_schedule:
    - cron: "0 */6 * * *"
      action: full-backup
  service_lifecycle:
    - event: pre_start
      action: incr-backup
  supd_lifecycle:
    - event: pre_shutdown
      action: full-backup
`
	if err := os.WriteFile(metaPath, []byte(full), 0644); err != nil {
		t.Fatal(err)
	}

	meta, err := LoadExtension(metaPath)
	if err != nil {
		t.Fatalf("LoadExtension failed: %v", err)
	}

	if meta.Name != "backup" {
		t.Errorf("Name = %q, want backup", meta.Name)
	}
	if meta.Version != "2.1.0" {
		t.Errorf("Version = %q, want 2.1.0", meta.Version)
	}
	if meta.Description != "Backup utility" {
		t.Errorf("Description = %q, want Backup utility", meta.Description)
	}
	if meta.Runtime != "python3" {
		t.Errorf("Runtime = %q, want python3", meta.Runtime)
	}
	if meta.Entry != "backup.py" {
		t.Errorf("Entry = %q, want backup.py", meta.Entry)
	}
	if meta.TimeoutSeconds != 300 {
		t.Errorf("TimeoutSeconds = %d, want 300", meta.TimeoutSeconds)
	}
	if meta.RunAs != "backup-user" {
		t.Errorf("RunAs = %q, want backup-user", meta.RunAs)
	}
	if meta.Concurrency != "replace" {
		t.Errorf("Concurrency = %q, want replace", meta.Concurrency)
	}
	if meta.UI.ShowLogs == nil || *meta.UI.ShowLogs != false {
		t.Error("UI.ShowLogs = true, want false")
	}
	if meta.UI.ButtonStyle != "primary" {
		t.Errorf("UI.ButtonStyle = %q, want primary", meta.UI.ButtonStyle)
	}
	if len(meta.Actions) != 2 {
		t.Fatalf("len(Actions) = %d, want 2", len(meta.Actions))
	}
	if meta.Actions[0].ID != "full-backup" {
		t.Errorf("Actions[0].ID = %q, want full-backup", meta.Actions[0].ID)
	}
	if meta.Actions[0].ButtonStyle != "danger" {
		t.Errorf("Actions[0].ButtonStyle = %q, want danger", meta.Actions[0].ButtonStyle)
	}
	if len(meta.Actions[0].Args) != 2 {
		t.Errorf("len(Actions[0].Args) = %d, want 2", len(meta.Actions[0].Args))
	}
	// action without explicit button_style inherits from ui.button_style
	if meta.Actions[1].ButtonStyle != "primary" {
		t.Errorf("Actions[1].ButtonStyle = %q, want primary (inherited from ui.button_style)", meta.Actions[1].ButtonStyle)
	}
	if meta.Triggers.OnDemand == nil || *meta.Triggers.OnDemand != true {
		t.Error("Triggers.OnDemand = false, want true")
	}
	if len(meta.Triggers.OnSchedule) != 1 {
		t.Fatalf("len(Triggers.OnSchedule) = %d, want 1", len(meta.Triggers.OnSchedule))
	}
	if meta.Triggers.OnSchedule[0].Cron != "0 */6 * * *" {
		t.Errorf("Triggers.OnSchedule[0].Cron = %q, want '0 */6 * * *'", meta.Triggers.OnSchedule[0].Cron)
	}
	if meta.Triggers.OnSchedule[0].Action != "full-backup" {
		t.Errorf("Triggers.OnSchedule[0].Action = %q, want full-backup", meta.Triggers.OnSchedule[0].Action)
	}
	if len(meta.Triggers.ServiceLifecycle) != 1 {
		t.Fatalf("len(Triggers.ServiceLifecycle) = %d, want 1", len(meta.Triggers.ServiceLifecycle))
	}
	if meta.Triggers.ServiceLifecycle[0].Event != "pre_start" {
		t.Errorf("Triggers.ServiceLifecycle[0].Event = %q, want pre_start", meta.Triggers.ServiceLifecycle[0].Event)
	}
	if len(meta.Triggers.SupdLifecycle) != 1 {
		t.Fatalf("len(Triggers.SupdLifecycle) = %d, want 1", len(meta.Triggers.SupdLifecycle))
	}
	if meta.Triggers.SupdLifecycle[0].Event != "pre_shutdown" {
		t.Errorf("Triggers.SupdLifecycle[0].Event = %q, want pre_shutdown", meta.Triggers.SupdLifecycle[0].Event)
	}
}

func TestValidateExtensionMissingName(t *testing.T) {
	meta := &ExtensionMeta{Version: "1.0.0", Entry: "run.sh"}
	SetExtensionDefaults(meta)
	if err := ValidateExtension(meta); err == nil {
		t.Error("expected error for missing name")
	}
}

func TestValidateExtensionMissingVersion(t *testing.T) {
	meta := &ExtensionMeta{Name: "my-ext", Entry: "run.sh"}
	SetExtensionDefaults(meta)
	if err := ValidateExtension(meta); err == nil {
		t.Error("expected error for missing version")
	}
}

func TestValidateExtensionMissingEntry(t *testing.T) {
	meta := &ExtensionMeta{Name: "my-ext", Version: "1.0.0"}
	SetExtensionDefaults(meta)
	if err := ValidateExtension(meta); err == nil {
		t.Error("expected error for missing entry")
	}
}

func TestValidateExtensionConcurrencyValid(t *testing.T) {
	validValues := []string{"replace", "serialize", "parallel", "debounce:5s", "debounce:100s", "debounce:1s"}
	for _, val := range validValues {
		meta := &ExtensionMeta{Name: "ext", Version: "1.0.0", Entry: "run.sh", Concurrency: val}
		SetExtensionDefaults(meta)
		if err := ValidateExtension(meta); err != nil {
			t.Errorf("concurrency %q should be valid, got error: %v", val, err)
		}
	}
}

func TestValidateExtensionConcurrencyInvalid(t *testing.T) {
	invalidValues := []string{
		"invalid",
		"debounce:",
		"debounce:s",
		"debounce:0s",
		"debounce:-1s",
		"debounce:abc",
		"debounce:1.5s",
	}
	for _, val := range invalidValues {
		meta := &ExtensionMeta{Name: "ext", Version: "1.0.0", Entry: "run.sh", Concurrency: val}
		SetExtensionDefaults(meta)
		if err := ValidateExtension(meta); err == nil {
			t.Errorf("concurrency %q should be invalid", val)
		}
	}
}

func TestValidateExtensionTriggerActionNotFound(t *testing.T) {
	meta := &ExtensionMeta{
		Name:    "ext",
		Version: "1.0.0",
		Entry:   "run.sh",
		Actions: []Action{{ID: "do-it", Label: "Do It"}},
		Triggers: Triggers{
			OnSchedule: []TriggerSchedule{
				{Cron: "0 * * * *", Action: "nonexistent"},
			},
		},
	}
	SetExtensionDefaults(meta)
	if err := ValidateExtension(meta); err == nil {
		t.Error("expected error for trigger referencing nonexistent action")
	}
}

func TestValidateExtensionDuplicateActionID(t *testing.T) {
	meta := &ExtensionMeta{
		Name:    "ext",
		Version: "1.0.0",
		Entry:   "run.sh",
		Actions: []Action{
			{ID: "run", Label: "Run"},
			{ID: "run", Label: "Run Again"},
		},
	}
	SetExtensionDefaults(meta)
	if err := ValidateExtension(meta); err == nil {
		t.Error("expected error for duplicate action id")
	}
}

func TestValidateExtensionButtonStyleInvalid(t *testing.T) {
	meta := &ExtensionMeta{
		Name:    "ext",
		Version: "1.0.0",
		Entry:   "run.sh",
		UI:      UIConfig{ButtonStyle: "fancy"},
	}
	SetExtensionDefaults(meta)
	if err := ValidateExtension(meta); err == nil {
		t.Error("expected error for invalid button_style")
	}
}

func TestValidateExtensionActionButtonStyleInvalid(t *testing.T) {
	meta := &ExtensionMeta{
		Name:    "ext",
		Version: "1.0.0",
		Entry:   "run.sh",
		Actions: []Action{
			{ID: "run", Label: "Run", ButtonStyle: "glow"},
		},
	}
	SetExtensionDefaults(meta)
	if err := ValidateExtension(meta); err == nil {
		t.Error("expected error for invalid action button_style")
	}
}

func TestValidateExtensionServiceLifecycleInvalidEvent(t *testing.T) {
	meta := &ExtensionMeta{
		Name:    "ext",
		Version: "1.0.0",
		Entry:   "run.sh",
		Actions: []Action{{ID: "run", Label: "Run"}},
		Triggers: Triggers{
			ServiceLifecycle: []TriggerServiceLifecycle{
				{Event: "paused", Action: "run"},
			},
		},
	}
	SetExtensionDefaults(meta)
	if err := ValidateExtension(meta); err == nil {
		t.Error("expected error for invalid service_lifecycle event")
	}
}

func TestValidateExtensionSupdLifecycleInvalidEvent(t *testing.T) {
	meta := &ExtensionMeta{
		Name:    "ext",
		Version: "1.0.0",
		Entry:   "run.sh",
		Actions: []Action{{ID: "run", Label: "Run"}},
		Triggers: Triggers{
			SupdLifecycle: []TriggerSupdLifecycle{
				{Event: "shutdown", Action: "run"},
			},
		},
	}
	SetExtensionDefaults(meta)
	if err := ValidateExtension(meta); err == nil {
		t.Error("expected error for invalid supd_lifecycle event")
	}
}

func TestValidateExtensionTimeoutTooLarge(t *testing.T) {
	meta := &ExtensionMeta{
		Name:           "ext",
		Version:        "1.0.0",
		Entry:          "run.sh",
		TimeoutSeconds: 2000,
	}
	SetExtensionDefaults(meta)
	if err := ValidateExtension(meta); err == nil {
		t.Error("expected error for timeout_seconds > 1800")
	}
}

func TestValidateExtensionTimeoutZero(t *testing.T) {
	meta := &ExtensionMeta{
		Name:           "ext",
		Version:        "1.0.0",
		Entry:          "run.sh",
		TimeoutSeconds: 0,
	}
	// 不调用 SetExtensionDefaults 以测试 timeout=0
	if err := ValidateExtension(meta); err == nil {
		t.Error("expected error for timeout_seconds = 0")
	}
}

// TestValidateExtensionTimeout_Boundary1800 锚定 1800s 硬上限的双边界行为
// L-02-001: 校验逻辑使用严格大于（> extensionHardLimit），因此 1800 应通过、1801 应拒绝。
// 同时覆盖 0 和负数拒绝路径，防止未来误改为 >= 或偏移边界。
// 注意：不调用 SetExtensionDefaults，因为它会将 0 转为 600（默认值），无法测 0 拒绝路径。
// 因此手动设置 Concurrency 为合法值以满足其他校验。
func TestValidateExtensionTimeout_Boundary1800(t *testing.T) {
	// 显式重置硬上限为默认值，防止其他测试通过 SetExtensionHardLimit 污染全局状态
	SetExtensionHardLimit(ExtensionHardLimitSeconds)

	tests := []struct {
		name      string
		timeout   int
		wantError bool
	}{
		{"exactly_1800_allowed", 1800, false},
		{"just_over_1801_rejected", 1801, true},
		{"zero_rejected", 0, true},
		{"negative_rejected", -1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta := &ExtensionMeta{
				Name:           "ext",
				Version:        "1.0.0",
				Entry:          "run.sh",
				TimeoutSeconds: tt.timeout,
				Concurrency:    "replace",
			}
			err := ValidateExtension(meta)
			if tt.wantError && err == nil {
				t.Errorf("timeout_seconds=%d: expected error, got nil", tt.timeout)
			}
			if !tt.wantError && err != nil {
				t.Errorf("timeout_seconds=%d: expected no error, got %v", tt.timeout, err)
			}
		})
	}
}

func TestValidateExtensionServiceLifecycleValidEvents(t *testing.T) {
	events := []string{"pre_start", "post_ready", "on_failure", "pre_stop"}
	for _, event := range events {
		meta := &ExtensionMeta{
			Name:    "ext",
			Version: "1.0.0",
			Entry:   "run.sh",
			Actions: []Action{{ID: "run", Label: "Run"}},
			Triggers: Triggers{
				ServiceLifecycle: []TriggerServiceLifecycle{
					{Event: event, Action: "run"},
				},
			},
		}
		SetExtensionDefaults(meta)
		if err := ValidateExtension(meta); err != nil {
			t.Errorf("service_lifecycle event %q should be valid, got error: %v", event, err)
		}
	}
}

func TestValidateExtensionSupdLifecycleValidEvents(t *testing.T) {
	events := []string{"pre_start", "post_ready", "pre_shutdown"}
	for _, event := range events {
		meta := &ExtensionMeta{
			Name:    "ext",
			Version: "1.0.0",
			Entry:   "run.sh",
			Actions: []Action{{ID: "run", Label: "Run"}},
			Triggers: Triggers{
				SupdLifecycle: []TriggerSupdLifecycle{
					{Event: event, Action: "run"},
				},
			},
		}
		SetExtensionDefaults(meta)
		if err := ValidateExtension(meta); err != nil {
			t.Errorf("supd_lifecycle event %q should be valid, got error: %v", event, err)
		}
	}
}

func TestLoadExtensionNonexistent(t *testing.T) {
	_, err := LoadExtension("/nonexistent/meta.yaml")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadExtensionInvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	metaPath := filepath.Join(tmpDir, "meta.yaml")
	if err := os.WriteFile(metaPath, []byte("{{invalid yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadExtension(metaPath)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestLoadExtensionEnabledFalse(t *testing.T) {
	tmpDir := t.TempDir()
	metaPath := filepath.Join(tmpDir, "meta.yaml")

	content := `
name: disabled-ext
version: 1.0.0
entry: run.sh
enabled: false
`
	if err := os.WriteFile(metaPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	meta, err := LoadExtension(metaPath)
	if err != nil {
		t.Fatalf("LoadExtension failed: %v", err)
	}
	if meta.Enabled == nil || *meta.Enabled != false {
		t.Error("Enabled should be false when explicitly set to false")
	}
}

func TestValidateExtensionServiceLifecycleActionNotFound(t *testing.T) {
	meta := &ExtensionMeta{
		Name:    "ext",
		Version: "1.0.0",
		Entry:   "run.sh",
		Actions: []Action{{ID: "run", Label: "Run"}},
		Triggers: Triggers{
			ServiceLifecycle: []TriggerServiceLifecycle{
				{Event: "started", Action: "nonexistent"},
			},
		},
	}
	SetExtensionDefaults(meta)
	if err := ValidateExtension(meta); err == nil {
		t.Error("expected error for service_lifecycle action not found")
	}
}

func TestValidateExtensionSupdLifecycleActionNotFound(t *testing.T) {
	meta := &ExtensionMeta{
		Name:    "ext",
		Version: "1.0.0",
		Entry:   "run.sh",
		Actions: []Action{{ID: "run", Label: "Run"}},
		Triggers: Triggers{
			SupdLifecycle: []TriggerSupdLifecycle{
				{Event: "pre_stop", Action: "nonexistent"},
			},
		},
	}
	SetExtensionDefaults(meta)
	if err := ValidateExtension(meta); err == nil {
		t.Error("expected error for supd_lifecycle action not found")
	}
}

func TestValidateExtensionActionMissingID(t *testing.T) {
	meta := &ExtensionMeta{
		Name:    "ext",
		Version: "1.0.0",
		Entry:   "run.sh",
		Actions: []Action{{Label: "Run"}},
	}
	SetExtensionDefaults(meta)
	if err := ValidateExtension(meta); err == nil {
		t.Error("expected error for action missing id")
	}
}

func TestValidateExtensionActionMissingLabel(t *testing.T) {
	meta := &ExtensionMeta{
		Name:    "ext",
		Version: "1.0.0",
		Entry:   "run.sh",
		Actions: []Action{{ID: "run"}},
	}
	SetExtensionDefaults(meta)
	if err := ValidateExtension(meta); err == nil {
		t.Error("expected error for action missing label")
	}
}

// TestValidateExtensionInvalidCron K-02-001: 验证非法 cron 表达式被拒绝。
// 规格 §2.2.3: on_schedule 触发器的 cron 表达式应在配置解析阶段校验格式。
func TestValidateExtensionInvalidCron(t *testing.T) {
	invalidCrons := []string{
		"invalid",
		"100 * * * *",       // 分钟越界（>59）
		"* 25 * * *",         // 小时越界（>23）
		"* * * * * * *",      // 字段过多（7 字段，标准 5 字段）
		"* * * *",            // 字段过少（4 字段）
		"abc def ghi jkl mno", // 非法字符
	}
	for _, cron := range invalidCrons {
		meta := &ExtensionMeta{
			Name:    "ext",
			Version: "1.0.0",
			Entry:   "run.sh",
			Actions:  []Action{{ID: "run", Label: "Run"}},
			Triggers: Triggers{
				OnSchedule: []TriggerSchedule{
					{Cron: cron, Action: "run"},
				},
			},
		}
		SetExtensionDefaults(meta)
		err := ValidateExtension(meta)
		if err == nil {
			t.Errorf("cron %q should be rejected, got nil error", cron)
			continue
		}
		if !strings.Contains(err.Error(), "invalid cron expression") {
			t.Errorf("cron %q: expected error containing 'invalid cron expression', got: %v", cron, err)
		}
	}
}

// TestValidateExtensionValidCron K-02-001 补充: 验证合法 cron 表达式被接受。
func TestValidateExtensionValidCron(t *testing.T) {
	validCrons := []string{
		"* * * * *",       // 每分钟
		"0 * * * *",       // 每小时整点
		"0 0 * * *",       // 每天 0 点
		"*/5 * * * *",     // 每 5 分钟
		"0 9 * * 1-5",     // 工作日 9 点
		"0 0 1 * *",       // 每月 1 号
	}
	for _, cron := range validCrons {
		meta := &ExtensionMeta{
			Name:    "ext",
			Version: "1.0.0",
			Entry:   "run.sh",
			Actions:  []Action{{ID: "run", Label: "Run"}},
			Triggers: Triggers{
				OnSchedule: []TriggerSchedule{
					{Cron: cron, Action: "run"},
				},
			},
		}
		SetExtensionDefaults(meta)
		if err := ValidateExtension(meta); err != nil {
			t.Errorf("cron %q should be valid, got error: %v", cron, err)
		}
	}
}

// TestValidateEntryPath_DotDot K-02-002: 验证 entry 路径含 ".." 被拒绝。
func TestValidateEntryPath_DotDot(t *testing.T) {
	dotDotPaths := []string{
		"../etc/passwd",
		"foo/../bar",
		"./..",
		"..",
		"foo/..",
	}
	for _, p := range dotDotPaths {
		meta := &ExtensionMeta{
			Name:    "ext",
			Version: "1.0.0",
			Entry:   p,
		}
		SetExtensionDefaults(meta)
		err := ValidateExtension(meta)
		if err == nil {
			t.Errorf("entry %q should be rejected (contains '..'), got nil", p)
			continue
		}
		if !strings.Contains(err.Error(), "..") {
			t.Errorf("entry %q: expected error mentioning '..', got: %v", p, err)
		}
	}
}

// TestValidateEntryPath_ShellMetaChar K-02-002: 验证 entry 路径含 shell 元字符被拒绝。
func TestValidateEntryPath_ShellMetaChar(t *testing.T) {
	shellMetaPaths := []string{
		"run.sh;rm -rf /",
		"run.sh|cat",
		"run.sh&&echo hi",
		"run.sh$HOME",
		"run.sh`whoami`",
	}
	for _, p := range shellMetaPaths {
		meta := &ExtensionMeta{
			Name:    "ext",
			Version: "1.0.0",
			Entry:   p,
		}
		SetExtensionDefaults(meta)
		err := ValidateExtension(meta)
		if err == nil {
			t.Errorf("entry %q should be rejected (shell meta char), got nil", p)
			continue
		}
		if !strings.Contains(err.Error(), "invalid character") {
			t.Errorf("entry %q: expected error mentioning 'invalid character', got: %v", p, err)
		}
	}
}

// TestValidateEntryPath_RedundantSlash K-02-002: 验证 entry 路径含冗余路径分隔符被拒绝。
func TestValidateEntryPath_RedundantSlash(t *testing.T) {
	redundantPaths := []string{
		"./run.sh",       // 前缀 ./
		"foo//bar",       // 双斜杠
		"foo/",           // 末尾斜杠
		"foo/./bar",      // 中间 ./
	}
	for _, p := range redundantPaths {
		meta := &ExtensionMeta{
			Name:    "ext",
			Version: "1.0.0",
			Entry:   p,
		}
		SetExtensionDefaults(meta)
		err := ValidateExtension(meta)
		if err == nil {
			t.Errorf("entry %q should be rejected (redundant path components), got nil", p)
			continue
		}
		if !strings.Contains(err.Error(), "redundant path") {
			t.Errorf("entry %q: expected error mentioning 'redundant path', got: %v", p, err)
		}
	}
}

// TestValidateEntryPath_Empty K-02-002 补充: 验证空 entry 路径被拒绝。
func TestValidateEntryPath_Empty(t *testing.T) {
	meta := &ExtensionMeta{
		Name:    "ext",
		Version: "1.0.0",
		Entry:   "",
	}
	SetExtensionDefaults(meta)
	err := ValidateExtension(meta)
	if err == nil {
		t.Error("empty entry should be rejected")
	}
}

// TestSetExtensionDefaults_DefaultTimeout600s
// L-02-001: 规格 §2.2.3 / §2.2.8 — 扩展默认 timeout=600 秒。
// 锚定 SetExtensionDefaults 在 TimeoutSeconds=0 时填入 600 默认值。
// 防止未来误改默认值（如改为 300 或 1200），影响扩展执行时序约定。
func TestSetExtensionDefaults_DefaultTimeout600s(t *testing.T) {
	meta := &ExtensionMeta{
		Name:           "ext",
		Version:        "1.0.0",
		Entry:          "run.sh",
		TimeoutSeconds: 0, // 触发默认值填充
	}
	SetExtensionDefaults(meta)
	if meta.TimeoutSeconds != DefaultExtensionTimeoutSeconds {
		t.Errorf("TimeoutSeconds = %d, want %d (DefaultExtensionTimeoutSeconds)", meta.TimeoutSeconds, DefaultExtensionTimeoutSeconds)
	}
	if meta.TimeoutSeconds != 600 {
		t.Errorf("TimeoutSeconds = %d, want 600 (REQ-2.2.3)", meta.TimeoutSeconds)
	}
}

// TestSetExtensionDefaults_NonZeroTimeoutPreserved
// L-02-001: 规格 §2.2.3 — 用户显式配置的 TimeoutSeconds 不应被默认值覆盖。
// 验证 SetExtensionDefaults 仅在 TimeoutSeconds=0 时填默认 600，非 0 值保持不变。
func TestSetExtensionDefaults_NonZeroTimeoutPreserved(t *testing.T) {
	meta := &ExtensionMeta{
		Name:           "ext",
		Version:        "1.0.0",
		Entry:          "run.sh",
		TimeoutSeconds: 300, // 用户显式配置低于默认值
	}
	SetExtensionDefaults(meta)
	if meta.TimeoutSeconds != 300 {
		t.Errorf("TimeoutSeconds = %d, want 300 (user value should be preserved)", meta.TimeoutSeconds)
	}
}

// TestValidateExtension_TimeoutBoundary600_IsValid
// L-02-001: 规格 §2.2.3 / §2.2.8 — 扩展默认 timeout=600s 是合法值。
// 锚定 600s 作为边界：恰好等于默认值时应通过校验（不被误判为 <=0 或 > 1800）。
func TestValidateExtension_TimeoutBoundary600_IsValid(t *testing.T) {
	SetExtensionHardLimit(ExtensionHardLimitSeconds) // 重置为 1800，防止其他测试污染
	meta := &ExtensionMeta{
		Name:           "ext",
		Version:        "1.0.0",
		Entry:          "run.sh",
		TimeoutSeconds: 600, // 恰好等于默认值
		Concurrency:    "replace",
	}
	if err := ValidateExtension(meta); err != nil {
		t.Errorf("expected 600s (default) to be valid, got error: %v", err)
	}
}

// TestValidateExtension_TimeoutBoundary601_IsValid
// L-02-001: 规格 §2.2.3 / §2.2.8 — 601s 超出默认 600s 但低于硬上限 1800s，应合法。
// 锚定 601s 边界：证明 600 不是硬上限（只是默认值），用户可配置更高值。
func TestValidateExtension_TimeoutBoundary601_IsValid(t *testing.T) {
	SetExtensionHardLimit(ExtensionHardLimitSeconds)
	meta := &ExtensionMeta{
		Name:           "ext",
		Version:        "1.0.0",
		Entry:          "run.sh",
		TimeoutSeconds: 601, // 超出默认 600，但低于硬上限 1800
		Concurrency:    "replace",
	}
	if err := ValidateExtension(meta); err != nil {
		t.Errorf("expected 601s (above default but below hard limit) to be valid, got error: %v", err)
	}
}

// TestValidateExtension_TimeoutBoundary599_IsValid
// L-02-001: 规格 §2.2.3 / §2.2.8 — 599s 低于默认 600s 但 >0，应合法。
// 锚定 599s 边界：证明用户可配置低于默认的 timeout（如快速失败场景）。
func TestValidateExtension_TimeoutBoundary599_IsValid(t *testing.T) {
	SetExtensionHardLimit(ExtensionHardLimitSeconds)
	meta := &ExtensionMeta{
		Name:           "ext",
		Version:        "1.0.0",
		Entry:          "run.sh",
		TimeoutSeconds: 599,
		Concurrency:    "replace",
	}
	if err := ValidateExtension(meta); err != nil {
		t.Errorf("expected 599s (below default but positive) to be valid, got error: %v", err)
	}
}

// TestValidateExtensionRunAsMutex 测试 run_as 与 run_as_uid 互斥校验
// §2.2.13: User 模式与 UID 模式不能同时指定
func TestValidateExtensionRunAsMutex(t *testing.T) {
	meta := &ExtensionMeta{
		Name:           "ext",
		Version:        "1.0.0",
		Entry:          "run.sh",
		TimeoutSeconds: 600,
		Concurrency:    "replace",
		RunAs:          "alice",
		RunAsUID:       1000,
	}
	err := ValidateExtension(meta)
	if err == nil {
		t.Fatal("expected error for run_as+run_as_uid mutual exclusion, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected mutual exclusion error, got: %v", err)
	}
}

// TestValidateExtensionRunAsUIDOnly 仅指定 run_as_uid（UID 模式）应通过
func TestValidateExtensionRunAsUIDOnly(t *testing.T) {
	meta := &ExtensionMeta{
		Name:           "ext",
		Version:        "1.0.0",
		Entry:          "run.sh",
		TimeoutSeconds: 600,
		Concurrency:    "replace",
		RunAsUID:       1000,
		RunAsGID:       1001,
		RunAsGroups:    []int{27, 100},
	}
	if err := ValidateExtension(meta); err != nil {
		t.Fatalf("unexpected error for run_as_uid only: %v", err)
	}
}

// TestValidateExtensionRunAsUIDNegative 测试 run_as_uid 负数拒绝
func TestValidateExtensionRunAsUIDNegative(t *testing.T) {
	meta := &ExtensionMeta{
		Name: "ext", Version: "1.0.0", Entry: "run.sh",
		TimeoutSeconds: 600, Concurrency: "replace",
		RunAsUID: -1,
	}
	err := ValidateExtension(meta)
	if err == nil {
		t.Fatal("expected error for negative run_as_uid, got nil")
	}
	if !strings.Contains(err.Error(), "run_as_uid: must be positive") {
		t.Errorf("expected 'run_as_uid: must be positive' error, got: %v", err)
	}
}

// TestValidateExtensionRunAsGIDNegative 测试 run_as_gid 负数拒绝
func TestValidateExtensionRunAsGIDNegative(t *testing.T) {
	meta := &ExtensionMeta{
		Name: "ext", Version: "1.0.0", Entry: "run.sh",
		TimeoutSeconds: 600, Concurrency: "replace",
		RunAsUID: 1000, RunAsGID: -5,
	}
	err := ValidateExtension(meta)
	if err == nil {
		t.Fatal("expected error for negative run_as_gid, got nil")
	}
	if !strings.Contains(err.Error(), "run_as_gid: must be non-negative") {
		t.Errorf("expected 'run_as_gid: must be non-negative' error, got: %v", err)
	}
}

// TestValidateExtensionRunAsGroupsNonPositive 测试 run_as_groups 含 0/负数 拒绝
func TestValidateExtensionRunAsGroupsNonPositive(t *testing.T) {
	for _, g := range []int{0, -1} {
		meta := &ExtensionMeta{
			Name: "ext", Version: "1.0.0", Entry: "run.sh",
			TimeoutSeconds: 600, Concurrency: "replace",
			RunAsUID: 1000, RunAsGroups: []int{g},
		}
		err := ValidateExtension(meta)
		if err == nil {
			t.Fatalf("expected error for non-positive run_as_groups %d, got nil", g)
		}
		if !strings.Contains(err.Error(), "run_as_groups[") || !strings.Contains(err.Error(), "must be positive") {
			t.Errorf("expected run_as_groups[] must be positive error, got: %v", err)
		}
	}
}
