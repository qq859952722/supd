package logging

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLogWriter_CreateAndWrite 创建 LogWriter 并写入日志
func TestLogWriter_CreateAndWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "current")

	w, err := NewLogWriter(path)
	if err != nil {
		t.Fatalf("NewLogWriter failed: %v", err)
	}

	data := []byte("hello log line\n")
	n, err := w.Write(data)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != len(data) {
		t.Errorf("Write returned %d, want %d", n, len(data))
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// 验证文件内容
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(content) != string(data) {
		t.Errorf("content = %q, want %q", string(content), string(data))
	}
}

// TestLogWriter_Append 验证追加写入
func TestLogWriter_Append(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "current")

	w1, err := NewLogWriter(path)
	if err != nil {
		t.Fatalf("NewLogWriter(1) failed: %v", err)
	}
	w1.Write([]byte("line1\n"))
	w1.Close()

	w2, err := NewLogWriter(path)
	if err != nil {
		t.Fatalf("NewLogWriter(2) failed: %v", err)
	}
	w2.Write([]byte("line2\n"))
	w2.Close()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	expected := "line1\nline2\n"
	if string(content) != expected {
		t.Errorf("content = %q, want %q", string(content), expected)
	}
}

// TestLogWriter_AutoCreateDir 自动创建目录
func TestLogWriter_AutoCreateDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "current")

	w, err := NewLogWriter(path)
	if err != nil {
		t.Fatalf("NewLogWriter failed: %v", err)
	}
	w.Write([]byte("deep dir\n"))
	w.Close()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("file not created in deep directory")
	}
}

// TestLogWriter_Path 返回路径
func TestLogWriter_Path(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "current")

	w, err := NewLogWriter(path)
	if err != nil {
		t.Fatalf("NewLogWriter failed: %v", err)
	}
	defer w.Close()

	if w.Path() != path {
		t.Errorf("Path() = %q, want %q", w.Path(), path)
	}
}

// TestServiceLogger_Basic 创建ServiceLogger，写入日志行，验证文件内容
func TestServiceLogger_Basic(t *testing.T) {
	dir := t.TempDir()

	logger, err := NewServiceLogger("myservice", dir, 0, 0)
	if err != nil {
		t.Fatalf("NewServiceLogger failed: %v", err)
	}

	// 用 io.Pipe 模拟子进程输出
	pr, pw := io.Pipe()

	logger.Start(pr, nil)

	// 写入日志行
	pw.Write([]byte("hello from service\n"))
	pw.Write([]byte("another line\n"))

	// 关闭写端 → logger goroutine 收到 EOF 退出
	pw.Close()

	// 等待 logger goroutine 退出
	logger.Wait()

	// 关闭日志文件
	if err := logger.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// 验证日志文件内容
	content, err := os.ReadFile(logger.LogPath())
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2; content: %q", len(lines), string(content))
	}

	// 验证格式：[ISO8601 timestamp] [level] content
	for i, line := range lines {
		if !strings.HasPrefix(line, "[") {
			t.Errorf("line %d: missing timestamp prefix: %q", i, line)
		}
		if !strings.Contains(line, " [info] ") {
			t.Errorf("line %d: missing [info] level: %q", i, line)
		}
	}

	// 验证原始内容保留
	if !strings.Contains(lines[0], "hello from service") {
		t.Errorf("line 0: missing original content: %q", lines[0])
	}
	if !strings.Contains(lines[1], "another line") {
		t.Errorf("line 1: missing original content: %q", lines[1])
	}
}

// TestServiceLogger_EOFExit pipe写端关闭后logger正常退出
func TestServiceLogger_EOFExit(t *testing.T) {
	dir := t.TempDir()

	logger, err := NewServiceLogger("eof-test", dir, 0, 0)
	if err != nil {
		t.Fatalf("NewServiceLogger failed: %v", err)
	}

	pr, pw := io.Pipe()
	logger.Start(pr, nil)

	// 关闭写端
	pw.Close()

	// Wait 应该在合理时间内返回
	done := make(chan struct{})
	go func() {
		logger.Wait()
		close(done)
	}()

	select {
	case <-done:
		// 预期：logger goroutine 已退出
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not return within 5 seconds after pipe close")
	}

	logger.Close()
}

// TestServiceLogger_MultipleLines 多行日志正确写入
func TestServiceLogger_MultipleLines(t *testing.T) {
	dir := t.TempDir()

	logger, err := NewServiceLogger("multiline-test", dir, 0, 0)
	if err != nil {
		t.Fatalf("NewServiceLogger failed: %v", err)
	}

	pr, pw := io.Pipe()
	logger.Start(pr, nil)

	// 写入多行
	for i := 0; i < 100; i++ {
		pw.Write([]byte("line content here\n"))
	}
	pw.Close()

	logger.Wait()
	logger.Close()

	content, err := os.ReadFile(logger.LogPath())
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 100 {
		t.Errorf("got %d lines, want 100", len(lines))
	}
}

// TestServiceLogger_DirAutoCreated 目录自动创建
func TestServiceLogger_DirAutoCreated(t *testing.T) {
	dir := t.TempDir()

	logger, err := NewServiceLogger("newservice", dir, 0, 0)
	if err != nil {
		t.Fatalf("NewServiceLogger failed: %v", err)
	}

	// 验证服务目录已创建
	svcDir := filepath.Join(dir, "newservice")
	if _, err := os.Stat(svcDir); os.IsNotExist(err) {
		t.Error("service directory not created")
	}

	// 验证日志文件已创建
	if _, err := os.Stat(logger.LogPath()); os.IsNotExist(err) {
		t.Error("log file not created")
	}

	logger.Close()
}

// TestServiceLogger_StdoutAndStderr 同时处理 stdout 和 stderr
func TestServiceLogger_StdoutAndStderr(t *testing.T) {
	dir := t.TempDir()

	logger, err := NewServiceLogger("both-pipes", dir, 0, 0)
	if err != nil {
		t.Fatalf("NewServiceLogger failed: %v", err)
	}

	stdoutR, stdoutW := io.Pipe()
	stderrR, stderrW := io.Pipe()

	logger.Start(stdoutR, stderrR)

	stdoutW.Write([]byte("stdout message\n"))
	stderrW.Write([]byte("stderr ERROR message\n"))

	stdoutW.Close()
	stderrW.Close()

	logger.Wait()
	logger.Close()

	content, err := os.ReadFile(logger.LogPath())
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2; content: %q", len(lines), string(content))
	}

	// stdout 和 stderr 并发写入，顺序不确定，逐一检查内容
	foundStdout := false
	foundStderr := false
	for _, line := range lines {
		if strings.Contains(line, " [info] stdout message") {
			foundStdout = true
		}
		if strings.Contains(line, " [error] stderr ERROR message") {
			foundStderr = true
		}
	}
	if !foundStdout {
		t.Errorf("missing stdout line in output: %q", string(content))
	}
	if !foundStderr {
		t.Errorf("missing stderr line in output: %q", string(content))
	}
}

// TestDetectLevel 启发式级别判定
func TestDetectLevel(t *testing.T) {
	tests := []struct {
		line  string
		level string
	}{
		{"something happened", "info"},
		{"ERROR: connection lost", "error"},
		{"error in module", "error"},
		{"Warning: high memory", "warn"},
		{"warn about timeout", "warn"},
		{"WARN: disk full", "warn"},
		{"normal operation", "info"},
		{"Error connecting to db", "error"},
	}

	for _, tt := range tests {
		got := detectLevel(tt.line)
		if got != tt.level {
			t.Errorf("detectLevel(%q) = %q, want %q", tt.line, got, tt.level)
		}
	}
}

// TestFormatLine 格式化行包含时间戳和级别
func TestFormatLine(t *testing.T) {
	line := "test message"
	result := formatLine(line)

	// 验证包含时间戳前缀
	if !strings.HasPrefix(result, "[") {
		t.Errorf("missing timestamp prefix: %q", result)
	}

	// 验证包含级别
	if !strings.Contains(result, " [info] ") {
		t.Errorf("missing [info] level: %q", result)
	}

	// 验证原始内容保留
	if !strings.HasSuffix(result, "test message") {
		t.Errorf("missing original content: %q", result)
	}
}

// TestServiceLogger_NilReaders 处理 nil reader
func TestServiceLogger_NilReaders(t *testing.T) {
	dir := t.TempDir()

	logger, err := NewServiceLogger("nil-readers", dir, 0, 0)
	if err != nil {
		t.Fatalf("NewServiceLogger failed: %v", err)
	}

	// 两个都是 nil，Start 不应 panic
	logger.Start(nil, nil)

	// Wait 应该很快返回（没有 goroutine 需要等待）
	done := make(chan struct{})
	go func() {
		logger.Wait()
		close(done)
	}()

	select {
	case <-done:
		// 预期
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return with nil readers")
	}

	logger.Close()
}

// TestServiceLogger_WriteLineWithoutStart WriteLine 在未 Start 时也能正确写入和 flush
func TestServiceLogger_WriteLineWithoutStart(t *testing.T) {
	dir := t.TempDir()

	logger, err := NewServiceLogger("no-start", dir, 0, 0)
	if err != nil {
		t.Fatalf("NewServiceLogger failed: %v", err)
	}

	// 不调用 Start()，直接 WriteLine + Close
	logger.WriteLine("error", "start failed: no such file or directory")
	if err := logger.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// 验证文件内容
	content, err := os.ReadFile(logger.LogPath())
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if len(content) == 0 {
		t.Fatal("log file is empty after WriteLine + Close")
	}

	if !strings.Contains(string(content), "start failed: no such file or directory") {
		t.Errorf("content does not contain expected message: %q", string(content))
	}
	if !strings.Contains(string(content), "[error]") {
		t.Errorf("content does not contain [error] level: %q", string(content))
	}
}

// TestServiceLogger_RealSubprocess 真实子进程日志读取
func TestServiceLogger_RealSubprocess(t *testing.T) {
	dir := t.TempDir()

	logger, err := NewServiceLogger("real-proc", dir, 0, 0)
	if err != nil {
		t.Fatalf("NewServiceLogger failed: %v", err)
	}

	// 使用真实的子进程 pipe
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}

	logger.Start(r, nil)

	// 模拟子进程写入
	w.Write([]byte("process started\n"))
	w.Write([]byte("WARN: low memory\n"))
	w.Write([]byte("working normally\n"))

	// 关闭写端模拟子进程退出
	w.Close()

	logger.Wait()
	logger.Close()

	// 验证文件内容
	content, err := os.ReadFile(logger.LogPath())
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	lineCount := 0
	for scanner.Scan() {
		lineCount++
	}
	if lineCount != 3 {
		t.Errorf("got %d lines, want 3", lineCount)
	}
}
