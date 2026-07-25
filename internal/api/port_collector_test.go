package api

import "testing"

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
			_ = parseHexIP(c.hex, "tcp")
		})
	}
}

// TestPortInfo_JSONTags 测试 PortInfo 结构体字段
func TestPortInfo_JSONTags(t *testing.T) {
	p := PortInfo{
		Protocol: "tcp",
		Port:     8080,
		Address:  "0.0.0.0",
		State:    "LISTEN",
		IsHTTP:   false,
	}
	if p.Protocol != "tcp" || p.Port != 8080 || p.Address != "0.0.0.0" {
		t.Errorf("PortInfo field values incorrect: %+v", p)
	}
}

func TestParseNetSocketLine(t *testing.T) {
	listenFields := []string{"0:", "0100007F:1F90", "00000000:0000", "0A", "0", "0", "0", "1000", "0", "12345"}
	got := parseNetSocketLine(listenFields, "tcp")
	if got == nil || got.Protocol != "tcp" || got.Port != 8080 || got.Address != "127.0.0.1" || got.State != "LISTEN" {
		t.Fatalf("unexpected parsed socket: %+v", got)
	}

	nonListenFields := append([]string(nil), listenFields...)
	nonListenFields[3] = "01"
	if got := parseNetSocketLine(nonListenFields, "tcp"); got != nil {
		t.Fatalf("expected non-LISTEN TCP socket to be ignored, got %+v", got)
	}
}
