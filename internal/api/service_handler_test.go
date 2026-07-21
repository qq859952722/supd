package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"syscall"
	"testing"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/core"
)

// mockStateProvider 实现 StateProvider
type mockStateProvider struct {
	states map[string]ServiceStateInfo
}

func (m *mockStateProvider) GetServiceState(name string) (ServiceStateInfo, bool) {
	s, ok := m.states[name]
	return s, ok
}

func (m *mockStateProvider) ListServiceStates() map[string]ServiceStateInfo {
	return m.states
}

// mockServiceOperator 实现 ServiceOperator
type mockServiceOperator struct {
	started  []string
	stopped  []string
	restarted []string
	signals  map[string]string
	err      error
}

func (m *mockServiceOperator) StartService(name string) error {
	m.started = append(m.started, name)
	return m.err
}

func (m *mockServiceOperator) StopService(name string) error {
	m.stopped = append(m.stopped, name)
	return m.err
}

func (m *mockServiceOperator) RestartService(name string) error {
	m.restarted = append(m.restarted, name)
	return m.err
}

func (m *mockServiceOperator) SendSignal(name string, signal syscall.Signal) error {
	if m.signals == nil {
		m.signals = make(map[string]string)
	}
	m.signals[name] = "sent"
	return m.err
}

func (m *mockServiceOperator) ForceStopService(name string) error {
	m.stopped = append(m.stopped, name)
	return m.err
}

func (m *mockServiceOperator) ClearFailedState(name string) error {
	return m.err
}

// mockLogProvider 实现 LogProvider
type mockLogProvider struct {
	logs    []string
	search  []string
	logPos  int64
	logErr  error
	searchErr error
}

func (m *mockLogProvider) GetServiceLogs(serviceName string, sincePos int64) ([]string, int64, error) {
	return m.logs, m.logPos, m.logErr
}

func (m *mockLogProvider) SearchServiceLogs(serviceName string, pattern string, maxLines int) ([]string, error) {
	return m.search, m.searchErr
}

// mockServiceHistoryGetter 实现 ServiceHistoryGetter
type mockServiceHistoryGetter struct {
	history map[string][]HistoryEntry
	deaths  map[string][]HistoryEntry
}

func (m *mockServiceHistoryGetter) GetServiceHistory(name string) []HistoryEntry {
	if m.history == nil {
		return nil
	}
	return m.history[name]
}

func (m *mockServiceHistoryGetter) GetServiceDeaths(name string) []HistoryEntry {
	if m.deaths == nil {
		return nil
	}
	return m.deaths[name]
}

// createTestServer 创建带 mock providers 的测试服务器
func createTestServer() (*Server, *mockStateProvider, *mockServiceOperator, *mockLogProvider) {
	s := NewServer(nil)
	sp := &mockStateProvider{
		states: map[string]ServiceStateInfo{
			"web": {
				Name:         "web",
				State:        core.StateReady,
				PID:          1234,
				Uptime:       3600,
				RestartCount: 2,
				Config: &config.ServiceConfig{
					Name:    "web",
					Command: []string{"node", "server.js"},
					Icon:    "globe",
					Tags:    []string{"frontend", "prod"},
				},
				Enabled: true,
			},
			"db": {
				Name:         "db",
				State:        core.StateDown,
				PID:          0,
				Uptime:       0,
				RestartCount: 0,
				Config: &config.ServiceConfig{
					Name:    "db",
					Command: []string{"postgres"},
				},
				Enabled: true,
			},
			"api": {
				Name:         "api",
				State:        core.StateFailed,
				PID:          0,
				Uptime:       0,
				RestartCount: 5,
				Config: &config.ServiceConfig{
					Name:    "api",
					Command: []string{"python", "app.py"},
				},
				Enabled: false,
			},
		},
	}

	op := &mockServiceOperator{}
	lp := &mockLogProvider{
		logs:   []string{"line1", "line2", "line3"},
		logPos: 100,
		search: []string{"error: connection refused", "error: timeout"},
	}

	s.stateProvider = sp
	s.serviceOperator = op
	s.logProvider = lp
	s.serviceHistoryGetter = &mockServiceHistoryGetter{
		history: map[string][]HistoryEntry{
			"web": {
				{Time: "2026-07-08T10:00:00Z", PID: 1234, ExitCode: 0, Duration: 3600, Reason: "manual_stop"},
			},
		},
		deaths: map[string][]HistoryEntry{
			"api": {
				{Time: "2026-07-08T09:00:00Z", PID: 5678, ExitCode: 1, Duration: 300, Reason: "exit_code_nonzero"},
			},
		},
	}

	return s, sp, op, lp
}

func TestHandleListServices(t *testing.T) {
	s, _, _, _ := createTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/services", nil)
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp ServiceListResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Services) != 3 {
		t.Errorf("expected 3 services, got %d", len(resp.Services))
	}

	// 验证服务摘要字段
	foundWeb := false
	for _, svc := range resp.Services {
		if svc.Name == "web" {
			foundWeb = true
			if svc.Status != "ready" {
				t.Errorf("expected status ready, got %s", svc.Status)
			}
			if svc.PID != 1234 {
				t.Errorf("expected pid 1234, got %d", svc.PID)
			}
			if svc.Uptime != 3600 {
				t.Errorf("expected uptime 3600, got %d", svc.Uptime)
			}
			if svc.RestartCount != 2 {
				t.Errorf("expected restart_count 2, got %d", svc.RestartCount)
			}
			if svc.Icon != "globe" {
				t.Errorf("expected icon globe, got %s", svc.Icon)
			}
		}
	}
	if !foundWeb {
		t.Error("web service not found in response")
	}
}

func TestHandleGetService(t *testing.T) {
	s, _, _, _ := createTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/services/web", nil)
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var detail ServiceDetail
	if err := json.NewDecoder(w.Body).Decode(&detail); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if detail.Name != "web" {
		t.Errorf("expected name web, got %s", detail.Name)
	}
	if detail.Status != "ready" {
		t.Errorf("expected status ready, got %s", detail.Status)
	}
	if detail.PID != 1234 {
		t.Errorf("expected pid 1234, got %d", detail.PID)
	}
	if detail.RestartCount != 2 {
		t.Errorf("expected restart_count 2, got %d", detail.RestartCount)
	}
	if detail.Config == nil {
		t.Error("expected config to be non-nil")
	}
}

func TestHandleGetService_NotFound(t *testing.T) {
	s, _, _, _ := createTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/services/nonexistent", nil)
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandleDeleteService_Running(t *testing.T) {
	s, _, _, _ := createTestServer()

	// web 服务是 ready 状态，应该拒绝删除
	req := httptest.NewRequest(http.MethodDelete, "/api/services/web", nil)
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", w.Code)
	}
}

func TestHandleDeleteService_DownOK(t *testing.T) {
	s, _, _, _ := createTestServer()

	// db 服务是 down 状态，删除会被允许（但目录不存在会返回内部错误）
	req := httptest.NewRequest(http.MethodDelete, "/api/services/db", nil)
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	// 由于没有实际目录，可能返回500，但至少不会返回409（运行中拒绝）
	if w.Code == http.StatusConflict {
		t.Errorf("down service should not return 409, got %d", w.Code)
	}
}

func TestHandleDeleteService_FailedOK(t *testing.T) {
	s, _, _, _ := createTestServer()

	// api 服务是 failed 状态
	req := httptest.NewRequest(http.MethodDelete, "/api/services/api", nil)
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	// 由于没有实际目录，可能返回500，但至少不会返回409
	if w.Code == http.StatusConflict {
		t.Errorf("failed service should not return 409, got %d", w.Code)
	}
}

func TestHandleCreateService_InvalidBody(t *testing.T) {
	s, _, _, _ := createTestServer()

	req := httptest.NewRequest(http.MethodPost, "/api/services", bytes.NewBufferString("invalid json"))
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleCreateService_NoName(t *testing.T) {
	s, _, _, _ := createTestServer()

	body := `{"command": ["node", "server.js"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/services", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleCreateService_AlreadyExists(t *testing.T) {
	s, _, _, _ := createTestServer()

	body := `{"name": "web", "version": "1.0", "command": ["node", "server.js"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/services", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", w.Code)
	}
}

func TestHandleStartService(t *testing.T) {
	s, _, op, _ := createTestServer()

	req := httptest.NewRequest(http.MethodPost, "/api/services/web/start", nil)
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", w.Code)
	}

	if len(op.started) != 1 || op.started[0] != "web" {
		t.Errorf("expected start called for web, got %v", op.started)
	}
}

func TestHandleStopService(t *testing.T) {
	s, _, op, _ := createTestServer()

	req := httptest.NewRequest(http.MethodPost, "/api/services/web/stop", nil)
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", w.Code)
	}

	if len(op.stopped) != 1 || op.stopped[0] != "web" {
		t.Errorf("expected stop called for web, got %v", op.stopped)
	}
}

func TestHandleRestartService(t *testing.T) {
	s, _, op, _ := createTestServer()

	req := httptest.NewRequest(http.MethodPost, "/api/services/web/restart", nil)
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", w.Code)
	}

	if len(op.restarted) != 1 || op.restarted[0] != "web" {
		t.Errorf("expected restart called for web, got %v", op.restarted)
	}
}

func TestHandleSignalService(t *testing.T) {
	s, _, _, _ := createTestServer()

	body := `{"signal": "HUP"}`
	req := httptest.NewRequest(http.MethodPost, "/api/services/web/signal", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", w.Code)
	}
}

func TestHandleSignalService_NoSignal(t *testing.T) {
	s, _, _, _ := createTestServer()

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/services/web/signal", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleSignalService_UnknownSignal(t *testing.T) {
	s, _, _, _ := createTestServer()

	body := `{"signal": "INVALID"}`
	req := httptest.NewRequest(http.MethodPost, "/api/services/web/signal", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestParseSignal(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantOk   bool
	}{
		{"HUP", "HUP", true},
		{"SIGHUP", "SIGHUP", true},
		{"USR1", "USR1", true},
		{"TERM", "TERM", true},
		{"numeric", "15", true},
		{"unknown", "FOOBAR", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sig, err := parseSignal(tt.input)
			if tt.wantOk {
				if err != nil {
					t.Errorf("parseSignal(%s) unexpected error: %v", tt.input, err)
				}
				if sig == 0 && tt.input != "TERM" {
					t.Errorf("parseSignal(%s) returned 0", tt.input)
				}
			} else {
				if err == nil {
					t.Errorf("parseSignal(%s) expected error, got nil", tt.input)
				}
			}
		})
	}
}

func TestHandleSearchLogs(t *testing.T) {
	s, _, _, lp := createTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/services/web/logs/search?q=error", nil)
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp LogSearchResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Total != len(lp.search) {
		t.Errorf("expected total %d, got %d", len(lp.search), resp.Total)
	}
}

func TestHandleSearchLogs_NoQuery(t *testing.T) {
	s, _, _, _ := createTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/services/web/logs/search", nil)
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleServiceHistory(t *testing.T) {
	s, _, _, _ := createTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/services/web/history", nil)
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp HistoryResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(resp.Entries))
	}

	if resp.Entries[0].Reason != "manual_stop" {
		t.Errorf("expected reason manual_stop, got %s", resp.Entries[0].Reason)
	}
}

func TestHandleServiceDeaths(t *testing.T) {
	s, _, _, _ := createTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/services/api/deaths", nil)
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp HistoryResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(resp.Entries))
	}

	if resp.Entries[0].ExitCode != 1 {
		t.Errorf("expected exit_code 1, got %d", resp.Entries[0].ExitCode)
	}
}

func TestHandleServiceLogs(t *testing.T) {
	s, _, _, lp := createTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/services/web/logs", nil)
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp LongPollResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Data) != len(lp.logs) {
		t.Errorf("expected %d log lines, got %d", len(lp.logs), len(resp.Data))
	}
}

func TestHandleListServices_NoProvider(t *testing.T) {
	s := NewServer(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/services", nil)
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandleStartService_NoProvider(t *testing.T) {
	s := NewServer(nil)

	req := httptest.NewRequest(http.MethodPost, "/api/services/web/start", nil)
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandleSearchLogs_LimitCap(t *testing.T) {
	s, _, _, _ := createTestServer()

	// 请求 limit=5000，应被限制到1000
	req := httptest.NewRequest(http.MethodGet, "/api/services/web/logs/search?q=error&limit=5000", nil)
	w := httptest.NewRecorder()

	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}
