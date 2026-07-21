package watch

import (
	"path/filepath"
	"strings"

	"github.com/supdorg/supd/internal/config"
)

// REQ-F-027, 2.4.3: 配置热重载分类

// ChangeCategory 变更分类
// REQ-F-027: 按需求规格说明书2.4.3节的分类表定义
type ChangeCategory string

const (
	// CategoryImmediate 立即生效
	CategoryImmediate ChangeCategory = "immediate"
	// CategoryNeedRestart 需重启服务
	CategoryNeedRestart ChangeCategory = "need_restart"
	// CategoryNextStart 下次启动生效
	CategoryNextStart ChangeCategory = "next_start"
	// CategoryNextRun 下次运行生效
	CategoryNextRun ChangeCategory = "next_run"
	// CategoryNeedSupdRestart 需重启supd
	CategoryNeedSupdRestart ChangeCategory = "need_supd_restart"
	// CategoryNoImpact 无影响（已运行服务不受影响）
	CategoryNoImpact ChangeCategory = "no_impact"
)

// ClassifiedChange 分类后的变更项
// REQ-F-027: 每个变更字段都有对应的分类和说明
type ClassifiedChange struct {
	File     string         // 变更的文件路径
	Category ChangeCategory // 分类
	Fields   []string       // 具体变更的字段列表
	Detail   string         // 分类说明（如"重启服务后生效"）
}

// ClassifyChange 根据文件类型和配置差异对变更进行分类
// REQ-F-027: 按需求规格说明书2.4.3节完整分类表
// filePath: 变更文件的路径
// oldConfig: 变更前的配置（类型取决于文件类型）
// newConfig: 变更后的配置（类型取决于文件类型）
func ClassifyChange(filePath string, oldConfig, newConfig interface{}) []ClassifiedChange {
	fileType := detectFileType(filePath)
	switch fileType {
	case "service":
		old, ok1 := oldConfig.(*config.ServiceConfig)
		new_, ok2 := newConfig.(*config.ServiceConfig)
		if ok1 && ok2 {
			return classifyServiceChange(filePath, old, new_)
		}
	case "extension":
		old, ok1 := oldConfig.(*config.ExtensionMeta)
		new_, ok2 := newConfig.(*config.ExtensionMeta)
		if ok1 && ok2 {
			return classifyExtensionChange(filePath, old, new_)
		}
	case "config":
		old, ok1 := oldConfig.(*config.Config)
		new_, ok2 := newConfig.(*config.Config)
		if ok1 && ok2 {
			return classifyConfigChange(filePath, old, new_)
		}
	case "service_env":
		// REQ-F-027: env.yaml（服务）→ 需重启服务
		return []ClassifiedChange{{
			File:     filePath,
			Category: CategoryNeedRestart,
			Fields:   []string{"env"},
			Detail:   "重启服务后生效",
		}}
	case "extension_env":
		// REQ-F-027: env.yaml（扩展）→ 下次运行生效
		return []ClassifiedChange{{
			File:     filePath,
			Category: CategoryNextRun,
			Fields:   []string{"env"},
			Detail:   "下次运行生效",
		}}
	case "global_env":
		// REQ-F-027: env/*.yaml（全局）→ 不影响已运行服务；新启动的服务用新 env
		return []ClassifiedChange{{
			File:     filePath,
			Category: CategoryNoImpact,
			Fields:   []string{"env"},
			Detail:   "不影响已运行服务，新启动的服务使用新环境变量",
		}}
	}
	return nil
}

// detectFileType 根据文件路径判断配置文件类型
// REQ-F-027: service.yaml / meta.yaml / env.yaml / config.yaml 各有不同的分类规则
func detectFileType(filePath string) string {
	base := filepath.Base(filePath)
	dir := filepath.Dir(filePath)

	switch base {
	case "service.yaml":
		return "service"
	case "meta.yaml":
		return "extension"
	case "config.yaml":
		return "config"
	case "env.yaml":
		// 区分服务级env、扩展级env、全局env
		// 路径结构：
		//   全局: /etc/supd/env/*.yaml (不在services/或extensions/下)
		//   服务: /etc/supd/services/<name>/env.yaml
		//   扩展: /etc/supd/extensions/<name>/env.yaml 或 /etc/supd/services/<name>/extensions/<name>/env.yaml
		// A-06-001: 使用路径分隔符感知匹配，避免 "myservices" 误匹配 "services"
		if pathContainsComponent(dir, "services") && pathContainsComponent(dir, "extensions") {
			return "extension_env"
		}
		if pathContainsComponent(dir, "services") {
			return "service_env"
		}
		if pathContainsComponent(dir, "extensions") {
			return "extension_env"
		}
		return "global_env"
	}

	// 全局 env 目录下的 .yaml 文件
	if strings.Contains(dir, string(filepath.Separator)+"env"+string(filepath.Separator)) ||
		strings.HasSuffix(dir, string(filepath.Separator)+"env") {
		if strings.HasSuffix(base, ".yaml") {
			return "global_env"
		}
	}

	return ""
}

// pathContainsComponent 检查路径中是否包含指定的路径分量（路径分隔符感知）
// A-06-001: 替代 strings.Contains 误匹配（如 "myservices" 匹配 "services"）
// 仅当 name 作为独立的路径分量出现时返回 true
func pathContainsComponent(dir, name string) bool {
	sep := string(filepath.Separator)
	if dir == name {
		return true
	}
	if strings.HasPrefix(dir, name+sep) {
		return true
	}
	return strings.Contains(dir, sep+name+sep) || strings.HasSuffix(dir, sep+name)
}

// classifyServiceChange 对比 ServiceConfig 各字段变更，按分类表归类
// REQ-F-027: 需求规格说明书2.4.3节 service.yaml 分类表
func classifyServiceChange(filePath string, old, new_ *config.ServiceConfig) []ClassifiedChange {
	var changes []ClassifiedChange

	// command/user/group/workdir → NeedRestart
	needRestartFields := []string{}
	if !sliceEqual(old.Command, new_.Command) {
		needRestartFields = append(needRestartFields, "command")
	}
	if old.User != new_.User {
		needRestartFields = append(needRestartFields, "user")
	}
	if old.Group != new_.Group {
		needRestartFields = append(needRestartFields, "group")
	}
	if old.Workdir != new_.Workdir {
		needRestartFields = append(needRestartFields, "workdir")
	}
	if len(needRestartFields) > 0 {
		changes = append(changes, ClassifiedChange{
			File:     filePath,
			Category: CategoryNeedRestart,
			Fields:   needRestartFields,
			Detail:   "重启服务后生效",
		})
	}

	// depends_on/readiness → NextStart
	nextStartFields := []string{}
	if !sliceEqual(old.DependsOn, new_.DependsOn) {
		nextStartFields = append(nextStartFields, "depends_on")
	}
	if !readinessEqual(old.Readiness, new_.Readiness) {
		nextStartFields = append(nextStartFields, "readiness")
	}
	if len(nextStartFields) > 0 {
		changes = append(changes, ClassifiedChange{
			File:     filePath,
			Category: CategoryNextStart,
			Fields:   nextStartFields,
			Detail:   "下次启动生效",
		})
	}

	// restart/logging/tags/stop/signals/package → Immediate
	immediateFields := []string{}
	if !restartEqual(old.Restart, new_.Restart) {
		immediateFields = append(immediateFields, "restart")
	}
	if !loggingEqual(old.Logging, new_.Logging) {
		immediateFields = append(immediateFields, "logging")
	}
	if !sliceEqual(old.Tags, new_.Tags) {
		immediateFields = append(immediateFields, "tags")
	}
	if !stopEqual(old.Stop, new_.Stop) {
		immediateFields = append(immediateFields, "stop")
	}
	if !signalsEqual(old.Signals, new_.Signals) {
		immediateFields = append(immediateFields, "signals")
	}
	if !packageEqual(old.Package, new_.Package) {
		immediateFields = append(immediateFields, "package")
	}
	if len(immediateFields) > 0 {
		changes = append(changes, ClassifiedChange{
			File:     filePath,
			Category: CategoryImmediate,
			Fields:   immediateFields,
			Detail:   "立即生效",
		})
	}

	return changes
}

// classifyExtensionChange 对比 ExtensionMeta 各字段变更，按分类表归类
// REQ-F-027: 需求规格说明书2.4.3节 meta.yaml 分类表
func classifyExtensionChange(filePath string, old, new_ *config.ExtensionMeta) []ClassifiedChange {
	var changes []ClassifiedChange

	// triggers/actions/concurrency → Immediate
	immediateFields := []string{}
	if !triggersEqual(old.Triggers, new_.Triggers) {
		immediateFields = append(immediateFields, "triggers")
	}
	if !actionsEqual(old.Actions, new_.Actions) {
		immediateFields = append(immediateFields, "actions")
	}
	if old.Concurrency != new_.Concurrency {
		immediateFields = append(immediateFields, "concurrency")
	}
	if len(immediateFields) > 0 {
		changes = append(changes, ClassifiedChange{
			File:     filePath,
			Category: CategoryImmediate,
			Fields:   immediateFields,
			Detail:   "立即生效",
		})
	}

	// timeout/run_as → NextRun
	nextRunFields := []string{}
	if old.TimeoutSeconds != new_.TimeoutSeconds {
		nextRunFields = append(nextRunFields, "timeout")
	}
	if old.RunAs != new_.RunAs {
		nextRunFields = append(nextRunFields, "run_as")
	}
	if len(nextRunFields) > 0 {
		changes = append(changes, ClassifiedChange{
			File:     filePath,
			Category: CategoryNextRun,
			Fields:   nextRunFields,
			Detail:   "下次运行生效",
		})
	}

	return changes
}

// classifyConfigChange 对比 Config 各字段变更，按分类表归类
// REQ-F-027: 需求规格说明书2.4.3节 config.yaml 分类表
func classifyConfigChange(filePath string, old, new_ *config.Config) []ClassifiedChange {
	var changes []ClassifiedChange

	// http_listen/auth_mode/auth_token/local_networks → NeedSupdRestart
	needSupdRestartFields := []string{}
	if old.Settings.HTTPListen != new_.Settings.HTTPListen {
		needSupdRestartFields = append(needSupdRestartFields, "settings.http_listen")
	}
	if old.Settings.AuthMode != new_.Settings.AuthMode {
		needSupdRestartFields = append(needSupdRestartFields, "settings.auth_mode")
	}
	if old.Settings.AuthToken != new_.Settings.AuthToken {
		needSupdRestartFields = append(needSupdRestartFields, "settings.auth_token")
	}
	if !sliceEqual(old.Settings.LocalNetworks, new_.Settings.LocalNetworks) {
		needSupdRestartFields = append(needSupdRestartFields, "settings.local_networks")
	}
	if len(needSupdRestartFields) > 0 {
		changes = append(changes, ClassifiedChange{
			File:     filePath,
			Category: CategoryNeedSupdRestart,
			Fields:   needSupdRestartFields,
			Detail:   "重启 supd 后生效",
		})
	}

	// log_*/shutdown_grace_seconds/extension_*/max_upload_size_mb/run_history_retention_seconds/file_history_versions → Immediate
	immediateFields := []string{}
	if old.Settings.LogMaxSizeMB != new_.Settings.LogMaxSizeMB {
		immediateFields = append(immediateFields, "settings.log_max_size_mb")
	}
	if old.Settings.LogMaxFiles != new_.Settings.LogMaxFiles {
		immediateFields = append(immediateFields, "settings.log_max_files")
	}
	if old.Settings.LogLevel != new_.Settings.LogLevel {
		immediateFields = append(immediateFields, "settings.log_level")
	}
	if old.Settings.ShutdownGraceSeconds != new_.Settings.ShutdownGraceSeconds {
		immediateFields = append(immediateFields, "settings.shutdown_grace_seconds")
	}
	if old.Settings.ExtensionDefaultTimeoutSeconds != new_.Settings.ExtensionDefaultTimeoutSeconds {
		immediateFields = append(immediateFields, "settings.extension_default_timeout_seconds")
	}
	if old.Settings.ExtensionHardLimitSeconds != new_.Settings.ExtensionHardLimitSeconds {
		immediateFields = append(immediateFields, "settings.extension_hard_limit_seconds")
	}
	if old.Settings.MaxUploadSizeMB != new_.Settings.MaxUploadSizeMB {
		immediateFields = append(immediateFields, "settings.max_upload_size_mb")
	}
	if old.Settings.RunHistoryRetentionSeconds != new_.Settings.RunHistoryRetentionSeconds {
		immediateFields = append(immediateFields, "settings.run_history_retention_seconds")
	}
	if old.Settings.FileHistoryVersions != new_.Settings.FileHistoryVersions {
		immediateFields = append(immediateFields, "settings.file_history_versions")
	}
	if len(immediateFields) > 0 {
		changes = append(changes, ClassifiedChange{
			File:     filePath,
			Category: CategoryImmediate,
			Fields:   immediateFields,
			Detail:   "立即生效",
		})
	}

	// defaults.restart → Immediate
	if !restartEqual(&old.Defaults.Restart, &new_.Defaults.Restart) {
		changes = append(changes, ClassifiedChange{
			File:     filePath,
			Category: CategoryImmediate,
			Fields:   []string{"defaults.restart"},
			Detail:   "立即生效",
		})
	}

	return changes
}

// --- 比较辅助函数 ---

// sliceEqual 比较两个字符串切片是否相等
func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// readinessEqual 比较两个 ReadinessConfig 是否相等
func readinessEqual(a, b *config.ReadinessConfig) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Type == b.Type &&
		a.Fd == b.Fd &&
		a.Port == b.Port &&
		a.URL == b.URL &&
		a.ExpectedStatus == b.ExpectedStatus &&
		sliceEqual(a.Check, b.Check) &&
		a.IntervalSeconds == b.IntervalSeconds &&
		a.TimeoutSeconds == b.TimeoutSeconds
}

// restartEqual 比较两个 RestartConfig 是否相等
func restartEqual(a, b *config.RestartConfig) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Policy == b.Policy &&
		a.BackoffMs == b.BackoffMs &&
		a.MaxBackoffMs == b.MaxBackoffMs &&
		a.Multiplier == b.Multiplier &&
		a.MaxRetries == b.MaxRetries &&
		a.ResetAfterSeconds == b.ResetAfterSeconds
}

// loggingEqual 比较两个 LoggingConfig 是否相等
func loggingEqual(a, b *config.LoggingConfig) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	aEnabled := a.Enabled != nil && *a.Enabled
	bEnabled := b.Enabled != nil && *b.Enabled
	if a.Enabled == nil {
		aEnabled = true
	}
	if b.Enabled == nil {
		bEnabled = true
	}
	return aEnabled == bEnabled &&
		a.MaxSizeMB == b.MaxSizeMB &&
		a.MaxFiles == b.MaxFiles
}

// stopEqual 比较两个 StopConfig 是否相等
func stopEqual(a, b *config.StopConfig) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.GraceSeconds == b.GraceSeconds &&
		a.TimeoutSeconds == b.TimeoutSeconds
}

// signalsEqual 比较两个 SignalsConfig 是否相等
func signalsEqual(a, b *config.SignalsConfig) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Reload == b.Reload &&
		a.RotateLogs == b.RotateLogs &&
		a.GracefulQuit == b.GracefulQuit
}

// packageEqual 比较两个 PackageConfig 是否相等
func packageEqual(a, b *config.PackageConfig) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return sliceEqual(a.Include, b.Include) &&
		sliceEqual(a.Exclude, b.Exclude) &&
		a.Default == b.Default
}

// triggersEqual 比较两个 Triggers 是否相等
func triggersEqual(a, b config.Triggers) bool {
	aOnDemand := a.OnDemand != nil && *a.OnDemand
	bOnDemand := b.OnDemand != nil && *b.OnDemand
	if a.OnDemand == nil {
		aOnDemand = false
	}
	if b.OnDemand == nil {
		bOnDemand = false
	}
	if aOnDemand != bOnDemand {
		return false
	}
	if !triggerSchedulesEqual(a.OnSchedule, b.OnSchedule) {
		return false
	}
	if !triggerServiceLifecyclesEqual(a.ServiceLifecycle, b.ServiceLifecycle) {
		return false
	}
	if !triggerSupdLifecyclesEqual(a.SupdLifecycle, b.SupdLifecycle) {
		return false
	}
	return true
}

// triggerSchedulesEqual 比较两个 TriggerSchedule 切片是否相等
func triggerSchedulesEqual(a, b []config.TriggerSchedule) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Cron != b[i].Cron || a[i].Action != b[i].Action {
			return false
		}
	}
	return true
}

// triggerServiceLifecyclesEqual 比较两个 TriggerServiceLifecycle 切片是否相等
func triggerServiceLifecyclesEqual(a, b []config.TriggerServiceLifecycle) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Event != b[i].Event || a[i].Action != b[i].Action {
			return false
		}
	}
	return true
}

// triggerSupdLifecyclesEqual 比较两个 TriggerSupdLifecycle 切片是否相等
func triggerSupdLifecyclesEqual(a, b []config.TriggerSupdLifecycle) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Event != b[i].Event || a[i].Action != b[i].Action {
			return false
		}
	}
	return true
}

// actionsEqual 比较两个 Action 切片是否相等
func actionsEqual(a, b []config.Action) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID ||
			a[i].Label != b[i].Label ||
			a[i].ButtonStyle != b[i].ButtonStyle ||
			!sliceEqual(a[i].Args, b[i].Args) {
			return false
		}
	}
	return true
}
