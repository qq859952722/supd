package config

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/robfig/cron/v3"
)

// shellMetaChars 需要拦截的shell元字符（S-04: 扩展 entry 路径安全校验）
var shellMetaChars = map[rune]bool{
	';': true,
	'|': true,
	'&': true,
	'$': true,
	'`': true,
	'(': true,
	')': true,
	'{': true,
	'}': true,
	'\n': true,
	'\r': true,
}

// extensionHardLimit 扩展执行时长硬上限（秒）
// K-02-001: 默认为编译期常量 ExtensionHardLimitSeconds（REQ-2.2.8: 1800）
// 运行时可通过 SetExtensionHardLimit 从 config.yaml 的 settings.extension_hard_limit_seconds 注入
var extensionHardLimit = ExtensionHardLimitSeconds

// SetExtensionHardLimit 从 config.yaml 注入 extension_hard_limit_seconds
// 允许校验时使用用户配置的硬上限而非编译期常量
func SetExtensionHardLimit(seconds int) {
	if seconds > 0 {
		extensionHardLimit = seconds
	}
}

// validateEntryPath 校验扩展 entry 路径安全性
// S-04: 防止 entry 指向白名单外二进制（绝对路径/..路径/shell元字符）
func validateEntryPath(entry string) error {
	if entry == "" {
		return fmt.Errorf("entry: must not be empty")
	}
	// 不含 ..
	if strings.Contains(entry, "..") {
		return fmt.Errorf("entry: must not contain '..': %s", entry)
	}
	// 不含 shell 元字符
	for _, ch := range entry {
		if shellMetaChars[ch] {
			return fmt.Errorf("entry: contains invalid character '%c': %s", ch, entry)
		}
	}
	// 清理后与原始值一致（防止多余路径分隔符等）
	cleaned := filepath.Clean(entry)
	if cleaned != entry {
		return fmt.Errorf("entry: contains redundant path components: %s", entry)
	}
	return nil
}

// ValidateExtension 校验扩展 meta.yaml 的合法性
// REQ-2.2.3: meta.yaml Schema 校验规则
func ValidateExtension(meta *ExtensionMeta) error {
	// 必填字段校验
	if meta.Name == "" {
		return fmt.Errorf("name: required")
	}
	// K-02-001: 校验扩展名格式，与服务名一致 ^[a-z][a-z0-9-]*$
	if !serviceNameRegex.MatchString(meta.Name) {
		return fmt.Errorf("name: invalid value %q, must match ^[a-z][a-z0-9-]*$", meta.Name)
	}
	if meta.Version == "" {
		return fmt.Errorf("version: required")
	}
	if meta.Entry == "" {
		return fmt.Errorf("entry: required")
	}
	// S-04: 校验 entry 路径安全性（防止任意二进制执行）
	if err := validateEntryPath(meta.Entry); err != nil {
		return err
	}

	// timeout_seconds 范围校验
	// REQ-2.2.8: 硬上限 1800 秒（可通过 config.yaml extension_hard_limit_seconds 配置）
	if meta.TimeoutSeconds <= 0 {
		return fmt.Errorf("timeout_seconds: must be positive, got %d", meta.TimeoutSeconds)
	}
	// K-02-001: 使用可配置的 extensionHardLimit 而非编译期常量
	if meta.TimeoutSeconds > extensionHardLimit {
		return fmt.Errorf("timeout_seconds: must be <= %d, got %d", extensionHardLimit, meta.TimeoutSeconds)
	}

	// concurrency 格式校验
	// REQ-2.2.7: replace | serialize | parallel | debounce:Ns
	if err := validateConcurrency(meta.Concurrency); err != nil {
		return err
	}

	// ui.button_style 枚举校验
	// REQ-2.2.3: default | primary | danger
	if err := validateButtonStyle("ui.button_style", meta.UI.ButtonStyle); err != nil {
		return err
	}

	// actions 校验
	actionIDs := make(map[string]bool)
	for i, a := range meta.Actions {
		if a.ID == "" {
			return fmt.Errorf("actions[%d].id: required", i)
		}
		if a.Label == "" {
			return fmt.Errorf("actions[%d].label: required", i)
		}
		if actionIDs[a.ID] {
			return fmt.Errorf("actions[%d].id: duplicate id %q", i, a.ID)
		}
		actionIDs[a.ID] = true

		// action 级别 button_style 校验
		if err := validateButtonStyle(fmt.Sprintf("actions[%d].button_style", i), a.ButtonStyle); err != nil {
			return err
		}
	}

	// triggers 校验
	if err := validateTriggers(&meta.Triggers, actionIDs); err != nil {
		return err
	}

	return nil
}

// validateConcurrency 校验 concurrency 字段格式
// REQ-2.2.7: replace | serialize | parallel | debounce:Ns
func validateConcurrency(val string) error {
	switch val {
	case "replace", "serialize", "parallel":
		return nil
	}
	// 检查 debounce:Ns 格式
	if strings.HasPrefix(val, "debounce:") {
		suffix := strings.TrimPrefix(val, "debounce:")
		if suffix == "" {
			return fmt.Errorf("concurrency: invalid debounce format %q, expected debounce:Ns", val)
		}
		// 去掉末尾的 's' 后解析数字
		if !strings.HasSuffix(suffix, "s") {
			return fmt.Errorf("concurrency: invalid debounce format %q, expected debounce:Ns", val)
		}
		numStr := strings.TrimSuffix(suffix, "s")
		n, err := strconv.Atoi(numStr)
		if err != nil || n <= 0 {
			return fmt.Errorf("concurrency: invalid debounce format %q, N must be a positive integer", val)
		}
		// A-04-002 修复：REQ-2.2.7 规定 debounce:Ns 的 N 上限为 3600s
		if n > 3600 {
			return fmt.Errorf("concurrency: invalid debounce format %q, N must be <= 3600", val)
		}
		return nil
	}
	return fmt.Errorf("concurrency: invalid value %q, must be one of: replace, serialize, parallel, debounce:Ns", val)
}

// validateButtonStyle 校验 button_style 枚举值
// REQ-2.2.3: default | primary | danger
func validateButtonStyle(field, val string) error {
	switch val {
	case "default", "primary", "danger", "": // 空字符串在默认值设置前允许
		return nil
	}
	return fmt.Errorf("%s: invalid value %q, must be one of: default, primary, danger", field, val)
}

// validateTriggers 校验触发器定义
func validateTriggers(triggers *Triggers, actionIDs map[string]bool) error {
	// on_schedule 校验
	for i, s := range triggers.OnSchedule {
		if s.Cron == "" {
			return fmt.Errorf("triggers.on_schedule[%d].cron: required", i)
		}
		// K-02-001 修复：配置阶段校验 cron 表达式格式，避免延迟到运行时才发现
		if err := validateCronExpression(s.Cron); err != nil {
			return fmt.Errorf("triggers.on_schedule[%d].cron: %w", i, err)
		}
		if err := validateActionRef(fmt.Sprintf("triggers.on_schedule[%d].action", i), s.Action, actionIDs); err != nil {
			return err
		}
	}

	// service_lifecycle 校验
	// REQ-D-004, 2.2.3: pre_start | post_ready | on_failure | pre_stop
	validServiceEvents := map[string]bool{
		"pre_start": true, "post_ready": true, "on_failure": true, "pre_stop": true,
	}
	for i, s := range triggers.ServiceLifecycle {
		if !validServiceEvents[s.Event] {
			return fmt.Errorf("triggers.service_lifecycle[%d].event: invalid value %q, must be one of: pre_start, post_ready, on_failure, pre_stop", i, s.Event)
		}
		if err := validateActionRef(fmt.Sprintf("triggers.service_lifecycle[%d].action", i), s.Action, actionIDs); err != nil {
			return err
		}
	}

	// supd_lifecycle 校验
	// REQ-D-004, 2.2.3: pre_start | post_ready | pre_shutdown
	validSupdEvents := map[string]bool{
		"pre_start": true, "post_ready": true, "pre_shutdown": true,
	}
	for i, s := range triggers.SupdLifecycle {
		if !validSupdEvents[s.Event] {
			return fmt.Errorf("triggers.supd_lifecycle[%d].event: invalid value %q, must be one of: pre_start, post_ready, pre_shutdown", i, s.Event)
		}
		if err := validateActionRef(fmt.Sprintf("triggers.supd_lifecycle[%d].action", i), s.Action, actionIDs); err != nil {
			return err
		}
	}

	return nil
}

// validateActionRef 校验触发器中的 action 引用是否指向已定义的 actions
func validateActionRef(field, actionID string, actionIDs map[string]bool) error {
	if actionID == "" {
		return fmt.Errorf("%s: required", field)
	}
	if !actionIDs[actionID] {
		return fmt.Errorf("%s: references undefined action %q", field, actionID)
	}
	return nil
}

// validateCronExpression 校验 cron 表达式格式
// K-02-001 修复：使用 robfig/cron 的 ParseStandard 提前校验，避免错误延迟到运行时
// 标准 5 段 cron：分 时 日 月 周
func validateCronExpression(expr string) error {
	if _, err := cron.ParseStandard(expr); err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", expr, err)
	}
	return nil
}
