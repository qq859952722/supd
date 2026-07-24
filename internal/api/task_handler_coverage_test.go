package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/errors"
	"github.com/supdorg/supd/internal/extension"
)

// fakeTaskProvider 嵌入接口以满足契约，覆盖 task_handler 测试所需方法。
type fakeTaskProvider struct {
	TaskProvider
	runs     []*extension.RunResult
	runByID  map[string]*extension.RunResult
	cancelErr error
	deleteErr error
	clearErr  error
	logs     []string
	logPos   int64
	logErr   error
	cleared  int
}

func (f *fakeTaskProvider) ListRuns(filter extension.RunFilter) []*extension.RunResult { return f.runs }
func (f *fakeTaskProvider) GetRun(runID string) *extension.RunResult                   { return f.runByID[runID] }
func (f *fakeTaskProvider) CancelRun(runID string) error                                { return f.cancelErr }
func (f *fakeTaskProvider) GetRunLogs(runID string, sincePos int64) ([]string, int64, error) {
	return f.logs, f.logPos, f.logErr
}
func (f *fakeTaskProvider) DeleteRunLogs(runID string) error { return f.deleteErr }
func (f *fakeTaskProvider) ClearRuns(filter extension.RunFilter) int {
	f.cleared++
	return f.cleared
}

func newTaskTestServer(t *testing.T, fp *fakeTaskProvider) *Server {
	t.Helper()
	server := NewServer(&config.Config{Settings: config.Settings{AuthMode: "none"}})
	server.taskProvider = fp
	server.longPollLimiter = NewLongPollLimiter(50, 5)
	server.eventRing = NewEventRingBuffer(200)
	return server
}

// TestListRuns 覆盖 handleListRuns：成功、各类过滤参数、非法limit、limit>1000截断、nil provider。
func TestListRuns(t *testing.T) {
	fp := &fakeTaskProvider{
		runs:    []*extension.RunResult{{RunID: "r1"}, {RunID: "r2"}},
		runByID: map[string]*extension.RunResult{"r1": {RunID: "r1"}, "r2": {RunID: "r2"}},
	}
	server := newTaskTestServer(t, fp)

	resp := doAPICall(t, server, http.MethodGet, "/api/extensions/runs", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("list runs: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// 各类过滤参数（status/extension/service/trigger_type/limit）
	resp = doAPICall(t, server, http.MethodGet, "/api/extensions/runs?status=success&extension=e1&service=s1&trigger_type=on_demand&limit=10", nil)
	if resp.Code != http.StatusOK {
		t.Errorf("list runs with filters: expected 200, got %d", resp.Code)
	}

	// 参数别名
	resp = doAPICall(t, server, http.MethodGet, "/api/extensions/runs?extension_name=e1&service_name=s1", nil)
	if resp.Code != http.StatusOK {
		t.Errorf("list runs alias params: expected 200, got %d", resp.Code)
	}

	// 非法 limit → 400
	resp = doAPICall(t, server, http.MethodGet, "/api/extensions/runs?limit=abc", nil)
	if resp.Code != http.StatusBadRequest {
		t.Errorf("list runs bad limit: expected 400, got %d", resp.Code)
	}

	// limit>1000 截断（不报错，仅截断）→ 200
	resp = doAPICall(t, server, http.MethodGet, "/api/extensions/runs?limit=5000", nil)
	if resp.Code != http.StatusOK {
		t.Errorf("list runs limit clamp: expected 200, got %d", resp.Code)
	}

	// nil provider → 500
	server.taskProvider = nil
	resp = doAPICall(t, server, http.MethodGet, "/api/extensions/runs", nil)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("list runs nil: expected 500, got %d", resp.Code)
	}
}

// TestGetRun 覆盖 handleGetRun：成功、不存在404、nil provider。
func TestGetRun(t *testing.T) {
	fp := &fakeTaskProvider{runByID: map[string]*extension.RunResult{"r1": {RunID: "r1", State: extension.TaskSuccess}}}
	server := newTaskTestServer(t, fp)

	resp := doAPICall(t, server, http.MethodGet, "/api/extensions/runs/r1", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("get run: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	resp = doAPICall(t, server, http.MethodGet, "/api/extensions/runs/nope", nil)
	if resp.Code != http.StatusNotFound {
		t.Errorf("get run missing: expected 404, got %d", resp.Code)
	}

	server.taskProvider = nil
	resp = doAPICall(t, server, http.MethodGet, "/api/extensions/runs/r1", nil)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("get run nil: expected 500, got %d", resp.Code)
	}
}

// TestGetRunLogs 覆盖 handleGetRunLogs：成功、不存在404、非法since_pos、wait分支(立即返回)、nil provider。
func TestGetRunLogs(t *testing.T) {
	running := &extension.RunResult{RunID: "r1", State: extension.TaskRunning}
	fp := &fakeTaskProvider{
		runByID: map[string]*extension.RunResult{"r1": running},
		logs:    []string{"line"},
		logPos:  5,
	}
	server := newTaskTestServer(t, fp)

	// 普通请求（无 wait）
	resp := doAPICall(t, server, http.MethodGet, "/api/extensions/runs/r1/logs", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("run logs: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// wait=true 但 logs 非空 → 立即返回（不进入 30s 挂起）
	resp = doAPICall(t, server, http.MethodGet, "/api/extensions/runs/r1/logs?wait=true&since_pos=0", nil)
	if resp.Code != http.StatusOK {
		t.Errorf("run logs wait: expected 200, got %d", resp.Code)
	}

	// 不存在 → 404
	resp = doAPICall(t, server, http.MethodGet, "/api/extensions/runs/nope/logs", nil)
	if resp.Code != http.StatusNotFound {
		t.Errorf("run logs missing: expected 404, got %d", resp.Code)
	}

	// 非法 since_pos → 400
	resp = doAPICall(t, server, http.MethodGet, "/api/extensions/runs/r1/logs?since_pos=abc", nil)
	if resp.Code != http.StatusBadRequest {
		t.Errorf("run logs bad since: expected 400, got %d", resp.Code)
	}

	// provider 错误 → 500
	server.taskProvider = &fakeTaskProvider{runByID: map[string]*extension.RunResult{"r1": running}, logErr: errors.NewServiceError(errors.ErrInternal, "boom")}
	resp = doAPICall(t, server, http.MethodGet, "/api/extensions/runs/r1/logs", nil)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("run logs err: expected 500, got %d", resp.Code)
	}

	server.taskProvider = nil
	resp = doAPICall(t, server, http.MethodGet, "/api/extensions/runs/r1/logs", nil)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("run logs nil: expected 500, got %d", resp.Code)
	}
}

// TestCancelRun 覆盖 handleCancelRun：成功204、已终态409、nil provider。
func TestCancelRun(t *testing.T) {
	fp := &fakeTaskProvider{runByID: map[string]*extension.RunResult{"r1": {RunID: "r1"}}}
	server := newTaskTestServer(t, fp)

	resp := doAPICall(t, server, http.MethodPost, "/api/extensions/runs/r1/cancel", nil)
	if resp.Code != http.StatusNoContent {
		t.Fatalf("cancel run: expected 204, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// 已终态 → 409
	server.taskProvider = &fakeTaskProvider{
		runByID:   map[string]*extension.RunResult{"r1": {RunID: "r1"}},
		cancelErr: errors.NewServiceError(errors.ErrRunAlreadyDone, "already done"),
	}
	resp = doAPICall(t, server, http.MethodPost, "/api/extensions/runs/r1/cancel", nil)
	if resp.Code != http.StatusConflict {
		t.Errorf("cancel already done: expected 409, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	server.taskProvider = nil
	resp = doAPICall(t, server, http.MethodPost, "/api/extensions/runs/r1/cancel", nil)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("cancel nil: expected 500, got %d", resp.Code)
	}
}

// TestDeleteRunLogs 覆盖 handleDeleteRunLogs：成功204、nil provider。
func TestDeleteRunLogs(t *testing.T) {
	server := newTaskTestServer(t, &fakeTaskProvider{})
	resp := doAPICall(t, server, http.MethodDelete, "/api/extensions/runs/r1/logs", nil)
	if resp.Code != http.StatusNoContent {
		t.Fatalf("delete run logs: expected 204, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	server.taskProvider = nil
	resp = doAPICall(t, server, http.MethodDelete, "/api/extensions/runs/r1/logs", nil)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("delete run logs nil: expected 500, got %d", resp.Code)
	}
}

// TestClearRuns 覆盖 handleClearRuns：成功、过滤参数、nil provider。
func TestClearRuns(t *testing.T) {
	fp := &fakeTaskProvider{cleared: 3}
	server := newTaskTestServer(t, fp)
	resp := doAPICall(t, server, http.MethodDelete, "/api/extensions/runs?extension_name=e1&service_name=s1", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("clear runs: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	var out map[string]int
	json.Unmarshal(resp.Body.Bytes(), &out)
	if out["deleted"] != 4 { // ClearRuns increments cleared (3→4)
		t.Errorf("clear runs deleted = %d, want 4", out["deleted"])
	}

	// 参数别名
	resp = doAPICall(t, server, http.MethodDelete, "/api/extensions/runs?extension=e1&service=s1", nil)
	if resp.Code != http.StatusOK {
		t.Errorf("clear runs alias: expected 200, got %d", resp.Code)
	}

	server.taskProvider = nil
	resp = doAPICall(t, server, http.MethodDelete, "/api/extensions/runs", nil)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("clear runs nil: expected 500, got %d", resp.Code)
	}
}
