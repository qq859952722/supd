package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/supdorg/supd/internal/config"
)

// TestLongPollIntegration_ImmediateData 验证已有事件时长轮询端点立即返回。
// L-04-003: 覆盖 事件产生(Publish) → /api/events GET → 响应包含事件数据。
func TestLongPollIntegration_ImmediateData(t *testing.T) {
	cfg := &config.Config{Settings: config.Settings{AuthMode: "none"}}
	server := NewServer(cfg)
	server.eventRing = NewEventRingBuffer(200)

	// 预先发布2个事件到 ring buffer
	server.eventRing.Publish("service_started", map[string]string{"name": "svc1"})
	// 确保事件 nano 严格递增，便于后续断言
	time.Sleep(time.Millisecond)
	server.eventRing.Publish("service_ready", map[string]string{"name": "svc1"})

	// GET /api/events（since 默认从头拉取）
	req := httptest.NewRequest(http.MethodGet, "/api/events?wait=1", nil)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp LongPollResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 events, got %d (data: %+v)", len(resp.Data), resp.Data)
	}

	// 验证事件类型和顺序（旧→新）
	first := resp.Data[0].(map[string]any)
	if first["type"] != "service_started" {
		t.Errorf("expected first event type=service_started, got %v", first["type"])
	}
	second := resp.Data[1].(map[string]any)
	if second["type"] != "service_ready" {
		t.Errorf("expected second event type=service_ready, got %v", second["type"])
	}

	// next_since 必须非空，供客户端增量拉取使用
	if resp.NextSince == "" {
		t.Error("expected non-empty next_since in response")
	}
}

// TestLongPollIntegration_WaitThenNotify 验证无数据时阻塞等待，事件发布后立即返回。
// L-04-003: 覆盖 空ring → 长轮询阻塞 → 异步Publish → waitNotify触发 → 响应携带新事件。
func TestLongPollIntegration_WaitThenNotify(t *testing.T) {
	cfg := &config.Config{Settings: config.Settings{AuthMode: "none"}}
	server := NewServer(cfg)
	server.eventRing = NewEventRingBuffer(200)

	// 使用 wait=10 秒，足够让异步 goroutine 发布事件；waitNotify 应使其提前返回
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// 等长轮询请求进入等待状态后再发布事件
		time.Sleep(200 * time.Millisecond)
		server.eventRing.Publish("service_died", map[string]string{"name": "svc2"})
	}()

	start := time.Now()
	req := httptest.NewRequest(http.MethodGet, "/api/events?wait=10", nil)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)
	elapsed := time.Since(start)

	wg.Wait()

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp LongPollResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 event after wait, got %d (data: %+v)", len(resp.Data), resp.Data)
	}
	ev := resp.Data[0].(map[string]any)
	if ev["type"] != "service_died" {
		t.Errorf("expected event type=service_died, got %v", ev["type"])
	}

	// 应远小于 wait=10s（waitNotify 触发后约立即返回），最多 ~1s 含 200ms 等待 + 余量
	if elapsed > 3*time.Second {
		t.Errorf("expected fast return via waitNotify (<3s), got %v", elapsed)
	}
}

// TestLongPollIntegration_SinceIncremental 验证 since 参数实现增量拉取。
// L-04-003: 覆盖 第一次拉取获取nextSince → 用nextSince作为since再拉取 → 只返回新事件。
func TestLongPollIntegration_SinceIncremental(t *testing.T) {
	cfg := &config.Config{Settings: config.Settings{AuthMode: "none"}}
	server := NewServer(cfg)
	server.eventRing = NewEventRingBuffer(200)

	// 发布第一批事件
	server.eventRing.Publish("extension_started", map[string]string{"name": "ext1"})
	time.Sleep(time.Millisecond)
	server.eventRing.Publish("extension_completed", map[string]string{"name": "ext1"})

	// 第一次拉取所有事件
	req1 := httptest.NewRequest(http.MethodGet, "/api/events?wait=1", nil)
	w1 := httptest.NewRecorder()
	server.router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", w1.Code)
	}
	var resp1 LongPollResponse
	if err := json.Unmarshal(w1.Body.Bytes(), &resp1); err != nil {
		t.Fatalf("unmarshal first response: %v", err)
	}
	if len(resp1.Data) != 2 {
		t.Fatalf("first request: expected 2 events, got %d", len(resp1.Data))
	}
	if resp1.NextSince == "" {
		t.Fatal("first request: expected non-empty next_since")
	}

	// 发布第二批事件
	time.Sleep(time.Millisecond)
	server.eventRing.Publish("extension_failed", map[string]string{"name": "ext2"})

	// 第二次拉取，使用第一次返回的 next_since 作为 since 参数
	req2 := httptest.NewRequest(http.MethodGet, "/api/events?wait=1&since="+resp1.NextSince, nil)
	w2 := httptest.NewRecorder()
	server.router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("second request: expected 200, got %d (body: %s)", w2.Code, w2.Body.String())
	}
	var resp2 LongPollResponse
	if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("unmarshal second response: %v", err)
	}

	// 增量拉取：应只返回第二批事件（extension_failed）
	if len(resp2.Data) != 1 {
		t.Fatalf("second request: expected 1 event (incremental), got %d (data: %+v)", len(resp2.Data), resp2.Data)
	}
	ev := resp2.Data[0].(map[string]any)
	if ev["type"] != "extension_failed" {
		t.Errorf("second request: expected event type=extension_failed, got %v", ev["type"])
	}
}

// TestLongPollIntegration_TimeoutNoData 验证无数据且无事件时按wait超时返回空data。
// L-04-003: 覆盖 空ring → wait=1秒 → 超时返回空data与next_since。
func TestLongPollIntegration_TimeoutNoData(t *testing.T) {
	cfg := &config.Config{Settings: config.Settings{AuthMode: "none"}}
	server := NewServer(cfg)
	server.eventRing = NewEventRingBuffer(200)

	start := time.Now()
	req := httptest.NewRequest(http.MethodGet, "/api/events?wait=1", nil)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)
	elapsed := time.Since(start)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp LongPollResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	// 无数据：data 应为空
	if len(resp.Data) != 0 {
		t.Errorf("expected 0 events on timeout, got %d", len(resp.Data))
	}
	// next_since 应非空（即使无事件也返回当前时间）
	if resp.NextSince == "" {
		t.Error("expected non-empty next_since on timeout")
	}

	// 应等待约 1 秒（wait 参数），允许 200ms 误差
	if elapsed < 800*time.Millisecond {
		t.Errorf("expected to wait ~1s, only waited %v", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Errorf("waited too long: %v (expected ~1s)", elapsed)
	}
}
