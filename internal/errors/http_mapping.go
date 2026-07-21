package errors

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// REQ-D-009: 错误码与 HTTP 状态码完整映射
// REQ-I-004: HTTP 状态码使用
var codeToHTTPStatus = map[ErrorCode]int{
	ErrAuthRequired:         http.StatusUnauthorized,
	ErrAuthInvalid:          http.StatusUnauthorized,
	ErrServiceNotFound:      http.StatusNotFound,
	ErrServiceExists:        http.StatusConflict,
	ErrServiceRunning:       http.StatusConflict,
	ErrServiceBusy:          http.StatusServiceUnavailable,
	ErrDependencyCycle:      http.StatusUnprocessableEntity,
	ErrDependencyMissing:    http.StatusOK,
	ErrServiceConfigInvalid: http.StatusUnprocessableEntity,
	ErrRuntimeNotFound:      http.StatusUnprocessableEntity,
	ErrRuntimeNotExecutable: http.StatusUnprocessableEntity,
	ErrRuntimeUserNotFound:  http.StatusUnprocessableEntity,
	ErrExtensionNotFound:    http.StatusNotFound,
	ErrExtensionFailed:      http.StatusOK,
	ErrRunNotFound:          http.StatusNotFound,
	ErrRunAlreadyDone:       http.StatusConflict,
	ErrFileNotFound:         http.StatusNotFound,
	ErrFilePermission:       http.StatusForbidden,
	ErrFileTooLarge:         http.StatusRequestEntityTooLarge,
	ErrFileAccessDenied:     http.StatusForbidden,
	ErrInvalidRequest:       http.StatusBadRequest,
	ErrInternal:             http.StatusInternalServerError,
}

// ToHTTPStatus 返回错误码对应的 HTTP 状态码。
func ToHTTPStatus(code ErrorCode) int {
	if status, ok := codeToHTTPStatus[code]; ok {
		return status
	}
	return http.StatusInternalServerError
}

// errorResponse REQ-I-003: 错误响应 JSON 格式
type errorResponse struct {
	Error ServiceError `json:"error"`
}

// WriteErrorResponse 写入统一的 JSON 错误响应。
// REQ-I-003: 错误响应格式 {"error": {"code": "...", "message": "...", "details": {...}}}
// H-04-001 修复：INTERNAL_ERROR 不向客户端暴露原始错误细节，仅记录到服务端日志
// P-01-001 修复：handler 未提供具体消息时，使用 DefaultMessage（含操作建议）作为回退
func WriteErrorResponse(w http.ResponseWriter, err *ServiceError) {
	status := ToHTTPStatus(err.Code)
	// P-01-001: 空消息回退到错误码默认消息（关键错误码含操作性恢复建议）
	if err.Message == "" {
		err.Message = DefaultMessage(err.Code)
	}
	// 对 500 内部错误，将原始消息记录到日志，向客户端返回通用提示
	if status == http.StatusInternalServerError && err.Message != "" {
		slog.Error("internal error", "code", err.Code, "message", err.Message)
		err.Message = "服务器内部错误，请查看服务端日志"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// C-01-001: 记录 JSON 编码/写入错误
	if encErr := json.NewEncoder(w).Encode(errorResponse{Error: *err}); encErr != nil {
		slog.Warn("failed to write error response", "code", err.Code, "error", encErr)
	}
}
