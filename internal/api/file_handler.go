package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/supdorg/supd/internal/errors"
)

// MaxReadFileSize GET /api/files 读取文件大小上限（10MB）
// D-04-01: 防止读取大文件导致 OOM（os.ReadFile 全量读入内存）
const MaxReadFileSize = 10 * 1024 * 1024 // 10MB

// REQ-P-001, REQ-E-002: 文件操作端点
// 所有路径必须通过PathValidator校验
// 写操作需ValidateWritePath
// YAML校验返回行列位置信息
// REQ-2.3.1: 文件历史版本50个（数值锁定）

// FileTreeResponse 文件树响应
type FileTreeResponse struct {
	Path     string        `json:"path"`
	Children []FileTreeNode `json:"children"`
}

// FileContentResponse 文件内容响应
type FileContentResponse struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// FileHistoryResponse 文件历史响应
type FileHistoryResponse struct {
	Path     string         `json:"path"`
	Versions []FileVersion  `json:"versions"`
}

// ValidateFileRequest 文件校验请求
type ValidateFileRequest struct {
	Content string `json:"content"`
}

// ValidateFileResponse 文件校验响应
type ValidateFileResponse struct {
	Valid   bool              `json:"valid"`
	Errors  []ValidationError `json:"errors,omitempty"`
}

// MoveFileRequest 移动文件请求
type MoveFileRequest struct {
	Destination string `json:"destination"`
}

// handleFileTree GET /api/files/tree
// D-04-001 修复：非空 path 参数需通过 PathValidator 校验，提供纵深防御
func (s *Server) handleFileTree(w http.ResponseWriter, r *http.Request) {
	if s.fileProvider == nil || s.pathValidator == nil {
		respondError(w, errors.ErrInternal, "file provider or path validator not configured")
		return
	}

	path := r.URL.Query().Get("path")
	// D-04-001 修复：非空 path 需校验白名单（空 path 表示列出根目录，FileTree 内部基于 baseDir 安全）
	if path != "" {
		if _, err := s.pathValidator.Validate(path); err != nil {
			respondError(w, errors.ErrFileAccessDenied, err.Error())
			return
		}
	}

	nodes, err := s.fileProvider.FileTree(path)
	if err != nil {
		respondError(w, errors.ErrInternal, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, nodes)
}

// handleReadFile GET /api/files
func (s *Server) handleReadFile(w http.ResponseWriter, r *http.Request) {
	if s.fileProvider == nil || s.pathValidator == nil {
		respondError(w, errors.ErrInternal, "file provider or path validator not configured")
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		respondError(w, errors.ErrInvalidRequest, "path parameter required")
		return
	}

	// REQ-P-001: 所有路径必须通过PathValidator校验
	validatedPath, err := s.pathValidator.Validate(path)
	if err != nil {
		respondError(w, errors.ErrFileAccessDenied, err.Error())
		return
	}

	// D-04-01: 读取前检查文件大小，防止 os.ReadFile 全量读入大文件导致 OOM
	info, err := os.Stat(validatedPath)
	if err != nil {
		respondError(w, errors.ErrFileNotFound, err.Error())
		return
	}
	if info.Size() > MaxReadFileSize {
		respondError(w, errors.ErrFileTooLarge, fmt.Sprintf("file size %d bytes exceeds read limit %d bytes", info.Size(), MaxReadFileSize))
		return
	}

	content, err := s.fileProvider.ReadFile(validatedPath)
	if err != nil {
		respondError(w, errors.ErrFileNotFound, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, FileContentResponse{
		Path:    validatedPath,
		Content: string(content),
	})
}

// handleWriteFile PUT /api/files
// REQ-P-001: 写操作需ValidateWritePath
// REQ-2.12.6: 上传大小限制100MB
func (s *Server) handleWriteFile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadSize) // O-05-002: 使用常量

	if s.fileProvider == nil || s.pathValidator == nil {
		respondError(w, errors.ErrInternal, "file provider or path validator not configured")
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		respondError(w, errors.ErrInvalidRequest, "path parameter required")
		return
	}

	// REQ-P-001: 写操作需要ValidateWritePath
	validatedPath, err := s.pathValidator.ValidateWritePath(path)
	if err != nil {
		respondError(w, errors.ErrFileAccessDenied, err.Error())
		return
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, errors.ErrInvalidRequest, "invalid request body")
		return
	}

	if err := s.fileProvider.WriteFile(validatedPath, []byte(req.Content)); err != nil {
		respondError(w, errors.ErrFilePermission, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleCreateFile POST /api/files
// REQ-2.12.6: 上传大小限制100MB
// 支持 is_dir=true 创建目录
func (s *Server) handleCreateFile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadSize) // O-05-002: 使用常量

	if s.fileProvider == nil || s.pathValidator == nil {
		respondError(w, errors.ErrInternal, "file provider or path validator not configured")
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		respondError(w, errors.ErrInvalidRequest, "path parameter required")
		return
	}

	// 写操作需要ValidateWritePath
	validatedPath, err := s.pathValidator.ValidateWritePath(path)
	if err != nil {
		respondError(w, errors.ErrFileAccessDenied, err.Error())
		return
	}

	var req struct {
		Content string `json:"content"`
		IsDir   bool   `json:"is_dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, errors.ErrInvalidRequest, "invalid request body")
		return
	}

	if req.IsDir {
		if err := s.fileProvider.CreateDir(validatedPath); err != nil {
			respondError(w, errors.ErrFilePermission, err.Error())
			return
		}
	} else {
		if err := s.fileProvider.CreateFile(validatedPath, []byte(req.Content)); err != nil {
			respondError(w, errors.ErrFilePermission, err.Error())
			return
		}
	}

	respondJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

// handleDeleteFile DELETE /api/files
func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	if s.fileProvider == nil || s.pathValidator == nil {
		respondError(w, errors.ErrInternal, "file provider or path validator not configured")
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		respondError(w, errors.ErrInvalidRequest, "path parameter required")
		return
	}

	validatedPath, err := s.pathValidator.ValidateWritePath(path)
	if err != nil {
		respondError(w, errors.ErrFileAccessDenied, err.Error())
		return
	}

	if err := s.fileProvider.DeleteFile(validatedPath); err != nil {
		respondError(w, errors.ErrFileNotFound, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleMoveFile POST /api/files/move
func (s *Server) handleMoveFile(w http.ResponseWriter, r *http.Request) {
	if s.fileProvider == nil || s.pathValidator == nil {
		respondError(w, errors.ErrInternal, "file provider or path validator not configured")
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		respondError(w, errors.ErrInvalidRequest, "path parameter required")
		return
	}

	oldPath, err := s.pathValidator.ValidateWritePath(path)
	if err != nil {
		respondError(w, errors.ErrFileAccessDenied, err.Error())
		return
	}

	var req MoveFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, errors.ErrInvalidRequest, "invalid request body")
		return
	}
	if req.Destination == "" {
		respondError(w, errors.ErrInvalidRequest, "destination required")
		return
	}

	newPath, err := s.pathValidator.ValidateWritePath(req.Destination)
	if err != nil {
		respondError(w, errors.ErrFileAccessDenied, err.Error())
		return
	}

	if err := s.fileProvider.MoveFile(oldPath, newPath); err != nil {
		respondError(w, errors.ErrInternal, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleFileHistory GET /api/files/history
func (s *Server) handleFileHistory(w http.ResponseWriter, r *http.Request) {
	if s.fileProvider == nil || s.pathValidator == nil {
		respondError(w, errors.ErrInternal, "file provider or path validator not configured")
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		respondError(w, errors.ErrInvalidRequest, "path parameter required")
		return
	}

	validatedPath, err := s.pathValidator.Validate(path)
	if err != nil {
		respondError(w, errors.ErrFileAccessDenied, err.Error())
		return
	}

	versions, err := s.fileProvider.FileHistory(validatedPath)
	if err != nil {
		respondError(w, errors.ErrFileNotFound, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, versions)
}

// handleRollbackFile POST /api/files/rollback
// REQ-2.3.1: 文件历史版本50个（数值锁定）
func (s *Server) handleRollbackFile(w http.ResponseWriter, r *http.Request) {
	if s.fileProvider == nil || s.pathValidator == nil {
		respondError(w, errors.ErrInternal, "file provider or path validator not configured")
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		respondError(w, errors.ErrInvalidRequest, "path parameter required")
		return
	}

	validatedPath, err := s.pathValidator.ValidateWritePath(path)
	if err != nil {
		respondError(w, errors.ErrFileAccessDenied, err.Error())
		return
	}

	var req struct {
		Version int `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, errors.ErrInvalidRequest, "invalid request body")
		return
	}

	if err := s.fileProvider.RollbackFile(validatedPath, req.Version); err != nil {
		respondError(w, errors.ErrInternal, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleValidateFile POST /api/files/validate
// REQ-P-001, REQ-E-002: YAML校验返回行列位置信息
// D-04-002 修复：限制请求体大小防止 DoS（content 字段可为任意大小）
func (s *Server) handleValidateFile(w http.ResponseWriter, r *http.Request) {
	if s.fileProvider == nil || s.pathValidator == nil {
		respondError(w, errors.ErrInternal, "file provider or path validator not configured")
		return
	}

	// D-04-002 修复：限制请求体大小为 MaxUploadSize（100MB），防止超大 content 导致 OOM
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadSize)

	path := r.URL.Query().Get("path")

	// D-04-003 修复：非空 path 需通过 PathValidator 校验，防止路径穿越
	// （path 仅用于判断文件扩展名以选择校验方式，但仍需 baseDir 边界检查）
	if path != "" {
		if _, err := s.pathValidator.Validate(path); err != nil {
			respondError(w, errors.ErrFileAccessDenied, fmt.Sprintf("invalid path: %v", err))
			return
		}
	}

	var req ValidateFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, errors.ErrInvalidRequest, "invalid request body")
		return
	}

	validationErrors, err := s.fileProvider.ValidateFile(path, []byte(req.Content))
	if err != nil {
		// 内部错误（如路径校验失败）
		respondError(w, errors.ErrInternal, fmt.Sprintf("validate failed: %v", err))
		return
	}
	// BUG-01 修复：ValidateFile 返回 (errs, nil) 时，应检查 errs 长度判断是否有校验错误。
	// 原代码检查 err（永远为 nil），导致所有 yaml 都返回 valid:true。
	if len(validationErrors) > 0 {
		respondJSON(w, http.StatusOK, ValidateFileResponse{
			Valid:  false,
			Errors: validationErrors,
		})
		return
	}

	respondJSON(w, http.StatusOK, ValidateFileResponse{
		Valid:  true,
	})
}

// handleSnapshotFile POST /api/files/snapshot
func (s *Server) handleSnapshotFile(w http.ResponseWriter, r *http.Request) {
	if s.fileProvider == nil || s.pathValidator == nil {
		respondError(w, errors.ErrInternal, "file provider or path validator not configured")
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		respondError(w, errors.ErrInvalidRequest, "path parameter required")
		return
	}

	validatedPath, err := s.pathValidator.Validate(path)
	if err != nil {
		respondError(w, errors.ErrFileAccessDenied, err.Error())
		return
	}

	if err := s.fileProvider.SnapshotFile(validatedPath); err != nil {
		respondError(w, errors.ErrInternal, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleUploadFile POST /api/files/upload
// 通用文件上传：multipart/form-data，字段名 file
// 查询参数 path 指定目标目录（baseDir 下的相对路径或绝对路径）
// REQ-2.12.6: 上传大小限制100MB
func (s *Server) handleUploadFile(w http.ResponseWriter, r *http.Request) {
	if s.fileProvider == nil || s.pathValidator == nil {
		respondError(w, errors.ErrInternal, "file provider or path validator not configured")
		return
	}

	// 限制上传大小
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadSize)

	// 目标目录
	targetDir := r.URL.Query().Get("path")
	// 空字符串表示根目录
	var validatedDir string
	if targetDir != "" {
		validatedDirTemp, err := s.pathValidator.ValidateWritePath(targetDir)
		if err != nil {
			respondError(w, errors.ErrFileAccessDenied, err.Error())
			return
		}
		validatedDir = validatedDirTemp
	} else {
		// 根目录使用 baseDir 本身
		validatedDir = s.pathValidator.baseDir
	}

	// 解析 multipart
	// MaxBytesReader 限制了整体请求体大小，32MB 内存阈值即可
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		respondError(w, errors.ErrFileTooLarge, "upload too large or invalid: "+err.Error())
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		respondError(w, errors.ErrInvalidRequest, "missing 'file' field in multipart form")
		return
	}
	defer file.Close()

	// 校验文件名：仅允许简单文件名（不含路径分隔符、不含 ..）
	safeName := SanitizeFilename(header.Filename)
	if safeName == "" || safeName == "." || safeName == ".." || strings.ContainsAny(safeName, `/\`) {
		respondError(w, errors.ErrInvalidRequest, "invalid filename")
		return
	}

	// 组合最终路径并再次校验（防止 targetDir + filename 逃逸）
	destPath := filepath.Clean(filepath.Join(validatedDir, safeName))
	if !strings.HasPrefix(destPath, s.pathValidator.baseDir) {
		respondError(w, errors.ErrFileAccessDenied, "resolved path is outside base directory")
		return
	}
	// extraAllowed 路径下的上传也要拒绝（只读区域）
	if s.pathValidator.IsReadOnly(destPath) {
		respondError(w, errors.ErrFileAccessDenied, "target directory is read-only")
		return
	}

	// R-002 修复：流式写入磁盘，避免 io.ReadAll 全量读入内存导致多并发上传 OOM
	// MaxBytesReader 已在请求层限制 100MB，此处再以硬上限兜底
	tmpPath := destPath + ".supd-tmp-" + randomSuffix()
	tmpFile, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		// 父目录可能不存在，先 CreateFile 的等价路径：通过 fileProvider 创建父目录
		if mkdirErr := os.MkdirAll(filepath.Dir(destPath), 0755); mkdirErr != nil {
			respondError(w, errors.ErrInternal, "failed to prepare upload directory: "+mkdirErr.Error())
			return
		}
		tmpFile, err = os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			respondError(w, errors.ErrInternal, "failed to create temp file: "+err.Error())
			return
		}
	}

	// 流式拷贝，边写边检查总大小（MaxBytesReader 已限制请求体，此处再以 MaxUploadSize 兜底）
	written, copyErr := io.Copy(tmpFile, file)
	// 显式 Close 后再判断错误（写入路径 Close 错误可能意味着数据未刷盘）
	closeErr := tmpFile.Close()
	if closeErr != nil {
		os.Remove(tmpPath)
		respondError(w, errors.ErrInternal, "failed to flush uploaded file: "+closeErr.Error())
		return
	}
	if copyErr != nil {
		os.Remove(tmpPath)
		respondError(w, errors.ErrInternal, "failed to write uploaded file: "+copyErr.Error())
		return
	}
	if written > int64(MaxUploadSize) {
		os.Remove(tmpPath)
		respondError(w, errors.ErrFileTooLarge, fmt.Sprintf("uploaded file size %d exceeds limit %d", written, MaxUploadSize))
		return
	}

	// 原子重命名到目标路径
	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		respondError(w, errors.ErrFilePermission, "failed to finalize upload: "+err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, map[string]string{
		"status": "ok",
		"path":   destPath,
	})
}

// randomSuffix 返回 8 字节十六进制随机串，用于临时文件名后缀，避免并发上传冲突
func randomSuffix() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// 极端情况下回退到时间戳
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
