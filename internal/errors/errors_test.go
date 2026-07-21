package errors

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestErrorCodeConstants(t *testing.T) {
	// REQ-D-009: 22 个标准错误码
	codes := []ErrorCode{
		ErrAuthRequired,
		ErrAuthInvalid,
		ErrServiceNotFound,
		ErrServiceExists,
		ErrServiceRunning,
		ErrServiceBusy,
		ErrDependencyCycle,
		ErrDependencyMissing,
		ErrServiceConfigInvalid,
		ErrRuntimeNotFound,
		ErrRuntimeNotExecutable,
		ErrRuntimeUserNotFound,
		ErrExtensionNotFound,
		ErrExtensionFailed,
		ErrRunNotFound,
		ErrRunAlreadyDone,
		ErrFileNotFound,
		ErrFilePermission,
		ErrFileTooLarge,
		ErrFileAccessDenied,
		ErrInvalidRequest,
		ErrInternal,
	}

	if len(codes) != 22 {
		t.Fatalf("expected 22 error codes, got %d", len(codes))
	}

	// 验证每个错误码值唯一
	seen := make(map[ErrorCode]bool)
	for _, c := range codes {
		if seen[c] {
			t.Fatalf("duplicate error code: %s", c)
		}
		seen[c] = true
	}

	// 验证错误码值与需求规格说明书5.4一致
	expectedValues := map[ErrorCode]string{
		ErrAuthRequired:         "AUTH_REQUIRED",
		ErrAuthInvalid:          "AUTH_INVALID",
		ErrServiceNotFound:      "SERVICE_NOT_FOUND",
		ErrServiceExists:        "SERVICE_EXISTS",
		ErrServiceRunning:       "SERVICE_RUNNING",
		ErrServiceBusy:          "SERVICE_BUSY",
		ErrDependencyCycle:      "DEPENDENCY_CYCLE",
		ErrDependencyMissing:    "DEPENDENCY_MISSING",
		ErrServiceConfigInvalid: "SERVICE_CONFIG_INVALID",
		ErrRuntimeNotFound:      "RUNTIME_NOT_FOUND",
		ErrRuntimeNotExecutable: "RUNTIME_NOT_EXECUTABLE",
		ErrRuntimeUserNotFound:  "RUNTIME_USER_NOT_FOUND",
		ErrExtensionNotFound:    "EXTENSION_NOT_FOUND",
		ErrExtensionFailed:      "EXTENSION_FAILED",
		ErrRunNotFound:          "RUN_NOT_FOUND",
		ErrRunAlreadyDone:       "RUN_ALREADY_DONE",
		ErrFileNotFound:         "FILE_NOT_FOUND",
		ErrFilePermission:       "FILE_PERMISSION",
		ErrFileTooLarge:         "FILE_TOO_LARGE",
		ErrFileAccessDenied:     "FILE_ACCESS_DENIED",
		ErrInvalidRequest:       "INVALID_REQUEST",
		ErrInternal:             "INTERNAL_ERROR",
	}

	for code, expected := range expectedValues {
		if string(code) != expected {
			t.Errorf("error code %s: expected value %q, got %q", code, expected, string(code))
		}
	}
}

func TestHTTPMappingCompleteness(t *testing.T) {
	// REQ-D-009: 所有22个错误码必须有HTTP状态码映射
	allCodes := []ErrorCode{
		ErrAuthRequired,
		ErrAuthInvalid,
		ErrServiceNotFound,
		ErrServiceExists,
		ErrServiceRunning,
		ErrServiceBusy,
		ErrDependencyCycle,
		ErrDependencyMissing,
		ErrServiceConfigInvalid,
		ErrRuntimeNotFound,
		ErrRuntimeNotExecutable,
		ErrRuntimeUserNotFound,
		ErrExtensionNotFound,
		ErrExtensionFailed,
		ErrRunNotFound,
		ErrRunAlreadyDone,
		ErrFileNotFound,
		ErrFilePermission,
		ErrFileTooLarge,
		ErrFileAccessDenied,
		ErrInvalidRequest,
		ErrInternal,
	}

	for _, code := range allCodes {
		_, ok := codeToHTTPStatus[code]
		if !ok {
			t.Errorf("error code %s: missing HTTP status mapping", code)
		}
	}
}

func TestHTTPMappingValues(t *testing.T) {
	// REQ-D-009: 验证关键HTTP状态码映射与需求规格说明书5.4一致
	tests := []struct {
		code     ErrorCode
		expected int
	}{
		{ErrAuthRequired, http.StatusUnauthorized},
		{ErrAuthInvalid, http.StatusUnauthorized},
		{ErrServiceNotFound, http.StatusNotFound},
		{ErrServiceExists, http.StatusConflict},
		{ErrServiceRunning, http.StatusConflict},
		{ErrServiceBusy, http.StatusServiceUnavailable},
		{ErrDependencyCycle, http.StatusUnprocessableEntity},
		{ErrDependencyMissing, http.StatusOK},
		{ErrServiceConfigInvalid, http.StatusUnprocessableEntity},
		{ErrRuntimeNotFound, http.StatusUnprocessableEntity},
		{ErrRuntimeNotExecutable, http.StatusUnprocessableEntity},
		{ErrRuntimeUserNotFound, http.StatusUnprocessableEntity},
		{ErrExtensionNotFound, http.StatusNotFound},
		{ErrExtensionFailed, http.StatusOK},
		{ErrRunNotFound, http.StatusNotFound},
		{ErrRunAlreadyDone, http.StatusConflict},
		{ErrFileNotFound, http.StatusNotFound},
		{ErrFilePermission, http.StatusForbidden},
		{ErrFileTooLarge, http.StatusRequestEntityTooLarge},
		{ErrFileAccessDenied, http.StatusForbidden},
		{ErrInvalidRequest, http.StatusBadRequest},
		{ErrInternal, http.StatusInternalServerError},
	}

	for _, tt := range tests {
		got := ToHTTPStatus(tt.code)
		if got != tt.expected {
			t.Errorf("ToHTTPStatus(%s): expected %d, got %d", tt.code, tt.expected, got)
		}
	}
}

func TestToHTTPStatusUnknown(t *testing.T) {
	// 未知错误码应返回500
	got := ToHTTPStatus(ErrorCode("UNKNOWN"))
	if got != http.StatusInternalServerError {
		t.Errorf("ToHTTPStatus(UNKNOWN): expected %d, got %d", http.StatusInternalServerError, got)
	}
}

func TestNewServiceError(t *testing.T) {
	err := NewServiceError(ErrServiceNotFound, "service not found")
	if err.Code != ErrServiceNotFound {
		t.Errorf("expected code %s, got %s", ErrServiceNotFound, err.Code)
	}
	if err.Message != "service not found" {
		t.Errorf("expected message 'service not found', got %q", err.Message)
	}
	if err.Details != nil {
		t.Errorf("expected nil details, got %v", err.Details)
	}
}

func TestServiceErrorWithFieldErrors(t *testing.T) {
	err := NewServiceError(ErrServiceConfigInvalid, "config validation failed")
	err.WithFieldErrors(
		FieldError{Field: "command", Message: "必须是数组"},
		FieldError{Field: "user", Message: "用户不存在"},
	)

	if err.Details == nil {
		t.Fatal("expected details to be set")
	}
	fields, ok := err.Details["fields"].([]FieldError)
	if !ok {
		t.Fatalf("expected fields to be []FieldError, got %T", err.Details["fields"])
	}
	if len(fields) != 2 {
		t.Fatalf("expected 2 field errors, got %d", len(fields))
	}
	if fields[0].Field != "command" {
		t.Errorf("expected field 'command', got %q", fields[0].Field)
	}
	if fields[1].Field != "user" {
		t.Errorf("expected field 'user', got %q", fields[1].Field)
	}
}

func TestServiceErrorWithDetails(t *testing.T) {
	err := NewServiceError(ErrInternal, "internal error")
	err.WithDetails(map[string]any{"service": "web"})

	if err.Details == nil {
		t.Fatal("expected details to be set")
	}
	if v, ok := err.Details["service"].(string); !ok || v != "web" {
		t.Errorf("expected details['service']='web', got %v", err.Details["service"])
	}
}

func TestServiceErrorImplementsError(t *testing.T) {
	// REQ-C-016: 支持 errors.As 检查
	err := NewServiceError(ErrServiceNotFound, "not found")

	var se *ServiceError
	if !errors.As(err, &se) {
		t.Error("ServiceError should be accessible via errors.As")
	}
	if se.Code != ErrServiceNotFound {
		t.Errorf("expected code %s, got %s", ErrServiceNotFound, se.Code)
	}
}

func TestServiceErrorErrorString(t *testing.T) {
	errNoDetails := NewServiceError(ErrInternal, "something broke")
	expectedNoDetails := "INTERNAL_ERROR: something broke"
	if errNoDetails.Error() != expectedNoDetails {
		t.Errorf("expected %q, got %q", expectedNoDetails, errNoDetails.Error())
	}

	errWithDetails := NewServiceError(ErrInternal, "something broke")
	errWithDetails.WithDetails(map[string]any{"key": "value"})
	s := errWithDetails.Error()
	if len(s) == 0 {
		t.Error("expected non-empty error string")
	}
	// 应包含 details 信息
	if s == expectedNoDetails {
		t.Errorf("expected details in error string, got %q", s)
	}
}

func TestWriteErrorResponse(t *testing.T) {
	// REQ-I-003: 错误响应格式 {"error": {"code": "...", "message": "..."}}
	err := NewServiceError(ErrServiceNotFound, "service xyz not found")

	rr := httptest.NewRecorder()
	WriteErrorResponse(rr, err)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, rr.Code)
	}

	ct := rr.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %q", ct)
	}

	var resp map[string]any
	if decodeErr := json.NewDecoder(rr.Body).Decode(&resp); decodeErr != nil {
		t.Fatalf("failed to decode response: %v", decodeErr)
	}

	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'error' key in response, got %T", resp["error"])
	}

	if errObj["code"] != "SERVICE_NOT_FOUND" {
		t.Errorf("expected code 'SERVICE_NOT_FOUND', got %v", errObj["code"])
	}
	if errObj["message"] != "service xyz not found" {
		t.Errorf("expected message 'service xyz not found', got %v", errObj["message"])
	}
}

func TestWriteErrorResponseWithFields(t *testing.T) {
	// REQ-I-003: 校验错误响应支持多字段
	err := NewServiceError(ErrServiceConfigInvalid, "validation failed")
	err.WithFieldErrors(
		FieldError{Field: "command", Message: "必须是数组"},
	)

	rr := httptest.NewRecorder()
	WriteErrorResponse(rr, err)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected status %d, got %d", http.StatusUnprocessableEntity, rr.Code)
	}

	var resp map[string]any
	if decodeErr := json.NewDecoder(rr.Body).Decode(&resp); decodeErr != nil {
		t.Fatalf("failed to decode response: %v", decodeErr)
	}

	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'error' key in response, got %T", resp["error"])
	}

	details, ok := errObj["details"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'details' in error, got %T", errObj["details"])
	}

	fields, ok := details["fields"].([]any)
	if !ok {
		t.Fatalf("expected 'fields' to be array, got %T", details["fields"])
	}
	if len(fields) != 1 {
		t.Fatalf("expected 1 field error, got %d", len(fields))
	}

	field0, ok := fields[0].(map[string]any)
	if !ok {
		t.Fatalf("expected field to be object, got %T", fields[0])
	}
	if field0["field"] != "command" {
		t.Errorf("expected field 'command', got %v", field0["field"])
	}
}

func TestWriteErrorResponseInternalError(t *testing.T) {
	err := NewServiceError(ErrInternal, "unexpected failure")

	rr := httptest.NewRecorder()
	WriteErrorResponse(rr, err)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
	}
}

// TestDefaultMessage P-01-001: 验证全部 22 个错误码的默认消息含操作性恢复建议
func TestDefaultMessage(t *testing.T) {
	tests := []struct {
		code     ErrorCode
		expected string
	}{
		{ErrAuthRequired, MsgAuthRequired},
		{ErrAuthInvalid, MsgAuthInvalid},
		{ErrServiceNotFound, MsgServiceNotFound},
		{ErrServiceExists, MsgServiceExists},
		{ErrServiceRunning, MsgServiceRunning},
		{ErrServiceBusy, MsgServiceBusy},
		{ErrDependencyCycle, MsgDependencyCycle},
		{ErrDependencyMissing, MsgDependencyMissing},
		{ErrServiceConfigInvalid, MsgServiceConfigInvalid},
		{ErrRuntimeNotFound, MsgRuntimeNotFound},
		{ErrRuntimeNotExecutable, MsgRuntimeNotExecutable},
		{ErrRuntimeUserNotFound, MsgRuntimeUserNotFound},
		{ErrExtensionNotFound, MsgExtensionNotFound},
		{ErrExtensionFailed, MsgExtensionFailed},
		{ErrRunNotFound, MsgRunNotFound},
		{ErrRunAlreadyDone, MsgRunAlreadyDone},
		{ErrFileNotFound, MsgFileNotFound},
		{ErrFilePermission, MsgFilePermission},
		{ErrFileTooLarge, MsgFileTooLarge},
		{ErrFileAccessDenied, MsgFileAccessDenied},
		{ErrInvalidRequest, MsgInvalidRequest},
		{ErrInternal, MsgInternal},
	}
	for _, tt := range tests {
		got := DefaultMessage(tt.code)
		if got != tt.expected {
			t.Errorf("DefaultMessage(%s): expected %q, got %q", tt.code, tt.expected, got)
		}
	}
}

// TestWriteErrorResponseEmptyMessageFallback P-01-001: 空 message 时回退到含操作建议的默认消息
func TestWriteErrorResponseEmptyMessageFallback(t *testing.T) {
	err := NewServiceError(ErrServiceNotFound, "")

	rr := httptest.NewRecorder()
	WriteErrorResponse(rr, err)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, rr.Code)
	}

	var resp map[string]any
	if decodeErr := json.NewDecoder(rr.Body).Decode(&resp); decodeErr != nil {
		t.Fatalf("failed to decode response: %v", decodeErr)
	}

	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'error' key in response, got %T", resp["error"])
	}

	if errObj["code"] != "SERVICE_NOT_FOUND" {
		t.Errorf("expected code 'SERVICE_NOT_FOUND', got %v", errObj["code"])
	}
	// 应回退到含操作建议的 MsgServiceNotFound
	if errObj["message"] != MsgServiceNotFound {
		t.Errorf("expected fallback message %q, got %v", MsgServiceNotFound, errObj["message"])
	}
}

// TestWriteErrorResponseEmptyMessageInternalError P-01-001: 空 message 的内部错误仍走 H-04-001 屏蔽逻辑
func TestWriteErrorResponseEmptyMessageInternalError(t *testing.T) {
	err := NewServiceError(ErrInternal, "")

	rr := httptest.NewRecorder()
	WriteErrorResponse(rr, err)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
	}

	var resp map[string]any
	if decodeErr := json.NewDecoder(rr.Body).Decode(&resp); decodeErr != nil {
		t.Fatalf("failed to decode response: %v", decodeErr)
	}

	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'error' key in response, got %T", resp["error"])
	}
	// 空 message 回退后仍应被 H-04-001 屏蔽为通用提示
	if errObj["message"] != "服务器内部错误，请查看服务端日志" {
		t.Errorf("expected masked message, got %v", errObj["message"])
	}
}
