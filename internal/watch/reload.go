package watch

import (
	"bytes"
	"fmt"
	"os"
	"time"

	"github.com/supdorg/supd/internal/config"
)

// REQ-F-027, 2.4.3: 配置热重载管理

// PendingChange 待生效的变更
// REQ-F-027: "需重启服务"类型的变更标记为"待生效"，不自动重启服务，下次服务重启时应用
type PendingChange struct {
	ServiceName string            // 所属服务（全局配置为空）
	Changes     []ClassifiedChange // 变更列表
	DetectedAt  time.Time         // 检测时间
}

// ReloadResult 热重载处理结果
// REQ-F-027: 将变更分为立即生效和待生效两类
type ReloadResult struct {
	ImmediateChanges []ClassifiedChange // 立即生效的变更
	PendingChanges   []PendingChange    // 待生效的变更
	Errors           []error            // 重载过程中的错误
}

// ReloadManager 配置热重载管理器
// REQ-F-027: 管理配置变更的分类和待生效变更的追踪
// 方法由调用方在同一个 goroutine 中串行调用，不需要并发保护
type ReloadManager struct {
	discovery      *Discovery
	pendingChanges map[string]*PendingChange // key=服务名或"global"
}

// NewReloadManager 创建热重载管理器
// REQ-F-027: 依赖 Discovery 进行文件发现
func NewReloadManager(discovery *Discovery) *ReloadManager {
	return &ReloadManager{
		discovery:      discovery,
		pendingChanges: make(map[string]*PendingChange),
	}
}

// Reload 对比旧的 DiscoveryResult 和新的 DiscoveryResult，生成 ReloadResult
// REQ-F-027: 对每个变更的服务/扩展/配置，调用 ClassifyChange 进行分类
// REQ-F-027: 热重载不中断正在运行的服务和脚本，让它们继续正常运行
func (rm *ReloadManager) Reload(oldResult, newResult *DiscoveryResult) *ReloadResult {
	result := &ReloadResult{}

	// N-04-I2 修复：配置错误时保留旧配置（不中断服务）
	// 规格 §2.4: 配置错误时旧配置仍然生效，不中断服务
	preserved := PreserveOldConfigOnError(oldResult, newResult)
	for _, name := range preserved {
		result.Errors = append(result.Errors, fmt.Errorf("service %q config load failed, using old config", name))
	}

	// 1. 对比全局配置（config.yaml）
	rm.compareConfig(oldResult, newResult, result)

	// 2. 对比服务配置（service.yaml + env.yaml）
	rm.compareServices(oldResult, newResult, result)

	// 3. 对比全局扩展配置（meta.yaml + env.yaml）
	rm.compareGlobalExtensions(oldResult, newResult, result)

	// 4. 对比服务级扩展配置
	rm.compareServiceExtensions(oldResult, newResult, result)

	return result
}

// compareConfig 对比全局配置变更
// REQ-F-027: config.yaml 中不同字段有不同的生效方式
// A-06-001: DiscoveryResult 不携带 *config.Config，此函数无法直接分类。
// config.yaml 变更由调用方（internal/cli/run.go 的 applyReload）独立加载 baseDir/config.yaml，
// 调用 ReloadConfig(cfgPath, oldCfg, newCfg) 完成分类，并合并到 ReloadResult。
// 此处保留空实现以维持 Reload() 主路径结构不变，分类实际在调用方完成。
func (rm *ReloadManager) compareConfig(oldResult, newResult *DiscoveryResult, result *ReloadResult) {
}

// compareServices 对比服务配置变更
// REQ-F-027: 对比 DiscoveryResult 中每个服务的 ServiceConfig 和 EnvPath
func (rm *ReloadManager) compareServices(oldResult, newResult *DiscoveryResult, result *ReloadResult) {
	// 检查旧结果中的每个服务
	for name, oldSvc := range oldResult.Services {
		newSvc, exists := newResult.Services[name]
		if !exists {
			// 服务被删除，不做热重载处理（由上层处理）
			continue
		}

		// REQ-F-027: 对比 service.yaml
		changes := ClassifyChange(oldSvc.ConfigPath, oldSvc.Config, newSvc.Config)
		if len(changes) > 0 {
			rm.processChanges(name, changes, result)
		}

		// A-06-001 修复：服务级 env.yaml 变更检测增加内容对比，避免误报
		// REQ-F-027: env.yaml（服务）→ 需重启服务
		// 当 env.yaml 路径变化或内容变化时，将变更加入处理列表
		if newSvc.EnvPath != "" || oldSvc.EnvPath != "" {
			envPath := newSvc.EnvPath
			if envPath == "" {
				envPath = oldSvc.EnvPath // 被删除时用旧路径记录
			}
			// A-06-001 修复：对比 old/new env.yaml 内容，内容相同则不生成变更
			oldContent := readFileIfExists(oldSvc.EnvPath)
			newContent := readFileIfExists(newSvc.EnvPath)
			// 路径变化或内容变化时才生成变更
			if oldSvc.EnvPath != newSvc.EnvPath || !bytes.Equal(oldContent, newContent) {
				envChanges := ClassifyChange(envPath, nil, nil)
				if len(envChanges) > 0 {
					rm.processChanges(name, envChanges, result)
				}
			}
		}
	}

	// 新增服务由上层服务启动流程处理，此处不做热重载
}

// compareGlobalExtensions 对比全局扩展配置变更
// REQ-F-027: 对比全局扩展的 ExtensionMeta
func (rm *ReloadManager) compareGlobalExtensions(oldResult, newResult *DiscoveryResult, result *ReloadResult) {
	for name, oldExt := range oldResult.GlobalExts {
		newExt, exists := newResult.GlobalExts[name]
		if !exists {
			continue
		}

		changes := ClassifyChange(oldExt.ConfigPath, oldExt.Meta, newExt.Meta)
		if len(changes) > 0 {
			// 全局扩展的变更归属到 "global" 键
			rm.processChanges("global", changes, result)
		}
	}
}

// compareServiceExtensions 对比服务级扩展配置变更
// REQ-F-027: 对比每个服务下的扩展配置
func (rm *ReloadManager) compareServiceExtensions(oldResult, newResult *DiscoveryResult, result *ReloadResult) {
	for svcName, oldSvc := range oldResult.Services {
		newSvc, exists := newResult.Services[svcName]
		if !exists {
			continue
		}

		for extName, oldExt := range oldSvc.Extensions {
			newExt, exists := newSvc.Extensions[extName]
			if !exists {
				continue
			}

			changes := ClassifyChange(oldExt.ConfigPath, oldExt.Meta, newExt.Meta)
			if len(changes) > 0 {
				rm.processChanges(svcName, changes, result)
			}
		}
	}
}

// processChanges 将分类后的变更分为立即生效和待生效
// REQ-F-027: 立即生效变更放入 ImmediateChanges，待生效变更放入 PendingChanges
func (rm *ReloadManager) processChanges(serviceName string, changes []ClassifiedChange, result *ReloadResult) {
	var immediateChanges []ClassifiedChange
	var pendingChanges []ClassifiedChange

	for _, change := range changes {
		switch change.Category {
		case CategoryImmediate:
			immediateChanges = append(immediateChanges, change)
		case CategoryNeedRestart, CategoryNextStart, CategoryNextRun, CategoryNeedSupdRestart:
			pendingChanges = append(pendingChanges, change)
		case CategoryNoImpact:
			// 无影响的变更不记录
		}
	}

	if len(immediateChanges) > 0 {
		result.ImmediateChanges = append(result.ImmediateChanges, immediateChanges...)
	}

	if len(pendingChanges) > 0 {
		key := serviceName
		if key == "" {
			key = "global"
		}
		pc := PendingChange{
			ServiceName: serviceName,
			Changes:     pendingChanges,
			DetectedAt:  time.Now(),
		}
		result.PendingChanges = append(result.PendingChanges, pc)

		// 同时更新 ReloadManager 内部的 pendingChanges 缓存
		if existing, ok := rm.pendingChanges[key]; ok {
			existing.Changes = append(existing.Changes, pendingChanges...)
			existing.DetectedAt = time.Now()
		} else {
			rm.pendingChanges[key] = &PendingChange{
				ServiceName: serviceName,
				Changes:     pendingChanges,
				DetectedAt:  time.Now(),
			}
		}
	}
}

// GetPendingChanges 获取指定服务的待生效变更
// REQ-F-027: WebUI 在服务详情页对"待生效"的变更显示提示"配置已更新，重启服务后生效"
// serviceName: 服务名，空字符串返回全局待生效变更
func (rm *ReloadManager) GetPendingChanges(serviceName string) []PendingChange {
	key := serviceName
	if key == "" {
		key = "global"
	}

	if pc, ok := rm.pendingChanges[key]; ok {
		return []PendingChange{*pc}
	}
	return nil
}

// ClearPendingChanges 清除指定服务的待生效变更
// REQ-F-027: 服务重启后调用，清除该服务的待生效变更
func (rm *ReloadManager) ClearPendingChanges(serviceName string) {
	key := serviceName
	if key == "" {
		key = "global"
	}
	delete(rm.pendingChanges, key)
}

// ReloadConfig 对比全局配置变更的便捷方法
// REQ-F-027: config.yaml 变更需要对比 old/new Config
func (rm *ReloadManager) ReloadConfig(configPath string, oldCfg, newCfg *config.Config) *ReloadResult {
	result := &ReloadResult{}

	changes := ClassifyChange(configPath, oldCfg, newCfg)
	if len(changes) > 0 {
		rm.processChanges("global", changes, result)
	}

	return result
}

// ReloadServiceEnv 对比服务级 env.yaml 变更
// REQ-F-027: env.yaml（服务）→ 需重启服务
func (rm *ReloadManager) ReloadServiceEnv(envPath string, serviceName string) *ReloadResult {
	result := &ReloadResult{}

	changes := ClassifyChange(envPath, nil, nil)
	if len(changes) > 0 {
		rm.processChanges(serviceName, changes, result)
	}

	return result
}

// ReloadGlobalEnv 对比全局 env 变更
// REQ-F-027: env/*.yaml（全局）→ 不影响已运行服务；新启动的服务用新 env
func (rm *ReloadManager) ReloadGlobalEnv(envPath string) *ReloadResult {
	result := &ReloadResult{}

	changes := ClassifyChange(envPath, nil, nil)
	// 全局 env 属于 NoImpact 类型，不记录到 ImmediateChanges 或 PendingChanges
	// 但仍然返回给调用方，让调用方知道发生了变更
	result.ImmediateChanges = changes

	return result
}

// GetAllPendingChanges 获取所有待生效变更
// REQ-F-027: 用于 WebUI 展示所有待生效变更
func (rm *ReloadManager) GetAllPendingChanges() []PendingChange {
	var all []PendingChange
	for _, pc := range rm.pendingChanges {
		all = append(all, *pc)
	}
	return all
}

// HasPendingChanges 检查是否有待生效变更
// REQ-F-027: 用于判断是否需要提示用户
func (rm *ReloadManager) HasPendingChanges(serviceName string) bool {
	key := serviceName
	if key == "" {
		key = "global"
	}
	_, ok := rm.pendingChanges[key]
	return ok
}

// FormatPendingMessage 格式化待生效变更的提示信息
// REQ-F-027: WebUI 在服务详情页显示"配置已更新，重启服务后生效"
func FormatPendingMessage(pc PendingChange) string {
	if len(pc.Changes) == 0 {
		return ""
	}

	var fieldNames []string
	for _, change := range pc.Changes {
		fieldNames = append(fieldNames, change.Fields...)
	}

	if pc.ServiceName == "" || pc.ServiceName == "global" {
		return fmt.Sprintf("全局配置已更新（%s），需重启后生效", formatFieldList(fieldNames))
	}
	return fmt.Sprintf("服务 %s 配置已更新（%s），重启服务后生效", pc.ServiceName, formatFieldList(fieldNames))
}

// formatFieldList 格式化字段列表
func formatFieldList(fields []string) string {
	if len(fields) == 0 {
		return ""
	}
	result := fields[0]
	for i := 1; i < len(fields); i++ {
		result += ", " + fields[i]
	}
	return result
}

// readFileIfExists 读取文件内容，文件不存在或读取失败时返回 nil
// A-06-001 修复：用于 env.yaml 内容对比，避免误报
func readFileIfExists(path string) []byte {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return data
}

// PreserveOldConfigOnError 在配置加载失败时保留旧配置
// N-04-I2 修复：若某服务在旧结果中存在但在新结果中缺失，且新结果有该服务配置加载错误，
// 则复用旧配置，避免服务从注册表消失（规格 §2.4: 配置错误时旧配置仍然生效，不中断服务）
// 返回被保留的服务名列表，供调用方记录提示
func PreserveOldConfigOnError(oldResult, newResult *DiscoveryResult) []string {
	if oldResult == nil || newResult == nil {
		return nil
	}
	var preserved []string
	for name, oldSvc := range oldResult.Services {
		if _, exists := newResult.Services[name]; exists {
			continue
		}
		// 仅在配置加载失败（语法错误等，错误路径为 configPath）时保留
		// 服务目录被删除（无错误记录）或 service.yaml 缺失（错误路径为 svcDir）不保留
		if hasDiscoveryError(newResult.Errors, oldSvc.ConfigPath) {
			newResult.Services[name] = oldSvc
			preserved = append(preserved, name)
		}
	}
	return preserved
}

// hasDiscoveryError 检查错误列表中是否包含指定路径的错误
func hasDiscoveryError(errs []DiscoveryError, path string) bool {
	for _, e := range errs {
		if e.Path == path {
			return true
		}
	}
	return false
}
