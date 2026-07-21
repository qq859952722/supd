package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestNewServer Server 创建成功
func TestNewServer(t *testing.T) {
	s := NewServer(nil)
	if s == nil {
		t.Fatal("NewServer returned nil")
	}
	if s.router == nil {
		t.Fatal("Server router is nil")
	}
}

// TestRouteCount 注册的路由数量验证
// REQ-I-001: 65 个 API 端点 + 1 个 health 端点 = 66
func TestRouteCount(t *testing.T) {
	s := NewServer(nil)
	r := s.Router()

	var routeCount int
	walkFunc := func(method string, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		// 排除 HEAD 方法（chi 自动为 GET 添加 HEAD）
		if method == http.MethodHead {
			return nil
		}
		routeCount++
		return nil
	}

	if err := chi.Walk(r, walkFunc); err != nil {
		t.Fatalf("Walk error: %v", err)
	}

	// 期望 65 个 API 端点 + 1 个 health = 66
	// 加上可能的静态文件路由 /* (GET)
	// 我们只关心 /api 下的路由
	if routeCount < 66 {
		t.Errorf("expected at least 66 routes, got %d", routeCount)
	}
}

// TestHealthEndpoint /api/health 端点工作正常
func TestHealthEndpoint(t *testing.T) {
	s := NewServer(nil)
	server := httptest.NewServer(s.Router())
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/health")
	if err != nil {
		t.Fatalf("GET /api/health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["status"] != "ok" {
		t.Errorf("expected status ok, got %s", result["status"])
	}
}

// TestEndpointsWithoutProvider 无 provider 时端点返回 500
func TestEndpointsWithoutProvider(t *testing.T) {
	s := NewServer(nil)
	server := httptest.NewServer(s.Router())
	defer server.Close()

	// I-04-001 修复：测试 nil provider 时端点返回 500（非 notImplemented 占位）
	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/services"},
		{http.MethodGet, "/api/system/status"},
		{http.MethodGet, "/api/extensions"},
		{http.MethodGet, "/api/events"},
		{http.MethodGet, "/api/settings"},
	}

	for _, ep := range endpoints {
		req, err := http.NewRequest(ep.method, server.URL+ep.path, nil)
		if err != nil {
			t.Fatalf("creating request %s %s: %v", ep.method, ep.path, err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", ep.method, ep.path, err)
		}
		resp.Body.Close()

		// nil provider 时 handler 返回 ErrInternal，映射到 HTTP 500
		// 而非 HTTP 501，因为错误码体系中无 501 专用码
		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("%s %s: expected 500, got %d", ep.method, ep.path, resp.StatusCode)
		}
	}
}

// TestAuthVerifyEndpoint 认证端点正常工作
func TestAuthVerifyEndpoint(t *testing.T) {
	s := NewServer(nil)
	server := httptest.NewServer(s.Router())
	defer server.Close()

	// POST /api/auth/verify 无body返回400
	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/auth/verify", nil)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/auth/verify: %v", err)
	}
	defer resp.Body.Close()

	// 空body导致JSON解析失败，返回400
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}
