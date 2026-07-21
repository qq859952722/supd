package core

import (
	"fmt"
	"sort"
	"testing"
)

// helper: 对一维切片排序
func sortStrings(s []string) []string {
	sorted := make([]string, len(s))
	copy(sorted, s)
	sort.Strings(sorted)
	return sorted
}

// 空图
func TestDependencyGraph_Empty(t *testing.T) {
	g := NewDependencyGraph()

	layers, cycle := g.TopologicalSort()
	if len(cycle) != 0 {
		t.Errorf("empty graph should have no cycle, got %v", cycle)
	}
	if len(layers) != 0 {
		t.Errorf("empty graph should have 0 layers, got %d", len(layers))
	}

	if len(g.DetectCycle()) != 0 {
		t.Error("empty graph should have no cycle")
	}
	if len(g.DetectSelfReference()) != 0 {
		t.Error("empty graph should have no self-reference")
	}
	if len(g.GetDependents("X")) != 0 {
		t.Error("empty graph should have no dependents")
	}
	if len(g.ReverseOrder()) != 0 {
		t.Error("empty graph should have empty reverse order")
	}
}

// 单服务无依赖
func TestDependencyGraph_SingleService_NoDeps(t *testing.T) {
	g := NewDependencyGraph()
	g.AddService("A", nil)

	layers, cycle := g.TopologicalSort()
	if len(cycle) != 0 {
		t.Errorf("single service should have no cycle, got %v", cycle)
	}
	if len(layers) != 1 {
		t.Fatalf("single service should have 1 layer, got %d", len(layers))
	}
	if len(layers[0]) != 1 || layers[0][0] != "A" {
		t.Errorf("layer 0 should be [A], got %v", layers[0])
	}

	if len(g.DetectCycle()) != 0 {
		t.Error("single service with no deps should have no cycle")
	}
	if len(g.DetectSelfReference()) != 0 {
		t.Error("single service with no deps should have no self-reference")
	}
	if len(g.GetDependents("A")) != 0 {
		t.Error("A has no dependents")
	}
}

// 线性依赖（A→B→C，A依赖B，B依赖C）
func TestDependencyGraph_Linear(t *testing.T) {
	g := NewDependencyGraph()
	g.AddService("A", []string{"B"})
	g.AddService("B", []string{"C"})
	g.AddService("C", nil)

	layers, cycle := g.TopologicalSort()
	if len(cycle) != 0 {
		t.Errorf("linear graph should have no cycle, got %v", cycle)
	}
	if len(layers) != 3 {
		t.Fatalf("linear graph should have 3 layers, got %d", len(layers))
	}

	// Layer 0: C (no deps)
	// Layer 1: B (depends on C)
	// Layer 2: A (depends on B)
	if layers[0][0] != "C" {
		t.Errorf("layer 0 should be [C], got %v", layers[0])
	}
	if layers[1][0] != "B" {
		t.Errorf("layer 1 should be [B], got %v", layers[1])
	}
	if layers[2][0] != "A" {
		t.Errorf("layer 2 should be [A], got %v", layers[2])
	}
}

// 同层并行（A、B都依赖C）
func TestDependencyGraph_ParallelSameLayer(t *testing.T) {
	g := NewDependencyGraph()
	g.AddService("A", []string{"C"})
	g.AddService("B", []string{"C"})
	g.AddService("C", nil)

	layers, cycle := g.TopologicalSort()
	if len(cycle) != 0 {
		t.Errorf("should have no cycle, got %v", cycle)
	}
	if len(layers) != 2 {
		t.Fatalf("should have 2 layers, got %d", len(layers))
	}

	// Layer 0: C
	if len(layers[0]) != 1 || layers[0][0] != "C" {
		t.Errorf("layer 0 should be [C], got %v", layers[0])
	}
	// Layer 1: A, B (both depend on C, same layer)
	if len(layers[1]) != 2 {
		t.Errorf("layer 1 should have 2 services, got %d", len(layers[1]))
	}
	sorted1 := sortStrings(layers[1])
	if sorted1[0] != "A" || sorted1[1] != "B" {
		t.Errorf("layer 1 should be [A, B], got %v", sorted1)
	}
}

// 循环检测（A→B→C→A）
func TestDependencyGraph_Cycle(t *testing.T) {
	g := NewDependencyGraph()
	g.AddService("A", []string{"B"})
	g.AddService("B", []string{"C"})
	g.AddService("C", []string{"A"})

	cycle := g.DetectCycle()
	if len(cycle) == 0 {
		t.Fatal("should detect cycle")
	}
	// 循环路径应该首尾相同
	if cycle[0] != cycle[len(cycle)-1] {
		t.Errorf("cycle path should start and end with same node, got %v", cycle)
	}
	// 循环路径长度至少为3（如 A, B, C, A）
	if len(cycle) < 3 {
		t.Errorf("cycle path should have at least 3 elements, got %d", len(cycle))
	}

	// TopologicalSort should return cycle
	_, sortCycle := g.TopologicalSort()
	if len(sortCycle) == 0 {
		t.Error("TopologicalSort should return cycle for cyclic graph")
	}
}

// 自引用检测（A→A）
func TestDependencyGraph_SelfReference(t *testing.T) {
	g := NewDependencyGraph()
	g.AddService("A", []string{"A"})

	selfRef := g.DetectSelfReference()
	if len(selfRef) != 1 || selfRef[0] != "A" {
		t.Errorf("should detect self-reference on A, got %v", selfRef)
	}

	// Self-reference is also a cycle
	cycle := g.DetectCycle()
	if len(cycle) == 0 {
		t.Error("self-reference should be detected as a cycle")
	}
	if cycle[0] != "A" || cycle[len(cycle)-1] != "A" {
		t.Errorf("cycle should start and end with A, got %v", cycle)
	}

	// TopologicalSort should return cycle
	_, sortCycle := g.TopologicalSort()
	if len(sortCycle) == 0 {
		t.Error("TopologicalSort should detect self-reference as cycle")
	}
}

// 多个自引用
func TestDependencyGraph_MultipleSelfReference(t *testing.T) {
	g := NewDependencyGraph()
	g.AddService("A", []string{"A"})
	g.AddService("B", []string{"B"})
	g.AddService("C", nil)

	selfRef := g.DetectSelfReference()
	sorted := sortStrings(selfRef)
	if len(sorted) != 2 || sorted[0] != "A" || sorted[1] != "B" {
		t.Errorf("should detect self-reference on A and B, got %v", sorted)
	}
}

// 拓扑排序层次正确（复杂图）
func TestDependencyGraph_ComplexLayers(t *testing.T) {
	g := NewDependencyGraph()
	// A depends on nothing
	// B depends on A
	// C depends on A
	// D depends on B and C
	g.AddService("A", nil)
	g.AddService("B", []string{"A"})
	g.AddService("C", []string{"A"})
	g.AddService("D", []string{"B", "C"})

	layers, cycle := g.TopologicalSort()
	if len(cycle) != 0 {
		t.Errorf("should have no cycle, got %v", cycle)
	}
	if len(layers) != 3 {
		t.Fatalf("should have 3 layers, got %d: %v", len(layers), layers)
	}

	// Layer 0: A
	if len(layers[0]) != 1 || layers[0][0] != "A" {
		t.Errorf("layer 0 should be [A], got %v", layers[0])
	}
	// Layer 1: B, C
	if len(layers[1]) != 2 {
		t.Errorf("layer 1 should have 2 services, got %v", layers[1])
	}
	sorted1 := sortStrings(layers[1])
	if sorted1[0] != "B" || sorted1[1] != "C" {
		t.Errorf("layer 1 should be [B, C], got %v", sorted1)
	}
	// Layer 2: D
	if len(layers[2]) != 1 || layers[2][0] != "D" {
		t.Errorf("layer 2 should be [D], got %v", layers[2])
	}
}

// 依赖反序（停止顺序）
func TestDependencyGraph_ReverseOrder(t *testing.T) {
	g := NewDependencyGraph()
	// A (no deps), B depends on A, C depends on B
	g.AddService("A", nil)
	g.AddService("B", []string{"A"})
	g.AddService("C", []string{"B"})

	order := g.ReverseOrder()
	// Stop order: C, B, A (dependents first, dependencies last)
	if len(order) != 3 {
		t.Fatalf("should have 3 services, got %d", len(order))
	}
	if order[0] != "C" {
		t.Errorf("C should stop first, got %s", order[0])
	}
	if order[1] != "B" {
		t.Errorf("B should stop second, got %s", order[1])
	}
	if order[2] != "A" {
		t.Errorf("A should stop last, got %s", order[2])
	}
}

// OnDependencyReady回调
func TestDependencyGraph_OnDependencyReady(t *testing.T) {
	g := NewDependencyGraph()
	// A depends on B and C
	// D depends on C
	g.AddService("A", []string{"B", "C"})
	g.AddService("D", []string{"C"})
	g.AddService("B", nil)
	g.AddService("C", nil)

	ready := map[string]bool{
		"B": true,
		"C": false,
	}

	// B becomes ready, but A still needs C
	result := g.OnDependencyReady("B", func(name string) bool {
		return ready[name]
	})
	if len(result) != 0 {
		t.Errorf("no service should be ready to start, got %v", result)
	}

	// C becomes ready, now A and D should both be ready
	ready["C"] = true
	result = g.OnDependencyReady("C", func(name string) bool {
		return ready[name]
	})
	sorted := sortStrings(result)
	if len(sorted) != 2 || sorted[0] != "A" || sorted[1] != "D" {
		t.Errorf("A and D should be ready to start, got %v", sorted)
	}
}

// OnDependencyReady: 服务没有依赖时不被触发
func TestDependencyGraph_OnDependencyReady_NoDeps(t *testing.T) {
	g := NewDependencyGraph()
	g.AddService("A", nil)
	g.AddService("B", []string{"A"})

	result := g.OnDependencyReady("A", func(name string) bool {
		return true
	})
	// B depends on A and A is ready, so B should be returned
	if len(result) != 1 || result[0] != "B" {
		t.Errorf("B should be ready to start, got %v", result)
	}
}

// OnDependencyReady: 依赖未全部就绪
func TestDependencyGraph_OnDependencyReady_PartialDeps(t *testing.T) {
	g := NewDependencyGraph()
	// A depends on B, C, D
	g.AddService("A", []string{"B", "C", "D"})
	g.AddService("B", nil)
	g.AddService("C", nil)
	g.AddService("D", nil)

	ready := map[string]bool{"B": true, "C": false, "D": false}

	// B becomes ready, A still needs C and D
	result := g.OnDependencyReady("B", func(name string) bool {
		return ready[name]
	})
	if len(result) != 0 {
		t.Errorf("A should not be ready (still needs C and D), got %v", result)
	}

	// D becomes ready, A still needs C
	ready["D"] = true
	result = g.OnDependencyReady("D", func(name string) bool {
		return ready[name]
	})
	if len(result) != 0 {
		t.Errorf("A should not be ready (still needs C), got %v", result)
	}

	// C becomes ready, now all deps ready
	ready["C"] = true
	result = g.OnDependencyReady("C", func(name string) bool {
		return ready[name]
	})
	if len(result) != 1 || result[0] != "A" {
		t.Errorf("A should be ready to start, got %v", result)
	}
}

// MissingDependencies
func TestDependencyGraph_MissingDependencies(t *testing.T) {
	g := NewDependencyGraph()
	g.AddService("A", []string{"B", "C"})
	g.AddService("B", []string{"D"})
	g.AddService("C", nil)

	known := map[string]bool{"A": true, "B": true, "C": true} // D is missing

	missing := g.MissingDependencies(known)
	if len(missing) != 1 {
		t.Fatalf("should have 1 service with missing deps, got %d", len(missing))
	}
	if len(missing["B"]) != 1 || missing["B"][0] != "D" {
		t.Errorf("B should have missing dep D, got %v", missing["B"])
	}

	// A's deps are all known
	if _, ok := missing["A"]; ok {
		t.Error("A should not have missing deps")
	}
}

// MissingDependencies: 多个服务有缺失依赖
func TestDependencyGraph_MissingDependencies_Multiple(t *testing.T) {
	g := NewDependencyGraph()
	g.AddService("A", []string{"X"})
	g.AddService("B", []string{"Y"})
	g.AddService("C", nil)

	known := map[string]bool{"A": true, "B": true, "C": true}

	missing := g.MissingDependencies(known)
	if len(missing) != 2 {
		t.Fatalf("should have 2 services with missing deps, got %d", len(missing))
	}
	if missing["A"][0] != "X" {
		t.Errorf("A should have missing dep X, got %v", missing["A"])
	}
	if missing["B"][0] != "Y" {
		t.Errorf("B should have missing dep Y, got %v", missing["B"])
	}
}

// 动态添加/移除服务
func TestDependencyGraph_AddRemove(t *testing.T) {
	g := NewDependencyGraph()
	g.AddService("A", []string{"B"})
	g.AddService("B", nil)

	layers, cycle := g.TopologicalSort()
	if len(cycle) != 0 || len(layers) != 2 {
		t.Fatalf("expected 2 layers no cycle, got layers=%v cycle=%v", layers, cycle)
	}

	// Remove B, A now has a missing dependency
	g.RemoveService("B")
	if _, exists := g.services["B"]; exists {
		t.Error("B should be removed")
	}

	// A's dep on B is now to a non-existent service
	layers, cycle = g.TopologicalSort()
	if len(cycle) != 0 {
		t.Errorf("should have no cycle after removing B, got %v", cycle)
	}
	if len(layers) != 1 || layers[0][0] != "A" {
		t.Errorf("should have 1 layer with A, got %v", layers)
	}

	// Check missing deps
	missing := g.MissingDependencies(map[string]bool{"A": true})
	if len(missing["A"]) != 1 || missing["A"][0] != "B" {
		t.Errorf("A should have missing dep B, got %v", missing["A"])
	}
}

// GetDependents
func TestDependencyGraph_GetDependents(t *testing.T) {
	g := NewDependencyGraph()
	g.AddService("A", []string{"C"})
	g.AddService("B", []string{"C"})
	g.AddService("C", nil)

	deps := g.GetDependents("C")
	sorted := sortStrings(deps)
	if len(sorted) != 2 || sorted[0] != "A" || sorted[1] != "B" {
		t.Errorf("dependents of C should be [A, B], got %v", sorted)
	}

	deps = g.GetDependents("A")
	if len(deps) != 0 {
		t.Errorf("A should have no dependents, got %v", deps)
	}
}

// 依赖不存在服务不影响拓扑排序（不构成循环）
func TestDependencyGraph_DepsOnNonExistent_NoCycle(t *testing.T) {
	g := NewDependencyGraph()
	g.AddService("A", []string{"NonExistent"})
	g.AddService("B", nil)

	cycle := g.DetectCycle()
	if len(cycle) != 0 {
		t.Errorf("depending on non-existent service should not cause cycle, got %v", cycle)
	}

	layers, sortCycle := g.TopologicalSort()
	if len(sortCycle) != 0 {
		t.Errorf("TopologicalSort should not report cycle, got %v", sortCycle)
	}
	// A and B are both in layer 0 (A's dep is not in graph, so it's as if A has no in-graph deps)
	if len(layers) != 1 {
		t.Fatalf("should have 1 layer, got %d", len(layers))
	}
	sorted := sortStrings(layers[0])
	if len(sorted) != 2 || sorted[0] != "A" || sorted[1] != "B" {
		t.Errorf("layer 0 should be [A, B], got %v", sorted)
	}
}

// ReverseOrder 有循环时返回所有服务
func TestDependencyGraph_ReverseOrder_WithCycle(t *testing.T) {
	g := NewDependencyGraph()
	g.AddService("A", []string{"B"})
	g.AddService("B", []string{"A"})

	order := g.ReverseOrder()
	if len(order) != 2 {
		t.Errorf("should return all 2 services when cycle exists, got %d", len(order))
	}
	sorted := sortStrings(order)
	if sorted[0] != "A" || sorted[1] != "B" {
		t.Errorf("should return [A, B], got %v", sorted)
	}
}

// 菱形依赖：A→B, A→C, B→D, C→D
func TestDependencyGraph_Diamond(t *testing.T) {
	g := NewDependencyGraph()
	g.AddService("A", []string{"B", "C"})
	g.AddService("B", []string{"D"})
	g.AddService("C", []string{"D"})
	g.AddService("D", nil)

	layers, cycle := g.TopologicalSort()
	if len(cycle) != 0 {
		t.Errorf("diamond should have no cycle, got %v", cycle)
	}
	if len(layers) != 3 {
		t.Fatalf("diamond should have 3 layers, got %d: %v", len(layers), layers)
	}

	// Layer 0: D
	if len(layers[0]) != 1 || layers[0][0] != "D" {
		t.Errorf("layer 0 should be [D], got %v", layers[0])
	}
	// Layer 1: B, C
	sorted1 := sortStrings(layers[1])
	if len(sorted1) != 2 || sorted1[0] != "B" || sorted1[1] != "C" {
		t.Errorf("layer 1 should be [B, C], got %v", sorted1)
	}
	// Layer 2: A
	if len(layers[2]) != 1 || layers[2][0] != "A" {
		t.Errorf("layer 2 should be [A], got %v", layers[2])
	}
}

// AddService覆盖已有服务
func TestDependencyGraph_AddService_Overwrite(t *testing.T) {
	g := NewDependencyGraph()
	g.AddService("A", []string{"B"})
	g.AddService("B", nil)

	g.AddService("A", []string{"C"}) // overwrite A's deps
	g.AddService("C", nil)

	deps := g.services["A"]
	if len(deps) != 1 || deps[0] != "C" {
		t.Errorf("A's deps should be [C] after overwrite, got %v", deps)
	}
}

// 空依赖列表
func TestDependencyGraph_EmptyDeps(t *testing.T) {
	g := NewDependencyGraph()
	g.AddService("A", []string{})

	layers, cycle := g.TopologicalSort()
	if len(cycle) != 0 {
		t.Errorf("should have no cycle, got %v", cycle)
	}
	if len(layers) != 1 || layers[0][0] != "A" {
		t.Errorf("should have 1 layer [A], got %v", layers)
	}
}

// OnDependencyReady: readyService本身不在图中
func TestDependencyGraph_OnDependencyReady_UnknownService(t *testing.T) {
	g := NewDependencyGraph()
	g.AddService("A", []string{"B"})
	g.AddService("B", nil)

	// Unknown X becomes ready
	result := g.OnDependencyReady("X", func(name string) bool {
		return name == "B" || name == "X"
	})
	if len(result) != 0 {
		t.Errorf("no service should be triggered by unknown X, got %v", result)
	}
}

// 循环中有自引用
func TestDependencyGraph_CycleWithSelfReference(t *testing.T) {
	g := NewDependencyGraph()
	g.AddService("A", []string{"A", "B"})
	g.AddService("B", []string{"A"})

	selfRef := g.DetectSelfReference()
	if len(selfRef) != 1 || selfRef[0] != "A" {
		t.Errorf("should detect self-reference on A, got %v", selfRef)
	}

	cycle := g.DetectCycle()
	if len(cycle) == 0 {
		t.Error("should detect cycle")
	}
}

// OnDependencyReady: 多层依赖逐级传播
func TestDependencyGraph_OnDependencyReady_Chained(t *testing.T) {
	g := NewDependencyGraph()
	// A depends on B, B depends on C
	g.AddService("A", []string{"B"})
	g.AddService("B", []string{"C"})
	g.AddService("C", nil)

	// C becomes ready → B can start
	ready := map[string]bool{"C": true, "B": false}
	result := g.OnDependencyReady("C", func(name string) bool {
		return ready[name]
	})
	if len(result) != 1 || result[0] != "B" {
		t.Errorf("B should be ready after C, got %v", result)
	}

	// B becomes ready → A can start
	ready["B"] = true
	result = g.OnDependencyReady("B", func(name string) bool {
		return ready[name]
	})
	if len(result) != 1 || result[0] != "A" {
		t.Errorf("A should be ready after B, got %v", result)
	}
}

// --- G-04 / G-05 修复：Benchmark 基准数据 ---

// benchBuildLinearGraph 构建包含 n 个服务的线性依赖图：
// svc-0000 无依赖，svc-0001 依赖 svc-0000，svc-0002 依赖 svc-0001，以此类推。
// 线性图会形成 n 层拓扑排序，是分层 BFS 的最坏情形，便于压力测试。
func benchBuildLinearGraph(n int) *DependencyGraph {
	g := NewDependencyGraph()
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("svc-%04d", i)
		if i == 0 {
			g.AddService(name, nil)
		} else {
			g.AddService(name, []string{fmt.Sprintf("svc-%04d", i-1)})
		}
	}
	return g
}

// BenchmarkTopologicalSort_SmallGraph 测量 10 个服务小图的拓扑排序性能。
// G-04 修复：建立 benchmark 基准数据
func BenchmarkTopologicalSort_SmallGraph(b *testing.B) {
	g := benchBuildLinearGraph(10)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = g.TopologicalSort()
	}
}

// BenchmarkTopologicalSort_MediumGraph 测量 50 个服务中图的拓扑排序性能。
// G-04 修复：建立 benchmark 基准数据
func BenchmarkTopologicalSort_MediumGraph(b *testing.B) {
	g := benchBuildLinearGraph(50)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = g.TopologicalSort()
	}
}

// BenchmarkTopologicalSort_LargeGraph 测量 100 个服务大图的拓扑排序性能。
// G-04 修复：建立 benchmark 基准数据
func BenchmarkTopologicalSort_LargeGraph(b *testing.B) {
	g := benchBuildLinearGraph(100)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = g.TopologicalSort()
	}
}

// BenchmarkDetectCycle_LargeGraph 测量 100 个服务大图的循环检测性能（无环情形）。
// G-04 修复：建立 benchmark 基准数据
func BenchmarkDetectCycle_LargeGraph(b *testing.B) {
	g := benchBuildLinearGraph(100)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = g.DetectCycle()
	}
}
