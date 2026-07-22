// Package identity 提供用户身份解析与 credential 构造的共享原语。
//
// 本包是叶子包（无 supd 内部依赖），供 core 和 extension 包共同复用，
// 避免 core → extension 的循环依赖。
// REQ-F-023, 规格说明书 §2.2.13: 执行身份切换的基础工具。
package identity

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
	"syscall"
)

// LookupUserGroups 通过用户名查找 uid/gid 及所有补充组。
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

// BuildCredential 构造 syscall.Credential，设置 Uid/Gid/Groups。
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

// GetCurrentUser 获取当前用户的 uid/gid/补充组。
// REQ-F-023: 全局扩展默认 run_as = supd 启动用户；服务 user 为空时继承 supd 用户
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
