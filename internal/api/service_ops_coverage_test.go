package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/core"
	"github.com/supdorg/supd/internal/errors"
)

// fakeServiceOperator 嵌入接口以满足契约，覆盖 service_ops 测试所需方法。
type fakeServiceOperator struct {
	ServiceOperator
	startErr  error
	stopErr   error
	restartErr error
	signalErr error
	forceErr  error
	clearErr  error
}

func (f *fakeServiceOperator) StartService(name string) error             { return f.startErr }
func (f *fakeServiceOperator) StopService(name string) error              { return f.stopErr }
func (f *fakeServiceOperator) RestartService(name string) error           { return f.restartErr }
func (f *fakeServiceOperator) SendSignal(name string, sig syscall.Signal) error { return f.signalErr }
func (f *fakeServiceOperator) ForceStopService(name string) error         { return f.forceErr }
func (f *fakeServiceOperator) ClearFailedState(name string) error         { return f.clearErr }

func newServiceOpsServer(t *testing.T, states map[string]ServiceStateInfo, op *fakeServiceOperator) *Server {
	t.Helper()
	baseDir := t.TempDir()
	server := NewServer(&config.Config{Settings: config.Settings{AuthMode: "none"}})
	server.stateProvider = &fakeStateProvider{states: states}
	server.serviceOperator = op
	server.pathValidator = NewPathValidator(baseDir)
	return server
}

// TestStartService 覆盖 handleStartService：成功、不存在404、已运行409、命令不存在400、ServiceError、generic500、nil operator、非法名。
func TestStartService(t *testing.T) {
	states := map[string]ServiceStateInfo{"svc-a": {Name: "svc-a", State: core.ServiceState("pending")}}
	server := newServiceOpsServer(t, states, &fakeServiceOperator{})

	// 成功 → 202
	resp := doAPICall(t, server, http.MethodPost, "/api/services/svc-a/start", nil)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("start: expected 202, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// 不存在 → 404
	resp = doAPICall(t, server, http.MethodPost, "/api/services/ghost/start", nil)
	if resp.Code != http.StatusNotFound {
		t.Errorf("start missing: expected 404, got %d", resp.Code)
	}

	// 已运行 → 409
	server.serviceOperator = &fakeServiceOperator{startErr: fmt.Errorf("service already running")}
	resp = doAPICall(t, server, http.MethodPost, "/api/services/svc-a/start", nil)
	if resp.Code != http.StatusConflict {
		t.Errorf("start already running: expected 409, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// 命令不存在 → 400
	server.serviceOperator = &fakeServiceOperator{startErr: fmt.Errorf("exec: \"cmd\": executable file not found in $PATH")}
	resp = doAPICall(t, server, http.MethodPost, "/api/services/svc-a/start", nil)
	if resp.Code != http.StatusBadRequest {
		t.Errorf("start cmd not found: expected 400, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// ServiceError → 按 Code 映射（ErrRuntimeNotFound → 422）
	server.serviceOperator = &fakeServiceOperator{startErr: errors.NewServiceError(errors.ErrRuntimeNotFound, "no runtime")}
	resp = doAPICall(t, server, http.MethodPost, "/api/services/svc-a/start", nil)
	if resp.Code != http.StatusUnprocessableEntity {
		t.Errorf("start service error: expected 422, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// 其他错误 → 500
	server.serviceOperator = &fakeServiceOperator{startErr: fmt.Errorf("boom")}
	resp = doAPICall(t, server, http.MethodPost, "/api/services/svc-a/start", nil)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("start boom: expected 500, got %d", resp.Code)
	}

	// nil operator → 500
	server.serviceOperator = nil
	resp = doAPICall(t, server, http.MethodPost, "/api/services/svc-a/start", nil)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("start nil op: expected 500, got %d", resp.Code)
	}

	// 非法名 → 400（路径穿越）
	resp = doAPICall(t, server, http.MethodPost, "/api/services/..%2F..%2Fetc/start", nil)
	if resp.Code != http.StatusBadRequest {
		t.Errorf("start invalid name: expected 400, got %d", resp.Code)
	}
}

// TestStopService 覆盖 handleStopService：成功、未运行400、不存在404、operator错误500。
func TestStopService(t *testing.T) {
	states := map[string]ServiceStateInfo{"svc-a": {Name: "svc-a", State: core.ServiceState("up")}}
	server := newServiceOpsServer(t, states, &fakeServiceOperator{})

	resp := doAPICall(t, server, http.MethodPost, "/api/services/svc-a/stop", nil)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("stop: expected 202, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// 未运行 → 400
	states2 := map[string]ServiceStateInfo{"svc-b": {Name: "svc-b", State: core.ServiceState("pending")}}
	server.stateProvider = &fakeStateProvider{states: states2}
	resp = doAPICall(t, server, http.MethodPost, "/api/services/svc-b/stop", nil)
	if resp.Code != http.StatusBadRequest {
		t.Errorf("stop not running: expected 400, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// 不存在 → 404
	resp = doAPICall(t, server, http.MethodPost, "/api/services/ghost/stop", nil)
	if resp.Code != http.StatusNotFound {
		t.Errorf("stop missing: expected 404, got %d", resp.Code)
	}

	// operator 错误 → 500
	server.stateProvider = &fakeStateProvider{states: states}
	server.serviceOperator = &fakeServiceOperator{stopErr: fmt.Errorf("boom")}
	resp = doAPICall(t, server, http.MethodPost, "/api/services/svc-a/stop", nil)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("stop err: expected 500, got %d", resp.Code)
	}
}

// TestRestartService 覆盖 handleRestartService：成功、不存在404、ServiceError、generic500。
func TestRestartService(t *testing.T) {
	states := map[string]ServiceStateInfo{"svc-a": {Name: "svc-a", State: core.ServiceState("up")}}
	server := newServiceOpsServer(t, states, &fakeServiceOperator{})

	resp := doAPICall(t, server, http.MethodPost, "/api/services/svc-a/restart", nil)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("restart: expected 202, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	resp = doAPICall(t, server, http.MethodPost, "/api/services/ghost/restart", nil)
	if resp.Code != http.StatusNotFound {
		t.Errorf("restart missing: expected 404, got %d", resp.Code)
	}

	server.serviceOperator = &fakeServiceOperator{restartErr: errors.NewServiceError(errors.ErrRuntimeNotFound, "nf")}
	resp = doAPICall(t, server, http.MethodPost, "/api/services/svc-a/restart", nil)
	if resp.Code != http.StatusUnprocessableEntity {
		t.Errorf("restart service error: expected 422, got %d", resp.Code)
	}

	server.serviceOperator = &fakeServiceOperator{restartErr: fmt.Errorf("boom")}
	resp = doAPICall(t, server, http.MethodPost, "/api/services/svc-a/restart", nil)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("restart boom: expected 500, got %d", resp.Code)
	}
}

// TestSignalService 覆盖 handleSignalService：成功、非法body、空signal、未知signal、未运行400、不存在404、operator错误。
func TestSignalService(t *testing.T) {
	states := map[string]ServiceStateInfo{"svc-a": {Name: "svc-a", State: core.ServiceState("up")}}
	server := newServiceOpsServer(t, states, &fakeServiceOperator{})

	body, _ := json.Marshal(SignalRequest{Signal: "HUP"})
	resp := doAPICall(t, server, http.MethodPost, "/api/services/svc-a/signal", body)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("signal: expected 202, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// 非法 body → 400
	resp = doAPICall(t, server, http.MethodPost, "/api/services/svc-a/signal", []byte("bad"))
	if resp.Code != http.StatusBadRequest {
		t.Errorf("signal bad body: expected 400, got %d", resp.Code)
	}

	// 空 signal → 400 field error
	resp = doAPICall(t, server, http.MethodPost, "/api/services/svc-a/signal", json.RawMessage(`{"signal":""}`))
	if resp.Code != http.StatusBadRequest {
		t.Errorf("signal empty: expected 400, got %d", resp.Code)
	}

	// 未知 signal → 400
	resp = doAPICall(t, server, http.MethodPost, "/api/services/svc-a/signal", json.RawMessage(`{"signal":"NOPE"}`))
	if resp.Code != http.StatusBadRequest {
		t.Errorf("signal unknown: expected 400, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// 未运行状态 → 400
	states2 := map[string]ServiceStateInfo{"svc-b": {Name: "svc-b", State: core.ServiceState("pending")}}
	server.stateProvider = &fakeStateProvider{states: states2}
	resp = doAPICall(t, server, http.MethodPost, "/api/services/svc-b/signal", body)
	if resp.Code != http.StatusBadRequest {
		t.Errorf("signal not running: expected 400, got %d", resp.Code)
	}

	// 不存在 → 404
	resp = doAPICall(t, server, http.MethodPost, "/api/services/ghost/signal", body)
	if resp.Code != http.StatusNotFound {
		t.Errorf("signal missing: expected 404, got %d", resp.Code)
	}

	// operator 错误 → 500
	server.stateProvider = &fakeStateProvider{states: states}
	server.serviceOperator = &fakeServiceOperator{signalErr: fmt.Errorf("boom")}
	resp = doAPICall(t, server, http.MethodPost, "/api/services/svc-a/signal", body)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("signal err: expected 500, got %d", resp.Code)
	}
}

// TestStartStopAllServices 覆盖 handleStartAllServices / handleStopAllServices。
func TestStartStopAllServices(t *testing.T) {
	states := map[string]ServiceStateInfo{
		"a": {Name: "a", State: core.ServiceState("pending")},
		"b": {Name: "b", State: core.ServiceState("pending")},
	}
	server := newServiceOpsServer(t, states, &fakeServiceOperator{})

	resp := doAPICall(t, server, http.MethodPost, "/api/services/start", nil)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("start all: expected 202, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	var out map[string]int
	json.Unmarshal(resp.Body.Bytes(), &out)
	if out["started"] != 2 {
		t.Errorf("start all started = %d, want 2", out["started"])
	}

	// 含失败计数：让 StartService 对 b 报错
	server.serviceOperator = &fakeServiceOperator{startErr: fmt.Errorf("boom")}
	resp = doAPICall(t, server, http.MethodPost, "/api/services/start", nil)
	json.Unmarshal(resp.Body.Bytes(), &out)
	if out["failed"] != 2 {
		t.Errorf("start all failed = %d, want 2", out["failed"])
	}

	resp = doAPICall(t, server, http.MethodPost, "/api/services/stop", nil)
	if resp.Code != http.StatusAccepted {
		t.Errorf("stop all: expected 202, got %d", resp.Code)
	}

	// nil operator → 500
	server.serviceOperator = nil
	resp = doAPICall(t, server, http.MethodPost, "/api/services/start", nil)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("start all nil op: expected 500, got %d", resp.Code)
	}
}

// TestForceStopAndClearFailed 覆盖 handleForceStopService / handleClearFailedService。
func TestForceStopAndClearFailed(t *testing.T) {
	states := map[string]ServiceStateInfo{"svc-a": {Name: "svc-a", State: core.ServiceState("up")}}
	server := newServiceOpsServer(t, states, &fakeServiceOperator{})

	resp := doAPICall(t, server, http.MethodPost, "/api/services/svc-a/force-stop", nil)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("force-stop: expected 202, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	resp = doAPICall(t, server, http.MethodPost, "/api/services/ghost/force-stop", nil)
	if resp.Code != http.StatusNotFound {
		t.Errorf("force-stop missing: expected 404, got %d", resp.Code)
	}
	server.serviceOperator = &fakeServiceOperator{forceErr: fmt.Errorf("boom")}
	resp = doAPICall(t, server, http.MethodPost, "/api/services/svc-a/force-stop", nil)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("force-stop err: expected 500, got %d", resp.Code)
	}

	server.serviceOperator = &fakeServiceOperator{}
	resp = doAPICall(t, server, http.MethodPost, "/api/services/svc-a/clear-failed", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("clear-failed: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	resp = doAPICall(t, server, http.MethodPost, "/api/services/ghost/clear-failed", nil)
	if resp.Code != http.StatusNotFound {
		t.Errorf("clear-failed missing: expected 404, got %d", resp.Code)
	}
	server.serviceOperator = &fakeServiceOperator{clearErr: fmt.Errorf("boom")}
	resp = doAPICall(t, server, http.MethodPost, "/api/services/svc-a/clear-failed", nil)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("clear-failed err: expected 500, got %d", resp.Code)
	}
}

// TestUpdateServiceConfig 覆盖 handleUpdateServiceConfig：成功写文件、空内容400、非法body、不存在404。
func TestUpdateServiceConfig(t *testing.T) {
	baseDir := t.TempDir()
	states := map[string]ServiceStateInfo{
		"svc-a": {Name: "svc-a", State: core.ServiceState("up"), ConfigPath: filepath.Join(baseDir, "services", "svc-a", "service.yaml")},
	}
	server := NewServer(&config.Config{Settings: config.Settings{AuthMode: "none"}})
	server.stateProvider = &fakeStateProvider{states: states}
	server.pathValidator = NewPathValidator(baseDir)
	if err := os.MkdirAll(filepath.Join(baseDir, "services", "svc-a"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"content": "name: svc-a\nversion: \"1.0\"\ncommand: [sleep, 1]\n"})
	resp := doAPICall(t, server, http.MethodPut, "/api/services/svc-a/config", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("update config: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	if _, err := os.Stat(filepath.Join(baseDir, "services", "svc-a", "service.yaml")); err != nil {
		t.Errorf("config file not written: %v", err)
	}

	// 空内容 → 400 field error
	resp = doAPICall(t, server, http.MethodPut, "/api/services/svc-a/config", json.RawMessage(`{"content":"   "}`))
	if resp.Code != http.StatusBadRequest {
		t.Errorf("update config empty: expected 400, got %d", resp.Code)
	}

	// 非法 body → 400
	resp = doAPICall(t, server, http.MethodPut, "/api/services/svc-a/config", []byte("bad"))
	if resp.Code != http.StatusBadRequest {
		t.Errorf("update config bad body: expected 400, got %d", resp.Code)
	}

	// 不存在 → 404
	resp = doAPICall(t, server, http.MethodPut, "/api/services/ghost/config", body)
	if resp.Code != http.StatusNotFound {
		t.Errorf("update config missing: expected 404, got %d", resp.Code)
	}
}

// TestSaveServiceEnv 覆盖 handleSaveServiceEnv：成功写文件、非法body、不存在404。
func TestSaveServiceEnv(t *testing.T) {
	baseDir := t.TempDir()
	states := map[string]ServiceStateInfo{"svc-a": {Name: "svc-a", State: core.ServiceState("up")}}
	server := NewServer(&config.Config{Settings: config.Settings{AuthMode: "none"}})
	server.stateProvider = &fakeStateProvider{states: states}
	server.pathValidator = NewPathValidator(baseDir)
	if err := os.MkdirAll(filepath.Join(baseDir, "services", "svc-a"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	body, _ := json.Marshal(config.EnvFile{Env: map[string]config.EnvVar{"FOO": {Value: "bar"}}})
	resp := doAPICall(t, server, http.MethodPut, "/api/services/svc-a/env", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("save env: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	if _, err := os.Stat(filepath.Join(baseDir, "services", "svc-a", "env.yaml")); err != nil {
		t.Errorf("env file not written: %v", err)
	}

	resp = doAPICall(t, server, http.MethodPut, "/api/services/svc-a/env", []byte("bad"))
	if resp.Code != http.StatusBadRequest {
		t.Errorf("save env bad body: expected 400, got %d", resp.Code)
	}

	resp = doAPICall(t, server, http.MethodPut, "/api/services/ghost/env", body)
	if resp.Code != http.StatusNotFound {
		t.Errorf("save env missing: expected 404, got %d", resp.Code)
	}
}
