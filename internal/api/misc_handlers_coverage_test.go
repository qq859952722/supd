package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/core"
	"github.com/supdorg/supd/internal/errors"
	"github.com/supdorg/supd/internal/extension"
)

func bytesBody(s string) *bytes.Reader { return bytes.NewReader([]byte(s)) }

// ---- fake providers（嵌入接口以满足契约，仅覆盖所需方法）----

type fakeLogProvider struct {
	LogProvider
	logs   []string
	search []string
	err    error
}

func (f *fakeLogProvider) GetServiceLogs(name string, sincePos int64) ([]string, int64, error) {
	if f.err != nil {
		return nil, 0, f.err
	}
	return f.logs, int64(len(f.logs)), nil
}

func (f *fakeLogProvider) SearchServiceLogs(name, pattern string, maxLines int) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.search, nil
}

type fakeServiceHistoryGetter struct {
	ServiceHistoryGetter
	history []HistoryEntry
	deaths  []HistoryEntry
}

func (f *fakeServiceHistoryGetter) GetServiceHistory(name string) []HistoryEntry { return f.history }
func (f *fakeServiceHistoryGetter) GetServiceDeaths(name string) []HistoryEntry  { return f.deaths }

type fakeAuthProvider struct {
	AuthProvider
	valid bool
}

func (f *fakeAuthProvider) VerifyToken(token string) bool { return f.valid }

type fakeRuntimeProvider struct {
	RuntimeProvider
	runtimes  []RuntimeInfo
	uploadErr error
	deleteErr error
}

func (f *fakeRuntimeProvider) ListRuntimes() []RuntimeInfo { return f.runtimes }
func (f *fakeRuntimeProvider) UploadRuntime(name string, data []byte) error {
	return f.uploadErr
}
func (f *fakeRuntimeProvider) DeleteRuntime(name string) error { return f.deleteErr }

type fakeCronProvider struct {
	CronProvider
	entries []CronEntryInfo
	history []*extension.RunResult
}

func (f *fakeCronProvider) ListCronEntries() []CronEntryInfo { return f.entries }
func (f *fakeCronProvider) ListCronHistory(filter extension.RunFilter) []*extension.RunResult {
	return f.history
}

// newMiscServer 构造一个所有 provider 均为 fake 的 Server（hermetic），用于覆盖各类小 handler
func newMiscServer(t *testing.T) *Server {
	t.Helper()
	states := map[string]ServiceStateInfo{
		"svc-a": {Name: "svc-a", State: core.ServiceState("up"), PID: 0},
	}
	server := NewServer(&config.Config{Settings: config.Settings{AuthMode: "none"}})
	server.stateProvider = &fakeStateProvider{states: states}
	server.logProvider = &fakeLogProvider{logs: []string{"line1", "line2"}}
	server.serviceHistoryGetter = &fakeServiceHistoryGetter{
		history: []HistoryEntry{{Time: "t1", PID: 1, ExitCode: 0, Reason: "stop"}},
		deaths:  []HistoryEntry{{Time: "t2", PID: 2, ExitCode: 1, Reason: "crash"}},
	}
	server.authProvider = &fakeAuthProvider{valid: true}
	server.runtimeProvider = &fakeRuntimeProvider{runtimes: []RuntimeInfo{{Alias: "bash", Path: "/bin/bash", Available: true}}}
	server.cronProvider = &fakeCronProvider{entries: []CronEntryInfo{{ExtensionName: "e", ActionID: "a", Schedule: "* * * * *"}}}
	server.eventRing = NewEventRingBuffer(200)
	return server
}

// ---- 日志 ----

func TestServiceLogs(t *testing.T) {
	server := newMiscServer(t)
	// 成功（wait=false，直接返回）
	resp := doAPICall(t, server, http.MethodGet, "/api/services/svc-a/logs", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET logs: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	// 服务不存在 → 404
	resp = doAPICall(t, server, http.MethodGet, "/api/services/ghost/logs", nil)
	if resp.Code != http.StatusNotFound {
		t.Errorf("logs missing svc: expected 404, got %d", resp.Code)
	}
	// 缺 logProvider → 500
	server.logProvider = nil
	resp = doAPICall(t, server, http.MethodGet, "/api/services/svc-a/logs", nil)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("logs nil provider: expected 500, got %d", resp.Code)
	}
}

func TestSearchLogs(t *testing.T) {
	server := newMiscServer(t)
	server.logProvider = &fakeLogProvider{search: []string{"matched"}}
	// 成功
	resp := doAPICall(t, server, http.MethodGet, "/api/services/svc-a/logs/search?q=err", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("search logs: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	// 缺 q → 400
	resp = doAPICall(t, server, http.MethodGet, "/api/services/svc-a/logs/search", nil)
	if resp.Code != http.StatusBadRequest {
		t.Errorf("search no q: expected 400, got %d", resp.Code)
	}
	// 非法 limit → 400
	resp = doAPICall(t, server, http.MethodGet, "/api/services/svc-a/logs/search?q=err&limit=abc", nil)
	if resp.Code != http.StatusBadRequest {
		t.Errorf("search bad limit: expected 400, got %d", resp.Code)
	}
	// 非法 since → 400
	resp = doAPICall(t, server, http.MethodGet, "/api/services/svc-a/logs/search?q=err&since=bad", nil)
	if resp.Code != http.StatusBadRequest {
		t.Errorf("search bad since: expected 400, got %d", resp.Code)
	}
	// 服务不存在 → 404
	resp = doAPICall(t, server, http.MethodGet, "/api/services/ghost/logs/search?q=err", nil)
	if resp.Code != http.StatusNotFound {
		t.Errorf("search missing svc: expected 404, got %d", resp.Code)
	}
}

// ---- 历史 ----

func TestServiceHistory(t *testing.T) {
	server := newMiscServer(t)
	resp := doAPICall(t, server, http.MethodGet, "/api/services/svc-a/history", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("history: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	// 不存在 → 404
	resp = doAPICall(t, server, http.MethodGet, "/api/services/ghost/history", nil)
	if resp.Code != http.StatusNotFound {
		t.Errorf("history missing: expected 404, got %d", resp.Code)
	}
	// 缺 getter → 500
	server.serviceHistoryGetter = nil
	resp = doAPICall(t, server, http.MethodGet, "/api/services/svc-a/history", nil)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("history nil getter: expected 500, got %d", resp.Code)
	}
}

func TestServiceDeaths(t *testing.T) {
	server := newMiscServer(t)
	resp := doAPICall(t, server, http.MethodGet, "/api/services/svc-a/deaths", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("deaths: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	server.serviceHistoryGetter = nil
	resp = doAPICall(t, server, http.MethodGet, "/api/services/svc-a/deaths", nil)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("deaths nil getter: expected 500, got %d", resp.Code)
	}
}

// ---- 事件 ----

func TestRecentEvents(t *testing.T) {
	server := newMiscServer(t)
	server.eventRing.Add("service_state", map[string]any{"name": "svc-a"})
	resp := doAPICall(t, server, http.MethodGet, "/api/system/events/recent?limit=10", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("recent events: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	// 非法 limit → 400
	resp = doAPICall(t, server, http.MethodGet, "/api/system/events/recent?limit=0", nil)
	if resp.Code != http.StatusBadRequest {
		t.Errorf("recent bad limit: expected 400, got %d", resp.Code)
	}
	// 缺 ring → 500
	server.eventRing = nil
	resp = doAPICall(t, server, http.MethodGet, "/api/system/events/recent", nil)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("recent nil ring: expected 500, got %d", resp.Code)
	}
}

func TestEventsLongPoll(t *testing.T) {
	server := newMiscServer(t)
	server.eventRing.Add("service_state", map[string]any{"name": "svc-a"})
	// 有数据时立即返回（不进入等待）
	resp := doAPICall(t, server, http.MethodGet, "/api/events?since=0", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("events: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	// 缺 ring → 500
	server.eventRing = nil
	resp = doAPICall(t, server, http.MethodGet, "/api/events", nil)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("events nil ring: expected 500, got %d", resp.Code)
	}
}

// ---- 认证 ----

func TestAuthVerify(t *testing.T) {
	server := newMiscServer(t)
	// provider 校验通过
	body, _ := json.Marshal(AuthVerifyRequest{Token: "tok"})
	resp := doAPICall(t, server, http.MethodPost, "/api/auth/verify", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("verify ok: expected 200, got %d", resp.Code)
	}
	var av AuthVerifyResponse
	json.Unmarshal(resp.Body.Bytes(), &av)
	if !av.Valid {
		t.Errorf("expected valid=true")
	}
	// provider 校验失败
	server.authProvider = &fakeAuthProvider{valid: false}
	resp = doAPICall(t, server, http.MethodPost, "/api/auth/verify", body)
	json.Unmarshal(resp.Body.Bytes(), &av)
	if av.Valid {
		t.Errorf("expected valid=false")
	}
	// 空 token → 401
	resp = doAPICall(t, server, http.MethodPost, "/api/auth/verify", json.RawMessage(`{"token":""}`))
	if resp.Code != http.StatusUnauthorized {
		t.Errorf("empty token: expected 401, got %d", resp.Code)
	}

	// 无 provider + AuthMode=always_token + 配置 token 比对分支
	cfg := &config.Config{Settings: config.Settings{AuthMode: "always_token", AuthToken: "secret"}}
	s2 := NewServer(cfg)
	s2.authProvider = nil
	okBody, _ := json.Marshal(AuthVerifyRequest{Token: "secret"})
	resp = doAPICall(t, s2, http.MethodPost, "/api/auth/verify", okBody)
	json.Unmarshal(resp.Body.Bytes(), &av)
	if !av.Valid {
		t.Errorf("config-based verify: expected valid=true for matching token")
	}
	badBody, _ := json.Marshal(AuthVerifyRequest{Token: "wrong"})
	resp = doAPICall(t, s2, http.MethodPost, "/api/auth/verify", badBody)
	json.Unmarshal(resp.Body.Bytes(), &av)
	if av.Valid {
		t.Errorf("config-based verify: expected valid=false for wrong token")
	}

	// 无 provider + AuthMode=none → 默认 valid
	s3 := NewServer(&config.Config{Settings: config.Settings{AuthMode: "none"}})
	resp = doAPICall(t, s3, http.MethodPost, "/api/auth/verify", okBody)
	json.Unmarshal(resp.Body.Bytes(), &av)
	if !av.Valid {
		t.Errorf("none mode no provider: expected valid=true")
	}
}

// ---- 运行时 ----

func TestListRuntimes(t *testing.T) {
	server := newMiscServer(t)
	resp := doAPICall(t, server, http.MethodGet, "/api/runtimes", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("list runtimes: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	server.runtimeProvider = nil
	resp = doAPICall(t, server, http.MethodGet, "/api/runtimes", nil)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("list runtimes nil: expected 500, got %d", resp.Code)
	}
}

func TestUploadRuntime(t *testing.T) {
	server := newMiscServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/runtimes/upload?name=node", bytesBody("binary"))
	rec := httptest.NewRecorder()
	server.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload runtime: expected 201, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	// 缺 name → 400
	req = httptest.NewRequest(http.MethodPost, "/api/runtimes/upload", bytesBody("x"))
	rec = httptest.NewRecorder()
	server.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("upload runtime no name: expected 400, got %d", rec.Code)
	}
	// 非法 name（含 /）→ 400
	req = httptest.NewRequest(http.MethodPost, "/api/runtimes/upload?name=a/b", bytesBody("x"))
	rec = httptest.NewRecorder()
	server.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("upload runtime bad name: expected 400, got %d", rec.Code)
	}
}

func TestDeleteRuntime(t *testing.T) {
	server := newMiscServer(t)
	resp := doAPICall(t, server, http.MethodDelete, "/api/runtimes/node", nil)
	if resp.Code != http.StatusNoContent {
		t.Fatalf("delete runtime: expected 204, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	// 删除不存在 → 422（ErrRuntimeNotFound 映射为 422 UnprocessableEntity）
	server.runtimeProvider = &fakeRuntimeProvider{deleteErr: errors.NewServiceError(errors.ErrRuntimeNotFound, "not found")}
	resp = doAPICall(t, server, http.MethodDelete, "/api/runtimes/missing", nil)
	if resp.Code != http.StatusUnprocessableEntity {
		t.Errorf("delete runtime missing: expected 422, got %d (body: %s)", resp.Code, resp.Body.String())
	}
}

// ---- 定时任务 ----

func TestCronHandlers(t *testing.T) {
	server := newMiscServer(t)
	resp := doAPICall(t, server, http.MethodGet, "/api/cron", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("list cron: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	resp = doAPICall(t, server, http.MethodGet, "/api/cron/history", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("cron history: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	server.cronProvider = nil
	resp = doAPICall(t, server, http.MethodGet, "/api/cron", nil)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("cron nil provider: expected 500, got %d", resp.Code)
	}
}

// ---- 资源（PID=0 路径，无需真实进程）----

func TestServiceResourcesPIDZero(t *testing.T) {
	server := newMiscServer(t)
	resp := doAPICall(t, server, http.MethodGet, "/api/services/svc-a/resources", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("resources: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	resp = doAPICall(t, server, http.MethodGet, "/api/services/svc-a/processes", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("processes: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	// 不存在 → 404
	resp = doAPICall(t, server, http.MethodGet, "/api/services/ghost/resources", nil)
	if resp.Code != http.StatusNotFound {
		t.Errorf("resources missing: expected 404, got %d", resp.Code)
	}
}

// ---- 热重载 ----

func TestReloadHandler(t *testing.T) {
	baseDir := t.TempDir()
	logDir := t.TempDir()
	server := NewServer(&config.Config{Settings: config.Settings{AuthMode: "none"}})
	server.watchProvider = &CoreWatchProvider{BaseDir: baseDir, LogDir: logDir}
	server.eventRing = NewEventRingBuffer(200)

	resp := doAPICall(t, server, http.MethodPost, "/api/reload", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("reload: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// watchProvider 非 CoreWatchProvider → 500
	server.watchProvider = &fakeWatchProviderShim{}
	resp = doAPICall(t, server, http.MethodPost, "/api/reload", nil)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("reload bad provider: expected 500, got %d", resp.Code)
	}
}

// fakeWatchProviderShim 仅用于触发 triggerReload 中 *CoreWatchProvider 断言失败分支
type fakeWatchProviderShim struct {
	WatchProvider
}
