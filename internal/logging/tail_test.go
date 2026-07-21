package logging

import (
	"os"
	"path/filepath"
	"testing"
)

// --- TailReader 测试 ---

// TestTailReader_ReadFromStart 从头读取所有行
func TestTailReader_ReadFromStart(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "current")

	content := "line1\nline2\nline3\n"
	os.WriteFile(filePath, []byte(content), 0644)

	reader := NewTailReader(filePath, 0)
	lines, err := reader.ReadNewLines()
	if err != nil {
		t.Fatalf("ReadNewLines failed: %v", err)
	}

	if len(lines) != 3 {
		t.Fatalf("len(lines) = %d, want 3", len(lines))
	}
	if lines[0] != "line1" {
		t.Errorf("lines[0] = %q, want %q", lines[0], "line1")
	}
	if lines[1] != "line2" {
		t.Errorf("lines[1] = %q, want %q", lines[1], "line2")
	}
	if lines[2] != "line3" {
		t.Errorf("lines[2] = %q, want %q", lines[2], "line3")
	}

	// 验证位置已更新
	if reader.CurrentPos() <= 0 {
		t.Errorf("CurrentPos() = %d, want > 0", reader.CurrentPos())
	}
}

// TestTailReader_ReadFromPosition 从指定位置读取新行
func TestTailReader_ReadFromPosition(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "current")

	content := "line1\nline2\nline3\n"
	os.WriteFile(filePath, []byte(content), 0644)

	// 先从头读取
	reader := NewTailReader(filePath, 0)
	lines1, err := reader.ReadNewLines()
	if err != nil {
		t.Fatalf("First ReadNewLines failed: %v", err)
	}
	if len(lines1) != 3 {
		t.Fatalf("First read: len(lines) = %d, want 3", len(lines1))
	}

	pos := reader.CurrentPos()

	// 追加新内容
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}
	f.WriteString("line4\nline5\n")
	f.Close()

	// 从上次位置读取
	lines2, err := reader.ReadNewLines()
	if err != nil {
		t.Fatalf("Second ReadNewLines failed: %v", err)
	}
	if len(lines2) != 2 {
		t.Fatalf("Second read: len(lines) = %d, want 2", len(lines2))
	}
	if lines2[0] != "line4" {
		t.Errorf("lines2[0] = %q, want %q", lines2[0], "line4")
	}
	if lines2[1] != "line5" {
		t.Errorf("lines2[1] = %q, want %q", lines2[1], "line5")
	}

	// 验证位置已更新
	if reader.CurrentPos() <= pos {
		t.Errorf("CurrentPos() = %d, want > %d", reader.CurrentPos(), pos)
	}
}

// TestTailReader_NoNewContent 无新内容时返回空
func TestTailReader_NoNewContent(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "current")

	content := "line1\nline2\n"
	os.WriteFile(filePath, []byte(content), 0644)

	// 先读取全部
	reader := NewTailReader(filePath, 0)
	lines1, err := reader.ReadNewLines()
	if err != nil {
		t.Fatalf("First ReadNewLines failed: %v", err)
	}
	if len(lines1) != 2 {
		t.Fatalf("First read: len(lines) = %d, want 2", len(lines1))
	}

	// 不追加新内容，再次读取
	lines2, err := reader.ReadNewLines()
	if err != nil {
		t.Fatalf("Second ReadNewLines failed: %v", err)
	}
	if len(lines2) != 0 {
		t.Errorf("Second read: len(lines) = %d, want 0", len(lines2))
	}
}

// TestTailReader_NonexistentFile 文件不存在
func TestTailReader_NonexistentFile(t *testing.T) {
	reader := NewTailReader("/nonexistent/path/current", 0)
	_, err := reader.ReadNewLines()
	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}
}

// TestTailReader_EmptyFile 空文件
func TestTailReader_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "current")
	os.WriteFile(filePath, []byte(""), 0644)

	reader := NewTailReader(filePath, 0)
	lines, err := reader.ReadNewLines()
	if err != nil {
		t.Fatalf("ReadNewLines failed: %v", err)
	}

	if len(lines) != 0 {
		t.Errorf("len(lines) = %d, want 0", len(lines))
	}
}

// TestTailReader_CurrentPos 返回当前位置
func TestTailReader_CurrentPos(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "current")
	os.WriteFile(filePath, []byte("hello\n"), 0644)

	reader := NewTailReader(filePath, 42)
	if reader.CurrentPos() != 42 {
		t.Errorf("CurrentPos() = %d, want 42", reader.CurrentPos())
	}
}

// --- TailServiceLogs 测试 ---

// TestTailServiceLogs_Basic 基本tail服务日志
func TestTailServiceLogs_Basic(t *testing.T) {
	dir := t.TempDir()
	svcDir := filepath.Join(dir, "myservice")
	os.MkdirAll(svcDir, 0755)

	currentPath := filepath.Join(svcDir, "current")
	content := "svc line1\nsvc line2\n"
	os.WriteFile(currentPath, []byte(content), 0644)

	lines, pos, err := TailServiceLogs("myservice", dir, 0)
	if err != nil {
		t.Fatalf("TailServiceLogs failed: %v", err)
	}

	if len(lines) != 2 {
		t.Fatalf("len(lines) = %d, want 2", len(lines))
	}
	if lines[0] != "svc line1" {
		t.Errorf("lines[0] = %q, want %q", lines[0], "svc line1")
	}
	if pos <= 0 {
		t.Errorf("pos = %d, want > 0", pos)
	}
}

// TestTailServiceLogs_FromPosition 从指定位置读取
func TestTailServiceLogs_FromPosition(t *testing.T) {
	dir := t.TempDir()
	svcDir := filepath.Join(dir, "myservice")
	os.MkdirAll(svcDir, 0755)

	currentPath := filepath.Join(svcDir, "current")
	content := "svc line1\nsvc line2\n"
	os.WriteFile(currentPath, []byte(content), 0644)

	// 先读取全部
	lines1, pos, err := TailServiceLogs("myservice", dir, 0)
	if err != nil {
		t.Fatalf("First TailServiceLogs failed: %v", err)
	}
	if len(lines1) != 2 {
		t.Fatalf("First read: len(lines) = %d, want 2", len(lines1))
	}

	// 追加新内容
	f, err := os.OpenFile(currentPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}
	f.WriteString("svc line3\n")
	f.Close()

	// 从上次位置读取
	lines2, newPos, err := TailServiceLogs("myservice", dir, pos)
	if err != nil {
		t.Fatalf("Second TailServiceLogs failed: %v", err)
	}
	if len(lines2) != 1 {
		t.Fatalf("Second read: len(lines) = %d, want 1", len(lines2))
	}
	if lines2[0] != "svc line3" {
		t.Errorf("lines2[0] = %q, want %q", lines2[0], "svc line3")
	}
	if newPos <= pos {
		t.Errorf("newPos = %d, want > %d", newPos, pos)
	}
}

// TestTailServiceLogs_NonexistentService 服务不存在
func TestTailServiceLogs_NonexistentService(t *testing.T) {
	dir := t.TempDir()

	lines, pos, err := TailServiceLogs("nosuchservice", dir, 0)
	if err != nil {
		t.Fatalf("TailServiceLogs failed: %v", err)
	}
	if lines != nil {
		t.Errorf("lines = %v, want nil", lines)
	}
	if pos != 0 {
		t.Errorf("pos = %d, want 0", pos)
	}
}
