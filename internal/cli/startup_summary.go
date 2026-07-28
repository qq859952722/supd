package cli

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"runtime"
	"strings"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/core"
	"github.com/supdorg/supd/internal/watch"
)

// startupSummary 汇总启动信息，供 formatStartupSummary/logStartupSummary 使用。
// 字段均来自已加载的配置与 Bootstrap 结果，不包含敏感值（auth_token 仅以布尔形式呈现）。
type startupSummary struct {
	Version           string
	BuildTime         string
	GoVersion         string
	GOOS              string
	GOArch            string
	PID               int
	WorkDir           string
	ConfigPath        string
	LogDir            string
	PID1Mode          string // "enabled" / "disabled (--no-pid1)" / "inactive (non-pid1)"
	AuthMode          string
	TokenSet          bool
	LocalNetworks     []string
	ConfiguredListen  string
	ServicesTotal     int
	ServicesAutostart int
	ExtensionsTotal   int
	Warnings          int
}

// formatStartupSummary 返回多行人类可读摘要（第一段，不含实际监听地址）。
// 纯格式化函数，无副作用，便于单元测试。
func formatStartupSummary(s startupSummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "supd %s (build %s, %s %s/%s)\n",
		s.Version, s.BuildTime, s.GoVersion, s.GOOS, s.GOArch)
	fmt.Fprintf(&b, "  pid:        %d\n", s.PID)
	fmt.Fprintf(&b, "  workdir:    %s\n", s.WorkDir)
	fmt.Fprintf(&b, "  config:     %s\n", s.ConfigPath)
	fmt.Fprintf(&b, "  log dir:    %s\n", s.LogDir)
	fmt.Fprintf(&b, "  pid1 mode:  %s\n", s.PID1Mode)
	fmt.Fprintf(&b, "  auth:       %s (token configured: %v)\n", s.AuthMode, s.TokenSet)
	listenDisplay := s.ConfiguredListen
	if listenDisplay == "" {
		listenDisplay = ":7979 (default)"
	}
	if isPortZero(s.ConfiguredListen) {
		listenDisplay += " (动态端口)"
	}
	fmt.Fprintf(&b, "  listen:     %s\n", listenDisplay)
	fmt.Fprintf(&b, "  services:   %d loaded, %d autostart\n", s.ServicesTotal, s.ServicesAutostart)
	fmt.Fprintf(&b, "  extensions: %d loaded\n", s.ExtensionsTotal)
	fmt.Fprintf(&b, "  warnings:   %d", s.Warnings)
	return b.String()
}

// formatListenSummary 返回第二段输出：实际监听地址 + 可访问 URL 列表。
// 纯格式化函数，便于单元测试。
func formatListenSummary(actualAddr string, accessURLs []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "实际监听: %s\n", actualAddr)
	if len(accessURLs) > 0 {
		b.WriteString("可访问地址:\n")
		for _, u := range accessURLs {
			fmt.Fprintf(&b, "  %s\n", u)
		}
		// 去掉末尾换行，与 formatStartupSummary 保持一致（调用方 infof 会补换行）
		s := b.String()
		return strings.TrimRight(s, "\n")
	}
	// 无可访问地址时，去掉末尾换行
	return strings.TrimRight(b.String(), "\n")
}

// logStartupSummary 以 slog 结构化形式记录等价信息（单行）。
func logStartupSummary(s startupSummary) {
	slog.Info("supd 启动摘要",
		"version", s.Version,
		"build_time", s.BuildTime,
		"go_version", s.GoVersion,
		"os_arch", s.GOOS+"/"+s.GOArch,
		"pid", s.PID,
		"workdir", s.WorkDir,
		"config", s.ConfigPath,
		"log_dir", s.LogDir,
		"pid1", s.PID1Mode,
		"auth_mode", s.AuthMode,
		"token_configured", s.TokenSet,
		"local_networks", s.LocalNetworks,
		"listen_configured", s.ConfiguredListen,
		"services_total", s.ServicesTotal,
		"services_autostart", s.ServicesAutostart,
		"extensions_total", s.ExtensionsTotal,
		"warnings", s.Warnings,
	)
}

// buildStartupSummary 从已加载的配置与 Bootstrap 结果构造摘要。
// 不读取任何敏感字段值（AuthToken 仅判 != ""）。
func buildStartupSummary(version, buildTime, workDir, cfgPath, logDir string, noPID1 bool, cfg *config.Config, result *watch.DiscoveryResult) startupSummary {
	s := startupSummary{
		Version:    version,
		BuildTime:  buildTime,
		GoVersion:  runtime.Version(),
		GOOS:       runtime.GOOS,
		GOArch:     runtime.GOARCH,
		PID:        os.Getpid(),
		WorkDir:    workDir,
		ConfigPath: cfgPath,
		LogDir:     logDir,
	}

	// PID1 模式判定（与 run.go:112-124 逻辑一致）
	if core.IsPID1() && !noPID1 {
		s.PID1Mode = "enabled (subreaper+zombie reaper)"
	} else if noPID1 {
		s.PID1Mode = "disabled (--no-pid1)"
	} else {
		s.PID1Mode = "inactive (non-pid1)"
	}

	if cfg != nil {
		s.AuthMode = cfg.Settings.AuthMode
		s.TokenSet = cfg.Settings.AuthToken != ""
		s.LocalNetworks = cfg.Settings.LocalNetworks
		s.ConfiguredListen = cfg.Settings.HTTPListen
	}

	if result != nil {
		s.ServicesTotal = len(result.Services)
		s.ServicesAutostart = countAutostart(result.Services)
		s.ExtensionsTotal = len(result.GlobalExts)
		s.Warnings = len(result.Errors)
	}

	return s
}

// countAutostart 统计 autostart=true（含 nil，与 core.isAutostart 语义一致）的服务数。
func countAutostart(services map[string]*watch.ServiceEntry) int {
	n := 0
	for _, svc := range services {
		if svc.Config == nil || svc.Config.Autostart == nil || *svc.Config.Autostart {
			n++
		}
	}
	return n
}

// isPortZero 判断配置的监听地址是否为 :0 形式（动态端口）。
func isPortZero(addr string) bool {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	return port == "0"
}

// enumerateAccessURLs 根据实际监听地址枚举本机可访问的 URL 列表。
// actualAddr 形如 "0.0.0.0:7979" / "[::]:7979" / "127.0.0.1:7979"。
// 返回值去重，loopback 排在最后（便于用户先看到 NAS 网段地址）。
// 网卡枚举失败时返回 nil，调用方应降级为仅打印实际监听地址。
func enumerateAccessURLs(actualAddr string) []string {
	host, port, err := net.SplitHostPort(actualAddr)
	if err != nil {
		return nil
	}
	// IPv6 地址 net.SplitHostPort 返回的 host 不含方括号
	ip := net.ParseIP(host)

	switch {
	case host == "" || ip == nil:
		// 理论上 net.Listen 后 Addr() 不会返回空 host，但防御性处理
		return listAllURLsFrom(port, true, nil)

	case ip.IsUnspecified():
		if ip.To4() != nil {
			// 0.0.0.0 → 仅 IPv4
			return listAllURLsFrom(port, true, nil)
		}
		// [::] IPv6 双栈：Linux 默认同时接受 IPv4 与 IPv6 连接，
		// 因此同时枚举 IPv4 与 IPv6 地址，方便用户用熟悉的 IPv4 访问。
		ipv4 := listAllURLsFrom(port, true, nil)
		ipv6 := listAllURLsFrom(port, false, nil)
		// 合并去重（loopback 已各自排后，IPv4 在前 IPv6 在后更符合用户习惯）
		seen := make(map[string]struct{}, len(ipv4)+len(ipv6))
		var out []string
		for _, u := range append(ipv4, ipv6...) {
			if _, dup := seen[u]; dup {
				continue
			}
			seen[u] = struct{}{}
			out = append(out, u)
		}
		return out

	default:
		// 绑定到具体 IP（loopback 或某网卡），仅返回该 IP
		return []string{formatURL(ip, port)}
	}
}

// listAllURLsFrom 枚举本机所有 UP 网卡的 IP，返回 URL 列表。
// ipv4Only=true 列 IPv4，false 列 IPv6。
// 若 ifaces 为 nil，调用 net.Interfaces() 获取真实网卡列表。
// 跳过 link-local 地址（IPv6 fe80::/10，URL 中需 zone index，家庭场景实用价值低）。
// loopback 排在最后。导出测试可通过传入 mock ifaces 避免依赖真实环境。
func listAllURLsFrom(port string, ipv4Only bool, ifaces []net.Interface) []string {
	if ifaces == nil {
		var err error
		ifaces, err = net.Interfaces()
		if err != nil {
			return nil
		}
	}
	addrs := make([][]net.Addr, len(ifaces))
	for i, iface := range ifaces {
		a, err := iface.Addrs()
		if err != nil {
			continue
		}
		addrs[i] = a
	}
	return collectURLs(port, ipv4Only, ifaces, addrs)
}

// collectURLs 是 listAllURLsFrom 的核心逻辑，接受注入的网卡地址列表，便于单元测试。
// addrs[i] 对应 ifaces[i] 的地址列表（Addrs() 返回值）。
func collectURLs(port string, ipv4Only bool, ifaces []net.Interface, addrs [][]net.Addr) []string {
	var loopback, others []string
	seen := make(map[string]struct{})

	for i, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if i >= len(addrs) {
			continue
		}
		for _, addr := range addrs[i] {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil {
				continue
			}
			// IPv4-mapped IPv6 (::ffff:1.2.3.4) 归一化到 IPv4
			if v4 := ip.To4(); v4 != nil {
				ip = v4
			}
			if ipv4Only && ip.To4() == nil {
				continue
			}
			if !ipv4Only && ip.To4() != nil {
				continue
			}
			// 跳过 link-local（fe80::/10、169.254.0.0/16）
			if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
				continue
			}
			u := formatURL(ip, port)
			if _, dup := seen[u]; dup {
				continue
			}
			seen[u] = struct{}{}
			if ip.IsLoopback() {
				loopback = append(loopback, u)
			} else {
				others = append(others, u)
			}
		}
	}

	return append(others, loopback...)
}

// formatURL 把 IP+port 格式化为 http URL。
// IPv4: http://127.0.0.1:7979
// IPv6: http://[::1]:7979
func formatURL(ip net.IP, port string) string {
	if ip.To4() != nil {
		return "http://" + ip.String() + ":" + port
	}
	return "http://[" + ip.String() + "]:" + port
}
