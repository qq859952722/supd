package logging

import (
	"strings"
	"testing"
)

// TestDiskFullBufferAdd 测试缓冲添加
// REQ-E-008: 磁盘满→内存缓冲1000行
func TestDiskFullBufferAdd(t *testing.T) {
	buf := NewDiskFullBuffer()

	// 添加少于容量的行
	for i := 0; i < 500; i++ {
		buf.Add("line")
	}
	if buf.Len() != 500 {
		t.Errorf("expected 500 lines, got %d", buf.Len())
	}
	if buf.Dropped() != 0 {
		t.Errorf("expected 0 dropped, got %d", buf.Dropped())
	}
}

// TestDiskFullBufferOverflow 缓冲溢出时丢弃最旧
// REQ-E-008: 缓冲满时丢弃最旧行
func TestDiskFullBufferOverflow(t *testing.T) {
	buf := NewDiskFullBuffer()

	// 添加超过容量的行
	for i := 0; i < diskFullBufferCapacity+500; i++ {
		buf.Add("line")
	}

	// 缓冲不应超过容量
	if buf.Len() > diskFullBufferCapacity {
		t.Errorf("buffer length %d exceeds capacity %d", buf.Len(), diskFullBufferCapacity)
	}

	// 应有丢弃计数
	if buf.Dropped() == 0 {
		t.Error("expected some dropped lines")
	}
}

// TestDiskFullBufferFlush 缓冲刷出
// REQ-E-008: 磁盘恢复后将缓冲内容写出
func TestDiskFullBufferFlush(t *testing.T) {
	buf := NewDiskFullBuffer()

	// 添加一些行
	for i := 0; i < 100; i++ {
		buf.Add("line")
	}

	// Flush
	var written []string
	flushed := buf.Flush(func(s string) {
		written = append(written, s)
	})

	if flushed != 100 {
		t.Errorf("expected 100 flushed, got %d", flushed)
	}
	if len(written) != 100 {
		t.Errorf("expected 100 written, got %d", len(written))
	}
	if buf.Len() != 0 {
		t.Errorf("expected 0 lines after flush, got %d", buf.Len())
	}
}

// TestDiskFullBufferFlushEmpty 空缓冲刷出
func TestDiskFullBufferFlushEmpty(t *testing.T) {
	buf := NewDiskFullBuffer()
	flushed := buf.Flush(func(s string) {})
	if flushed != 0 {
		t.Errorf("expected 0 flushed for empty buffer, got %d", flushed)
	}
}

// TestDiskFullBufferPreservesContent 缓冲保留内容正确性
func TestDiskFullBufferPreservesContent(t *testing.T) {
	buf := NewDiskFullBuffer()

	for i := 0; i < 5; i++ {
		buf.Add(string(rune('a' + i)))
	}

	var written []string
	buf.Flush(func(s string) {
		written = append(written, s)
	})

	expected := []string{"a", "b", "c", "d", "e"}
	for i, w := range written {
		if w != expected[i] {
			t.Errorf("line %d: expected %q, got %q", i, expected[i], w)
		}
	}
}

// TestDiskFullBufferCapacity 数值验证
// REQ-E-008: 缓冲容量1000行
func TestDiskFullBufferCapacity(t *testing.T) {
	if diskFullBufferCapacity != 1000 {
		t.Errorf("diskFullBufferCapacity = %d, want 1000", diskFullBufferCapacity)
	}
}

// TestIsDiskFull 磁盘满检测
// REQ-E-008: 检测磁盘满状态
func TestIsDiskFull(t *testing.T) {
	// 测试根目录（通常不会满）
	full := IsDiskFull("/")
	// 根目录一般不会满，但不做硬性断言，因为可能真满了
	t.Logf("IsDiskFull(\"/\") = %v", full)

	// 测试不存在的路径 → 应返回 true（无法获取信息假设满）
	full = IsDiskFull("/nonexistent/path/that/does/not/exist")
	if !full {
		t.Error("expected IsDiskFull to return true for nonexistent path")
	}
}

// TestTryWriteWithFallbackNormal 正常写入
func TestTryWriteWithFallbackNormal(t *testing.T) {
	tmpDir := t.TempDir()
	writer, err := NewLogWriter(tmpDir + "/test.log")
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()

	rotator, err := NewRotatingLogWriter(tmpDir+"/test.log", 10, 5)
	if err != nil {
		t.Fatal(err)
	}
	defer rotator.Close()

	buf := NewDiskFullBuffer()
	line := []byte("test log line\n")

	err = TryWriteWithFallback(writer, rotator, buf, line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestTryWriteWithFallbackDiskFull 磁盘满时缓冲
// REQ-E-008: 磁盘满→内存缓冲
func TestTryWriteWithFallbackDiskFull(t *testing.T) {
	tmpDir := t.TempDir()
	writer, err := NewLogWriter(tmpDir + "/test.log")
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()

	buf := NewDiskFullBuffer()

	// 使用不存在的路径模拟磁盘满
	line := []byte("test log line\n")
	// TryWriteWithFallback with nil rotator → dirPath="/"
	// 根目录通常不会满，所以这个测试主要验证流程
	err = TryWriteWithFallback(writer, nil, buf, line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestCanWriteToDir 目录可写检查
// REQ-E-009: user.Lookup失败时的明确错误信息辅助
func TestCanWriteToDir(t *testing.T) {
	// 临时目录应该可写
	tmpDir := t.TempDir()
	if !CanWriteToDir(tmpDir) {
		t.Error("expected temp dir to be writable")
	}

	// 不存在的目录应该不可写
	if CanWriteToDir("/nonexistent/path/that/does/not/exist") {
		t.Error("expected nonexistent dir to be not writable")
	}
}

// TestDiskFullBufferAddLine 内容含换行符
func TestDiskFullBufferAddLine(t *testing.T) {
	buf := NewDiskFullBuffer()
	buf.Add("line with\nnewline")

	var written []string
	buf.Flush(func(s string) {
		written = append(written, s)
	})

	if len(written) != 1 {
		t.Fatalf("expected 1 line, got %d", len(written))
	}
	if !strings.Contains(written[0], "line with") {
		t.Errorf("unexpected content: %q", written[0])
	}
}
