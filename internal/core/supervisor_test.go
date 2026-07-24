package core

import (
	"context"
	"syscall"
	"testing"
	"time"
)

// T1: DecideRestart — max_retries reached → RestartActionFailed
func TestDecideRestart_MaxRetries(t *testing.T) {
	engine := NewRestartEngine(RestartAlways, 100, 1000, 2, 3, 0)
	// Increment to reach max retries
	engine.IncrementRetries()
	engine.IncrementRetries()
	engine.IncrementRetries()

	sm := NewStateMachine()
	sm.Transition(EventDependsReady)   // pending → starting
	sm.Transition(EventProcessStarted) // starting → up

	action := DecideRestart(engine, sm, 1, false, syscall.SIGTERM)
	if action != RestartActionFailed {
		t.Errorf("expected RestartActionFailed, got %v", action)
	}
}

// T2: DecideRestart — never policy → RestartActionFailed
func TestDecideRestart_NeverPolicy(t *testing.T) {
	sm := NewStateMachine()
	sm.Transition(EventDependsReady)
	sm.Transition(EventProcessStarted)

	for _, exitCode := range []int{0, 1, 127} {
		engine := NewRestartEngine(RestartNever, 100, 1000, 2, 3, 0)
		action := DecideRestart(engine, sm, exitCode, false, syscall.SIGTERM)
		if action != RestartActionFailed {
			t.Errorf("exitCode=%d: expected RestartActionFailed, got %v", exitCode, action)
		}
	}
}

// T3: DecideRestart — on-failure + exit 0 → RestartActionDown
func TestDecideRestart_OnFailure_ExitZero(t *testing.T) {
	engine := NewRestartEngine(RestartOnFailure, 100, 1000, 2, 5, 0)

	sm := NewStateMachine()
	sm.Transition(EventDependsReady)
	sm.Transition(EventProcessStarted)

	action := DecideRestart(engine, sm, 0, false, syscall.SIGTERM)
	if action != RestartActionDown {
		t.Errorf("expected RestartActionDown for on-failure + exit 0, got %v", action)
	}
}

// T4: DecideRestart — always policy + exit 1 → RestartActionWait
// P3 修复：需要 StateMachine 处于 StateUp 状态
func TestDecideRestart_AllowRestart(t *testing.T) {
	engine := NewRestartEngine(RestartAlways, 100, 1000, 2, 5, 0)

	sm := NewStateMachine()
	sm.Transition(EventDependsReady)   // pending → starting
	sm.Transition(EventProcessStarted) // starting → up

	action := DecideRestart(engine, sm, 1, false, syscall.SIGTERM)
	if action != RestartActionWait {
		t.Errorf("expected RestartActionWait, got %v", action)
	}
	if engine.Retries() != 1 {
		t.Errorf("expected retries=1 after DecideRestart, got %d", engine.Retries())
	}
}

// T5: doBackoff — context cancel → return false + state down
func TestDoBackoff_ContextCancel(t *testing.T) {
	sm := NewStateMachine()
	sm.Transition(EventDependsReady)   // pending → starting
	sm.Transition(EventRestartAllowed) // starting → starting (restart allowed)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancel

	result := doBackoff(ctx, sm, "test-service", 10*time.Second)
	if result {
		t.Error("expected doBackoff to return false on ctx cancel")
	}
	if sm.Current() != StateDown {
		t.Errorf("expected state Down after backoff abort, got %v", sm.Current())
	}
}
