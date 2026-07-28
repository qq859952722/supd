package cli

import (
	"net"
	"strings"
	"testing"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/watch"
)

func boolPtr(b bool) *bool { return &b }

// TestFormatStartupSummary_ContainsFields 验证摘要包含各字段且不含 auth_token 明文。
func TestFormatStartupSummary_ContainsFields(t *testing.T) {
	s := startupSummary{
		Version:           "v0.0.38",
		BuildTime:         "20260728",
		GoVersion:         "go1.25",
		GOOS:              "linux",
		GOArch:            "amd64",
		PID:               12345,
		WorkDir:           "/etc/supd",
		ConfigPath:        "/etc/supd/config.yaml",
		LogDir:            "/var/log/supd",
		PID1Mode:          "enabled (subreaper+zombie reaper)",
		AuthMode:          "local_skip",
		TokenSet:          true,
		LocalNetworks:     []string{"127.0.0.0/8"},
		ConfiguredListen:  ":7979",
		ServicesTotal:     8,
		ServicesAutostart: 5,
		ExtensionsTotal:   12,
		Warnings:          0,
	}
	out := formatStartupSummary(s)

	checks := []string{
		"v0.0.38", "20260728", "go1.25", "linux/amd64",
		"pid:        12345",
		"workdir:    /etc/supd",
		"config:     /etc/supd/config.yaml",
		"log dir:    /var/log/supd",
		"pid1 mode:  enabled (subreaper+zombie reaper)",
		"auth:       local_skip (token configured: true)",
		"listen:     :7979",
		"services:   8 loaded, 5 autostart",
		"extensions: 12 loaded",
		"warnings:   0",
	}
	for _, c := range checks {
		if !strings.Contains(out, c) {
			t.Errorf("摘要缺少 %q\n输出:\n%s", c, out)
		}
	}

	// 安全红线：auth_token 明文绝不出现（即使 TokenSet=true）
	if strings.Contains(out, "auth_token") || strings.Contains(out, "secret") {
		t.Errorf("摘要泄漏敏感信息:\n%s", out)
	}
}

// TestFormatStartupSummary_PortZero 验证 :0 动态端口标注。
func TestFormatStartupSummary_PortZero(t *testing.T) {
	s := startupSummary{
		Version: "dev", BuildTime: "unknown", GoVersion: "go1.25",
		GOOS: "linux", GOArch: "amd64", PID: 1,
		WorkDir: "/tmp", ConfigPath: "/tmp/config.yaml", LogDir: "/tmp/logs",
		PID1Mode: "inactive", AuthMode: "none",
		ConfiguredListen: ":0",
	}
	out := formatStartupSummary(s)
	if !strings.Contains(out, "listen:     :0 (动态端口)") {
		t.Errorf("动态端口标注缺失:\n%s", out)
	}
}

// TestFormatStartupSummary_EmptyListen 验证空 listen 显示默认值。
func TestFormatStartupSummary_EmptyListen(t *testing.T) {
	s := startupSummary{
		Version: "dev", BuildTime: "unknown", GoVersion: "go1.25",
		GOOS: "linux", GOArch: "amd64", PID: 1,
		WorkDir: "/tmp", ConfigPath: "/tmp/config.yaml", LogDir: "/tmp/logs",
		PID1Mode: "inactive", AuthMode: "none",
		ConfiguredListen: "",
	}
	out := formatStartupSummary(s)
	if !strings.Contains(out, ":7979 (default)") {
		t.Errorf("空 listen 应显示默认值:\n%s", out)
	}
}

// TestFormatListenSummary 验证第二段输出格式。
func TestFormatListenSummary(t *testing.T) {
	t.Run("multiple_urls", func(t *testing.T) {
		out := formatListenSummary("0.0.0.0:7979", []string{
			"http://192.168.1.10:7979",
			"http://127.0.0.1:7979",
		})
		if !strings.Contains(out, "实际监听: 0.0.0.0:7979") {
			t.Errorf("缺少实际监听行:\n%s", out)
		}
		if !strings.Contains(out, "可访问地址:") {
			t.Errorf("缺少可访问地址标题:\n%s", out)
		}
		if !strings.Contains(out, "  http://192.168.1.10:7979") {
			t.Errorf("缺少 URL 行:\n%s", out)
		}
	})

	t.Run("no_urls", func(t *testing.T) {
		out := formatListenSummary("0.0.0.0:7979", nil)
		if strings.Contains(out, "可访问地址") {
			t.Errorf("空 URL 列表不应出现可访问地址块:\n%s", out)
		}
		if !strings.Contains(out, "实际监听: 0.0.0.0:7979") {
			t.Errorf("缺少实际监听行:\n%s", out)
		}
	})

	t.Run("single_url", func(t *testing.T) {
		out := formatListenSummary("127.0.0.1:7979", []string{"http://127.0.0.1:7979"})
		if !strings.Contains(out, "http://127.0.0.1:7979") {
			t.Errorf("缺少 URL:\n%s", out)
		}
	})
}

// TestCountAutostart 验证 autostart 计数（nil/true/false 三种情况）。
func TestCountAutostart(t *testing.T) {
	services := map[string]*watch.ServiceEntry{
		"svc-nil":   {Config: &config.ServiceConfig{Name: "svc-nil", Autostart: nil}},
		"svc-true":  {Config: &config.ServiceConfig{Name: "svc-true", Autostart: boolPtr(true)}},
		"svc-false": {Config: &config.ServiceConfig{Name: "svc-false", Autostart: boolPtr(false)}},
		"svc-nil2":  {Config: nil}, // Config 为 nil 视为 autostart
	}
	got := countAutostart(services)
	if got != 3 {
		t.Errorf("countAutostart = %d, want 3 (nil+true+nil-config)", got)
	}
}

// TestCountAutostart_Empty 验证空 map。
func TestCountAutostart_Empty(t *testing.T) {
	if got := countAutostart(nil); got != 0 {
		t.Errorf("countAutostart(nil) = %d, want 0", got)
	}
}

// TestIsPortZero 验证 :0 动态端口判定。
func TestIsPortZero(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{":0", true},
		{"0.0.0.0:0", true},
		{"[::]:0", true},
		{":7979", false},
		{"0.0.0.0:7979", false},
		{"", false}, // SplitHostPort 报错
		{"bad", false},
	}
	for _, c := range cases {
		if got := isPortZero(c.addr); got != c.want {
			t.Errorf("isPortZero(%q) = %v, want %v", c.addr, got, c.want)
		}
	}
}

// TestFormatURL 验证 IPv4/IPv6 URL 格式化。
func TestFormatURL(t *testing.T) {
	cases := []struct {
		ip   net.IP
		port string
		want string
	}{
		{net.ParseIP("127.0.0.1"), "7979", "http://127.0.0.1:7979"},
		{net.ParseIP("192.168.1.10"), "7979", "http://192.168.1.10:7979"},
		{net.ParseIP("::1"), "7979", "http://[::1]:7979"},
		{net.ParseIP("fe80::1"), "7979", "http://[fe80::1]:7979"},
	}
	for _, c := range cases {
		if got := formatURL(c.ip, c.port); got != c.want {
			t.Errorf("formatURL(%v, %q) = %q, want %q", c.ip, c.port, got, c.want)
		}
	}
}

// TestEnumerateAccessURLs_SpecificIP 绑定到具体 IP 仅返回该 IP。
func TestEnumerateAccessURLs_SpecificIP(t *testing.T) {
	urls := enumerateAccessURLs("127.0.0.1:7979")
	if len(urls) != 1 || urls[0] != "http://127.0.0.1:7979" {
		t.Errorf("enumerateAccessURLs(127.0.0.1:7979) = %v, want [http://127.0.0.1:7979]", urls)
	}

	urls = enumerateAccessURLs("192.168.1.10:7979")
	if len(urls) != 1 || urls[0] != "http://192.168.1.10:7979" {
		t.Errorf("enumerateAccessURLs(192.168.1.10:7979) = %v, want [http://192.168.1.10:7979]", urls)
	}
}

// TestEnumerateAccessURLs_BadInput 非法地址返回 nil。
func TestEnumerateAccessURLs_BadInput(t *testing.T) {
	if urls := enumerateAccessURLs("bad-no-port"); urls != nil {
		t.Errorf("enumerateAccessURLs(bad) = %v, want nil", urls)
	}
	if urls := enumerateAccessURLs(""); urls != nil {
		t.Errorf("enumerateAccessURLs(\"\") = %v, want nil", urls)
	}
}

// TestCollectURLs_WildcardIPv4 通配监听枚举所有 IPv4，loopback 排后，跳过 link-local。
func TestCollectURLs_WildcardIPv4(t *testing.T) {
	// mock 网卡：lo(127.0.0.1) + eth0(192.168.1.10, 169.254.0.1 link-local) + docker0(172.17.0.1) + down0(未UP)
	ifaces := []net.Interface{
		{Index: 1, Name: "lo", Flags: net.FlagUp | net.FlagLoopback},
		{Index: 2, Name: "eth0", Flags: net.FlagUp},
		{Index: 3, Name: "docker0", Flags: net.FlagUp},
		{Index: 4, Name: "down0", Flags: 0}, // 未 UP，应跳过
	}
	addrs := [][]net.Addr{
		{&net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(8, 32)}},
		{
			&net.IPNet{IP: net.ParseIP("192.168.1.10"), Mask: net.CIDRMask(24, 32)},
			&net.IPNet{IP: net.ParseIP("169.254.0.1"), Mask: net.CIDRMask(16, 32)}, // link-local，应跳过
		},
		{&net.IPNet{IP: net.ParseIP("172.17.0.1"), Mask: net.CIDRMask(16, 32)}},
		{&net.IPNet{IP: net.ParseIP("10.0.0.99"), Mask: net.CIDRMask(24, 32)}},
	}

	urls := collectURLs("7979", true, ifaces, addrs)
	// 期望顺序：eth0、docker0（非 loopback 排前），lo 排后；link-local 与 down 跳过
	want := []string{
		"http://192.168.1.10:7979",
		"http://172.17.0.1:7979",
		"http://127.0.0.1:7979",
	}
	if len(urls) != len(want) {
		t.Fatalf("collectURLs = %v, want %v", urls, want)
	}
	for i, u := range urls {
		if u != want[i] {
			t.Errorf("urls[%d] = %q, want %q", i, u, want[i])
		}
	}
}

// TestCollectURLs_NoInterfaces 空网卡列表返回 nil（不 panic）。
func TestCollectURLs_NoInterfaces(t *testing.T) {
	if urls := collectURLs("7979", true, []net.Interface{}, [][]net.Addr{}); urls != nil {
		t.Errorf("collectURLs(empty) = %v, want nil", urls)
	}
}

// TestCollectURLs_IPv6 验证 IPv6 通配枚举跳过 IPv4 与 link-local。
func TestCollectURLs_IPv6(t *testing.T) {
	ifaces := []net.Interface{
		{Index: 1, Name: "lo", Flags: net.FlagUp | net.FlagLoopback},
		{Index: 2, Name: "eth0", Flags: net.FlagUp},
	}
	addrs := [][]net.Addr{
		{&net.IPNet{IP: net.ParseIP("::1"), Mask: net.CIDRMask(128, 128)}},
		{
			&net.IPNet{IP: net.ParseIP("2001:db8::1"), Mask: net.CIDRMask(64, 128)},
			&net.IPNet{IP: net.ParseIP("fe80::1"), Mask: net.CIDRMask(64, 128)},           // link-local，跳过
			&net.IPNet{IP: net.ParseIP("192.168.1.10"), Mask: net.CIDRMask(24, 32)},       // IPv4，ipv4Only=false 时跳过
		},
	}

	urls := collectURLs("7979", false, ifaces, addrs)
	want := []string{
		"http://[2001:db8::1]:7979",
		"http://[::1]:7979",
	}
	if len(urls) != len(want) {
		t.Fatalf("collectURLs(ipv6) = %v, want %v", urls, want)
	}
	for i, u := range urls {
		if u != want[i] {
			t.Errorf("urls[%d] = %q, want %q", i, u, want[i])
		}
	}
}

// TestCollectURLs_Dedup 验证重复 IP 去重。
func TestCollectURLs_Dedup(t *testing.T) {
	ifaces := []net.Interface{
		{Index: 1, Name: "eth0", Flags: net.FlagUp},
		{Index: 2, Name: "eth1", Flags: net.FlagUp},
	}
	addrs := [][]net.Addr{
		{&net.IPNet{IP: net.ParseIP("192.168.1.10"), Mask: net.CIDRMask(24, 32)}},
		{&net.IPNet{IP: net.ParseIP("192.168.1.10"), Mask: net.CIDRMask(24, 32)}}, // 重复
	}
	urls := collectURLs("7979", true, ifaces, addrs)
	if len(urls) != 1 {
		t.Errorf("collectURLs 去重失败 = %v, want 1 个", urls)
	}
}

// TestBuildStartupSummary 验证 buildStartupSummary 正确填充字段（不依赖真实环境）。
func TestBuildStartupSummary(t *testing.T) {
	t.Setenv("SUPD_LOG_DIR", "")
	cfg := &config.Config{
		Settings: config.Settings{
			AuthMode:      "local_skip",
			AuthToken:     "secret-token",
			HTTPListen:    ":7979",
			LocalNetworks: []string{"127.0.0.0/8"},
		},
	}
	services := map[string]*watch.ServiceEntry{
		"svc1": {Config: &config.ServiceConfig{Name: "svc1", Autostart: nil}},
		"svc2": {Config: &config.ServiceConfig{Name: "svc2", Autostart: boolPtr(false)}},
	}
	globalExts := map[string]*watch.ExtensionEntry{
		"ext1": {Name: "ext1"},
		"ext2": {Name: "ext2"},
	}
	result := &watch.DiscoveryResult{
		Services:   services,
		GlobalExts: globalExts,
		Errors:     []watch.DiscoveryError{{Path: "/x", Message: "err"}},
	}

	s := buildStartupSummary("v1.0", "20260728", "/etc/supd", "/etc/supd/config.yaml", "/var/log/supd", true, cfg, result)

	if s.Version != "v1.0" || s.BuildTime != "20260728" {
		t.Errorf("version/buildTime 错误: %+v", s)
	}
	if s.AuthMode != "local_skip" || !s.TokenSet {
		t.Errorf("auth 字段错误: mode=%q tokenSet=%v", s.AuthMode, s.TokenSet)
	}
	if s.ServicesTotal != 2 || s.ServicesAutostart != 1 {
		t.Errorf("services 计数错误: total=%d autostart=%d", s.ServicesTotal, s.ServicesAutostart)
	}
	if s.ExtensionsTotal != 2 {
		t.Errorf("extensions 计数错误: %d", s.ExtensionsTotal)
	}
	if s.Warnings != 1 {
		t.Errorf("warnings 错误: %d", s.Warnings)
	}
	if s.PID1Mode != "disabled (--no-pid1)" {
		t.Errorf("PID1 模式错误（--no-pid1）: %q", s.PID1Mode)
	}
}

// TestBuildStartupSummary_NilConfig 验证 nil config 不 panic。
func TestBuildStartupSummary_NilConfig(t *testing.T) {
	s := buildStartupSummary("dev", "unknown", "/tmp", "/tmp/c", "/tmp/l", false, nil, nil)
	if s.AuthMode != "" || s.TokenSet {
		t.Errorf("nil config 应得到空 auth 字段: %+v", s)
	}
	if s.ServicesTotal != 0 {
		t.Errorf("nil result 应得到 0 计数: %+v", s)
	}
}
