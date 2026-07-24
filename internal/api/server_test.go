package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/errors"

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

// TestAuthModes_RequestLevel 挂载 api.Server 后，对三种认证模式做请求级端到端验证（L-04-001）
// 重点验证 always_token 模式下 /api/health 作为公共端点绕过认证。
func TestAuthModes_RequestLevel(t *testing.T) {
	// ---------- always_token 模式 ----------
	s := NewServer(&config.Config{Settings: config.Settings{AuthMode: "always_token", AuthToken: "secret"}})
	r := s.Router()

	// /api/health 为公共端点，绕过认证 → 200（即便带错误 token 也为 200）
	healthReq := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	healthReq.Header.Set("Authorization", "Bearer wrong")
	hr := httptest.NewRecorder()
	r.ServeHTTP(hr, healthReq)
	if hr.Code != http.StatusOK {
		t.Errorf("always_token: /api/health 应绕过认证(200)，实际 %d", hr.Code)
	}

	// /api/auth/verify 亦为公共端点 → 不应返回 401
	verifyReq := httptest.NewRequest(http.MethodPost, "/api/auth/verify", nil)
	verifyReq.Header.Set("Content-Type", "application/json")
	vr := httptest.NewRecorder()
	r.ServeHTTP(vr, verifyReq)
	if vr.Code == http.StatusUnauthorized {
		t.Errorf("always_token: /api/auth/verify 应绕过认证，实际 401")
	}

	// 受保护路由无 token → 401 AUTH_REQUIRED
	noTok := httptest.NewRequest(http.MethodGet, "/api/services", nil)
	nr := httptest.NewRecorder()
	r.ServeHTTP(nr, noTok)
	if nr.Code != http.StatusUnauthorized {
		t.Errorf("always_token: /api/services 无 token 应 401，实际 %d", nr.Code)
	}
	var noTokResp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.NewDecoder(nr.Body).Decode(&noTokResp)
	if noTokResp.Error.Code != string(errors.ErrAuthRequired) {
		t.Errorf("always_token: 期望 AUTH_REQUIRED，实际 %q", noTokResp.Error.Code)
	}

	// 受保护路由无效 token → 401 AUTH_INVALID
	badTok := httptest.NewRequest(http.MethodGet, "/api/services", nil)
	badTok.Header.Set("Authorization", "Bearer wrong")
	br := httptest.NewRecorder()
	r.ServeHTTP(br, badTok)
	if br.Code != http.StatusUnauthorized {
		t.Errorf("always_token: /api/services 错误 token 应 401，实际 %d", br.Code)
	}
	var badTokResp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.NewDecoder(br.Body).Decode(&badTokResp)
	if badTokResp.Error.Code != string(errors.ErrAuthInvalid) {
		t.Errorf("always_token: 期望 AUTH_INVALID，实际 %q", badTokResp.Error.Code)
	}

	// 受保护路由有效 token → 通过认证到达 handler（无 provider 返回 500，但关键是非 401）
	goodTok := httptest.NewRequest(http.MethodGet, "/api/services", nil)
	goodTok.Header.Set("Authorization", "Bearer secret")
	gr := httptest.NewRecorder()
	r.ServeHTTP(gr, goodTok)
	if gr.Code == http.StatusUnauthorized {
		t.Errorf("always_token: /api/services 正确 token 不应为 401，实际 401")
	}

	// ---------- local_skip 模式：远程 IP 无 token → 401 ----------
	ls := NewServer(&config.Config{Settings: config.Settings{AuthMode: "local_skip", AuthToken: "secret", LocalNetworks: []string{"127.0.0.0/8"}}})
	lsr := ls.Router()
	remote := httptest.NewRequest(http.MethodGet, "/api/services", nil)
	remote.RemoteAddr = "1.2.3.4:1234" // 非本地网段
	lsrr := httptest.NewRecorder()
	lsr.ServeHTTP(lsrr, remote)
	if lsrr.Code != http.StatusUnauthorized {
		t.Errorf("local_skip: 远程 IP 无 token 应 401，实际 %d", lsrr.Code)
	}

	// local_skip 模式：loopback IP 免认证
	local := httptest.NewRequest(http.MethodGet, "/api/services", nil)
	local.RemoteAddr = "127.0.0.1:1234"
	lslr := httptest.NewRecorder()
	lsr.ServeHTTP(lslr, local)
	if lslr.Code == http.StatusUnauthorized {
		t.Errorf("local_skip: loopback IP 应跳过认证，实际 401")
	}

	// ---------- none 模式：所有请求免认证 ----------
	ns := NewServer(&config.Config{Settings: config.Settings{AuthMode: "none"}})
	nsr := ns.Router()
	noneReq := httptest.NewRequest(http.MethodGet, "/api/services", nil)
	nsrr := httptest.NewRecorder()
	nsr.ServeHTTP(nsrr, noneReq)
	if nsrr.Code == http.StatusUnauthorized {
		t.Errorf("none: 请求应跳过认证，实际 401")
	}
}
