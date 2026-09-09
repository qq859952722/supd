package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// OpenWithMigrations 仅供测试使用（仅存在于 *_test.go，生产代码走 Open）。
// 用于迁移失败/回滚测试：注入完整的迁移列表（含故意失败版本）。
func OpenWithMigrations(baseDir string, migrations []string) (*Store, error) {
	return openWithList(baseDir, migrations)
}

func testCtx() context.Context { return context.Background() }

func mustOpen(t *testing.T, baseDir string) *Store {
	t.Helper()
	s, err := Open(baseDir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	return s
}

// waitForLastSeq 等待异步 writer 把某 Topic 推送到目标 last_seq（带超时）。
func waitForLastSeq(t *testing.T, s *Store, topicID string, target int64) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		d, err := s.GetTopic(testCtx(), topicID)
		if err == nil && d != nil && d.LastSeq >= target {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("writer did not reach last_seq=%d (topic drained too slow)", target)
}

func mustCreateOperationTopic(t *testing.T, s *Store, opID string, createdAt int64) string {
	t.Helper()
	svc := "web"
	topicID, err := s.CreateExecution(testCtx(), CreateExecutionInput{
		ExecutionID:    uuid.New().String(),
		OperationID:    opID,
		OperationLabel: opID,
		CreatedAt:      createdAt,
		Runs: []PlannedRun{
			{RunID: uuid.New().String(), Phase: "global", ServiceName: nil, ExtensionName: "g-ext", ActionID: opID},
			{RunID: uuid.New().String(), Phase: "service", ServiceName: &svc, ExtensionName: "s-ext", ActionID: opID},
		},
	})
	if err != nil {
		t.Fatalf("CreateExecution failed: %v", err)
	}
	return topicID
}

func mustAppend(t *testing.T, s *Store, topicID, level, content, sourceType string) {
	t.Helper()
	err := s.AppendNotification(testCtx(), NotificationInput{
		TopicID:    topicID,
		Level:      level,
		Content:    content,
		SourceType: sourceType,
	})
	if err != nil {
		t.Fatalf("AppendNotification failed: %v", err)
	}
}

func TestOpenCreatesDataDir(t *testing.T) {
	base := t.TempDir()
	s := mustOpen(t, base)
	defer s.Close(5 * time.Second)

	dataDir := filepath.Join(base, "data")
	info, err := os.Stat(dataDir)
	if err != nil {
		t.Fatalf("data dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("data path is not a directory")
	}
	if perm := info.Mode().Perm(); perm != 0750 {
		t.Errorf("data dir mode = %o, want 750", perm)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "supd.db")); err != nil {
		t.Errorf("supd.db not created: %v", err)
	}
}

func TestMigrationVersioning(t *testing.T) {
	base := t.TempDir()
	// 首次打开：v2 生效。
	s := mustOpen(t, base)
	if v, err := currentVersion(s.db); err != nil || v != 2 {
		t.Fatalf("user_version = %d (%v), want 2", v, err)
	}
	s.Close(5 * time.Second)

	// 重复打开幂等：user_version 不变。
	s2 := mustOpen(t, base)
	if v, _ := currentVersion(s2.db); v != 2 {
		t.Fatalf("reopen user_version = %d, want 2 (idempotent)", v)
	}
	s2.Close(5 * time.Second)

	// 注入故意失败的 v3 → Open 返回 error，且 v3 部分变更不存在，v2 保留。
	bad := append(append([]string{}, migrations...), "CREATE TABLE partial_v3 (id TEXT); CREATE TABLE boom_bad (broken @@)")
	_, err := OpenWithMigrations(base, bad)
	if err == nil {
		t.Fatal("OpenWithMigrations with failing v3 should error")
	}
	if !strings.Contains(err.Error(), "v3") {
		t.Errorf("error should mention version v3, got: %v", err)
	}

	// 失败后数据库可重新打开，user_version 仍为 v2。
	s3 := mustOpen(t, base)
	// v2 的表存在。
	var n int
	if err := s3.db.QueryRow("SELECT COUNT(*) FROM notification_topic").Scan(&n); err != nil {
		t.Fatalf("v2 table missing after failed migration: %v", err)
	}
	// v3 的部分表不存在。
	qErr := s3.db.QueryRow("SELECT COUNT(*) FROM partial_v3").Scan(&n)
	if qErr == nil {
		t.Fatal("partial_v3 table exists, migration was not rolled back")
	}
	if v, _ := currentVersion(s3.db); v != 2 {
		t.Fatalf("user_version after rollback = %d, want 2", v)
	}
	s3.Close(5 * time.Second)
}

func TestWALAndSingleConnection(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	defer s.Close(5 * time.Second)

	var mode string
	if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if !strings.Contains(mode, "wal") {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
	if got := s.db.Stats().MaxOpenConnections; got != 1 {
		t.Errorf("MaxOpenConnections = %d, want 1", got)
	}
}

func TestCreateExecutionAtomic(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	defer s.Close(5 * time.Second)

	// 全局 run 误填 service_name → 违反 CHECK → 插入失败 → 整个事务回滚。
	svc := "web"
	_, err := s.CreateExecution(testCtx(), CreateExecutionInput{
		ExecutionID:    uuid.New().String(),
		OperationID:    "op",
		OperationLabel: "op",
		CreatedAt:      nowMillis(),
		Runs: []PlannedRun{
			{RunID: uuid.New().String(), Phase: "global", ServiceName: &svc, ExtensionName: "g-ext", ActionID: "op"},
		},
	})
	if err == nil {
		t.Fatal("CreateExecution should fail on CHECK violation")
	}

	var execCount, topicCount int
	_ = s.db.QueryRow("SELECT COUNT(*) FROM operation_execution").Scan(&execCount)
	_ = s.db.QueryRow("SELECT COUNT(*) FROM notification_topic").Scan(&topicCount)
	if execCount != 0 || topicCount != 0 {
		t.Fatalf("transaction not rolled back: executions=%d topics=%d, want 0/0", execCount, topicCount)
	}
}

func TestAppendNotificationSeq(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	defer s.Close(5 * time.Second)
	topicID := mustCreateOperationTopic(t, s, "op", nowMillis())

	const total = 300
	var wg sync.WaitGroup
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s.TryAppendNotification(NotificationInput{
				TopicID: topicID, Level: "info", Content: fmt.Sprintf("m%d", i), SourceType: SrcTypeService,
			})
		}(i)
	}
	wg.Wait()

	// tryEnqueue 是异步非阻塞的，需等待 writer 排空队列后再校验。
	waitForLastSeq(t, s, topicID, total)

	rows, err := s.db.Query("SELECT seq FROM notification WHERE topic_id=? ORDER BY seq", topicID)
	if err != nil {
		t.Fatalf("query seqs: %v", err)
	}
	defer rows.Close()
	seen := make(map[int64]struct{}, total)
	var prev int64
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			t.Fatal(err)
		}
		if _, dup := seen[seq]; dup {
			t.Fatalf("duplicate seq %d", seq)
		}
		if prev != 0 && seq != prev+1 {
			t.Fatalf("seq gap: %d then %d", prev, seq)
		}
		seen[seq] = struct{}{}
		prev = seq
	}
	if len(seen) != total {
		t.Fatalf("got %d notifications, want %d", len(seen), total)
	}
	detail, err := s.GetTopic(testCtx(), topicID)
	if err != nil || detail == nil {
		t.Fatalf("GetTopic: %v", err)
	}
	if detail.LastSeq != total {
		t.Errorf("last_seq = %d, want %d", detail.LastSeq, total)
	}
}

func TestTopicCap500(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	defer s.Close(5 * time.Second)
	topicID := mustCreateOperationTopic(t, s, "op", nowMillis())

	const total = 501
	for i := 1; i <= total; i++ {
		mustAppend(t, s, topicID, "info", fmt.Sprintf("m%d", i), SrcTypeService)
	}

	n, err := s.GetTopic(testCtx(), topicID)
	if err != nil || n == nil {
		t.Fatalf("GetTopic: %v", err)
	}
	if n.NotificationCount != 500 {
		t.Errorf("count = %d, want 500 (oldest pruned)", n.NotificationCount)
	}
	if n.LastSeq != total {
		t.Errorf("last_seq = %d, want %d", n.LastSeq, total)
	}
	// 最旧 1 条(seq=1)被淘汰，保留 2..501。
	list, err := s.ListNotifications(testCtx(), topicID, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 500 || list[0].Seq != 2 || list[499].Seq != 501 {
		t.Errorf("kept seqs wrong: len=%d first=%d last=%d", len(list), list[0].Seq, list[499].Seq)
	}
}

func TestMarkReadForwardOnly(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	defer s.Close(5 * time.Second)
	topicID := mustCreateOperationTopic(t, s, "op", nowMillis())
	mustAppend(t, s, topicID, "info", "a", SrcTypeService) // last_seq=1
	mustAppend(t, s, topicID, "info", "b", SrcTypeService) // last_seq=2

	if err := s.MarkRead(testCtx(), topicID, 2); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkRead(testCtx(), topicID, 1); err != nil {
		t.Fatal(err)
	}
	d, _ := s.GetTopic(testCtx(), topicID)
	if d.ReadSeq != 2 {
		t.Errorf("read_seq = %d, want 2 (forward only)", d.ReadSeq)
	}
	if err := s.MarkRead(testCtx(), topicID, 5); err != nil {
		t.Fatal(err)
	}
	d, _ = s.GetTopic(testCtx(), topicID)
	if d.ReadSeq != 5 {
		t.Errorf("read_seq = %d, want 5", d.ReadSeq)
	}
	if d.UnreadCount != 0 {
		t.Errorf("unread = %d, want 0", d.UnreadCount)
	}
}

func TestDeleteTopicSoft(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	defer s.Close(5 * time.Second)
	topicID := mustCreateOperationTopic(t, s, "op", nowMillis())
	mustAppend(t, s, topicID, "info", "a", SrcTypeSystem)

	if err := s.DeleteTopic(testCtx(), topicID, nowMillis()); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListTopics(testCtx(), TopicFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range list {
		if it.ID == topicID {
			t.Fatal("deleted topic still listed")
		}
	}
	if d, _ := s.GetTopic(testCtx(), topicID); d != nil {
		t.Fatal("deleted topic still returned by GetTopic")
	}
	// 软删除行仍存在（deleted_at 已置位）。
	var del sql.NullInt64
	if err := s.db.QueryRow("SELECT deleted_at FROM notification_topic WHERE id=?", topicID).Scan(&del); err != nil {
		t.Fatal(err)
	}
	if !del.Valid {
		t.Fatal("deleted_at not set on soft delete")
	}
}

func TestRetention(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	defer s.Close(5 * time.Second)

	now := nowMillis()
	old := now - 40*24*60*60*1000

	// 旧的 operation topic（kind=operation，永不因 closed 清理，仅验证独立保留）。
	oldOpTopic := mustCreateOperationTopic(t, s, "oldop", old)

	// 造超过 200 条的 execution 目录（新的关闭 topic，created_at 现在，不受 30 天影响）。
	rel := oldOpTopic
	for i := 0; i < 3; i++ {
		rel = mustCreateOperationTopic(t, s, fmt.Sprintf("fill-%d", i), now)
	}
	baseCnt, _ := countExecutions(s)
	fillAdditional(t, s, 250-baseCnt+10, now) // 确保总执行数 > 200

	execTotal, _ := countExecutions(s)
	if execTotal <= 200 {
		t.Fatalf("setup: execution total %d not > 200", execTotal)
	}

	// 关闭一个 Topic（当前时间，不应被 30 天清理）。
	closedNow := mustCreateOperationTopic(t, s, "closed-now", now)
	if err := s.CloseTopic(testCtx(), closedNow, now); err != nil {
		t.Fatal(err)
	}
	// 老的 closed Topic（40 天前）。
	oldTopic := mustCreateOperationTopic(t, s, "old-closed", old)
	if err := s.CloseTopic(testCtx(), oldTopic, old); err != nil {
		t.Fatal(err)
	}
	mustAppend(t, s, oldTopic, "info", "old", SrcTypeSystem)
	mustAppend(t, s, closedNow, "info", "x", SrcTypeSystem)

	if err := s.RunRetention(testCtx(), now); err != nil {
		t.Fatal(err)
	}

	// 30 天前 closed 的 Topic 被清理。
	if d, _ := s.GetTopic(testCtx(), oldTopic); d != nil {
		t.Fatal("old closed topic should be deleted by retention")
	}
	// 当前关闭的 Topic 保留。
	if d, _ := s.GetTopic(testCtx(), closedNow); d == nil {
		t.Fatal("recently closed topic should be retained")
	}
	// 执行记录被截断到 200 以内。
	if total, _ := countExecutions(s); total > 200 {
		t.Errorf("executions after retention = %d, want <= 200", total)
	}
	// 删除 execution 不删关联 topic。
	if _, err := s.GetTopic(testCtx(), rel); err != nil {
		t.Errorf("topic should be independent of execution retention: %v", err)
	}
}

func fillAdditional(t *testing.T, s *Store, count int, ts int64) {
	t.Helper()
	for i := 0; i < count; i++ {
		if _, err := s.CreateExecution(testCtx(), CreateExecutionInput{
			ExecutionID: uuid.New().String(), OperationID: "x", OperationLabel: "x", CreatedAt: ts,
			Runs: []PlannedRun{{RunID: uuid.New().String(), Phase: "global", ExtensionName: "e", ActionID: "a"}},
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func countExecutions(s *Store) (int, error) {
	var n int
	err := s.db.QueryRow("SELECT COUNT(*) FROM operation_execution").Scan(&n)
	return n, err
}

func TestWriterBoundedQueue(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	release := make(chan struct{})
	blockRun := func(db *sql.DB) error { <-release; return nil }

	// 持续入队阻塞命令直到队列满（writer 取走首命令后立即阻塞，释放一个槽位，故需多入队若干）。
	filled := false
	for i := 0; ; i++ {
		if !s.writer.tryEnqueue(&command{run: blockRun}) {
			filled = true
			break
		}
		if i > writerQueueCapacity+8 {
			t.Fatal("queue never reached capacity")
		}
	}
	if !filled {
		t.Fatal("queue never reported full")
	}
	// 队列满后继续非阻塞入队 → false 且丢弃计数递增。
	if s.writer.tryEnqueue(&command{run: blockRun}) {
		t.Fatal("queue should be full, tryEnqueue must return false")
	}
	if s.writer.droppedCount() == 0 {
		t.Fatal("dropped count not incremented")
	}
	close(release) // 让 writer 排空
	s.Close(5 * time.Second)
}

func TestOrderedCloseVsNotify(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	defer s.Close(5 * time.Second)
	topicID := mustCreateOperationTopic(t, s, "op", nowMillis())

	const total = 100
	for i := 0; i < total; i++ {
		if !s.TryAppendNotification(NotificationInput{TopicID: topicID, Level: "info", Content: "m", SourceType: SrcTypeService}) {
			t.Fatal("notify dropped unexpectedly")
		}
	}
	// 关闭命令作为普通命令排队，先到的通知先执行。
	if err := s.CloseTopic(testCtx(), topicID, nowMillis()); err != nil {
		t.Fatal(err)
	}

	d, err := s.GetTopic(testCtx(), topicID)
	if err != nil || d == nil {
		t.Fatalf("GetTopic: %v", err)
	}
	if d.ClosedAt == nil {
		t.Fatal("topic should be closed after close command processed")
	}
	if d.NotificationCount != total || d.LastSeq != total {
		t.Errorf("after close: count=%d last_seq=%d, want %d/%d", d.NotificationCount, d.LastSeq, total, total)
	}
}

func TestRestartInterruptRecovery(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	defer s.Close(5 * time.Second)
	_ = mustCreateOperationTopic(t, s, "op", nowMillis()) // run state pending

	unfinished, err := s.ListUnfinishedExecutions(testCtx())
	if err != nil {
		t.Fatal(err)
	}
	if len(unfinished) != 1 {
		t.Fatalf("unfinished executions = %d, want 1", len(unfinished))
	}
	execID := unfinished[0].ID

	// 重启：加入一个已完成的 execution（不应被中断）。
	doneID := uuid.New().String()
	if _, err := s.CreateExecution(testCtx(), CreateExecutionInput{
		ExecutionID: doneID, OperationID: "done", OperationLabel: "done", CreatedAt: nowMillis(),
		Runs: []PlannedRun{{RunID: uuid.New().String(), Phase: "global", ExtensionName: "e", ActionID: "a"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishExecution(testCtx(), doneID, nowMillis()); err != nil {
		t.Fatal(err)
	}

	// 模拟重启流程：对每条未完成 execution 执行 InterruptExecution。
	unfinished2, _ := s.ListUnfinishedExecutions(testCtx())
	for _, e := range unfinished2 {
		if err := s.InterruptExecution(testCtx(), e.ID, nowMillis()); err != nil {
			t.Fatal(err)
		}
	}

	det, err := s.GetExecution(testCtx(), execID)
	if err != nil {
		t.Fatal(err)
	}
	if det.InterruptedAt == nil {
		t.Fatal("execution should be marked interrupted")
	}
	// run 状态保留原值（pending 不变）。
	for _, r := range det.Runs {
		if r.State != StatePending {
			t.Errorf("run %s state = %s, want pending preserved", r.RunID, r.State)
		}
	}
	// 已完成的 execution 不受影响。
	if det2, _ := s.GetExecution(testCtx(), doneID); det2.InterruptedAt != nil {
		t.Error("finished execution should not be marked interrupted")
	}
}

func TestStoreErrorObservable(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	topicID := mustCreateOperationTopic(t, s, "op", nowMillis())

	// 强制写失败：关闭底层连接。
	if err := s.db.Close(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < consecutiveFailThreshold; i++ {
		_ = s.AppendNotification(testCtx(), NotificationInput{TopicID: topicID, Level: "info", Content: "x", SourceType: SrcTypeSystem})
	}
	if se := s.StoreError(); se == nil || se.Code != "store_unavailable" {
		t.Fatalf("StoreError = %+v, want store_unavailable", se)
	}
}

func TestListTopicsOrdering(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	defer s.Close(5 * time.Second)

	base := time.Now().Add(-1 * time.Minute).UnixMilli()
	// 无通知的 Topic：先建（created_at 更早），应排在最后（无通知用 topic.created_at）。
	noNotify := mustCreateOperationTopic(t, s, "opEmpty", base-1000)
	// topicA/topicC：各自追加通知，topicC 先完成，A 最后追加（最近活动最晚）。
	topicA := mustCreateOperationTopic(t, s, "opA", base)
	time.Sleep(5 * time.Millisecond)
	topicC := mustCreateOperationTopic(t, s, "opC", base+50)
	time.Sleep(5 * time.Millisecond)
	mustAppend(t, s, topicC, "info", "C1", SrcTypeService)
	mustAppend(t, s, topicC, "info", "C2", SrcTypeService)
	time.Sleep(5 * time.Millisecond)
	mustAppend(t, s, topicA, "info", "A-latest", SrcTypeService) // A 最近通知最晚

	list, err := s.ListTopics(testCtx(), TopicFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("got %d topics, want 3", len(list))
	}
	// A 的最近通知最晚 → first；C 次之；无通知的 opEmpty（created_at 最早）→ last。
	if list[0].ID != topicA {
		t.Errorf("first topic = %s, want A", list[0].ID)
	}
	if list[len(list)-1].ID != noNotify {
		t.Errorf("last topic = %s, want no-notify opEmpty", list[len(list)-1].ID)
	}
}

// TestUUIDv7PrimaryKeys 验证持久化主键（operation topic/notification/默认 Topic）为 UUIDv7
// （设计稿 §七.2：id TEXT PRIMARY KEY -- UUIDv7）。
func TestUUIDv7PrimaryKeys(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	defer s.Close(5 * time.Second)

	execID := uuid.Must(uuid.NewV7()).String()
	topicID, err := s.CreateExecution(testCtx(), CreateExecutionInput{
		ExecutionID:    execID,
		OperationID:    "op",
		OperationLabel: "op",
		CreatedAt:      nowMillis(),
		Runs: []PlannedRun{
			{RunID: uuid.New().String(), Phase: "global", ExtensionName: "g-ext", ActionID: "op"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if v := uuid.MustParse(topicID).Version(); v != 7 {
		t.Errorf("operation topic id version = %d, want 7", v)
	}
	if v := uuid.MustParse(execID).Version(); v != 7 {
		t.Errorf("execution id version = %d, want 7", v)
	}

	// 追加一条通知（同步，等待落库），断言 notification.id 为 v7。
	if err := s.AppendNotification(testCtx(), NotificationInput{
		TopicID: topicID, Level: "info", Content: "hello", SourceType: SrcTypeService,
	}); err != nil {
		t.Fatal(err)
	}
	var notifID string
	if err := s.db.QueryRow(`SELECT id FROM notification LIMIT 1`).Scan(&notifID); err != nil {
		t.Fatal(err)
	}
	if v := uuid.MustParse(notifID).Version(); v != 7 {
		t.Errorf("notification id version = %d, want 7", v)
	}

	// 默认 Topic（服务）首次创建 id 为 v7（异步入队，轮询等待落库）。
	svc := "svc-a"
	if !s.TryAppendToDefaultTopic(TopicKindService, "svc-a", NotificationInput{
		Level: "info", Content: "x", SourceType: SrcTypeService, ServiceName: &svc,
	}) {
		t.Fatal("TryAppendToDefaultTopic returned false")
	}
	var defTopicID string
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := s.db.QueryRow(`SELECT id FROM notification_topic WHERE kind='service' AND source_name='svc-a'`).Scan(&defTopicID)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("default topic not persisted in time: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if v := uuid.MustParse(defTopicID).Version(); v != 7 {
		t.Errorf("default topic id version = %d, want 7", v)
	}
}
