package extension

import (
	"fmt"
	"sync"
	"testing"
)

// TestRunResultsCapacityBound 写入 501 条 → 第 1 条被淘汰、第 2~501 条存在（容量 500）。
func TestRunResultsCapacityBound(t *testing.T) {
	e := NewExecutor("/tmp", "/tmp")
	for i := 1; i <= maxRunResults+1; i++ {
		e.storeResult(&RunResult{RunID: fmt.Sprintf("run-%d", i), State: TaskSuccess})
	}

	if got := e.GetResult("run-1"); got != nil {
		t.Fatalf("run-1 should be evicted (over capacity), got %v", got)
	}
	for i := 2; i <= maxRunResults+1; i++ {
		if got := e.GetResult(fmt.Sprintf("run-%d", i)); got == nil {
			t.Fatalf("run-%d should exist, got nil", i)
		}
	}
	if n := len(e.ListResults()); n != maxRunResults {
		t.Fatalf("ListResults length = %d, want %d", n, maxRunResults)
	}
}

// TestRunResultsEvictionOrder 按插入序淘汰最旧（非随机）；ListResults 按最旧到最新返回。
func TestRunResultsEvictionOrder(t *testing.T) {
	e := NewExecutor("/tmp", "/tmp")
	for i := 1; i <= maxRunResults+3; i++ {
		e.storeResult(&RunResult{RunID: fmt.Sprintf("run-%d", i), State: TaskSuccess})
	}

	// 最旧的 3 条被淘汰。
	for i := 1; i <= 3; i++ {
		if got := e.GetResult(fmt.Sprintf("run-%d", i)); got != nil {
			t.Fatalf("run-%d should be evicted, got %v", i, got)
		}
	}

	// ListResults 按插入序从最旧到最新：第一条应为 run-4，最后一条应为 run-503。
	results := e.ListResults()
	if len(results) != maxRunResults {
		t.Fatalf("ListResults length = %d, want %d", len(results), maxRunResults)
	}
	if results[0].RunID != "run-4" {
		t.Fatalf("first ListResults = %s, want run-4", results[0].RunID)
	}
	last := results[len(results)-1]
	if want := fmt.Sprintf("run-%d", maxRunResults+3); last.RunID != want {
		t.Fatalf("last ListResults = %s, want %s", last.RunID, want)
	}
}

// TestRunResultsConcurrentWrite 并发 1000 写入，最终 ≤500 且无竞态。
func TestRunResultsConcurrentWrite(t *testing.T) {
	e := NewExecutor("/tmp", "/tmp")

	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			e.storeResult(&RunResult{RunID: fmt.Sprintf("run-%d", i), State: TaskSuccess})
		}(i)
	}
	wg.Wait()

	if n := len(e.ListResults()); n > maxRunResults {
		t.Fatalf("ListResults length = %d, want <= %d", n, maxRunResults)
	}
}
