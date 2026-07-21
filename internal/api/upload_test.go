package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/supdorg/supd/internal/errors"
)

// createUploadRequest 创建上传测试请求，同时返回 ResponseRecorder 用于 ValidateUpload
func createUploadRequest(filename string, content []byte) (*http.Request, http.ResponseWriter) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		panic(err)
	}
	part.Write(content)
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/files/upload", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	// H-02-002/D-06-001 修复：ValidateUpload 需要一个真实 ResponseWriter 以便 MaxBytesReader 超限时设置 413
	rec := httptest.NewRecorder()
	return req, rec
}

// TestUploadValidateNormal 正常上传
func TestUploadValidateNormal(t *testing.T) {
	content := []byte("hello world")
	req, w := createUploadRequest("test.txt", content)

	result, err := ValidateUpload(w, req, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer result.File.Close()

	if result.Filename != "test.txt" {
		t.Errorf("expected filename test.txt, got %s", result.Filename)
	}
	if result.Size != int64(len(content)) {
		t.Errorf("expected size %d, got %d", len(content), result.Size)
	}
}

// TestUploadValidateTooLarge 文件过大
// REQ-C-009: 上传大小限制100MB
func TestUploadValidateTooLarge(t *testing.T) {
	content := make([]byte, MaxUploadSize+1)
	req, w := createUploadRequest("big.txt", content)

	_, err := ValidateUpload(w, req, 100)
	if err == nil {
		t.Fatal("expected error for oversized upload")
	}
	if serr, ok := err.(*errors.ServiceError); !ok || serr.Code != errors.ErrFileTooLarge {
		t.Errorf("expected ErrFileTooLarge, got %v", err)
	}
}

// TestUploadValidateSmallLimit 小限制
func TestUploadValidateSmallLimit(t *testing.T) {
	content := []byte("hello world, this is more than 5 bytes")
	req, w := createUploadRequest("test.txt", content)

	// 5 bytes限制
	_, err := ValidateUpload(w, req, 0) // 0 MB = 0 bytes limit → should fail
	if err == nil {
		t.Fatal("expected error for upload exceeding 0 MB limit")
	}
}

// TestUploadValidatePathTraversal 文件名路径穿越
// REQ-E-006: 上传文件名安全
func TestUploadValidatePathTraversal(t *testing.T) {
	content := []byte("malicious")
	req, w := createUploadRequest("../../../etc/passwd", content)

	result, err := ValidateUpload(w, req, 100)
	if err != nil {
		// SanitizeFilename会清理路径穿越，但文件名可能变为空或"passwd"
		// 如果结果合法（文件名被清理为passwd），那也OK
		t.Logf("got expected error: %v", err)
		return
	}
	defer result.File.Close()
	// 文件名应该被清理，不包含路径组件
	if strings.Contains(result.Filename, "/") || strings.Contains(result.Filename, "\\") {
		t.Errorf("filename should not contain path separators: %s", result.Filename)
	}
	if strings.Contains(result.Filename, "..") {
		t.Errorf("filename should not contain ..: %s", result.Filename)
	}
}

// TestUploadValidateEmptyFilename 空文件名
func TestUploadValidateEmptyFilename(t *testing.T) {
	content := []byte("data")
	req, w := createUploadRequest("..", content)

	_, err := ValidateUpload(w, req, 100)
	if err == nil {
		t.Fatal("expected error for invalid filename after sanitization")
	}
}

// TestValidateUploadedFilename 校验目标文件名
// REQ-E-006: 上传存储路径校验
func TestValidateUploadedFilename(t *testing.T) {
	tests := []struct {
		name     string
		baseDir  string
		filename string
		wantErr  bool
	}{
		{"normal", "/etc/supd/assets", "logo.png", false},
		// traversal 路径被 SanitizeFilename 清理为 "passwd"，在 baseDir 下是安全的
		{"traversal sanitized", "/etc/supd/assets", "../../../etc/passwd", false},
		{"empty name", "/etc/supd/assets", "..", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidateUploadedFilename(tt.baseDir, tt.filename)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateUploadedFilename() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestMaxUploadSizeConstant 上传大小常量验证
// REQ-C-009: 数值锁定100MB
func TestMaxUploadSizeConstant(t *testing.T) {
	expected := int64(100 * 1024 * 1024)
	if MaxUploadSize != expected {
		t.Errorf("MaxUploadSize = %d, want %d", MaxUploadSize, expected)
	}
}

// TestUploadValidate_MaxSizeBoundary 锚定 100MB 上传上限的双边界行为
// L-02-002: ValidateUpload 的最终大小检查使用 size > MaxUploadSize（严格大于），
// 因此恰好 MaxUploadSize 字节应通过、MaxUploadSize+1 字节应拒绝。
// 注：maxUploadMB 传 110 以避免 MaxBytesReader 因 multipart 开销（headers/boundary）
// 在 body 层面拒绝恰好 100MB 的文件；这样请求能到达第98行的 size > MaxUploadSize 检查。
func TestUploadValidate_MaxSizeBoundary(t *testing.T) {
	tests := []struct {
		name      string
		size      int64
		wantError bool
	}{
		{"one_byte_below_max_allowed", MaxUploadSize - 1, false},
		{"exactly_max_allowed", MaxUploadSize, false},
		{"one_byte_over_max_rejected", MaxUploadSize + 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := make([]byte, tt.size)
			req, w := createUploadRequest("boundary.bin", content)
			result, err := ValidateUpload(w, req, 110)
			if tt.wantError && err == nil {
				t.Errorf("size=%d: expected error, got nil", tt.size)
				return
			}
			if !tt.wantError && err != nil {
				t.Errorf("size=%d: expected no error, got %v", tt.size, err)
				return
			}
			if !tt.wantError && result != nil {
				if result.Size != tt.size {
					t.Errorf("size=%d: result.Size = %d, want %d", tt.size, result.Size, tt.size)
				}
				result.File.Close()
			}
			if tt.wantError && err != nil {
				if serr, ok := err.(*errors.ServiceError); !ok || serr.Code != errors.ErrFileTooLarge {
					t.Errorf("size=%d: expected ErrFileTooLarge, got %v", tt.size, err)
				}
			}
		})
	}
}
