package core

import (
	"fmt"
	"log/slog"
	"os"
	"syscall"

	"github.com/supdorg/supd/internal/errors"
	"github.com/supdorg/supd/internal/identity"
)

// ResolveServiceCredential 解析服务身份配置为 syscall.Credential。
//
// REQ-F-023, 规格说明书 §2.2.13:
//   - spec 为空（User 和 UID 均未设置）→ 继承 supd 启动用户，返回 (nil, nil)
//   - User 模式（spec.User 非空）→ 通过 user.Lookup 解析，查找失败返回 *ServiceError
//   - UID 模式（spec.UID 非 0）→ 直接使用 uid/gid/groups，不查 /etc/passwd
//   - 非 root 启动 supd 且目标 uid ≠ 当前 uid → 返回 *ServiceError(ErrRuntimeUserNotFound)（严格拒绝）
//   - 非 root 启动 supd 且目标 uid = 当前 uid → 允许，返回 nil（无需 Credential）
//   - root 启动 → 返回目标用户的 Credential
//
// 与 extension.ResolveRunAs 的语义差异（重要）：
//   - 扩展非 root 切换其他用户 → 记录警告以当前用户运行（宽松）
//   - 服务非 root 切换其他用户 → 拒绝启动该服务（规格 line 700，严格）
//
// 参数:
//   - serviceName: 服务名（用于错误日志和错误消息定位）
//   - spec: 身份配置（User 模式或 UID 模式，由 config 校验层保证互斥）
//   - configPath: service.yaml 路径（用于错误消息中的"配置位置"提示，规格 line 700 要求）
func ResolveServiceCredential(serviceName string, spec identity.CredentialSpec, configPath string) (*syscall.Credential, error) {
	// spec 为空 → 继承 supd 启动用户，credential=nil 即不切换
	if spec.IsEmpty() {
		return nil, nil
	}

	// configPath 兜底，避免错误消息中"配置位置"显示空
	if configPath == "" {
		configPath = "<unknown>"
	}

	// 解析 spec 为 uid/gid/groups
	uid, gid, groups, err := identity.ResolveSpec(spec)
	if err != nil {
		slog.Error("service start rejected: credential resolve failed",
			"service", serviceName,
			"user", spec.User,
			"uid", spec.UID,
			"config_path", configPath,
			"error", err)
		// 构建可读的身份描述
		identDesc := spec.User
		if spec.IsUIDMode() {
			identDesc = fmt.Sprintf("uid=%d", spec.UID)
		}
		return nil, errors.NewServiceError(errors.ErrRuntimeUserNotFound,
			fmt.Sprintf("service %s: configured identity %q resolve failed: %v; "+
				"请检查 service.yaml 的 user/uid 字段是否正确，"+
				"或修改为空以继承 supd 启动用户（配置位置：%s）",
				serviceName, identDesc, err, configPath))
	}

	// 非 root 启动 supd 时，禁止切换到其他用户（规格 line 700: 拒绝启动该服务）
	currentUID := uint32(os.Getuid())
	if currentUID != 0 && uid != currentUID {
		identDesc := spec.User
		if spec.IsUIDMode() {
			identDesc = fmt.Sprintf("uid=%d", spec.UID)
		}
		slog.Error("service start rejected: non-root supd cannot switch user",
			"service", serviceName,
			"configured_identity", identDesc,
			"configured_uid", uid,
			"current_uid", currentUID,
			"config_path", configPath)
		return nil, errors.NewServiceError(errors.ErrRuntimeUserNotFound,
			fmt.Sprintf("service %s: supd 未以 root 运行（current uid=%d），"+
				"无法切换到配置的身份 %q (uid=%d)；"+
				"请以 root 启动 supd，或将 service.yaml 的 user/uid 字段改为当前用户或留空以继承 supd 用户（配置位置：%s）",
				serviceName, currentUID, identDesc, uid, configPath))
	}

	// 显式指定为当前用户（非 root 环境）时，无需设置 Credential，
	// 避免 setuid 系统调用在非 root 环境下被拒绝（EPERM）。
	if currentUID != 0 && uid == currentUID {
		return nil, nil
	}

	// root 启动 → 构造 Credential 切换到目标用户
	return identity.BuildCredential(uid, gid, groups), nil
}

// StartServiceProcess 解析服务身份并启动子进程。
//
// 是 StartProcess 的服务专用包装：先 ResolveServiceCredential 解析身份配置，
// 再调 StartProcess。失败时返回 *errors.ServiceError（含 ErrRuntimeUserNotFound），
// 调用方应通过状态机转移（EventMaxRetries）+ writeServiceLog + slog.Error 记录，
// API 层通过 errors.As 识别并映射 HTTP 422。
//
// 参数:
//   - name: 服务名
//   - command: 启动命令（command[0] 为可执行文件）
//   - env: 环境变量
//   - dir: 工作目录
//   - spec: 身份配置（User 模式或 UID 模式，由 config 校验层保证互斥）
//   - configPath: service.yaml 路径（用于错误消息中的配置位置提示）
//   - extraFiles: 额外传递给子进程的文件描述符（如 fd_notify readiness 的管道写端）
func StartServiceProcess(name string, command, env []string, dir string, spec identity.CredentialSpec, configPath string, extraFiles ...*os.File) (*Process, error) {
	credential, err := ResolveServiceCredential(name, spec, configPath)
	if err != nil {
		return nil, err
	}
	return StartProcess(name, command, env, dir, credential, extraFiles...)
}
