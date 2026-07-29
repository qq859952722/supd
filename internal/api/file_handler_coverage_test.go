package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/supdorg/supd/internal/config"
)

// newFileTestServer 构造一个使用真实 OsFileProvider 的 API Server（hermetic，基于临时目录）。
// 用于覆盖 file_handler.go 的全部 handler（文件树/读写/创建/删除/移动/历史/回滚/校验/快照/上传）。
func newFileTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	baseDir := t.TempDir()
	historyDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(baseDir, "services", "test"), 0755); err != nil {
		t.Fatalf("mkdir writable tree: %v", err)
	}
	fp := &OsFileProvider{
		BaseDir:       baseDir,
		PathValidator: NewPathValidator(baseDir),
		HistoryDir:    historyDir,
		MaxVersions:   10,
	}
	cfg := &config.Config{Settings: config.Settings{AuthMode: "none"}}
	server := NewServer(cfg)
	server.fileProvider = fp
	server.pathValidator = NewPathValidator(baseDir)
	return server, baseDir
}

// TestFileTree 覆盖 handleFileTree：根目录、子目录、无效路径
func TestFileTree(t *testing.T) {
	server, baseDir := newFileTestServer(t)

	// 创建一个白名单内子目录与文件，供文件树展示
	subDir := filepath.Join(baseDir, "services", "test", "conf.d")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "a.yaml"), []byte("k: v"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// 根目录文件树
	resp := doAPICall(t, server, http.MethodGet, "/api/files/tree", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET /api/files/tree: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// 带 path 参数（子目录）的文件树
	resp = doAPICall(t, server, http.MethodGet, "/api/files/tree?path=services/test/conf.d", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET /api/files/tree?path=services/test/conf.d: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// 无效路径（含 ..）→ 403
	resp = doAPICall(t, server, http.MethodGet, "/api/files/tree?path=..%2F..%2Fetc", nil)
	if resp.Code != http.StatusForbidden {
		t.Errorf("GET /api/files/tree?path=..: expected 403, got %d (body: %s)", resp.Code, resp.Body.String())
	}
}

// TestReadFile 覆盖 handleReadFile：成功、缺 path、文件不存在、超出读取上限
func TestReadFile(t *testing.T) {
	server, baseDir := newFileTestServer(t)
	rel := "services/test/note.txt"
	full := filepath.Join(baseDir, rel)
	if err := os.WriteFile(full, []byte("hello supd"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// 成功读取
	resp := doAPICall(t, server, http.MethodGet, "/api/files?path="+rel, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET /api/files: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	var fc FileContentResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &fc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if fc.Content != "hello supd" {
		t.Errorf("content = %q, want %q", fc.Content, "hello supd")
	}

	// 缺 path → 400
	resp = doAPICall(t, server, http.MethodGet, "/api/files", nil)
	if resp.Code != http.StatusBadRequest {
		t.Errorf("GET /api/files (no path): expected 400, got %d", resp.Code)
	}

	// 白名单内文件不存在 → 404
	resp = doAPICall(t, server, http.MethodGet, "/api/files?path=services/test/missing.txt", nil)
	if resp.Code != http.StatusNotFound {
		t.Errorf("GET missing file: expected 404, got %d", resp.Code)
	}

	// 超出读取上限（>10MB）→ 413
	big := make([]byte, MaxReadFileSize+1)
	bigRel := "services/test/big.bin"
	if err := os.WriteFile(filepath.Join(baseDir, bigRel), big, 0644); err != nil {
		t.Fatalf("write big: %v", err)
	}
	resp = doAPICall(t, server, http.MethodGet, "/api/files?path="+bigRel, nil)
	if resp.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("GET /api/files?path=big.bin: expected 413, got %d (body: %s)", resp.Code, resp.Body.String())
	}
}

// TestWriteFile 覆盖 handleWriteFile：成功、缺 path
func TestWriteFile(t *testing.T) {
	server, baseDir := newFileTestServer(t)
	rel := "services/test/out.txt"
	body, _ := json.Marshal(map[string]string{"content": "written"})

	resp := doAPICall(t, server, http.MethodPut, "/api/files?path="+rel, body)
	if resp.Code != http.StatusOK {
		t.Fatalf("PUT /api/files: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	var writeResp FileWriteResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &writeResp); err != nil {
		t.Fatalf("unmarshal write response: %v", err)
	}
	if !writeResp.Saved || writeResp.ReloadState != "skipped" || writeResp.RequiresRestart != "unknown" {
		t.Fatalf("unexpected write response: %+v", writeResp)
	}
	got, err := os.ReadFile(filepath.Join(baseDir, rel))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "written" {
		t.Errorf("file content = %q, want %q", string(got), "written")
	}

	// 缺 path → 400
	resp = doAPICall(t, server, http.MethodPut, "/api/files", body)
	if resp.Code != http.StatusBadRequest {
		t.Errorf("PUT /api/files (no path): expected 400, got %d", resp.Code)
	}
}

// TestCreateFile 覆盖 handleCreateFile：普通文件、目录、缺 path
func TestCreateFile(t *testing.T) {
	server, baseDir := newFileTestServer(t)

	// 创建普通文件
	rel := "services/test/new.txt"
	body, _ := json.Marshal(map[string]string{"content": "created"})
	resp := doAPICall(t, server, http.MethodPost, "/api/files?path="+rel, body)
	if resp.Code != http.StatusCreated {
		t.Fatalf("POST /api/files: expected 201, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	if _, err := os.Stat(filepath.Join(baseDir, rel)); err != nil {
		t.Errorf("file not created: %v", err)
	}

	// 在可写白名单内创建目录
	dirRel := "services/test/mydir"
	dirBody, _ := json.Marshal(map[string]any{"content": "", "is_dir": true})
	resp = doAPICall(t, server, http.MethodPost, "/api/files?path="+dirRel, dirBody)
	if resp.Code != http.StatusCreated {
		t.Fatalf("POST /api/files (dir): expected 201, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	info, err := os.Stat(filepath.Join(baseDir, dirRel))
	if err != nil || !info.IsDir() {
		t.Errorf("dir not created: %v", err)
	}

	// 缺 path → 400
	resp = doAPICall(t, server, http.MethodPost, "/api/files", body)
	if resp.Code != http.StatusBadRequest {
		t.Errorf("POST /api/files (no path): expected 400, got %d", resp.Code)
	}
}

// TestDeleteFile 覆盖 handleDeleteFile：成功、文件不存在、缺 path
func TestDeleteFile(t *testing.T) {
	server, baseDir := newFileTestServer(t)
	rel := "services/test/todelete.txt"
	if err := os.WriteFile(filepath.Join(baseDir, rel), []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	resp := doAPICall(t, server, http.MethodDelete, "/api/files?path="+rel, nil)
	if resp.Code != http.StatusNoContent {
		t.Fatalf("DELETE /api/files: expected 204, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	if _, err := os.Stat(filepath.Join(baseDir, rel)); !os.IsNotExist(err) {
		t.Errorf("file should be deleted")
	}

	// 删除不存在文件 → 404
	resp = doAPICall(t, server, http.MethodDelete, "/api/files?path=services/test/ghost.txt", nil)
	if resp.Code != http.StatusNotFound {
		t.Errorf("DELETE missing: expected 404, got %d", resp.Code)
	}

	// 缺 path → 400
	resp = doAPICall(t, server, http.MethodDelete, "/api/files", nil)
	if resp.Code != http.StatusBadRequest {
		t.Errorf("DELETE (no path): expected 400, got %d", resp.Code)
	}
}

// TestMoveFile 覆盖 handleMoveFile：成功、缺 destination、缺 path
func TestMoveFile(t *testing.T) {
	server, baseDir := newFileTestServer(t)
	src := "services/test/src.txt"
	dst := "services/test/dst.txt"
	if err := os.WriteFile(filepath.Join(baseDir, src), []byte("moveme"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	body, _ := json.Marshal(MoveFileRequest{Destination: dst})
	resp := doAPICall(t, server, http.MethodPost, "/api/files/move?path="+src, body)
	if resp.Code != http.StatusNoContent {
		t.Fatalf("POST /api/files/move: expected 204, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	if _, err := os.Stat(filepath.Join(baseDir, dst)); err != nil {
		t.Errorf("dst not present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(baseDir, src)); !os.IsNotExist(err) {
		t.Errorf("src should be gone")
	}

	// 缺 destination → 400
	body, _ = json.Marshal(MoveFileRequest{})
	resp = doAPICall(t, server, http.MethodPost, "/api/files/move?path="+dst, body)
	if resp.Code != http.StatusBadRequest {
		t.Errorf("move no dest: expected 400, got %d", resp.Code)
	}

	// 缺 path → 400
	resp = doAPICall(t, server, http.MethodPost, "/api/files/move", body)
	if resp.Code != http.StatusBadRequest {
		t.Errorf("move no path: expected 400, got %d", resp.Code)
	}
}

// TestFileHistoryAndRollback 覆盖手动快照、普通保存不建历史和回滚。
func TestFileHistoryAndRollback(t *testing.T) {
	server, baseDir := newFileTestServer(t)
	rel := "services/test/hist.txt"
	full := filepath.Join(baseDir, rel)

	if err := os.WriteFile(full, []byte("seed"), 0644); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	resp := doAPICall(t, server, http.MethodPost, "/api/files/snapshot?path="+rel, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("snapshot v1: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	body, _ := json.Marshal(map[string]string{"content": "v2"})
	resp = doAPICall(t, server, http.MethodPut, "/api/files?path="+rel, body)
	if resp.Code != http.StatusOK {
		t.Fatalf("write: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	resp = doAPICall(t, server, http.MethodGet, "/api/files/history?path="+rel, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("history: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	var versions []FileVersion
	if err := json.Unmarshal(resp.Body.Bytes(), &versions); err != nil {
		t.Fatalf("unmarshal versions: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("ordinary save must not create history: got %d versions", len(versions))
	}

	rbBody, _ := json.Marshal(map[string]int{"version": 1})
	resp = doAPICall(t, server, http.MethodPost, "/api/files/rollback?path="+rel, rbBody)
	if resp.Code != http.StatusOK {
		t.Fatalf("rollback: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	got, _ := os.ReadFile(full)
	if string(got) != "seed" {
		t.Errorf("after rollback content = %q, want %q", string(got), "seed")
	}

	// 回滚不存在的版本 → 500
	rbBody2, _ := json.Marshal(map[string]int{"version": 999})
	resp = doAPICall(t, server, http.MethodPost, "/api/files/rollback?path="+rel, rbBody2)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("rollback missing version: expected 500, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// 缺 path → 400
	resp = doAPICall(t, server, http.MethodPost, "/api/files/rollback", rbBody)
	if resp.Code != http.StatusBadRequest {
		t.Errorf("rollback no path: expected 400, got %d", resp.Code)
	}
}

// TestValidateFile 覆盖 handleValidateFile：合法 YAML、非法 service.yaml、缺 path
func TestValidateFile(t *testing.T) {
	server, _ := newFileTestServer(t)

	// 合法 service.yaml
	valid := "name: s1\nversion: \"1.0\"\ncommand: [sleep, \"1\"]\n"
	vbody, _ := json.Marshal(ValidateFileRequest{Content: valid})
	resp := doAPICall(t, server, http.MethodPost, "/api/files/validate?path=services/s1/service.yaml", vbody)
	if resp.Code != http.StatusOK {
		t.Fatalf("validate valid: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
	var vr ValidateFileResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &vr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !vr.Valid {
		t.Errorf("expected valid=true, got errors: %+v", vr.Errors)
	}

	// 非法 service.yaml（未知字段应严格校验失败）
	invalid := "name: s1\nunknown_field: true\n"
	ibody, _ := json.Marshal(ValidateFileRequest{Content: invalid})
	resp = doAPICall(t, server, http.MethodPost, "/api/files/validate?path=services/s1/service.yaml", ibody)
	if resp.Code != http.StatusOK {
		t.Fatalf("validate invalid: expected 200, got %d", resp.Code)
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &vr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if vr.Valid {
		t.Errorf("expected valid=false for unknown field, got valid=true")
	}

	// 缺 path 不报错（path 仅用于选择校验类型），仍返回 valid
	resp = doAPICall(t, server, http.MethodPost, "/api/files/validate", vbody)
	if resp.Code != http.StatusOK {
		t.Errorf("validate no path: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}
}

// TestSnapshotFile 覆盖 handleSnapshotFile：成功快照、缺 path、provider 缺失
func TestSnapshotFile(t *testing.T) {
	server, baseDir := newFileTestServer(t)
	rel := "services/test/snap.txt"
	full := filepath.Join(baseDir, rel)
	if err := os.WriteFile(full, []byte("snapme"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	resp := doAPICall(t, server, http.MethodPost, "/api/files/snapshot?path="+rel, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("snapshot: expected 200, got %d (body: %s)", resp.Code, resp.Body.String())
	}

	// 缺 path → 400
	resp = doAPICall(t, server, http.MethodPost, "/api/files/snapshot", nil)
	if resp.Code != http.StatusBadRequest {
		t.Errorf("snapshot no path: expected 400, got %d", resp.Code)
	}

	// provider 缺失 → 500
	server.pathValidator = nil
	resp = doAPICall(t, server, http.MethodPost, "/api/files/snapshot?path="+rel, nil)
	if resp.Code != http.StatusInternalServerError {
		t.Errorf("snapshot nil provider: expected 500, got %d", resp.Code)
	}
}

// TestUploadFileHandler 覆盖 handleUploadFile：成功上传、缺 file 字段、非法文件名
func TestUploadFileHandler(t *testing.T) {
	server, baseDir := newFileTestServer(t)

	// 成功上传到可写白名单目录
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", "uploaded.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	part.Write([]byte("upload-content"))
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/files/upload?path=services%2Ftest", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	server.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: expected 201, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	got, err := os.ReadFile(filepath.Join(baseDir, "services", "test", "uploaded.txt"))
	if err != nil {
		t.Fatalf("read uploaded: %v", err)
	}
	if string(got) != "upload-content" {
		t.Errorf("uploaded content = %q, want %q", string(got), "upload-content")
	}

	// 缺 file 字段 → 400
	var buf2 bytes.Buffer
	w2 := multipart.NewWriter(&buf2)
	w2.Close()
	req2 := httptest.NewRequest(http.MethodPost, "/api/files/upload", &buf2)
	req2.Header.Set("Content-Type", w2.FormDataContentType())
	rec2 := httptest.NewRecorder()
	server.router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Errorf("upload missing file: expected 400, got %d (body: %s)", rec2.Code, rec2.Body.String())
	}

	// 非法文件名（".."）→ 400
	var buf3 bytes.Buffer
	w3 := multipart.NewWriter(&buf3)
	p3, _ := w3.CreateFormFile("file", "..")
	p3.Write([]byte("x"))
	w3.Close()
	req3 := httptest.NewRequest(http.MethodPost, "/api/files/upload", &buf3)
	req3.Header.Set("Content-Type", w3.FormDataContentType())
	rec3 := httptest.NewRecorder()
	server.router.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusBadRequest {
		t.Errorf("upload invalid filename: expected 400, got %d (body: %s)", rec3.Code, rec3.Body.String())
	}

	// provider 缺失 → 500（防御分支）
	server.fileProvider = nil
	var buf4 bytes.Buffer
	w4 := multipart.NewWriter(&buf4)
	p4, _ := w4.CreateFormFile("file", "x.txt")
	p4.Write([]byte("x"))
	w4.Close()
	req4 := httptest.NewRequest(http.MethodPost, "/api/files/upload?path=services/test", &buf4)
	req4.Header.Set("Content-Type", w4.FormDataContentType())
	rec4 := httptest.NewRecorder()
	server.router.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusInternalServerError {
		t.Errorf("upload nil provider: expected 500, got %d", rec4.Code)
	}
}
