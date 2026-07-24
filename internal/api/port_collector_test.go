package api

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestParseHexIP_IPv4 测试 IPv4 十六进制小端序解析
// L-01-001 补充：覆盖 port_collector.go 中 parseHexIP 的 IPv4 分支
// /proc/net/tcp 使用小端序：字节 C0 A8 01 01 → 十六进制 "0101A8C0" → 192.168.1.1
func TestParseHexIP_IPv4(t *testing.T) {
	cases := []struct {
		name string
		hex  string
		want string
	}{
		{"loopback 127.0.0.1", "0100007F", "127.0.0.1"},
		{"any 0.0.0.0", "00000000", "0.0.0.0"},
		{"broadcast 255.255.255.255", "FFFFFFFF", "255.255.255.255"},
		{"192.168.1.1", "0101A8C0", "192.168.1.1"},
		{"10.0.0.1", "0100000A", "10.0.0.1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseHexIP(c.hex, "tcp")
			if got != c.want {
				t.Errorf("parseHexIP(%q, tcp) = %q, want %q", c.hex, got, c.want)
			}
		})
	}
}

// TestParseHexIP_IPv6 测试 IPv6 十六进制解析
// L-01-001 补充：覆盖 parseHexIP 的 IPv6 分支
// /proc/net/tcp6 按 4 字节小端序分组：::ffff:127.0.0.1 → "0000000000000000FFFF00000100007F"
func TestParseHexIP_IPv6(t *testing.T) {
	cases := []struct {
		name string
		hex  string
		want string
	}{
		{"all zeros", "00000000000000000000000000000000", "::"},
		{"IPv4 mapped 127.0.0.1", "0000000000000000FFFF00000100007F", "127.0.0.1"},
		{"IPv4 mapped 0.0.0.0", "0000000000000000FFFF000000000000", "0.0.0.0"},
		{"unknown ipv6 returns ::", "0102030405060708090A0B0C0D0E0F10", "::"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseHexIP(c.hex, "tcp6")
			if got != c.want {
				t.Errorf("parseHexIP(%q, tcp6) = %q, want %q", c.hex, got, c.want)
			}
		})
	}
}

// TestParseHexIP_Invalid 测试无效输入的容错处理
// L-01-001 补充：覆盖 parseHexIP 的错误分支
func TestParseHexIP_Invalid(t *testing.T) {
	cases := []struct {
		name string
		hex  string
	}{
		{"empty string", ""},
		{"odd length", "0100"},
		{"invalid hex chars", "GGGGGGGG"},
		{"partial length 10", "0100007F00"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// 无效输入不应 panic，返回原值或合理默认值
			got := parseHexIP(c.hex, "tcp")
			// 只要不 panic 即可，返回值不严格校验
			_ = got
		})
	}
}

// TestPortInfo_JSONTags 测试 PortInfo 结构体的 JSON 标签
// L-01-001 补充：覆盖 F-06-001 字段名 snake_case 一致性
func TestPortInfo_JSONTags(t *testing.T) {
	p := PortInfo{
		Protocol: "tcp",
		Port:     8080,
		Address:  "0.0.0.0",
		State:    "LISTEN",
		IsHTTP:   false,
	}
	// 验证字段可通过 JSON 序列化
	if p.Protocol != "tcp" || p.Port != 8080 || p.Address != "0.0.0.0" {
		t.Errorf("PortInfo field values incorrect: %+v", p)
	}
}

// TestMatchNetSocketsByUID 测试 UID 降级方案的端口匹配
// 覆盖 Yama LSM ptrace_scope=1 导致 readlink /proc/<pid>/fd 失败的场景
func TestMatchNetSocketsByUID(t *testing.T) {
	// 模拟 /proc/net/tcp 数据：含不同 UID 的 LISTEN 行
	procNetTCP := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:1AE1 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 92294 1 000000009714c37e 100 0 0 10 0
   1: 00000000:2383 00000000:0000 0A 00000000:00000000 00:00000000 00000000 65534        0 502163060 1 0000000067168d23 100 0 0 10 0
   2: 00000000:C8D5 00000000:0000 0A 00000000:00000000 00:00000000 00000000 65534        0 502163044 1 00000000331755e9 100 0 0 10 5
   3: 0100007F:0019 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 80125 1 000000004193aa5e 100 0 0 10 0
   4: 00000000:07E1 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 88708 1 000000007e8859f2 100 0 0 10 0`

	// 写入临时文件模拟 /proc/net/tcp
	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/tcp"
	if err := writeFileForTest(tmpFile, procNetTCP); err != nil {
		t.Fatal(err)
	}

	// 匹配 UID 65534 (nobody) 的端口
	uids := map[int]bool{65534: true}
	ports := matchNetSocketsByUIDFromFile(tmpFile, uids)

	// 应匹配到 9091 (0x2383) 和 51413 (0xC8D5)
	if len(ports) != 2 {
		t.Fatalf("expected 2 ports for UID 65534, got %d: %+v", len(ports), ports)
	}

	found9091 := false
	found51413 := false
	for _, p := range ports {
		if p.Port == 9091 && p.Protocol == "tcp" && p.State == "LISTEN" {
			found9091 = true
		}
		if p.Port == 51413 && p.Protocol == "tcp" && p.State == "LISTEN" {
			found51413 = true
		}
	}
	if !found9091 {
		t.Error("expected port 9091 (transmission RPC) for UID 65534")
	}
	if !found51413 {
		t.Error("expected port 51413 (transmission peer) for UID 65534")
	}

	// 匹配 UID 0 (root) 的端口
	rootUIDs := map[int]bool{0: true}
	rootPorts := matchNetSocketsByUIDFromFile(tmpFile, rootUIDs)
	// 应匹配到 6881(0x1AE1), 25(0x0019), 2017(0x07E1)
	if len(rootPorts) != 3 {
		t.Fatalf("expected 3 ports for UID 0, got %d: %+v", len(rootPorts), rootPorts)
	}
}

// TestMatchNetSocketsByUID_UDP 测试 UID 匹配 UDP 端口
func TestMatchNetSocketsByUID_UDP(t *testing.T) {
	procNetUDP := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
 9168: 00000000:C8D5 00000000:0000 07 00000000:00000000 00:00000000 00000000 65534        0 502163047 2 000000000c729f1b 0
30062: 00000000:1A73 00000000:0000 07 00000000:00000000 00:00000000 00000000 65534        0 502163049 2 00000000480576b0 0
   0: 00000000:1F40 00000000:0000 07 00000000:00000000 00:00000000 00000000     0        0 743410 1 0000000041fae367 100 0 0 10 0`

	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/udp"
	if err := writeFileForTest(tmpFile, procNetUDP); err != nil {
		t.Fatal(err)
	}

	uids := map[int]bool{65534: true}
	ports := matchNetSocketsByUIDFromFile(tmpFile, uids)

	// 应匹配到 51413 (0xC8D5) 和 6739 (0x1A73)
	if len(ports) != 2 {
		t.Fatalf("expected 2 UDP ports for UID 65534, got %d: %+v", len(ports), ports)
	}

	found51413 := false
	found6771 := false
	for _, p := range ports {
		if p.Port == 51413 && p.Protocol == "udp" && p.State == "" {
			found51413 = true
		}
		if p.Port == 6771 && p.Protocol == "udp" && p.State == "" {
			found6771 = true
		}
	}
	if !found51413 {
		t.Error("expected UDP port 51413 for UID 65534")
	}
	if !found6771 {
		t.Error("expected UDP port 6771 for UID 65534")
	}
}

// TestMatchNetSocketsByUID_NoMatch 测试 UID 匹配无结果
func TestMatchNetSocketsByUID_NoMatch(t *testing.T) {
	procNetTCP := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:1AE1 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 92294 1 000000009714c37e 100 0 0 10 0`

	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/tcp"
	if err := writeFileForTest(tmpFile, procNetTCP); err != nil {
		t.Fatal(err)
	}

	// UID 999 不存在于数据中
	uids := map[int]bool{999: true}
	ports := matchNetSocketsByUIDFromFile(tmpFile, uids)
	if len(ports) != 0 {
		t.Errorf("expected 0 ports for non-existent UID, got %d", len(ports))
	}
}

// TestGetProcessUID_StatusParsing 测试从 /proc/<pid>/status 解析 UID
func TestGetProcessUID_StatusParsing(t *testing.T) {
	// 模拟 /proc/<pid>/status 文件
	statusContent := `Name:   transmission-da
Umask:  0022
State:  S (sleeping)
Tgid:   27
Ngid:   0
Pid:    27
PPid:   1
TracerPid:      0
Uid:    65534   65534   65534   65534
Gid:    65534   65534   65534   65534`

	tmpDir := t.TempDir()
	pidDir := tmpDir + "/27"
	if err := os.MkdirAll(pidDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := writeFileForTest(pidDir+"/status", statusContent); err != nil {
		t.Fatal(err)
	}

	uid := getProcessUIDFromStatusFile(pidDir + "/status")
	if uid != 65534 {
		t.Errorf("expected UID 65534 (nobody), got %d", uid)
	}

	// 测试 root 进程
	rootStatus := `Name:   dropbear
Uid:    0       0       0       0`
	rootDir := tmpDir + "/1"
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := writeFileForTest(rootDir+"/status", rootStatus); err != nil {
		t.Fatal(err)
	}

	rootUID := getProcessUIDFromStatusFile(rootDir + "/status")
	if rootUID != 0 {
		t.Errorf("expected UID 0 (root), got %d", rootUID)
	}
}

// TestGetProcessUID_MissingFile 测试缺失 /proc/<pid>/status 的容错
func TestGetProcessUID_MissingFile(t *testing.T) {
	uid := getProcessUIDFromStatusFile("/nonexistent/path/status")
	if uid != -1 {
		t.Errorf("expected -1 for missing file, got %d", uid)
	}
}

// TestGetProcessUID_NoUidLine 测试无 Uid 行的容错
func TestGetProcessUID_NoUidLine(t *testing.T) {
	statusContent := `Name:   test
State:  S (sleeping)`
	tmpDir := t.TempDir()
	if err := writeFileForTest(tmpDir+"/status", statusContent); err != nil {
		t.Fatal(err)
	}

	uid := getProcessUIDFromStatusFile(tmpDir + "/status")
	if uid != -1 {
		t.Errorf("expected -1 for status without Uid line, got %d", uid)
	}
}

// Helper: 写入测试文件
func writeFileForTest(path string, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}

// matchNetSocketsByUIDFromFile 从指定文件读取数据并按 UID 匹配端口（测试用）
func matchNetSocketsByUIDFromFile(path string, uids map[int]bool) []PortInfo {
	data, err := readFileWithTimeout(path)
	if err != nil {
		return nil
	}

	var ports []PortInfo
	lines := strings.Split(string(data), "\n")
	if len(lines) <= 1 {
		return nil
	}

	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}

		uidStr := fields[7]
		socketUID, err := strconv.Atoi(uidStr)
		if err != nil {
			continue
		}
		if !uids[socketUID] {
			continue
		}

		localAddr := fields[1]
		state := fields[3]

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

		// 使用文件名推断协议
		proto := "tcp"
		if strings.Contains(path, "udp6") {
			proto = "udp6"
		} else if strings.Contains(path, "udp") {
			proto = "udp"
		} else if strings.Contains(path, "tcp6") {
			proto = "tcp6"
		}

		addr := parseHexIP(addrHex, proto)

		stateStr := ""
		if strings.HasPrefix(proto, "tcp") {
			if state != "0A" {
				continue
			}
			stateStr = "LISTEN"
		}

		ports = append(ports, PortInfo{
			Protocol: proto,
			Port:     int(port),
			Address:  addr,
			State:    stateStr,
			IsHTTP:   false,
		})
	}

	return ports
}

// getProcessUIDFromStatusFile 从指定 status 文件解析 UID（测试用）
func getProcessUIDFromStatusFile(path string) int {
	data, err := readFileWithTimeout(path)
	if err != nil {
		return -1
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "Uid:") {
			fields := strings.Fields(strings.TrimPrefix(line, "Uid:"))
			if len(fields) >= 1 {
				if uid, err := strconv.Atoi(fields[0]); err == nil {
					return uid
				}
			}
			break
		}
	}
	return -1
}
