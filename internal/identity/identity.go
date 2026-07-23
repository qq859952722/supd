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

// LookupGroup 通过组名查找 gid。
// 用于 User 模式下 Group 字段覆盖主组 gid（如 user:alice + group:docker）。
func LookupGroup(name string) (uint32, error) {
	g, err := user.LookupGroup(name)
	if err != nil {
		return 0, fmt.Errorf("group lookup %s: %w", name, err)
	}
	gid, err := strconv.ParseUint(g.Gid, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parse gid %s: %w", g.Gid, err)
	}
	return uint32(gid), nil
}

// CredentialSpec 描述服务/扩展的执行身份配置。
// User 模式（User 非空）与 UID 模式（UID 非 0）互斥，由 config 校验层拦截同时指定的情况。
//   - User 模式：通过 user.Lookup 获取 uid/gid/补充组；Group 可选覆盖主组 gid（保留补充组）
//   - UID 模式：直接用 UID/GID/Groups，不查 /etc/passwd（适用于用户不在 passwd 的场景，如 NAS 固定 uid 服务）
type CredentialSpec struct {
	User   string // 用户名（User 模式）
	Group  string // 组名（User 模式下可选，覆盖主组 gid）
	UID    int    // 直接 uid（UID 模式，0 表示未设置）
	GID    int    // 直接 gid（UID 模式下可选，0 表示 = UID）
	Groups []int  // 补充组 gid 列表（UID 模式下可选）
}

// IsEmpty 返回 spec 是否完全未设置（两种模式均未配置）。
func (s CredentialSpec) IsEmpty() bool {
	return s.User == "" && s.UID == 0
}

// IsUIDMode 返回是否为 UID 模式（UID 非 0）。
func (s CredentialSpec) IsUIDMode() bool {
	return s.UID != 0
}

// ResolveSpec 解析 CredentialSpec 为 uid/gid/补充组。
//   - IsEmpty → 返回 (0,0,nil,nil)，调用方应回退到 GetCurrentUser
//   - User 模式 → LookupUserGroups，Group 非空时 LookupGroup 覆盖 gid
//   - UID 模式 → 直接用 UID/GID/Groups（GID=0 时取 UID）
//
// 不处理非 root 切换限制（由调用方 ResolveServiceCredential/ResolveRunAs 按语义处理）。
func ResolveSpec(spec CredentialSpec) (uid uint32, gid uint32, groups []uint32, err error) {
	if spec.IsEmpty() {
		return 0, 0, nil, nil
	}
	if spec.IsUIDMode() {
		uid = uint32(spec.UID)
		gid = uint32(spec.GID)
		if gid == 0 {
			gid = uid
		}
		groups = make([]uint32, 0, len(spec.Groups))
		for _, g := range spec.Groups {
			groups = append(groups, uint32(g))
		}
		return uid, gid, groups, nil
	}
	// User 模式
	uid, gid, groups, err = LookupUserGroups(spec.User)
	if err != nil {
		return 0, 0, nil, err
	}
	// Group 可选覆盖主组 gid（保留用户的补充组不变）
	if spec.Group != "" {
		newGID, gerr := LookupGroup(spec.Group)
		if gerr != nil {
			return 0, 0, nil, gerr
		}
		gid = newGID
	}
	return uid, gid, groups, nil
}
