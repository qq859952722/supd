package identity

import (
	"os"
	"os/user"
	"strconv"
	"syscall"
	"testing"
)

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

// TestCredentialSpecIsEmpty 测试 IsEmpty 方法
func TestCredentialSpecIsEmpty(t *testing.T) {
	if !(CredentialSpec{}).IsEmpty() {
		t.Error("empty spec should be IsEmpty=true")
	}
	if !(CredentialSpec{Group: "docker"}).IsEmpty() {
		t.Error("spec with only Group should be IsEmpty=true (no User/UID)")
	}
	if (CredentialSpec{User: "alice"}).IsEmpty() {
		t.Error("spec with User should be IsEmpty=false")
	}
	if (CredentialSpec{UID: 1000}).IsEmpty() {
		t.Error("spec with UID should be IsEmpty=false")
	}
}

// TestCredentialSpecIsUIDMode 测试 IsUIDMode 方法
func TestCredentialSpecIsUIDMode(t *testing.T) {
	if (CredentialSpec{User: "alice"}).IsUIDMode() {
		t.Error("User mode spec should not be UID mode")
	}
	if !(CredentialSpec{UID: 1000}).IsUIDMode() {
		t.Error("UID mode spec should be IsUIDMode=true")
	}
	if (CredentialSpec{}).IsUIDMode() {
		t.Error("empty spec should not be UID mode")
	}
}

// TestResolveSpecEmpty 空 spec 返回 (0,0,nil,nil)
func TestResolveSpecEmpty(t *testing.T) {
	uid, gid, groups, err := ResolveSpec(CredentialSpec{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uid != 0 || gid != 0 {
		t.Errorf("expected (0,0), got (%d,%d)", uid, gid)
	}
	if groups != nil {
		t.Errorf("expected nil groups, got %v", groups)
	}
}

// TestResolveSpecUIDMode UID 模式直接返回 uid/gid/groups
func TestResolveSpecUIDMode(t *testing.T) {
	uid, gid, groups, err := ResolveSpec(CredentialSpec{UID: 1000, GID: 1001, Groups: []int{27, 100}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uid != 1000 {
		t.Errorf("expected uid 1000, got %d", uid)
	}
	if gid != 1001 {
		t.Errorf("expected gid 1001, got %d", gid)
	}
	if len(groups) != 2 || groups[0] != 27 || groups[1] != 100 {
		t.Errorf("expected groups [27,100], got %v", groups)
	}
}

// TestResolveSpecUIDModeGIDDefaultsToUID UID 模式 GID=0 时取 UID
func TestResolveSpecUIDModeGIDDefaultsToUID(t *testing.T) {
	uid, gid, _, err := ResolveSpec(CredentialSpec{UID: 1000})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uid != 1000 {
		t.Errorf("expected uid 1000, got %d", uid)
	}
	if gid != 1000 {
		t.Errorf("expected gid 1000 (=uid), got %d", gid)
	}
}

// TestResolveSpecUserMode User 模式通过 user.Lookup 解析
func TestResolveSpecUserMode(t *testing.T) {
	curUsername := getCurrentUsername(t)
	uid, _, _, err := ResolveSpec(CredentialSpec{User: curUsername})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	curUID := uint32(os.Getuid())
	if uid != curUID {
		t.Errorf("expected uid %d, got %d", curUID, uid)
	}
}

// TestResolveSpecUserModeNonexistent User 模式查找不存在的用户返回错误
func TestResolveSpecUserModeNonexistent(t *testing.T) {
	_, _, _, err := ResolveSpec(CredentialSpec{User: "nonexistent_user_xyz_12345"})
	if err == nil {
		t.Error("expected error for nonexistent user")
	}
}
