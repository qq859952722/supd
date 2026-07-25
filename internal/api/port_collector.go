package api

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/supdorg/supd/internal/config"
)

// readFileWithTimeout reads a file with a 2-second timeout.
// /proc is a memory filesystem and normally very fast; the timeout
// protects against extreme cases where a read may block.
//
// G-06-002 评估：超时后 goroutine 泄漏（os.ReadFile 无法取消）。
// 实际影响可接受：/proc 是内存文件系统，读取通常 <1ms，不会永久阻塞，
// goroutine 会在 os.ReadFile 返回后自动退出。
// 真正修复需要 io.Reader + context 取消，对 /proc 读取属于过度设计。
func readFileWithTimeout(path string) ([]byte, error) {
	type result struct {
		data []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		data, err := os.ReadFile(path)
		ch <- result{data: data, err: err}
	}()
	select {
	case r := <-ch:
		return r.data, r.err
	case <-time.After(2 * time.Second):
		return nil, fmt.Errorf("timeout reading %s", path)
	}
}

// PortInfo 进程监听端口信息
type PortInfo struct {
	Protocol string `json:"protocol"` // tcp / tcp6 / udp / udp6
	Port     int    `json:"port"`
	Address  string `json:"address"` // 绑定地址，如 0.0.0.0 / 127.0.0.1 / ::
	State    string `json:"state"`   // TCP状态：LISTEN / ESTABLISHED 等；UDP固定为 ""
	IsHTTP   bool   `json:"is_http"` // 是否为 HTTP 端口（由前端浏览器 fetch 探测判定，后端仅返回原始端口数据）
}

// collectProcessPorts 采集进程及其子进程监听的端口
// 主路径：通过 /proc/<pid>/fd/* 读取 socket inode 匹配 /proc/net/
// 降级路径1：UID + cmdline 交叉验证 — 扫描 /proc 下所有同 UID 且 cmdline 匹合的进程，
//   再次尝试 inode 收集；如果 inode 仍然全部失败，进入下一级降级
// 降级路径2：网络 CLI 探测 — 用 ss/netstat 命令获取 LISTEN 端口列表，
//   对能显示 PID 的行做 cmdline 精确匹配；对不能显示 PID 的行（ptrace 受限）按 UID 匹配
// 降级路径3（最终兜底）：纯 /proc/net/ UID 匹配，同一 UID 的所有进程端口会被归到该服务，
//   可能导致多服务端口列表重叠（仅记录一次 Warn，避免日志刷屏）
func collectProcessPorts(pid int, cmdPattern string) []PortInfo {
	// 1. 尝试主路径：收集进程树所有 PID 的 socket inode
	inodes := collectSocketInodes(pid)

	var ports []PortInfo
	if len(inodes) > 0 {
		// 主路径：inode 精确匹配
		for _, proto := range []string{"tcp", "tcp6", "udp", "udp6"} {
			ports = append(ports, matchNetSockets(proto, inodes)...)
		}
	} else {
		// 降级路径：Yama LSM 或权限限制导致 readlink /proc/<pid>/fd 失败
		uid := getProcessUID(pid)
		if uid < 0 {
			return nil
		}
		slog.Debug("port collection: inode method failed, falling back to UID+cmdline matching",
			"pid", pid, "uid", uid)

		// 同时收集子进程 UID（子进程可能以不同用户运行）
		childUIDs := getChildProcessUIDs(pid)
		allUIDs := map[int]bool{uid: true}
		for _, cu := range childUIDs {
			allUIDs[cu] = true
		}

		// 降级路径1：UID + cmdline 交叉验证
		cmdVerifiedInodes := collectInodesByCmdlineUID(allUIDs, cmdPattern)
		if len(cmdVerifiedInodes) > 0 {
			slog.Debug("port collection: cmdline-verified inode matching succeeded",
				"pid", pid, "uid", uid, "cmdPattern", cmdPattern,
				"inodeCount", len(cmdVerifiedInodes))
			for _, proto := range []string{"tcp", "tcp6", "udp", "udp6"} {
				ports = append(ports, matchNetSockets(proto, cmdVerifiedInodes)...)
			}
		} else {
			// 降级路径2：网络 CLI 探测（ss/netstat）
			cliPorts := collectPortsByNetCLI(cmdPattern, allUIDs)
			if len(cliPorts) > 0 {
				slog.Debug("port collection: CLI net tool probing succeeded",
					"pid", pid, "uid", uid, "cmdPattern", cmdPattern,
					"portCount", len(cliPorts))
				ports = cliPorts
			} else {
				// 降级路径3：纯 UID 匹配（最终兜底）
				// 仅记录一次 Warn，避免日志刷屏
				logUIDFallbackOnce("port collection: all methods failed, using pure UID matching (ports may overlap across same-UID services)",
					"pid", pid, "uid", uid, "cmdPattern", cmdPattern)
				for _, proto := range []string{"tcp", "tcp6", "udp", "udp6"} {
					ports = append(ports, matchNetSocketsByUID(proto, allUIDs)...)
				}
			}
		}
	}

	// 3. 按 (protocol, port) 去重
	// 同一端口可能被多个 socket（不同 inode）绑定（如 BT 客户端的 DHT/uTP），
	// 或 tcp+tcp6 双栈监听，对显示而言只需保留一个代表
	seen := make(map[string]bool, len(ports))
	deduped := make([]PortInfo, 0, len(ports))
	for _, p := range ports {
		key := fmt.Sprintf("%s:%d", p.Protocol, p.Port)
		if !seen[key] {
			seen[key] = true
			deduped = append(deduped, p)
		}
	}
	return deduped
}

// collectProcessPortsByCommand 通过命令行匹配 /proc 查找进程并采集端口（PID命名空间降级方案）
// 当 gopsutil 因 PID 命名空间不一致无法找到进程时使用
func collectProcessPortsByCommand(cmdPattern string) []PortInfo {
	if cmdPattern == "" {
		return nil
	}
	// 提取命令基础名（如 ./bin/qbittorrent-nox → qbittorrent-nox）
	cmdBase := filepath.Base(cmdPattern)
	if cmdBase == "." || cmdBase == "/" || cmdBase == "" {
		cmdBase = cmdPattern
	}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		hostPID, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		// 读取 cmdline 验证命令匹配
		cmdlinePath := filepath.Join("/proc", entry.Name(), "cmdline")
		cmdData, err := readFileWithTimeout(cmdlinePath)
		if err != nil {
			continue
		}
		cmdline := strings.ReplaceAll(string(cmdData), "\x00", " ")
		if cmdline == "" || !strings.Contains(cmdline, cmdBase) {
			continue
		}
		// 匹配成功，采集该 host PID 的端口（collectProcessPorts 内含 UID 降级）
		return collectProcessPorts(hostPID, cmdBase)
	}
	return nil
}

// collectSocketInodes 收集进程树中所有 PID 的 socket inode
func collectSocketInodes(pid int) map[uint64]bool {
	inodes := make(map[uint64]bool)

	// 收集主进程 + 子进程的 PID
	pids := []int{pid}
	if children, err := getProcessChildren(pid); err == nil {
		pids = append(pids, children...)
	}

	for _, p := range pids {
		fdDir := filepath.Join("/proc", strconv.Itoa(p), "fd")
		entries, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			link, err := os.Readlink(filepath.Join(fdDir, entry.Name()))
			if err != nil {
				continue
			}
			// socket:[12345] 格式
			if strings.HasPrefix(link, "socket:[") {
				inodeStr := link[8 : len(link)-1]
				if inode, err := strconv.ParseUint(inodeStr, 10, 64); err == nil {
					inodes[inode] = true
				}
			}
		}
	}

	return inodes
}

// getProcessChildren 获取进程的所有子进程 PID
func getProcessChildren(pid int) ([]int, error) {
	p, err := getProcess(int32(pid))
	if err != nil {
		return nil, err
	}
	children, err := p.Children()
	if err != nil {
		return nil, err
	}
	result := make([]int, 0, len(children))
	for _, c := range children {
		result = append(result, int(c.Pid))
		// 递归获取孙子进程
		if grand, err := getProcessChildren(int(c.Pid)); err == nil {
			result = append(result, grand...)
		}
	}
	return result, nil
}

// matchNetSockets 解析 /proc/net/{proto} 文件，匹配属于目标进程的 socket
func matchNetSockets(proto string, inodes map[uint64]bool) []PortInfo {
	return readNetSockets(proto, func(fields []string) bool {
		if len(fields) < 10 {
			return false
		}
		inode, err := strconv.ParseUint(fields[9], 10, 64)
		return err == nil && inodes[inode]
	})
}

// matchNetSocketsByUID 在 /proc/net/{proto} 中按 UID 匹配端口
// 降级方案：当 readlink /proc/<pid>/fd 因 Yama LSM 等限制失败时使用
// 注意：UID 匹配精度低于 inode 匹配（同一 UID 的所有进程端口都会被匹配到），
// 但对于服务进程（通常独占一个 UID）精度足够
func matchNetSocketsByUID(proto string, uids map[int]bool) []PortInfo {
	return readNetSockets(proto, func(fields []string) bool {
		if len(fields) < 10 {
			return false
		}
		socketUID, err := strconv.Atoi(fields[7])
		return err == nil && uids[socketUID]
	})
}

// readNetSockets 读取 /proc/net/{proto} 文件，对每行调用 matchFn 判断是否属于目标进程
// 若匹配则解析为 PortInfo 返回
func readNetSockets(proto string, matchFn func(fields []string) bool) []PortInfo {
	path := "/proc/net/" + proto
	data, err := readFileWithTimeout(path)
	if err != nil {
		return nil
	}

	var ports []PortInfo
	lines := strings.Split(string(data), "\n")
	if len(lines) <= 1 {
		return nil
	}

	// 跳过第一行（表头）
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if !matchFn(fields) {
			continue
		}

		pi := parseNetSocketLine(fields, proto)
		if pi == nil {
			continue
		}
		ports = append(ports, *pi)
	}

	return ports
}

// parseNetSocketLine 将 /proc/net/ 中的一行数据解析为 PortInfo
// fields 为空格分隔的字段列表，proto 为 "tcp"/"tcp6"/"udp"/"udp6"
// 非 LISTEN 状态的 TCP socket 返回 nil
func parseNetSocketLine(fields []string, proto string) *PortInfo {
	// fields[1] = local_address:port (hex)
	localAddr := fields[1]
	state := fields[3]

	colonIdx := strings.LastIndex(localAddr, ":")
	if colonIdx < 0 {
		return nil
	}
	addrHex := localAddr[:colonIdx]
	portHex := localAddr[colonIdx+1:]

	port, err := strconv.ParseInt(portHex, 16, 32)
	if err != nil {
		return nil
	}

	addr := parseHexIP(addrHex, proto)

	// TCP: 只返回 LISTEN 状态 (0A)
	// UDP: 无状态，全部返回
	stateStr := ""
	if strings.HasPrefix(proto, "tcp") {
		if state != "0A" {
			return nil
		}
		stateStr = "LISTEN"
	}

	return &PortInfo{
		Protocol: proto,
		Port:     int(port),
		Address:  addr,
		State:    stateStr,
		IsHTTP:   false,
	}
}

// parseHexIP 将 /proc/net 中的十六进制 IP 地址转换为可读格式
// /proc/net/tcp 使用小端序十六进制表示 IP 地址
func parseHexIP(hex, proto string) string {
	if len(hex) != 8 && len(hex) != 32 {
		return hex
	}

	if len(hex) == 8 {
		// IPv4: 小端序，如 0100007F = 127.0.0.1
		n, err := strconv.ParseUint(hex, 16, 32)
		if err != nil {
			return hex
		}
		b := uint32(n)
		return strconv.Itoa(int(b&0xFF)) + "." +
			strconv.Itoa(int((b>>8)&0xFF)) + "." +
			strconv.Itoa(int((b>>16)&0xFF)) + "." +
			strconv.Itoa(int((b>>24)&0xFF))
	}

	// IPv6: 32字符十六进制，按 4 字节小端序分组
	// 简化处理：如果是全零返回 "::"，如果是 IPv4 映射地址返回 IPv4
	if hex == "00000000000000000000000000000000" {
		return "::"
	}
	// IPv4 映射地址: 最后8字符为 IPv4 的十六进制小端序
	if strings.HasPrefix(hex, "0000000000000000FFFF0000") {
		v4Part := hex[24:32]
		n, err := strconv.ParseUint(v4Part, 16, 32)
		if err == nil {
			b := uint32(n)
			return strconv.Itoa(int(b&0xFF)) + "." +
				strconv.Itoa(int((b>>8)&0xFF)) + "." +
				strconv.Itoa(int((b>>16)&0xFF)) + "." +
				strconv.Itoa(int((b>>24)&0xFF))
		}
	}
	return "::"
}

// getProcessUID 从 /proc/<pid>/status 读取进程的真实 UID
// 复用 readUIDGIDFromStatus，只取 UID 部分
// 返回 -1 表示读取失败
func getProcessUID(pid int) int {
	uid, _ := readUIDGIDFromStatus(pid)
	return uid
}

// getChildProcessUIDs 收集子进程的 UID 列表
// 用于 UID 降级方案中，确保子进程的端口也能被发现
func getChildProcessUIDs(pid int) []int {
	children, err := getProcessChildren(pid)
	if err != nil {
		return nil
	}
	var uids []int
	for _, childPID := range children {
		uid := getProcessUID(childPID)
		if uid >= 0 {
			uids = append(uids, uid)
		}
	}
	return uids
}

// collectInodesByCmdlineUID 通过 UID + cmdline 交叉验证收集 socket inode
// 扫描 /proc 下所有 UID 匹合且 cmdline 包含 cmdPattern 基础名的进程，
// 对这些进程尝试 readlink /proc/<pid>/fd/* 收集 inode
// 返回收集到的 inode map；如果所有 readlink 都失败（如 yama ptrace_scope=1）返回空 map
func collectInodesByCmdlineUID(uids map[int]bool, cmdPattern string) map[uint64]bool {
	if len(uids) == 0 || cmdPattern == "" {
		return nil
	}

	// 提取命令基础名（如 ./bin/qbittorrent-nox → qbittorrent-nox）
	cmdBase := filepath.Base(cmdPattern)
	if cmdBase == "." || cmdBase == "/" || cmdBase == "" {
		cmdBase = cmdPattern
	}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}

	inodes := make(map[uint64]bool)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		// 1. 检查进程 UID 是否在目标 UID 集合中
		procUID := getProcessUID(pid)
		if procUID < 0 || !uids[procUID] {
			continue
		}

		// 2. 检查 cmdline 是否包含目标服务的命令基础名
		cmdlinePath := filepath.Join("/proc", entry.Name(), "cmdline")
		cmdData, err := readFileWithTimeout(cmdlinePath)
		if err != nil {
			continue
		}
		cmdline := strings.ReplaceAll(string(cmdData), "\x00", " ")
		if cmdline == "" || !strings.Contains(cmdline, cmdBase) {
			continue
		}

		// 3. 对匹配的进程尝试 collectSocketInodes
		pidInodes := collectSocketInodes(pid)
		for inode := range pidInodes {
			inodes[inode] = true
		}
	}

	return inodes
}

// cmdPatternFromConfig 从服务配置中提取命令模式字符串
// 用于 collectProcessPorts 的 cmdline 交叉验证
// 返回 Command[0]（如果存在）或空字符串
func cmdPatternFromConfig(cfg *config.ServiceConfig) string {
	if cfg != nil && len(cfg.Command) > 0 {
		return cfg.Command[0]
	}
	return ""
}

// --- 降级路径2：网络 CLI 探测（ss / netstat）---

// collectPortsByNetCLI 使用 ss 或 netstat 命令行工具采集 LISTEN 端口
// 解析输出中带 PID/进程名的行（做 cmdline 交叉验证），对无 PID 的行按 UID 匹配
// 如果 ss 和 netstat 都不可用或全部失败，返回空列表（由上层退回纯 UID 匹配）
func collectPortsByNetCLI(cmdPattern string, uids map[int]bool) []PortInfo {
	// 提取命令基础名
	cmdBase := filepath.Base(cmdPattern)
	if cmdBase == "." || cmdBase == "/" || cmdBase == "" {
		cmdBase = cmdPattern
	}

	var ports []PortInfo

	// 优先尝试 ss（分别获取 TCP 和 UDP）
	ssTCP, ssTCPErr := runNetCLI("ss", []string{"-tlnp"})
	ssUDP, ssUDPErr := runNetCLI("ss", []string{"-ulnp"})

	if ssTCPErr == nil || ssUDPErr == nil {
		// ss 至少有一个可用
		if ssTCPErr == nil {
			ports = append(ports, parseSSOutput(ssTCP, cmdBase, uids)...)
		}
		if ssUDPErr == nil {
			ports = append(ports, parseSSOutput(ssUDP, cmdBase, uids)...)
		}
		if len(ports) > 0 {
			return ports
		}
	}

	// ss 不可用或结果为空，尝试 netstat（BusyBox 或 GNU net-tools）
	nsTCP, nsTCPErr := runNetCLI("netstat", []string{"-tlnp"})
	nsUDP, nsUDPErr := runNetCLI("netstat", []string{"-ulnp"})

	if nsTCPErr == nil || nsUDPErr == nil {
		if nsTCPErr == nil {
			ports = append(ports, parseNetstatOutput(nsTCP, cmdBase, uids)...)
		}
		if nsUDPErr == nil {
			ports = append(ports, parseNetstatOutput(nsUDP, cmdBase, uids)...)
		}
		if len(ports) > 0 {
			return ports
		}
	}

	// ss 和 netstat 都不可用或结果为空
	slog.Debug("port collection: no usable CLI net tool or empty results")
	return nil
}

// runNetCLI 执行网络命令行工具，返回其输出
func runNetCLI(name string, args []string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s not found: %w", name, err)
	}
	cmd := exec.Command(path, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s failed: %w", name, err)
	}
	return string(out), nil
}

// ssLineRegex 解析 ss 输出中的 users:((\"procname\",pid=N,fd=N)) 格式
// ss 输出可能含多个用户: users:((\"dropbear\",pid=46,fd=3),("supd",pid=1,fd=5))
// 只匹配第一个用户条目（通常就是进程本身）
var ssLineRegex = regexp.MustCompile(`users:\(\("([^"]+)",pid=(\d+),fd=(\d+)`)

// parseSSOutput 解析 ss -tlnp/-ulnp 输出
// 对带 Process 列的行：提取 PID 和进程名，做 cmdline 交叉验证
// 对无 Process 列的行：从 /proc/net/<proto> 按 UID 匹配（复用已有逻辑）
//
// ss 输出格式（iproute2-6.9.0，strings.Fields 分割后的列索引）：
//
//	无 Process 列: [0]=State [1]=Recv-Q [2]=Send-Q [3]=Local Address:Port [4]=Peer Address:Port
//	有 Process 列: [0]=State [1]=Recv-Q [2]=Send-Q [3]=Local Address:Port [4]=Peer Address:Port [5]=Process
//
// 注意：ss 的列之间有大量填充空格，strings.Fields 会合并，
// 所以 Local Address:Port 永远在 fields[3]（不是表头中视觉位置）
func parseSSOutput(output string, cmdBase string, uids map[int]bool) []PortInfo {
	lines := strings.Split(output, "\n")
	if len(lines) <= 1 {
		return nil
	}

	var ports []PortInfo
	var uidMatchedPorts []PortInfo // 无 PID 的行按 UID 匹配

	for _, line := range lines[1:] { // 跳过表头
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		// 检查是否有 Process 列（users:((...)) 格式）
		processMatch := ssLineRegex.FindStringSubmatch(line)

		// ss 用 strings.Fields 分割后，Local Address:Port 在 fields[3]
		// （fields[0]=State, [1]=Recv-Q, [2]=Send-Q, [3]=Local Address:Port, [4]=Peer Address:Port）
		localAddrField := fields[3]
		proto := "tcp"
		// ss -ulnp 输出的 State 列以 UNCONN 开头
		if strings.HasPrefix(fields[0], "UNCONN") {
			proto = "udp"
		}

		// 解析地址和端口
		addr, port, ok := parseSSAddressPort(localAddrField)
		if !ok {
			continue
		}

		// 判断是否为 IPv6
		if strings.HasPrefix(localAddrField, "[") || strings.HasPrefix(addr, "::") {
			if proto == "tcp" {
				proto = "tcp6"
			} else if proto == "udp" {
				proto = "udp6"
			}
		}

		// TCP 只返回 LISTEN 状态
		stateStr := ""
		if strings.HasPrefix(proto, "tcp") {
			if fields[0] != "LISTEN" {
				continue
			}
			stateStr = "LISTEN"
		}

		if processMatch != nil {
			// 有 PID 信息——做 cmdline 交叉验证
			pidStr := processMatch[2]
			_, err := strconv.Atoi(pidStr)
			if err != nil {
				continue
			}
			procName := processMatch[1]

			// cmdline 交叉验证：进程名包含 cmdBase
			if cmdBase != "" && !strings.Contains(procName, cmdBase) {
				// 还可以读 /proc/<pid>/cmdline 做更精确验证
				cmdlinePath := filepath.Join("/proc", pidStr, "cmdline")
				cmdData, err := readFileWithTimeout(cmdlinePath)
				if err != nil || !strings.Contains(strings.ReplaceAll(string(cmdData), "\x00", " "), cmdBase) {
					continue // 不匹配目标服务
				}
			}

			ports = append(ports, PortInfo{
				Protocol: proto,
				Port:     port,
				Address:  addr,
				State:    stateStr,
				IsHTTP:   false,
			})
		} else {
			// 无 PID 信息——记录为 UID 匹配候选
			// 后面从 /proc/net/ 按 UID 重新匹配这些端口（确保 UID 准确）
			uidMatchedPorts = append(uidMatchedPorts, PortInfo{
				Protocol: proto,
				Port:     port,
				Address:  addr,
				State:    stateStr,
				IsHTTP:   false,
			})
		}
	}

	// 对无 PID 的端口，用 /proc/net/ 按 UID 精确匹配
	// 这样确保只有真正属于目标 UID 的端口被包含
	if len(uidMatchedPorts) > 0 {
		for _, proto := range []string{"tcp", "tcp6", "udp", "udp6"} {
			uidPorts := matchNetSocketsByUID(proto, uids)
			// 只保留在 ss 输出中出现的端口（交叉验证）
			ssPortSet := make(map[int]bool)
			for _, p := range uidMatchedPorts {
				ssPortSet[p.Port] = true
			}
			for _, p := range uidPorts {
				if ssPortSet[p.Port] {
					ports = append(ports, p)
				}
			}
		}
	}

	return ports
}

// parseSSAddressPort 解析 ss 输出中的地址:端口字段
// 格式可能为: "0.0.0.0:9091", "[::]:8000", "192.168.31.188%eth0:29321",
// "*:7979", "127.0.0.1:25", "[::1]%lo:29321"
func parseSSAddressPort(addrPort string) (addr string, port int, ok bool) {
	// 找最后一个冒号分隔端口
	colonIdx := strings.LastIndex(addrPort, ":")
	if colonIdx < 0 {
		return "", 0, false
	}

	portStr := addrPort[colonIdx+1:]
	p, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, false
	}

	addrPart := addrPort[:colonIdx]

	// 处理 IPv6 格式: [::]:8000 或 [::1]%lo:29321 → 提取 [] 内的地址
	if strings.HasPrefix(addrPart, "[") {
		endBracket := strings.Index(addrPart, "]")
		if endBracket > 0 {
			addr = addrPart[1:endBracket]
		} else {
			addr = addrPart[1:] // 去掉开头的 [
		}
	} else if addrPart == "*" {
		addr = "0.0.0.0" // ss 用 * 表示通配
	} else {
		// 处理 %interface 后缀: 192.168.31.188%eth0 → 192.168.31.188
		percentIdx := strings.Index(addrPart, "%")
		if percentIdx > 0 {
			addr = addrPart[:percentIdx]
		} else {
			addr = addrPart
		}
	}

	// IPv6 地址规范化
	if strings.Contains(addr, ":") && addr != "::" {
		// 对非 :: 的 IPv6 地址
		// 如果是 IPv4 映射地址 ::ffff:127.0.0.1，提取 IPv4 部分
		if strings.HasPrefix(addr, "::ffff:") {
			addr = addr[7:] // 去掉 ::ffff: 前缀
		}
		// 其他 IPv6 地址（如 ::1）保持原样
	}

	return addr, p, true
}

// parseNetstatOutput 解析 netstat -tlnp/-ulnp 输出
// 格式: Proto Recv-Q Send-Q Local Address  Foreign Address  State(TCP only)  PID/Program name
//
// 注意：TCP 行有 State 列（7字段），UDP 行无 State 列（6字段）：
//   TCP: Proto Recv-Q Send-Q Local Address  Foreign Address  State  PID/Program name
//   UDP: Proto Recv-Q Send-Q Local Address  Foreign Address  PID/Program name
func parseNetstatOutput(output string, cmdBase string, uids map[int]bool) []PortInfo {
	lines := strings.Split(output, "\n")
	if len(lines) <= 2 {
		return nil
	}

	var ports []PortInfo
	var uidMatchedPorts []PortInfo

	for _, line := range lines[2:] { // 跳过前两行（标题 + 表头）
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}

		proto := fields[0]  // tcp / tcp6 / udp / udp6
		localAddr := fields[3] // Local Address:Port
		pidProgram := "" // PID/Program name

		// TCP 行: fields[5]=State, fields[6]=PID/Program name (7字段)
		// UDP 行: fields[5]=PID/Program name (6字段)
		state := ""
		if strings.HasPrefix(proto, "tcp") {
			if len(fields) < 7 {
				continue
			}
			state = fields[5]
			pidProgram = fields[6]
		} else {
			// UDP: 无 State 列
			pidProgram = fields[5]
		}

		// 解析地址:端口
		colonIdx := strings.LastIndex(localAddr, ":")
		if colonIdx < 0 {
			continue
		}
		addr := localAddr[:colonIdx]
		portStr := localAddr[colonIdx+1:]
		port, err := strconv.Atoi(portStr)
		if err != nil {
			continue
		}

		// TCP 只返回 LISTEN 状态
		stateStr := ""
		if strings.HasPrefix(proto, "tcp") {
			if state != "LISTEN" {
				continue
			}
			stateStr = "LISTEN"
		}

		// 解析 PID/Program name
		// 格式: "46/dropbear" 或 "-"（无 PID）
		if pidProgram != "-" && strings.Contains(pidProgram, "/") {
			slashIdx := strings.Index(pidProgram, "/")
			pidStr := pidProgram[:slashIdx]
			procName := pidProgram[slashIdx+1:]

			_, err := strconv.Atoi(pidStr)
			if err != nil {
				continue
			}

			// cmdline 交叉验证
			if cmdBase != "" && !strings.Contains(procName, cmdBase) {
				cmdlinePath := filepath.Join("/proc", pidStr, "cmdline")
				cmdData, err := readFileWithTimeout(cmdlinePath)
				if err != nil || !strings.Contains(strings.ReplaceAll(string(cmdData), "\x00", " "), cmdBase) {
					continue
				}
			}

			ports = append(ports, PortInfo{
				Protocol: proto,
				Port:     port,
				Address:  addr,
				State:    stateStr,
				IsHTTP:   false,
			})
		} else {
			// 无 PID——UID 匹配候选
			uidMatchedPorts = append(uidMatchedPorts, PortInfo{
				Protocol: proto,
				Port:     port,
				Address:  addr,
				State:    stateStr,
				IsHTTP:   false,
			})
		}
	}

	// 对无 PID 的端口做 UID 交叉验证
	if len(uidMatchedPorts) > 0 {
		for _, proto := range []string{"tcp", "tcp6", "udp", "udp6"} {
			uidPorts := matchNetSocketsByUID(proto, uids)
			ssPortSet := make(map[int]bool)
			for _, p := range uidMatchedPorts {
				ssPortSet[p.Port] = true
			}
			for _, p := range uidPorts {
				if ssPortSet[p.Port] {
					ports = append(ports, p)
				}
			}
		}
	}

	return ports
}

// --- 一次性日志：避免 UID 降级 Warn 每 5 秒刷屏 ---

// uidFallbackLogger 记录每个 (uid, cmdPattern) 组合的 Warn 是否已输出
var uidFallbackLogger = struct {
	sync.Mutex
	logged map[string]bool
}{logged: make(map[string]bool)}

// logUIDFallbackOnce 对同一 (uid, cmdPattern) 组合只输出一次 Warn 日志
// 后续相同组合只输出 Debug 级别
func logUIDFallbackOnce(msg string, args ...any) {
	// 从 args 中提取 uid 和 cmdPattern 作为 key
	key := ""
	for i := 0; i+1 < len(args); i++ {
		k := args[i]
		v := args[i+1]
		if k == "uid" || k == "cmdPattern" {
			key += fmt.Sprintf("%v:%v", k, v) + ","
		}
	}

	uidFallbackLogger.Lock()
	first := !uidFallbackLogger.logged[key]
	if first {
		uidFallbackLogger.logged[key] = true
	}
	uidFallbackLogger.Unlock()

	if first {
		slog.Warn(msg, args...)
	} else {
		slog.Debug(msg, args...)
	}
}
