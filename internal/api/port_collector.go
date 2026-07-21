package api

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
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
// 通过读取 /proc/net/{tcp,tcp6,udp,udp6} 和 /proc/<pid>/fd/* 匹配 socket inode
func collectProcessPorts(pid int) []PortInfo {
	// 1. 收集进程树所有 PID 的 socket inode
	inodes := collectSocketInodes(pid)
	if len(inodes) == 0 {
		return nil
	}

	// 2. 读取 /proc/net/{tcp,tcp6,udp,udp6} 匹配 inode
	var ports []PortInfo
	for _, proto := range []string{"tcp", "tcp6", "udp", "udp6"} {
		ports = append(ports, matchNetSockets(proto, inodes)...)
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

	var ports []PortInfo
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
		// 匹配成功，采集该 host PID 的端口
		ports = append(ports, collectProcessPorts(hostPID)...)
		break // 只取第一个匹配的主进程
	}
	return ports
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
		if len(fields) < 10 {
			continue
		}

		// fields[1] = local_address:port (hex)
		// fields[2] = remote_address:port (hex)
		// fields[3] = state (hex, TCP only)
		// fields[9] = inode
		localAddr := fields[1]
		state := fields[3]
		inodeStr := fields[9]

		inode, err := strconv.ParseUint(inodeStr, 10, 64)
		if err != nil {
			continue
		}
		if !inodes[inode] {
			continue
		}

		// 解析本地地址和端口
		colonIdx := strings.LastIndex(localAddr, ":")
		if colonIdx < 0 {
			continue
		}
		addrHex := localAddr[:colonIdx]
		portHex := localAddr[colonIdx+1:]

		port, err := strconv.ParseInt(portHex, 16, 32)
		if err != nil {
			continue
		}

		addr := parseHexIP(addrHex, proto)

		// TCP: 只返回 LISTEN 状态 (0A)
		// UDP: 无状态，全部返回
		stateStr := ""
		if strings.HasPrefix(proto, "tcp") {
			if state != "0A" {
				continue // 非 LISTEN 状态跳过
			}
			stateStr = "LISTEN"
		}

		ports = append(ports, PortInfo{
			Protocol: proto,
			Port:     int(port),
			Address:  addr,
			State:    stateStr,
			IsHTTP:   false, // HTTP 判定由前端浏览器 fetch 探测完成
		})
	}

	return ports
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
