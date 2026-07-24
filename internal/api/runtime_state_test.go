package api

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// killProcessGroup 尽力清理整个进程组，避免孤儿进程残留（仅 Linux 有效）
func killProcessGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

// TestRuntime_ProcessTreeDFS 验证 collectProcessTree 能递归收集所有子孙进程（而非仅直接子进程）
//
// 构造 2 层进程树：
//
//	A(bash) -> { B(bash), C(sleep 100) }
//	B(bash) -> { D(sleep 100), E(sleep 100) }
//
// 若仅收集直接子进程只能得到 A,B,C（3 个）；DFS 递归应得到 A,B,C,D,E（5 个）。
func TestRuntime_ProcessTreeDFS(t *testing.T) {
	cmd := exec.Command("bash", "-c",
		"bash -c 'sleep 100 & sleep 100 & wait' & sleep 100 & wait")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动进程树失败: %v", err)
	}
	rootPID := cmd.Process.Pid
	defer killProcessGroup(rootPID)

	// 轮询等待子进程全部就绪（最多 2s），避免时序导致漏采
	var procs []ProcessInfo
	collected := false
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		p, err := collectProcessTree(rootPID)
		if err == nil {
			procs = p
			if len(p) >= 5 {
				collected = true
				break
			}
		}
	}
	if !collected {
		var pids []string
		for _, pr := range procs {
			pids = append(pids, strconv.Itoa(pr.PID))
		}
		t.Fatalf("进程树收集不完整：期望 >=5 个进程，实际 %d (%v)", len(procs), strings.Join(pids, ","))
	}
	t.Logf("进程树 DFS 递归收集成功：root=%d 共 %d 个进程", rootPID, len(procs))
}

// TestRuntime_CPUFirstSampleCompensation 验证 CPU 首次采样补偿：
// 对刚建立基线的进程，首次 collectProcessResources 应返回真实 CPU 值（>0），
// 而不是 gopsutil 首次采样恒为 ~0 的伪值。
func TestRuntime_CPUFirstSampleCompensation(t *testing.T) {
	// 启动 CPU 密集进程（无限空循环，单线程 100% 占用一个核心）
	cmd := exec.Command("bash", "-c", "while true; do :; done")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动 CPU 密集进程失败: %v", err)
	}
	defer killProcessGroup(cmd.Process.Pid)

	time.Sleep(300 * time.Millisecond)

	res, err := collectProcessResources(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("collectProcessResources 失败: %v", err)
	}
	t.Logf("首次采样（含 200ms 补偿）：CPU=%.2f%% Mem=%.1fMB 进程数=%d FD=%d",
		res.CPUPercent, res.MemoryMB, res.ProcessCount, res.FDCount)
	if res.CPUPercent <= 0 {
		t.Errorf("CPU 首次采样补偿未生效：首次采样 CPU=%.2f%%（应为 >0）", res.CPUPercent)
	}
}
