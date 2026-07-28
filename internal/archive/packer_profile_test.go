package archive

import (
	"bytes"
	"compress/gzip"
	"archive/tar"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/supdorg/supd/internal/config"
)

// listArchiveEntries 打包后解包列出条目名称，用于验证过滤效果
func listArchiveEntries(t *testing.T, buf *bytes.Buffer) []string {
	t.Helper()
	gr, err := gzip.NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	var entries []string
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		entries = append(entries, hdr.Name)
	}
	sort.Strings(entries)
	return entries
}

// createTestServiceDir 创建模拟服务目录
//
//	svcDir/
//	  service.yaml
//	  env.yaml
//	  bin/
//	    myapp
//	  data/
//	    config/
//	      app.conf
//	    cache/
//	      tmp.data
//	    state/
//	      state.db
//	  extensions/
//	    my-ext/
//	      meta.yaml
//	  app.log
//	  app.bak
//	  .cache/
//	    junk
func createTestServiceDir(t *testing.T) string {
	t.Helper()
	svcDir := t.TempDir()

	mustWrite := func(rel string, content []byte) {
		full := filepath.Join(svcDir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, content, 0644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	mustWrite("service.yaml", []byte("name: test-svc\n"))
	mustWrite("env.yaml", []byte("env: {}\n"))
	mustWrite("bin/myapp", []byte("binary"))
	mustWrite("data/config/app.conf", []byte("config"))
	mustWrite("data/cache/tmp.data", []byte("cache"))
	mustWrite("data/state/state.db", []byte("state"))
	mustWrite("extensions/my-ext/meta.yaml", []byte("name: my-ext\n"))
	mustWrite("app.log", []byte("log"))
	mustWrite("app.bak", []byte("backup"))
	mustWrite(".cache/junk", []byte("junk"))

	return svcDir
}

// TestPackDirDefaultProfile 默认导出（无 profile）：排除 data/ 和垃圾文件
func TestPackDirDefaultProfile(t *testing.T) {
	svcDir := createTestServiceDir(t)

	var buf bytes.Buffer
	if err := PackDir(svcDir, &buf); err != nil {
		t.Fatalf("PackDir: %v", err)
	}

	entries := listArchiveEntries(t, &buf)

	// 应包含
	mustContain := []string{"service.yaml", "env.yaml", "bin/myapp", "extensions/my-ext/meta.yaml"}
	for _, e := range mustContain {
		found := false
		for _, got := range entries {
			if got == e {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("default export should contain %q, entries=%v", e, entries)
		}
	}

	// 不应包含 data/ 和垃圾文件
	mustNotContain := []string{"data/config/app.conf", "data/cache/tmp.data", "data/state/state.db", "app.log", "app.bak", ".cache/junk"}
	for _, e := range mustNotContain {
		for _, got := range entries {
			if got == e {
				t.Errorf("default export should NOT contain %q, entries=%v", e, entries)
			}
		}
	}
}

// TestPackDirMigrateProfile 迁移导出：含 data/config，排除 data/cache 和 data/state
func TestPackDirMigrateProfile(t *testing.T) {
	svcDir := createTestServiceDir(t)

	profile := &config.PackageConfig{
		Default: "include",
		Exclude: []string{"data/cache/", "data/state/", "*.bak"},
	}

	var buf bytes.Buffer
	if err := PackDirWithProfile(svcDir, &buf, profile); err != nil {
		t.Fatalf("PackDirWithProfile: %v", err)
	}

	entries := listArchiveEntries(t, &buf)

	// 应包含 data/config/（迁移时需要配置）
	if !contains(entries, "data/config/app.conf") {
		t.Errorf("migrate profile should contain data/config/app.conf, entries=%v", entries)
	}
	// 不应包含 data/cache/ 和 data/state/
	if contains(entries, "data/cache/tmp.data") {
		t.Errorf("migrate profile should NOT contain data/cache/, entries=%v", entries)
	}
	if contains(entries, "data/state/state.db") {
		t.Errorf("migrate profile should NOT contain data/state/, entries=%v", entries)
	}
	// 不应包含 .bak
	if contains(entries, "app.bak") {
		t.Errorf("migrate profile should NOT contain .bak, entries=%v", entries)
	}
}

// TestPackDirShareProfile 共享导出：default:exclude，仅含 bin/ 和 extensions/
func TestPackDirShareProfile(t *testing.T) {
	svcDir := createTestServiceDir(t)

	profile := &config.PackageConfig{
		Default: "exclude",
		Include: []string{"bin/", "extensions/"},
	}

	var buf bytes.Buffer
	if err := PackDirWithProfile(svcDir, &buf, profile); err != nil {
		t.Fatalf("PackDirWithProfile: %v", err)
	}

	entries := listArchiveEntries(t, &buf)

	// 应包含 bin/、extensions/ 和强制包含的 service.yaml
	if !contains(entries, "bin/myapp") {
		t.Errorf("share profile should contain bin/myapp, entries=%v", entries)
	}
	if !contains(entries, "extensions/my-ext/meta.yaml") {
		t.Errorf("share profile should contain extensions/, entries=%v", entries)
	}
	if !contains(entries, "service.yaml") {
		t.Errorf("share profile should always contain service.yaml (forced), entries=%v", entries)
	}

	// 不应包含任何 data/ 内容
	if contains(entries, "data/config/app.conf") {
		t.Errorf("share profile should NOT contain data/, entries=%v", entries)
	}
	// 不应包含 env.yaml（default:exclude 模式下未在 include 中）
	if contains(entries, "env.yaml") {
		t.Errorf("share profile should NOT contain env.yaml (not in include), entries=%v", entries)
	}
}

// TestMatchPattern 测试路径模式匹配逻辑
func TestMatchPattern(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		// 目录模式（以 / 结尾）
		{"data/", "data", true},
		{"data/", "data/config/app.conf", true},
		{"data/", "data/cache", true},
		{"data/", "database", false},
		{"data/", "mydata", false},

		// 含 / 的模式（完整路径匹配）
		{"data/config/", "data/config", true},
		{"data/config/", "data/config/app.conf", true},
		{"data/config/", "data/cache/tmp", false},

		// 不含 / 的模式（basename 匹配）
		{"*.log", "app.log", true},
		{"*.log", "data/app.log", true},
		{"*.log", "app.txt", false},
		{"*.bak", "app.bak", true},

		// 精确文件名（无 / 时匹配 basename，与 .gitignore 语义一致）
		{"service.yaml", "service.yaml", true},
		{"service.yaml", "data/service.yaml", true}, // 无 / 时匹配 basename
		{"service.yaml", "data/app.yaml", false},
	}

	for _, tt := range tests {
		got := matchPattern(tt.pattern, tt.path)
		if got != tt.want {
			t.Errorf("matchPattern(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
		}
	}
}

// contains 检查切片中是否包含指定字符串
func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
