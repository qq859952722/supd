package extension

import (
	"context"
	"sync"

	"github.com/supdorg/supd/internal/watch"
)

// taskRecorder 公共的 taskMgr 注入与记录逻辑
// I-03-002 修复：消除 ServiceLifecycleTrigger 和 SupdLifecycleTrigger 中
// SetTaskManager + recordResults 方法的 26 行字符级重复代码
type taskRecorder struct {
	taskMgr *TaskManager
	mu      sync.Mutex
}

// SetTaskManager 注入 TaskManager，用于记录 lifecycle 触发的执行结果
// 与 CronScheduler.SetTaskManager 保持一致的模式（加锁保护并发读写）
func (r *taskRecorder) SetTaskManager(tm *TaskManager) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.taskMgr = tm
}

// recordResults 将执行结果保存到 TaskManager，使前端能看到 lifecycle 触发的执行历史
// 加锁读取 taskMgr 字段，防止与 SetTaskManager 写入竞态
func (r *taskRecorder) recordResults(results []*RunResult) {
	r.mu.Lock()
	taskMgr := r.taskMgr
	r.mu.Unlock()
	if taskMgr == nil {
		return
	}
	for _, r := range results {
		if r != nil {
			taskMgr.RecordRun(r)
		}
	}
}

// discoveryHolder 公共的 DiscoveryResult 注入与读取逻辑
// I-03-001 修复：消除 ServiceLifecycleTrigger 和 SupdLifecycleTrigger 中
// SetDiscovery + getDiscovery 方法的 15 行字符级重复代码
type discoveryHolder struct {
	discovery *watch.DiscoveryResult
	mu        sync.RWMutex
}

// SetDiscovery 更新 DiscoveryResult 引用
// 在 Bootstrap 完成后调用，将最终的 DiscoveryResult 注入触发器
// B-03-001/B-03-002 修复：加写锁保护 discovery，防止与 handler 读取竞态
func (h *discoveryHolder) SetDiscovery(discovery *watch.DiscoveryResult) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.discovery = discovery
}

// getDiscovery 加读锁读取 discovery，防止与 SetDiscovery 写入竞态
func (h *discoveryHolder) getDiscovery() *watch.DiscoveryResult {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.discovery
}

// ServiceLifecycleTrigger 服务生命周期触发器
// REQ-D-004, 2.2.3: service_lifecycle 触发器，4个 phase
//   - pre_start: 服务启动前触发
//   - post_ready: 服务进入 ready 状态后触发
//   - on_failure: 服务失败后触发
//   - pre_stop: 服务停止前触发
//
// 扩展失败不阻止服务启停（REQ-D-004, 2.2.11）
type ServiceLifecycleTrigger struct {
	dispatcher *Dispatcher
	discoveryHolder // I-03-001: 内嵌公共 discovery 注入与读取逻辑
	taskRecorder   // I-03-002: 内嵌公共 taskMgr 注入与记录逻辑
}

// NewServiceLifecycleTrigger 创建服务生命周期触发器
// REQ-D-004: 初始化触发器，绑定 Dispatcher 和当前 DiscoveryResult
func NewServiceLifecycleTrigger(dispatcher *Dispatcher, discovery *watch.DiscoveryResult) *ServiceLifecycleTrigger {
	return &ServiceLifecycleTrigger{
		dispatcher:      dispatcher,
		discoveryHolder: discoveryHolder{discovery: discovery},
	}
}

// OnPreStart 触发 service_lifecycle:pre_start 扩展
// REQ-D-004, 2.1.4: 服务启动前触发，扩展失败不阻止服务启动
// REQ-D-004, 2.2.5: pre_start 时 SUPD_SERVICE_PID 为空（进程尚未启动）
func (t *ServiceLifecycleTrigger) OnPreStart(ctx context.Context, serviceName string) []*RunResult {
	req := DispatchRequest{
		EventType:   "service_lifecycle",
		Phase:       "pre_start",
		ServiceName: serviceName,
		// REQ-D-004, 2.2.5: pre_start 时 ServicePID 为 0（服务进程尚未启动）
		ServicePID:    0,
		Discovery:     t.getDiscovery(),
		TriggerUser:   "service_lifecycle",
	}
	results := t.dispatcher.Dispatch(ctx, req)
	t.recordResults(results)
	return results
}

// OnPostReady 触发 service_lifecycle:post_ready 扩展
// REQ-D-004, 2.1.6: 服务进入 ready 状态后触发，扩展失败不阻止服务
// REQ-D-004, 2.2.5: post_ready 时 SUPD_SERVICE_PID 为服务进程的实际 PID
func (t *ServiceLifecycleTrigger) OnPostReady(ctx context.Context, serviceName string, servicePID int) []*RunResult {
	req := DispatchRequest{
		EventType:   "service_lifecycle",
		Phase:       "post_ready",
		ServiceName: serviceName,
		ServicePID:  servicePID,
		Discovery:   t.getDiscovery(),
		TriggerUser: "service_lifecycle",
	}
	results := t.dispatcher.Dispatch(ctx, req)
	t.recordResults(results)
	return results
}

// OnPreStop 触发 service_lifecycle:pre_stop 扩展
// REQ-D-004, 2.1.4: 服务停止前触发，扩展失败不阻止服务停止
// REQ-D-004, 2.2.5: pre_stop 时 SUPD_SERVICE_PID 为服务进程的实际 PID
func (t *ServiceLifecycleTrigger) OnPreStop(ctx context.Context, serviceName string, servicePID int) []*RunResult {
	req := DispatchRequest{
		EventType:   "service_lifecycle",
		Phase:       "pre_stop",
		ServiceName: serviceName,
		ServicePID:  servicePID,
		Discovery:   t.getDiscovery(),
		TriggerUser: "service_lifecycle",
	}
	results := t.dispatcher.Dispatch(ctx, req)
	t.recordResults(results)
	return results
}

// OnFailure 触发 service_lifecycle:on_failure 扩展
// REQ-D-004, 2.2.11: 服务失败后触发，扩展失败不阻止服务
// REQ-D-004, 2.2.5: on_failure 时注入 SUPD_SERVICE_EXIT_CODE/SUPD_SERVICE_SIGNAL/SUPD_SERVICE_RESTART_COUNT
// REQ-D-004, 2.2.5: on_failure 时 SUPD_SERVICE_PID 为进程退出前的 PID
// 手动停止不算 failure，不触发 on_failure（REQ-D-004, 2.1.4）
func (t *ServiceLifecycleTrigger) OnFailure(ctx context.Context, serviceName string, exitCode, signal, restartCount, servicePID int) []*RunResult {
	req := DispatchRequest{
		EventType:       "service_lifecycle",
		Phase:           "on_failure",
		ServiceName:     serviceName,
		ServicePID:      servicePID,
		ServiceExitCode: exitCode,
		ServiceSignal:   signal,
		RestartCount:    restartCount,
		Discovery:       t.getDiscovery(),
		TriggerUser:     "service_lifecycle",
	}
	results := t.dispatcher.Dispatch(ctx, req)
	t.recordResults(results)
	return results
}
