// REQ-D-009: 22 个标准错误码 + HTTP 状态码映射 + 统一错误响应格式

package cli

// cliErrorMessages 错误码到中文提示的映射（与前端 errorCodeMessages 保持一致）
var cliErrorMessages = map[string]string{
	"AUTH_REQUIRED":          "认证失败：未提供有效的 Token",
	"AUTH_INVALID":           "认证失败：Token 无效或已失效",
	"SERVICE_NOT_FOUND":      "服务不存在",
	"SERVICE_EXISTS":         "服务已存在",
	"SERVICE_RUNNING":        "服务运行中，无法执行此操作",
	"SERVICE_BUSY":           "请求并发超限，请稍后重试",
	"DEPENDENCY_CYCLE":       "服务依赖存在循环引用",
	"DEPENDENCY_MISSING":     "依赖服务缺失",
	"SERVICE_CONFIG_INVALID": "服务配置校验失败",
	"RUNTIME_NOT_FOUND":      "运行时未找到",
	"RUNTIME_NOT_EXECUTABLE": "运行时路径不可执行",
	"RUNTIME_USER_NOT_FOUND": "运行时指定的用户不存在",
	"EXTENSION_NOT_FOUND":    "扩展不存在",
	"EXTENSION_FAILED":       "扩展运行失败",
	"RUN_NOT_FOUND":          "任务不存在",
	"RUN_ALREADY_DONE":       "任务已完成，无法取消",
	"FILE_NOT_FOUND":         "文件不存在",
	"FILE_PERMISSION":        "文件权限不足",
	"FILE_TOO_LARGE":         "文件大小超过上传限制",
	"FILE_ACCESS_DENIED":     "文件访问被拒绝",
	"INVALID_REQUEST":        "请求参数错误",
	"INTERNAL_ERROR":         "服务器内部错误，请查看服务端日志",
}

// cliErrorMessage 根据错误码返回中文提示，未匹配时返回空字符串。
func cliErrorMessage(code string) string {
	return cliErrorMessages[code]
}
