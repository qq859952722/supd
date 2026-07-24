package api

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

// procCache 缓存 gopsutil Process 对象，使 CPUPercent 能建立基线
// 第一次调用 CPUPercent 返回 0（建立基线），第二次起返回真实值
var procCache sync.Map // map[int32]*process.Process

// G-06-001 修复：cleanupProcCache 限流，避免每次 API 调用都全量扫描
// 30 秒清理一次失效条目，高频 API 下显著减少 /proc 读 syscall
// N-G-02 修复：使用 atomic 访问 lastCleanupTime，避免 race detector 警告
var (
	lastCleanupTime atomic.Int64 // unix nano
	cleanupInterval = int64(30 * time.Second)
)

// getProcess 从缓存获取或创建 Process 对象
func getProcess(pid int32) (*process.Process, error) {
	if cached, ok := procCache.Load(pid); ok {
		p := cached.(*process.Process)
		// 验证进程是否仍存在
		if _, err := p.Status(); err != nil {
			procCache.Delete(pid)
		} else {
			return p, nil
		}
	}
	p, err := process.NewProcess(pid)
	if err != nil {
		return nil, err
	}
	// 预热：建立 CPU 基线
	_, _ = p.CPUPercent()
	procCache.Store(pid, p)
	return p, nil
}

// cleanupProcCache 清理已失效的进程缓存条目
// G-06-001 修复：限流清理，30 秒一次，避免高频 API 下 syscall 风暴
// 防止 PID 复用导致的数据混淆
// G-04-002 修复：使用真正的 CompareAndSwap，避免多 goroutine 同时进入重复扫描
func cleanupProcCache() {
	now := time.Now().UnixNano()
	last := lastCleanupTime.Load()
	if now-last < cleanupInterval {
		// 30 秒内已清理过，跳过
		return
	}
	// 真正的 CAS：仅当 lastCleanupTime 仍为 last 时才更新为 now
	// 若其他 goroutine 已抢先更新，则当前 goroutine 跳过扫描
	if !lastCleanupTime.CompareAndSwap(last, now) {
		return
	}

	procCache.Range(func(key, value any) bool {
		p := value.(*process.Process)
		if _, err := p.Status(); err != nil {
			procCache.Delete(key)
		}
		return true
	})
}

// collectProcessResources 按需采集进程资源使用情况
// REQ-I-006: 使用gopsutil采集数据
// G-06-003 修复：添加 5 秒 context 超时保护，避免 /proc 读取阻塞
// G-06-001 修复：调用前清理失效缓存条目，防止 PID 复用数据混淆
func collectProcessResources(pid int) (*ResourceResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// G-06-001: 清理已失效的进程缓存，防止 PID 复用导致数据混淆
	cleanupProcCache()

	p, err := getProcess(int32(pid))
	if err != nil {
		return nil, fmt.Errorf("process %d not found: %w", pid, err)
	}

	cpuPercent, _ := p.CPUPercent()
	memInfo, _ := p.MemoryInfo()
	memPercent, _ := p.MemoryPercent()

	// G-06-003: 检查超时
	if ctx.Err() != nil {
		return nil, fmt.Errorf("collect process resources timeout: %w", ctx.Err())
	}

	// 统计子进程数
	children, _ := p.Children()
	processCount := 1 + len(children)

	// 统计FD数
	fdCount := 0
	if fds, err := p.NumFDs(); err == nil {
		fdCount = int(fds)
	}

	memMB := 0.0
	if memInfo != nil {
		memMB = float64(memInfo.RSS) / 1024 / 1024
	}

	return &ResourceResponse{
		CPUPercent:    cpuPercent,
		MemoryMB:      memMB,
		MemoryPercent: float64(memPercent),
		ProcessCount:  processCount,
		FDCount:       fdCount,
	}, nil
}

// collectProcessTree 获取进程及其子进程信息
// REQ-I-006: 使用gopsutil采集进程树
// G-06-003 修复：添加 5 秒 context 超时保护，避免 /proc 读取阻塞
func collectProcessTree(pid int) ([]ProcessInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	p, err := getProcess(int32(pid))
	if err != nil {
		return nil, fmt.Errorf("process %d not found: %w", pid, err)
	}

	var processes []ProcessInfo

	// 主进程信息
	mainInfo, err := processToInfo(p)
	if err == nil {
		processes = append(processes, *mainInfo)
	}

	// G-06-003: 检查超时
	if ctx.Err() != nil {
		return processes, fmt.Errorf("collect process tree timeout: %w", ctx.Err())
	}

	// 子进程信息
	children, err := p.Children()
	if err == nil {
		for _, child := range children {
			info, err := processToInfo(child)
			if err == nil {
				processes = append(processes, *info)
			}
		}
	}

	// G-06-003: 检查超时，超时则跳过 /proc 降级读取
	if ctx.Err() != nil {
		if processes == nil {
			processes = []ProcessInfo{}
		}
		return processes, nil
	}

	// 同时从 /proc 读取子进程（补充 gopsutil 未获取的）
	procChildren := findChildProcesses(pid)
	for _, childPID := range procChildren {
		// 跳过已通过 gopsutil 获取的
		found := false
		for _, p := range processes {
			if p.PID == childPID {
				found = true
				break
			}
		}
		if !found {
			info, err := readProcessInfoFromProc(childPID)
			if err == nil {
				processes = append(processes, *info)
			}
		}
	}

	if processes == nil {
		processes = []ProcessInfo{}
	}

	return processes, nil
}

// processToInfo 将 gopsutil Process 转换为 ProcessInfo
func processToInfo(p *process.Process) (*ProcessInfo, error) {
	pid := int(p.Pid)
	ppid := 0
	if pp, err := p.Ppid(); err == nil {
		ppid = int(pp)
	}

	username, _ := p.Username()
	cpuPercent, _ := p.CPUPercent()

	// 获取 UID/GID（Uids/Gids 返回 [real, effective, saved, fs]，取 real 即第一个）
	uid := 0
	if uids, err := p.Uids(); err == nil && len(uids) > 0 {
		uid = int(uids[0])
	}
	gid := 0
	if gids, err := p.Gids(); err == nil && len(gids) > 0 {
		gid = int(gids[0])
	}

	// 获取组名：通过 UID 反查 /etc/passwd 获取 GID，再查 /etc/group
	groupName := lookupGroupByGID(gid)

	memMB := 0.0
	if memInfo, err := p.MemoryInfo(); err == nil && memInfo != nil {
		memMB = float64(memInfo.RSS) / 1024 / 1024
	}

	status := ""
	if s, err := p.Status(); err == nil && len(s) > 0 {
		status = s[0]
	}

	threadCount := 0
	if n, err := p.NumThreads(); err == nil {
		threadCount = int(n)
	}

	cmdline, _ := p.Cmdline()

	// CreateTime 返回进程创建时间（毫秒，自 epoch 起）
	var startedAt int64
	if ct, err := p.CreateTime(); err == nil {
		startedAt = ct
	}

	return &ProcessInfo{
		PID:         pid,
		PPID:        ppid,
		User:        username,
		UID:         uid,
		Group:       groupName,
		GID:         gid,
		CPUPercent:  cpuPercent,
		MemoryMB:    memMB,
		Status:      status,
		ThreadCount: threadCount,
		Command:     cmdline,
		StartedAt:   startedAt,
	}, nil
}

// findChildProcesses 从 /proc 查找子进程
func findChildProcesses(ppid int) []int {
	var children []int
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return children
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		statPath := filepath.Join("/proc", entry.Name(), "stat")
		data, err := readFileWithTimeout(statPath)
		if err != nil {
			continue
		}

		// /proc/pid/stat 格式: pid (comm) state ppid ...
		// 需要找到最后一个 ')' 后的字段
		content := string(data)
		idx := strings.LastIndex(content, ")")
		if idx < 0 {
			continue
		}
		fields := strings.Fields(content[idx+1:])
		if len(fields) < 2 {
			continue
		}
		parentPID, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		if parentPID == ppid {
			children = append(children, pid)
		}
	}

	return children
}

// readProcessInfoFromProc 从 /proc 读取进程信息（降级方案）
func readProcessInfoFromProc(pid int) (*ProcessInfo, error) {
	statPath := filepath.Join("/proc", strconv.Itoa(pid), "stat")
	data, err := readFileWithTimeout(statPath)
	if err != nil {
		return nil, err
	}

	content := string(data)
	idx := strings.LastIndex(content, ")")
	if idx < 0 {
		return nil, fmt.Errorf("invalid stat format")
	}

	fields := strings.Fields(content[idx+1:])
	if len(fields) < 2 {
		return nil, fmt.Errorf("invalid stat format")
	}

	ppid, _ := strconv.Atoi(fields[1])

	// 获取命令行
	cmdlinePath := filepath.Join("/proc", strconv.Itoa(pid), "cmdline")
	cmdData, err := readFileWithTimeout(cmdlinePath)
	if err != nil {
		cmdData = []byte{}
	}
	cmdline := strings.ReplaceAll(string(cmdData), "\x00", " ")

	// 获取状态
	status := ""
	if len(fields) > 0 {
		status = strings.Trim(fields[0], " ")
	}

	// 启动时间：fields[19] 为 starttime（自开机以来的时钟滴答数，USER_HZ=100）
	// started_at_ms = (btime_seconds + starttime/100) * 1000
	var startedAt int64
	if len(fields) > 19 {
		if startTicks, err := strconv.ParseInt(fields[19], 10, 64); err == nil {
			if bootTime := readBootTime(); bootTime > 0 {
				startedAt = (bootTime + startTicks/100) * 1000
			}
		}
	}

	// 从 /proc/<pid>/status 获取 UID/GID 和用户名/组名
	uid, gid, username, groupName := readProcessIdentityFromStatus(pid)

	return &ProcessInfo{
		PID:       pid,
		PPID:      ppid,
		User:      username,
		UID:       uid,
		Group:     groupName,
		GID:       gid,
		Status:    status,
		Command:   strings.TrimSpace(cmdline),
		StartedAt: startedAt,
	}, nil
}

// readBootTime 从 /proc/stat 读取系统启动时间（秒，自 epoch 起）
func readBootTime() int64 {
	data, err := readFileWithTimeout("/proc/stat")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "btime ") {
			if v, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "btime ")), 10, 64); err == nil {
				return v
			}
		}
	}
	return 0
}

// uidNameCache 缓存 UID→用户名 和 GID→组名 的映射
// 容器环境中 /etc/passwd 和 /etc/group 不会运行时变化，读一次后终生有效
var (
	uidNameCache sync.Map // map[int]string
	gidNameCache sync.Map // map[int]string
)

// lookupGroupByGID 通过 GID 查找组名
// 读取 /etc/group 文件，格式: group_name:x:gid:members
// 结果缓存于 gidNameCache，文件不会运行时变化
func lookupGroupByGID(gid int) string {
	if gid == 0 {
		return "root"
	}
	if cached, ok := gidNameCache.Load(gid); ok {
		return cached.(string)
	}

	name := readEtcGroupByGID(gid)
	if name != "" {
		gidNameCache.Store(gid, name)
	}
	return name
}

// readEtcGroupByGID 从 /etc/group 读取 GID→组名 映射
func readEtcGroupByGID(gid int) string {
	data, err := readFileWithTimeout("/etc/group")
	if err != nil {
		return ""
	}
	gidStr := strconv.Itoa(gid)
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, ":", 4)
		if len(parts) >= 3 && parts[2] == gidStr {
			return parts[0]
		}
	}
	return ""
}

// lookupUserByUID 通过 UID 查找用户名
// 读取 /etc/passwd 文件，格式: username:x:uid:gid:gecos:home:shell
// 结果缓存于 uidNameCache，文件不会运行时变化
func lookupUserByUID(uid int) string {
	if uid == 0 {
		return "root"
	}
	if cached, ok := uidNameCache.Load(uid); ok {
		return cached.(string)
	}

	name := readEtcPasswdByUID(uid)
	if name != "" {
		uidNameCache.Store(uid, name)
	}
	return name
}

// readEtcPasswdByUID 从 /etc/passwd 读取 UID→用户名 映射
func readEtcPasswdByUID(uid int) string {
	data, err := readFileWithTimeout("/etc/passwd")
	if err != nil {
		return ""
	}
	uidStr := strconv.Itoa(uid)
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, ":", 7)
		if len(parts) >= 3 && parts[2] == uidStr {
			return parts[0]
		}
	}
	return ""
}

// readUIDGIDFromStatus 从 /proc/<pid>/status 读取进程的 real UID 和 real GID
// /proc/<pid>/status 不受 Yama ptrace_scope 限制
// 返回 uid, gid，读取失败时返回 -1, -1
func readUIDGIDFromStatus(pid int) (int, int) {
	statusPath := filepath.Join("/proc", strconv.Itoa(pid), "status")
	data, err := readFileWithTimeout(statusPath)
	if err != nil {
		return -1, -1
	}

	uid, gid := -1, -1
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "Uid:") {
			fields := strings.Fields(strings.TrimPrefix(line, "Uid:"))
			if len(fields) >= 1 {
				if v, err := strconv.Atoi(fields[0]); err == nil {
					uid = v
				}
			}
		} else if strings.HasPrefix(line, "Gid:") {
			fields := strings.Fields(strings.TrimPrefix(line, "Gid:"))
			if len(fields) >= 1 {
				if v, err := strconv.Atoi(fields[0]); err == nil {
					gid = v
				}
			}
		}
	}

	return uid, gid
}

// readProcessIdentityFromStatus 从 /proc/<pid>/status 读取进程完整身份信息
// 返回 uid, gid, username, groupName
func readProcessIdentityFromStatus(pid int) (int, int, string, string) {
	uid, gid := readUIDGIDFromStatus(pid)
	if uid < 0 {
		uid = 0
	}
	if gid < 0 {
		gid = 0
	}

	username := lookupUserByUID(uid)
	groupName := lookupGroupByGID(gid)

	return uid, gid, username, groupName
}

// collectProcessTreeByCommand 通过命令行匹配 /proc 查找进程（PID命名空间降级方案）
// 当 gopsutil 因 PID 命名空间不一致无法找到进程时使用
func collectProcessTreeByCommand(namespacePID int, cmdPattern string) []ProcessInfo {
	var processes []ProcessInfo

	// 提取命令的基础名（如 ./bin/qbittorrent-nox → qbittorrent-nox）
	cmdBase := filepath.Base(cmdPattern)
	if cmdBase == "." || cmdBase == "/" || cmdBase == "" {
		cmdBase = cmdPattern
	}

	// 扫描 /proc 查找匹配的进程
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return []ProcessInfo{}
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		hostPID, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		// 读取 cmdline
		cmdlinePath := filepath.Join("/proc", entry.Name(), "cmdline")
		cmdData, err := readFileWithTimeout(cmdlinePath)
		if err != nil {
			continue
		}
		cmdline := strings.ReplaceAll(string(cmdData), "\x00", " ")
		if cmdline == "" {
			continue
		}

		// 匹配命令
		if cmdPattern != "" && !strings.Contains(cmdline, cmdBase) {
			continue
		}

		// 读取 /proc/{pid}/stat
		info, err := readProcessInfoFromProc(hostPID)
		if err != nil {
			continue
		}
		// 用 namespace PID 替换 host PID（主进程）
		if processes == nil {
			info.PID = namespacePID
		}
		// 覆盖命令行为完整命令行
		info.Command = strings.TrimSpace(cmdline)

		// 尝试读取内存信息
		statmPath := filepath.Join("/proc", entry.Name(), "statm")
		if statmData, err := readFileWithTimeout(statmPath); err == nil {
			fields := strings.Fields(string(statmData))
			if len(fields) >= 2 {
				rssPages, _ := strconv.ParseInt(fields[1], 10, 64)
				info.MemoryMB = float64(rssPages) * 4096 / 1024 / 1024
			}
		}

		// 尝试读取线程数
		taskDir := filepath.Join("/proc", entry.Name(), "task")
		if taskEntries, err := os.ReadDir(taskDir); err == nil {
			info.ThreadCount = len(taskEntries)
		}

		processes = append(processes, *info)

		// 查找子进程
		children := findChildProcesses(hostPID)
		for _, childPID := range children {
			childInfo, err := readProcessInfoFromProc(childPID)
			if err != nil {
				continue
			}
			// 读取子进程 cmdline
			childCmdPath := filepath.Join("/proc", strconv.Itoa(childPID), "cmdline")
			if childCmdData, err := readFileWithTimeout(childCmdPath); err == nil {
				childInfo.Command = strings.TrimSpace(strings.ReplaceAll(string(childCmdData), "\x00", " "))
			}
			// 读取子进程内存
			childStatmPath := filepath.Join("/proc", strconv.Itoa(childPID), "statm")
			if statmData, err := readFileWithTimeout(childStatmPath); err == nil {
				fields := strings.Fields(string(statmData))
				if len(fields) >= 2 {
					rssPages, _ := strconv.ParseInt(fields[1], 10, 64)
					childInfo.MemoryMB = float64(rssPages) * 4096 / 1024 / 1024
				}
			}
			processes = append(processes, *childInfo)
		}

		break // 只取第一个匹配的主进程
	}

	if processes == nil {
		processes = []ProcessInfo{}
	}
	return processes
}
