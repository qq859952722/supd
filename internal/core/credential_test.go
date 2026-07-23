package core

import (
	"errors"
	"os"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"testing"

	svcerr "github.com/supdorg/supd/internal/errors"
	"github.com/supdorg/supd/internal/identity"
)

// TestResolveServiceCredential_EmptyUser user 为空时返回 nil credential（继承 supd 用户）
// REQ-F-023, 规格说明书 §2.2.13 line 684: user 字段默认继承 supd 启动用户
func TestResolveServiceCredential_EmptyUser(t *testing.T) {
	cred, err := ResolveServiceCredential("test-svc", identity.CredentialSpec{}, "/etc/supd/services/test-svc/service.yaml")
	if err != nil {
		t.Fatalf("expected nil error for empty user, got %v", err)
	}
	if cred != nil {
		t.Errorf("expected nil credential for empty user, got %+v", cred)
	}
}

// TestResolveServiceCredential_NonexistentUser user 不存在时返回 *ServiceError
// REQ-F-023, 规格说明书 §2.2.13 line 700: 用户不存在 → 拒绝启动并提示原因和解决方法
func TestResolveServiceCredential_NonexistentUser(t *testing.T) {
	const fakeUser = "nonexistent_user_xyz_12345"
	cred, err := ResolveServiceCredential("test-svc", identity.CredentialSpec{User: fakeUser}, "/etc/supd/services/test-svc/service.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent user, got nil")
	}
	if cred != nil {
		t.Errorf("expected nil credential on error, got %+v", cred)
	}

	// 验证是 *ServiceError 类型
	var se *svcerr.ServiceError
	if !errors.As(err, &se) {
		t.Fatalf("expected *ServiceError, got %T: %v", err, err)
	}
	if se.Code != svcerr.ErrRuntimeUserNotFound {
		t.Errorf("expected code %s, got %s", svcerr.ErrRuntimeUserNotFound, se.Code)
	}

	// 验证错误消息包含关键信息：用户名、错误原因、解决方法、配置位置
	msg := se.Message
	if !strings.Contains(msg, fakeUser) {
		t.Errorf("error message should contain username %q, got: %s", fakeUser, msg)
	}
	if !strings.Contains(msg, "service.yaml") {
		t.Errorf("error message should contain config path, got: %s", msg)
	}
}

// TestResolveServiceCredential_CurrentUser 显式指定当前用户 → 允许
// REQ-F-023: 非 root 时显式指定为当前用户 → 允许
// 优化：当目标 UID 等于当前 UID（非 root 环境）时，返回 nil credential，
// 避免非 root 进程调用 setuid 触发 EPERM（与 extension.ResolveRunAs 一致）
func TestResolveServiceCredential_CurrentUser(t *testing.T) {
	curUsername := getCurrentUsernameHelper(t)
	cred, err := ResolveServiceCredential("test-svc", identity.CredentialSpec{User: curUsername}, "/etc/supd/services/test-svc/service.yaml")
	if err != nil {
		t.Fatalf("expected nil error for current user, got %v", err)
	}

	curUID := uint32(os.Getuid())

	if curUID == 0 {
		// root 环境：显式指定当前用户(root) → 返回 root 的 Credential
		if cred == nil {
			t.Fatal("expected non-nil credential for root-to-root")
		}
		if cred.Uid != 0 {
			t.Errorf("expected uid 0 (root), got %d", cred.Uid)
		}
		if cred.Gid != 0 {
			t.Errorf("expected gid 0 (root), got %d", cred.Gid)
		}
	} else {
		// 非 root 环境：显式指定当前用户 → 返回 nil（避免不必要的 setuid）
		if cred != nil {
			t.Errorf("expected nil credential (no switch needed for current user), got %+v", cred)
		}
	}
}

// TestResolveServiceCredential_RootUser 显式指定 root 用户
// REQ-F-023: root 启动 supd → 切换到 root；非 root 启动 → 拒绝
func TestResolveServiceCredential_RootUser(t *testing.T) {
	cred, err := ResolveServiceCredential("test-svc", identity.CredentialSpec{User: "root"}, "/etc/supd/services/test-svc/service.yaml")
	curUID := uint32(os.Getuid())

	if curUID == 0 {
		// root 环境：应能正常切换到 root
		if err != nil {
			t.Fatalf("unexpected error for root-to-root: %v", err)
		}
		if cred == nil {
			t.Fatal("expected non-nil credential for root-to-root")
		}
		if cred.Uid != 0 {
			t.Errorf("expected uid 0 (root), got %d", cred.Uid)
		}
		if cred.Gid != 0 {
			t.Errorf("expected gid 0 (root), got %d", cred.Gid)
		}
	} else {
		// 非 root 环境：应拒绝启动，返回 *ServiceError
		if err == nil {
			t.Fatal("expected error for non-root switching to root, got nil")
		}
		var se *svcerr.ServiceError
		if !errors.As(err, &se) {
			t.Fatalf("expected *ServiceError, got %T: %v", err, err)
		}
		if se.Code != svcerr.ErrRuntimeUserNotFound {
			t.Errorf("expected code %s, got %s", svcerr.ErrRuntimeUserNotFound, se.Code)
		}
		if cred != nil {
			t.Errorf("expected nil credential on rejection, got %+v", cred)
		}
		// 错误消息应包含配置位置和解决建议
		if !strings.Contains(se.Message, "root") {
			t.Errorf("error message should mention root, got: %s", se.Message)
		}
		if !strings.Contains(se.Message, "service.yaml") {
			t.Errorf("error message should contain config path, got: %s", se.Message)
		}
	}
}

// TestResolveServiceCredential_NonRootSwitchRejected 非 root 启动 supd 且 user 指定其他真实存在的用户时拒绝启动
// REQ-F-023, 规格说明书 §2.2.13 line 700: 非 root 切换其他用户 → 拒绝启动该服务（区别于扩展的警告宽松语义）
// 该测试仅在非 root 环境且系统存在 root 用户时有效
func TestResolveServiceCredential_NonRootSwitchRejected(t *testing.T) {
	curUID := uint32(os.Getuid())
	if curUID == 0 {
		t.Skip("skipping: test requires non-root user")
	}

	// root 用户在几乎所有 Linux 系统上都存在
	cred, err := ResolveServiceCredential("test-svc", identity.CredentialSpec{User: "root"}, "/etc/supd/services/test-svc/service.yaml")
	if err == nil {
		t.Fatal("expected error for non-root switching to root, got nil")
	}
	if cred != nil {
		t.Errorf("expected nil credential on rejection, got %+v", cred)
	}

	var se *svcerr.ServiceError
	if !errors.As(err, &se) {
		t.Fatalf("expected *ServiceError, got %T: %v", err, err)
	}
	if se.Code != svcerr.ErrRuntimeUserNotFound {
		t.Errorf("expected code %s, got %s", svcerr.ErrRuntimeUserNotFound, se.Code)
	}

	// 错误消息应提示"以 root 启动 supd"或"改为当前用户或留空"
	msg := se.Message
	if !strings.Contains(msg, "root") {
		t.Errorf("error message should mention root, got: %s", msg)
	}
}

// TestResolveServiceCredential_EmptyConfigPath 兜底 configPath 为空时不影响错误消息构造
func TestResolveServiceCredential_EmptyConfigPath(t *testing.T) {
	const fakeUser = "nonexistent_user_xyz_12345"
	_, err := ResolveServiceCredential("test-svc", identity.CredentialSpec{User: fakeUser}, "")
	if err == nil {
		t.Fatal("expected error for nonexistent user, got nil")
	}
	var se *svcerr.ServiceError
	if !errors.As(err, &se) {
		t.Fatalf("expected *ServiceError, got %T: %v", err, err)
	}
	// 应该用 <unknown> 兜底
	if !strings.Contains(se.Message, "<unknown>") {
		t.Errorf("error message should contain <unknown> fallback path, got: %s", se.Message)
	}
}

// TestResolveServiceCredential_UIDMode UID 模式直接指定 uid/gid
// §2.2.13: UID 模式不查 /etc/passwd，直接使用 uid/gid/groups
func TestResolveServiceCredential_UIDMode(t *testing.T) {
	curUID := uint32(os.Getuid())

	// 指定当前 uid → 非 root 时返回 nil（无需切换）；root 时返回 Credential
	cred, err := ResolveServiceCredential("test-svc", identity.CredentialSpec{UID: int(curUID)}, "/etc/supd/services/test-svc/service.yaml")
	if err != nil {
		t.Fatalf("unexpected error for uid=current: %v", err)
	}
	if curUID != 0 {
		if cred != nil {
			t.Errorf("expected nil credential for uid=current (non-root), got %+v", cred)
		}
	} else {
		if cred == nil {
			t.Fatal("expected non-nil credential for root uid=0")
		}
	}
}

// TestResolveServiceCredential_UIDMode_NonRootRejected 非 root 时 UID 模式切换其他 uid 被拒绝
func TestResolveServiceCredential_UIDMode_NonRootRejected(t *testing.T) {
	curUID := uint32(os.Getuid())
	if curUID == 0 {
		t.Skip("skipping: test requires non-root user")
	}

	// 指定一个不等于当前 uid 的值（如 99999）
	cred, err := ResolveServiceCredential("test-svc", identity.CredentialSpec{UID: 99999}, "/etc/supd/services/test-svc/service.yaml")
	if err == nil {
		t.Fatal("expected error for non-root switching to other uid, got nil")
	}
	if cred != nil {
		t.Errorf("expected nil credential on rejection, got %+v", cred)
	}
	var se *svcerr.ServiceError
	if !errors.As(err, &se) {
		t.Fatalf("expected *ServiceError, got %T: %v", err, err)
	}
	if se.Code != svcerr.ErrRuntimeUserNotFound {
		t.Errorf("expected code %s, got %s", svcerr.ErrRuntimeUserNotFound, se.Code)
	}
	if !strings.Contains(se.Message, "uid=99999") {
		t.Errorf("error message should contain uid=99999, got: %s", se.Message)
	}
}

// TestStartServiceProcess_EmptyUser 继承 supd 用户（user 为空）
func TestStartServiceProcess_EmptyUser(t *testing.T) {
	// 启动一个立即退出的命令，验证 StartServiceProcess 能正常工作
	proc, err := StartServiceProcess(
		"test-true",
		[]string{"true"},
		nil,
		"",
		identity.CredentialSpec{}, // 空 spec → 继承当前用户
		"/etc/supd/services/test-svc/service.yaml",
	)
	if err != nil {
		t.Fatalf("StartServiceProcess failed: %v", err)
	}
	defer func() {
		// 真命令已经退出，但 Process 对象需要清理（Wait 已被调用则可能 no-op）
		_ = proc.KillProcessGroup()
	}()

	if proc.PID() <= 0 {
		t.Errorf("expected PID > 0, got %d", proc.PID())
	}
}

// TestStartServiceProcess_NonexistentUser 用户不存在时拒绝启动
// REQ-F-023: 拒绝启动并返回 *ServiceError
func TestStartServiceProcess_NonexistentUser(t *testing.T) {
	const fakeUser = "nonexistent_user_xyz_12345"
	_, err := StartServiceProcess(
		"test-svc",
		[]string{"sleep", "1"},
		nil,
		"",
		identity.CredentialSpec{User: fakeUser},
		"/etc/supd/services/test-svc/service.yaml",
	)
	if err == nil {
		t.Fatal("expected error for nonexistent user, got nil")
	}

	var se *svcerr.ServiceError
	if !errors.As(err, &se) {
		t.Fatalf("expected *ServiceError, got %T: %v", err, err)
	}
	if se.Code != svcerr.ErrRuntimeUserNotFound {
		t.Errorf("expected code %s, got %s", svcerr.ErrRuntimeUserNotFound, se.Code)
	}
}

// TestStartServiceProcess_CurrentUser 显式指定当前用户能正常启动
func TestStartServiceProcess_CurrentUser(t *testing.T) {
	curUsername := getCurrentUsernameHelper(t)
	proc, err := StartServiceProcess(
		"test-true",
		[]string{"true"},
		nil,
		"",
		identity.CredentialSpec{User: curUsername},
		"/etc/supd/services/test-svc/service.yaml",
	)
	if err != nil {
		t.Fatalf("StartServiceProcess failed: %v", err)
	}
	defer func() { _ = proc.KillProcessGroup() }()

	if proc.PID() <= 0 {
		t.Errorf("expected PID > 0, got %d", proc.PID())
	}

	// 验证进程的实际 UID 等于当前用户
	status, err := os.ReadFile("/proc/" + strconv.Itoa(proc.PID()) + "/status")
	if err == nil {
		// 解析 Uid 行：格式 "Uid:\t1000\t1000\t1000\t1000"
		for _, line := range strings.Split(string(status), "\n") {
			if strings.HasPrefix(line, "Uid:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					uid, _ := strconv.Atoi(fields[1])
					if uint32(uid) != uint32(os.Getuid()) {
						t.Errorf("process uid = %d, want %d (current user)", uid, os.Getuid())
					}
				}
				break
			}
		}
	}
}

// TestStartServiceProcess_ExtraFiles 验证 extraFiles 参数能正常传递
// （StartServiceProcess 应透传 extraFiles 给 StartProcess，不影响身份解析）
func TestStartServiceProcess_ExtraFiles(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	defer r.Close()

	proc, err := StartServiceProcess(
		"test-true",
		[]string{"true"},
		nil,
		"",
		identity.CredentialSpec{},
		"/etc/supd/services/test-svc/service.yaml",
		w, // extraFiles
	)
	if err != nil {
		w.Close()
		t.Fatalf("StartServiceProcess failed: %v", err)
	}
	w.Close()
	defer func() { _ = proc.KillProcessGroup() }()

	if proc.PID() <= 0 {
		t.Errorf("expected PID > 0, got %d", proc.PID())
	}
}

// getCurrentUsernameHelper 获取当前用户名的辅助函数
func getCurrentUsernameHelper(t *testing.T) string {
	t.Helper()
	uid := os.Getuid()
	u, err := user.LookupId(strconv.Itoa(uid))
	if err != nil {
		t.Fatalf("failed to get current username: %v", err)
	}
	return u.Username
}

// 编译期断言：确保 syscall.Credential 在测试中被引用（避免未使用 import）
var _ = syscall.Credential{}
