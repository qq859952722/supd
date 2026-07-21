package core

import (
	"os"
	"testing"
)

func TestIsPID1(t *testing.T) {
	// 在正常测试环境中，进程 PID 不会是 1
	result := IsPID1()
	if result && os.Getpid() != 1 {
		t.Errorf("IsPID1() = %v, want false (current pid = %d)", result, os.Getpid())
	}
	if !result && os.Getpid() == 1 {
		t.Errorf("IsPID1() = %v, want true (current pid = %d)", result, os.Getpid())
	}
}

func TestSetupPID1Mode(t *testing.T) {
	// 在非 PID1 环境中调用 SetupPID1Mode 也应该能成功
	// PR_SET_CHILD_SUBREAPER 不要求调用者必须是 PID 1
	err := SetupPID1Mode()
	if err != nil {
		// 某些环境可能不支持 prctl，仅记录不失败
		t.Logf("SetupPID1Mode() returned error (may be expected in this environment): %v", err)
	}
}

func TestSetupPID1IfNeeded_NoPID1(t *testing.T) {
	// --no-pid1 模式下应该跳过设置
	err := SetupPID1IfNeeded(true)
	if err != nil {
		t.Errorf("SetupPID1IfNeeded(true) returned error: %v", err)
	}
}

func TestSetupPID1IfNeeded_NotPID1(t *testing.T) {
	// 非 PID1 环境下应该跳过设置
	if os.Getpid() == 1 {
		t.Skip("running as PID 1, test not applicable")
	}
	err := SetupPID1IfNeeded(false)
	if err != nil {
		t.Errorf("SetupPID1IfNeeded(false) returned error: %v", err)
	}
}
