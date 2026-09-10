package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// mustDefaultAppend 向默认 Topic（按 kind/source）追加一条通知，非阻塞等待落库。
func mustDefaultAppend(t *testing.T, s *Store, kind, source string, level string) {
	t.Helper()
	if !s.TryAppendToDefaultTopic(kind, source, NotificationInput{
		Level: level, Content: "x", SourceType: SrcTypeService,
	}) {
		t.Fatalf("TryAppendToDefaultTopic dropped (queue full)")
	}
	// 等待写入完成：等待对应默认 Topic 出现且 last_seq >= 1。
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		list, err := s.ListTopics(testCtx(), TopicFilter{Kind: kind})
		if err == nil {
			for _, it := range list {
				if it.SourceName == source && it.LastSeq >= 1 {
					return
				}
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("default topic %s/%s not written", kind, source)
}

func TestDefaultTopicCreateAndReuse(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	defer s.Close(5 * time.Second)

	svc := "web"
	mustDefaultAppend(t, s, TopicKindService, svc, "info")
	// 第二次 append 复用同一默认 Topic（同一 id）。
	if !s.TryAppendToDefaultTopic(TopicKindService, svc, NotificationInput{
		Level: "info", Content: "again", SourceType: SrcTypeService,
	}) {
		t.Fatal("second append dropped")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		list, err := s.ListTopics(testCtx(), TopicFilter{Kind: TopicKindService})
		if err == nil && len(list) == 1 && list[0].SourceName == svc {
			if list[0].LastSeq != 2 {
				time.Sleep(5 * time.Millisecond)
				continue
			}
			if list[0].ServiceName == nil || *list[0].ServiceName != svc {
				t.Fatalf("service default topic service_name = %v, want %s", list[0].ServiceName, svc)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("only one service default topic expected for %s", svc)
}

func TestDefaultTopicKindSeparate(t *testing.T) {
	// 同名服务与同名全局扩展 → 不同 (kind, source_name) 默认 Topic，不冲突（v2 迁移关键场景）。
	s := mustOpen(t, t.TempDir())
	defer s.Close(5 * time.Second)

	mustDefaultAppend(t, s, TopicKindService, "dup", "info")
	mustDefaultAppend(t, s, TopicKindExtension, "dup", "info")

	list, err := s.ListTopics(testCtx(), TopicFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d default topics, want 2 (service+extension with same source name)", len(list))
	}
	var svcTopic, extTopic bool
	for _, it := range list {
		if it.Kind == TopicKindService && it.SourceName == "dup" {
			svcTopic = true
		}
		if it.Kind == TopicKindExtension && it.SourceName == "dup" {
			extTopic = true
		}
	}
	if !svcTopic || !extTopic {
		t.Fatalf("expected both service & extension default topic for source 'dup', got %+v", list)
	}
}

func TestDeleteAllTopicsSoft(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	defer s.Close(5 * time.Second)

	mustDefaultAppend(t, s, TopicKindService, "a", "info")
	mustDefaultAppend(t, s, TopicKindService, "b", "info")

	if err := s.DeleteAllTopics(testCtx(), nowMillis()); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListTopics(testCtx(), TopicFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("after delete-all, %d topics still listed", len(list))
	}
}

func TestGetOperationTopicRoutingLookup(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	defer s.Close(5 * time.Second)

	// 无对应 execution → nil。
	got, err := s.GetOperationTopic(testCtx(), uuid.New().String())
	if err != nil || got != nil {
		t.Fatalf("nil expected for unknown execution, got %v err=%v", got, err)
	}

	// 建立 execution 并拿到 topic，按 execution_id 定位操作 Topic。
	execID := uuid.New().String()
	svc := "web"
	topID, err := s.CreateExecution(testCtx(), CreateExecutionInput{
		ExecutionID:    execID,
		OperationID:    "op",
		OperationLabel: "op",
		CreatedAt:      nowMillis(),
		Runs: []PlannedRun{
			{RunID: uuid.New().String(), Phase: "service", ServiceName: &svc, ExtensionName: "s-ext", ActionID: "op"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	topic, err := s.GetOperationTopic(testCtx(), execID)
	if err != nil || topic == nil {
		t.Fatalf("GetOperationTopic for exec %v-err, want topic", err)
	}
	if topic.ID != topID {
		t.Fatalf("operation topic id = %s, want %s", topic.ID, topID)
	}
	if topic.ClosedAt != nil {
		t.Errorf("fresh operation topic should not be closed")
	}

	// 软删除后查询返回 nil（已删除操作 Topic 不再路由）。
	if err := s.DeleteTopic(testCtx(), topID, nowMillis()); err != nil {
		t.Fatal(err)
	}
	topic2, err := s.GetOperationTopic(testCtx(), execID)
	if err != nil || topic2 != nil {
		t.Fatalf("after soft delete GetOperationTopic = %v err=%v, want nil", topic2, err)
	}
}

func TestWaitForGlobalChange(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	defer s.Close(5 * time.Second)
	topicID := mustCreateOperationTopic(t, s, "op", nowMillis())

	since := s.GlobalSeq()
	// since 已落后 → 立即返回。
	ctx := context.Background()
	select {
	case <-s.WaitForGlobalChange(ctx, since-1):
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForGlobalChange should return immediately for stale since")
	}

	// since == 当前 → 阻塞直至新写事件（追加通知推进 GlobalSeq）。
	done := make(chan struct{})
	go func() {
		<-s.WaitForGlobalChange(ctx, s.GlobalSeq())
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	mustAppend(t, s, topicID, "info", "wake", SrcTypeSystem)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("WaitForGlobalChange did not signal on new write")
	}
}
