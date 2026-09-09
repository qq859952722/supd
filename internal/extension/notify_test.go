package extension

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/supdorg/supd/internal/notification"
)

// extSink 记录通知的测试 Sink，可选择丢弃模式。
type extSink struct {
	mu     sync.Mutex
	got    []notification.PendingNotification
	alwaysDrop bool
}

func (s *extSink) TryEnqueue(n notification.PendingNotification) bool {
	if s.alwaysDrop {
		return false
	}
	s.mu.Lock()
	s.got = append(s.got, n)
	s.mu.Unlock()
	return true
}

func (s *extSink) notifications() []notification.PendingNotification {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]notification.PendingNotification, len(s.got))
	copy(out, s.got)
	return out
}

func TestExecutor_NotifyReachesSinkAndLog(t *testing.T) {
	tmpDir := t.TempDir()
	sink := &extSink{}

	scriptPath := createTestScript(t, tmpDir, "notify.sh", `echo '::notify:: info "hello from ext"'`)
	executor := NewExecutor(filepath.Join(tmpDir, "log"), tmpDir)
	executor.SetNotifySink(sink)

	meta := testMeta("notify-ext", scriptPath)
	tc := TriggerContext{
		EventType:     "on_demand",
		TriggerSource: "cli",
		TriggerUser:   "test-user",
		ActionID:      "run",
		ServiceName:   "my-service",
	}

	done := make(chan struct{})
	var result *RunResult
	var execErr error
	go func() {
		result, execErr = executor.Execute(context.Background(), meta, tc, nil, 1800, nil)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Execute did not return (reader blocked?)")
	}
	if execErr != nil {
		t.Fatalf("Execute error: %v", execErr)
	}
	if result.State != TaskSuccess {
		t.Fatalf("state = %s, want success", result.State)
	}

	got := sink.notifications()
	if len(got) != 1 {
		t.Fatalf("sink got %d notifications, want 1", len(got))
	}
	n := got[0]
	if n.Level != notification.NotifyInfo || n.Content != "hello from ext" {
		t.Errorf("notification = %+v, want info/hello from ext", n)
	}
	if n.ExtensionName != "notify-ext" || n.ServiceName != "my-service" || n.ActionID != "run" {
		t.Errorf("source context mismatch: %+v", n)
	}
	if n.RunID == "" {
		t.Error("RunID should be filled by supd")
	}

	// 扩展日志应包含原始 notify 行
	logPath := filepath.Join(tmpDir, "log", "extensions", "notify-ext", result.RunID+".log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "::notify:: info \"hello from ext\"") {
		t.Errorf("extension log missing notify line: %q", string(data))
	}
}

func TestExecutor_NotifyChannelBlockedDoesNotStallReader(t *testing.T) {
	// sink 始终返回 false（模拟队列满/消费方阻塞语义）；reader 必须非阻塞排水。
	tmpDir := t.TempDir()
	sink := &extSink{alwaysDrop: true}

	// 输出大量 notify 行，验证 reader 不因消费方慢而卡住
	const n = 2000
	var sb strings.Builder
	for i := 0; i < n; i++ {
		sb.WriteString("echo '::notify:: info \"msg\"'\n")
	}
	scriptPath := createTestScript(t, tmpDir, "drop.sh", sb.String())

	executor := NewExecutor(filepath.Join(tmpDir, "log"), tmpDir)
	executor.SetNotifySink(sink)
	meta := testMeta("drop-ext", scriptPath)
	tc := TriggerContext{EventType: "on_demand", ActionID: "run"}

	done := make(chan struct{})
	var execErr error
	go func() {
		_, execErr = executor.Execute(context.Background(), meta, tc, nil, 1800, nil)
		close(done)
	}()

	// 短超时：reader 非阻塞则很快完成
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Execute did not return with dropped notify (reader stalled on consumer)")
	}
	if execErr != nil {
		t.Fatalf("Execute error: %v", execErr)
	}
	// 计数 ≥1：至少确认有 notify 被非阻塞丢弃（reader 不因消费方慢而卡）。
	// 进程退出时 exec.Cmd 会提前关闭 pipe，末尾若干行未被读取（与原 Scanner
	// 行为一致），因此不要求等于 n。
	if dropped := executor.notifyDropped.Load(); dropped == 0 {
		t.Errorf("notifyDropped = 0, want >0 (notify should have been non-blockingly dropped)")
	}
}

func TestExecutor_ProgressResultAfterReaderRefactor(t *testing.T) {
	// 回归：改用排水安全读取器后，progress/result 解析与 channel 语义不变。
	tmpDir := t.TempDir()
	scriptPath := createTestScript(t, tmpDir, "pr.sh", `echo '::progress:: 50 "half"'
echo '::result:: success "all good"'`)

	executor := NewExecutor(filepath.Join(tmpDir, "log"), tmpDir)
	meta := testMeta("pr-ext", scriptPath)
	tc := TriggerContext{EventType: "on_demand", ActionID: "run"}

	progressSeen := -1
	result, err := executor.Execute(context.Background(), meta, tc, nil, 1800, func(p int, msg string) {
		progressSeen = p
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.State != TaskSuccess {
		t.Errorf("state = %s, want success", result.State)
	}
	if result.ResultLevel != "success" || result.ResultMsg != "all good" {
		t.Errorf("result level/msg = %q/%q, want success/all good", result.ResultLevel, result.ResultMsg)
	}
	if progressSeen != 50 {
		t.Errorf("progress callback last = %d, want 50", progressSeen)
	}
}