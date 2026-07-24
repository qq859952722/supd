package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/core"
)

// fakeStateProvider 轻量 StateProvider 实现，仅用于 service_handler 的 CRUD 测试，
// 避免启动真实子进程。通过嵌入接口满足契约，仅覆盖测试所需方法。
type fakeStateProvider struct {
	StateProvider
	states map[string]ServiceStateInfo
}

func (f *fakeStateProvider) GetServiceState(name string) (ServiceStateInfo, bool) {
	s, ok := f.states[name]
	return s, ok
}

func (f *fakeStateProvider) ListServiceStates() map[string]ServiceStateInfo {
	return f.states
}

// newServiceTestServer 构造仅含 fakeStateProvider + PathValidator 的 Server（hermetic）。
func newServiceTestServer(t *testing.T, states map[string]ServiceStateInfo) *Server {
	t.Helper()
	baseDir := t.TempDir()
	server := NewServer(&config.Config{Settings: config.Settings{AuthMode: "none"}})
	server.stateProvider = &fakeStateProvider{states: states}
	server.pathValidator = NewPathValidator(baseDir)
	return server
}

// TestListServices 覆盖 handleListServices：列出已知服务
func TestListServices(t *testing.T) {
	states := map[string]ServiceStateInfo{
		"svc-a": {Name: "svc-a", State: core.ServiceState("up"), RestartCount: 2},
	}
	server := newServiceTestServer(t, states)

	resp := doAPICall(t, server, http.MethodGet, "/api/services", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET /api/services: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	var list ServiceListResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(list.Services) != 1 || list.Services[0].Name != "svc-a" {
		t.Errorf("expected 1 service svc-a, got %+v", list.Services)
	}
	if list.Services[0].RestartCount != 2 {
		t.Errorf("restart_count = %d, want 2", list.Services[0].RestartCount)
	}
}

// TestGetService 覆盖 handleGetService：存在、不存在、非法名
func TestGetService(t *testing.T) {
	states := map[string]ServiceStateInfo{
		"svc-a": {Name: "svc-a", State: core.ServiceState("up")},
	}
	server := newServiceTestServer(t, states)

	// 存在
	resp := doAPICall(t, server, http.MethodGet, "/api/services/svc-a", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET /api/services/svc-a: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// 不存在
	resp = doAPICall(t, server, http.MethodGet, "/api/services/nope", nil)
	if resp.Code != http.StatusNotFound {
		t.Errorf("GET missing: expected 404, got %d", resp.Code)
	}

	// 非法名（路径穿越）
	resp = doAPICall(t, server, http.MethodGet, "/api/services/..%2F..", nil)
	if resp.Code != http.StatusBadRequest {
		t.Errorf("GET invalid name: expected 400, got %d", resp.Code)
	}
}

// TestCreateService 覆盖 handleCreateService：成功 + 各校验分支
func TestCreateService(t *testing.T) {
	server := newServiceTestServer(t, map[string]ServiceStateInfo{})
	baseDir := server.pathValidator.baseDir

	validBody, _ := json.Marshal(CreateServiceRequest{
		Name:    "newsvc",
		Version: "1.0",
		Command: []string{"sleep", "1"},
	})

	// 成功创建
	resp := doAPICall(t, server, http.MethodPost, "/api/services", validBody)
	if resp.Code != http.StatusCreated {
		t.Fatalf("POST /api/services: expected 201, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	// 校验 service.yaml 已写入
	yamlPath := filepath.Join(baseDir, "services", "newsvc", "service.yaml")
	if _, err := os.Stat(yamlPath); err != nil {
		t.Errorf("service.yaml not written: %v", err)
	}

	// 空 body → 400
	resp = doAPICall(t, server, http.MethodPost, "/api/services", []byte(""))
	if resp.Code != http.StatusBadRequest {
		t.Errorf("empty body: expected 400, got %d", resp.Code)
	}

	// 缺少 name → 400
	noName, _ := json.Marshal(CreateServiceRequest{Version: "1.0", Command: []string{"sleep", "1"}})
	resp = doAPICall(t, server, http.MethodPost, "/api/services", noName)
	if resp.Code != http.StatusBadRequest {
		t.Errorf("missing name: expected 400, got %d", resp.Code)
	}

	// 非法 name（大写）→ 400
	badName, _ := json.Marshal(CreateServiceRequest{Name: "BadName", Version: "1.0", Command: []string{"sleep", "1"}})
	resp = doAPICall(t, server, http.MethodPost, "/api/services", badName)
	if resp.Code != http.StatusBadRequest {
		t.Errorf("invalid name: expected 400, got %d", resp.Code)
	}

	// 缺少 command → 400
	noCmd, _ := json.Marshal(CreateServiceRequest{Name: "x", Version: "1.0"})
	resp = doAPICall(t, server, http.MethodPost, "/api/services", noCmd)
	if resp.Code != http.StatusBadRequest {
		t.Errorf("missing command: expected 400, got %d", resp.Code)
	}

	// 缺少 version → 400
	noVer, _ := json.Marshal(CreateServiceRequest{Name: "x", Command: []string{"sleep", "1"}})
	resp = doAPICall(t, server, http.MethodPost, "/api/services", noVer)
	if resp.Code != http.StatusBadRequest {
		t.Errorf("missing version: expected 400, got %d", resp.Code)
	}

	// 已存在 → 409
	states := map[string]ServiceStateInfo{"dup": {Name: "dup", State: core.ServiceState("pending")}}
	server2 := newServiceTestServer(t, states)
	dupBody, _ := json.Marshal(CreateServiceRequest{Name: "dup", Version: "1.0", Command: []string{"sleep", "1"}})
	resp = doAPICall(t, server2, http.MethodPost, "/api/services", dupBody)
	if resp.Code != http.StatusConflict {
		t.Errorf("already exists: expected 409, got %d (body: %s)", resp.Code, resp.Body.String())
	}
}

// TestUpdateService 覆盖 handleUpdateService：成功、不存在、非法名
func TestUpdateService(t *testing.T) {
	states := map[string]ServiceStateInfo{
		"svc-a": {Name: "svc-a", State: core.ServiceState("up")},
	}
	server := newServiceTestServer(t, states)
	baseDir := server.pathValidator.baseDir
	// update handler 不会自建目录（create 才会），需预建服务目录
	if err := os.MkdirAll(filepath.Join(baseDir, "services", "svc-a"), 0755); err != nil {
		t.Fatalf("mkdir svc dir: %v", err)
	}

	body, _ := json.Marshal(CreateServiceRequest{
		Name:    "svc-a",
		Version: "2.0",
		Command: []string{"sleep", "5"},
	})
	resp := doAPICall(t, server, http.MethodPut, "/api/services/svc-a", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("PUT /api/services/svc-a: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	yamlPath := filepath.Join(baseDir, "services", "svc-a", "service.yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("read service.yaml: %v", err)
	}
	if !strings.Contains(string(data), "2.0") {
		t.Errorf("service.yaml not updated, content: %s", string(data))
	}

	// 不存在 → 404
	resp = doAPICall(t, server, http.MethodPut, "/api/services/ghost", body)
	if resp.Code != http.StatusNotFound {
		t.Errorf("update missing: expected 404, got %d", resp.Code)
	}

	// 非法名 → 400
	resp = doAPICall(t, server, http.MethodPut, "/api/services/..%2F..", body)
	if resp.Code != http.StatusBadRequest {
		t.Errorf("update invalid name: expected 400, got %d", resp.Code)
	}
}

// TestCreateServiceFullConfig 覆盖 serviceRequestToConfig 的全部可选配置块
// （readiness/restart/stop/logging/signals/autostart/depends_on/tags/workdir）
func TestCreateServiceFullConfig(t *testing.T) {
	server := newServiceTestServer(t, map[string]ServiceStateInfo{})
	baseDir := server.pathValidator.baseDir

	autostart := true
	logEnabled := true
	req := CreateServiceRequest{
		Name:      "fullsvc",
		Version:   "1.0",
		Command:   []string{"sleep", "1"},
		Autostart: &autostart,
		Workdir:   "/tmp",
		DependsOn: []string{},
		Tags:      []string{"web", "demo"},
		Readiness:  &ReadinessRequest{Type: "http_check", Port: 8080, URL: "http://localhost:8080/health", ExpectedStatus: 200, IntervalSeconds: 2, TimeoutSeconds: 5},
		Restart:    &RestartRequest{Policy: "always", MaxRetries: 3, BackoffMs: 500},
		Stop:       &StopRequest{GraceSeconds: 10, TimeoutSeconds: 30},
		Logging:    &LoggingRequest{Enabled: &logEnabled, MaxSizeMB: 10, MaxFiles: 3},
		Signals:    &SignalsRequest{Reload: "SIGHUP", RotateLogs: "SIGUSR1"},
	}
	body, _ := json.Marshal(req)
	resp := doAPICall(t, server, http.MethodPost, "/api/services", body)
	if resp.Code != http.StatusCreated {
		t.Fatalf("POST /api/services full: expected 201, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(baseDir, "services", "fullsvc", "service.yaml"))
	if err != nil {
		t.Fatalf("read service.yaml: %v", err)
	}
	content := string(data)
	for _, want := range []string{"readiness", "http_check", "restart", "always", "stop", "logging", "signals", "SIGHUP", "web", "demo"} {
		if !strings.Contains(content, want) {
			t.Errorf("service.yaml missing %q; content: %s", want, content)
		}
	}
}

// TestDeleteService 覆盖 handleDeleteService：pending 删除、运行中 409、不存在 404
func TestDeleteService(t *testing.T) {
	baseDir := t.TempDir()
	// 预先创建服务目录
	svcDir := filepath.Join(baseDir, "services", "svc-a")
	if err := os.MkdirAll(svcDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(svcDir, "service.yaml"), []byte("name: svc-a\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	states := map[string]ServiceStateInfo{
		"svc-a": {Name: "svc-a", State: core.ServiceState("pending")},
	}
	server := NewServer(&config.Config{Settings: config.Settings{AuthMode: "none"}})
	server.stateProvider = &fakeStateProvider{states: states}
	server.pathValidator = NewPathValidator(baseDir)

	// pending → 删除成功
	resp := doAPICall(t, server, http.MethodDelete, "/api/services/svc-a", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("DELETE /api/services/svc-a: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	if _, err := os.Stat(svcDir); !os.IsNotExist(err) {
		t.Errorf("service dir should be removed")
	}

	// 运行中 → 409（fake 返回 up 状态）
	runningStates := map[string]ServiceStateInfo{
		"svc-b": {Name: "svc-b", State: core.ServiceState("up")},
	}
	runningDir := filepath.Join(baseDir, "services", "svc-b")
	os.MkdirAll(runningDir, 0755)
	server.stateProvider = &fakeStateProvider{states: runningStates}
	resp = doAPICall(t, server, http.MethodDelete, "/api/services/svc-b", nil)
	if resp.Code != http.StatusConflict {
		t.Errorf("delete running: expected 409, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// 不存在 → 404
	resp = doAPICall(t, server, http.MethodDelete, "/api/services/ghost", nil)
	if resp.Code != http.StatusNotFound {
		t.Errorf("delete missing: expected 404, got %d", resp.Code)
	}

	// 非法名 → 400
	resp = doAPICall(t, server, http.MethodDelete, "/api/services/..%2F..", nil)
	if resp.Code != http.StatusBadRequest {
		t.Errorf("delete invalid name: expected 400, got %d", resp.Code)
	}
}
