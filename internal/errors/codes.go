// Package errors 定义 supd 错误码体系。
// REQ-D-009: 22 个标准错误码 + HTTP 状态码映射 + 统一错误响应格式
// REQ-C-016: 错误处理用 errors.Is/errors.As，禁止 == 比较
package errors

// ErrorCode 错误码类型。
type ErrorCode string

const (
	// 认证相关
	// REQ-D-009: AUTH_REQUIRED — 未提供 token 或 token 无效
	ErrAuthRequired ErrorCode = "AUTH_REQUIRED"
	// REQ-D-009: AUTH_INVALID — token 格式错误或已失效
	ErrAuthInvalid ErrorCode = "AUTH_INVALID"

	// 服务相关
	// REQ-D-009: SERVICE_NOT_FOUND — 服务不存在
	ErrServiceNotFound ErrorCode = "SERVICE_NOT_FOUND"
	// REQ-D-009: SERVICE_EXISTS — 创建服务时名称已存在
	ErrServiceExists ErrorCode = "SERVICE_EXISTS"
	// REQ-D-009: SERVICE_RUNNING — 服务运行中拒绝冒犯操作
	ErrServiceRunning ErrorCode = "SERVICE_RUNNING"
	// REQ-D-009: SERVICE_BUSY — 长轮询并发超限
	ErrServiceBusy ErrorCode = "SERVICE_BUSY"
	// REQ-D-009: DEPENDENCY_CYCLE — 依赖图检测到环
	ErrDependencyCycle ErrorCode = "DEPENDENCY_CYCLE"
	// REQ-D-009: DEPENDENCY_MISSING — 依赖服务不存在（仅警告）
	ErrDependencyMissing ErrorCode = "DEPENDENCY_MISSING"
	// REQ-D-009: SERVICE_CONFIG_INVALID — service.yaml 校验失败
	ErrServiceConfigInvalid ErrorCode = "SERVICE_CONFIG_INVALID"

	// 运行时相关
	// REQ-D-009: RUNTIME_NOT_FOUND — runtime 别名无法解析
	ErrRuntimeNotFound ErrorCode = "RUNTIME_NOT_FOUND"
	// REQ-D-009: RUNTIME_NOT_EXECUTABLE — runtime 路径不可执行
	ErrRuntimeNotExecutable ErrorCode = "RUNTIME_NOT_EXECUTABLE"
	// REQ-D-009: RUNTIME_USER_NOT_FOUND — run_as 指定的用户不存在
	ErrRuntimeUserNotFound ErrorCode = "RUNTIME_USER_NOT_FOUND"

	// 扩展相关
	// REQ-D-009: EXTENSION_NOT_FOUND — 扩展不存在
	ErrExtensionNotFound ErrorCode = "EXTENSION_NOT_FOUND"
	// REQ-D-009: EXTENSION_FAILED — 扩展运行失败
	ErrExtensionFailed ErrorCode = "EXTENSION_FAILED"

	// 任务相关
	// REQ-D-009: RUN_NOT_FOUND — 任务不存在
	ErrRunNotFound ErrorCode = "RUN_NOT_FOUND"
	// REQ-D-009: RUN_ALREADY_DONE — 取消已完成的任务
	ErrRunAlreadyDone ErrorCode = "RUN_ALREADY_DONE"

	// 文件相关
	// REQ-D-009: FILE_NOT_FOUND — 文件不存在
	ErrFileNotFound ErrorCode = "FILE_NOT_FOUND"
	// REQ-D-009: FILE_PERMISSION — 文件权限不足
	ErrFilePermission ErrorCode = "FILE_PERMISSION"
	// REQ-D-009: FILE_TOO_LARGE — 上传超过 max_upload_size_mb
	ErrFileTooLarge ErrorCode = "FILE_TOO_LARGE"
	// REQ-D-009: FILE_ACCESS_DENIED — 路径不在白名单内
	ErrFileAccessDenied ErrorCode = "FILE_ACCESS_DENIED"

	// 通用
	// REQ-D-009: INVALID_REQUEST — 请求参数错误
	ErrInvalidRequest ErrorCode = "INVALID_REQUEST"
	// REQ-D-009: INTERNAL_ERROR — 服务端内部错误
	ErrInternal ErrorCode = "INTERNAL_ERROR"
)

// DefaultMessages 全部 22 个错误码的默认消息（含操作性恢复建议）。
// P-01-001 修复：为所有错误码在"是什么"描述后追加"怎么办"的操作建议。
const (
	// 认证相关
	// MsgAuthRequired AUTH_REQUIRED 默认消息
	MsgAuthRequired = "authentication required; 使用 supd token generate 生成 token"
	// MsgAuthInvalid AUTH_INVALID 默认消息
	MsgAuthInvalid = "authentication invalid; token 已失效，请使用 supd token generate 重新生成"

	// 服务相关
	// MsgServiceNotFound SERVICE_NOT_FOUND 默认消息
	MsgServiceNotFound = "service not found; 请检查服务名或使用 GET /api/services 查看可用服务列表"
	// MsgServiceExists SERVICE_EXISTS 默认消息
	MsgServiceExists = "service already exists; 请使用其他服务名或先删除已有服务"
	// MsgServiceRunning SERVICE_RUNNING 默认消息
	MsgServiceRunning = "service is running; 请先停止服务再执行此操作"
	// MsgServiceBusy SERVICE_BUSY 默认消息
	MsgServiceBusy = "service is busy; 请求并发超限，请稍后重试"
	// MsgDependencyCycle DEPENDENCY_CYCLE 默认消息
	MsgDependencyCycle = "dependency cycle detected; 请检查 depends_on 配置并移除循环引用"
	// MsgDependencyMissing DEPENDENCY_MISSING 默认消息
	MsgDependencyMissing = "dependency missing; 请检查 depends_on 中的服务名是否已定义"
	// MsgServiceConfigInvalid SERVICE_CONFIG_INVALID 默认消息
	MsgServiceConfigInvalid = "service config invalid; 使用 supd validate 校验配置文件"

	// 运行时相关
	// MsgRuntimeNotFound RUNTIME_NOT_FOUND 默认消息
	MsgRuntimeNotFound = "runtime not found; 使用 supd runtimes list 查看可用运行时"
	// MsgRuntimeNotExecutable RUNTIME_NOT_EXECUTABLE 默认消息
	MsgRuntimeNotExecutable = "runtime not executable; 请检查可执行文件路径和权限"
	// MsgRuntimeUserNotFound RUNTIME_USER_NOT_FOUND 默认消息
	MsgRuntimeUserNotFound = "runtime user not found; 请检查 run_as 指定的用户是否存在"

	// 扩展相关
	// MsgExtensionNotFound EXTENSION_NOT_FOUND 默认消息
	MsgExtensionNotFound = "extension not found; 请使用 supd ext list 查看可用扩展"
	// MsgExtensionFailed EXTENSION_FAILED 默认消息
	MsgExtensionFailed = "extension failed; 请查看扩展运行日志定位具体错误"

	// 任务相关
	// MsgRunNotFound RUN_NOT_FOUND 默认消息
	MsgRunNotFound = "run not found; 请使用 GET /api/runs 查看任务列表"
	// MsgRunAlreadyDone RUN_ALREADY_DONE 默认消息
	MsgRunAlreadyDone = "run already done; 任务已完成，无法取消"

	// 文件相关
	// MsgFileNotFound FILE_NOT_FOUND 默认消息
	MsgFileNotFound = "file not found; 请检查文件路径是否正确"
	// MsgFilePermission FILE_PERMISSION 默认消息
	MsgFilePermission = "file permission denied; 请检查文件读写权限"
	// MsgFileTooLarge FILE_TOO_LARGE 默认消息
	MsgFileTooLarge = "file too large; 上传限制为 100MB"
	// MsgFileAccessDenied FILE_ACCESS_DENIED 默认消息
	MsgFileAccessDenied = "file access denied; 路径不在白名单内，请使用工作目录下的文件"

	// 通用
	// MsgInvalidRequest INVALID_REQUEST 默认消息
	MsgInvalidRequest = "invalid request; 请检查请求参数格式"
	// MsgInternal INTERNAL_ERROR 默认消息
	MsgInternal = "internal error; 请查看服务端日志并联系管理员"
)

// DefaultMessage 返回错误码对应的默认消息。
// P-01-001: 对全部 22 个错误码返回含操作性恢复建议的消息（MsgXxx）。
// 当 handler 未提供具体消息（空字符串）时，WriteErrorResponse 使用此消息。
func DefaultMessage(code ErrorCode) string {
	switch code {
	case ErrAuthRequired:
		return MsgAuthRequired
	case ErrAuthInvalid:
		return MsgAuthInvalid
	case ErrServiceNotFound:
		return MsgServiceNotFound
	case ErrServiceExists:
		return MsgServiceExists
	case ErrServiceRunning:
		return MsgServiceRunning
	case ErrServiceBusy:
		return MsgServiceBusy
	case ErrDependencyCycle:
		return MsgDependencyCycle
	case ErrDependencyMissing:
		return MsgDependencyMissing
	case ErrServiceConfigInvalid:
		return MsgServiceConfigInvalid
	case ErrRuntimeNotFound:
		return MsgRuntimeNotFound
	case ErrRuntimeNotExecutable:
		return MsgRuntimeNotExecutable
	case ErrRuntimeUserNotFound:
		return MsgRuntimeUserNotFound
	case ErrExtensionNotFound:
		return MsgExtensionNotFound
	case ErrExtensionFailed:
		return MsgExtensionFailed
	case ErrRunNotFound:
		return MsgRunNotFound
	case ErrRunAlreadyDone:
		return MsgRunAlreadyDone
	case ErrFileNotFound:
		return MsgFileNotFound
	case ErrFilePermission:
		return MsgFilePermission
	case ErrFileTooLarge:
		return MsgFileTooLarge
	case ErrFileAccessDenied:
		return MsgFileAccessDenied
	case ErrInvalidRequest:
		return MsgInvalidRequest
	case ErrInternal:
		return MsgInternal
	}
	return string(code)
}
