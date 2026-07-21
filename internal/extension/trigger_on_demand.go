package extension

import (
	"context"
	"fmt"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/watch"
)

// OnDemandTrigger 手动触发器
// REQ-D-004: type: on_demand — 手动触发（CLI/API/WebUI按钮），支持 action 选择
type OnDemandTrigger struct {
	dispatcher *Dispatcher
}

// NewOnDemandTrigger 创建手动触发器
// REQ-D-004: 初始化 on_demand 触发器
func NewOnDemandTrigger(dispatcher *Dispatcher) *OnDemandTrigger {
	return &OnDemandTrigger{
		dispatcher: dispatcher,
	}
}

// Trigger 手动触发扩展执行
// REQ-D-004: 手动触发入口，在 DiscoveryResult 中找到对应扩展，
// 验证扩展有匹配的 on_demand trigger 和 action，直接执行
// triggerSource 区分调用来源（规格 §2.2.5）：webui / cli
func (t *OnDemandTrigger) Trigger(ctx context.Context, extName, actionID, triggerUser, triggerSource string, discovery *watch.DiscoveryResult) (*RunResult, error) {
	// REQ-D-004: 在 DiscoveryResult 中找到对应扩展
	extEntry, svcName, err := findExtensionByName(discovery, extName)
	if err != nil {
		return nil, err
	}

	// REQ-D-004: 验证扩展有 on_demand trigger
	if extEntry.Meta.Triggers.OnDemand == nil || !*extEntry.Meta.Triggers.OnDemand {
		return nil, fmt.Errorf("extension %s: on_demand trigger not enabled", extName)
	}

	// REQ-D-004: 验证 action 存在，并获取 action args
	// triggers 中的 action 字段引用 actions 的 id，触发时使用对应 action 的 args
	resolvedActionID, actionArgs := FindActionByID(extEntry.Meta, actionID)
	if actionID != "" && resolvedActionID != actionID {
		return nil, fmt.Errorf("extension %s: action %s not found", extName, actionID)
	}

	// REQ-D-004: 精确触发指定扩展，直接执行
	return t.triggerDirect(ctx, extEntry, resolvedActionID, actionArgs, svcName, triggerUser, triggerSource)
}

// triggerDirect 直接执行指定扩展的 on_demand 触发
// REQ-D-004: 精确触发指定扩展，不经过 Dispatcher 的通用匹配逻辑
func (t *OnDemandTrigger) triggerDirect(ctx context.Context, extEntry *watch.ExtensionEntry, actionID string, actionArgs []string, svcName, triggerUser, triggerSource string) (*RunResult, error) {
	workDir := buildWorkDir(t.dispatcher.baseDir, extEntry)

	// A-05-001 修复：TriggerSource 使用调用方传入的值（webui/cli），不再硬编码 "on_demand"
	// "on_demand" 是 EventType 值，不是 TriggerSource（规格 §2.2.5）
	tc := TriggerContext{
		EventType:     "on_demand",
		TriggerSource: triggerSource,
		TriggerUser:   triggerUser,
		ActionID:      actionID,
		ActionArgs:    actionArgs,
		ServiceName:   svcName,
		WorkDir:       workDir,
	}

	// B-05-001 修复：通过 ConcurrencyManager 执行，应用 concurrency 策略
	result, err := t.dispatcher.executeWithConcurrency(ctx, extEntry.Meta, tc, actionID, nil)
	if err != nil {
		return result, err
	}

	return result, nil
}

// findExtensionByName 在 DiscoveryResult 中按名称查找扩展
// REQ-D-004: 先查全局扩展，再查服务级扩展
func findExtensionByName(discovery *watch.DiscoveryResult, extName string) (*watch.ExtensionEntry, string, error) {
	// 先查全局扩展
	if extEntry, ok := discovery.GlobalExts[extName]; ok {
		return extEntry, "", nil
	}

	// 再查服务级扩展
	for svcName, svcEntry := range discovery.Services {
		if extEntry, ok := svcEntry.Extensions[extName]; ok {
			return extEntry, svcName, nil
		}
	}

	return nil, "", fmt.Errorf("extension %s: not found in discovery result", extName)
}

// validateActionID 验证 actionID 在扩展的 actions 列表中存在
// REQ-D-004: triggers.action 引用不存在的 id → 配置错误
func validateActionID(meta *config.ExtensionMeta, actionID string) bool {
	if actionID == "" {
		// 空 actionID 将使用第一个 action，始终有效
		return len(meta.Actions) > 0
	}
	for _, a := range meta.Actions {
		if a.ID == actionID {
			return true
		}
	}
	return false
}
