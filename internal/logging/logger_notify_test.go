package logging

import (
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/supdorg/supd/internal/notification"
)

// memSink 记录通知的测试 Sink。
type memSink struct {
	mu  sync.Mutex
	got []notification.PendingNotification
}

func (s *memSink) TryEnqueue(n notification.PendingNotification) bool {
	s.mu.Lock()
	s.got = append(s.got, n)
	s.mu.Unlock()
	return true
}

func (s *memSink) notifications() []notification.PendingNotification {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]notification.PendingNotification, len(s.got))
	copy(out, s.got)
	return out
}

func TestServiceLogger_StdoutNotifyWritesLogAndSink(t *testing.T) {
	dir := t.TempDir()
	sink := &memSink{}

	logger, err := NewServiceLogger("notify-svc", dir, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	logger.SetNotifySink(sink)

	stdoutR, stdoutW := io.Pipe()
	logger.Start(stdoutR, nil)

	stdoutW.Write([]byte("::notify:: info \"hello from stdout\"\n"))
	stdoutW.Write([]byte("a normal line\n"))
	stdoutW.Close()

	logger.Wait()
	logger.Close()

	contentBytes, err := readLog(logger)
	if err != nil {
		t.Fatal(err)
	}
	content := string(contentBytes)

	// 原始 notify 行必须保留在日志
	if !strings.Contains(content, "::notify:: info \"hello from stdout\"") {
		t.Errorf("log missing notify line: %q", content)
	}
	if !strings.Contains(content, "a normal line") {
		t.Errorf("log missing normal line: %q", content)
	}

	// Sink 应收到 1 条通知，来源为服务名
	got := sink.notifications()
	if len(got) != 1 {
		t.Fatalf("sink got %d notifications, want 1", len(got))
	}
	n := got[0]
	if n.Level != notification.NotifyInfo || n.Content != "hello from stdout" {
		t.Errorf("notification = %+v, want info/hello from stdout", n)
	}
	if n.ServiceName != "notify-svc" {
		t.Errorf("ServiceName = %q, want notify-svc", n.ServiceName)
	}
	if n.ExtensionName != "" || n.ActionID != "" || n.RunID != "" {
		t.Errorf("service stdout notify should not carry extension/action/run context: %+v", n)
	}
}

func TestServiceLogger_StderrSameTextOnlyLogNoSink(t *testing.T) {
	dir := t.TempDir()
	sink := &memSink{}

	logger, err := NewServiceLogger("stderr-svc", dir, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	logger.SetNotifySink(sink)

	stderrR, stderrW := io.Pipe()
	logger.Start(nil, stderrR)

	// 与 stdout 测试相同的协议文本，但走 stderr → 应只写日志，不生成通知
	stderrW.Write([]byte("::notify:: info \"hello from stderr\"\n"))
	stderrW.Close()

	logger.Wait()
	logger.Close()

	contentBytes, err := readLog(logger)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contentBytes), "::notify:: info \"hello from stderr\"") {
		t.Errorf("stderr notify text should still be logged: %q", string(contentBytes))
	}
	if got := sink.notifications(); len(got) != 0 {
		t.Errorf("stderr should not produce notifications, got %d", len(got))
	}
}

func TestServiceLogger_StdoutFormatErrorLogsWarningNoSink(t *testing.T) {
	dir := t.TempDir()
	sink := &memSink{}

	logger, err := NewServiceLogger("fmt-svc", dir, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	logger.SetNotifySink(sink)

	pr, pw := io.Pipe()
	logger.Start(pr, nil)
	pw.Write([]byte("::notify:: hiss \"bad level\"\n")) // 非法等级
	pw.Write([]byte("::notify:: info noquote\n"))       // 缺引号
	pw.Close()

	logger.Wait()
	logger.Close()

	contentBytes, err := readLog(logger)
	if err != nil {
		t.Fatal(err)
	}
	content := string(contentBytes)
	if strings.Contains(content, "hiss") && !strings.Contains(content, "[warn]") {
		t.Errorf("format error should append a warning log, content: %q", content)
	}
	if got := sink.notifications(); len(got) != 0 {
		t.Errorf("invalid notify lines must not reach sink, got %d", len(got))
	}
}

func TestServiceLogger_OverlongLineDrains(t *testing.T) {
	dir := t.TempDir()
	sink := &memSink{}

	logger, err := NewServiceLogger("long-svc", dir, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	logger.SetNotifySink(sink)

	pr, pw := io.Pipe()
	logger.Start(pr, nil)

	// 64KB+1 的无换行超长行 + 一条短行；不中断排水
	long := strings.Repeat("x", 64*1024+1)
	pw.Write([]byte(long + "\nshort tail\n"))
	pw.Close()

	// Wait 应在合理时间内返回（证明不因超长行阻塞/退出）
	done := make(chan struct{})
	go func() { logger.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not return after overlong line (drain blocked)")
	}
	logger.Close()

	contentBytes, err := readLog(logger)
	if err != nil {
		t.Fatal(err)
	}
	content := string(contentBytes)
	if strings.Count(content, "\n") != 2 {
		t.Errorf("expected 2 log lines (truncated long + short tail), got %q", content)
	}
	if !strings.Contains(content, "short tail") {
		t.Errorf("drain lost short tail after overlong line: %q", content)
	}
}

func TestServiceLogger_CloseNoGoroutineLeak(t *testing.T) {
	runtime.GC()
	runtime.GC()
	baseline := runtime.NumGoroutine()

	for i := 0; i < 3; i++ {
		logger, err := NewServiceLogger("leak", t.TempDir(), 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		pr, pw := io.Pipe()
		logger.Start(pr, nil)
		pw.Write([]byte("::notify:: info \"x\"\n"))
		pw.Close()
		logger.Wait()
		logger.Close()
	}

	runtime.GC()
	after := runtime.NumGoroutine()
	if after > baseline+2 {
		t.Errorf("goroutine leak: baseline=%d, after=%d", baseline, after)
	}
}

func readLog(l *ServiceLogger) ([]byte, error) {
	return os.ReadFile(l.LogPath())
}