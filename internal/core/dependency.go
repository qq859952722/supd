package core

// DependencyGraph 依赖图
// REQ-F-005: 拓扑排序+循环检测+自引用+自动重算
type DependencyGraph struct {
	// services: service name -> depends_on list
	services map[string][]string
}

// NewDependencyGraph 创建空依赖图
func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{
		services: make(map[string][]string),
	}
}

// AddService 添加服务及其依赖
func (g *DependencyGraph) AddService(name string, dependsOn []string) {
	deps := make([]string, len(dependsOn))
	copy(deps, dependsOn)
	g.services[name] = deps
}

// RemoveService 移除服务
func (g *DependencyGraph) RemoveService(name string) {
	delete(g.services, name)
}

// TopologicalSort 拓扑排序，返回分层结果
// 返回：[][]string（每层的服务列表），[]string（循环路径，如果无循环则为空）
// REQ-F-005: 启动时按依赖图拓扑排序，按层级并行启动
func (g *DependencyGraph) TopologicalSort() (layers [][]string, cycle []string) {
	// 先检测循环，有循环则返回
	if c := g.DetectCycle(); len(c) > 0 {
		return nil, c
	}

	// 构建反向邻接表 dependents（key 是被依赖的服务，value 是依赖它的服务列表）
	// 同时计算入度：只计算图内服务间的依赖。复杂度 O(V+E)
	dependents := make(map[string][]string)
	inDegree := make(map[string]int)
	for name := range g.services {
		inDegree[name] = 0
	}
	for name, deps := range g.services {
		for _, dep := range deps {
			if _, exists := g.services[dep]; exists {
				// name依赖dep，所以name的入度+1（name需要dep先启动）
				inDegree[name]++
				// 反向边：dep 被 name 依赖
				dependents[dep] = append(dependents[dep], name)
			}
		}
	}

	// Kahn 算法分层 BFS：入度为 0 的服务为第一层
	var current []string
	for name, deg := range inDegree {
		if deg == 0 {
			current = append(current, name)
		}
	}

	for len(current) > 0 {
		layers = append(layers, current)
		var next []string
		for _, name := range current {
			// 直接通过反向邻接表找到依赖 name 的服务，减少它们的入度
			for _, svc := range dependents[name] {
				inDegree[svc]--
				if inDegree[svc] == 0 {
					next = append(next, svc)
				}
			}
		}
		current = next
	}

	return layers, nil
}

// DetectCycle 检测循环依赖
// 返回循环路径（如 [A, B, C, A]），无循环返回空
// REQ-F-005: 循环依赖检测：发现循环拒绝启动相关服务
func (g *DependencyGraph) DetectCycle() []string {
	// DFS三色标记法：0=white(未访问), 1=gray(访问中), 2=black(已完成)
	color := make(map[string]int)
	parent := make(map[string]string)

	for name := range g.services {
		if color[name] == 0 {
			if cycle := g.dfsCycle(name, color, parent); len(cycle) > 0 {
				return cycle
			}
		}
	}
	return nil
}

// dfsCycle 深度优先搜索检测环
func (g *DependencyGraph) dfsCycle(node string, color map[string]int, parent map[string]string) []string {
	color[node] = 1 // gray: 正在访问

	deps := g.services[node]
	for _, dep := range deps {
		// 只考虑图中存在的服务
		if _, exists := g.services[dep]; !exists {
			continue
		}
		if color[dep] == 1 {
			// 发现环：从dep到node回溯构建环路径
			return g.buildCyclePath(node, dep, parent)
		}
		if color[dep] == 0 {
			parent[dep] = node
			if cycle := g.dfsCycle(dep, color, parent); len(cycle) > 0 {
				return cycle
			}
		}
	}

	color[node] = 2 // black: 访问完成
	return nil
}

// buildCyclePath 从回边构建环路径
func (g *DependencyGraph) buildCyclePath(from, to string, parent map[string]string) []string {
	var path []string
	// 从from回溯到to
	current := from
	path = append(path, to)
	for current != to {
		path = append(path, current)
		current = parent[current]
	}
	path = append(path, to)

	// 反转路径使其顺序正确：to -> ... -> from -> to
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}

	return path
}

// DetectSelfReference 检测自引用
// 返回自引用的服务名列表
// REQ-F-005: 自引用检测：服务A的depends_on包含A自己，视为循环依赖
func (g *DependencyGraph) DetectSelfReference() []string {
	var result []string
	for name, deps := range g.services {
		for _, dep := range deps {
			if dep == name {
				result = append(result, name)
				break
			}
		}
	}
	return result
}

// GetDependents 获取依赖指定服务的所有服务（反向依赖）
// 用于停止时按反序停止
// REQ-F-005: 停止时按依赖反序停止（被依赖的先停）
func (g *DependencyGraph) GetDependents(name string) []string {
	var result []string
	for svc, deps := range g.services {
		for _, dep := range deps {
			if dep == name {
				result = append(result, svc)
				break
			}
		}
	}
	return result
}

// OnDependencyReady 依赖状态变化时的回调
// 当一个服务变为ready时，检查是否有其他服务因此可以启动
// 返回：可以启动的服务列表（其所有依赖都已ready）
// REQ-F-005: 依赖状态变化自动重算
func (g *DependencyGraph) OnDependencyReady(readyService string, isReady func(string) bool) []string {
	var result []string
	for svc, deps := range g.services {
		if svc == readyService {
			continue
		}
		// 检查svc的所有依赖是否都已ready
		allReady := true
		for _, dep := range deps {
			if !isReady(dep) {
				allReady = false
				break
			}
		}
		if allReady && len(deps) > 0 {
			// 只有当readyService是svc的依赖之一时，svc才是本次重算的结果
			isDep := false
			for _, dep := range deps {
				if dep == readyService {
					isDep = true
					break
				}
			}
			if isDep {
				result = append(result, svc)
			}
		}
	}
	return result
}

// MissingDependencies 检查缺失的依赖服务
// 返回：service -> []missing_dep
// REQ-F-005: 依赖服务不存在：当前服务停留pending状态
func (g *DependencyGraph) MissingDependencies(knownServices map[string]bool) map[string][]string {
	result := make(map[string][]string)
	for svc, deps := range g.services {
		var missing []string
		for _, dep := range deps {
			if !knownServices[dep] {
				missing = append(missing, dep)
			}
		}
		if len(missing) > 0 {
			result[svc] = missing
		}
	}
	return result
}

// ReverseLayers 按依赖反序返回分层结果（停止顺序）
// 每层内的服务可以并行停止，层间需要串行等待
// REQ-F-032: 同层服务并行停止，同层全部停止后才停止下一层
func (g *DependencyGraph) ReverseLayers() [][]string {
	layers, cycle := g.TopologicalSort()
	if len(cycle) > 0 {
		// 有循环时，返回所有服务作为一层
		var result []string
		for name := range g.services {
			result = append(result, name)
		}
		if len(result) > 0 {
			return [][]string{result}
		}
		return nil
	}

	// 反转层顺序：停止顺序与启动顺序相反
	// 启动顺序：[[A], [B], [C]]（A无依赖先启动，B依赖A后启动，C依赖B最后启动）
	// 停止顺序：[[C], [B], [A]]（C先停，B后停，A最后停）
	reversed := make([][]string, len(layers))
	for i, layer := range layers {
		reversed[len(layers)-1-i] = layer
	}
	return reversed
}

// ReverseOrder 按依赖反序返回服务列表（停止顺序）
// 被依赖的先停
// REQ-F-005: 停止时按依赖反序停止
func (g *DependencyGraph) ReverseOrder() []string {
	layers, cycle := g.TopologicalSort()
	if len(cycle) > 0 {
		// 有循环时，返回所有服务名（无法排序）
		var result []string
		for name := range g.services {
			result = append(result, name)
		}
		return result
	}

	// 反转层的顺序：最后一层（无依赖的服务）先停，第一层（被依赖最多的）后停
	// 但实际上"被依赖的先停"意味着：如果有 A→B（B依赖A），停止时A先停
	// 所以停止顺序应该和启动顺序相反
	// 启动顺序：A先启动（无依赖），B后启动（依赖A）
	// 停止顺序：B先停止（依赖者先停），A后停止（被依赖者后停）
	// 这就是拓扑排序的反序
	var result []string
	for i := len(layers) - 1; i >= 0; i-- {
		result = append(result, layers[i]...)
	}
	return result
}
