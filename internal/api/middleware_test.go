package api

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/supdorg/supd/internal/errors"
)

// TestAuthModeNone none 模式所有请求都通过
func TestAuthModeNone(t *testing.T) {
	mw := AuthMiddleware("none", "secret", nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("none mode: expected 200, got %d", rr.Code)
	}
}

// TestAuthModeAlwaysToken always_token 模式无 token → 401 AUTH_REQUIRED
func TestAuthModeAlwaysToken(t *testing.T) {
	mw := AuthMiddleware("always_token", "secret", nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("always_token no token: expected 401, got %d", rr.Code)
	}

	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Error.Code != string(errors.ErrAuthRequired) {
		t.Errorf("expected AUTH_REQUIRED, got %s", resp.Error.Code)
	}
}

// TestAuthModeAlwaysTokenInvalid 无效 token → 401 AUTH_INVALID
func TestAuthModeAlwaysTokenInvalid(t *testing.T) {
	mw := AuthMiddleware("always_token", "secret", nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer wrongtoken")
	req.RemoteAddr = "1.2.3.4:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("always_token invalid token: expected 401, got %d", rr.Code)
	}

	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Error.Code != string(errors.ErrAuthInvalid) {
		t.Errorf("expected AUTH_INVALID, got %s", resp.Error.Code)
	}
}

// TestAuthModeAlwaysTokenValid 有效 token → 通过
func TestAuthModeAlwaysTokenValid(t *testing.T) {
	mw := AuthMiddleware("always_token", "secret", nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.RemoteAddr = "1.2.3.4:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("always_token valid token: expected 200, got %d", rr.Code)
	}
}

// TestAuthModeLocalSkipLocalIP local_skip 模式 + 本地 IP → 免认证
func TestAuthModeLocalSkipLocalIP(t *testing.T) {
	mw := AuthMiddleware("local_skip", "secret", []string{"127.0.0.0/8"})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("local_skip local IP: expected 200, got %d", rr.Code)
	}
}

// TestAuthModeLocalSkipRemoteIP local_skip 模式 + 远程 IP + 无 token → 401
func TestAuthModeLocalSkipRemoteIP(t *testing.T) {
	mw := AuthMiddleware("local_skip", "secret", []string{"127.0.0.0/8"})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("local_skip remote IP no token: expected 401, got %d", rr.Code)
	}
}

// TestAuthModeLocalSkipRemoteIPWithToken local_skip + 远程 + 有效 token → 通过
func TestAuthModeLocalSkipRemoteIPWithToken(t *testing.T) {
	mw := AuthMiddleware("local_skip", "secret", []string{"127.0.0.0/8"})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.RemoteAddr = "1.2.3.4:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("local_skip remote IP with token: expected 200, got %d", rr.Code)
	}
}

// TestParseNetworks 网段解析正确
func TestParseNetworks(t *testing.T) {
	cidrs := []string{"127.0.0.0/8", "10.0.0.0/8", "invalid", "192.168.0.0/16"}
	networks := parseNetworks(cidrs)

	if len(networks) != 3 {
		t.Errorf("expected 3 valid networks, got %d", len(networks))
	}
}

// TestIsLocalIP 测试内网判定
func TestIsLocalIP(t *testing.T) {
	networks := parseNetworks([]string{"127.0.0.0/8", "10.0.0.0/8", "192.168.0.0/16", "172.16.0.0/12"})

	tests := []struct {
		ip     string
		expect bool
	}{
		{"127.0.0.1", true},
		{"10.0.0.1", true},
		{"192.168.1.1", true},
		{"172.16.0.1", true},
		{"::1", true},
		{"1.2.3.4", false},
		{"8.8.8.8", false},
	}

	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		if ip == nil {
			t.Fatalf("failed to parse IP: %s", tt.ip)
		}
		got := isLocalIP(ip, networks)
		if got != tt.expect {
			t.Errorf("isLocalIP(%s) = %v, want %v", tt.ip, got, tt.expect)
		}
	}
}

// TestLongPollLimiterGlobalLimit 全局并发超限 → 503
func TestLongPollLimiterGlobalLimit(t *testing.T) {
	limiter := NewLongPollLimiter(2, 5)

	// 使用 ready 通道确保 goroutine 已进入处理
	ready := make(chan struct{}, 2)
	block := make(chan struct{})
	handler := limiter.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ready <- struct{}{}
		<-block // 阻塞直到测试释放
	}))

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
			req.RemoteAddr = "1.2.3.4:1234"
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
		}()
	}

	// 等待两个请求都已进入处理
	for i := 0; i < 2; i++ {
		select {
		case <-ready:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for goroutine to start")
		}
	}

	// 第 3 个请求应该被拒绝
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	req.RemoteAddr = "5.6.7.8:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("global limit exceeded: expected 503, got %d", rr.Code)
	}

	// 释放阻塞的请求
	close(block)
	wg.Wait()
}

// TestLongPollLimiterPerClientLimit 单客户端并发超限 → 503
func TestLongPollLimiterPerClientLimit(t *testing.T) {
	limiter := NewLongPollLimiter(50, 2) // 全局50，单客户端2

	ready := make(chan struct{}, 2)
	block := make(chan struct{})
	handler := limiter.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ready <- struct{}{}
		<-block
	}))

	var wg sync.WaitGroup
	// 同一客户端发起 2 个请求
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
			req.RemoteAddr = "1.2.3.4:1234"
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
		}()
	}

	// 等待请求进入
	for i := 0; i < 2; i++ {
		select {
		case <-ready:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for goroutine to start")
		}
	}

	// 同一客户端第 3 个请求应该被拒绝
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("per-client limit exceeded: expected 503, got %d", rr.Code)
	}

	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Error.Code != string(errors.ErrServiceBusy) {
		t.Errorf("expected SERVICE_BUSY, got %s", resp.Error.Code)
	}

	// 释放
	close(block)
	wg.Wait()
}

// TestExtractClientIP 测试客户端 IP 提取
func TestExtractClientIP(t *testing.T) {
	tests := []struct {
		remoteAddr string
		want       string
	}{
		{"1.2.3.4:1234", "1.2.3.4"},
		{"[::1]:1234", "::1"},
		{"127.0.0.1:0", "127.0.0.1"},
	}

	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = tt.remoteAddr
		ip := extractClientIP(req)
		if ip == nil {
			t.Errorf("extractClientIP(%s) = nil, want %s", tt.remoteAddr, tt.want)
			continue
		}
		got := ip.String()
		// IPv6 地址可能包含 zone 信息，只比较前缀
		if !strings.HasPrefix(got, tt.want) {
			t.Errorf("extractClientIP(%s) = %s, want %s", tt.remoteAddr, got, tt.want)
		}
	}
}
