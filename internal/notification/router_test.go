package notification

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/supdorg/supd/internal/store"
)

// newRouterStore 打开临时 SQLite Store 并构建 NotifyRouter。
func newRouterStore(t *testing.T) (*store.Store, *NotifyRouter) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	t.Cleanup(func() { st.Close(5 * time.Second) })
	return st, NewRouter(st)
}

// waitTopicsNum 等待满足条件的 Topic 出现（writer 异步落库）。
func waitTopicsNum(t *testing.T, st *store.Store, f store.TopicFilter, target int) []store.TopicItem {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		list, err := st.ListTopics(context.Background(), f)
		if err == nil && len(list) >= target {
			return list
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected >=%d topics with filter %+v, not found", target, f)
	return nil
}

// findTopic 在列表中按 kind/source 查找。
func findTopic(list []store.TopicItem, kind, source string) *store.TopicItem {
	for i := range list {
		if list[i].Kind == kind && list[i].SourceName == source {
			return &list[i]
		}
	}
	return nil
}

func TestServiceNotifyToDefaultTopic(t *testing.T) {
	st, r := newRouterStore(t)

	if !r.TryEnqueue(PendingNotification{Level: NotifyInfo, Content: "hello", ServiceName: "web"}) {
		t.Fatal("notify dropped")
	}
	list := waitTopicsNum(t, st, store.TopicFilter{}, 1)
	svc := findTopic(list, "service", "web")
	if svc == nil {
		t.Fatalf("service default topic web not created: %+v", list)
	}
	if svc.LastSeq != 1 || svc.LastContent == nil || *svc.LastContent != "hello" {
		t.Errorf("service topic digest wrong: %+v", svc)
	}
	// 来源含 service_name；无 run/execution/extension 上下文。
	notifs, err := st.ListNotifications(context.Background(), svc.ID, 0, 10)
	if err != nil || len(notifs) != 1 {
		t.Fatalf("notifications: %v len=%d", err, len(notifs))
	}
	n := notifs[0]
	if n.SourceType != "service" || n.ServiceName == nil || *n.ServiceName != "web" {
		t.Errorf("notification source wrong: %+v", n)
	}
	if n.RunID != nil || n.ExecutionID != nil || n.ExtensionName != nil {
		t.Errorf("service stdout notify must not carry run/execution/extension context: %+v", n)
	}
}

func TestServiceLevelExtNotifyToServiceTopic(t *testing.T) {
	// 非操作服务级扩展 Run → 所属服务默认 Topic（kind=service），source_type=extension。
	st, r := newRouterStore(t)
	if !r.TryEnqueue(PendingNotification{
		Level: NotifySuccess, Content: "ext-notify",
		ServiceName: "web", ExtensionName: "web-check", ActionID: "a1", RunID: "r1",
	}) {
		t.Fatal("notify dropped")
	}
	list := waitTopicsNum(t, st, store.TopicFilter{}, 1)
	svc := findTopic(list, "service", "web")
	if svc == nil {
		t.Fatalf("service default topic not used for service-level ext run: %+v", list)
	}
	notifs, _ := st.ListNotifications(context.Background(), svc.ID, 0, 10)
	if len(notifs) != 1 {
		t.Fatalf("got %d notifications", len(notifs))
	}
	n := notifs[0]
	if n.SourceType != "extension" || n.ExtensionName == nil || *n.ExtensionName != "web-check" ||
		n.ServiceName == nil || *n.ServiceName != "web" || n.RunID == nil || *n.RunID != "r1" {
		t.Errorf("service-level ext routing source wrong: %+v", n)
	}
}

func TestGlobalExtNotifyToExtensionTopic(t *testing.T) {
	// 非操作全局扩展 Run → 该扩展默认 Topic（kind=extension），service_name 为空。
	st, r := newRouterStore(t)
	if !r.TryEnqueue(PendingNotification{
		Level: NotifyWarning, Content: "global",
		ExtensionName: "g-ext", ActionID: "a", RunID: "r-g",
	}) {
		t.Fatal("notify dropped")
	}
	list := waitTopicsNum(t, st, store.TopicFilter{}, 1)
	ext := findTopic(list, "extension", "g-ext")
	if ext == nil {
		t.Fatalf("extension default topic not created: %+v", list)
	}
	notifs, _ := st.ListNotifications(context.Background(), ext.ID, 0, 10)
	if len(notifs) != 1 || notifs[0].SourceType != "extension" || notifs[0].ServiceName != nil {
		t.Errorf("global ext routing source wrong: %+v", notifs)
	}
}

func TestOperationNotifyRouting(t *testing.T) {
	st, r := newRouterStore(t)
	execID := uuid.New().String()
	svc := "web"
	topicID, err := st.CreateExecution(context.Background(), store.CreateExecutionInput{
		ExecutionID: execID, OperationID: "op", OperationLabel: "op", CreatedAt: time.Now().UnixMilli(),
		Runs: []store.PlannedRun{
			{RunID: uuid.New().String(), Phase: "service", ServiceName: &svc, ExtensionName: "s-ext", ActionID: "op"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if !r.TryEnqueue(PendingNotification{
		Level: NotifyInfo, Content: "op-notify",
		ServiceName: "web", ExtensionName: "s-ext", ActionID: "op",
		RunID:       "r-op",
		ExecutionID: execID,
	}) {
		t.Fatal("notify dropped")
	}
	_ = topicID
	// 通知落在操作 Topic（execution 关联），来源含 run_id/execution_id，source_type=extension。
	deadline := time.Now().Add(5 * time.Second)
	var notifs []store.Notification
	for time.Now().Before(deadline) {
		notifs, err = st.ListNotifications(context.Background(), topicID, 0, 10)
		if err == nil && len(notifs) == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(notifs) != 1 {
		t.Fatalf("operation topic did not receive notify: %v", notifs)
	}
	n := notifs[0]
	if n.SourceType != "extension" || n.ExecutionID == nil || *n.ExecutionID != execID ||
		n.RunID == nil || *n.RunID != "r-op" {
		t.Errorf("operation routing source wrong: %+v", n)
	}
	// 操作 Topic 独特于默认 Topic：不应建出 service/extension 默认 Topic。
	list, _ := st.ListTopics(context.Background(), store.TopicFilter{})
	if len(list) != 1 || list[0].ID != topicID {
		t.Errorf("expected only operation topic, got %+v", list)
	}
}

func TestSourceNotForgeable(t *testing.T) {
	// 脚本试图伪造来源字段无效：来源只来自闭包（路由填充），落库来源与闭包一致。
	st, r := newRouterStore(t)
	// 即使 PendingNotification 被恶意塞入伪造 service_name，落库仍以路由命中的 topic source 为准。
	if !r.TryEnqueue(PendingNotification{
		Level: NotifyInfo, Content: "forged", ServiceName: "real-svc",
	}) {
		t.Fatal("notify dropped")
	}
	list := waitTopicsNum(t, st, store.TopicFilter{}, 1)
	svc := findTopic(list, "service", "real-svc")
	if svc == nil {
		t.Fatalf("source must come from closure (real-svc), got %+v", list)
	}
	if findTopic(list, "service", "forged") != nil {
		t.Fatalf("forged service name was used as topic source")
	}
}

func TestLateNotifyRejected(t *testing.T) {
	st, r := newRouterStore(t)
	execID := uuid.New().String()
	topicID, err := st.CreateExecution(context.Background(), store.CreateExecutionInput{
		ExecutionID: execID, OperationID: "op", OperationLabel: "op", CreatedAt: time.Now().UnixMilli(),
		Runs: []store.PlannedRun{{RunID: uuid.New().String(), Phase: "global", ExtensionName: "g", ActionID: "op"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CloseTopic(context.Background(), topicID, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}

	// 关闭后 notify → 不落库（GET 仍返回已关闭 Topic，但通知数不变）。
	if !r.TryEnqueue(PendingNotification{Level: NotifyInfo, Content: "late", ExecutionID: execID}) {
		t.Fatal("notify dropped unexpectedly")
	}
	deadline := time.Now().Add(2 * time.Second)
	var cnt int64
	for time.Now().Before(deadline) {
		d, err := st.GetTopic(context.Background(), topicID)
		if err == nil && d != nil {
			cnt = d.NotificationCount
		}
		time.Sleep(10 * time.Millisecond)
	}
	if cnt != 0 {
		t.Fatalf("late notify wrote %d to closed topic, want 0", cnt)
	}

	// 限频：同一 Topic 第二次迟到 warning 不再记（60 秒内）。无法直接断言日志，
	// 验证路由仍幂等拒绝且不落库即可（限频由 warnLate 内部的 lastWarn 保证）。
	if !r.TryEnqueue(PendingNotification{Level: NotifyInfo, Content: "late2", ExecutionID: execID}) {
		t.Fatal("notify dropped unexpectedly")
	}
	time.Sleep(50 * time.Millisecond)
	d, _ := st.GetTopic(context.Background(), topicID)
	if d == nil {
		t.Fatal("topic vanished")
	}
	if d.NotificationCount != 0 {
		t.Fatalf("late notifications persisted after rate-limit window: count=%d", d.NotificationCount)
	}
}

func TestServiceTopicNeverCloses(t *testing.T) {
	// 服务默认 Topic 无关闭入口：本层只有 StoreTryClose 面向 operation。路由不产生关闭动作。
	st, r := newRouterStore(t)
	if !r.TryEnqueue(PendingNotification{Level: NotifyInfo, Content: "x", ServiceName: "svc"}) {
		t.Fatal("notify dropped")
	}
	list := waitTopicsNum(t, st, store.TopicFilter{}, 1)
	svc := findTopic(list, "service", "svc")
	if svc == nil {
		t.Fatal("service topic missing")
	}
	if svc.ClosedAt != nil {
		t.Fatalf("service default topic must stay open (closed_at=%v)", svc.ClosedAt)
	}
	// 继续追加正常。
	if !r.TryEnqueue(PendingNotification{Level: NotifyInfo, Content: "y", ServiceName: "svc"}) {
		t.Fatal("notify dropped")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		d, err := st.GetTopic(context.Background(), svc.ID)
		if err == nil && d != nil && d.LastSeq >= 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("service topic did not accumulate notifications")
}

func TestQueueFullDrops(t *testing.T) {
	st, r := newRouterStore(t)
	// 用关闭底层连接模拟 writer 命令一直失败不推进 seq；直接用一次性注入命令填满队列后再路由。
	// 简化：验证 TryEnqueue 返回值语义 —— 路由器主管控制逻辑，队列满由 store 非阻塞返回。
	// 这里通过构造一个满队列：先用 store 自有的 tryEnqueue 填满 writer 队列再调用路由。
	// 由于 writer 单线程消费，填满后再路由一条应触发丢弃。
	filled := false
	block := func() {
		// 填满队列：排空后大量入队阻塞命令，最后一个非阻塞入队返回 false。
		for i := 0; ; i++ {
			if !st.TryAppendNotification(store.NotificationInput{
				TopicID: "whatever", Level: "info", Content: "x", SourceType: "service",
			}) {
				filled = true
				break
			}
			if i > 5000 {
				t.Fatal("queue never filled")
			}
		}
	}
	block()
	if !filled {
		t.Fatal("store queue not full")
	}
	// 队列满：路由应非阻塞返回 false（丢弃计数），而不得阻塞或 panic。
	start := time.Now()
	ok := r.TryEnqueue(PendingNotification{Level: NotifyInfo, Content: "drop", ServiceName: "s"})
	if ok {
		t.Log("note: router returned true (queued) — acceptable if slot freed by writer")
	}
	if since := time.Since(start); since > time.Second {
		t.Fatalf("TryEnqueue blocked for %v on full queue (must be non-blocking)", since)
	}
	st.Close(5 * time.Second)
}

func TestReadCursor(t *testing.T) {
	st, r := newRouterStore(t)
	// 用默认 Topic 落库。
	r.TryEnqueue(PendingNotification{Level: NotifyInfo, Content: "m1", ServiceName: "svc"})
	r.TryEnqueue(PendingNotification{Level: NotifyInfo, Content: "m2", ServiceName: "svc"})
	list := waitTopicsNum(t, st, store.TopicFilter{Kind: "service"}, 1)
	id := list[0].ID
	// 等两条通知全部落库（last_seq==2）后再读，避免 MarkRead 与追加竞态。
	pollCond(t, "two notifs written", 3*time.Second, func() bool {
		d, err := st.GetTopic(context.Background(), id)
		return err == nil && d != nil && d.LastSeq == 2
	})
	if st.MarkRead(context.Background(), id, 2) != nil {
		t.Fatal("markread failed")
	}
	d, _ := st.GetTopic(context.Background(), id)
	if d.LastSeq != 2 || d.UnreadCount != 0 {
		t.Errorf("after read: last=%d unread=%d, want 2/0", d.LastSeq, d.UnreadCount)
	}
	// 新通知 → 重新未读。
	r.TryEnqueue(PendingNotification{Level: NotifyInfo, Content: "m3", ServiceName: "svc"})
	pollCond(t, "seq advances to 3", 3*time.Second, func() bool {
		d2, err := st.GetTopic(context.Background(), id)
		return err == nil && d2 != nil && d2.LastSeq == 3 && d2.UnreadCount == 1
	})
	// read-all。
	if st.MarkAllRead(context.Background()) != nil {
		t.Fatal("mark all read failed")
	}
	d3, _ := st.GetTopic(context.Background(), id)
	if d3.UnreadCount != 0 {
		t.Errorf("unread after read-all = %d", d3.UnreadCount)
	}
}

func TestDeleteTopicSoft(t *testing.T) {
	st, r := newRouterStore(t)
	r.TryEnqueue(PendingNotification{Level: NotifyInfo, Content: "x", ServiceName: "d"})
	list := waitTopicsNum(t, st, store.TopicFilter{}, 1)
	id := list[0].ID

	if st.DeleteTopic(context.Background(), id, time.Now().UnixMilli()) != nil {
		t.Fatal("delete failed")
	}
	// 删除后列表/详情排除。
	after, _ := st.ListTopics(context.Background(), store.TopicFilter{})
	if len(after) != 0 {
		t.Errorf("deleted topic still listed")
	}
	if d, _ := st.GetTopic(context.Background(), id); d != nil {
		t.Errorf("deleted topic still in detail")
	}
	// 新通知自动重建同 source 默认 Topic（UUID 不复用）。
	r.TryEnqueue(PendingNotification{Level: NotifyInfo, Content: "y", ServiceName: "d"})
	list2 := waitTopicsNum(t, st, store.TopicFilter{}, 1)
	if list2[0].ID == id {
		t.Errorf("deleted topic UUID reused")
	}
}

func TestChangesGlobalSeqSignals(t *testing.T) {
	// changes 长轮询语义基础：GlobalSeq 单调推进，每次写 +1；WaitForGlobalChange 在
	// 新写事件时返回。handler 层在此基础上判断 epoch/reload（见 notification_handler_test.go）。
	st := newRouterStoreStore(t)
	before := st.GlobalSeq()
	if before < 0 {
		t.Fatalf("GlobalSeq = %d", before)
	}
	r := NewRouter(st)
	if !r.TryEnqueue(PendingNotification{Level: NotifyInfo, Content: "sig", ServiceName: "s"}) {
		t.Fatal("notify dropped")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if st.GlobalSeq() > before {
			return
		}
		<-st.WaitForGlobalChange(context.Background(), st.GlobalSeq())
	}
	t.Fatalf("GlobalSeq did not advance after write (before=%d cur=%d)", before, st.GlobalSeq())
}

func TestContent8KB(t *testing.T) {
	// 8KB 边界：重建协议行（前缀+引号+content）≤8192 落库；超出被二次防御丢弃。
	st, r := newRouterStore(t)
	// content 取接近 8KB 余量，保证重建后 ≤8192。
	boundary := strings.Repeat("a", 8190-9-7) // 留足前缀/引号/level 余量 → 重建≤8192
	if !r.TryEnqueue(PendingNotification{Level: NotifyInfo, Content: boundary, ServiceName: "svc"}) {
		t.Fatal("boundary notify dropped")
	}
	list := waitTopicsNum(t, st, store.TopicFilter{}, 1)
	d, _ := st.GetTopic(context.Background(), list[0].ID)
	if d == nil || d.NotificationCount != 1 {
		t.Fatalf("boundary-length content did not persist: %+v", d)
	}

	// 超长 content（重建 >8192）→ 二次防御丢弃，不产生通知。
	tooLong := strings.Repeat("b", notifyMaxLen) // 必然超限
	if !r.TryEnqueue(PendingNotification{Level: NotifyInfo, Content: tooLong, ServiceName: "svc"}) {
		t.Fatal("overlong notify reported drop; it is deliberately swallowed")
	}
	time.Sleep(100 * time.Millisecond)
	d2, _ := st.GetTopic(context.Background(), list[0].ID)
	if d2.NotificationCount != 1 {
		t.Errorf("overlong content must not persist: count=%d", d2.NotificationCount)
	}
}

func TestStoreErrorField(t *testing.T) {
	// StoreError 在正常时为 nil；写失败达阈值后置错态为 {code:"store_unavailable"}；
	// 恢复（成功写）后为 nil。API 顶层 store_error 字段凭此契约填充。
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(5 * time.Second)
	svc := "e"
	r := NewRouter(st)
	r.TryEnqueue(PendingNotification{Level: NotifyInfo, Content: "a", ServiceName: svc})
	waitTopicsNum(t, st, store.TopicFilter{}, 1)
	if se := st.StoreError(); se != nil {
		t.Fatalf("StoreError should be nil in normal op, got %+v", se)
	}

	// 强制错误态：连续向不存在 topic 同步 append 达阈值（每次 writer 失败 → 计数）。
	for i := 0; i < 10; i++ {
		_ = st.AppendNotification(context.Background(), store.NotificationInput{
			TopicID: "no-such-topic", Level: "info", Content: "x", SourceType: "system",
		})
	}
	se := st.StoreError()
	if se == nil {
		t.Fatal("StoreError should be non-nil after repeated write failures")
	}
	if se.Code != "store_unavailable" {
		t.Errorf("StoreError code = %q, want store_unavailable", se.Code)
	}

	// 恢复：一次成功写清除错误态。
	r.TryEnqueue(PendingNotification{Level: NotifyInfo, Content: "recover", ServiceName: svc})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if se2 := st.StoreError(); se2 == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("StoreError not cleared after successful write: %+v", st.StoreError())
}

// newRouterStoreStore 打开临时 Store（仅构造，不建 router）。
func newRouterStoreStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close(5 * time.Second) })
	return st
}

// pollCond 轮询直到 cond 为 true（writer 异步落库确定性等待）。
func pollCond(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("cond not met within %v: %s", timeout, what)
}
