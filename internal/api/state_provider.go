package api

import (
	"sync"
	"time"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/core"
	"github.com/supdorg/supd/internal/watch"
)

// --- StateProvider 适配器 ---

type CoreStateProvider struct {
	mu            sync.RWMutex
	StateMachines map[string]*core.StateMachine
	ProcessMgr    *core.ProcessManager
	Discovery     *watch.DiscoveryResult
	Config        *config.Config
	StartTime     time.Time
	// D-05-001 修复：从 RestartEngine 获取实际重启次数
	RestartEngines map[string]*core.RestartEngine
}

// SetDiscovery 热重载时更新 Discovery 引用并为新发现的服务创建状态机
// N-04-001 修复：providers 持有 Discovery 指针值拷贝，reload 后需要显式更新
// 修复：目录被删除的服务需从 StateMachines 中移除（§2.4.2: 服务目录被删→卸载该服务）
func (p *CoreStateProvider) SetDiscovery(d *watch.DiscoveryResult) {
	if p == nil || d == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Discovery = d
	// 为新发现的服务创建状态机（已存在的保留运行状态）
	if d.Services != nil {
		for name := range d.Services {
			if _, ok := p.StateMachines[name]; !ok {
				sm := core.NewStateMachine()
				sm.SetName(name)
				p.StateMachines[name] = sm
			}
		}
		// 移除已删除目录的服务（不在新 Discovery 中且未运行的服务）
		for name, sm := range p.StateMachines {
			if _, exists := d.Services[name]; !exists {
				state := sm.Current()
				// 仅移除非运行状态的服务（pending/down/failed）
				// 运行中的服务保留状态机，避免丢失进程信息
				if state != core.StateStarting && state != core.StateUp &&
					state != core.StateReady && state != core.StateStopping {
					delete(p.StateMachines, name)
				}
			}
		}
	}
}

func (p *CoreStateProvider) GetServiceState(name string) (ServiceStateInfo, bool) {
	p.mu.RLock()
	sm, ok := p.StateMachines[name]
	p.mu.RUnlock()
	if !ok {
		return ServiceStateInfo{}, false
	}

	info := ServiceStateInfo{
		Name:    name,
		State:   sm.Current(),
		Enabled: true,
	}

	// 从 discovery 获取配置
	// D-05-01 修复：Discovery 为 nil 时跳过（热重载期间可能短暂为 nil）
	if p.Discovery != nil {
		if svc, ok := p.Discovery.Services[name]; ok {
			info.ConfigPath = svc.ConfigPath
			if svc.Config != nil {
				info.Config = svc.Config
				if svc.Config.Autostart != nil {
					info.Enabled = *svc.Config.Autostart
				}
			}
		}
	}

	// 从 ProcessMgr 获取 PID 和 Uptime（仅运行中状态）
	switch info.State {
	case core.StateStarting, core.StateUp, core.StateReady, core.StateStopping:
		if proc, ok := p.ProcessMgr.Get(name); ok {
			select {
			case <-proc.Done():
				// 进程已退出
			default:
				info.PID = proc.PID()
				info.Uptime = int64(time.Since(proc.StartTime()).Seconds())
			}
		}
	}

	// D-05-001 修复：从 RestartEngine 获取实际重启次数
	if p.RestartEngines != nil {
		p.mu.RLock()
		engine, ok := p.RestartEngines[name]
		p.mu.RUnlock()
		if ok && engine != nil {
			info.RestartCount = engine.Retries()
		}
	}

	return info, true
}

func (p *CoreStateProvider) ListServiceStates() map[string]ServiceStateInfo {
	p.mu.RLock()
	names := make([]string, 0, len(p.StateMachines))
	for name := range p.StateMachines {
		names = append(names, name)
	}
	p.mu.RUnlock()

	states := make(map[string]ServiceStateInfo, len(names))
	for _, name := range names {
		if info, ok := p.GetServiceState(name); ok {
			states[name] = info
		}
	}
	return states
}
