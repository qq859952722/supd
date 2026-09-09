package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/supdorg/supd/internal/notification"
	"github.com/supdorg/supd/internal/store"
)

// newNotifTestServer 构造带 NotificationProvider 的 API Server（真 SQLite 临时库），
// 并用 NotifyRouter 落库预置数据。
func newNotifTestServer(t *testing.T) (*Server, *store.Store, *notification.NotifyRouter) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close(5 * time.Second) })
	router := notification.NewRouter(st)
	srv := NewServer(nil)
	srv.SetNotificationProvider(NewCoreNotificationProvider(st))
	return srv, st, router
}

// seedServiceNotify 通过路由向某服务默认 Topic 落库一条通知。
func seedServiceNotify(r *notification.NotifyRouter, service, content string) {
	r.TryEnqueue(notification.PendingNotification{Level: notification.NotifyInfo, Content: content, ServiceName: service})
}

// pollCond 轮询直到 cond 为 true。
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

func doNotifReq(srv *Server, method, path, body string) *httptest.ResponseRecorder {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, r)
	return w
}

func TestNotifTopicsListAndFilters(t *testing.T) {
	srv, st, router := newNotifTestServer(t)
	seedServiceNotify(router, "web", "web-notify-1")
	seedServiceNotify(router, "db", "db-notify-1")
	pollCond(t, "topics persisted", 3*time.Second, func() bool {
		l, _ := st.ListTopics(context.Background(), store.TopicFilter{})
		return len(l) >= 2
	})

	resp := doNotifReq(srv, "GET", "/api/notifications/topics", "")
	if resp.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", resp.Code, resp.Body.String())
	}
	var body topicsResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.StoreError != nil {
		t.Errorf("store_error should be null, got %+v", body.StoreError)
	}
	if len(body.Topics) != 2 {
		t.Fatalf("got %d topics, want 2", len(body.Topics))
	}
	// 按 service_name 筛选。
	resp2 := doNotifReq(srv, "GET", "/api/notifications/topics?service_name=web", "")
	var b2 topicsResponse
	_ = json.Unmarshal(resp2.Body.Bytes(), &b2)
	if len(b2.Topics) != 1 || b2.Topics[0].SourceName != "web" {
		t.Errorf("service filter wrong: %+v", b2.Topics)
	}
	// 未读筛选：都未读。
	resp3 := doNotifReq(srv, "GET", "/api/notifications/topics?unread=true", "")
	var b3 topicsResponse
	_ = json.Unmarshal(resp3.Body.Bytes(), &b3)
	if len(b3.Topics) != 2 {
		t.Errorf("unread filter wrong: %+v", b3.Topics)
	}
}

func TestNotifTopicDetailPaging(t *testing.T) {
	srv, st, router := newNotifTestServer(t)
	for i := 0; i < 5; i++ {
		seedServiceNotify(router, "srv", "m")
	}
	var id string
	pollCond(t, "topic persisted", 3*time.Second, func() bool {
		l, _ := st.ListTopics(context.Background(), store.TopicFilter{})
		if len(l) == 0 {
			return false
		}
		id = l[0].ID
		return true
	})

	resp := doNotifReq(srv, "GET", "/api/notifications/topics/"+id, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("detail status = %d body=%s", resp.Code, resp.Body.String())
	}
	var body topicDetailResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Topic == nil || len(body.Notifications) != 5 {
		t.Fatalf("detail wrong: %+v", body)
	}
	if body.Notifications[0].Seq != 1 || body.Notifications[4].Seq != 5 {
		t.Errorf("notifications not seq-ascending: %+v", body.Notifications)
	}
	if body.NextSeq != 5 || body.HasMore {
		t.Errorf("next_seq=%d has_more=%v, want 5/false", body.NextSeq, body.HasMore)
	}
	// 分页：since=2 limit=2。
	resp2 := doNotifReq(srv, "GET", "/api/notifications/topics/"+id+"?seq=2&limit=2", "")
	var b2 topicDetailResponse
	_ = json.Unmarshal(resp2.Body.Bytes(), &b2)
	if len(b2.Notifications) != 2 || b2.Notifications[0].Seq != 3 {
		t.Errorf("paging seq wrong: %+v", b2.Notifications)
	}

	// 未知 topic 404。
	resp3 := doNotifReq(srv, "GET", "/api/notifications/topics/nope", "")
	if resp3.Code != http.StatusNotFound {
		t.Errorf("unknown topic status = %d, want 404", resp3.Code)
	}
}

func TestNotifReadCursorEndpoints(t *testing.T) {
	srv, st, router := newNotifTestServer(t)
	seedServiceNotify(router, "web", "a")
	seedServiceNotify(router, "web", "b")
	var id string
	pollCond(t, "topic persisted", 3*time.Second, func() bool {
		l, _ := st.ListTopics(context.Background(), store.TopicFilter{Kind: "service"})
		if len(l) == 0 {
			return false
		}
		id = l[0].ID
		return true
	})

	// 单 Topic 已读。
	resp := doNotifReq(srv, "POST", "/api/notifications/topics/"+id+"/read", "")
	if resp.Code != http.StatusOK {
		t.Fatalf("read status = %d body=%s", resp.Code, resp.Body.String())
	}
	var rr map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &rr)
	if rr["read_seq"] != float64(2) {
		t.Errorf("read_seq = %v, want 2", rr["read_seq"])
	}
	d, _ := st.GetTopic(context.Background(), id)
	if d.UnreadCount != 0 {
		t.Errorf("unread after single read = %d", d.UnreadCount)
	}

	// 新通知 → 重新未读。
	seedServiceNotify(router, "web", "c")
	pollCond(t, "seq advances", 3*time.Second, func() bool {
		d, _ := st.GetTopic(context.Background(), id)
		return d != nil && d.LastSeq == 3 && d.UnreadCount == 1
	})

	// read-all → 全部已读。
	respAll := doNotifReq(srv, "POST", "/api/notifications/read-all", "")
	if respAll.Code != http.StatusOK {
		t.Fatalf("read-all status = %d", respAll.Code)
	}
	d2, _ := st.GetTopic(context.Background(), id)
	if d2.UnreadCount != 0 {
		t.Errorf("unread after read-all = %d", d2.UnreadCount)
	}
}

func TestNotifDeleteEndpoints(t *testing.T) {
	srv, st, router := newNotifTestServer(t)
	seedServiceNotify(router, "web", "x")
	var id string
	pollCond(t, "topic persisted", 3*time.Second, func() bool {
		l, _ := st.ListTopics(context.Background(), store.TopicFilter{})
		if len(l) == 0 {
			return false
		}
		id = l[0].ID
		return true
	})

	// 单删。
	resp := doNotifReq(srv, "DELETE", "/api/notifications/topics/"+id, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", resp.Code, resp.Body.String())
	}
	l, _ := st.ListTopics(context.Background(), store.TopicFilter{})
	if len(l) != 0 {
		t.Errorf("topic not soft-deleted via API")
	}
	// 再删不存在 → 404。
	resp2 := doNotifReq(srv, "DELETE", "/api/notifications/topics/"+id, "")
	if resp2.Code != http.StatusNotFound {
		t.Errorf("re-delete status = %d, want 404", resp2.Code)
	}

	// 重建后清空。
	seedServiceNotify(router, "web", "y")
	pollCond(t, "recreated", 3*time.Second, func() bool {
		l, _ := st.ListTopics(context.Background(), store.TopicFilter{})
		return len(l) == 1
	})
	respClear := doNotifReq(srv, "DELETE", "/api/notifications/topics", "")
	if respClear.Code != http.StatusOK {
		t.Fatalf("clear status = %d", respClear.Code)
	}
	l2, _ := st.ListTopics(context.Background(), store.TopicFilter{})
	if len(l2) != 0 {
		t.Errorf("clear-all did not remove topics: %+v", l2)
	}
}

func TestNotifChangesEpochReload(t *testing.T) {
	srv, st, _ := newNotifTestServer(t)
	epoch := st.Epoch()

	// 正确 epoch + since >= 当前 → 阻塞（wait=1）后返回，reload=false。
	cur := st.GlobalSeq()
	start := time.Now()
	resp := doNotifReq(srv, "GET", "/api/notifications/changes?epoch="+epoch+"&since="+itoa(cur)+"&wait=1", "")
	if resp.Code != http.StatusOK {
		t.Fatalf("changes status = %d body=%s", resp.Code, resp.Body.String())
	}
	var body changesResponse
	_ = json.Unmarshal(resp.Body.Bytes(), &body)
	if body.Reload {
		t.Errorf("reload should be false for matching epoch, got %+v", body)
	}
	if body.Epoch != epoch {
		t.Errorf("epoch echoed = %q, want %q", body.Epoch, epoch)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Errorf("expected wait-blocking changes (waited %v)", time.Since(start))
	}

	// 错误 epoch → reload=true + 当前 epoch。
	resp2 := doNotifReq(srv, "GET", "/api/notifications/changes?epoch=wrong-epoch&since=0", "")
	if resp2.Code != http.StatusOK {
		t.Fatalf("changes status = %d", resp2.Code)
	}
	var b2 changesResponse
	_ = json.Unmarshal(resp2.Body.Bytes(), &b2)
	if !b2.Reload || b2.Epoch != epoch {
		t.Errorf("wrong epoch should trigger reload, got %+v", b2)
	}
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}

func TestNotifChangesSignalOnWrite(t *testing.T) {
	srv, st, router := newNotifTestServer(t)
	epoch := st.Epoch()
	since := st.GlobalSeq()

	// 在一个 goroutine 触发写（推进 GlobalSeq），changes 应立即返回新 seq。
	done := make(chan struct{})
	go func() {
		time.Sleep(200 * time.Millisecond)
		seedServiceNotify(router, "sig", "wake")
		close(done)
	}()
	resp := doNotifReq(srv, "GET", "/api/notifications/changes?epoch="+epoch+"&since="+itoa(since)+"&wait=5", "")
	<-done
	if resp.Code != http.StatusOK {
		t.Fatalf("changes status = %d", resp.Code)
	}
	var body changesResponse
	_ = json.Unmarshal(resp.Body.Bytes(), &body)
	if body.Reload {
		t.Error("reload should be false")
	}
	if body.Seq <= since {
		t.Errorf("changes seq = %d, want > %d on new write", body.Seq, since)
	}
}

func TestNotifChangesRateLimit(t *testing.T) {
	// 单客户端第 6 个并发 changes → 429（复用 longPollLimiter 单客户端 5）。
	srv, st, _ := newNotifTestServer(t)
	epoch := st.Epoch()

	// 用 wait=2 挂住 5 个请求。
	max := 6
	ch := make(chan *httptest.ResponseRecorder, max)
	for i := 0; i < max; i++ {
		// 每个请求必须来自同一客户端 IP → 使用相同 RemoteAddr。
		r := httptest.NewRequest("GET", "/api/notifications/changes?epoch="+epoch+"&since="+itoa(st.GlobalSeq())+"&wait=2", nil)
		r.RemoteAddr = "10.0.0.1:12345"
		go func() {
			w := httptest.NewRecorder()
			srv.Router().ServeHTTP(w, r)
			ch <- w
		}()
	}
	var statuses []int
	for i := 0; i < max; i++ {
		w := <-ch
		statuses = append(statuses, w.Code)
	}
	var okCount, rlCount int
	for _, s := range statuses {
		switch s {
		case http.StatusOK:
			okCount++
		case http.StatusTooManyRequests:
			rlCount++
		}
	}
	if okCount != 5 {
		t.Errorf("changes accepted %d (want 5), statuses=%v", okCount, statuses)
	}
	if rlCount < 1 {
		t.Errorf("expected at least one 429 rate-limited change, statuses=%v", statuses)
	}
}

func TestNotifChangesInvalidParams(t *testing.T) {
	srv, st, _ := newNotifTestServer(t)
	epoch := st.Epoch()
	resp := doNotifReq(srv, "GET", "/api/notifications/changes?epoch="+epoch+"&since=abc", "")
	if resp.Code != http.StatusBadRequest {
		t.Errorf("invalid since status = %d, want 400", resp.Code)
	}
}

func TestNotifStoreErrorField(t *testing.T) {
	// 错误态置位 → topics 顶层 store_error={code:store_unavailable}；恢复 → null。
	srv, st, router := newNotifTestServer(t)
	seedServiceNotify(router, "web", "a")
	pollCond(t, "topic exists", 3*time.Second, func() bool {
		l, _ := st.ListTopics(context.Background(), store.TopicFilter{})
		return len(l) == 1
	})

	// 正常 → store_error null。
	resp := doNotifReq(srv, "GET", "/api/notifications/topics", "")
	var body topicsResponse
	_ = json.Unmarshal(resp.Body.Bytes(), &body)
	if body.StoreError != nil {
		t.Errorf("store_error should be null, got %+v", body.StoreError)
	}

	// 强制错误态。
	for i := 0; i < 10; i++ {
		_ = st.AppendNotification(context.Background(), store.NotificationInput{
			TopicID: "no-such", Level: "info", Content: "x", SourceType: "system",
		})
	}
	pollCond(t, "store error set", 3*time.Second, func() bool {
		return st.StoreError() != nil
	})
	resp2 := doNotifReq(srv, "GET", "/api/notifications/topics", "")
	var b2 topicsResponse
	_ = json.Unmarshal(resp2.Body.Bytes(), &b2)
	if b2.StoreError == nil {
		t.Fatalf("store_error should be set in error mode")
	}
	if b2.StoreError.Code != "store_unavailable" {
		t.Errorf("store_error code = %q, want store_unavailable", b2.StoreError.Code)
	}
	// 响应不得泄露路径/SQL/堆栈文本。
	if strings.Contains(resp2.Body.String(), "supd.db") || strings.Contains(resp2.Body.String(), "topics/") || strings.Contains(resp2.Body.String(), "no-such") {
		t.Errorf("response leaks internal detail: %s", resp2.Body.String())
	}

	// 恢复 → store_error null。
	seedServiceNotify(router, "web", "recover")
	pollCond(t, "store error cleared", 3*time.Second, func() bool {
		return st.StoreError() == nil
	})
	resp3 := doNotifReq(srv, "GET", "/api/notifications/topics", "")
	var b3 topicsResponse
	_ = json.Unmarshal(resp3.Body.Bytes(), &b3)
	if b3.StoreError != nil {
		t.Errorf("store_error should be null after recovery, got %+v", b3.StoreError)
	}
}