package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/errors"
	"github.com/supdorg/supd/internal/extension"
)

// ---- fake ExtensionProvider（嵌入接口以满足契约，仅覆盖测试所需方法）----

type fakeExtensionProvider struct {
	ExtensionProvider
	exts       map[string]*ExtensionInfo
	createErr  error
	updateErr  error
	deleteErr  error
	saveEnvErr error
	runErr     error
	runResult  *extension.RunResult
	runAction  string
	runService string
	runEnv     map[string]string
	statusErr  error
	statusVal  map[string]any
}

func (f *fakeExtensionProvider) ListExtensions() []ExtensionInfo {
	out := make([]ExtensionInfo, 0, len(f.exts))
	for _, e := range f.exts {
		out = append(out, *e)
	}
	return out
}

func (f *fakeExtensionProvider) GetExtension(name string) (*ExtensionInfo, bool) {
	e, ok := f.exts[name]
	return e, ok
}

func (f *fakeExtensionProvider) CreateExtension(meta *config.ExtensionMeta, service string) error {
	return f.createErr
}

func (f *fakeExtensionProvider) UpdateExtension(name string, meta *config.ExtensionMeta, service string) error {
	return f.updateErr
}

func (f *fakeExtensionProvider) DeleteExtension(name string, service string) error {
	return f.deleteErr
}

func (f *fakeExtensionProvider) SaveExtensionEnv(name string, envData *config.EnvFile, service string) error {
	return f.saveEnvErr
}

func (f *fakeExtensionProvider) RunExtension(ctx context.Context, name string, actionID string, service string, dryRun bool, env map[string]string) (*extension.RunResult, error) {
	f.runAction = actionID
	f.runService = service
	f.runEnv = env
	return f.runResult, f.runErr
}

func (f *fakeExtensionProvider) GetExtensionStatus(name string, service string) (map[string]any, error) {
	if f.statusErr != nil {
		return nil, f.statusErr
	}
	return f.statusVal, nil
}

// newExtensionTestServer 构造一个 extProvider + PathValidator 的 hermetic Server。
// baseDir 由 PathValidator 提供，供 stripWorkdirPrefix / import confirm 使用。
func newExtensionTestServer(t *testing.T, fp *fakeExtensionProvider) *Server {
	t.Helper()
	baseDir := t.TempDir()
	server := NewServer(&config.Config{Settings: config.Settings{AuthMode: "none"}})
	server.extProvider = fp
	server.pathValidator = NewPathValidator(baseDir)
	server.eventRing = NewEventRingBuffer(200)
	return server
}

// validExtMetaJSON 构造一个通过 ValidateExtension 的扩展 meta，供 create/update 使用。
func validExtMetaJSON(t *testing.T) []byte {
	t.Helper()
	meta := config.ExtensionMeta{
		Name:        "myext",
		Version:     "1.0",
		Entry:       "run.sh",
		Concurrency: "replace",
		Actions:     []config.Action{{ID: "a", Label: "Run"}},
	}
	b, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal meta: %v", err)
	}
	return b
}

// buildMetaTarGz 构造含两个 meta.yaml 的 tar.gz：顶层（depth 0）与 nested（depth 1），
// 用于覆盖 handleImportExtension 中按最小深度挑选 meta.yaml 的分支。
func buildMetaTarGz(t *testing.T, metaYAML string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	entries := []struct {
		name    string
		content string
	}{
		{"meta.yaml", metaYAML},
		{"nested/meta.yaml", metaYAML},
	}
	for _, e := range entries {
		hdr := &tar.Header{
			Name:     e.name,
			Mode:     0644,
			Size:     int64(len(e.content)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar write header: %v", err)
		}
		if _, err := tw.Write([]byte(e.content)); err != nil {
			t.Fatalf("tar write: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

// newUploadRequest 构造 multipart 上传请求（用于 import 预览/确认）。
func newUploadRequest(t *testing.T, url, field, filename string, data []byte, extraForm map[string]string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range extraForm {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatalf("write field: %v", err)
		}
	}
	fw, err := mw.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, url, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

// TestExtensionHandlersNilProvider 覆盖各 handler 中 `s.extProvider == nil` 的 500 分支。
func TestExtensionHandlersNilProvider(t *testing.T) {
	server := newExtensionTestServer(t, &fakeExtensionProvider{})
	server.extProvider = nil

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/extensions"},
		{http.MethodGet, "/api/extensions/e"},
		{http.MethodPost, "/api/extensions"},
		{http.MethodPut, "/api/extensions/e"},
		{http.MethodDelete, "/api/extensions/e"},
		{http.MethodPut, "/api/extensions/e/env"},
		{http.MethodPost, "/api/extensions/e/run"},
		{http.MethodGet, "/api/extensions/e/status"},
		{http.MethodGet, "/api/extensions/e/export"},
		{http.MethodPost, "/api/extensions/import"},
		{http.MethodPost, "/api/extensions/import/confirm"},
		{http.MethodGet, "/api/services/svc/extensions"},
		{http.MethodGet, "/api/services/svc/extensions/e"},
		{http.MethodPost, "/api/services/svc/extensions"},
		{http.MethodPut, "/api/services/svc/extensions/e"},
		{http.MethodDelete, "/api/services/svc/extensions/e"},
		{http.MethodPut, "/api/services/svc/extensions/e/env"},
		{http.MethodPost, "/api/services/svc/extensions/e/run"},
	}
	for _, c := range cases {
		resp := doAPICall(t, server, c.method, c.path, nil)
		if resp.Code != http.StatusInternalServerError {
			t.Errorf("%s %s: expected 500 (nil provider), got %d (body: %s)", c.method, c.path, resp.Code, resp.Body.String())
		}
	}
}

// TestListExtensions 覆盖 handleListExtensions：空列表、含 Meta、不含 Meta。
func TestListExtensions(t *testing.T) {
	fp := &fakeExtensionProvider{
		exts: map[string]*ExtensionInfo{
			"ext-with-meta": {
				Name: "ext-with-meta", Version: "1.0", Enabled: true, DisplayState: "active",
				Meta: &config.ExtensionMeta{
					Name:        "ext-with-meta",
					Runtime:     "bash",
					Entry:       "run.sh",
					Concurrency: "replace",
					Actions:     []config.Action{{ID: "a", Label: "Run"}},
					Triggers:    config.Triggers{},
				},
			},
			"ext-no-meta": {
				Name: "ext-no-meta", Version: "0.9", Enabled: false, DisplayState: "disabled",
				Meta: nil,
			},
			// R-06 修复：解析失败的扩展应携带 ConfigErrors 供前端诊断
			"ext-broken": {
				Name:         "ext-broken",
				Enabled:      false,
				DisplayState: "config_error",
				Meta:         nil,
				ConfigErrors: []string{"parse extension meta: yaml: line 1: could not find expected ':'"},
			},
		},
	}
	server := newExtensionTestServer(t, fp)
	resp := doAPICall(t, server, http.MethodGet, "/api/extensions", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET /api/extensions: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	var list []ExtensionSummary
	if err := json.Unmarshal(resp.Body.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 extensions, got %d", len(list))
	}
	// 含 Meta 的项应透传 Triggers/Actions/Concurrency/Runtime
	var withMeta *ExtensionSummary
	var broken *ExtensionSummary
	for i := range list {
		if list[i].Name == "ext-with-meta" {
			withMeta = &list[i]
		}
		if list[i].Name == "ext-broken" {
			broken = &list[i]
		}
	}
	if withMeta == nil {
		t.Fatal("ext-with-meta missing from list")
	}
	if withMeta.Concurrency != "replace" || withMeta.Runtime != "bash" || len(withMeta.Actions) != 1 {
		t.Errorf("meta fields not propagated: %+v", withMeta)
	}
	// R-06：ConfigErrors 应透传到 ExtensionSummary，前端可据此显示错误诊断
	if broken == nil {
		t.Fatal("ext-broken missing from list")
	}
	if len(broken.ConfigErrors) == 0 {
		t.Errorf("expected ConfigErrors to be propagated for broken extension, got empty")
	}
	if broken.DisplayState != "config_error" {
		t.Errorf("expected DisplayState=config_error, got %q", broken.DisplayState)
	}
}

// TestGetExtension 覆盖 handleGetExtension：存在(含Meta)、env_path 推导、无 Meta、不存在。
func TestGetExtension(t *testing.T) {
	baseDir := t.TempDir()
	fp := &fakeExtensionProvider{
		exts: map[string]*ExtensionInfo{
			"ext-a": {
				Name: "ext-a", Version: "1.0", Enabled: true,
				Meta: &config.ExtensionMeta{
					Name: "ext-a", Runtime: "bash", Entry: "run.sh",
					Concurrency: "serialize", Actions: []config.Action{{ID: "a", Label: "Run"}},
				},
				// ConfigPath 落在 baseDir 下、EnvPath 为空 → 触发 env_path 推导
				ConfigPath: filepath.Join(baseDir, "extensions", "ext-a", "meta.yaml"),
				EnvPath:    "",
			},
			"ext-b": {
				Name: "ext-b", Version: "0.9", Enabled: false, Meta: nil,
			},
		},
	}
	server := newExtensionTestServer(t, fp)
	server.pathValidator = NewPathValidator(baseDir)

	// 存在，含 Meta
	resp := doAPICall(t, server, http.MethodGet, "/api/extensions/ext-a", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET ext-a: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	var detail ExtensionDetail
	if err := json.Unmarshal(resp.Body.Bytes(), &detail); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if detail.Concurrency != "serialize" || len(detail.Actions) != 1 {
		t.Errorf("detail meta fields wrong: %+v", detail)
	}
	// env_path 推导：extensions/ext-a/meta.yaml → extensions/ext-a/env.yaml
	if detail.EnvPath != "extensions/ext-a/env.yaml" {
		t.Errorf("derived env_path = %q, want extensions/ext-a/env.yaml", detail.EnvPath)
	}

	// 无 Meta → 200，Actions 应为空
	resp = doAPICall(t, server, http.MethodGet, "/api/extensions/ext-b", nil)
	if resp.Code != http.StatusOK {
		t.Errorf("GET ext-b: expected 200, got %d", resp.Code)
	}

	// 不存在 → 404
	resp = doAPICall(t, server, http.MethodGet, "/api/extensions/ghost", nil)
	if resp.Code != http.StatusNotFound {
		t.Errorf("GET ghost: expected 404, got %d", resp.Code)
	}
}

// TestCreateExtension 覆盖 handleCreateExtension：成功、空body、校验失败、provider错误。
func TestCreateExtension(t *testing.T) {
	server := newExtensionTestServer(t, &fakeExtensionProvider{})

	// 成功 → 201
	resp := doAPICall(t, server, http.MethodPost, "/api/extensions", validExtMetaJSON(t))
	if resp.Code != http.StatusCreated {
		t.Fatalf("POST /api/extensions: expected 201, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// 空 body → 400
	resp = doAPICall(t, server, http.MethodPost, "/api/extensions", []byte(""))
	if resp.Code != http.StatusBadRequest {
		t.Errorf("empty body: expected 400, got %d", resp.Code)
	}

	// 校验失败（name 为空）→ 422
	bad, _ := json.Marshal(config.ExtensionMeta{Version: "1.0", Entry: "run.sh"})
	resp = doAPICall(t, server, http.MethodPost, "/api/extensions", bad)
	if resp.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid meta: expected 422, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// provider 错误 → 500
	server.extProvider = &fakeExtensionProvider{createErr: errors.NewServiceError(errors.ErrInternal, "boom")}
	resp = doAPICall(t, server, http.MethodPost, "/api/extensions", validExtMetaJSON(t))
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("create err: expected 500, got %d", resp.Code)
	}
}

// TestUpdateExtension 覆盖 handleUpdateExtension：完整更新、部分更新(存在/不存在/Meta nil)、校验失败、provider错误。
func TestUpdateExtension(t *testing.T) {
	fp := &fakeExtensionProvider{
		exts: map[string]*ExtensionInfo{
			"ext-a":   {Name: "ext-a", Version: "1.0", Enabled: true, Meta: &config.ExtensionMeta{Name: "ext-a", Entry: "run.sh"}},
			"ext-nil": {Name: "ext-nil", Version: "1.0", Enabled: true, Meta: nil},
		},
	}
	server := newExtensionTestServer(t, fp)

	// 完整更新成功 → 200
	full, _ := json.Marshal(config.ExtensionMeta{Name: "ext-a", Version: "2.0", Entry: "run.sh", Concurrency: "replace", Actions: []config.Action{{ID: "a", Label: "Run"}}})
	resp := doAPICall(t, server, http.MethodPut, "/api/extensions/ext-a", full)
	if resp.Code != http.StatusOK {
		t.Fatalf("PUT full: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// 部分更新：存在 → 仅切换 enabled → 200
	partial, _ := json.Marshal(map[string]any{"enabled": false})
	resp = doAPICall(t, server, http.MethodPut, "/api/extensions/ext-a", partial)
	if resp.Code != http.StatusOK {
		t.Errorf("PUT partial: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	var d ExtensionDetail
	json.Unmarshal(resp.Body.Bytes(), &d)
	if d.Enabled {
		t.Errorf("partial update should have toggled enabled to false, got true")
	}

	// 部分更新：existing.Meta == nil → 200（直接透传，不报错）
	resp = doAPICall(t, server, http.MethodPut, "/api/extensions/ext-nil", partial)
	if resp.Code != http.StatusOK {
		t.Errorf("PUT partial nil meta: expected 200, got %d", resp.Code)
	}

	// 部分更新：不存在 → 404
	resp = doAPICall(t, server, http.MethodPut, "/api/extensions/ghost", partial)
	if resp.Code != http.StatusNotFound {
		t.Errorf("PUT partial ghost: expected 404, got %d", resp.Code)
	}

	// 完整更新校验失败（entry 为空）→ 422
	badFull, _ := json.Marshal(config.ExtensionMeta{Name: "ext-a", Version: "2.0"})
	resp = doAPICall(t, server, http.MethodPut, "/api/extensions/ext-a", badFull)
	if resp.Code != http.StatusUnprocessableEntity {
		t.Errorf("PUT full invalid: expected 422, got %d", resp.Code)
	}

	// provider 错误 → 500
	server.extProvider = &fakeExtensionProvider{
		exts:      fp.exts,
		updateErr: errors.NewServiceError(errors.ErrInternal, "boom"),
	}
	resp = doAPICall(t, server, http.MethodPut, "/api/extensions/ext-a", full)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("PUT update err: expected 500, got %d", resp.Code)
	}
}

// TestDeleteExtension 覆盖 handleDeleteExtension：成功 204、不存在 404、provider错误 500。
func TestDeleteExtension(t *testing.T) {
	fp := &fakeExtensionProvider{
		exts: map[string]*ExtensionInfo{"ext-a": {Name: "ext-a"}},
	}
	server := newExtensionTestServer(t, fp)

	// 成功 → 204
	resp := doAPICall(t, server, http.MethodDelete, "/api/extensions/ext-a", nil)
	if resp.Code != http.StatusNoContent {
		t.Fatalf("DELETE: expected 204, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// 不存在（ErrExtensionNotFound）→ 404（经 respondProviderError 映射）
	server.extProvider = &fakeExtensionProvider{deleteErr: errors.NewServiceError(errors.ErrExtensionNotFound, "not found")}
	resp = doAPICall(t, server, http.MethodDelete, "/api/extensions/ghost", nil)
	if resp.Code != http.StatusNotFound {
		t.Errorf("DELETE ghost: expected 404, got %d", resp.Code)
	}

	// 其他错误 → 500
	server.extProvider = &fakeExtensionProvider{deleteErr: errors.NewServiceError(errors.ErrInternal, "boom")}
	resp = doAPICall(t, server, http.MethodDelete, "/api/extensions/ghost", nil)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("DELETE err: expected 500, got %d", resp.Code)
	}
}

// TestSaveExtensionEnv 覆盖 handleSaveExtensionEnv：成功、空body、provider错误。
func TestSaveExtensionEnv(t *testing.T) {
	server := newExtensionTestServer(t, &fakeExtensionProvider{})

	body, _ := json.Marshal(config.EnvFile{Env: map[string]config.EnvVar{"FOO": {Value: "bar"}}})
	resp := doAPICall(t, server, http.MethodPut, "/api/extensions/ext-a/env", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("PUT env: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// 空 body → 400
	resp = doAPICall(t, server, http.MethodPut, "/api/extensions/ext-a/env", []byte(""))
	if resp.Code != http.StatusBadRequest {
		t.Errorf("empty body: expected 400, got %d", resp.Code)
	}

	// provider 错误 → 500
	server.extProvider = &fakeExtensionProvider{saveEnvErr: errors.NewServiceError(errors.ErrInternal, "boom")}
	resp = doAPICall(t, server, http.MethodPut, "/api/extensions/ext-a/env", body)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("env err: expected 500, got %d", resp.Code)
	}
}

// TestRunExtension 覆盖 handleRunExtension：默认action、指定action、dry_run、action不存在、不存在、body错误、provider错误。
func TestRunExtension(t *testing.T) {
	fp := &fakeExtensionProvider{
		exts: map[string]*ExtensionInfo{
			"ext-a": {
				Name: "ext-a", Version: "1.0", Enabled: true,
				Meta: &config.ExtensionMeta{Name: "ext-a", Entry: "run.sh", Actions: []config.Action{{ID: "a", Label: "Run"}}},
			},
			"ext-noact": {
				Name: "ext-noact", Version: "1.0", Enabled: true,
				Meta: &config.ExtensionMeta{Name: "ext-noact", Entry: "run.sh", Actions: []config.Action{{ID: "a", Label: "Run"}}},
			},
		},
		runResult: &extension.RunResult{},
	}
	server := newExtensionTestServer(t, fp)

	// 默认 action（空 body）→ 200
	resp := doAPICall(t, server, http.MethodPost, "/api/extensions/ext-a/run", []byte(""))
	if resp.Code != http.StatusOK {
		t.Fatalf("run default: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// 指定合法 action → 200
	resp = doAPICall(t, server, http.MethodPost, "/api/extensions/ext-a/run", mustJSON(RunExtensionRequest{Action: "a"}))
	if resp.Code != http.StatusOK {
		t.Errorf("run action a: expected 200, got %d", resp.Code)
	}

	// dry_run 通过 query → 200
	resp = doAPICall(t, server, http.MethodPost, "/api/extensions/ext-a/run?dry_run=true", []byte(""))
	if resp.Code != http.StatusOK {
		t.Errorf("run dry_run: expected 200, got %d", resp.Code)
	}

	// action 不存在 → 400 field error
	resp = doAPICall(t, server, http.MethodPost, "/api/extensions/ext-a/run", mustJSON(RunExtensionRequest{Action: "zzz"}))
	if resp.Code != http.StatusBadRequest {
		t.Errorf("run unknown action: expected 400, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// 扩展不存在 → 404
	resp = doAPICall(t, server, http.MethodPost, "/api/extensions/ghost/run", []byte(""))
	if resp.Code != http.StatusNotFound {
		t.Errorf("run ghost: expected 404, got %d", resp.Code)
	}

	// body 解析错误 → 400
	resp = doAPICall(t, server, http.MethodPost, "/api/extensions/ext-a/run", []byte("not-json"))
	if resp.Code != http.StatusBadRequest {
		t.Errorf("run bad body: expected 400, got %d", resp.Code)
	}

	// provider 错误 → 200（ErrExtensionFailed 映射为 200，规格约定）
	server.extProvider = &fakeExtensionProvider{
		exts:   fp.exts,
		runErr: errors.NewServiceError(errors.ErrExtensionFailed, "boom"),
	}
	resp = doAPICall(t, server, http.MethodPost, "/api/extensions/ext-a/run", []byte(""))
	if resp.Code != http.StatusOK {
		t.Errorf("run provider err: expected 200, got %d", resp.Code)
	}
}

// TestGetExtensionStatus 覆盖 handleGetExtensionStatus：成功、错误 404。
func TestGetExtensionStatus(t *testing.T) {
	fp := &fakeExtensionProvider{
		exts:      map[string]*ExtensionInfo{"ext-a": {Name: "ext-a"}},
		statusVal: map[string]any{"state": "idle"},
	}
	server := newExtensionTestServer(t, fp)

	resp := doAPICall(t, server, http.MethodGet, "/api/extensions/ext-a/status", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	server.extProvider = &fakeExtensionProvider{
		exts:      fp.exts,
		statusErr: errors.NewServiceError(errors.ErrExtensionNotFound, "nf"),
	}
	resp = doAPICall(t, server, http.MethodGet, "/api/extensions/ext-a/status", nil)
	if resp.Code != http.StatusNotFound {
		t.Errorf("status err: expected 404, got %d", resp.Code)
	}
}

// TestExportExtension 覆盖 handleExportExtension：成功(打包gzip)、不存在404、目录不存在404。
func TestExportExtension(t *testing.T) {
	extDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(extDir, "meta.yaml"), []byte("name: x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	fp := &fakeExtensionProvider{
		exts: map[string]*ExtensionInfo{
			"ext-x": {Name: "ext-x", ConfigPath: filepath.Join(extDir, "meta.yaml")},
		},
	}
	server := newExtensionTestServer(t, fp)

	// 成功 → 200 + application/gzip
	resp := doAPICall(t, server, http.MethodGet, "/api/extensions/ext-x/export", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("export: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	if ct := resp.Header().Get("Content-Type"); ct != "application/gzip" {
		t.Errorf("export content-type = %q, want application/gzip", ct)
	}

	// 不存在 → 404
	resp = doAPICall(t, server, http.MethodGet, "/api/extensions/ghost/export", nil)
	if resp.Code != http.StatusNotFound {
		t.Errorf("export ghost: expected 404, got %d", resp.Code)
	}

	// 目录不存在 → 404
	server.extProvider = &fakeExtensionProvider{
		exts: map[string]*ExtensionInfo{
			"ext-bad": {Name: "ext-bad", ConfigPath: filepath.Join(t.TempDir(), "nope", "meta.yaml")},
		},
	}
	resp = doAPICall(t, server, http.MethodGet, "/api/extensions/ext-bad/export", nil)
	if resp.Code != http.StatusNotFound {
		t.Errorf("export missing dir: expected 404, got %d", resp.Code)
	}
}

// TestImportExtensionPreview 覆盖 handleImportExtension：缺文件400、有效归档200、本地已存在200、非法meta.yaml 422。
func TestImportExtensionPreview(t *testing.T) {
	server := newExtensionTestServer(t, &fakeExtensionProvider{})

	// 合法 multipart 但缺 file 字段 → ParseMultipartForm 成功、FormFile 失败 → 400
	// （注意：ParseMultipartForm 自身失败会被映射为 413 ErrFileTooLarge，此处专门覆盖缺失文件的 400 分支）
	var noFileBuf bytes.Buffer
	nfw := multipart.NewWriter(&noFileBuf)
	if err := nfw.WriteField("name", "myext"); err != nil {
		t.Fatalf("write field: %v", err)
	}
	nfw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/extensions/import", &noFileBuf)
	req.Header.Set("Content-Type", nfw.FormDataContentType())
	rec := httptest.NewRecorder()
	server.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("import no file: expected 400, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	// 有效归档 → 200 + 预览
	metaYAML := "name: myext\nversion: \"1.0\"\nentry: run.sh\nconcurrency: replace\nactions:\n  - id: a\n    label: Run\n"
	archiveData := buildMetaTarGz(t, metaYAML)
	req = newUploadRequest(t, "/api/extensions/import", "file", "myext.tar.gz", archiveData, nil)
	rec = httptest.NewRecorder()
	server.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("import preview: expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	var prev ExtensionImportPreviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &prev); err != nil {
		t.Fatalf("unmarshal preview: %v", err)
	}
	if prev.Name != "myext" || prev.ArchiveVer != "1.0" {
		t.Errorf("preview = %+v, want name myext / ver 1.0", prev)
	}
	if prev.ExistsLocal {
		t.Errorf("preview ExistsLocal should be false when not seeded")
	}

	// 本地已存在 → ExistsLocal true + LocalVer
	server.extProvider = &fakeExtensionProvider{
		exts: map[string]*ExtensionInfo{"myext": {Name: "myext", Version: "0.5"}},
	}
	req = newUploadRequest(t, "/api/extensions/import", "file", "myext.tar.gz", archiveData, nil)
	rec = httptest.NewRecorder()
	server.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("import preview exists: expected 200, got %d", rec.Code)
	}
	json.Unmarshal(rec.Body.Bytes(), &prev)
	if !prev.ExistsLocal || prev.LocalVer != "0.5" {
		t.Errorf("preview exists = %+v, want ExistsLocal true / LocalVer 0.5", prev)
	}

	// 非法 meta.yaml（缺 entry）→ 422
	badYAML := "name: myext\nversion: \"1.0\"\n"
	badArchive := buildMetaTarGz(t, badYAML)
	req = newUploadRequest(t, "/api/extensions/import", "file", "bad.tar.gz", badArchive, nil)
	rec = httptest.NewRecorder()
	server.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("import invalid meta: expected 422, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestImportExtensionConfirm 覆盖 handleImportExtensionConfirm：缺name400、非法name400、成功201、解包失败500。
func TestImportExtensionConfirm(t *testing.T) {
	server := newExtensionTestServer(t, &fakeExtensionProvider{})

	// 缺 name → 400 field error
	archiveData := buildMetaTarGz(t, "name: myext\nversion: \"1.0\"\nentry: run.sh\n")
	req := newUploadRequest(t, "/api/extensions/import/confirm", "file", "myext.tar.gz", archiveData, nil)
	rec := httptest.NewRecorder()
	server.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("confirm no name: expected 400, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	// 非法 name → 400
	req = newUploadRequest(t, "/api/extensions/import/confirm", "file", "myext.tar.gz", archiveData, map[string]string{"name": "MyExt"})
	rec = httptest.NewRecorder()
	server.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("confirm bad name: expected 400, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	// 成功 → 201（watchProvider 未配置，triggerReload 返回 nil，走跳过分支）
	req = newUploadRequest(t, "/api/extensions/import/confirm", "file", "myext.tar.gz", archiveData, map[string]string{"name": "myext"})
	rec = httptest.NewRecorder()
	server.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("confirm success: expected 201, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	// 校验目标目录确实被创建并解包
	targetDir := filepath.Join(server.pathValidator.baseDir, "extensions", "myext")
	if _, err := os.Stat(targetDir); err != nil {
		t.Errorf("confirm did not create target dir: %v", err)
	}

	// 解包失败（非法归档）→ 500
	req = newUploadRequest(t, "/api/extensions/import/confirm", "file", "broken.tar.gz", []byte("not-a-real-archive"), map[string]string{"name": "myext"})
	rec = httptest.NewRecorder()
	server.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("confirm broken archive: expected 500, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestServiceScopedExtensionHandlers 覆盖服务级扩展 handler：list/get/create/update/delete/saveEnv/run。
func TestServiceScopedExtensionHandlers(t *testing.T) {
	fp := &fakeExtensionProvider{
		exts: map[string]*ExtensionInfo{
			"ext-a": {Name: "ext-a", Version: "1.0", Service: "svc-a", Enabled: true, Meta: &config.ExtensionMeta{Name: "ext-a", Entry: "run.sh", Actions: []config.Action{{ID: "a", Label: "R"}}}},
			"ext-b": {Name: "ext-b", Version: "1.0", Service: "svc-b", Enabled: true, Meta: &config.ExtensionMeta{Name: "ext-b", Entry: "run.sh"}},
		},
		runResult: &extension.RunResult{},
	}
	server := newExtensionTestServer(t, fp)
	fp.exts["ext-a"].EnvPath = filepath.Join(server.pathValidator.baseDir, "services", "svc-a", "extensions", "ext-a", "env.yaml")

	// list（仅返回 svc-a 的扩展）
	resp := doAPICall(t, server, http.MethodGet, "/api/services/svc-a/extensions", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("list svc ext: expected 200, got %d", resp.Code)
	}
	var list []ExtensionSummary
	json.Unmarshal(resp.Body.Bytes(), &list)
	if len(list) != 1 || list[0].Name != "ext-a" {
		t.Errorf("svc-a ext list = %+v, want [ext-a]", list)
	}
	if len(list) == 1 && list[0].EnvPath != "services/svc-a/extensions/ext-a/env.yaml" {
		t.Errorf("svc-a ext env_path = %q", list[0].EnvPath)
	}

	// get（service 匹配）→ 200
	resp = doAPICall(t, server, http.MethodGet, "/api/services/svc-a/extensions/ext-a", nil)
	if resp.Code != http.StatusOK {
		t.Errorf("get svc ext match: expected 200, got %d", resp.Code)
	}

	// get（service 不匹配）→ 404
	resp = doAPICall(t, server, http.MethodGet, "/api/services/svc-a/extensions/ext-b", nil)
	if resp.Code != http.StatusNotFound {
		t.Errorf("get svc ext mismatch: expected 404, got %d", resp.Code)
	}

	// create → 201
	resp = doAPICall(t, server, http.MethodPost, "/api/services/svc-a/extensions", validExtMetaJSON(t))
	if resp.Code != http.StatusCreated {
		t.Errorf("create svc ext: expected 201, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// update → 200
	full, _ := json.Marshal(config.ExtensionMeta{Name: "ext-a", Version: "2.0", Entry: "run.sh", Concurrency: "replace", Actions: []config.Action{{ID: "a", Label: "Run"}}})
	resp = doAPICall(t, server, http.MethodPut, "/api/services/svc-a/extensions/ext-a", full)
	if resp.Code != http.StatusOK {
		t.Errorf("update svc ext: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// save env → 200
	envBody, _ := json.Marshal(config.EnvFile{Env: map[string]config.EnvVar{"FOO": {Value: "bar"}}})
	resp = doAPICall(t, server, http.MethodPut, "/api/services/svc-a/extensions/ext-a/env", envBody)
	if resp.Code != http.StatusOK {
		t.Errorf("save svc ext env: expected 200, got %d", resp.Code)
	}

	// delete → 204
	resp = doAPICall(t, server, http.MethodDelete, "/api/services/svc-a/extensions/ext-a", nil)
	if resp.Code != http.StatusNoContent {
		t.Errorf("delete svc ext: expected 204, got %d", resp.Code)
	}

	// run：action 与临时环境变量透传给 provider
	runBody := []byte(`{"action":"a","env":{"FOO":"temporary"}}`)
	resp = doAPICall(t, server, http.MethodPost, "/api/services/svc-a/extensions/ext-a/run", runBody)
	if resp.Code != http.StatusOK {
		t.Errorf("run svc ext: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	if fp.runAction != "a" || fp.runService != "svc-a" || fp.runEnv["FOO"] != "temporary" {
		t.Errorf("run svc ext args = action:%q service:%q env:%v", fp.runAction, fp.runService, fp.runEnv)
	}

	// 不存在的 action 必须拒绝，不能静默回退到首个 action
	resp = doAPICall(t, server, http.MethodPost, "/api/services/svc-a/extensions/ext-a/run", []byte(`{"action":"missing"}`))
	if resp.Code != http.StatusBadRequest {
		t.Errorf("run svc ext unknown action: expected 400, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// 服务不匹配时按不存在处理
	resp = doAPICall(t, server, http.MethodPost, "/api/services/other/extensions/ext-a/run", []byte(`{"action":"a"}`))
	if resp.Code != http.StatusNotFound {
		t.Errorf("run svc ext mismatch: expected 404, got %d (body: %s)", resp.Code, resp.Body.String())
	}
}

// mustJSON 小工具：将 v 序列化为 JSON（测试内失败时直接 fatal）。
func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
