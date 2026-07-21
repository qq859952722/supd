package config

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

// TestYAMLSafeUnmarshalNormal 正常YAML解析
func TestYAMLSafeUnmarshalNormal(t *testing.T) {
	data := []byte("settings:\n  http_listen: \":9090\"\n  auth_mode: local_skip\n")
	var cfg Config
	opts := DefaultSafeYAMLOptions
	if err := SafeUnmarshal(data, &cfg, opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Settings.HTTPListen != ":9090" {
		t.Errorf("expected http_listen :9090, got %s", cfg.Settings.HTTPListen)
	}
}

// TestYAMLSafeUnmarshalDepthExceeded 深度超限
// REQ-E-003: 深度100层限制
func TestYAMLSafeUnmarshalDepthExceeded(t *testing.T) {
	// 构造深度为5的嵌套YAML
	yamlStr := "a:\n"
	prefix := "  "
	for i := 0; i < 4; i++ {
		yamlStr += prefix + "b:\n"
		prefix += "  "
	}
	yamlStr += prefix + "c: val\n"

	var result map[string]any
	opts := SafeYAMLOptions{MaxDepth: 3, MaxAliases: 50}
	err := SafeUnmarshal([]byte(yamlStr), &result, opts)
	if err == nil {
		t.Fatal("expected depth exceeded error, got nil")
	}
	if !strings.Contains(err.Error(), "nesting depth") {
		t.Errorf("expected nesting depth error, got: %v", err)
	}
}

// TestYAMLSafeUnmarshalAliasExceeded 别名数超限
// REQ-E-003: 别名50限制
func TestYAMLSafeUnmarshalAliasExceeded(t *testing.T) {
	// 构造包含3个别名的YAML
	yamlStr := "defaults: &defaults\n  name: test\n  value: 1\na:\n  <<: *defaults\nb:\n  <<: *defaults\nc:\n  <<: *defaults\n"

	var result map[string]any
	opts := SafeYAMLOptions{MaxDepth: 100, MaxAliases: 2}
	err := SafeUnmarshal([]byte(yamlStr), &result, opts)
	if err == nil {
		t.Fatal("expected alias exceeded error, got nil")
	}
	if !strings.Contains(err.Error(), "alias count") {
		t.Errorf("expected alias count error, got: %v", err)
	}
}

// TestYAMLSafeUnmarshalInvalidYAML 非法YAML
func TestYAMLSafeUnmarshalInvalidYAML(t *testing.T) {
	data := []byte(":\n  :\n    bad yaml [[[[")
	var result map[string]any
	opts := DefaultSafeYAMLOptions
	err := SafeUnmarshal(data, &result, opts)
	if err == nil {
		t.Fatal("expected parse error for invalid YAML, got nil")
	}
}

// TestYAMLSafeUnmarshalDefaultOpts 默认选项
func TestYAMLSafeUnmarshalDefaultOpts(t *testing.T) {
	data := []byte("settings:\n  auth_mode: none\n")
	var cfg Config
	// 传入零值选项应使用默认值
	opts := SafeYAMLOptions{}
	if err := SafeUnmarshal(data, &cfg, opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestMeasureDepth 测量深度函数
func TestMeasureDepth(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		expected int
	}{
		{"flat scalar", "key: value\n", 1},
		{"nested 2 levels", "a:\n  b: val\n", 2},
		{"nested 3 levels", "a:\n  b:\n    c: val\n", 3},
		{"sequence", "items:\n  - a\n  - b\n", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var node yaml.Node
			if err := yaml.Unmarshal([]byte(tt.yaml), &node); err != nil {
				t.Fatalf("parse error: %v", err)
			}
			depth := measureDepth(&node, 0, make(map[*yaml.Node]bool))
			if depth != tt.expected {
				t.Errorf("expected depth %d, got %d", tt.expected, depth)
			}
		})
	}
}

// TestCountAliases 别名计数函数
func TestCountAliases(t *testing.T) {
	yamlStr := "defaults: &defaults\n  name: test\na:\n  <<: *defaults\nb:\n  <<: *defaults\n"
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(yamlStr), &node); err != nil {
		t.Fatalf("parse error: %v", err)
	}
	count := countAliases(&node, make(map[*yaml.Node]bool))
	if count < 2 {
		t.Errorf("expected at least 2 aliases, got %d", count)
	}
}

// TestYAMLSafeUnmarshalEmptyInput 空输入
func TestYAMLSafeUnmarshalEmptyInput(t *testing.T) {
	var result map[string]any
	opts := DefaultSafeYAMLOptions
	err := SafeUnmarshal([]byte(""), &result, opts)
	// 空YAML不应报错
	if err != nil {
		t.Fatalf("unexpected error for empty input: %v", err)
	}
}

// --- StrictUnmarshal 测试 ---

// TestStrictUnmarshalNormal 正常YAML严格解析
func TestStrictUnmarshalNormal(t *testing.T) {
	data := []byte("settings:\n  http_listen: \":9090\"\n  auth_mode: local_skip\n")
	var cfg Config
	opts := DefaultSafeYAMLOptions
	if err := StrictUnmarshal(data, &cfg, opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Settings.HTTPListen != ":9090" {
		t.Errorf("expected http_listen :9090, got %s", cfg.Settings.HTTPListen)
	}
}

// TestStrictUnmarshalUnknownField 未知字段被拒绝
func TestStrictUnmarshalUnknownField(t *testing.T) {
	data := []byte("settings:\n  http_listen: \":9090\"\n  unknown_field: bad\n")
	var cfg Config
	opts := DefaultSafeYAMLOptions
	err := StrictUnmarshal(data, &cfg, opts)
	if err == nil {
		t.Fatal("expected error for unknown field in strict mode, got nil")
	}
	if !strings.Contains(err.Error(), "strict decode") {
		t.Errorf("expected strict decode error, got: %v", err)
	}
}

// TestStrictUnmarshalDepthExceeded 严格模式深度超限
func TestStrictUnmarshalDepthExceeded(t *testing.T) {
	yamlStr := "a:\n"
	prefix := "  "
	for i := 0; i < 4; i++ {
		yamlStr += prefix + "b:\n"
		prefix += "  "
	}
	yamlStr += prefix + "c: val\n"

	var result map[string]any
	opts := SafeYAMLOptions{MaxDepth: 3, MaxAliases: 50}
	err := StrictUnmarshal([]byte(yamlStr), &result, opts)
	if err == nil {
		t.Fatal("expected depth exceeded error, got nil")
	}
	if !strings.Contains(err.Error(), "nesting depth") {
		t.Errorf("expected nesting depth error, got: %v", err)
	}
}

// TestStrictUnmarshalAliasExceeded 严格模式别名数超限
func TestStrictUnmarshalAliasExceeded(t *testing.T) {
	yamlStr := "defaults: &defaults\n  name: test\n  value: 1\na:\n  <<: *defaults\nb:\n  <<: *defaults\nc:\n  <<: *defaults\n"

	var result map[string]any
	opts := SafeYAMLOptions{MaxDepth: 100, MaxAliases: 2}
	err := StrictUnmarshal([]byte(yamlStr), &result, opts)
	if err == nil {
		t.Fatal("expected alias exceeded error, got nil")
	}
	if !strings.Contains(err.Error(), "alias count") {
		t.Errorf("expected alias count error, got: %v", err)
	}
}

// TestStrictUnmarshalEmptyInput 严格模式空输入
func TestStrictUnmarshalEmptyInput(t *testing.T) {
	var result map[string]any
	opts := DefaultSafeYAMLOptions
	err := StrictUnmarshal([]byte(""), &result, opts)
	if err != nil {
		t.Fatalf("unexpected error for empty input: %v", err)
	}
}

// TestSafeUnmarshalAllowsUnknownFields SafeUnmarshal 允许未知字段
func TestSafeUnmarshalAllowsUnknownFields(t *testing.T) {
	data := []byte("settings:\n  http_listen: \":9090\"\n  unknown_field: ignored\n")
	var cfg Config
	opts := DefaultSafeYAMLOptions
	// SafeUnmarshal 不应因未知字段报错
	if err := SafeUnmarshal(data, &cfg, opts); err != nil {
		t.Fatalf("SafeUnmarshal should allow unknown fields, got error: %v", err)
	}
	if cfg.Settings.HTTPListen != ":9090" {
		t.Errorf("expected http_listen :9090, got %s", cfg.Settings.HTTPListen)
	}
}

// --- H-01-001 补充：YAML 安全边界测试 ---

// TestYAMLSafeUnmarshalDepthBoundary101 测试超过默认 100 限制的嵌套
// REQ-E-003: 深度100层限制，101层应被拒绝
func TestYAMLSafeUnmarshalDepthBoundary101(t *testing.T) {
	// 构造 100 层 "a:" + 1 层 "val:" = 101 个 MappingNode
	var yamlBuilder strings.Builder
	for i := 0; i < 100; i++ {
		for j := 0; j < i; j++ {
			yamlBuilder.WriteByte(' ')
		}
		yamlBuilder.WriteString("a:\n")
	}
	for j := 0; j < 100; j++ {
		yamlBuilder.WriteByte(' ')
	}
	yamlBuilder.WriteString("val: end\n")

	var result map[string]any
	opts := DefaultSafeYAMLOptions // MaxDepth=100
	err := SafeUnmarshal([]byte(yamlBuilder.String()), &result, opts)
	if err == nil {
		t.Fatal("expected depth exceeded error for 101 levels, got nil")
	}
	if !strings.Contains(err.Error(), "nesting depth") {
		t.Errorf("expected nesting depth error, got: %v", err)
	}
}

// TestYAMLSafeUnmarshalDepthBoundary100 测试 100 层嵌套刚好等于限制（应通过）
// REQ-E-003: 深度100层限制，100层应被允许
func TestYAMLSafeUnmarshalDepthBoundary100(t *testing.T) {
	// 构造 99 层 "a:" + 1 层 "val:" = 100 个 MappingNode
	var yamlBuilder strings.Builder
	for i := 0; i < 99; i++ {
		for j := 0; j < i; j++ {
			yamlBuilder.WriteByte(' ')
		}
		yamlBuilder.WriteString("a:\n")
	}
	for j := 0; j < 99; j++ {
		yamlBuilder.WriteByte(' ')
	}
	yamlBuilder.WriteString("val: end\n")

	var result map[string]any
	opts := DefaultSafeYAMLOptions // MaxDepth=100
	err := SafeUnmarshal([]byte(yamlBuilder.String()), &result, opts)
	if err != nil {
		t.Fatalf("expected 100 levels to pass, got error: %v", err)
	}
}

// TestYAMLSafeUnmarshalYAMLBomb 别名扩展攻击（YAML bomb）
// REQ-E-003: 别名50限制，大量别名应被拒绝
func TestYAMLSafeUnmarshalYAMLBomb(t *testing.T) {
	// 构造 YAML bomb：定义一个 anchor，多次引用使其扩展
	// 但别名限制为 50，超过应被拒绝
	var yamlBuilder strings.Builder
	yamlBuilder.WriteString("base: &base\n  data: \"x\"\n")
	// 生成 100 个别名引用（超过 50 限制）
	for i := 0; i < 100; i++ {
		yamlBuilder.WriteString(fmt.Sprintf("item%d: *base\n", i))
	}

	var result map[string]any
	opts := DefaultSafeYAMLOptions // MaxAliases=50
	err := SafeUnmarshal([]byte(yamlBuilder.String()), &result, opts)
	if err == nil {
		t.Fatal("expected alias exceeded error for YAML bomb, got nil")
	}
	if !strings.Contains(err.Error(), "alias count") {
		t.Errorf("expected alias count error, got: %v", err)
	}
}

// TestYAMLSafeUnmarshalPythonObjectTag 测试 !!python/object 危险标签处理
// yaml.v4 是纯 Go 实现，不会执行 Python 代码
// 验证解析后不会产生命令执行的副作用（如创建文件）
func TestYAMLSafeUnmarshalPythonObjectTag(t *testing.T) {
	// 尝试注入 Python 对象标签
	data := []byte("value: !!python/object/apply:os.system ['touch /tmp/yaml-pwned']\n")
	var result map[string]any
	opts := DefaultSafeYAMLOptions
	// SafeUnmarshal 可能报错（未知标签）或解析为通用类型，但都不应执行命令
	_ = SafeUnmarshal(data, &result, opts)

	// 验证没有产生命令执行的副作用
	if _, err := os.Stat("/tmp/yaml-pwned"); err == nil {
		t.Fatal("SECURITY: !!python/object tag executed command (file created)")
		_ = os.Remove("/tmp/yaml-pwned")
	}
}

// TestYAMLSafeUnmarshalBinaryTag 测试 !!binary 标签处理
// !!binary 是合法的 YAML 标签，应被正常处理
func TestYAMLSafeUnmarshalBinaryTag(t *testing.T) {
	data := []byte("value: !!binary aGVsbG8=\n")
	var result map[string]any
	opts := DefaultSafeYAMLOptions
	err := SafeUnmarshal(data, &result, opts)
	if err != nil {
		t.Fatalf("expected !!binary to be allowed, got error: %v", err)
	}
}

// TestYAMLSafeUnmarshalLargeDocument 大文档测试
// 验证大文档不会触发深度/别名限制（数据量大但结构浅）
func TestYAMLSafeUnmarshalLargeDocument(t *testing.T) {
	var yamlBuilder strings.Builder
	yamlBuilder.WriteString("items:\n")
	for i := 0; i < 1000; i++ {
		yamlBuilder.WriteString(fmt.Sprintf("  - name: item%d\n", i))
		yamlBuilder.WriteString(fmt.Sprintf("    value: %d\n", i))
	}

	var result map[string]any
	opts := DefaultSafeYAMLOptions
	err := SafeUnmarshal([]byte(yamlBuilder.String()), &result, opts)
	if err != nil {
		t.Fatalf("expected large flat document to parse, got error: %v", err)
	}
}
