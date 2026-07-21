package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSearchServiceLogs_Boundary1000_AtExactly1000Lines
// L-02-001: 规格 §2.1.7 / REQ-F-010 — 日志搜索上限 1000 行。
// 边界值场景：日志文件恰好包含 1000 行全部匹配，maxLines=1000 时应返回 1000 行（恰好在上限）。
func TestSearchServiceLogs_Boundary1000_AtExactly1000Lines(t *testing.T) {
	logDir := t.TempDir()
	svcLogDir := filepath.Join(logDir, "services", "svc1")
	if err := os.MkdirAll(svcLogDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// 构造 1000 行全部匹配 "match" 关键字
	var sb strings.Builder
	for i := 0; i < 1000; i++ {
		sb.WriteString("match line\n")
	}
	if err := os.WriteFile(filepath.Join(svcLogDir, "current"), []byte(sb.String()), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	p := &FileLogProvider{LogDir: logDir}
	lines, err := p.SearchServiceLogs("svc1", "match", 1000)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(lines) != 1000 {
		t.Errorf("expected exactly 1000 lines at boundary, got %d", len(lines))
	}
}

// TestSearchServiceLogs_Boundary1000_OverLimit1001LinesTruncated
// L-02-001: 规格 §2.1.7 / REQ-F-010 — 日志搜索上限 1000 行。
// 超出场景：日志文件包含 1001 行全部匹配，maxLines=1000 时应返回 1000 行（被截断到上限）。
func TestSearchServiceLogs_Boundary1000_OverLimit1001LinesTruncated(t *testing.T) {
	logDir := t.TempDir()
	svcLogDir := filepath.Join(logDir, "services", "svc1")
	if err := os.MkdirAll(svcLogDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// 构造 1001 行全部匹配 "match" 关键字（超出 1000 上限 1 行）
	var sb strings.Builder
	for i := 0; i < 1001; i++ {
		sb.WriteString("match line\n")
	}
	if err := os.WriteFile(filepath.Join(svcLogDir, "current"), []byte(sb.String()), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	p := &FileLogProvider{LogDir: logDir}
	lines, err := p.SearchServiceLogs("svc1", "match", 1000)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	// 规格 §2.1.7 锁定上限 1000 行：即便源日志有 1001 行匹配，也应被截断到 1000 行
	if len(lines) != 1000 {
		t.Errorf("expected exactly 1000 lines (truncated from 1001), got %d", len(lines))
	}
}

// TestSearchServiceLogs_DefaultMaxLines1000
// L-02-001: 规格 §2.1.7 — 调用方传 maxLines=0 时，logging.SearchLogs 使用 DefaultMaxLines=1000。
// 验证默认值路径与上限一致：1001 行匹配在默认 maxLines 下被截断到 1000 行。
func TestSearchServiceLogs_DefaultMaxLines1000(t *testing.T) {
	logDir := t.TempDir()
	svcLogDir := filepath.Join(logDir, "services", "svc1")
	if err := os.MkdirAll(svcLogDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	var sb strings.Builder
	for i := 0; i < 1001; i++ {
		sb.WriteString("match line\n")
	}
	if err := os.WriteFile(filepath.Join(svcLogDir, "current"), []byte(sb.String()), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	p := &FileLogProvider{LogDir: logDir}
	// maxLines=0 触发 logging.SearchLogs 内部默认值 DefaultMaxLines=1000
	lines, err := p.SearchServiceLogs("svc1", "match", 0)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(lines) != 1000 {
		t.Errorf("expected default maxLines=1000 to truncate 1001 matches to 1000, got %d", len(lines))
	}
}

// TestSearchServiceLogs_Boundary1000_MixedMatchLines
// L-02-001: 规格 §2.1.7 — 混合匹配/非匹配行场景下的边界行为。
// 构造 2000 行日志，其中 1500 行匹配（超过上限），maxLines=1000 应截断到 1000 行。
func TestSearchServiceLogs_Boundary1000_MixedMatchLines(t *testing.T) {
	logDir := t.TempDir()
	svcLogDir := filepath.Join(logDir, "services", "svc1")
	if err := os.MkdirAll(svcLogDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	var sb strings.Builder
	// 500 行不匹配 + 1500 行匹配 = 2000 行
	for i := 0; i < 500; i++ {
		sb.WriteString("info line\n")
	}
	for i := 0; i < 1500; i++ {
		sb.WriteString("match line\n")
	}
	if err := os.WriteFile(filepath.Join(svcLogDir, "current"), []byte(sb.String()), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	p := &FileLogProvider{LogDir: logDir}
	lines, err := p.SearchServiceLogs("svc1", "match", 1000)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	// 1500 匹配超过 1000 上限，应截断到 1000 行
	if len(lines) != 1000 {
		t.Errorf("expected 1000 lines (truncated from 1500 matches), got %d", len(lines))
	}
	// 所有返回行均应包含匹配关键字（不应包含非匹配的 info line）
	for i, line := range lines {
		if !strings.Contains(line, "match") {
			t.Errorf("line %d should contain 'match' keyword, got: %q", i, line)
		}
	}
}
