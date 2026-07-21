package core

import "sync/atomic"

// StateEvent 状态转移事件类型
type StateEvent string

const (
	EventDependsReady     StateEvent = "depends_ready"      // 依赖就绪
	EventProcessStarted   StateEvent = "process_started"    // 进程启动
	EventReadinessPassed  StateEvent = "readiness_passed"   // readiness检查通过
	EventStopRequested    StateEvent = "stop_requested"     // 用户停止
	EventProcessDied      StateEvent = "process_died"       // 进程死亡（由调用者判断后映射为RestartAllowed或MaxRetries）
	EventRestartAllowed   StateEvent = "restart_allowed"    // restart.policy允许重启
	EventMaxRetries       StateEvent = "max_retries"        // 达到最大重试次数
	EventReadinessTimeout StateEvent = "readiness_timeout"  // readiness检查超时
	EventProcessExited    StateEvent = "process_exited"     // 进程正常退出（停止后）
	EventManualStart      StateEvent = "manual_start"       // 用户手动启动
	EventBackoffAbort     StateEvent = "backoff_abort"      // 退避等待被停止中断
)

// StateTransition 记录一次状态转移
type StateTransition struct {
	From  ServiceState
	To    ServiceState
	Event StateEvent
}

// transitionKey 转移表键
type transitionKey struct {
	from  ServiceState
	event StateEvent
}

// validTransitions 合法转移表：key=(fromState, event), value=toState
// REQ-F-004: 10条转移规则
var validTransitions = map[transitionKey]ServiceState{
	// 规则1: pending → starting：所有depends_on服务进入ready后
	{StatePending, EventDependsReady}: StateStarting,

	// 规则2: starting → up：进程启动后立即进入
	{StateStarting, EventProcessStarted}: StateUp,

	// 规则3: up → ready：readiness检查通过
	{StateUp, EventReadinessPassed}: StateReady,

	// 规则4: up/ready/starting → stopping：用户停止或重启
	// starting 状态包含退避等待期间，用户应能停止正在重试的服务
	{StateUp, EventStopRequested}:       StateStopping,
	{StateReady, EventStopRequested}:    StateStopping,
	{StateStarting, EventStopRequested}: StateStopping,

	// 规则5: up/ready → starting：进程死亡且restart.policy允许，经退避等待后重新starting
	{StateUp, EventRestartAllowed}:    StateStarting,
	{StateReady, EventRestartAllowed}: StateStarting,

	// 规则6: up/ready → failed：进程死亡且达到max_retries，或readiness检查超时
	{StateUp, EventMaxRetries}:         StateFailed,
	{StateReady, EventMaxRetries}:      StateFailed,
	{StateUp, EventReadinessTimeout}:   StateFailed,
	{StateReady, EventReadinessTimeout}: StateFailed,

	// 规则7: starting → starting：starting阶段进程退出且restart.policy允许，经退避等待后重新starting
	{StateStarting, EventRestartAllowed}: StateStarting,

	// 规则8: starting → failed：starting阶段进程退出且restart.policy不允许，或readiness检查超时
	{StateStarting, EventMaxRetries}:      StateFailed,
	{StateStarting, EventReadinessTimeout}: StateFailed,

	// 规则9: stopping → down：进程退出 或 退避等待中被停止
	{StateStopping, EventProcessExited}: StateDown,
	{StateStopping, EventBackoffAbort}:  StateDown, // 退避等待期间停止，直接 down

	// 规则10: down/failed → starting：用户手动启动
	{StateDown, EventManualStart}:   StateStarting,
	{StateFailed, EventManualStart}: StateStarting,
}

type requestKind int

const (
	reqTransition requestKind = iota
	reqSubscribe
)

type smRequest struct {
	kind   requestKind
	event  StateEvent
	result chan transitionResp
	subCh  chan StateTransition
}

type transitionResp struct {
	newState ServiceState
	ok       bool
}

// StateMachine 服务状态机
// REQ-F-004: 7种状态+10条转移规则，channel驱动
// REQ-C-003: goroutine间通过channel通信，禁止共享状态+mutex
type StateMachine struct {
	stateVal  atomic.Value   // 当前状态，atomic访问
	reqCh     chan smRequest // 请求channel
	done      chan struct{}  // 关闭信号
	name      string         // 服务名，用于事件发布
	publisher EventPublisher // 事件发布器，可为nil
}

// NewStateMachine 创建状态机，初始状态为pending
func NewStateMachine() *StateMachine {
	sm := &StateMachine{
		reqCh: make(chan smRequest, 16),
		done:  make(chan struct{}),
	}
	sm.stateVal.Store(StatePending)
	go sm.run()
	return sm
}

// SetName 设置服务名，用于事件发布
func (sm *StateMachine) SetName(name string) {
	sm.name = name
}

// SetPublisher 设置事件发布器
// REQ-2.9.7: 状态转移时发布service_state事件
func (sm *StateMachine) SetPublisher(publisher EventPublisher) {
	sm.publisher = publisher
}

// Current 返回当前状态
func (sm *StateMachine) Current() ServiceState {
	return sm.stateVal.Load().(ServiceState)
}

// ResetTo 重置状态到指定值（管理操作，绕过转移规则）
// 用于 clear-failed 等管理操作，仅在 failed/down 等终态使用
func (sm *StateMachine) ResetTo(state ServiceState) {
	old := sm.stateVal.Load().(ServiceState)
	sm.stateVal.Store(state)
	if sm.publisher != nil {
		sm.publisher.Publish("service_state", map[string]any{
			"service":   sm.name,
			"old_state": string(old),
			"new_state": string(state),
			"event":     "admin_reset",
		})
	}
}

// Transition 尝试状态转移，返回新状态和是否成功
// REQ-F-004: 严格按照10条转移规则判定合法性
func (sm *StateMachine) Transition(event StateEvent) (ServiceState, bool) {
	result := make(chan transitionResp, 1)
	select {
	case sm.reqCh <- smRequest{kind: reqTransition, event: event, result: result}:
		select {
		case r := <-result:
			return r.newState, r.ok
		case <-sm.done:
			return sm.Current(), false
		}
	case <-sm.done:
		return sm.Current(), false
	}
}

// Subscribe 订阅状态变更通知
func (sm *StateMachine) Subscribe() <-chan StateTransition {
	ch := make(chan StateTransition, 64)
	select {
	case sm.reqCh <- smRequest{kind: reqSubscribe, subCh: ch}:
		return ch
	case <-sm.done:
		close(ch)
		return ch
	}
}

// Close 关闭状态机，停止后台goroutine
func (sm *StateMachine) Close() {
	close(sm.done)
}

// run 事件处理循环，所有状态变更在此goroutine中串行执行，保证原子性
func (sm *StateMachine) run() {
	var subscribers []chan StateTransition
	for {
		select {
		case <-sm.done:
			for _, ch := range subscribers {
				close(ch)
			}
			return
		case req := <-sm.reqCh:
			switch req.kind {
			case reqSubscribe:
				subscribers = append(subscribers, req.subCh)
			case reqTransition:
				current := sm.stateVal.Load().(ServiceState)
				key := transitionKey{from: current, event: req.event}
				newState, ok := validTransitions[key]
				if ok {
					sm.stateVal.Store(newState)
					t := StateTransition{From: current, To: newState, Event: req.event}
					for _, ch := range subscribers {
						select {
						case ch <- t:
						default:
							// 订阅者消费过慢则丢弃通知
						}
					}
					// REQ-2.9.7: 状态转移成功时发布service_state事件
					if sm.publisher != nil {
						sm.publisher.Publish("service_state", map[string]any{
							"service":   sm.name,
							"old_state": string(current),
							"new_state": string(newState),
							"event":     string(req.event),
						})
						// P-03-001 修复：补充 service_ready/service_failed 事件
						if newState == StateReady {
							sm.publisher.Publish("service_ready", map[string]any{
								"service":   sm.name,
								"old_state": string(current),
							})
						} else if newState == StateFailed {
							sm.publisher.Publish("service_failed", map[string]any{
								"service":   sm.name,
								"old_state": string(current),
							})
						}
					}
					req.result <- transitionResp{newState: newState, ok: true}
				} else {
					req.result <- transitionResp{newState: current, ok: false}
				}
			}
		}
	}
}
