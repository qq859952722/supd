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
	"strings"
	"testing"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/extension"
)

// 本文件覆盖 §2.12.5 扩展两阶段导入流程（handleImportExtension / handleImportExtensionConfirm）：
//   - 正常导入流程（预览 → 确认 → 验证目录创建）
//   - 路径穿越防护（归档含 ../ 恶意条目）
//   - zip slip 防护（归档含 ../../etc/passwd 条目）
//   - 扩展已存在的冲突提示（预览返回 exists_local + local_version）
//   - 预览/确认阶段的错误场景（table-driven）

// --- mock ---

// mockExtensionProvider 导入测试用的 ExtensionProvider mock
type mockExtensionProvider struct {
	exts map[string]*ExtensionInfo
}

func (m *mockExtensionProvider) ListExtensions() []ExtensionInfo {
	out := make([]ExtensionInfo, 0, len(m.exts))
	for _, e := range m.exts {
		out = append(out, *e)
	}
	return out
}

func (m *mockExtensionProvider) GetExtension(name string) (*ExtensionInfo, bool) {
	e, ok := m.exts[name]
	if !ok {
		return nil, false
	}
	return e, true
}

func (m *mockExtensionProvider) CreateExtension(meta *config.ExtensionMeta, service string) error {
	return nil
}

func (m *mockExtensionProvider) UpdateExtension(name string, meta *config.ExtensionMeta, service string) error {
	return nil
}

func (m *mockExtensionProvider) DeleteExtension(name string, service string) error {
	return nil
}

func (m *mockExtensionProvider) SaveExtensionEnv(name string, envData *config.EnvFile, service string) error {
	return nil
}

func (m *mockExtensionProvider) RunExtension(ctx context.Context, name, actionID, service string, dryRun bool) (*extension.RunResult, error) {
	return &extension.RunResult{}, nil
}

func (m *mockExtensionProvider) GetExtensionStatus(name, service string) (map[string]any, error) {
	return map[string]any{}, nil
}

// --- helpers ---

// validMetaYAML 合法的 meta.yaml 内容（通过 SetExtensionDefaults + ValidateExtension）
const validMetaYAML = `name: test-ext
version: "1.0.0"
entry: run.sh
runtime: shell
timeout_seconds: 60
concurrency: replace
`

// buildTarGz 构建 tar.gz 归档，files 为 文件名→内容 映射
func buildTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for name, content := range files {
		hdr := &tar.Header{Name: name, Mode: 0644, Size: int64(len(content))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write tar header %q: %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("write tar body %q: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	return buf.Bytes()
}

// newMultipartRequest 构建 multipart 上传请求；archive 为 nil 时不添加 file 字段
func newMultipartRequest(t *testing.T, url string, archive []byte, fields map[string]string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatalf("write field %q: %v", k, err)
		}
	}
	if archive != nil {
		part, err := mw.CreateFormFile("file", "archive.tar.gz")
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := part.Write(archive); err != nil {
			t.Fatalf("write archive: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, url, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

// createImportTestServer 创建带 mock extProvider 和临时 baseDir 的测试服务器
func createImportTestServer(t *testing.T) (*Server, *mockExtensionProvider, string) {
	t.Helper()
	baseDir := t.TempDir()
	s := NewServer(nil)
	prov := &mockExtensionProvider{exts: make(map[string]*ExtensionInfo)}
	s.extProvider = prov
	s.pathValidator = NewPathValidator(baseDir)
	return s, prov, baseDir
}

// --- 正常导入流程 ---

// TestImportExtension_FullFlow 正常两阶段导入：预览 → 确认 → 验证扩展目录已创建
func TestImportExtension_FullFlow(t *testing.T) {
	s, _, baseDir := createImportTestServer(t)
	archive := buildTarGz(t, map[string]string{
		"meta.yaml": validMetaYAML,
		"run.sh":    "#!/bin/sh\necho hello\n",
	})

	// 阶段1：预览
	previewReq := newMultipartRequest(t, "/api/extensions/import", archive, nil)
	pw := httptest.NewRecorder()
	s.router.ServeHTTP(pw, previewReq)
	if pw.Code != http.StatusOK {
		t.Fatalf("preview: expected 200, got %d (body=%s)", pw.Code, pw.Body.String())
	}
	var preview ExtensionImportPreviewResponse
	if err := json.NewDecoder(pw.Body).Decode(&preview); err != nil {
		t.Fatalf("decode preview response: %v", err)
	}
	if preview.Name != "test-ext" {
		t.Errorf("preview name = %q, want test-ext", preview.Name)
	}
	if preview.ArchiveVer != "1.0.0" {
		t.Errorf("preview archive_version = %q, want 1.0.0", preview.ArchiveVer)
	}
	if preview.ExistsLocal {
		t.Errorf("preview exists_local = true, want false (extension should not exist yet)")
	}

	// 阶段2：确认导入
	confirmReq := newMultipartRequest(t, "/api/extensions/import/confirm", archive, map[string]string{"name": "test-ext"})
	cw := httptest.NewRecorder()
	s.router.ServeHTTP(cw, confirmReq)
	if cw.Code != http.StatusCreated {
		t.Fatalf("confirm: expected 201, got %d (body=%s)", cw.Code, cw.Body.String())
	}

	// 阶段3：验证扩展目录与文件已创建
	extDir := filepath.Join(baseDir, "extensions", "test-ext")
	metaPath := filepath.Join(extDir, "meta.yaml")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read imported meta.yaml: %v", err)
	}
	if !strings.Contains(string(data), "name: test-ext") {
		t.Errorf("imported meta.yaml content mismatch: %s", data)
	}
	if _, err := os.Stat(filepath.Join(extDir, "run.sh")); err != nil {
		t.Errorf("imported run.sh not found: %v", err)
	}
}

// --- 扩展已存在 ---

// TestImportExtensionPreview_ExistingExtension 扩展已存在时预览返回冲突提示
// §2.12.5: 预览阶段应返回 exists_local=true 及本地版本号
func TestImportExtensionPreview_ExistingExtension(t *testing.T) {
	s, prov, _ := createImportTestServer(t)
	// 预置已存在的本地扩展（版本 0.9.0）
	prov.exts["test-ext"] = &ExtensionInfo{Name: "test-ext", Version: "0.9.0"}

	archive := buildTarGz(t, map[string]string{"meta.yaml": validMetaYAML})
	req := newMultipartRequest(t, "/api/extensions/import", archive, nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	var resp ExtensionImportPreviewResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.ExistsLocal {
		t.Error("expected exists_local=true, got false")
	}
	if resp.LocalVer != "0.9.0" {
		t.Errorf("local_version = %q, want 0.9.0", resp.LocalVer)
	}
	if resp.ArchiveVer != "1.0.0" {
		t.Errorf("archive_version = %q, want 1.0.0", resp.ArchiveVer)
	}
}

// --- 路径穿越防护 ---

// TestImportExtensionConfirm_PathTraversalArchive 路径穿越防护：归档含 ../ 恶意条目被拒绝
// H-03-002: zip slip / 路径穿越返回 403 而非 500
func TestImportExtensionConfirm_PathTraversalArchive(t *testing.T) {
	s, _, baseDir := createImportTestServer(t)
	// 构造含 ../malicious.txt 条目的归档（单级路径穿越）
	archive := buildTarGz(t, map[string]string{"../malicious.txt": "malicious content"})
	req := newMultipartRequest(t, "/api/extensions/import/confirm", archive, map[string]string{"name": "test-ext"})
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for path traversal archive, got %d (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "path traversal") {
		t.Errorf("expected 'path traversal' in error body, got: %s", w.Body.String())
	}
	// 验证目标目录已被回滚清理（UnpackDir 失败后 handler 执行 RemoveAll 回滚）
	targetDir := filepath.Join(baseDir, "extensions", "test-ext")
	if _, err := os.Stat(targetDir); err == nil {
		t.Error("target directory should have been removed after rollback")
	}
}

// --- zip slip 防护 ---

// TestImportExtensionConfirm_ZipSlip zip slip 防护：归档含 ../../etc/passwd 条目被拒绝
// 经典 zip slip 攻击：多级 ../ 试图逃逸到目标目录外
func TestImportExtensionConfirm_ZipSlip(t *testing.T) {
	s, _, baseDir := createImportTestServer(t)
	archive := buildTarGz(t, map[string]string{"../../etc/passwd": "root:x:0:0:root:/root:/bin/sh\n"})
	req := newMultipartRequest(t, "/api/extensions/import/confirm", archive, map[string]string{"name": "test-ext"})
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for zip slip archive, got %d (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "path traversal") {
		t.Errorf("expected 'path traversal' in error body, got: %s", w.Body.String())
	}
	// 验证目标目录已被回滚清理
	targetDir := filepath.Join(baseDir, "extensions", "test-ext")
	if _, err := os.Stat(targetDir); err == nil {
		t.Error("target directory should have been removed after rollback")
	}
}

// --- 预览阶段错误场景（table-driven） ---

// TestImportExtensionPreview_ErrorCases 预览阶段错误场景
func TestImportExtensionPreview_ErrorCases(t *testing.T) {
	tests := []struct {
		name       string
		buildReq   func(t *testing.T) *http.Request
		wantStatus int
	}{
		{
			name: "missing_file_field",
			buildReq: func(t *testing.T) *http.Request {
				return newMultipartRequest(t, "/api/extensions/import", nil, nil)
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "invalid_archive_not_targz",
			buildReq: func(t *testing.T) *http.Request {
				return newMultipartRequest(t, "/api/extensions/import", []byte("not a tar.gz file"), nil)
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "no_meta_yaml_in_archive",
			buildReq: func(t *testing.T) *http.Request {
				return newMultipartRequest(t, "/api/extensions/import",
					buildTarGz(t, map[string]string{"run.sh": "echo hi"}), nil)
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "invalid_meta_yaml_missing_entry",
			buildReq: func(t *testing.T) *http.Request {
				// meta.yaml 缺少必填字段 entry，ValidateExtension 应失败
				return newMultipartRequest(t, "/api/extensions/import",
					buildTarGz(t, map[string]string{"meta.yaml": "name: bad-ext\nversion: \"1.0.0\"\n"}), nil)
			},
			wantStatus: http.StatusUnprocessableEntity, // ErrServiceConfigInvalid → 422
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _, _ := createImportTestServer(t)
			req := tt.buildReq(t)
			w := httptest.NewRecorder()
			s.router.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("expected %d, got %d (body=%s)", tt.wantStatus, w.Code, w.Body.String())
			}
		})
	}
}

// --- 确认阶段错误场景（table-driven） ---

// TestImportExtensionConfirm_ErrorCases 确认阶段错误场景
func TestImportExtensionConfirm_ErrorCases(t *testing.T) {
	validArchive := buildTarGz(t, map[string]string{"meta.yaml": validMetaYAML})
	tests := []struct {
		name       string
		buildReq   func(t *testing.T) *http.Request
		wantStatus int
	}{
		{
			name: "missing_name_field",
			buildReq: func(t *testing.T) *http.Request {
				return newMultipartRequest(t, "/api/extensions/import/confirm", validArchive, nil)
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "missing_file_field",
			buildReq: func(t *testing.T) *http.Request {
				return newMultipartRequest(t, "/api/extensions/import/confirm", nil, map[string]string{"name": "test-ext"})
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "invalid_name_uppercase",
			buildReq: func(t *testing.T) *http.Request {
				// 不匹配 ^[a-z][a-z0-9-]*$
				return newMultipartRequest(t, "/api/extensions/import/confirm", validArchive, map[string]string{"name": "TestExt"})
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "path_traversal_in_name",
			buildReq: func(t *testing.T) *http.Request {
				// name 含 ../，SanitizeFilename 清理后 != 原值 → 拒绝
				return newMultipartRequest(t, "/api/extensions/import/confirm", validArchive, map[string]string{"name": "../etc"})
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "name_with_path_separator",
			buildReq: func(t *testing.T) *http.Request {
				// name 含路径分隔符，SanitizeFilename 清理后 != 原值 → 拒绝
				return newMultipartRequest(t, "/api/extensions/import/confirm", validArchive, map[string]string{"name": "a/b"})
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _, _ := createImportTestServer(t)
			req := tt.buildReq(t)
			w := httptest.NewRecorder()
			s.router.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("expected %d, got %d (body=%s)", tt.wantStatus, w.Code, w.Body.String())
			}
		})
	}
}
