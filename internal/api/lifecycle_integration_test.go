package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/core"
	"github.com/supdorg/supd/internal/logging"
	"github.com/supdorg/supd/internal/watch"
)

// TestLifecycleIntegration_StartStopDown 端到端服务生命周期集成测试
// L-04-001: 覆盖 pending → starting → up → stopping → down 全流程，
// 涉及 CoreServiceOperator + CoreStateProvider + ProcessManager + StateMachine 协同。
// 使用真实子进程（sleep 5），不使用 mock。
func TestLifecycleIntegration_StartStopDown(t *testing.T) {
	// 准备临时目录与配置
	baseDir := t.TempDir()
	logDir := t.TempDir()
	servicesDir := filepath.Join(baseDir, "services", "test-svc")
	if err := os.MkdirAll(servicesDir, 0755); err != nil {
		t.Fatalf("mkdir services: %v", err)
	}
	// sleep 5：进程会运行足够久以供测试轮询状态，测试结束后即使清理失败也会自然退出
	svcYAML := `name: test-svc
version: "1.0"
command:
  - sleep
  - "5"
`
	if err := os.WriteFile(filepath.Join(servicesDir, "service.yaml"), []byte(svcYAML), 0644); err != nil {
		t.Fatalf("write service.yaml: %v", err)
	}

	// 扫描发现服务
	disc := watch.NewDiscovery(baseDir, logDir)
	discovery := disc.Scan()
	if len(discovery.Services) == 0 {
		t.Fatalf("discovery found no services in %s", baseDir)
	}

	// 构建共享组件
	processMgr := core.NewProcessManager()
	processMgr.SetPIDFileDir(baseDir)
	stateMachines := make(map[string]*core.StateMachine)
	restartEngines := make(map[string]*core.RestartEngine)
	for name := range discovery.Services {
		sm := core.NewStateMachine()
		sm.SetName(name)
		stateMachines[name] = sm
	}

	cfg := &config.Config{Settings: config.Settings{AuthMode: "none"}}

	// 构造 CoreServiceOperator 和 CoreStateProvider
	op := &CoreServiceOperator{
		ProcessMgr:    processMgr,
		StateMachines: stateMachines,
		Discovery:     discovery,
		Config:        cfg,
		BaseDir:       baseDir,
		LogDir:        logDir,
		loggers:       make(map[string]*logging.ServiceLogger),
		cancelFuncs:   make(map[string]context.CancelFunc),
	}
	sp := &CoreStateProvider{
		StateMachines:  stateMachines,
		ProcessMgr:     processMgr,
		Discovery:      discovery,
		Config:         cfg,
		StartTime:      time.Now(),
		RestartEngines: restartEngines,
	}
	op.RestartEngines = restartEngines

	// 构造 API Server 并注入 providers
	server := NewServer(cfg)
	server.stateProvider = sp
	server.serviceOperator = op
	server.pathValidator = NewPathValidator(baseDir)
	// eventRing 供 service_died/service_exited 等事件发布使用
	server.eventRing = NewEventRingBuffer(200)
	op.EventPublisher = server.eventRing

	// 确保清理：杀进程、关状态机、关日志器
	t.Cleanup(func() {
		// 1. 杀进程（触发 supervisor 的 proc.Wait() 返回）
		for _, name := range processMgr.List() {
			processMgr.KillProcessGroup(name)
		}
		// 2. 取消所有 supervisor context（停止退避等待中的服务）
		op.cancelFuncsMu.Lock()
		for _, cancel := range op.cancelFuncs {
			cancel()
		}
		op.cancelFuncsMu.Unlock()
		// 3. 等待 supervisor goroutine 退出，避免与后续 map 操作竞态
		time.Sleep(300 * time.Millisecond)
		// 4. 关闭状态机和日志器（supervisor 已退出，可安全访问）
		for _, sm := range stateMachines {
			sm.Close()
		}
		op.loggersMu.Lock()
		for _, logger := range op.loggers {
			logger.Close()
		}
		op.loggersMu.Unlock()
	})

	// 1. 初始状态应为 pending（状态机默认初始状态）
	initialState := getStateViaAPI(t, server, "test-svc")
	if initialState != "pending" {
		t.Errorf("initial state = %s, want pending", initialState)
	}

	// 2. 启动服务
	startResp := doAPICall(t, server, http.MethodPost, "/api/services/test-svc/start", nil)
	if startResp.Code != http.StatusAccepted {
		t.Fatalf("start service: expected 202, got %d (body: %s)", startResp.Code, startResp.Body.String())
	}

	// 3. 轮询状态到 up（无 readiness 配置，应快速进入 up）
	deadline := time.Now().Add(5 * time.Second)
	var state string
	for time.Now().Before(deadline) {
		state = getStateViaAPI(t, server, "test-svc")
		if state == "up" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if state != "up" {
		t.Fatalf("service did not reach up state (got %s) within timeout", state)
	}

	// 4. 停止服务
	stopResp := doAPICall(t, server, http.MethodPost, "/api/services/test-svc/stop", nil)
	if stopResp.Code != http.StatusAccepted {
		t.Fatalf("stop service: expected 202, got %d (body: %s)", stopResp.Code, stopResp.Body.String())
	}

	// 5. 轮询状态到 down
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		state = getStateViaAPI(t, server, "test-svc")
		if state == "down" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if state != "down" {
		t.Errorf("service did not reach down state (got %s) within timeout", state)
	}
}

// getStateViaAPI 通过 HTTP API 查询服务状态
func getStateViaAPI(t *testing.T, server *Server, name string) string {
	t.Helper()
	resp := doAPICall(t, server, http.MethodGet, "/api/services/"+name, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET /api/services/%s: expected 200, got %d (body: %s)", name, resp.Code, resp.Body.String())
	}
	var detail ServiceDetail
	if err := json.Unmarshal(resp.Body.Bytes(), &detail); err != nil {
		t.Fatalf("unmarshal service detail: %v", err)
	}
	return detail.Status
}

// doAPICall 执行 API 调用并返回响应
func doAPICall(t *testing.T, server *Server, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var bodyReader *bytes.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	var req *http.Request
	var err error
	if bodyReader != nil {
		req, err = http.NewRequest(method, path, bodyReader)
	} else {
		req, err = http.NewRequest(method, path, nil)
	}
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)
	return w
}
