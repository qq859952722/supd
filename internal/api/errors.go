package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	svcerr "github.com/supdorg/supd/internal/errors"
)

// respondError 写入标准错误响应。
// REQ-I-003: 错误响应 JSON 格式 {"error": {"code": "...", "message": "...", "details": {...}}}
func respondError(w http.ResponseWriter, code svcerr.ErrorCode, message string) {
	err := svcerr.NewServiceError(code, message)
	svcerr.WriteErrorResponse(w, err)
}

// respondProviderError 将 provider 返回的错误写入响应。
// 若 err 是 *errors.ServiceError（带错误码），按其 Code 映射 HTTP 状态码；
// 否则统一为 ErrInternal（500）。
// N-03-DELETE-404 修复：避免 ServiceError 被降级为 500
func respondProviderError(w http.ResponseWriter, err error) {
	var se *svcerr.ServiceError
	if errors.As(err, &se) {
		svcerr.WriteErrorResponse(w, se)
		return
	}
	svcerr.WriteErrorResponse(w, svcerr.NewServiceError(svcerr.ErrInternal, err.Error()))
}

// respondFieldErrors 写入字段校验错误响应。
// REQ-I-003: 校验错误响应支持多字段，fields 放在 details 中。
func respondFieldErrors(w http.ResponseWriter, code svcerr.ErrorCode, message string, fields ...svcerr.FieldError) {
	err := svcerr.NewServiceError(code, message).WithFieldErrors(fields...)
	svcerr.WriteErrorResponse(w, err)
}

// respondJSON 写入成功 JSON 响应。
func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// C-01-001: 记录 JSON 编码/写入错误
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Warn("failed to write JSON response", "error", err)
	}
}

// decodeJSONBody 解析请求体 JSON。
// N-01-001 修复：空 body 时返回友好错误消息（避免向用户暴露 "EOF"）。
// 调用方应在调用前自行设置 MaxBytesReader 限制请求体大小。
// 返回的错误消息可直接作为用户可见的错误响应。
func decodeJSONBody(r *http.Request, dst any) error {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("请求体不能为空")
		}
		return fmt.Errorf("invalid request body: %w", err)
	}
	return nil
}
