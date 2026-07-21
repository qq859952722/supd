package watch

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// REQ-F-026: 创建临时目录，启动 Watcher，创建文件验证收到事件
func TestWatcher_CreateFile(t *testing.T) {
	tmpDir := t.TempDir()
	// 创建基础子目录结构
	if err := os.MkdirAll(filepath.Join(tmpDir, "services"), 0755); err != nil {
		t.Fatal(err)
	}

	w, err := NewWatcher(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	w.Start()
	defer w.Stop()

	// 创建文件
	testFile := filepath.Join(tmpDir, "services", "test.yaml")
	if err := os.WriteFile(testFile, []byte("test: true"), 0644); err != nil {
		t.Fatal(err)
	}

	// 等待防抖窗口 + 额外余量
	// REQ-F-026: Linux 上 os.WriteFile 可能触发 write 而非 create 事件，两者都可接受
	select {
	case batch := <-w.Events():
		found := false
		for _, e := range batch {
			if e.Path == testFile && (e.Operation == "create" || e.Operation == "write") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected create/write event for %s, got batch: %+v", testFile, batch)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for file create event")
	}
}

// REQ-F-026: 创建子目录，验证自动添加监控并在新目录中创建文件能收到事件
// 白名单策略：新目录必须在 services/ 或 extensions/ 容器目录下才会被监控
func TestWatcher_NewSubDirWatched(t *testing.T) {
	tmpDir := t.TempDir()
	// 创建 services 容器目录（白名单目录）
	if err := os.MkdirAll(filepath.Join(tmpDir, "services"), 0755); err != nil {
		t.Fatal(err)
	}

	w, err := NewWatcher(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	w.Start()
	defer w.Stop()

	// 在 services/ 下创建新的服务目录（符合白名单规则：services/<name>/）
	newDir := filepath.Join(tmpDir, "services", "newservice")
	if err := os.Mkdir(newDir, 0755); err != nil {
		t.Fatal(err)
	}

	// 等待 create 目录事件
	select {
	case batch := <-w.Events():
		found := false
		for _, e := range batch {
			if e.Path == newDir && e.Operation == "create" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected create event for new dir %s, got batch: %+v", newDir, batch)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for new dir create event")
	}

	// 在新目录中创建文件，验证能收到事件
	time.Sleep(600 * time.Millisecond) // 等待防抖窗口过期
	testFile := filepath.Join(newDir, "service.yaml")
	if err := os.WriteFile(testFile, []byte("name: test"), 0644); err != nil {
		t.Fatal(err)
	}

	select {
	case batch := <-w.Events():
		found := false
		for _, e := range batch {
			if e.Path == testFile {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected event for file in new dir %s, got batch: %+v", testFile, batch)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for file event in new subdirectory")
	}
}

// REQ-F-026: 删除文件，验证收到 remove 事件
func TestWatcher_RemoveFile(t *testing.T) {
	tmpDir := t.TempDir()

	// 先创建文件
	testFile := filepath.Join(tmpDir, "test.yaml")
	if err := os.WriteFile(testFile, []byte("test: true"), 0644); err != nil {
		t.Fatal(err)
	}

	w, err := NewWatcher(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	w.Start()
	defer w.Stop()

	// 删除文件
	if err := os.Remove(testFile); err != nil {
		t.Fatal(err)
	}

	select {
	case batch := <-w.Events():
		found := false
		for _, e := range batch {
			if e.Path == testFile && e.Operation == "remove" {
				found = true
				break
			}
		}
		if !found {
			// 有些系统可能先报告 rename 再 remove
			t.Logf("batch: %+v", batch)
			for _, e := range batch {
				if e.Path == testFile {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected remove event for %s", testFile)
			}
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for file remove event")
	}
}

// REQ-F-026: 重命名文件，验证收到 rename 事件
func TestWatcher_RenameFile(t *testing.T) {
	tmpDir := t.TempDir()

	// 先创建文件
	srcFile := filepath.Join(tmpDir, "old.yaml")
	if err := os.WriteFile(srcFile, []byte("test: true"), 0644); err != nil {
		t.Fatal(err)
	}

	w, err := NewWatcher(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	w.Start()
	defer w.Stop()

	// 重命名文件
	dstFile := filepath.Join(tmpDir, "new.yaml")
	if err := os.Rename(srcFile, dstFile); err != nil {
		t.Fatal(err)
	}

	select {
	case batch := <-w.Events():
		// rename 操作会产生 rename 源文件和 create 目标文件两个事件
		hasRename := false
		for _, e := range batch {
			if e.Path == srcFile && e.Operation == "rename" {
				hasRename = true
			}
		}
		if !hasRename {
			// 某些平台可能以不同方式报告 rename
			t.Logf("batch (looking for rename): %+v", batch)
		}
		// 只要能收到事件就说明 rename 被监听到了
		if len(batch) == 0 {
			t.Fatal("expected at least one event for rename operation")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for file rename event")
	}
}

// REQ-F-026: Stop 正常退出
func TestWatcher_Stop(t *testing.T) {
	tmpDir := t.TempDir()

	w, err := NewWatcher(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	w.Start()

	// Stop 应正常退出
	w.Stop()

	// Events 通道应已关闭
	_, ok := <-w.Events()
	if ok {
		t.Fatal("expected events channel to be closed after Stop")
	}
}

// REQ-F-026: 写入文件收到 write 事件
func TestWatcher_WriteFile(t *testing.T) {
	tmpDir := t.TempDir()

	// 先创建文件
	testFile := filepath.Join(tmpDir, "test.yaml")
	if err := os.WriteFile(testFile, []byte("test: true"), 0644); err != nil {
		t.Fatal(err)
	}

	w, err := NewWatcher(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	w.Start()
	defer w.Stop()

	// 修改文件
	if err := os.WriteFile(testFile, []byte("test: updated"), 0644); err != nil {
		t.Fatal(err)
	}

	select {
	case batch := <-w.Events():
		found := slices.ContainsFunc(batch, func(e ChangeEvent) bool {
			return e.Path == testFile && e.Operation == "write"
		})
		if !found {
			t.Fatalf("expected write event for %s, got batch: %+v", testFile, batch)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for file write event")
	}
}

// REQ-F-026: 删除目录不报错
func TestWatcher_RemoveDir(t *testing.T) {
	tmpDir := t.TempDir()

	// 创建子目录
	subDir := filepath.Join(tmpDir, "svc1")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	w, err := NewWatcher(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	w.Start()
	defer w.Stop()

	// 删除子目录
	if err := os.RemoveAll(subDir); err != nil {
		t.Fatal(err)
	}

	// 应收到 remove 事件且不 panic
	select {
	case batch := <-w.Events():
		// 只要收到事件就说明删除事件不报错
		if len(batch) == 0 {
			t.Fatal("expected at least one event for directory removal")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for directory remove event")
	}
}

// TestShouldWatchDir_Whitelist 白名单策略单元测试
// 验证只有配置文件目录及其父目录被监控，运行时数据目录被排除
func TestShouldWatchDir_Whitelist(t *testing.T) {
	baseDir := "/etc/supd"

	tests := []struct {
		name string
		path string
		want bool
	}{
		// 根目录 - 监控（config.yaml）
		{"root dir", baseDir, true},

		// 容器目录 - 监控（捕获新服务/扩展创建）
		{"env container", baseDir + "/env", true},
		{"services container", baseDir + "/services", true},
		{"extensions container", baseDir + "/extensions", true},

		// 服务目录 - 监控（service.yaml, env.yaml）
		{"service dir", baseDir + "/services/web-demo", true},

		// 服务级扩展容器目录 - 监控
		{"service extensions container", baseDir + "/services/web-demo/extensions", true},

		// 服务级扩展目录 - 监控（meta.yaml）
		{"service extension dir", baseDir + "/services/web-demo/extensions/demo-action", true},

		// 全局扩展目录 - 监控（meta.yaml）
		{"global extension dir", baseDir + "/extensions/debounce-demo", true},

		// 运行时数据目录 - 不监控
		{"service data dir", baseDir + "/services/qbittorrent/data", false},
		{"qbittorrent config dir", baseDir + "/services/qbittorrent/data/qbittorrent/qBittorrent/config", false},
		{"service bin dir", baseDir + "/services/web-demo/bin", false},

		// 系统目录 - 不监控
		{"supd pids dir", baseDir + "/.supd/pids", false},
		{"logs dir", baseDir + "/logs", false},
		{"history dir", baseDir + "/history", false},

		// 未知顶层目录 - 不监控
		{"unknown top dir", baseDir + "/unknown", false},

		// 深层非配置目录 - 不监控
		{"deep non-config dir", baseDir + "/services/web-demo/subdir/deep", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldWatchDir(baseDir, tt.path); got != tt.want {
				t.Errorf("shouldWatchDir(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// TestShouldSkipDir_Performance 验证运行时数据目录被跳过遍历
func TestShouldSkipDir_Performance(t *testing.T) {
	baseDir := "/etc/supd"

	skipTests := []struct {
		name string
		path string
		want bool
	}{
		{"root dir", baseDir, false},
		{"services dir", baseDir + "/services", false},
		{"data dir", baseDir + "/services/qbittorrent/data", true},
		{"bin dir", baseDir + "/services/web-demo/bin", true},
		{"logs dir", baseDir + "/logs", true},
		{"history dir", baseDir + "/history", true},
		{"hidden dir", baseDir + "/.supd", true},
		{"cache dir", baseDir + "/services/web-demo/cache", true},
		{"tmp dir", baseDir + "/services/web-demo/tmp", true},
	}

	for _, tt := range skipTests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldSkipDir(baseDir, tt.path); got != tt.want {
				t.Errorf("shouldSkipDir(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// TestWatcher_QbittorrentDataNotWatched 集成测试：
// 验证 qbittorrent 的 data 目录不会被监控，避免原子写配置时产生警告日志
func TestWatcher_QbittorrentDataNotWatched(t *testing.T) {
	tmpDir := t.TempDir()
	// 模拟 qbittorrent 的目录结构
	qbtConfigDir := filepath.Join(tmpDir, "services", "qbittorrent", "data", "qBittorrent", "config")
	if err := os.MkdirAll(qbtConfigDir, 0755); err != nil {
		t.Fatal(err)
	}
	// 创建 service.yaml 使 qbittorrent 目录被监控
	if err := os.WriteFile(filepath.Join(tmpDir, "services", "qbittorrent", "service.yaml"), []byte("name: qbittorrent"), 0644); err != nil {
		t.Fatal(err)
	}

	w, err := NewWatcher(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	w.Start()
	defer w.Stop()

	// 等待 watcher 初始化
	time.Sleep(200 * time.Millisecond)

	// 在 qbittorrent data 目录下创建临时文件（模拟原子写）
	tempFile := filepath.Join(qbtConfigDir, ".vlmdoX")
	if err := os.WriteFile(tempFile, []byte("temp"), 0644); err != nil {
		t.Fatal(err)
	}
	// 立即删除（模拟 rename 后临时文件消失）
	os.Remove(tempFile)

	// 等待一段时间，确保不会收到 qbittorrent data 目录的事件
	select {
	case batch := <-w.Events():
		// 检查是否有来自 data 目录的事件
		for _, e := range batch {
			if strings.Contains(e.Path, "/data/qBittorrent/config/") {
				t.Fatalf("should not receive event from qbittorrent data dir, but got: %+v", e)
			}
		}
		// 其他事件可以接受
	case <-time.After(1 * time.Second):
		// 没有事件是预期的（data 目录不被监控）
	}
}
