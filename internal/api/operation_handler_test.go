package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/extension"
	"github.com/supdorg/supd/internal/watch"
)

// fakeOpRunner 模拟 OperationRunner.triggerer（Begin → 校验参数 → 返回 outcome）。
type fakeOpRunner struct {
	mu      sync.Mutex
	calls   int
	started []string
}

func (f *fakeOpRunner) StartExecution(_ context.Context, opID string, params []byte) (*extension.TriggerOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if opID != "op1" {
		return nil, extension.ErrOperationNotFound
	}
	if len(bytes.TrimSpace(params)) > 0 && bytes.TrimSpace(params)[0] != '{' {
		return nil, fmt.Errorf("%w: params must be a top-level JSON object", extension.ErrOperationParamInvalid)
	}
	f.calls++
	f.started = append(f.started, opID)
	return &extension.TriggerOutcome{ExecutionID: fmt.Sprintf("exec-%d", f.calls), TopicID: fmt.Sprintf("topic-%d", f.calls)}, nil
}

// newOperationTestServer 构造带 OperationProvider 的 API Server。
func newOperationTestServer(t *testing.T) (*Server, *fakeOpRunner) {
	t.Helper()
	enabled := true
	meta := &config.ExtensionMeta{
		Name: "g-ext", Enabled: &enabled, Description: "global ops",
		Actions: []config.Action{{ID: "a1", Label: "检查更新", ButtonStyle: "default", Operations: []string{"op1"}}},
	}
	disco := &watch.DiscoveryResult{
		Services:   map[string]*watch.ServiceEntry{},
		GlobalExts: map[string]*watch.ExtensionEntry{"g-ext": {Name: "g-ext", Meta: meta, ConfigPath: "/x/meta.yaml"}},
		Runtimes:   map[string]string{},
	}
	reg := extension.NewOperationRegistry()
	reg.Rebuild(disco)

	fake := &fakeOpRunner{}
	prov := NewCoreOperationProvider(reg, fake, nil)
	srv := NewServer(nil)
	srv.SetOperationProvider(prov)
	return srv, fake
}

func doReq(srv *Server, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, r)
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode response: %v body=%s", err, w.Body.String())
	}
}

func TestOperationList(t *testing.T) {
	srv, _ := newOperationTestServer(t)
	w := doReq(srv, "GET", "/api/operations", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/operations status = %d, body=%s", w.Code, w.Body.String())
	}
	var cards []OperationCard
	decodeBody(t, w, &cards)
	if len(cards) != 1 || cards[0].ID != "op1" || cards[0].Label != "检查更新" {
		t.Errorf("cards = %+v", cards)
	}
}

func TestOperationDetail(t *testing.T) {
	srv, _ := newOperationTestServer(t)
	w := doReq(srv, "GET", "/api/operations/op1", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/operations/op1 status = %d, body=%s", w.Code, w.Body.String())
	}
	var det OperationDetail
	decodeBody(t, w, &det)
	if det.ID != "op1" || len(det.Registrants) != 1 || det.Registrants[0] != "g-ext" {
		t.Errorf("detail = %+v", det)
	}
	// 未知操作 404。
	w2 := doReq(srv, "GET", "/api/operations/nope", "", nil)
	if w2.Code != http.StatusNotFound {
		t.Errorf("unknown op detail status = %d, want 404", w2.Code)
	}
}

func TestOperationRunSuccess(t *testing.T) {
	srv, fake := newOperationTestServer(t)
	w := doReq(srv, "POST", "/api/operations/op1/run", `{"params":{"force":true}}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("POST run status = %d, body=%s", w.Code, w.Body.String())
	}
	var res RunOperationResult
	decodeBody(t, w, &res)
	if res.ExecutionID != "exec-1" {
		t.Errorf("execution_id = %q, want exec-1", res.ExecutionID)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.started) != 1 {
		t.Errorf("runner triggered %d times, want 1", len(fake.started))
	}
}

func TestOperationRunParamInvalid400(t *testing.T) {
	srv, _ := newOperationTestServer(t)
	w := doReq(srv, "POST", "/api/operations/op1/run", `{"params":[1,2,3]}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid params status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
	var e svcErrResp
	decodeBody(t, w, &e)
	if e.Error.Code != "INVALID_REQUEST" {
		t.Errorf("error code = %q, want INVALID_REQUEST", e.Error.Code)
	}
}

type svcErrResp struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func TestOperationRunUnknown404(t *testing.T) {
	srv, _ := newOperationTestServer(t)
	w := doReq(srv, "POST", "/api/operations/nope/run", `{}`, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown op run status = %d, want 404, body=%s", w.Code, w.Body.String())
	}
}

func TestOperationRunIdempotencyKey(t *testing.T) {
	srv, _ := newOperationTestServer(t)
	// 第一次无 key → 新建 exec-1。
	w1 := doReq(srv, "POST", "/api/operations/op1/run", `{}`, nil)
	var r1 RunOperationResult
	decodeBody(t, w1, &r1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first run status = %d", w1.Code)
	}
	hdr := map[string]string{"Idempotency-Key": "idem-001"}

	// 同 key 第一次 → exec-2；同 key 第二次 → 复用 exec-2。
	w2 := doReq(srv, "POST", "/api/operations/op1/run", `{}`, hdr)
	var r2 RunOperationResult
	decodeBody(t, w2, &r2)
	if r2.ExecutionID != "exec-2" {
		t.Errorf("idempotent first = %q, want exec-2", r2.ExecutionID)
	}
	w3 := doReq(srv, "POST", "/api/operations/op1/run", `{}`, hdr)
	var r3 RunOperationResult
	decodeBody(t, w3, &r3)
	if r3.ExecutionID != r2.ExecutionID {
		t.Errorf("idempotent repeat returned %q, want reuse %q", r3.ExecutionID, r2.ExecutionID)
	}
}

func TestOperationExecutionsListAndDetail404(t *testing.T) {
	srv, _ := newOperationTestServer(t)
	w := doReq(srv, "GET", "/api/operation-executions", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list executions status = %d", w.Code)
	}
	// store nil → 返回空列表。
	if got := strings.TrimSpace(w.Body.String()); got != "[]" && got != "null" {
		t.Errorf("empty executions body = %s", got)
	}
	// 详情不存在 → 404。
	w2 := doReq(srv, "GET", "/api/operation-executions/no-such", "", nil)
	if w2.Code != http.StatusNotFound {
		t.Errorf("unknown execution detail status = %d, want 404", w2.Code)
	}
}