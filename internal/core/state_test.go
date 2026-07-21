package core

import (
	"sync"
	"testing"
)

// reachState 创建一个状态机并将其推进到目标状态
func reachState(t *testing.T, target ServiceState) *StateMachine {
	t.Helper()
	sm := NewStateMachine()
	t.Cleanup(sm.Close)

	switch target {
	case StatePending:
		// 初始状态，无需转移
	case StateStarting:
		sm.Transition(EventDependsReady)
	case StateUp:
		sm.Transition(EventDependsReady)
		sm.Transition(EventProcessStarted)
	case StateReady:
		sm.Transition(EventDependsReady)
		sm.Transition(EventProcessStarted)
		sm.Transition(EventReadinessPassed)
	case StateStopping:
		sm.Transition(EventDependsReady)
		sm.Transition(EventProcessStarted)
		sm.Transition(EventStopRequested)
	case StateDown:
		sm.Transition(EventDependsReady)
		sm.Transition(EventProcessStarted)
		sm.Transition(EventStopRequested)
		sm.Transition(EventProcessExited)
	case StateFailed:
		sm.Transition(EventDependsReady)
		sm.Transition(EventProcessStarted)
		sm.Transition(EventMaxRetries)
	default:
		t.Fatalf("unknown state: %s", target)
	}

	if sm.Current() != target {
		t.Fatalf("reachState: want %s, got %s", target, sm.Current())
	}
	return sm
}

func TestStateMachine_InitialState(t *testing.T) {
	sm := NewStateMachine()
	defer sm.Close()

	if sm.Current() != StatePending {
		t.Errorf("initial state = %s, want pending", sm.Current())
	}
}

// 规则1: pending → starting：所有depends_on服务进入ready后
func TestStateMachine_Rule1_PendingToStarting(t *testing.T) {
	sm := reachState(t, StatePending)

	newState, ok := sm.Transition(EventDependsReady)
	if !ok || newState != StateStarting {
		t.Errorf("pending + depends_ready = (%s, %v), want (starting, true)", newState, ok)
	}
}

// 规则2: starting → up：进程启动后立即进入
func TestStateMachine_Rule2_StartingToUp(t *testing.T) {
	sm := reachState(t, StateStarting)

	newState, ok := sm.Transition(EventProcessStarted)
	if !ok || newState != StateUp {
		t.Errorf("starting + process_started = (%s, %v), want (up, true)", newState, ok)
	}
}

// 规则3: up → ready：readiness检查通过
func TestStateMachine_Rule3_UpToReady(t *testing.T) {
	sm := reachState(t, StateUp)

	newState, ok := sm.Transition(EventReadinessPassed)
	if !ok || newState != StateReady {
		t.Errorf("up + readiness_passed = (%s, %v), want (ready, true)", newState, ok)
	}
}

// 规则4: up → stopping：用户停止
func TestStateMachine_Rule4_UpToStopping(t *testing.T) {
	sm := reachState(t, StateUp)

	newState, ok := sm.Transition(EventStopRequested)
	if !ok || newState != StateStopping {
		t.Errorf("up + stop_requested = (%s, %v), want (stopping, true)", newState, ok)
	}
}

// 规则4: ready → stopping：用户停止
func TestStateMachine_Rule4_ReadyToStopping(t *testing.T) {
	sm := reachState(t, StateReady)

	newState, ok := sm.Transition(EventStopRequested)
	if !ok || newState != StateStopping {
		t.Errorf("ready + stop_requested = (%s, %v), want (stopping, true)", newState, ok)
	}
}

// 规则5: up → starting：进程死亡且restart.policy允许
func TestStateMachine_Rule5_UpToStarting(t *testing.T) {
	sm := reachState(t, StateUp)

	newState, ok := sm.Transition(EventRestartAllowed)
	if !ok || newState != StateStarting {
		t.Errorf("up + restart_allowed = (%s, %v), want (starting, true)", newState, ok)
	}
}

// 规则5: ready → starting：进程死亡且restart.policy允许
func TestStateMachine_Rule5_ReadyToStarting(t *testing.T) {
	sm := reachState(t, StateReady)

	newState, ok := sm.Transition(EventRestartAllowed)
	if !ok || newState != StateStarting {
		t.Errorf("ready + restart_allowed = (%s, %v), want (starting, true)", newState, ok)
	}
}

// 规则6: up → failed：进程死亡且达到max_retries
func TestStateMachine_Rule6_UpToFailed_MaxRetries(t *testing.T) {
	sm := reachState(t, StateUp)

	newState, ok := sm.Transition(EventMaxRetries)
	if !ok || newState != StateFailed {
		t.Errorf("up + max_retries = (%s, %v), want (failed, true)", newState, ok)
	}
}

// 规则6: ready → failed：进程死亡且达到max_retries
func TestStateMachine_Rule6_ReadyToFailed_MaxRetries(t *testing.T) {
	sm := reachState(t, StateReady)

	newState, ok := sm.Transition(EventMaxRetries)
	if !ok || newState != StateFailed {
		t.Errorf("ready + max_retries = (%s, %v), want (failed, true)", newState, ok)
	}
}

// 规则6: up → failed：readiness检查超时
func TestStateMachine_Rule6_UpToFailed_ReadinessTimeout(t *testing.T) {
	sm := reachState(t, StateUp)

	newState, ok := sm.Transition(EventReadinessTimeout)
	if !ok || newState != StateFailed {
		t.Errorf("up + readiness_timeout = (%s, %v), want (failed, true)", newState, ok)
	}
}

// 规则6: ready → failed：readiness检查超时
func TestStateMachine_Rule6_ReadyToFailed_ReadinessTimeout(t *testing.T) {
	sm := reachState(t, StateReady)

	newState, ok := sm.Transition(EventReadinessTimeout)
	if !ok || newState != StateFailed {
		t.Errorf("ready + readiness_timeout = (%s, %v), want (failed, true)", newState, ok)
	}
}

// 规则7: starting → starting：starting阶段进程退出且restart.policy允许
func TestStateMachine_Rule7_StartingToStarting(t *testing.T) {
	sm := reachState(t, StateStarting)

	newState, ok := sm.Transition(EventRestartAllowed)
	if !ok || newState != StateStarting {
		t.Errorf("starting + restart_allowed = (%s, %v), want (starting, true)", newState, ok)
	}
}

// 规则8: starting → failed：starting阶段进程退出且restart.policy不允许
func TestStateMachine_Rule8_StartingToFailed_MaxRetries(t *testing.T) {
	sm := reachState(t, StateStarting)

	newState, ok := sm.Transition(EventMaxRetries)
	if !ok || newState != StateFailed {
		t.Errorf("starting + max_retries = (%s, %v), want (failed, true)", newState, ok)
	}
}

// 规则8: starting → failed：readiness检查超时
func TestStateMachine_Rule8_StartingToFailed_ReadinessTimeout(t *testing.T) {
	sm := reachState(t, StateStarting)

	newState, ok := sm.Transition(EventReadinessTimeout)
	if !ok || newState != StateFailed {
		t.Errorf("starting + readiness_timeout = (%s, %v), want (failed, true)", newState, ok)
	}
}

// 规则9: stopping → down：进程退出
func TestStateMachine_Rule9_StoppingToDown(t *testing.T) {
	sm := reachState(t, StateStopping)

	newState, ok := sm.Transition(EventProcessExited)
	if !ok || newState != StateDown {
		t.Errorf("stopping + process_exited = (%s, %v), want (down, true)", newState, ok)
	}
}

// 规则10: down → starting：用户手动启动
func TestStateMachine_Rule10_DownToStarting(t *testing.T) {
	sm := reachState(t, StateDown)

	newState, ok := sm.Transition(EventManualStart)
	if !ok || newState != StateStarting {
		t.Errorf("down + manual_start = (%s, %v), want (starting, true)", newState, ok)
	}
}

// 规则10: failed → starting：用户手动启动
func TestStateMachine_Rule10_FailedToStarting(t *testing.T) {
	sm := reachState(t, StateFailed)

	newState, ok := sm.Transition(EventManualStart)
	if !ok || newState != StateStarting {
		t.Errorf("failed + manual_start = (%s, %v), want (starting, true)", newState, ok)
	}
}

// 测试关键非法转移
func TestStateMachine_IllegalTransitions_FromPending(t *testing.T) {
	illegalEvents := []StateEvent{
		EventProcessStarted, EventReadinessPassed, EventStopRequested,
		EventRestartAllowed, EventMaxRetries, EventReadinessTimeout,
		EventProcessExited, EventManualStart,
	}
	for _, event := range illegalEvents {
		sm := reachState(t, StatePending)
		newState, ok := sm.Transition(event)
		if ok {
			t.Errorf("pending + %s should be illegal, got (%s, true)", event, newState)
		}
		if newState != StatePending {
			t.Errorf("pending + %s: state changed to %s, should remain pending", event, newState)
		}
	}
}

func TestStateMachine_IllegalTransitions_FromDown(t *testing.T) {
	illegalEvents := []StateEvent{
		EventDependsReady, EventProcessStarted, EventReadinessPassed,
		EventStopRequested, EventRestartAllowed, EventMaxRetries,
		EventReadinessTimeout, EventProcessExited,
	}
	for _, event := range illegalEvents {
		sm := reachState(t, StateDown)
		newState, ok := sm.Transition(event)
		if ok {
			t.Errorf("down + %s should be illegal, got (%s, true)", event, newState)
		}
		if newState != StateDown {
			t.Errorf("down + %s: state changed to %s, should remain down", event, newState)
		}
	}
}

func TestStateMachine_IllegalTransitions_FromFailed(t *testing.T) {
	illegalEvents := []StateEvent{
		EventDependsReady, EventProcessStarted, EventReadinessPassed,
		EventStopRequested, EventRestartAllowed, EventMaxRetries,
		EventReadinessTimeout, EventProcessExited,
	}
	for _, event := range illegalEvents {
		sm := reachState(t, StateFailed)
		newState, ok := sm.Transition(event)
		if ok {
			t.Errorf("failed + %s should be illegal, got (%s, true)", event, newState)
		}
		if newState != StateFailed {
			t.Errorf("failed + %s: state changed to %s, should remain failed", event, newState)
		}
	}
}

func TestStateMachine_IllegalTransitions_FromReady(t *testing.T) {
	illegalEvents := []StateEvent{
		EventDependsReady, EventProcessStarted, EventProcessExited,
		EventReadinessPassed,
	}
	for _, event := range illegalEvents {
		sm := reachState(t, StateReady)
		newState, ok := sm.Transition(event)
		if ok {
			t.Errorf("ready + %s should be illegal, got (%s, true)", event, newState)
		}
		if newState != StateReady {
			t.Errorf("ready + %s: state changed to %s, should remain ready", event, newState)
		}
	}
}

func TestStateMachine_IllegalTransitions_FromStopping(t *testing.T) {
	illegalEvents := []StateEvent{
		EventDependsReady, EventProcessStarted, EventReadinessPassed,
		EventStopRequested, EventRestartAllowed, EventMaxRetries,
		EventReadinessTimeout, EventManualStart,
	}
	for _, event := range illegalEvents {
		sm := reachState(t, StateStopping)
		newState, ok := sm.Transition(event)
		if ok {
			t.Errorf("stopping + %s should be illegal, got (%s, true)", event, newState)
		}
		if newState != StateStopping {
			t.Errorf("stopping + %s: state changed to %s, should remain stopping", event, newState)
		}
	}
}

// TestStateMachine_IllegalTransitions_FromStarting 验证 starting 状态的非法转移。
// A-01-002: 审计发现缺少 starting 非法转移测试，本测试补齐。
//
// starting 状态的合法转移（validTransitions 表）：
//   - EventProcessStarted → up（规则2）
//   - EventStopRequested → stopping（规则4，starting 阶段允许停止）
//   - EventRestartAllowed → starting（规则7，退避后重新 starting，自转移）
//   - EventMaxRetries → failed（规则8，达到最大重试）
//   - EventReadinessTimeout → failed（规则8，readiness 超时）
//
// 以下事件从 starting 触发均为非法：
//   - EventDependsReady：依赖就绪只在 pending 合法，starting 阶段依赖已就绪
//   - EventReadinessPassed：readiness 检查只在 up 合法，starting→up→ready 是唯一路径
//   - EventProcessExited：进程退出只在 stopping 合法，starting 阶段进程尚未启动或未进入停止流程
//   - EventManualStart：手动启动只在 down/failed 合法，starting 阶段服务已在启动中
//   - EventBackoffAbort：退避中断只在 stopping 合法（规则9），starting 自身的退避由 EventRestartAllowed/EventMaxRetries 处理
func TestStateMachine_IllegalTransitions_FromStarting(t *testing.T) {
	illegalEvents := []StateEvent{
		EventDependsReady,
		EventReadinessPassed,
		EventProcessExited,
		EventManualStart,
		EventBackoffAbort,
	}
	for _, event := range illegalEvents {
		sm := reachState(t, StateStarting)
		newState, ok := sm.Transition(event)
		if ok {
			t.Errorf("starting + %s should be illegal, got (%s, true)", event, newState)
		}
		if newState != StateStarting {
			t.Errorf("starting + %s: state changed to %s, should remain starting", event, newState)
		}
	}
}

// TestStateMachine_FromStarting_CannotSkipToReady 验证 starting 不能跳过 up 直接进入 ready。
// 规格 §2.1.1：starting→up→ready 是唯一路径，readiness 检查只在 up 状态有意义。
// A-01-002: starting→up 必须经过 readiness 检查（即不能直接 starting→ready）。
func TestStateMachine_FromStarting_CannotSkipToReady(t *testing.T) {
	sm := reachState(t, StateStarting)

	// starting + readiness_passed 非法（必须先 starting→up 再 up→ready）
	newState, ok := sm.Transition(EventReadinessPassed)
	if ok {
		t.Errorf("starting + readiness_passed should be illegal (must go through up), got (%s, true)", newState)
	}
	if newState != StateStarting {
		t.Errorf("state should remain starting, got %s", newState)
	}

	// 正确路径：starting → up → ready
	newState, ok = sm.Transition(EventProcessStarted)
	if !ok || newState != StateUp {
		t.Fatalf("starting + process_started = (%s, %v), want (up, true)", newState, ok)
	}
	newState, ok = sm.Transition(EventReadinessPassed)
	if !ok || newState != StateReady {
		t.Errorf("up + readiness_passed = (%s, %v), want (ready, true)", newState, ok)
	}
}

// TestStateMachine_FromStarting_FailedRequiresProcessExit 验证 starting→failed 的语义。
// 规格 §2.1.1 规则8：starting→failed 发生在"starting 阶段进程退出且 restart.policy 不允许"
// 或"readiness 检查超时"。即 failed 不是直接跳转，而是进程死亡后由重启策略判定。
// A-01-002: starting→failed requires process exit（通过 max_retries 事件体现）。
func TestStateMachine_FromStarting_FailedRequiresProcessExit(t *testing.T) {
	// 路径1：starting + max_retries → failed（进程反复退出达到上限）
	sm1 := reachState(t, StateStarting)
	newState, ok := sm1.Transition(EventMaxRetries)
	if !ok || newState != StateFailed {
		t.Errorf("starting + max_retries = (%s, %v), want (failed, true)", newState, ok)
	}

	// 路径2：starting + readiness_timeout → failed（readiness 检查超时）
	sm2 := reachState(t, StateStarting)
	newState, ok = sm2.Transition(EventReadinessTimeout)
	if !ok || newState != StateFailed {
		t.Errorf("starting + readiness_timeout = (%s, %v), want (failed, true)", newState, ok)
	}

	// 反证：starting + process_exited 非法（不能直接通过进程退出跳到 failed/down），
	// 必须由调用者判定 restart.policy 后映射为 EventRestartAllowed 或 EventMaxRetries。
	sm3 := reachState(t, StateStarting)
	newState, ok = sm3.Transition(EventProcessExited)
	if ok {
		t.Errorf("starting + process_exited should be illegal (must be mapped to restart_allowed/max_retries), got (%s, true)", newState)
	}
	if newState != StateStarting {
		t.Errorf("state should remain starting, got %s", newState)
	}
}

// TestStateMachine_FromStarting_LegalSelfTransition 验证 starting→starting 自转移是合法的。
// 规格 §2.1.1 规则7：starting 阶段进程退出且 restart.policy 允许时，经退避等待后重新 starting。
// 注：此自转移通过 EventRestartAllowed 触发，并非任意事件都能触发自转移。
func TestStateMachine_FromStarting_LegalSelfTransition(t *testing.T) {
	sm := reachState(t, StateStarting)

	// starting + restart_allowed → starting（合法自转移，规则7）
	newState, ok := sm.Transition(EventRestartAllowed)
	if !ok || newState != StateStarting {
		t.Errorf("starting + restart_allowed = (%s, %v), want (starting, true)", newState, ok)
	}
	if sm.Current() != StateStarting {
		t.Errorf("state should remain starting after self-transition, got %s", sm.Current())
	}

	// 自转移后仍可正常进入 up
	newState, ok = sm.Transition(EventProcessStarted)
	if !ok || newState != StateUp {
		t.Errorf("starting + process_started after self-transition = (%s, %v), want (up, true)", newState, ok)
	}
}

// down → up 是非法的（只能 down → starting → up）
func TestStateMachine_IllegalTransition_DownToUp(t *testing.T) {
	sm := reachState(t, StateDown)
	newState, ok := sm.Transition(EventProcessStarted)
	if ok {
		t.Errorf("down + process_started should be illegal, got (%s, true)", newState)
	}
}

// ready → pending 是非法的
func TestStateMachine_IllegalTransition_ReadyToPending(t *testing.T) {
	sm := reachState(t, StateReady)
	newState, ok := sm.Transition(EventDependsReady)
	if ok {
		t.Errorf("ready + depends_ready should be illegal, got (%s, true)", newState)
	}
}

// 测试完整生命周期：pending → starting → up → ready → stopping → down → starting → up
func TestStateMachine_FullLifecycle(t *testing.T) {
	sm := NewStateMachine()
	defer sm.Close()

	// pending → starting
	newState, ok := sm.Transition(EventDependsReady)
	if !ok || newState != StateStarting {
		t.Fatalf("step 1: want (starting, true), got (%s, %v)", newState, ok)
	}

	// starting → up
	newState, ok = sm.Transition(EventProcessStarted)
	if !ok || newState != StateUp {
		t.Fatalf("step 2: want (up, true), got (%s, %v)", newState, ok)
	}

	// up → ready
	newState, ok = sm.Transition(EventReadinessPassed)
	if !ok || newState != StateReady {
		t.Fatalf("step 3: want (ready, true), got (%s, %v)", newState, ok)
	}

	// ready → stopping
	newState, ok = sm.Transition(EventStopRequested)
	if !ok || newState != StateStopping {
		t.Fatalf("step 4: want (stopping, true), got (%s, %v)", newState, ok)
	}

	// stopping → down
	newState, ok = sm.Transition(EventProcessExited)
	if !ok || newState != StateDown {
		t.Fatalf("step 5: want (down, true), got (%s, %v)", newState, ok)
	}

	// down → starting（手动重启）
	newState, ok = sm.Transition(EventManualStart)
	if !ok || newState != StateStarting {
		t.Fatalf("step 6: want (starting, true), got (%s, %v)", newState, ok)
	}

	// starting → up
	newState, ok = sm.Transition(EventProcessStarted)
	if !ok || newState != StateUp {
		t.Fatalf("step 7: want (up, true), got (%s, %v)", newState, ok)
	}

	// up → stopping（无readiness的终态停止）
	newState, ok = sm.Transition(EventStopRequested)
	if !ok || newState != StateStopping {
		t.Fatalf("step 8: want (stopping, true), got (%s, %v)", newState, ok)
	}
}

// 测试重启场景：up → starting（进程死亡，允许重启）
func TestStateMachine_RestartFromUp(t *testing.T) {
	sm := reachState(t, StateUp)

	newState, ok := sm.Transition(EventRestartAllowed)
	if !ok || newState != StateStarting {
		t.Errorf("up + restart_allowed = (%s, %v), want (starting, true)", newState, ok)
	}

	// 重新 starting → up
	newState, ok = sm.Transition(EventProcessStarted)
	if !ok || newState != StateUp {
		t.Errorf("starting + process_started = (%s, %v), want (up, true)", newState, ok)
	}
}

// 测试失败场景：up → failed（达到最大重试）
func TestStateMachine_FailureFromUp(t *testing.T) {
	sm := reachState(t, StateUp)

	newState, ok := sm.Transition(EventMaxRetries)
	if !ok || newState != StateFailed {
		t.Errorf("up + max_retries = (%s, %v), want (failed, true)", newState, ok)
	}

	// failed 只能通过 manual_start 恢复
	_, ok = sm.Transition(EventDependsReady)
	if ok {
		t.Errorf("failed + depends_ready should be illegal")
	}

	newState, ok = sm.Transition(EventManualStart)
	if !ok || newState != StateStarting {
		t.Errorf("failed + manual_start = (%s, %v), want (starting, true)", newState, ok)
	}
}

// 测试Subscribe通知
func TestStateMachine_Subscribe(t *testing.T) {
	sm := NewStateMachine()
	defer sm.Close()

	ch := sm.Subscribe()

	// 执行一系列转移
	sm.Transition(EventDependsReady)
	sm.Transition(EventProcessStarted)

	// 验证收到通知
	tr1 := <-ch
	if tr1.From != StatePending || tr1.To != StateStarting || tr1.Event != EventDependsReady {
		t.Errorf("notification 1: want {pending → starting, depends_ready}, got %+v", tr1)
	}

	tr2 := <-ch
	if tr2.From != StateStarting || tr2.To != StateUp || tr2.Event != EventProcessStarted {
		t.Errorf("notification 2: want {starting → up, process_started}, got %+v", tr2)
	}
}

// 测试Subscribe不收到非法转移的通知
func TestStateMachine_Subscribe_IgnoresIllegalTransition(t *testing.T) {
	sm := NewStateMachine()
	defer sm.Close()

	ch := sm.Subscribe()

	// 尝试非法转移（pending + process_started）
	newState, ok := sm.Transition(EventProcessStarted)
	if ok {
		t.Errorf("pending + process_started should be illegal")
	}
	if newState != StatePending {
		t.Errorf("state should remain pending, got %s", newState)
	}

	// 不应该收到任何通知
	select {
	case tr := <-ch:
		t.Errorf("should not receive notification for illegal transition, got %+v", tr)
	default:
		// 期望：无通知
	}
}

// 测试多次Subscribe
func TestStateMachine_MultipleSubscribers(t *testing.T) {
	sm := NewStateMachine()
	defer sm.Close()

	ch1 := sm.Subscribe()
	ch2 := sm.Subscribe()

	sm.Transition(EventDependsReady)

	// 两个订阅者都应收到
	tr1 := <-ch1
	tr2 := <-ch2

	if tr1.From != StatePending || tr1.To != StateStarting {
		t.Errorf("subscriber 1: want {pending → starting}, got %+v", tr1)
	}
	if tr2.From != StatePending || tr2.To != StateStarting {
		t.Errorf("subscriber 2: want {pending → starting}, got %+v", tr2)
	}
}

// 测试非法转移不改变状态
func TestStateMachine_IllegalTransition_NoStateChange(t *testing.T) {
	sm := reachState(t, StateReady)

	newState, ok := sm.Transition(EventDependsReady)
	if ok {
		t.Errorf("ready + depends_ready should be illegal")
	}
	if sm.Current() != StateReady {
		t.Errorf("state should remain ready after illegal transition, got %s", sm.Current())
	}
	if newState != StateReady {
		t.Errorf("returned state should be ready, got %s", newState)
	}
}

// 测试规则7的starting→starting自转移
func TestStateMachine_StartingSelfTransition(t *testing.T) {
	sm := reachState(t, StateStarting)

	// starting → starting（restart_allowed）
	newState, ok := sm.Transition(EventRestartAllowed)
	if !ok || newState != StateStarting {
		t.Errorf("starting + restart_allowed = (%s, %v), want (starting, true)", newState, ok)
	}

	// 状态仍然是starting，可以继续正常的starting → up
	newState, ok = sm.Transition(EventProcessStarted)
	if !ok || newState != StateUp {
		t.Errorf("starting + process_started after self-transition = (%s, %v), want (up, true)", newState, ok)
	}
}

// 测试Close后不再处理转移
func TestStateMachine_Close(t *testing.T) {
	sm := NewStateMachine()
	sm.Close()

	// Close后Transition应该安全返回false
	newState, ok := sm.Transition(EventDependsReady)
	if ok {
		t.Errorf("transition after close should return false")
	}
	_ = newState // 状态未定义，只需不panic即可
}

// TestStateMachine_Concurrent 验证状态机在并发转移下无竞态、无panic、状态始终合法。
// L-03-001: 状态机采用 channel 驱动的 actor 模式串行化转移（见 state_machine.go run 循环），
// 此处补充专门并发测试，在 race detector 下验证安全性。
func TestStateMachine_Concurrent(t *testing.T) {
	sm := NewStateMachine()
	defer sm.Close()

	// 事件池：混合合法与非法转移，验证非法事件不会破坏状态机
	events := []StateEvent{
		EventDependsReady,
		EventProcessStarted,
		EventReadinessPassed,
		EventStopRequested,
		EventProcessExited,
		EventManualStart,
		EventMaxRetries,
		EventReadinessTimeout,
		EventRestartAllowed,
	}

	validStates := map[ServiceState]bool{
		StatePending: true, StateStarting: true, StateUp: true,
		StateReady: true, StateStopping: true, StateDown: true, StateFailed: true,
	}

	var wg sync.WaitGroup
	const writers = 60
	const readers = 20
	const subscribers = 10

	// 并发写入：每个 goroutine 触发多次转移
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				ev := events[(n+j)%len(events)]
				sm.Transition(ev)
			}
		}(i)
	}

	// 并发读取：校验状态始终是7种合法状态之一
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if s := sm.Current(); !validStates[s] {
					t.Errorf("invalid state during concurrent access: %s", s)
				}
			}
		}()
	}

	// 并发订阅：订阅后非阻塞消费部分通知
	for i := 0; i < subscribers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := sm.Subscribe()
			for j := 0; j < 5; j++ {
				select {
				case <-ch:
				default:
				}
			}
		}()
	}

	wg.Wait()

	// 最终状态必须是7种合法状态之一
	if final := sm.Current(); !validStates[final] {
		t.Fatalf("final state invalid: %s", final)
	}
}

// TestStateMachine_Concurrent_LegalTransitions 验证并发合法转移序列下状态机最终到达预期终态。
// 所有 goroutine 推进同一确定性序列 pending→starting→up→ready→stopping→down，
// 由于转移表对已处于终态的重复事件返回非法（不改变状态），最终所有 goroutine 完成后应停留在 down。
func TestStateMachine_Concurrent_LegalTransitions(t *testing.T) {
	sm := NewStateMachine()
	defer sm.Close()

	sequence := []StateEvent{
		EventDependsReady,    // pending → starting
		EventProcessStarted,  // starting → up
		EventReadinessPassed, // up → ready
		EventStopRequested,   // ready → stopping
		EventProcessExited,   // stopping → down
	}

	var wg sync.WaitGroup
	const goroutines = 30
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, ev := range sequence {
				sm.Transition(ev)
			}
		}()
	}
	wg.Wait()

	// 所有 goroutine 推进同一序列；首次完整序列后到达 down，
	// 后续 goroutine 的事件在 down 态多为非法（仅 manual_start 合法，不在序列中），故最终为 down。
	if final := sm.Current(); final != StateDown {
		t.Fatalf("expected final state down after concurrent legal sequence, got %s", final)
	}
}
