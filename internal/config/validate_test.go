package config

import (
	"strings"
	"testing"
)

// newValidTestConfig 创建一个通过基础校验的 Config（auth_token 等已设置）
func newValidTestConfig() *Config {
	cfg := &Config{}
	SetDefaults(cfg)
	cfg.Settings.AuthToken = "test-token-for-validation"
	return cfg
}

// TestValidateLocalNetworksValid 校验合法的 CIDR 通过校验
// K-04-002 修复：local_networks CIDR 格式校验
func TestValidateLocalNetworksValid(t *testing.T) {
	tests := []struct {
		name    string
		network string
	}{
		{"ipv4_private_8", "10.0.0.0/8"},
		{"ipv4_private_16", "192.168.0.0/16"},
		{"ipv4_private_12", "172.16.0.0/12"},
		{"ipv4_loopback", "127.0.0.0/8"},
		{"ipv4_full", "0.0.0.0/0"},
		{"ipv4_32", "192.168.1.1/32"},
		{"ipv6_local", "fe80::/10"},
		{"ipv6_full", "::/0"},
		{"ipv6_128", "::1/128"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newValidTestConfig()
			cfg.Settings.LocalNetworks = []string{tt.network}
			if err := ValidateConfig(cfg); err != nil {
				t.Errorf("expected %q to be valid CIDR, got error: %v", tt.network, err)
			}
		})
	}
}

// TestValidateLocalNetworksInvalid 校验非法的 CIDR 被拒绝
func TestValidateLocalNetworksInvalid(t *testing.T) {
	tests := []struct {
		name    string
		network string
	}{
		{"empty", ""},
		{"plain_ip", "192.168.1.1"},
		{"missing_mask", "10.0.0.0"},
		{"bad_mask", "10.0.0.0/33"},
		{"bad_ip", "999.168.1.0/24"},
		{"non_numeric", "abc.def/24"},
		{"ipv6_bad_mask", "fe80::/200"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newValidTestConfig()
			cfg.Settings.LocalNetworks = []string{tt.network}
			err := ValidateConfig(cfg)
			if err == nil {
				t.Errorf("expected error for invalid CIDR %q, got nil", tt.network)
				return
			}
			// 错误消息应来自 local_networks 校验，而非其它字段
			if !strings.Contains(err.Error(), "local_networks") {
				t.Errorf("error should mention local_networks, got: %v", err)
			}
		})
	}
}

// TestValidateLocalNetworksMultipleMixed 校验多个 CIDR 中有一个非法时整体校验失败
func TestValidateLocalNetworksMultipleMixed(t *testing.T) {
	cfg := newValidTestConfig()
	cfg.Settings.LocalNetworks = []string{"10.0.0.0/8", "invalid-cidr", "192.168.0.0/16"}
	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error when one of multiple CIDRs is invalid")
	}
	if !strings.Contains(err.Error(), "local_networks[1]") {
		t.Errorf("error should reference index [1], got: %v", err)
	}
}

// TestValidateLocalNetworksEmptyList 校验空列表通过校验（local_networks 是可选的）
func TestValidateLocalNetworksEmptyList(t *testing.T) {
	cfg := newValidTestConfig()
	cfg.Settings.LocalNetworks = nil
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("expected nil LocalNetworks to be valid, got error: %v", err)
	}
	cfg.Settings.LocalNetworks = []string{}
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("expected empty LocalNetworks slice to be valid, got error: %v", err)
	}
}

// TestValidateLocalNetworksErrorContainsIndex 校验错误消息包含索引位置
func TestValidateLocalNetworksErrorContainsIndex(t *testing.T) {
	cfg := newValidTestConfig()
	cfg.Settings.LocalNetworks = []string{"10.0.0.0/8", "192.168.1.1", "172.16.0.0/12"}
	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// 错误消息应包含索引 1（第二个元素非法）
	if !strings.Contains(err.Error(), "local_networks[1]") {
		t.Errorf("error message should contain index [1], got: %v", err)
	}
}

// TestValidateLogDirBootstrap 校验 log_dir 校验逻辑本身的可达性
// 注意：实际启动期校验逻辑在 cli/run.go，这里仅测试 ValidateConfig 不影响 log_dir
func TestValidateLogDirNoConfigField(t *testing.T) {
	cfg := newValidTestConfig()
	// Settings 结构中无 log_dir 字段（来自 SUPD_LOG_DIR 环境变量），校验应通过
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("expected config without log_dir field to be valid, got: %v", err)
	}
}

