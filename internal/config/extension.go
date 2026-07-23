package config

import (
	"fmt"
	"os"

	"github.com/supdorg/supd/internal/identity"
)

// ExtensionMeta 扩展 meta.yaml 完整映射
// REQ-2.2.3: 扩展定义与 meta.yaml Schema
type ExtensionMeta struct {
	Name           string   `yaml:"name" json:"name"`
	Version        string   `yaml:"version" json:"version"`
	Description    string   `yaml:"description" json:"description,omitempty"`
	Enabled        *bool    `yaml:"enabled" json:"enabled"`
	Runtime        string   `yaml:"runtime" json:"runtime,omitempty"`
	Entry          string   `yaml:"entry" json:"entry"`
	TimeoutSeconds int      `yaml:"timeout_seconds" json:"timeout_seconds"`
	RunAs          string   `yaml:"run_as" json:"run_as,omitempty"`
	RunAsUID       int      `yaml:"run_as_uid" json:"run_as_uid,omitempty"`          // UID 模式：直接指定 uid（与 run_as 互斥）
	RunAsGID       int      `yaml:"run_as_gid" json:"run_as_gid,omitempty"`          // UID 模式：直接指定 gid（0 表示 = run_as_uid）
	RunAsGroups    []int    `yaml:"run_as_groups" json:"run_as_groups,omitempty"`    // UID 模式：补充组 gid 列表
	Concurrency    string   `yaml:"concurrency" json:"concurrency"`
	UI             UIConfig `yaml:"ui" json:"ui"`
	Actions        []Action `yaml:"actions" json:"actions,omitempty"`
	Triggers       Triggers `yaml:"triggers" json:"triggers"`
}

// UIConfig 扩展 UI 配置
// REQ-2.2.3: ui 段配置
type UIConfig struct {
	ShowLogs    *bool  `yaml:"show_logs" json:"show_logs"`
	ButtonStyle string `yaml:"button_style" json:"button_style,omitempty"`
	Icon        string `yaml:"icon" json:"icon,omitempty"`
}

// Action 扩展动作定义
// REQ-2.2.3: actions 段
type Action struct {
	ID          string   `yaml:"id" json:"id"`
	Label       string   `yaml:"label" json:"label,omitempty"`
	ButtonStyle string   `yaml:"button_style" json:"button_style,omitempty"`
	Args        []string `yaml:"args" json:"args,omitempty"`
}

// Triggers 触发器定义
// REQ-2.2.3: triggers 段
// REQ-2.2.3: 4种触发器类型 on_demand/on_schedule/service_lifecycle/supd_lifecycle
type Triggers struct {
	OnDemand         *bool                     `yaml:"on_demand" json:"on_demand,omitempty"`
	OnSchedule       []TriggerSchedule         `yaml:"on_schedule" json:"on_schedule,omitempty"`
	ServiceLifecycle []TriggerServiceLifecycle `yaml:"service_lifecycle" json:"service_lifecycle,omitempty"`
	SupdLifecycle    []TriggerSupdLifecycle    `yaml:"supd_lifecycle" json:"supd_lifecycle,omitempty"`
}

// TriggerSchedule 定时触发器
// REQ-2.2.3: on_schedule 段
type TriggerSchedule struct {
	Cron           string                `yaml:"cron" json:"cron"`
	Action         string                `yaml:"action" json:"action"`
	RetryOnFailure *RetryOnFailureConfig `yaml:"retry_on_failure,omitempty" json:"retry_on_failure,omitempty"`
}

// RetryOnFailureConfig on_schedule 触发器的失败重试配置
// REQ-D-004: retry_on_failure — 失败后每次重试生成新的 run_id，
// 在任务历史中标记为重试，max_retries 用尽后不再重试
type RetryOnFailureConfig struct {
	MaxRetries      int `yaml:"max_retries" json:"max_retries"`
	IntervalMinutes int `yaml:"interval_minutes" json:"interval_minutes"`
}

// TriggerServiceLifecycle 服务生命周期触发器
// REQ-D-004, 2.2.3: service_lifecycle 段
// 事件类型：pre_start | post_ready | on_failure | pre_stop
type TriggerServiceLifecycle struct {
	Event  string `yaml:"event" json:"event"`
	Action string `yaml:"action" json:"action"`
}

// TriggerSupdLifecycle supd 生命周期触发器
// REQ-D-004, 2.2.3: supd_lifecycle 段
// 事件类型：pre_start | post_ready | pre_shutdown
type TriggerSupdLifecycle struct {
	Event  string `yaml:"event" json:"event"`
	Action string `yaml:"action" json:"action"`
}

// SetExtensionDefaults 为扩展 meta.yaml 未设置的字段填充默认值
// REQ-2.2.3: 可选字段默认值
// REQ-2.4.4: 数值锁定清单中的默认值必须与需求规格说明书一致
func SetExtensionDefaults(meta *ExtensionMeta) {
	if meta.Enabled == nil {
		v := true
		meta.Enabled = &v
	}
	if meta.TimeoutSeconds == 0 {
		meta.TimeoutSeconds = DefaultExtensionTimeoutSeconds // REQ-2.2.3, REQ-2.2.8: 默认600秒
	}
	if meta.Concurrency == "" {
		meta.Concurrency = "replace" // REQ-2.2.7: 默认 replace
	}
	if meta.UI.ShowLogs == nil {
		v := true
		meta.UI.ShowLogs = &v
	}
	if meta.UI.ButtonStyle == "" {
		meta.UI.ButtonStyle = "default"
	}
	// 有 actions 时 on_demand 默认 true
	if len(meta.Actions) > 0 && meta.Triggers.OnDemand == nil {
		v := true
		meta.Triggers.OnDemand = &v
	}
	// 为每个 action 设置 button_style 默认值
	for i := range meta.Actions {
		if meta.Actions[i].ButtonStyle == "" {
			meta.Actions[i].ButtonStyle = meta.UI.ButtonStyle
		}
	}
}

// LoadExtension 从 meta.yaml 文件加载扩展配置，填充默认值并校验
func LoadExtension(path string) (*ExtensionMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read extension meta: %w", err)
	}

	meta := &ExtensionMeta{}
	if err := SafeUnmarshal(data, meta, DefaultSafeYAMLOptions); err != nil {
		return nil, fmt.Errorf("parse extension meta %s: %w", path, err)
	}

	SetExtensionDefaults(meta)
	if err := ValidateExtension(meta); err != nil {
		return nil, err
	}

	return meta, nil
}

// ToCredentialSpec 从扩展配置构造身份 spec。
// §2.2.13: User 模式（run_as）与 UID 模式（run_as_uid/run_as_gid/run_as_groups）互斥，
// 互斥由 ValidateExtension 保证，此处直接映射。
func (m *ExtensionMeta) ToCredentialSpec() identity.CredentialSpec {
	return identity.CredentialSpec{
		User:   m.RunAs,
		UID:    m.RunAsUID,
		GID:    m.RunAsGID,
		Groups: m.RunAsGroups,
	}
}
