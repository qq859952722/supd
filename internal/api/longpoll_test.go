package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestParseLongPollRequest_Defaults(t *testing.T) {
	refWall := time.Now()
	r := httptest.NewRequest("GET", "/api/events", nil)
	req, err := ParseLongPollRequest(r, refWall)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Wait != 30 {
		t.Errorf("expected Wait=30, got %d", req.Wait)
	}
	if req.Limit != 500 {
		t.Errorf("expected Limit=500, got %d", req.Limit)
	}
	if req.SinceNano != 0 {
		t.Errorf("expected SinceNano=0, got %d", req.SinceNano)
	}
}

func TestParseLongPollRequest_CustomWait(t *testing.T) {
	refWall := time.Now()
	r := httptest.NewRequest("GET", "/api/events?wait=45", nil)
	req, err := ParseLongPollRequest(r, refWall)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Wait != 45 {
		t.Errorf("expected Wait=45, got %d", req.Wait)
	}
}

func TestParseLongPollRequest_WaitTooLarge(t *testing.T) {
	refWall := time.Now()
	r := httptest.NewRequest("GET", "/api/events?wait=120", nil)
	req, err := ParseLongPollRequest(r, refWall)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Wait != 60 {
		t.Errorf("expected Wait=60 (capped), got %d", req.Wait)
	}
}

func TestParseLongPollRequest_InvalidWait(t *testing.T) {
	refWall := time.Now()
	r := httptest.NewRequest("GET", "/api/events?wait=abc", nil)
	_, err := ParseLongPollRequest(r, refWall)
	if err == nil {
		t.Fatal("expected error for invalid wait parameter")
	}
}

func TestParseLongPollRequest_InvalidLimit(t *testing.T) {
	refWall := time.Now()
	r := httptest.NewRequest("GET", "/api/events?limit=xyz", nil)
	_, err := ParseLongPollRequest(r, refWall)
	if err == nil {
		t.Fatal("expected error for invalid limit parameter")
	}
}

func TestParseLongPollRequest_SinceISO8601(t *testing.T) {
	refWall := time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)
	sinceTime := refWall.Add(5 * time.Second)
	sinceStr := sinceTime.Format(time.RFC3339Nano)

	r := httptest.NewRequest("GET", "/api/events?since="+sinceStr, nil)
	req, err := ParseLongPollRequest(r, refWall)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedNano := int64(5 * time.Second)
	// 允许纳秒精度误差（ISO8601格式化/解析可能损失精度）
	delta := req.SinceNano - expectedNano
	if delta < -1000 || delta > 1000 {
		t.Errorf("expected SinceNano≈%d, got %d (delta=%d)", expectedNano, req.SinceNano, delta)
	}
}

func TestEventRingBuffer_AddAndGet(t *testing.T) {
	buf := NewEventRingBuffer(200)

	buf.Add("service_started", map[string]string{"name": "svc1"})
	buf.Add("service_stopped", map[string]string{"name": "svc2"})

	events, hasMore := buf.Since(0, 500)
	if hasMore {
		t.Error("expected hasMore=false")
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Type != "service_started" {
		t.Errorf("expected type=service_started, got %s", events[0].Type)
	}
	if events[1].Type != "service_stopped" {
		t.Errorf("expected type=service_stopped, got %s", events[1].Type)
	}
}

func TestEventRingBuffer_Capacity(t *testing.T) {
	buf := NewEventRingBuffer(5) // 小容量用于测试

	// 添加7个事件，超过容量5
	for i := 0; i < 7; i++ {
		buf.Add("event", map[string]int{"i": i})
	}

	// 应该只保留最后5个事件（i=2,3,4,5,6）
	events, _ := buf.Since(0, 500)
	if len(events) != 5 {
		t.Fatalf("expected 5 events (capacity), got %d", len(events))
	}

	// 验证保留的是最新的5个（i=2到6）
	for i, ev := range events {
		m, ok := ev.Payload.(map[string]int)
		if !ok {
			t.Fatalf("event %d: unexpected payload type", i)
		}
		expectedI := i + 2
		if m["i"] != expectedI {
			t.Errorf("event %d: expected i=%d, got i=%d", i, expectedI, m["i"])
		}
	}
}

func TestEventRingBuffer_Since(t *testing.T) {
	buf := NewEventRingBuffer(200)

	buf.Add("event_1", nil)
	time.Sleep(time.Millisecond) // 确保时间递增
	buf.Add("event_2", nil)
	time.Sleep(time.Millisecond)
	buf.Add("event_3", nil)

	// 获取所有事件
	all, _ := buf.Since(0, 500)
	if len(all) != 3 {
		t.Fatalf("expected 3 events, got %d", len(all))
	}

	// 获取第一个事件之后的事件
	sinceFirst := all[0].Nano
	afterFirst, _ := buf.Since(sinceFirst, 500)
	if len(afterFirst) != 2 {
		t.Errorf("expected 2 events after first, got %d", len(afterFirst))
	}

	// 获取第二个事件之后的事件
	sinceSecond := all[1].Nano
	afterSecond, _ := buf.Since(sinceSecond, 500)
	if len(afterSecond) != 1 {
		t.Errorf("expected 1 event after second, got %d", len(afterSecond))
	}

	// 获取最后一个事件之后的事件（应无结果）
	sinceLast := all[2].Nano
	afterLast, hasMore := buf.Since(sinceLast, 500)
	if len(afterLast) != 0 {
		t.Errorf("expected 0 events after last, got %d", len(afterLast))
	}
	if hasMore {
		t.Error("expected hasMore=false for no events")
	}
}

func TestEventRingBuffer_HasMore(t *testing.T) {
	buf := NewEventRingBuffer(200)

	// 添加10个事件
	for i := 0; i < 10; i++ {
		buf.Add("event", map[string]int{"i": i})
		time.Sleep(time.Millisecond) // 确保时间递增
	}

	// limit=5，应该返回5个事件并has_more=true
	events, hasMore := buf.Since(0, 5)
	if len(events) != 5 {
		t.Fatalf("expected 5 events, got %d", len(events))
	}
	if !hasMore {
		t.Error("expected hasMore=true when limit < total")
	}

	// limit=10，应该返回10个事件并has_more=false
	events, hasMore = buf.Since(0, 10)
	if len(events) != 10 {
		t.Fatalf("expected 10 events, got %d", len(events))
	}
	if hasMore {
		t.Error("expected hasMore=false when limit >= total")
	}

	// limit=20，应该返回10个事件并has_more=false
	events, hasMore = buf.Since(0, 20)
	if len(events) != 10 {
		t.Fatalf("expected 10 events, got %d", len(events))
	}
	if hasMore {
		t.Error("expected hasMore=false when limit > total")
	}
}

func TestEventRingBuffer_Concurrent(t *testing.T) {
	buf := NewEventRingBuffer(200)
	var wg sync.WaitGroup

	// 并发写入
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			buf.Add("concurrent", map[string]int{"n": n})
		}(i)
	}

	// 并发读取
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf.Since(0, 500)
		}()
	}

	wg.Wait()

	// 验证最终状态一致
	events, _ := buf.Since(0, 500)
	if len(events) > 200 {
		t.Errorf("expected at most 200 events, got %d", len(events))
	}
	if len(events) < 100 {
		// 由于并发写入，可能有些被覆盖，但不应少于100太多
		// 实际上所有100个写入都在wg.Wait()前完成，200容量足够
		t.Errorf("expected at least 100 events, got %d", len(events))
	}
}

func TestLongPollFormatSince(t *testing.T) {
	refWall := time.Date(2026, 7, 8, 10, 30, 0, 0, time.UTC)

	// nano=0 → 应返回refWall的ISO8601
	result := formatSince(0, refWall)
	expected := "2026-07-08T10:30:00Z"
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}

	// nano=5秒 → refWall + 5秒
	result = formatSince(int64(5*time.Second), refWall)
	expected = "2026-07-08T10:30:05Z"
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}

	// nano=1小时 → refWall + 1小时
	result = formatSince(int64(time.Hour), refWall)
	expected = "2026-07-08T11:30:00Z"
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}

	// 带时区的refWall
	loc := time.FixedZone("CST", 8*3600)
	refWallCST := time.Date(2026, 7, 8, 18, 30, 0, 0, loc)
	result = formatSince(0, refWallCST)
	expected = "2026-07-08T18:30:00+08:00"
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestEventRingBuffer_WaitForEvent(t *testing.T) {
	buf := NewEventRingBuffer(200)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 在一个事件都没有时，WaitForEvent应该阻塞
	ch := buf.WaitForEvent(ctx, 0)

	// 异步添加事件
	go func() {
		time.Sleep(50 * time.Millisecond)
		buf.Add("test_event", nil)
	}()

	// 应该在超时前收到通知
	select {
	case <-ch:
		// 成功收到事件通知
	case <-time.After(3 * time.Second):
		t.Fatal("WaitForEvent did not return after event was added")
	}

	// 验证事件确实存在
	events, _ := buf.Since(0, 500)
	if len(events) != 1 {
		t.Errorf("expected 1 event, got %d", len(events))
	}
}

func TestEventRingBuffer_WaitForEvent_Timeout(t *testing.T) {
	buf := NewEventRingBuffer(200)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	ch := buf.WaitForEvent(ctx, 0)

	// 不添加事件，等待超时
	select {
	case <-ch:
		// 超时返回
	case <-time.After(3 * time.Second):
		t.Fatal("WaitForEvent did not return after context timeout")
	}
}

func TestEventRingBuffer_WaitForEvent_AlreadyExists(t *testing.T) {
	buf := NewEventRingBuffer(200)

	buf.Add("existing_event", nil)
	time.Sleep(time.Millisecond)

	// 事件已存在，WaitForEvent应该立即返回
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events, _ := buf.Since(0, 500)
	if len(events) == 0 {
		t.Fatal("expected at least 1 event")
	}

	ch := buf.WaitForEvent(ctx, 0)

	select {
	case <-ch:
		// 立即返回，因为已有事件
	case <-time.After(1 * time.Second):
		t.Fatal("WaitForEvent did not return immediately when event already exists")
	}
}

func TestEventRingBuffer_WallToMono(t *testing.T) {
	buf := NewEventRingBuffer(200)

	// refWall之后5秒的时间
	future := buf.refWall.Add(5 * time.Second)
	nano := buf.WallToMono(future)

	expected := int64(5 * time.Second)
	delta := nano - expected
	if delta < -1000 || delta > 1000 {
		t.Errorf("expected WallToMono≈%d, got %d (delta=%d)", expected, nano, delta)
	}
}

func TestEventRingBuffer_MonoToWall(t *testing.T) {
	buf := NewEventRingBuffer(200)

	// 5秒的单调纳秒 → 应该得到refWall + 5秒
	nano := int64(5 * time.Second)
	wall := buf.MonoToWall(nano)

	expected := buf.refWall.Add(5 * time.Second)
	if !wall.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, wall)
	}
}

func TestHandleLongPoll_ImmediateData(t *testing.T) {
	refWall := time.Now()

	fetcher := func(sinceNano int64, limit int) ([]any, int64, bool, error) {
		return []any{"event1", "event2"}, int64(5 * time.Second), false, nil
	}

	r := httptest.NewRequest("GET", "/api/events", nil)
	w := httptest.NewRecorder()

	HandleLongPoll(w, r, refWall, fetcher, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	// 验证响应包含数据
	body := w.Body.String()
	if len(body) == 0 {
		t.Fatal("expected non-empty response body")
	}
}

func TestHandleLongPoll_TimeoutNoData(t *testing.T) {
	refWall := time.Now()

	fetcher := func(sinceNano int64, limit int) ([]any, int64, bool, error) {
		return nil, 0, false, nil
	}

	r := httptest.NewRequest("GET", "/api/events?wait=1", nil)
	w := httptest.NewRecorder()

	start := time.Now()
	HandleLongPoll(w, r, refWall, fetcher, nil)
	elapsed := time.Since(start)

	// 应该等待约1秒（wait参数）
	if elapsed < 800*time.Millisecond {
		t.Errorf("expected to wait ~1s, only waited %v", elapsed)
	}

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
}

func TestHandleLongPoll_InvalidParams(t *testing.T) {
	refWall := time.Now()

	r := httptest.NewRequest("GET", "/api/events?wait=abc", nil)
	w := httptest.NewRecorder()

	HandleLongPoll(w, r, refWall, nil, nil)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

// TestLongPollLimiter_PerClientBoundary5 测试单客户端并发边界：5个接受，第6个拒绝（503）
// L-02-004: 验证规格要求的全局50/单客户端5并发限制边界
func TestLongPollLimiter_PerClientBoundary5(t *testing.T) {
	limiter := NewLongPollLimiter(50, 5) // 规格 §1.2: 全局50/单客户端5

	ready := make(chan struct{}, 5)
	block := make(chan struct{})
	handler := limiter.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ready <- struct{}{}
		<-block
	}))

	var wg sync.WaitGroup
	// 同一客户端发起 5 个请求（恰好达到边界，应全部接受）
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
			req.RemoteAddr = "1.2.3.4:1234"
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
		}()
	}

	// 等待 5 个请求全部进入处理
	for i := 0; i < 5; i++ {
		select {
		case <-ready:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for 5 concurrent requests to start")
		}
	}

	// 同一客户端第 6 个请求应被拒绝（503 SERVICE_BUSY）
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("per-client boundary: 6th request expected 503, got %d", rr.Code)
	}

	close(block)
	wg.Wait()
}

// TestLongPollLimiter_GlobalBoundary50 测试全局并发边界：50个接受，第51个拒绝（503）
// L-02-004: 验证规格要求的全局50/单客户端5并发限制边界
func TestLongPollLimiter_GlobalBoundary50(t *testing.T) {
	limiter := NewLongPollLimiter(50, 5) // 规格 §1.2: 全局50/单客户端5

	ready := make(chan struct{}, 50)
	block := make(chan struct{})
	handler := limiter.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ready <- struct{}{}
		<-block
	}))

	var wg sync.WaitGroup
	// 50 个不同客户端各发 1 个请求（恰好达到全局边界，应全部接受）
	for i := 1; i <= 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
			req.RemoteAddr = fmt.Sprintf("10.0.0.%d:1234", idx)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
		}(i)
	}

	// 等待 50 个请求全部进入处理
	for i := 0; i < 50; i++ {
		select {
		case <-ready:
		case <-time.After(10 * time.Second):
			t.Fatal("timeout waiting for 50 concurrent requests to start")
		}
	}

	// 第 51 个请求应被拒绝（503 SERVICE_BUSY）
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	req.RemoteAddr = "10.0.0.51:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("global boundary: 51st request expected 503, got %d", rr.Code)
	}

	close(block)
	wg.Wait()
}

// TestEventRingBuffer_Boundary200 验证规格§2.9.7 环形缓冲容量200的实际边界。
// L-02-002: 现有 TestEventRingBuffer_Capacity 用 cap=5 等效测试，此处补充实际规格边界值。
func TestEventRingBuffer_Boundary200(t *testing.T) {
	tests := []struct {
		name         string
		writes       int
		wantRetained int
		wantFirst    int // 期望保留的最旧事件 i
		wantLast     int // 期望保留的最新事件 i
	}{
		{"AtCapacity_200", 200, 200, 0, 199},
		{"OverCapacity_201_EvictsOldest", 201, 200, 1, 200},
		{"OverCapacity_250_KeepsLatest200", 250, 200, 50, 249},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := NewEventRingBuffer(200) // 规格§2.9.7: capacity=200
			for i := 0; i < tt.writes; i++ {
				buf.Add("event", map[string]int{"i": i})
			}

			events, hasMore := buf.Since(0, 1000)
			if hasMore {
				t.Error("expected hasMore=false")
			}
			if len(events) != tt.wantRetained {
				t.Fatalf("expected %d retained, got %d", tt.wantRetained, len(events))
			}

			first := events[0].Payload.(map[string]int)["i"]
			last := events[len(events)-1].Payload.(map[string]int)["i"]
			if first != tt.wantFirst {
				t.Errorf("expected first i=%d, got i=%d", tt.wantFirst, first)
			}
			if last != tt.wantLast {
				t.Errorf("expected last i=%d, got i=%d", tt.wantLast, last)
			}
		})
	}
}

// TestEventRingBuffer_Boundary200_SinceTrace 验证溢出后 Since 参数可正确追溯。
// L-02-002: 写入200条后第201条覆盖最旧，Since 游标仍能正确分页。
func TestEventRingBuffer_Boundary200_SinceTrace(t *testing.T) {
	buf := NewEventRingBuffer(200)

	// 写入200条，确保 nano 严格递增以便 Since 游标准确
	for i := 0; i < 200; i++ {
		buf.Add("event", map[string]int{"i": i})
		time.Sleep(time.Microsecond)
	}

	// 捕获第101条事件（i=100）的 nano 作为游标
	all, _ := buf.Since(0, 1000)
	cursor := all[100].Nano

	// 写入第201条触发覆盖最旧
	buf.Add("event", map[string]int{"i": 200})
	time.Sleep(time.Microsecond)

	// Since(cursor) 应返回 nano > cursor 的事件：i=101..199（99条）+ i=200（1条）= 100条
	after, hasMore := buf.Since(cursor, 1000)
	if hasMore {
		t.Error("expected hasMore=false")
	}
	if len(after) != 100 {
		t.Fatalf("expected 100 events after cursor, got %d", len(after))
	}
	first := after[0].Payload.(map[string]int)["i"]
	if first != 101 {
		t.Errorf("expected first after cursor i=101, got i=%d", first)
	}
	last := after[len(after)-1].Payload.(map[string]int)["i"]
	if last != 200 {
		t.Errorf("expected last after cursor i=200, got i=%d", last)
	}
}
