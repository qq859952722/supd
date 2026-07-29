package core

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestDependencyCoordinatorStartsEligibleDependentOnce(t *testing.T) {
	graph := NewDependencyGraph()
	graph.AddService("db", nil)
	graph.AddService("api", []string{"db"})

	var eligible atomic.Bool
	var starts atomic.Int32
	started := make(chan struct{}, 1)
	coordinator := NewDependencyCoordinator(graph, func(name string) bool {
		return name == "api" && eligible.Load()
	}, func(_ context.Context, name string) error {
		starts.Add(1)
		started <- struct{}{}
		time.Sleep(20 * time.Millisecond)
		return nil
	})
	coordinator.Enable()

	coordinator.OnServiceDependable(context.Background(), "db")
	select {
	case <-started:
		t.Fatal("ineligible dependent was started")
	case <-time.After(20 * time.Millisecond):
	}

	eligible.Store(true)
	coordinator.OnServiceDependable(context.Background(), "db")
	coordinator.OnServiceDependable(context.Background(), "db")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("eligible dependent was not started")
	}
	time.Sleep(30 * time.Millisecond)
	if got := starts.Load(); got != 1 {
		t.Fatalf("start count = %d, want 1", got)
	}
}

func TestDependencyCoordinatorDisabledDuringBootstrap(t *testing.T) {
	graph := NewDependencyGraph()
	graph.AddService("db", nil)
	graph.AddService("api", []string{"db"})
	var starts atomic.Int32
	coordinator := NewDependencyCoordinator(graph, func(string) bool { return true }, func(context.Context, string) error {
		starts.Add(1)
		return nil
	})
	coordinator.OnServiceDependable(context.Background(), "db")
	time.Sleep(20 * time.Millisecond)
	if got := starts.Load(); got != 0 {
		t.Fatalf("disabled coordinator start count = %d, want 0", got)
	}
}
