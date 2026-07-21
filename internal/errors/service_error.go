package errors

import "fmt"

// ServiceError 统一错误响应结构。
// REQ-I-003: 错误响应 JSON 格式含 code/message/details。
// REQ-C-016: 实现 error 接口，支持 errors.As 检查。
type ServiceError struct {
	Code    ErrorCode      `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// FieldError 字段级校验错误。
// REQ-I-003: 校验错误响应支持多字段。
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// NewServiceError 创建统一错误。
func NewServiceError(code ErrorCode, message string) *ServiceError {
	return &ServiceError{
		Code:    code,
		Message: message,
	}
}

// WithFieldErrors 添加字段校验错误。
// REQ-I-003: 校验错误响应支持多字段，fields 放在 details 中。
func (e *ServiceError) WithFieldErrors(fields ...FieldError) *ServiceError {
	if e.Details == nil {
		e.Details = make(map[string]any)
	}
	e.Details["fields"] = fields
	return e
}

// WithDetails 添加额外上下文信息。
func (e *ServiceError) WithDetails(details map[string]any) *ServiceError {
	if e.Details == nil {
		e.Details = make(map[string]any)
	}
	for k, v := range details {
		e.Details[k] = v
	}
	return e
}

// Error 实现 error 接口，支持 errors.As 检查。
// REQ-C-016: 错误处理用 errors.Is/errors.As。
func (e *ServiceError) Error() string {
	if len(e.Details) > 0 {
		return fmt.Sprintf("%s: %s (details: %v)", e.Code, e.Message, e.Details)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}
