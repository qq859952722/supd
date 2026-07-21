package api

import (
	"sync"
	"time"

	"github.com/supdorg/supd/internal/extension"
	"github.com/supdorg/supd/internal/watch"
)

// --- CronProvider 适配器 ---

type CoreCronProvider struct {
	CronScheduler *extension.CronScheduler
	Discovery     *watch.DiscoveryResult
	TaskMgr       *extension.TaskManager
	mu            sync.RWMutex // B-05-006 修复：保护 Discovery 字段的并发访问
}

// SetDiscovery 热重载时更新 Discovery 引用
// N-04-001 修复：providers 持有 Discovery 指针值拷贝，reload 后需要显式更新
func (p *CoreCronProvider) SetDiscovery(d *watch.DiscoveryResult) {
	if p == nil || d == nil {
		return
	}
	// B-05-006 修复：添加写锁保护 Discovery 字段
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Discovery = d
}

func (p *CoreCronProvider) ListCronEntries() []CronEntryInfo {
	// B-05-006 修复：添加读锁保护 Discovery 字段
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.CronScheduler == nil || p.Discovery == nil {
		return nil
	}

	var entries []CronEntryInfo

	// 遍历全局扩展的 on_schedule 触发器
	for name, extEntry := range p.Discovery.GlobalExts {
		if extEntry.Meta == nil || extEntry.Meta.Enabled == nil || !*extEntry.Meta.Enabled {
			continue
		}
		for _, schedule := range extEntry.Meta.Triggers.OnSchedule {
			entry := CronEntryInfo{
				ExtensionName: name,
				ActionID:      schedule.Action,
				Schedule:      schedule.Cron,
			}
			// D-05-002 修复：从 CronScheduler 获取下次执行时间
			if next, ok := p.CronScheduler.GetNextRun(name, schedule.Action); ok && !next.IsZero() {
				entry.NextRun = next.Format(time.RFC3339)
			}
			entries = append(entries, entry)
		}
	}

	// 遍历服务级扩展
	for _, svcEntry := range p.Discovery.Services {
		for extName, extEntry := range svcEntry.Extensions {
			if extEntry.Meta == nil || extEntry.Meta.Enabled == nil || !*extEntry.Meta.Enabled {
				continue
			}
			for _, schedule := range extEntry.Meta.Triggers.OnSchedule {
				entry := CronEntryInfo{
					ExtensionName: extName,
					ActionID:      schedule.Action,
					Schedule:      schedule.Cron,
					Service:       svcEntry.Name,
				}
				// D-05-002 修复：从 CronScheduler 获取下次执行时间
				if next, ok := p.CronScheduler.GetNextRun(extName, schedule.Action); ok && !next.IsZero() {
					entry.NextRun = next.Format(time.RFC3339)
				}
				entries = append(entries, entry)
			}
		}
	}

	return entries
}

func (p *CoreCronProvider) ListCronHistory(filter extension.RunFilter) []*extension.RunResult {
	// REQ-D-004: 仅查询 on_schedule 触发的任务历史
	if p.TaskMgr == nil {
		return []*extension.RunResult{}
	}
	filter.TriggerType = "on_schedule"
	runs := p.TaskMgr.ListRuns(filter)
	if runs == nil {
		return []*extension.RunResult{}
	}
	return runs
}
