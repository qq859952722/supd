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

// ResolveRunAs 解析扩展的执行身份，返回 uid/gid/补充组
// REQ-F-023, REQ-P-005, 2.2.13: run_as 字段语义（User 模式与 UID 模式互斥）
//   - extSpec 非空（扩展自身配置了 run_as 或 run_as_uid）→ 用扩展自身身份
//   - extSpec 为空且服务级扩展 → 继承 serviceSpec（服务的身份配置）
//   - extSpec 为空且全局扩展 → 继承 supd 启动用户
//   - 非 root 时切换其他用户仅记录警告，不报错（宽松语义，区别于服务的严格拒绝）
//
// 参数:
//   - extSpec: 扩展自身的身份配置（run_as 或 run_as_uid/run_as_gid/run_as_groups）
//   - serviceSpec: 服务的身份配置（服务级扩展继承用，全局扩展为空）
//   - isServiceLevel: 是否为服务级扩展
func ResolveRunAs(extSpec identity.CredentialSpec, serviceSpec identity.CredentialSpec, isServiceLevel bool) (uid uint32, gid uint32, groups []uint32, warn string, err error) {
	// 确定最终使用的 spec
	spec := extSpec
	if spec.IsEmpty() {
		if isServiceLevel {
			// 服务级扩展：默认继承服务的身份配置
			spec = serviceSpec
		}
		// spec 仍为空 → 继承 supd 启动用户
		if spec.IsEmpty() {
			curUID, curGID, curGroups, curErr := GetCurrentUser()
			if curErr != nil {
				return 0, 0, nil, "", fmt.Errorf("get current user: %w", curErr)
			}
			return curUID, curGID, curGroups, "", nil
		}
	}

	// 解析 spec 为 uid/gid/groups
	uid, gid, groups, err = identity.ResolveSpec(spec)
	if err != nil {
		// N-04-USER-CRED 修复：身份解析失败时返回详细错误消息（包含解决方法）
		identDesc := spec.User
		if spec.IsUIDMode() {
			identDesc = fmt.Sprintf("uid=%d", spec.UID)
		}
		return 0, 0, nil, "", fmt.Errorf("resolve identity %s: %w; 请检查 meta.yaml 的 run_as/run_as_uid 字段是否正确，或修改为空以继承 supd 启动用户",
			identDesc, err)
	}

	// 非 root 时检查能否切换到目标用户
	// REQ-P-005: 非 root 启动 supd 时，如果 run_as 指定了其他用户，记录警告日志，以当前用户身份运行
	currentUID := uint32(os.Getuid())
	if currentUID != 0 && uid != currentUID {
		// 非 root 且目标用户不是当前用户：返回警告，使用当前用户身份
		curUID, curGID, curGroups, curErr := GetCurrentUser()
		if curErr != nil {
			return 0, 0, nil, "", fmt.Errorf("get current user: %w", curErr)
		}
		identDesc := spec.User
		if spec.IsUIDMode() {
			identDesc = fmt.Sprintf("uid=%d", spec.UID)
		}
		warn = fmt.Sprintf("supd not running as root, cannot switch to %s (uid=%d); running as current user (uid=%d)", identDesc, uid, curUID)
		return curUID, curGID, curGroups, warn, nil
	}

	return uid, gid, groups, "", nil
}
