package core

import (
	"sync"
	"testing"
)

// fakePublisher 记录发布的事件，用于验证状态转移时的事件发布
// EventPublisher 接口签名为 Publish(string, any)
type fakePublisher struct {
	mu       sync.Mutex
	events   []string
	payloads []any
}

func (f *fakePublisher) Publish(event string, payload any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event)
	f.payloads = append(f.payloads, payload)
}

func (f *fakePublisher) count(event string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, e := range f.events {
		if e == event {
			n++
		}
	}
	return n
}

// TestStateMachine_ResetToPublishes 验证 ResetTo 在设置 publisher 后发布 admin_reset 事件
// 补齐 state_test.go 未覆盖的 ResetTo 发布分支（state_machine.go:147-154）
func TestStateMachine_ResetToPublishes(t *testing.T) {
	sm := NewStateMachine()
	defer sm.Close()
	pub := &fakePublisher{}
	sm.SetPublisher(pub)
	sm.SetName("svc1")

	sm.ResetTo(StateDown)
	if pub.count("service_state") != 1 {
		t.Fatalf("expected 1 service_state publish, got %d", pub.count("service_state"))
	}
	if sm.Current() != StateDown {
		t.Fatalf("expected state down after ResetTo, got %s", sm.Current())
	}
	// 校验 payload 内容
	pub.mu.Lock()
	last := pub.payloads[len(pub.payloads)-1].(map[string]any)
	pub.mu.Unlock()
	if last["event"] != "admin_reset" || last["new_state"] != string(StateDown) {
		t.Fatalf("unexpected admin_reset payload: %+v", last)
	}
}

// TestStateMachine_PublishesReadyAndFailed 验证到达 ready/failed 时发布 service_ready/service_failed
// 补齐 state_test.go 未覆盖的 run() 循环发布分支（state_machine.go:219-238）
func TestStateMachine_PublishesReadyAndFailed(t *testing.T) {
	sm := NewStateMachine()
	defer sm.Close()
	pub := &fakePublisher{}
	sm.SetPublisher(pub)
	sm.SetName("svc1")

	// pending -> starting -> up -> ready
	sm.Transition(EventDependsReady)
	sm.Transition(EventProcessStarted)
	sm.Transition(EventReadinessPassed)
	if sm.Current() != StateReady {
		t.Fatalf("expected ready, got %s", sm.Current())
	}
	if pub.count("service_ready") != 1 {
		t.Fatalf("expected 1 service_ready event, got %d", pub.count("service_ready"))
	}

	// down -> starting -> up -> failed
	sm.ResetTo(StateDown)
	sm.Transition(EventManualStart)
	sm.Transition(EventProcessStarted)
	sm.Transition(EventMaxRetries)
	if sm.Current() != StateFailed {
		t.Fatalf("expected failed, got %s", sm.Current())
	}
	if pub.count("service_failed") != 1 {
		t.Fatalf("expected 1 service_failed event, got %d", pub.count("service_failed"))
	}
}

// TestStateMachine_PublishesServiceStateOnEachTransition 验证每次合法转移都会发布 service_state
func TestStateMachine_PublishesServiceStateOnEachTransition(t *testing.T) {
	sm := NewStateMachine()
	defer sm.Close()
	pub := &fakePublisher{}
	sm.SetPublisher(pub)
	sm.SetName("svc1")

	sm.Transition(EventDependsReady)
	sm.Transition(EventProcessStarted)
	sm.Transition(EventReadinessPassed)
	sm.Transition(EventStopRequested)
	sm.Transition(EventProcessExited)

	// 5 次合法转移 → 5 次 service_state 发布
	if pub.count("service_state") != 5 {
		t.Fatalf("expected 5 service_state events, got %d", pub.count("service_state"))
	}
}
