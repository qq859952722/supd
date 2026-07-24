package core

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestPIDFilePath 验证 PID 文件路径拼接
func TestPIDFilePath(t *testing.T) {
	p := pidFilePath("/base", "svc1")
	want := filepath.Join("/base", ".supd", "pids", "svc1.pid")
	if p != want {
		t.Fatalf("pidFilePath = %s, want %s", p, want)
	}
}

// TestProcessAlive 验证进程存活判定
func TestProcessAlive(t *testing.T) {
	if processAlive(0) {
		t.Error("pid<=0 should be not alive")
	}
	if processAlive(-5) {
		t.Error("negative pid should be not alive")
	}
	if !processAlive(os.Getpid()) {
		t.Error("current process should be alive")
	}
}

// TestCommandMatches 验证命令行匹配（仅错误路径，不依赖 /proc 真实进程）
func TestCommandMatches(t *testing.T) {
	// 空 expected → false
	if commandMatches(12345, nil) {
		t.Error("empty expected should be false")
	}
	// 不存在的 pid → false（/proc 读取失败）
	if commandMatches(999999, []string{"/bin/true"}) {
		t.Error("nonexistent pid should be false")
	}
}

// TestWriteAndRemovePIDFile 验证 PID 文件原子写入与删除
func TestWriteAndRemovePIDFile(t *testing.T) {
	dir := t.TempDir()
	proc := &Process{
		pid:       12345,
		pgid:      12345,
		cmd:       &exec.Cmd{Args: []string{"/bin/true", "a"}},
		startTime: time.Now(),
	}
	writePIDFile(dir, "svc1", proc)

	p := pidFilePath(dir, "svc1")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("pid file not written: %v", err)
	}
	data, err := os.ReadFile(p)
	if err != nil || len(data) == 0 {
		t.Fatalf("pid file empty or unreadable: %v", err)
	}

	removePIDFile(dir, "svc1")
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("pid file should be removed")
	}
}

// TestReapOrphans_DirNotExist 目录不存在应安全返回 nil
func TestReapOrphans_DirNotExist(t *testing.T) {
	reaped := ReapOrphans(filepath.Join(t.TempDir(), "nope"))
	if reaped != nil {
		t.Errorf("expected nil, got %v", reaped)
	}
}

// TestReapOrphans_InvalidJSONRemoved 非法 JSON 的 pid 文件应被删除且不 kill
func TestReapOrphans_InvalidJSONRemoved(t *testing.T) {
	dir := t.TempDir()
	pidsDir := filepath.Join(dir, ".supd", "pids")
	os.MkdirAll(pidsDir, 0755)
	os.WriteFile(filepath.Join(pidsDir, "bad.pid"), []byte("not json"), 0644)

	reaped := ReapOrphans(dir)
	if reaped != nil {
		t.Errorf("expected nil reaped (no kill), got %v", reaped)
	}
	if _, err := os.Stat(filepath.Join(pidsDir, "bad.pid")); !os.IsNotExist(err) {
		t.Error("invalid pid file should be removed")
	}
}

// TestReapOrphans_DeadPIDRemoved 指向已退出 PID 的合法记录应被清理且不 kill
func TestReapOrphans_DeadPIDRemoved(t *testing.T) {
	dir := t.TempDir()
	pidsDir := filepath.Join(dir, ".supd", "pids")
	os.MkdirAll(pidsDir, 0755)
	rec := pidFileRecord{
		Name:      "dead",
		PID:       999999,
		PGID:      999999,
		Command:   []string{"/bin/true"},
		StartTime: time.Now().Unix(),
	}
	data, _ := json.Marshal(rec)
	os.WriteFile(filepath.Join(pidsDir, "dead.pid"), data, 0644)

	reaped := ReapOrphans(dir)
	if reaped != nil {
		t.Errorf("expected nil reaped for dead pid (no kill), got %v", reaped)
	}
	if _, err := os.Stat(filepath.Join(pidsDir, "dead.pid")); !os.IsNotExist(err) {
		t.Error("dead pid file should be removed")
	}
}
