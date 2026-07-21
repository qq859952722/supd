package extension

import (
	"sort"
	"sync"
	"time"
)

// TaskHistory 任务历史存储，仅存内存
// REQ-F-020, 2.2.9: 任务历史保留7天（内存），惰性清理
// DEC-001: 惰性清理 — 每次记录新任务时顺带删除超期记录
type TaskHistory struct {
	runs          map[string]*RunResult // key=runID
	retentionDays int                   // REQ-F-020: 默认7天
	maxLogSize    int64                 // REQ-F-024: 扩展运行日志上限10MB硬编码
	mu            sync.RWMutex
}

// NewTaskHistory 创建任务历史存储
// REQ-F-020: retentionDays 默认7天
func NewTaskHistory(retentionDays int) *TaskHistory {
	if retentionDays <= 0 {
		retentionDays = DefaultTaskRetentionDays // REQ-F-020, 2.2.9: 任务历史保留7天
	}
	return &TaskHistory{
		runs:          make(map[string]*RunResult),
		retentionDays: retentionDays,
		maxLogSize:    MaxExtensionLogSize, // REQ-F-024, 2.2.16: 扩展运行日志上限10MB硬编码
	}
}

// Add 添加任务结果到历史，并触发惰性清理
// REQ-F-020: 记录任务历史
// DEC-001: 惰性清理 — 每次记录新任务时顺带删除超期记录
func (h *TaskHistory) Add(result *RunResult) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.runs[result.RunID] = result

	// DEC-001: 惰性清理
	h.lazyCleanupLocked()
}

// Get 根据 runID 获取任务结果
// REQ-F-020: 查询任务历史
func (h *TaskHistory) Get(runID string) *RunResult {
	h.mu.RLock()
	defer h.mu.RUnlock()
	r, ok := h.runs[runID]
	if !ok {
		return nil
	}
	// B-03-001 修复：返回拷贝而非指针，避免 UpdateProgress 并发写 Progress/ResultMsg 字段造成竞态
	cp := *r
	return &cp
}

// List 根据过滤条件列出任务结果
// REQ-F-020: 列出任务历史
func (h *TaskHistory) List(filter RunFilter) []*RunResult {
	h.mu.RLock()
	defer h.mu.RUnlock()

	results := make([]*RunResult, 0)
	for _, r := range h.runs {
		if !h.matchFilter(r, filter) {
			continue
		}
		// B-03-001 修复：返回拷贝而非指针，避免 UpdateProgress 并发写 Progress/ResultMsg 字段造成竞态
		cp := *r
		results = append(results, &cp)
	}

	// 按时间降序排列（最新的在前）
	sort.Slice(results, func(i, j int) bool {
		return results[i].StartedAt.After(results[j].StartedAt)
	})

	// 应用 Limit
	if filter.Limit > 0 && len(results) > filter.Limit {
		results = results[:filter.Limit]
	}

	return results
}

// Remove 删除指定任务记录
// REQ-F-020: 删除任务历史
func (h *TaskHistory) Remove(runID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.runs, runID)
}

// ClearByFilter 删除匹配过滤条件的任务记录（仅终态记录），返回删除数量
func (h *TaskHistory) ClearByFilter(filter RunFilter) int {
	h.mu.Lock()
	defer h.mu.Unlock()

	count := 0
	for id, r := range h.runs {
		if !h.matchFilter(r, filter) {
			continue
		}
		// 跳过运行中的任务
		if r.State == TaskRunning || r.State == TaskPending {
			continue
		}
		delete(h.runs, id)
		count++
	}
	return count
}

// UpdateProgress 实时更新任务的进度和结果消息（不替换整个记录）
// 用于异步执行场景：stdout goroutine 解析 ::progress:: 后实时更新
func (h *TaskHistory) UpdateProgress(runID string, progress int, resultMsg string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r, ok := h.runs[runID]; ok {
		r.Progress = progress
		if resultMsg != "" {
			r.ResultMsg = resultMsg
		}
	}
}

// TotalCount 返回任务历史总数
// REQ-F-020: 统计任务历史数量
func (h *TaskHistory) TotalCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.runs)
}

// CountByState 返回指定状态的任务数量
// REQ-F-020: 按状态统计任务数量
func (h *TaskHistory) CountByState(state TaskState) int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	count := 0
	for _, r := range h.runs {
		if r.State == state {
			count++
		}
	}
	return count
}

// lazyCleanupLocked 惰性清理：删除超过 retentionDays 的记录
// DEC-001: 每次记录新任务时顺带删除超期记录
// 必须在持有写锁时调用
func (h *TaskHistory) lazyCleanupLocked() {
	cutoff := time.Now().AddDate(0, 0, -h.retentionDays)
	for id, r := range h.runs {
		if r.StartedAt.Before(cutoff) {
			delete(h.runs, id)
		}
	}
}

// matchFilter 判断任务结果是否匹配过滤条件
// REQ-F-020: 任务历史过滤查询
func (h *TaskHistory) matchFilter(r *RunResult, filter RunFilter) bool {
	if filter.ExtensionName != "" && r.ExtensionName != filter.ExtensionName {
		return false
	}
	if filter.ServiceName != "" && r.ServiceName != filter.ServiceName {
		return false
	}
	if filter.State != "" && r.State != filter.State {
		return false
	}
	if filter.TriggerType != "" && r.TriggerType != filter.TriggerType {
		return false
	}
	if !filter.Since.IsZero() && r.StartedAt.Before(filter.Since) {
		return false
	}
	if !filter.Until.IsZero() && r.StartedAt.After(filter.Until) {
		return false
	}
	return true
}
