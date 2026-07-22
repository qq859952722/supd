package extension

import (
	"fmt"
	"os"
	"syscall"

	"github.com/supdorg/supd/internal/identity"
)

// LookupUserGroups 通过用户名查找 uid/gid 及所有补充组（委托到 identity 包）。
// 保留导出以兼容现有测试；新代码应直接调用 identity.LookupUserGroups。
func LookupUserGroups(username string) (uid uint32, gid uint32, groups []uint32, err error) {
	return identity.LookupUserGroups(username)
}

// BuildCredential 构造 syscall.Credential（委托到 identity 包）。
// 保留导出以兼容现有测试；新代码应直接调用 identity.BuildCredential。
func BuildCredential(uid, gid uint32, groups []uint32) *syscall.Credential {
	return identity.BuildCredential(uid, gid, groups)
}

// GetCurrentUser 获取当前用户的 uid/gid/补充组（委托到 identity 包）。
// 保留导出以兼容现有测试；新代码应直接调用 identity.GetCurrentUser。
func GetCurrentUser() (uid uint32, gid uint32, groups []uint32, err error) {
	return identity.GetCurrentUser()
}

// ResolveRunAs 解析扩展的 run_as 身份，返回 uid/gid/补充组
// REQ-F-023, REQ-P-005, 2.2.13: run_as 字段语义
//   - 全局扩展：run_as 为空时默认为 supd 启动用户
//   - 服务级扩展：run_as 为空时默认为服务的 user 字段值
//   - 显式 root → 用 root
//   - 显式 <用户名> → 用指定用户
//   - 非 root 时切换其他用户仅记录警告，不报错
func ResolveRunAs(runAs string, serviceUser string, isServiceLevel bool) (uid uint32, gid uint32, groups []uint32, warn string, err error) {
	targetUser := runAs

	// 确定目标用户名
	if targetUser == "" {
		if isServiceLevel {
			// 服务级扩展：默认 run_as = 服务的 user 字段值
			if serviceUser != "" {
				targetUser = serviceUser
			} else {
				// 服务 user 也为空，使用当前用户
				curUID, curGID, curGroups, curErr := GetCurrentUser()
				if curErr != nil {
					return 0, 0, nil, "", fmt.Errorf("get current user: %w", curErr)
				}
				return curUID, curGID, curGroups, "", nil
			}
		} else {
			// 全局扩展：默认 run_as = supd 启动用户
			curUID, curGID, curGroups, curErr := GetCurrentUser()
			if curErr != nil {
				return 0, 0, nil, "", fmt.Errorf("get current user: %w", curErr)
			}
			return curUID, curGID, curGroups, "", nil
		}
	}

	// 查找目标用户信息
	targetUID, targetGID, targetGroups, lookupErr := LookupUserGroups(targetUser)
	if lookupErr != nil {
		// N-04-USER-CRED 修复：用户不存在时返回详细错误消息（包含解决方法）
		// 用户要求"详细的记录并提示错误原因和解决方法"
		return 0, 0, nil, "", fmt.Errorf("lookup user %s: %w; 请在容器内创建该用户（如 `adduser %s` 或 `useradd %s`），或修改 meta.yaml 的 run_as 字段为空以继承 supd 启动用户",
			targetUser, lookupErr, targetUser, targetUser)
	}

	// 非 root 时检查能否切换到目标用户
	// REQ-P-005: 非 root 启动 supd 时，如果 run_as 指定了其他用户，记录警告日志，以当前用户身份运行
	currentUID := uint32(os.Getuid())
	if currentUID != 0 && targetUID != currentUID {
		// 非 root 且目标用户不是当前用户：返回警告，使用当前用户身份
		curUID, curGID, curGroups, curErr := GetCurrentUser()
		if curErr != nil {
			return 0, 0, nil, "", fmt.Errorf("get current user: %w", curErr)
		}
		warn = fmt.Sprintf("supd not running as root, cannot switch to user %s (uid=%d); running as current user (uid=%d)", targetUser, targetUID, curUID)
		return curUID, curGID, curGroups, warn, nil
	}

	return targetUID, targetGID, targetGroups, "", nil
}
