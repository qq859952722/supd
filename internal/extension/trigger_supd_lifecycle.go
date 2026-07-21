package extension

import (
	"context"

	"github.com/supdorg/supd/internal/watch"
)

// SupdLifecycleTrigger supd 生命周期触发器
// REQ-D-004, 2.2.3: supd_lifecycle 触发器，3个 phase
//   - pre_start: supd 启动前触发
//   - post_ready: 所有 autostart=true 的服务进入终态(ready/failed)后触发
//   - pre_shutdown: supd 退出第1步触发
//
// 扩展失败不阻止 supd 启停
type SupdLifecycleTrigger struct {
	dispatcher *Dispatcher
	discoveryHolder // I-03-001: 内嵌公共 discovery 注入与读取逻辑
	taskRecorder   // I-03-002: 内嵌公共 taskMgr 注入与记录逻辑
}

// NewSupdLifecycleTrigger 创建 supd 生命周期触发器
// REQ-D-004: 初始化触发器，绑定 Dispatcher 和当前 DiscoveryResult
func NewSupdLifecycleTrigger(dispatcher *Dispatcher, discovery *watch.DiscoveryResult) *SupdLifecycleTrigger {
	return &SupdLifecycleTrigger{
		dispatcher:      dispatcher,
		discoveryHolder: discoveryHolder{discovery: discovery},
	}
}

// OnPreStart 触发 supd_lifecycle:pre_start 扩展
// REQ-D-004, 2.8.1: supd 启动第9步，触发 pre_start 扩展
func (t *SupdLifecycleTrigger) OnPreStart(ctx context.Context) []*RunResult {
	req := DispatchRequest{
		EventType:   "supd_lifecycle",
		Phase:       "pre_start",
		Discovery:   t.getDiscovery(),
		TriggerUser: "supd_lifecycle",
	}
	results := t.dispatcher.Dispatch(ctx, req)
	t.recordResults(results)
	return results
}

// OnPostReady 触发 supd_lifecycle:post_ready 扩展
// REQ-D-004, 2.8.1: supd 启动第11步，所有 autostart=true 的服务进入终态(ready/failed)后触发
// autostart=false 的服务不参与此判定
func (t *SupdLifecycleTrigger) OnPostReady(ctx context.Context) []*RunResult {
	req := DispatchRequest{
		EventType:   "supd_lifecycle",
		Phase:       "post_ready",
		Discovery:   t.getDiscovery(),
		TriggerUser: "supd_lifecycle",
	}
	results := t.dispatcher.Dispatch(ctx, req)
	t.recordResults(results)
	return results
}

// OnPreShutdown 触发 supd_lifecycle:pre_shutdown 扩展
// REQ-D-004, 2.8.1: supd 退出第1步，触发 pre_shutdown 扩展
func (t *SupdLifecycleTrigger) OnPreShutdown(ctx context.Context) []*RunResult {
	req := DispatchRequest{
		EventType:   "supd_lifecycle",
		Phase:       "pre_shutdown",
		Discovery:   t.getDiscovery(),
		TriggerUser: "supd_lifecycle",
	}
	results := t.dispatcher.Dispatch(ctx, req)
	t.recordResults(results)
	return results
}
