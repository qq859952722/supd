package extension

import "time"

// RunFilter 任务历史过滤条件
// REQ-F-020: 任务历史查询过滤
type RunFilter struct {
	// ExtensionName 按扩展名过滤
	ExtensionName string
	// ServiceName 按服务名过滤
	ServiceName string
	// State 按任务状态过滤
	State TaskState
	// TriggerType 按触发类型过滤（on_demand/on_schedule/service_lifecycle/supd_lifecycle）
	TriggerType string
	// Since 起始时间（包含）
	Since time.Time
	// Until 截止时间（包含）
	Until time.Time
	// Limit 返回结果上限，0 表示不限制
	Limit int
}

// TaskManager 任务状态管理器
// REQ-D-005: 任务状态管理
// REQ-F-020: 任务历史存储与查询
// REQ-C-007: 任务历史仅存内存，不持久化
type TaskManager struct {
	history    *TaskHistory
	maxLogSize int64 // REQ-F-024, 2.2.16: 扩展运行日志上限10MB硬编码
	concMgr    *ConcurrencyManager // N-03-001 修复：用于取消运行中的任务
}

// NewTaskManager 创建任务管理器
// REQ-F-020: retentionDays 默认7天
func NewTaskManager(retentionDays int) *TaskManager {
	return &TaskManager{
		history:    NewTaskHistory(retentionDays),
		maxLogSize: MaxExtensionLogSize, // REQ-F-024, 2.2.16: 10MB硬编码
	}
}

// SetConcurrencyManager 注入并发管理器
// N-03-001 修复：使 TaskManager 能取消运行中的任务
func (m *TaskManager) SetConcurrencyManager(mgr *ConcurrencyManager) {
	m.concMgr = mgr
}

// CancelRun 取消指定 runID 的运行中任务
// N-03-001 修复：实际调用 ConcurrencyManager.CancelRun 终止进程
func (m *TaskManager) CancelRun(runID string) bool {
	if m.concMgr == nil {
		return false
	}
	return m.concMgr.CancelRun(runID)
}

// RecordRun 记录任务运行结果到历史
// REQ-D-005: 记录任务最终状态
// REQ-F-020: 任务历史记录
func (m *TaskManager) RecordRun(result *RunResult) {
	m.history.Add(result)
}

// GetRun 根据 runID 获取任务运行结果
// REQ-D-005: 查询任务状态
// REQ-F-020: 任务历史查询
func (m *TaskManager) GetRun(runID string) *RunResult {
	return m.history.Get(runID)
}

// ListRuns 根据过滤条件列出任务运行结果
// REQ-F-020: 任务历史过滤查询
func (m *TaskManager) ListRuns(filter RunFilter) []*RunResult {
	return m.history.List(filter)
}

// ListRunsByExtension 按扩展名列出任务运行结果
// REQ-F-020: 按扩展名查询任务历史
func (m *TaskManager) ListRunsByExtension(extName string) []*RunResult {
	return m.history.List(RunFilter{ExtensionName: extName})
}

// ListRunsByService 按服务名列出任务运行结果
// REQ-F-020: 按服务名查询任务历史
func (m *TaskManager) ListRunsByService(serviceName string) []*RunResult {
	return m.history.List(RunFilter{ServiceName: serviceName})
}

// ClearRuns 清除匹配过滤条件的终态任务记录，返回删除数量
func (m *TaskManager) ClearRuns(filter RunFilter) int {
	return m.history.ClearByFilter(filter)
}

// UpdateProgress 实时更新任务进度
// 用于异步执行场景：stdout goroutine 解析 ::progress:: 后实时更新
func (m *TaskManager) UpdateProgress(runID string, progress int, resultMsg string) {
	m.history.UpdateProgress(runID, progress, resultMsg)
}
