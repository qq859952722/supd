package api

import (
	stderrors "errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"log/slog"
	"strings"

	"github.com/supdorg/supd/internal/errors"
)

// MaxUploadSize 上传大小限制 100MB（数值锁定）
// REQ-C-009: 上传大小限制100MB
const MaxUploadSize = 100 * 1024 * 1024 // 100MB

// UploadResult 上传校验结果
type UploadResult struct {
	File     multipart.File
	Filename string
	Size     int64
	Header   *multipart.FileHeader
}

// ValidateUpload 校验上传文件
// REQ-E-006: 上传文件安全（大小限制+类型检查+路径校验）
// REQ-C-009: 数值锁定100MB
// H-02-002/D-06-001 修复：第一个参数必须传 ResponseWriter，
// 以便超限时 MaxBytesReader 能正确触发 413 响应并关闭连接
func ValidateUpload(w http.ResponseWriter, r *http.Request, maxUploadMB int) (*UploadResult, error) {
	// 1. 限制请求体大小（在ParseMultipartForm之前）
	maxBytes := int64(maxUploadMB) * 1024 * 1024
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	// 2. 解析 multipart form
	if err := r.ParseMultipartForm(int64(maxBytes)); err != nil {
		var maxErr *http.MaxBytesError
		if stderrors.As(err, &maxErr) {
			return nil, errors.NewServiceError(errors.ErrFileTooLarge,
				fmt.Sprintf("upload exceeds size limit of %d MB", maxUploadMB))
		}
		return nil, errors.NewServiceError(errors.ErrInvalidRequest,
			fmt.Sprintf("parse upload form: %v", err))
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		if stderrors.Is(err, http.ErrMissingFile) {
			return nil, errors.NewServiceError(errors.ErrInvalidRequest, "upload requires a 'file' field")
		}
		return nil, errors.NewServiceError(errors.ErrInvalidRequest,
			fmt.Sprintf("read upload file: %v", err))
	}

	// 3. 清理文件名
	filename := SanitizeFilename(header.Filename)
	if filename == "" || filename == "." {
		// I-03-002 修复：Close 错误记录日志，便于诊断资源清理失败
		if err := file.Close(); err != nil {
			slog.Warn("close upload file failed", "filename", header.Filename, "error", err)
		}
		return nil, errors.NewServiceError(errors.ErrInvalidRequest,
			"invalid filename after sanitization")
	}

	// 4. 检查文件名不含 ..
	if strings.Contains(filename, "..") {
		// I-03-002 修复：Close 错误记录日志，便于诊断资源清理失败
		if err := file.Close(); err != nil {
			slog.Warn("close upload file failed", "filename", header.Filename, "error", err)
		}
		return nil, errors.NewServiceError(errors.ErrFileAccessDenied,
			fmt.Sprintf("filename contains '..': %s", header.Filename))
	}

	// 5. 获取实际文件大小
	// H-03-001 修复：使用 io.LimitReader 限制读取量，避免 io.ReadAll 无上限读取
	// MaxBytesReader 已在请求层限制总大小，此处再以 maxBytes+1 兜底并检测超限
	size := header.Size
	if size <= 0 {
		// 如果 Content-Length 为0，尝试读取确定大小
		lr := io.LimitReader(file, maxBytes+1)
		buf, readErr := io.ReadAll(lr)
		if readErr != nil && !stderrors.Is(readErr, io.EOF) {
			// I-03-002 修复：Close 错误记录日志，便于诊断资源清理失败
			if err := file.Close(); err != nil {
				slog.Warn("close upload file failed", "filename", header.Filename, "error", err)
			}
			return nil, errors.NewServiceError(errors.ErrInternal,
				fmt.Sprintf("read file content: %v", readErr))
		}
		size = int64(len(buf))
		// 用一个包装器让调用者可以重新读取
		file = &rewindableReader{data: buf}
		header.Size = size
	} else {
		// 包装为可重读的 reader
		lr := io.LimitReader(file, maxBytes+1)
		data, readErr := io.ReadAll(lr)
		if readErr != nil && !stderrors.Is(readErr, io.EOF) {
			// I-03-002 修复：Close 错误记录日志，便于诊断资源清理失败
			if err := file.Close(); err != nil {
				slog.Warn("close upload file failed", "filename", header.Filename, "error", err)
			}
			return nil, errors.NewServiceError(errors.ErrInternal,
				fmt.Sprintf("read file content: %v", readErr))
		}
		file = &rewindableReader{data: data}
		header.Size = int64(len(data))
		size = int64(len(data))
	}

	// 6. 再次校验大小
	if size > int64(MaxUploadSize) {
		// I-03-002 修复：Close 错误记录日志，便于诊断资源清理失败
		if err := file.Close(); err != nil {
			slog.Warn("close upload file failed", "filename", header.Filename, "error", err)
		}
		return nil, errors.NewServiceError(errors.ErrFileTooLarge,
			fmt.Sprintf("file size %d bytes exceeds limit %d bytes", size, MaxUploadSize))
	}

	return &UploadResult{
		File:     file,
		Filename: filename,
		Size:     size,
		Header:   header,
	}, nil
}

// rewindableReader 可重复读取的字节流，实现 multipart.File 接口
type rewindableReader struct {
	data   []byte
	offset int
}

func (r *rewindableReader) Read(p []byte) (int, error) {
	if r.offset >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.offset:])
	r.offset += n
	return n, nil
}

func (r *rewindableReader) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(r.data)) {
		return 0, io.EOF
	}
	n := copy(p, r.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (r *rewindableReader) Seek(offset int64, whence int) (int64, error) {
	var newOffset int64
	switch whence {
	case io.SeekStart:
		newOffset = offset
	case io.SeekCurrent:
		newOffset = int64(r.offset) + offset
	case io.SeekEnd:
		newOffset = int64(len(r.data)) + offset
	default:
		return 0, fmt.Errorf("invalid whence: %d", whence)
	}
	if newOffset < 0 {
		return 0, fmt.Errorf("negative position")
	}
	r.offset = int(newOffset)
	return newOffset, nil
}

func (r *rewindableReader) Close() error { return nil }

// ValidateUploadedFilename 校验上传后的目标文件路径是否安全
// REQ-E-006: 上传后存储路径必须在白名单目录下
func ValidateUploadedFilename(baseDir, filename string) (string, error) {
	safeName := SanitizeFilename(filename)
	if safeName == "" {
		return "", errors.NewServiceError(errors.ErrInvalidRequest,
			"invalid filename after sanitization")
	}

	targetPath := filepath.Join(baseDir, safeName)

	// 确保最终路径在 baseDir 下
	if !IsPathInBase(targetPath, baseDir) {
		return "", errors.NewServiceError(errors.ErrFileAccessDenied,
			fmt.Sprintf("resolved path is outside allowed directory: %s", targetPath))
	}

	return targetPath, nil
}
