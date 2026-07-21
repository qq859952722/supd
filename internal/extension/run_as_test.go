package extension

import (
	"os"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// TestResolveRunAsGlobalEmpty 测试全局扩展 run_as 为空时使用当前用户
// REQ-F-023: 全局扩展默认 run_as = supd 启动用户
func TestResolveRunAsGlobalEmpty(t *testing.T) {
	uid, gid, groups, warn, err := ResolveRunAs("", "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	curUID := uint32(os.Getuid())
	curGID := uint32(os.Getgid())

	if uid != curUID {
		t.Errorf("expected uid %d (current user), got %d", curUID, uid)
	}
	if gid != curGID {
		t.Errorf("expected gid %d (current user), got %d", curGID, gid)
	}
	if warn != "" {
		t.Errorf("expected no warning, got %q", warn)
	}
	// groups 可以为空（如果用户没有补充组），但不应报错
	_ = groups
}

// TestResolveRunAsServiceLevelEmptyWithUser 测试服务级扩展 run_as 为空且有 serviceUser 时使用 serviceUser
// REQ-F-023: 服务级扩展默认 run_as = 服务的 user 字段值
func TestResolveRunAsServiceLevelEmptyWithUser(t *testing.T) {
	curUsername := getCurrentUsername(t)

	uid, gid, groups, warn, err := ResolveRunAs("", curUsername, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	curUID := uint32(os.Getuid())
	curGID := uint32(os.Getgid())

	if uid != curUID {
		t.Errorf("expected uid %d (current user via serviceUser), got %d", curUID, uid)
	}
	if gid != curGID {
		t.Errorf("expected gid %d (current user via serviceUser), got %d", curGID, gid)
	}
	if warn != "" {
		t.Errorf("expected no warning, got %q", warn)
	}
	_ = groups
}

// TestResolveRunAsServiceLevelEmptyNoUser 测试服务级扩展 run_as 为空且 serviceUser 为空时使用当前用户
// REQ-F-023: 服务级扩展 run_as/serviceUser 都为空 → 当前用户
func TestResolveRunAsServiceLevelEmptyNoUser(t *testing.T) {
	uid, gid, groups, warn, err := ResolveRunAs("", "", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	curUID := uint32(os.Getuid())
	curGID := uint32(os.Getgid())

	if uid != curUID {
		t.Errorf("expected uid %d, got %d", curUID, uid)
	}
	if gid != curGID {
		t.Errorf("expected gid %d, got %d", curGID, gid)
	}
	if warn != "" {
		t.Errorf("expected no warning, got %q", warn)
	}
	_ = groups
}

// TestResolveRunAsExplicitCurrentUser 测试显式指定当前用户名
// REQ-F-023: 显式 <用户名> → 用指定用户
func TestResolveRunAsExplicitCurrentUser(t *testing.T) {
	curUsername := getCurrentUsername(t)

	uid, gid, groups, warn, err := ResolveRunAs(curUsername, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	curUID := uint32(os.Getuid())
	curGID := uint32(os.Getgid())

	if uid != curUID {
		t.Errorf("expected uid %d, got %d", curUID, uid)
	}
	if gid != curGID {
		t.Errorf("expected gid %d, got %d", curGID, gid)
	}
	if warn != "" {
		t.Errorf("expected no warning, got %q", warn)
	}
	_ = groups
}

// TestResolveRunAsExplicitRoot 测试显式指定 root
// REQ-F-023: 显式 root → 用 root
func TestResolveRunAsExplicitRoot(t *testing.T) {
	uid, gid, groups, warn, err := ResolveRunAs("root", "", false)

	curUID := uint32(os.Getuid())

	if curUID == 0 {
		// root 环境下应能正常切换到 root
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if uid != 0 {
			t.Errorf("expected uid 0 (root), got %d", uid)
		}
		if gid != 0 {
			t.Errorf("expected gid 0 (root), got %d", gid)
		}
		if warn != "" {
			t.Errorf("expected no warning, got %q", warn)
		}
		_ = groups
	} else {
		// 非 root 环境：应返回警告并以当前用户身份运行
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if uid != curUID {
			t.Errorf("expected uid %d (current user, not root), got %d", curUID, uid)
		}
		if warn == "" {
			t.Error("expected warning for non-root switching to root")
		}
		if !strings.Contains(warn, "cannot switch to user root") {
			t.Errorf("expected warning about root switch, got %q", warn)
		}
	}
	_ = groups
}

// TestResolveRunAsNonexistentUser 测试指定不存在的用户
// REQ-F-023: user.Lookup 失败时返回错误
func TestResolveRunAsNonexistentUser(t *testing.T) {
	_, _, _, _, err := ResolveRunAs("nonexistent_user_xyz_12345", "", false)
	if err == nil {
		t.Error("expected error for nonexistent user")
	}
	if !strings.Contains(err.Error(), "lookup user nonexistent_user_xyz_12345") {
		t.Errorf("expected lookup error, got %v", err)
	}
}

// TestResolveRunAsNonRootSwitchWarning 测试非 root 时切换其他用户的警告
// REQ-P-005: 非 root 时切换其他用户仅警告不报错
func TestResolveRunAsNonRootSwitchWarning(t *testing.T) {
	curUID := uint32(os.Getuid())
	if curUID == 0 {
		t.Skip("skipping: test requires non-root user")
	}

	// root 用户在大多数系统上存在
	uid, gid, _, warn, err := ResolveRunAs("root", "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 非 root 时应返回当前用户身份
	if uid != curUID {
		t.Errorf("expected uid %d (current user), got %d", curUID, uid)
	}

	curGID := uint32(os.Getgid())
	if gid != curGID {
		t.Errorf("expected gid %d (current user), got %d", curGID, gid)
	}

	if warn == "" {
		t.Error("expected warning when non-root tries to switch to another user")
	}
}

// TestBuildCredential 测试 BuildCredential 构造
// REQ-F-023: 构造 syscall.Credential，设置 Uid/Gid/Groups
func TestBuildCredential(t *testing.T) {
	cred := BuildCredential(1000, 1000, []uint32{1000, 100, 27})
	if cred.Uid != 1000 {
		t.Errorf("expected uid 1000, got %d", cred.Uid)
	}
	if cred.Gid != 1000 {
		t.Errorf("expected gid 1000, got %d", cred.Gid)
	}
	if len(cred.Groups) != 3 {
		t.Errorf("expected 3 groups, got %d", len(cred.Groups))
	}
}

// TestBuildCredentialIncludesGID 测试 BuildCredential 确保 groups 包含 gid
// REQ-F-023: Groups 必须包含 gid 本身
func TestBuildCredentialIncludesGID(t *testing.T) {
	// groups 中不包含 gid=2000，应自动补充
	cred := BuildCredential(1000, 2000, []uint32{100, 27})
	if cred.Uid != 1000 {
		t.Errorf("expected uid 1000, got %d", cred.Uid)
	}
	if cred.Gid != 2000 {
		t.Errorf("expected gid 2000, got %d", cred.Gid)
	}

	// 应自动包含 gid=2000
	foundGID := false
	for _, g := range cred.Groups {
		if g == 2000 {
			foundGID = true
			break
		}
	}
	if !foundGID {
		t.Errorf("expected groups to contain gid 2000, groups=%v", cred.Groups)
	}
}

// TestBuildCredentialEmptyGroups 测试 BuildCredential 空 groups 时自动补充 gid
// REQ-F-023: Groups 必须包含 gid 本身
func TestBuildCredentialEmptyGroups(t *testing.T) {
	cred := BuildCredential(1000, 1000, nil)
	if len(cred.Groups) != 1 {
		t.Errorf("expected 1 group (gid itself), got %d", len(cred.Groups))
	}
	if len(cred.Groups) > 0 && cred.Groups[0] != 1000 {
		t.Errorf("expected groups[0]=1000, got %d", cred.Groups[0])
	}
}

// TestBuildCredentialRoot 测试 BuildCredential 构造 root 凭据
// REQ-F-023: 显式 root → uid=0, gid=0
func TestBuildCredentialRoot(t *testing.T) {
	cred := BuildCredential(0, 0, []uint32{0})
	if cred.Uid != 0 {
		t.Errorf("expected uid 0, got %d", cred.Uid)
	}
	if cred.Gid != 0 {
		t.Errorf("expected gid 0, got %d", cred.Gid)
	}
}

// TestGetCurrentUser 测试 GetCurrentUser 获取当前用户信息
// REQ-F-023: 获取当前用户的 uid/gid/补充组
func TestGetCurrentUser(t *testing.T) {
	uid, gid, groups, err := GetCurrentUser()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	curUID := uint32(os.Getuid())
	curGID := uint32(os.Getgid())

	if uid != curUID {
		t.Errorf("expected uid %d, got %d", curUID, uid)
	}
	if gid != curGID {
		t.Errorf("expected gid %d, got %d", curGID, gid)
	}
	// groups 至少应包含 gid 本身（通过 GroupIds 获取）
	// 注意：如果 GroupIds 调用失败，groups 可能为空，这是可接受的
	_ = groups
}

// TestLookupUserGroupsCurrent 测试 LookupUserGroups 查找当前用户
// REQ-F-023: user.Lookup + user.GroupIds 获取所有组
func TestLookupUserGroupsCurrent(t *testing.T) {
	curUsername := getCurrentUsername(t)

	uid, gid, groups, err := LookupUserGroups(curUsername)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	curUID := uint32(os.Getuid())
	curGID := uint32(os.Getgid())

	if uid != curUID {
		t.Errorf("expected uid %d, got %d", curUID, uid)
	}
	if gid != curGID {
		t.Errorf("expected gid %d, got %d", curGID, gid)
	}
	_ = groups
}

// TestLookupUserGroupsRoot 测试 LookupUserGroups 查找 root
// REQ-F-023: user.Lookup(root) 应返回 uid=0
func TestLookupUserGroupsRoot(t *testing.T) {
	uid, gid, _, err := LookupUserGroups("root")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uid != 0 {
		t.Errorf("expected uid 0 for root, got %d", uid)
	}
	if gid != 0 {
		t.Errorf("expected gid 0 for root, got %d", gid)
	}
}

// TestLookupUserGroupsNonexistent 测试 LookupUserGroups 查找不存在的用户
// REQ-F-023: user.Lookup 失败时返回错误
func TestLookupUserGroupsNonexistent(t *testing.T) {
	_, _, _, err := LookupUserGroups("nonexistent_user_xyz_12345")
	if err == nil {
		t.Error("expected error for nonexistent user")
	}
}

// TestResolveRunAsServiceLevelWithServiceUser 测试服务级扩展使用服务 user 字段
// REQ-F-023: 服务级扩展默认 run_as = 服务的 user 字段值
func TestResolveRunAsServiceLevelWithServiceUser(t *testing.T) {
	curUsername := getCurrentUsername(t)

	// run_as 为空，serviceUser 为当前用户
	uid, gid, _, warn, err := ResolveRunAs("", curUsername, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	curUID := uint32(os.Getuid())
	curGID := uint32(os.Getgid())

	if uid != curUID {
		t.Errorf("expected uid %d, got %d", curUID, uid)
	}
	if gid != curGID {
		t.Errorf("expected gid %d, got %d", curGID, gid)
	}
	if warn != "" {
		t.Errorf("expected no warning, got %q", warn)
	}
}

// TestResolveRunAsOverridesServiceUser 测试 run_as 显式指定时覆盖 serviceUser
// REQ-F-023: 显式 run_as 优先于 serviceUser
func TestResolveRunAsOverridesServiceUser(t *testing.T) {
	curUsername := getCurrentUsername(t)

	// run_as 显式指定当前用户，serviceUser 为另一个值（这里用 root 测试优先级）
	uid, _, _, warn, err := ResolveRunAs(curUsername, "root", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	curUID := uint32(os.Getuid())
	// 应使用 runAs 指定的当前用户，而非 serviceUser 的 root
	if uid != curUID {
		t.Errorf("expected uid %d (runAs takes priority), got %d", curUID, uid)
	}
	if warn != "" {
		t.Errorf("expected no warning, got %q", warn)
	}
}

// TestBuildCredentialSysProcAttr 测试 Credential 可正确设置到 SysProcAttr
// REQ-F-023: 用 cmd.SysProcAttr.Credential 设置执行身份
func TestBuildCredentialSysProcAttr(t *testing.T) {
	cred := BuildCredential(1000, 1000, []uint32{1000, 27})
	attr := &syscall.SysProcAttr{
		Credential: cred,
		Setpgid:    true,
	}
	if attr.Credential == nil {
		t.Error("expected credential to be set")
	}
	if attr.Credential.Uid != 1000 {
		t.Errorf("expected uid 1000, got %d", attr.Credential.Uid)
	}
	if !attr.Setpgid {
		t.Error("expected Setpgid true")
	}
}

// getCurrentUsername 获取当前用户名的辅助函数
func getCurrentUsername(t *testing.T) string {
	t.Helper()
	uid := os.Getuid()
	u, err := user.LookupId(strconv.Itoa(uid))
	if err != nil {
		t.Fatalf("failed to get current username: %v", err)
	}
	return u.Username
}
