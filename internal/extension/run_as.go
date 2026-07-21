package extension

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
	"syscall"
)

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
		return 0, 0, nil, "", fmt.Errorf("lookup user %s: %w", targetUser, lookupErr)
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

// BuildCredential 构造 syscall.Credential，设置 Uid/Gid/Groups
// REQ-F-023, 2.2.13: 用 cmd.SysProcAttr.Credential 设置执行身份
// 必须设置补充组：通过 syscall.Setgroups 设置目标用户的所有补充组
// Groups 必须包含 gid 本身
func BuildCredential(uid, gid uint32, groups []uint32) *syscall.Credential {
	// 确保 groups 中包含 gid 本身
	hasGID := false
	for _, g := range groups {
		if g == gid {
			hasGID = true
			break
		}
	}
	if !hasGID {
		groups = append(groups, gid)
	}

	return &syscall.Credential{
		Uid:    uid,
		Gid:    gid,
		Groups: groups,
	}
}

// GetCurrentUser 获取当前用户的 uid/gid/补充组
// REQ-F-023: 全局扩展默认 run_as = supd 启动用户
func GetCurrentUser() (uid uint32, gid uint32, groups []uint32, err error) {
	uid = uint32(os.Getuid())
	gid = uint32(os.Getgid())

	// 获取当前用户名以查找补充组
	u, lookupErr := user.LookupId(strconv.Itoa(int(uid)))
	if lookupErr != nil {
		// 查找失败时仅返回 uid/gid，补充组为空
		return uid, gid, nil, nil
	}

	groupIDs, groupErr := u.GroupIds()
	if groupErr != nil {
		// 获取组失败时仅返回 uid/gid
		return uid, gid, nil, nil
	}

	groups = make([]uint32, 0, len(groupIDs))
	for _, gidStr := range groupIDs {
		g, parseErr := strconv.ParseUint(gidStr, 10, 32)
		if parseErr != nil {
			continue
		}
		groups = append(groups, uint32(g))
	}

	return uid, gid, groups, nil
}

// LookupUserGroups 通过用户名查找 uid/gid 及所有补充组
// REQ-F-023, 2.2.13: 通过 user.Lookup(name) 获取 uid/gid
// 必须设置补充组：遍历 user.GroupIds 获取所有 gid
func LookupUserGroups(username string) (uid uint32, gid uint32, groups []uint32, err error) {
	u, lookupErr := user.Lookup(username)
	if lookupErr != nil {
		return 0, 0, nil, fmt.Errorf("user lookup %s: %w", username, lookupErr)
	}

	parsedUID, parseErr := strconv.ParseUint(u.Uid, 10, 32)
	if parseErr != nil {
		return 0, 0, nil, fmt.Errorf("parse uid %s: %w", u.Uid, parseErr)
	}

	parsedGID, parseErr := strconv.ParseUint(u.Gid, 10, 32)
	if parseErr != nil {
		return 0, 0, nil, fmt.Errorf("parse gid %s: %w", u.Gid, parseErr)
	}

	uid = uint32(parsedUID)
	gid = uint32(parsedGID)

	// 获取补充组
	groupIDs, groupErr := u.GroupIds()
	if groupErr != nil {
		// 补充组获取失败时仅返回 uid/gid
		return uid, gid, nil, nil
	}

	groups = make([]uint32, 0, len(groupIDs))
	for _, gidStr := range groupIDs {
		g, parseErr := strconv.ParseUint(gidStr, 10, 32)
		if parseErr != nil {
			continue
		}
		groups = append(groups, uint32(g))
	}

	return uid, gid, groups, nil
}
