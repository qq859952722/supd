package config

import (
	"bytes"
	"fmt"

	"go.yaml.in/yaml/v4"
)

// SafeYAMLOptions 安全YAML解析选项
// REQ-E-003: YAML解析防护（深度100层+别名50）
type SafeYAMLOptions struct {
	MaxDepth   int // 默认100
	MaxAliases int // 默认50
}

// MaxYAMLDocSize YAML 文档原始字节数上限（1MB）
// H-01-001: 防御超大输入；合法配置通常远小于此值
const MaxYAMLDocSize = 1 << 20 // 1MB

// maxExpandedNodes 展开后总节点数上限
// H-01-001: 防御别名指数展开（YAML bomb）：别名数可能未超限，
// 但展开后节点数爆炸。此为最终防线。
const maxExpandedNodes = 100000

// maxAliasExpansionDepth 别名展开递归深度上限
// H-01-001: 防止循环别名导致无限递归（合法别名链远小于此值）
const maxAliasExpansionDepth = 2000

// DefaultSafeYAMLOptions 默认安全选项
// REQ-E-003: 深度100层+别名50
var DefaultSafeYAMLOptions = SafeYAMLOptions{
	MaxDepth:   100,
	MaxAliases: 50,
}

// safeParse 执行 SafeUnmarshal 与 StrictUnmarshal 共用的安全校验逻辑：
// 规范化 opts、限制文档原始字节数、解析到 Node 树并校验嵌套深度/别名数/展开节点数。
// I-03-002 修复：提取原本两个函数重复的 21 行校验逻辑。
// 成功时返回解析后的 Node 树根节点（供调用方按需解码使用），失败时返回 error。
func safeParse(data []byte, opts SafeYAMLOptions) (*yaml.Node, error) {
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = DefaultSafeYAMLOptions.MaxDepth
	}
	if opts.MaxAliases <= 0 {
		opts.MaxAliases = DefaultSafeYAMLOptions.MaxAliases
	}

	// H-01-001: 限制文档原始字节数，防御超大输入
	if len(data) > MaxYAMLDocSize {
		return nil, fmt.Errorf("yaml document size %d exceeds limit %d", len(data), MaxYAMLDocSize)
	}

	// 第一步：解析到 Node 树
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, fmt.Errorf("yaml parse: %w", err)
	}

	// 第二步：校验嵌套深度
	visitedDepth := make(map[*yaml.Node]bool)
	if depth := measureDepth(&node, 0, visitedDepth); depth > opts.MaxDepth {
		return nil, fmt.Errorf("yaml nesting depth %d exceeds limit %d", depth, opts.MaxDepth)
	}

	// 第三步：校验别名数量
	visitedAlias := make(map[*yaml.Node]bool)
	if count := countAliases(&node, visitedAlias); count > opts.MaxAliases {
		return nil, fmt.Errorf("yaml alias count %d exceeds limit %d", count, opts.MaxAliases)
	}

	// 第四步：H-01-001 校验展开后总节点数（防止别名链指数展开的 bomb：
	// 此类输入别名引用数未超限，但解码时别名展开会导致内存爆炸）
	if _, err := countExpandedNodes(&node, 0, 0); err != nil {
		return nil, err
	}

	return &node, nil
}

// SafeUnmarshal 安全YAML解析，先解析到Node树校验深度和别名数，再解码到目标结构体
// REQ-E-003: YAML解析防护（深度100层+别名50）
func SafeUnmarshal(data []byte, out any, opts SafeYAMLOptions) error {
	node, err := safeParse(data, opts)
	if err != nil {
		return err
	}

	// 第五步：从 Node 树解码到目标结构体
	if err := node.Decode(out); err != nil {
		return fmt.Errorf("yaml decode: %w", err)
	}

	return nil
}

// StrictUnmarshal 严格YAML解析，在 SafeUnmarshal 基础上开启 KnownFields(true)，
// 拒绝目标结构体中未定义的未知字段。
// 适用于用户创建/更新配置的API校验场景；内部配置加载使用 SafeUnmarshal。
// REQ-E-003: YAML解析防护 + 严格模式
func StrictUnmarshal(data []byte, out any, opts SafeYAMLOptions) error {
	if _, err := safeParse(data, opts); err != nil {
		return err
	}

	// 第五步：使用 Decoder 开启严格模式解码（重新从原始字节解析以启用 KnownFields）
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(out); err != nil {
		// io.EOF 表示空输入，不是错误
		if err.Error() == "EOF" {
			return nil
		}
		return fmt.Errorf("yaml strict decode: %w", err)
	}

	return nil
}

// measureDepth 递归测量 YAML Node 树的最大嵌套深度
// REQ-E-003: 深度限制100层
// H-01-002: 使用 visited 防止循环别名导致无限递归
func measureDepth(node *yaml.Node, currentDepth int, visited map[*yaml.Node]bool) int {
	if node == nil {
		return currentDepth
	}
	if visited[node] {
		return currentDepth
	}
	visited[node] = true

	switch node.Kind {
	case yaml.DocumentNode:
		maxDepth := currentDepth
		for _, child := range node.Content {
			if d := measureDepth(child, currentDepth, visited); d > maxDepth {
				maxDepth = d
			}
		}
		return maxDepth

	case yaml.MappingNode, yaml.SequenceNode:
		maxDepth := currentDepth + 1
		for _, child := range node.Content {
			if d := measureDepth(child, currentDepth+1, visited); d > maxDepth {
				maxDepth = d
			}
		}
		return maxDepth

	case yaml.AliasNode:
		if node.Alias != nil {
			return measureDepth(node.Alias, currentDepth, visited)
		}
		return currentDepth

	default:
		return currentDepth
	}
}

// countAliases 递归统计 YAML Node 树中 AliasNode 的数量
// REQ-E-003: 别名限制50
// H-01-002: 使用 visited 防止循环别名导致无限递归
func countAliases(node *yaml.Node, visited map[*yaml.Node]bool) int {
	if node == nil {
		return 0
	}
	if visited[node] {
		return 0
	}
	visited[node] = true

	count := 0
	if node.Kind == yaml.AliasNode {
		count = 1
	}

	for _, child := range node.Content {
		count += countAliases(child, visited)
	}

	// 别名指向的节点也需要递归统计
	if node.Kind == yaml.AliasNode && node.Alias != nil {
		count += countAliases(node.Alias, visited)
	}

	return count
}

// countExpandedNodes 递归统计展开后的总节点数（别名按引用次数展开计数，不去重）。
// H-01-001: 防御 YAML bomb（别名链指数展开）。使用累加器 acc 线程化计数，
// 一旦超过 maxExpandedNodes 立即短路返回错误，避免完整展开造成的资源消耗。
// depth 在每次跟随别名时递增，达到 maxAliasExpansionDepth 即判定为循环别名并拒绝，
// 从而避免循环别名导致的无限递归（合法别名链深度远小于该上限）。
func countExpandedNodes(node *yaml.Node, depth int, acc int) (int, error) {
	if node == nil {
		return acc, nil
	}
	if depth > maxAliasExpansionDepth {
		return acc, fmt.Errorf("yaml alias expansion too deep (possible cyclic alias), depth %d exceeds limit %d", depth, maxAliasExpansionDepth)
	}

	acc++
	if acc > maxExpandedNodes {
		return acc, fmt.Errorf("yaml expanded node count %d exceeds limit %d (possible alias bomb)", acc, maxExpandedNodes)
	}

	for _, child := range node.Content {
		var err error
		acc, err = countExpandedNodes(child, depth, acc)
		if err != nil {
			return acc, err
		}
	}

	if node.Kind == yaml.AliasNode && node.Alias != nil {
		var err error
		acc, err = countExpandedNodes(node.Alias, depth+1, acc)
		if err != nil {
			return acc, err
		}
	}

	return acc, nil
}
