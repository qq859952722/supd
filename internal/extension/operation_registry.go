package extension

import (
	"sort"
	"sync"

	"github.com/supdorg/supd/internal/watch"
)

// ResponderRef 服务扩展响应者引用（§三.2 服务扩展响应操作）。
type ResponderRef struct {
	ServiceName   string `json:"service_name"`
	ExtensionName string `json:"extension_name"`
	ActionID      string `json:"action_id"`
}

// GlobalRef 全局扩展执行者引用（注册该操作的全局 action）。
// 服务阶段不会复制全局扩展（§四.2：全局扩展每次只执行一次），
// GlobalRef 仅用于全局阶段。
type GlobalRef struct {
	ExtensionName string `json:"extension_name"`
	ActionID      string `json:"action_id"`
}

// OperationInfo 操作注册信息（§三 操作注册模型）。
type OperationInfo struct {
	ID string `json:"id"`
	// Label 按钮名称：按稳定排序（extension_name→action_id）第一项决定。
	Label string `json:"label"`
	// ButtonStyle primary/default/danger，同上取第一项。
	ButtonStyle string `json:"button_style"`
	// Description 注册扩展 meta.yaml description，无则空串。
	Description string `json:"description,omitempty"`
	// Registrants 全局注册者 extension_name 列表（稳定序）。
	Registrants []string `json:"registrants"`
	// Responders 服务扩展响应者（service_name/extension_name/action_id，稳定序）。
	Responders []ResponderRef `json:"responders,omitempty"`
}

// OperationSnapshot Runner 触发时冻结的操作快照（§5.2 热重载快照）。
// 冻结后热重载只影响新 Execution。
type OperationSnapshot struct {
	ID          string
	Label       string
	ButtonStyle string
	Description string
	GlobalRuns  []GlobalRef     // 全局阶段执行者（稳定序）
	ServiceRuns []ResponderRef  // 服务阶段响应者（稳定序）
	Warnings    []string
}

// OperationRegistry 操作注册表（§三）。
// 规则：
//   - 仅全局扩展 action 的 operations 注册操作；服务扩展不生成按钮只作响应者；
//   - 无效/解析失败扩展（Meta==nil）不进入 Registry；
//   - 不同全局扩展同 ID 合并一个按钮；名称/样式差异记录 warning；
//   - 服务扩展引用不存在的操作 ID → warning，不进 Responders；
//   - Registry 由扩展加载/热重载时重建；无合法 operations 时为空。
type OperationRegistry struct {
	mu sync.RWMutex
	// ops 展示信息：id -> 合并后
	ops map[string]*OperationInfo
	// globals 全局阶段执行者：id -> 稳定序
	globals map[string][]GlobalRef
	// warnings id -> warning 列表
	warnings map[string][]string
}

// NewOperationRegistry 创建空 Registry。
func NewOperationRegistry() *OperationRegistry {
	return &OperationRegistry{
		ops:      make(map[string]*OperationInfo),
		globals:  make(map[string][]GlobalRef),
		warnings: make(map[string][]string),
	}
}

// globalReg 构建期间的单个全局注册者（保留 label/style/description 用于合并冲突判定）。
type globalReg struct {
	ref         GlobalRef
	label       string
	buttonStyle string
	description string
}

// Rebuild 由扩展加载/热重载完成时调用，从 DiscoveryResult 重建 Registry。
func (r *OperationRegistry) Rebuild(discovery *watch.DiscoveryResult) {
	r.mu.Lock()
	defer r.mu.Unlock()

	ops := make(map[string]*OperationInfo)
	globals := make(map[string][]GlobalRef)
	warnings := make(map[string][]string)

	if discovery == nil {
		r.ops = ops
		r.globals = globals
		r.warnings = warnings
		return
	}

	// 1) 全局扩展注册操作（仅 enabled 生效）。
	builder := make(map[string][]globalReg)
	for _, extEntry := range discovery.GlobalExts {
		if extEntry == nil || extEntry.Meta == nil {
			continue // 解析失败扩展不参与注册（§十二.5.2）
		}
		meta := extEntry.Meta
		if meta.Enabled != nil && !*meta.Enabled {
			continue
		}
		for _, a := range meta.Actions {
			for _, opID := range a.Operations {
				builder[opID] = append(builder[opID], globalReg{
					ref:         GlobalRef{ExtensionName: extEntry.Name, ActionID: a.ID},
					label:       a.Label,
					buttonStyle: a.ButtonStyle,
					description: meta.Description,
				})
			}
		}
	}

	for opID, regs := range builder {
		// 稳定排序 extension_name→action_id；第一项决定名称/样式。
		sort.SliceStable(regs, func(i, j int) bool {
			if regs[i].ref.ExtensionName != regs[j].ref.ExtensionName {
				return regs[i].ref.ExtensionName < regs[j].ref.ExtensionName
			}
			return regs[i].ref.ActionID < regs[j].ref.ActionID
		})
		first := regs[0]
		info := &OperationInfo{
			ID:          opID,
			Label:       first.label,
			ButtonStyle: first.buttonStyle,
			Description: first.description,
		}
		var regNames []string
		var globalRefs []GlobalRef
		for idx, g := range regs {
			globalRefs = append(globalRefs, g.ref)
			regNames = append(regNames, g.ref.ExtensionName)
			if idx > 0 && (g.label != first.label || g.buttonStyle != first.buttonStyle) {
				warnings[opID] = append(warnings[opID],
					"operation "+opID+": 全局扩展 "+g.ref.ExtensionName+" 与首个注册者名称/样式不一致，已采用首个注册者")
			}
		}
		// 排序并去重注册者名（同 ID 在同一扩展内已由配置校验唯一，但防御性去重）。
		sort.Strings(regNames)
		info.Registrants = regNames
		ops[opID] = info
		globals[opID] = globalRefs
	}

	// 2) 服务扩展响应匹配。
	if len(ops) > 0 {
		for _, svcEntry := range discovery.Services {
			if svcEntry == nil {
				continue
			}
			for _, extEntry := range svcEntry.Extensions {
				if extEntry == nil || extEntry.Meta == nil {
					continue
				}
				meta := extEntry.Meta
				if meta.Enabled != nil && !*meta.Enabled {
					continue
				}
				for _, a := range meta.Actions {
					for _, opID := range a.Operations {
						info, ok := ops[opID]
						if !ok {
							warnings[opID] = append(warnings[opID],
								"service "+svcEntry.Name+" extension "+extEntry.Name+" 引用了不存在的操作 id "+opID+"，已忽略")
							continue
						}
						info.Responders = append(info.Responders, ResponderRef{
							ServiceName:   svcEntry.Name,
							ExtensionName: extEntry.Name,
							ActionID:      a.ID,
						})
					}
				}
			}
		}
		// 服务响应者稳定排序 service_name→extension_name→action_id。
		for _, info := range ops {
			sort.SliceStable(info.Responders, func(i, j int) bool {
				if info.Responders[i].ServiceName != info.Responders[j].ServiceName {
					return info.Responders[i].ServiceName < info.Responders[j].ServiceName
				}
				if info.Responders[i].ExtensionName != info.Responders[j].ExtensionName {
					return info.Responders[i].ExtensionName < info.Responders[j].ExtensionName
				}
				return info.Responders[i].ActionID < info.Responders[j].ActionID
			})
		}
	}

	r.ops = ops
	r.globals = globals
	r.warnings = warnings
}

// List 返回所有已注册操作（按 ID 稳定序）。
func (r *OperationRegistry) List() []OperationInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]OperationInfo, 0, len(r.ops))
	for _, info := range r.ops {
		out = append(out, *info)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Get 返回某操作的合并展示信息；不存在返回 false。
func (r *OperationRegistry) Get(id string) (*OperationInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	info, ok := r.ops[id]
	if !ok {
		return nil, false
	}
	return info, true
}

// Warnings 返回某操作的配置 warning 列表。
func (r *OperationRegistry) Warnings(id string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.warnings[id]...)
}

// Snapshot 冻结某操作的执行快照；不存在返回 false。
// Runner 触发时使用冻结快照，热重载不影响已创建 Execution（§5.2）。
func (r *OperationRegistry) Snapshot(id string) (*OperationSnapshot, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	info, ok := r.ops[id]
	if !ok {
		return nil, false
	}
	snap := &OperationSnapshot{
		ID:          info.ID,
		Label:       info.Label,
		ButtonStyle: info.ButtonStyle,
		Description: info.Description,
		ServiceRuns: append([]ResponderRef(nil), info.Responders...),
		Warnings:    append([]string(nil), r.warnings[id]...),
	}
	snap.GlobalRuns = append([]GlobalRef(nil), r.globals[id]...)
	return snap, true
}