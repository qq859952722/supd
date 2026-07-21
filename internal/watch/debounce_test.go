package watch

import (
	"slices"
	"testing"
	"time"
)

// REQ-F-026: 单个事件直接通过（防抖后输出）
func TestDebouncer_SingleEvent(t *testing.T) {
	d := NewDebouncer(50 * time.Millisecond)
	d.Start()
	defer d.Stop()

	event := ChangeEvent{
		Path:      "/etc/supd/test.yaml",
		Operation: "write",
		Timestamp: time.Now(),
	}
	d.Push(event)

	select {
	case batch := <-d.Events():
		if len(batch) != 1 {
			t.Fatalf("expected 1 event, got %d", len(batch))
		}
		if batch[0].Path != event.Path {
			t.Fatalf("expected path %s, got %s", event.Path, batch[0].Path)
		}
		if batch[0].Operation != event.Operation {
			t.Fatalf("expected operation %s, got %s", event.Operation, batch[0].Operation)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for debounced event")
	}
}

// REQ-F-026: 500ms 内多个事件合并为一批
func TestDebouncer_MultipleEventsBatched(t *testing.T) {
	d := NewDebouncer(100 * time.Millisecond)
	d.Start()
	defer d.Stop()

	events := []ChangeEvent{
		{Path: "/etc/supd/a.yaml", Operation: "write", Timestamp: time.Now()},
		{Path: "/etc/supd/b.yaml", Operation: "create", Timestamp: time.Now()},
		{Path: "/etc/supd/c.yaml", Operation: "remove", Timestamp: time.Now()},
	}

	for _, e := range events {
		d.Push(e)
		// 短暂延迟确保事件在同一个防抖窗口内
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case batch := <-d.Events():
		if len(batch) != 3 {
			t.Fatalf("expected 3 events in batch, got %d", len(batch))
		}
		paths := make([]string, len(batch))
		for i, e := range batch {
			paths[i] = e.Path
		}
		for _, orig := range events {
			if !slices.Contains(paths, orig.Path) {
				t.Fatalf("expected path %s in batch", orig.Path)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for debounced batch")
	}
}

// REQ-F-026: 同路径去重，保留最后一个事件
func TestDebouncer_DedupByPath(t *testing.T) {
	d := NewDebouncer(100 * time.Millisecond)
	d.Start()
	defer d.Stop()

	// 同一文件先 write 再 rename，应只保留 rename
	d.Push(ChangeEvent{Path: "/etc/supd/test.yaml", Operation: "write", Timestamp: time.Now()})
	time.Sleep(10 * time.Millisecond)
	d.Push(ChangeEvent{Path: "/etc/supd/test.yaml", Operation: "rename", Timestamp: time.Now()})

	select {
	case batch := <-d.Events():
		if len(batch) != 1 {
			t.Fatalf("expected 1 event (deduped), got %d", len(batch))
		}
		if batch[0].Operation != "rename" {
			t.Fatalf("expected operation 'rename' (last event), got %s", batch[0].Operation)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for debounced event")
	}
}

// REQ-F-026: 不同路径不合并为同一事件
func TestDebouncer_DifferentPathsNotDeduped(t *testing.T) {
	d := NewDebouncer(100 * time.Millisecond)
	d.Start()
	defer d.Stop()

	d.Push(ChangeEvent{Path: "/etc/supd/a.yaml", Operation: "write", Timestamp: time.Now()})
	time.Sleep(5 * time.Millisecond)
	d.Push(ChangeEvent{Path: "/etc/supd/b.yaml", Operation: "write", Timestamp: time.Now()})

	select {
	case batch := <-d.Events():
		if len(batch) != 2 {
			t.Fatalf("expected 2 events (different paths), got %d", len(batch))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for debounced batch")
	}
}

// REQ-F-026: Stop 正常退出
func TestDebouncer_Stop(t *testing.T) {
	d := NewDebouncer(50 * time.Millisecond)
	d.Start()

	// 推入事件后立即停止
	d.Push(ChangeEvent{Path: "/etc/supd/test.yaml", Operation: "write", Timestamp: time.Now()})

	// Stop 应该 flush 剩余事件后正常退出
	d.Stop()

	// 通道应已关闭（可能先收到 flush 的数据，需要 drain）
	for range d.Events() {
		// drain any remaining batches
	}
}

// REQ-F-026: 两批事件间隔验证（防抖窗口过期后才能收到下一批）
func TestDebouncer_TwoBatches(t *testing.T) {
	d := NewDebouncer(50 * time.Millisecond)
	d.Start()
	defer d.Stop()

	// 第一批
	d.Push(ChangeEvent{Path: "/etc/supd/a.yaml", Operation: "write", Timestamp: time.Now()})

	select {
	case batch := <-d.Events():
		if len(batch) != 1 {
			t.Fatalf("first batch: expected 1 event, got %d", len(batch))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for first batch")
	}

	// 等待防抖窗口完全过期后推入第二批
	time.Sleep(100 * time.Millisecond)
	d.Push(ChangeEvent{Path: "/etc/supd/b.yaml", Operation: "create", Timestamp: time.Now()})

	select {
	case batch := <-d.Events():
		if len(batch) != 1 {
			t.Fatalf("second batch: expected 1 event, got %d", len(batch))
		}
		if batch[0].Path != "/etc/supd/b.yaml" {
			t.Fatalf("second batch: expected path b.yaml, got %s", batch[0].Path)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for second batch")
	}
}
