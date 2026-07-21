package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestPackAndUnpack 打包+解包目录，验证内容一致
func TestPackAndUnpack(t *testing.T) {
	// 创建临时源目录
	srcDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(srcDir, "subdir"), 0755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "hello.txt"), []byte("hello world"), 0644); err != nil {
		t.Fatalf("write hello.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "subdir", "nested.txt"), []byte("nested content"), 0644); err != nil {
		t.Fatalf("write nested.txt: %v", err)
	}

	// 打包到 buffer
	var buf bytes.Buffer
	if err := PackDir(srcDir, &buf); err != nil {
		t.Fatalf("PackDir: %v", err)
	}

	// 解包到新目录
	destDir := t.TempDir()
	if err := UnpackDir(&buf, destDir); err != nil {
		t.Fatalf("UnpackDir: %v", err)
	}

	// 验证文件内容
	got, err := os.ReadFile(filepath.Join(destDir, "hello.txt"))
	if err != nil {
		t.Fatalf("read hello.txt: %v", err)
	}
	if string(got) != "hello world" {
		t.Errorf("hello.txt content = %q, want %q", got, "hello world")
	}

	got2, err := os.ReadFile(filepath.Join(destDir, "subdir", "nested.txt"))
	if err != nil {
		t.Fatalf("read nested.txt: %v", err)
	}
	if string(got2) != "nested content" {
		t.Errorf("nested.txt content = %q, want %q", got2, "nested content")
	}
}

// TestUnpackPathTraversal 解包包含路径穿越的恶意 tar.gz，验证返回错误
func TestUnpackPathTraversal(t *testing.T) {
	destDir := t.TempDir()

	// 构造恶意 tar.gz，含 ../../etc/passwd 条目
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// 恶意条目：尝试穿越到上级目录
	header := &tar.Header{
		Name: "../../etc/passwd",
		Mode: 0644,
		Size: int64(len("malicious")),
	}
	if err := tw.WriteHeader(header); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := tw.Write([]byte("malicious")); err != nil {
		t.Fatalf("write body: %v", err)
	}
	tw.Close()
	gw.Close()

	err := UnpackDir(&buf, destDir)
	if err == nil {
		t.Error("expected error for path traversal, got nil")
	}
}

// TestListEntries 列出 tar.gz 中的条目
func TestListEntries(t *testing.T) {
	// 创建临时源目录
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(srcDir, "dir"), 0755); err != nil {
		t.Fatalf("mkdir dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "dir", "b.txt"), []byte("b"), 0644); err != nil {
		t.Fatalf("write b.txt: %v", err)
	}

	// 打包
	var buf bytes.Buffer
	if err := PackDir(srcDir, &buf); err != nil {
		t.Fatalf("PackDir: %v", err)
	}

	// 列出条目
	entries, err := ListEntries(&buf)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}

	// 验证至少包含我们创建的文件
	sort.Strings(entries)
	foundA := false
	foundB := false
	for _, e := range entries {
		if e == "a.txt" {
			foundA = true
		}
		if e == "dir/b.txt" || e == "dir\\b.txt" {
			foundB = true
		}
	}
	if !foundA {
		t.Errorf("entries missing a.txt, got %v", entries)
	}
	if !foundB {
		t.Errorf("entries missing dir/b.txt, got %v", entries)
	}
}

// TestUnpackStripsSingleTopLevelPrefix 解包时自动检测并去除单一顶层目录前缀
// BUG-03/04 修复回归测试：用户用 `tar -czf xxx.tar.gz dir/` 打包后导入，
// 不应导致 destDir/dir/file.yaml 双层嵌套
func TestUnpackStripsSingleTopLevelPrefix(t *testing.T) {
	destDir := t.TempDir()

	// 构造带顶层目录前缀的 tar.gz（模拟 `tar -czf xxx.tar.gz my-service/`）
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	entries := []struct {
		name     string
		typ      byte
		content  string
	}{
		{"my-service/", tar.TypeDir, ""},
		{"my-service/service.yaml", tar.TypeReg, "name: my-service\n"},
		{"my-service/run.sh", tar.TypeReg, "#!/bin/sh\n"},
	}
	for _, e := range entries {
		hdr := &tar.Header{
			Name:     e.name,
			Mode:     0644,
			Size:     int64(len(e.content)),
			Typeflag: e.typ,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header %q: %v", e.name, err)
		}
		if e.content != "" {
			if _, err := tw.Write([]byte(e.content)); err != nil {
				t.Fatalf("write body %q: %v", e.name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}

	// 解包：应去除 "my-service/" 前缀
	if err := UnpackDir(&buf, destDir); err != nil {
		t.Fatalf("UnpackDir: %v", err)
	}

	// 验证：service.yaml 应直接在 destDir 下，而非 destDir/my-service/
	data, err := os.ReadFile(filepath.Join(destDir, "service.yaml"))
	if err != nil {
		t.Fatalf("read service.yaml (expected at destDir root): %v", err)
	}
	if string(data) != "name: my-service\n" {
		t.Errorf("service.yaml content = %q, want %q", data, "name: my-service\n")
	}

	// run.sh 也应在 destDir 根
	if _, err := os.Stat(filepath.Join(destDir, "run.sh")); err != nil {
		t.Errorf("run.sh not found at destDir root: %v", err)
	}

	// my-service/ 子目录不应存在
	if _, err := os.Stat(filepath.Join(destDir, "my-service")); err == nil {
		t.Error("my-service/ directory should not exist (prefix should have been stripped)")
	}
}

// TestUnpackMixedTopLevelEntries 顶层有多个不同目录时不去除前缀
// 当 tar 包内顶层有多个不同目录（如 dir1/ + dir2/），不能去除前缀
func TestUnpackMixedTopLevelEntries(t *testing.T) {
	destDir := t.TempDir()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// 顶层有两个不同目录：dir1/ 和 dir2/
	for _, name := range []string{"dir1/a.txt", "dir2/b.txt"} {
		hdr := &tar.Header{
			Name:     name,
			Mode:     0644,
			Size:     1,
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header %q: %v", name, err)
		}
		if _, err := tw.Write([]byte("x")); err != nil {
			t.Fatalf("write body %q: %v", name, err)
		}
	}
	tw.Close()
	gw.Close()

	// 解包：应保留原结构（因为顶层有多个不同目录）
	if err := UnpackDir(&buf, destDir); err != nil {
		t.Fatalf("UnpackDir: %v", err)
	}

	// 验证两个目录都保留
	if _, err := os.Stat(filepath.Join(destDir, "dir1", "a.txt")); err != nil {
		t.Errorf("dir1/a.txt not found: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "dir2", "b.txt")); err != nil {
		t.Errorf("dir2/b.txt not found: %v", err)
	}
}

// TestPackNonExistentDir 打包不存在的目录应返回错误
func TestPackNonExistentDir(t *testing.T) {
	var buf bytes.Buffer
	err := PackDir("/nonexistent/path/that/does/not/exist", &buf)
	if err == nil {
		t.Error("expected error for nonexistent directory, got nil")
	}
}

// TestUnpackTarBomb_SingleFileExceedsLimit 解包单文件超过 MaxSingleFileSize 应返回错误
// D-06-1 修复验证
func TestUnpackTarBomb_SingleFileExceedsLimit(t *testing.T) {
	destDir := t.TempDir()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// 声明单文件大小超过 1GB 限制（不需要实际写这么多数据）
	header := &tar.Header{
		Name:     "huge.txt",
		Mode:     0644,
		Size:     MaxSingleFileSize + 1,
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(header); err != nil {
		t.Fatalf("write header: %v", err)
	}
	// 不调用 tw.Close()：Close 会尝试写入 MaxSingleFileSize+1 字节的零 padding，
	// 导致内存暴涨。header 已写入 buffer，tar reader 能读取 header 触发大小检查。
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}

	err := UnpackDir(&buf, destDir)
	if err == nil {
		t.Fatal("expected error for single file exceeding limit, got nil")
	}
}

// TestUnpackTarBomb_TotalSizeExceedsLimit 解包累计大小超过 MaxUncompressedSize 应返回错误
// D-06-1 修复验证：构造 11 个 header（每个声明 1GB），累计 11GB > 10GB 上限
// 使用独立的 tar.Writer 构造每个 header，避免 tw.Close() 写入 1GB padding 导致 OOM
func TestUnpackTarBomb_TotalSizeExceedsLimit(t *testing.T) {
	destDir := t.TempDir()

	// 手工拼接 tar header：每个 header 声明 MaxSingleFileSize (1GB)，
	// 11 个 header 累计 11GB > MaxUncompressedSize (10GB)
	var tarBuf bytes.Buffer
	for i := 0; i < 11; i++ {
		// 每次用独立 tar.Writer 仅写入 header（无文件内容、无 padding）
		hw := tar.NewWriter(&tarBuf)
		h := &tar.Header{
			Name:     fmt.Sprintf("big%d.bin", i),
			Mode:     0644,
			Size:     MaxSingleFileSize,
			Typeflag: tar.TypeReg,
		}
		if err := hw.WriteHeader(h); err != nil {
			t.Fatalf("write header %d: %v", i, err)
		}
		// 不调用 hw.Close()，避免触发 padding
	}
	// 写入两个 512 字节零块作为 tar footer
	zeroBlock := make([]byte, 1024)
	tarBuf.Write(zeroBlock)

	// gzip 压缩 tar 流
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(tarBuf.Bytes()); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}

	err := UnpackDir(&buf, destDir)
	if err == nil {
		t.Fatal("expected error for total size exceeding limit, got nil")
	}
}

// TestUnpackNormalSizeStillWorks 正常大小的归档应能解包成功
// D-06-1 修复验证：确保防护不影响正常使用
func TestUnpackNormalSizeStillWorks(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "normal.txt"), []byte("normal content"), 0644); err != nil {
		t.Fatalf("write normal.txt: %v", err)
	}

	var buf bytes.Buffer
	if err := PackDir(srcDir, &buf); err != nil {
		t.Fatalf("PackDir: %v", err)
	}

	destDir := t.TempDir()
	if err := UnpackDir(&buf, destDir); err != nil {
		t.Fatalf("UnpackDir normal archive failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(destDir, "normal.txt"))
	if err != nil {
		t.Fatalf("read normal.txt: %v", err)
	}
	if string(got) != "normal content" {
		t.Errorf("normal.txt content = %q, want %q", got, "normal content")
	}
}
