package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ProfileNamePattern 打包 profile 名称的合法字符规则：小写字母开头，仅含小写字母/数字/连字符。
// 对应文件名：package.<name>.yaml
var ProfileNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// PackageProfileFileName 构造 profile 文件名。
// name 为 "default" 时返回 "package.default.yaml"，其他同理。
func PackageProfileFileName(name string) string {
	return fmt.Sprintf("package.%s.yaml", name)
}

// LoadPackageProfile 从服务目录加载指定名称的打包 profile。
// 查找文件 package.<name>.yaml，解析为 PackageConfig。
// 文件不存在时返回 (nil, os.ErrNotExist)。
func LoadPackageProfile(svcDir, name string) (*PackageConfig, error) {
	if !ProfileNamePattern.MatchString(name) {
		return nil, fmt.Errorf("invalid profile name %q: must match %s", name, ProfileNamePattern.String())
	}

	path := filepath.Join(svcDir, PackageProfileFileName(name))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	pc := &PackageConfig{}
	if err := SafeUnmarshal(data, pc, DefaultSafeYAMLOptions); err != nil {
		return nil, fmt.Errorf("parse package profile %s: %w", path, err)
	}

	// 填充默认值
	if pc.Default == "" {
		pc.Default = "include"
	}

	return pc, nil
}

// ListPackageProfiles 扫描服务目录，返回所有可用的打包 profile 名称。
// 查找匹配 package.<name>.yaml 的文件，提取 <name> 部分，按字母序返回。
// 始终包含 "default"（即使文件不存在，因为默认导出使用内置规则）。
func ListPackageProfiles(svcDir string) ([]string, error) {
	entries, err := os.ReadDir(svcDir)
	if err != nil {
		return nil, err
	}

	profiles := []string{"default"}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// 匹配 package.<name>.yaml
		if !strings.HasPrefix(name, "package.") || !strings.HasSuffix(name, ".yaml") {
			continue
		}
		// 提取 <name>：去掉 "package." 前缀和 ".yaml" 后缀
		profileName := strings.TrimSuffix(strings.TrimPrefix(name, "package."), ".yaml")
		if !ProfileNamePattern.MatchString(profileName) {
			continue
		}
		// "default" 始终在列表首位，不重复添加
		if profileName != "default" {
			profiles = append(profiles, profileName)
		}
	}

	// 非 default 的 profile 按字母序排列（default 始终在最前）
	if len(profiles) > 1 {
		sort.Strings(profiles[1:])
	}

	return profiles, nil
}

// ResolveExportProfile 解析导出时实际使用的 profile。
//
// 优先级：
//  1. name != "default"：必须存在 package.<name>.yaml，否则返回错误
//  2. name == "default"：如果存在 package.default.yaml 则加载它
//  3. name == "default" 且文件不存在：返回 service.yaml 中的 package 配置（可为 nil）
//
// 返回 (profile, source) 其中 source 描述 profile 来源（"file:<name>" / "service.yaml" / "builtin"）。
func ResolveExportProfile(svcDir, name string, svcPackage *PackageConfig) (*PackageConfig, string, error) {
	if name == "" {
		name = "default"
	}

	if name != "default" {
		pc, err := LoadPackageProfile(svcDir, name)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, "", fmt.Errorf("profile %q not found: %w", name, err)
			}
			return nil, "", err
		}
		return pc, "file:" + name, nil
	}

	// default：尝试加载 package.default.yaml
	pc, err := LoadPackageProfile(svcDir, "default")
	if err == nil {
		return pc, "file:default", nil
	}
	if !os.IsNotExist(err) {
		return nil, "", err
	}

	// 文件不存在：回退到 service.yaml 中的 package 配置
	if svcPackage != nil {
		return svcPackage, "service.yaml", nil
	}

	// 最终回退：nil（由 PackDirWithProfile 使用内置默认）
	return nil, "builtin", nil
}
