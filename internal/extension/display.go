package extension

import (
	"strconv"

	"github.com/supdorg/supd/internal/config"
)

// ExtensionDisplayState 扩展展示状态
// REQ-F-024: 扩展 UI 展示规则
type ExtensionDisplayState string

const (
	// DisplayActive 正常可用：有 actions 且 enabled
	DisplayActive ExtensionDisplayState = "active"
	// DisplayAutomated 自动化：无 actions 但有 triggers
	DisplayAutomated ExtensionDisplayState = "automated"
	// DisplayDisabled 已禁用：enabled=false
	DisplayDisabled ExtensionDisplayState = "disabled"
	// DisplayConfigError 配置错误：triggers.action 引用不存在的 action
	DisplayConfigError ExtensionDisplayState = "config_error"
)

// GetDisplayState 根据扩展配置判定展示状态
// REQ-F-024: 扩展展示规则
//   - enabled=false → DisplayDisabled
//   - triggers.action 引用不存在的 action id → DisplayConfigError
//   - 无 actions → DisplayAutomated
//   - 其他 → DisplayActive
func GetDisplayState(meta *config.ExtensionMeta) ExtensionDisplayState {
	// enabled=false → DisplayDisabled
	if meta.Enabled != nil && !*meta.Enabled {
		return DisplayDisabled
	}

	// triggers.action 引用不存在的 action id → DisplayConfigError
	if hasConfigErrors(meta) {
		return DisplayConfigError
	}

	// 无 actions → DisplayAutomated
	if len(meta.Actions) == 0 {
		return DisplayAutomated
	}

	// 其他 → DisplayActive
	return DisplayActive
}

// GetConfigErrors 返回扩展配置错误列表
// REQ-F-024: 检查 triggers.action 是否引用了不存在的 action id
func GetConfigErrors(meta *config.ExtensionMeta) []string {
	var errors []string

	// 构建已定义的 action ID 集合
	actionIDs := make(map[string]bool)
	for _, a := range meta.Actions {
		actionIDs[a.ID] = true
	}

	// 检查 on_schedule 中的 action 引用
	for i, s := range meta.Triggers.OnSchedule {
		if s.Action != "" && !actionIDs[s.Action] {
			errors = append(errors, "triggers.on_schedule["+strconv.Itoa(i)+"].action: references undefined action "+s.Action)
		}
	}

	// 检查 service_lifecycle 中的 action 引用
	for i, s := range meta.Triggers.ServiceLifecycle {
		if s.Action != "" && !actionIDs[s.Action] {
			errors = append(errors, "triggers.service_lifecycle["+strconv.Itoa(i)+"].action: references undefined action "+s.Action)
		}
	}

	// 检查 supd_lifecycle 中的 action 引用
	for i, s := range meta.Triggers.SupdLifecycle {
		if s.Action != "" && !actionIDs[s.Action] {
			errors = append(errors, "triggers.supd_lifecycle["+strconv.Itoa(i)+"].action: references undefined action "+s.Action)
		}
	}

	return errors
}

// hasConfigErrors 判断扩展是否存在配置错误
// REQ-F-024: triggers.action 引用不存在的 action id 视为配置错误
func hasConfigErrors(meta *config.ExtensionMeta) bool {
	return len(GetConfigErrors(meta)) > 0
}

// GetTriggerType 根据扩展配置推断主要触发器类型。
// REQ-C-008: 触发器类型固定4种 on_demand/on_schedule/service_lifecycle/supd_lifecycle
// 一个扩展可配置多种触发器，按优先级返回主要类型（非 on_demand 优先）：
// on_schedule > service_lifecycle > supd_lifecycle > on_demand
func GetTriggerType(meta *config.ExtensionMeta) string {
	if len(meta.Triggers.OnSchedule) > 0 {
		return "on_schedule"
	}
	if len(meta.Triggers.ServiceLifecycle) > 0 {
		return "service_lifecycle"
	}
	if len(meta.Triggers.SupdLifecycle) > 0 {
		return "supd_lifecycle"
	}
	return "on_demand"
}


