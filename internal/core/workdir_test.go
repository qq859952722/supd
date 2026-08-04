package core

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/watch"
)

// buildSvcEntry 构造测试用 ServiceEntry，ConfigPath 决定服务根目录
func buildSvcEntry(configPath string) *watch.ServiceEntry {
	return &watch.ServiceEntry{
		ConfigPath: configPath,
		Config:    &config.ServiceConfig{},
	}
}

func TestResolveWorkdir_EmptyReturnsServiceRoot(t *testing.T) {
	// 空 workdir → 返回 service.yaml 所在目录（服务根目录）
	svcEntry := buildSvcEntry("/etc/supd/services/foo/service.yaml")
	svcConfig := &config.ServiceConfig{Workdir: ""}

	got := ResolveWorkdir(svcConfig, svcEntry)
	want := "/etc/supd/services/foo"
	if got != want {
		t.Errorf("empty workdir: got %q, want %q", got, want)
	}
}

func TestResolveWorkdir_AbsolutePathCleaned(t *testing.T) {
	t.Run("plain absolute", func(t *testing.T) {
		svcEntry := buildSvcEntry("/etc/supd/services/foo/service.yaml")
		svcConfig := &config.ServiceConfig{Workdir: "/var/lib/foo"}

		got := ResolveWorkdir(svcConfig, svcEntry)
		if got != "/var/lib/foo" {
			t.Errorf("absolute path: got %q, want /var/lib/foo", got)
		}
	})

	t.Run("absolute with redundant segments", func(t *testing.T) {
		svcEntry := buildSvcEntry("/etc/supd/services/foo/service.yaml")
		svcConfig := &config.ServiceConfig{Workdir: "/var/lib/./foo/../bar"}

		got := ResolveWorkdir(svcConfig, svcEntry)
		want := "/var/lib/bar"
		if got != want {
			t.Errorf("absolute path with redundant segments: got %q, want %q", got, want)
		}
	})
}

func TestResolveWorkdir_RelativePathJoinedToRoot(t *testing.T) {
	svcEntry := buildSvcEntry("/etc/supd/services/foo/service.yaml")

	cases := []struct {
		name    string
		workdir string
		want    string
	}{
		{"simple relative", "config", "/etc/supd/services/foo/config"},
		{"relative with dot slash", "./config", "/etc/supd/services/foo/config"},
		{"relative trailing dot", "config/.", "/etc/supd/services/foo/config"},
		{"nested relative", "data/sub/dir", "/etc/supd/services/foo/data/sub/dir"},
		{"single dot equals root", ".", "/etc/supd/services/foo"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svcConfig := &config.ServiceConfig{Workdir: tc.workdir}
			got := ResolveWorkdir(svcConfig, svcEntry)
			if got != tc.want {
				t.Errorf("workdir=%q: got %q, want %q", tc.workdir, got, tc.want)
			}
		})
	}
}

func TestResolveWorkdir_ParentEscapeAllowed(t *testing.T) {
	// 相对路径含 ".." 允许逃逸到服务根目录之外
	// 路径穿越安全性由文件操作 API 的 baseDir 边界约束，不由 ResolveWorkdir 拦截
	svcEntry := buildSvcEntry("/etc/supd/services/foo/service.yaml")
	svcConfig := &config.ServiceConfig{Workdir: "../sibling"}

	got := ResolveWorkdir(svcConfig, svcEntry)
	want := "/etc/supd/services/sibling"
	if got != want {
		t.Errorf("parent escape: got %q, want %q", got, want)
	}
}

func TestResolveWorkdir_RelativeCleanedProperly(t *testing.T) {
	// 多段 .. 应正确 Clean
	svcEntry := buildSvcEntry("/etc/supd/services/foo/service.yaml")
	svcConfig := &config.ServiceConfig{Workdir: "../../bar/baz"}

	got := ResolveWorkdir(svcConfig, svcEntry)
	want := "/etc/supd/bar/baz"
	if got != want {
		t.Errorf("multi-dot relative: got %q, want %q", got, want)
	}
}

func TestResolveWorkdir_ConsistencyWithOldLogic(t *testing.T) {
	// 验证新 helper 与原 4 处内联逻辑行为等价
	// 原逻辑：workdir == "" → filepath.Dir(ConfigPath)；否则原样使用
	// 新逻辑：workdir == "" → filepath.Dir(ConfigPath)；绝对 → Clean；相对 → Join+Clean
	// 关键不变量：空 workdir 和绝对路径 workdir 行为完全一致（相对路径是新增能力）

	svcEntry := buildSvcEntry("/etc/supd/services/foo/service.yaml")

	// 空 workdir：原 = filepath.Dir(ConfigPath) = 新
	svcConfig := &config.ServiceConfig{Workdir: ""}
	oldLogic := filepath.Dir(svcEntry.ConfigPath)
	newLogic := ResolveWorkdir(svcConfig, svcEntry)
	if oldLogic != newLogic {
		t.Errorf("empty workdir regression: old=%q new=%q", oldLogic, newLogic)
	}

	// 绝对路径 workdir：原 = workdir（原样）= 新（Clean 后，但已 Clean 的路径不变）
	absWorkdir := "/var/lib/foo"
	svcConfig.Workdir = absWorkdir
	oldLogic = absWorkdir
	newLogic = ResolveWorkdir(svcConfig, svcEntry)
	if oldLogic != newLogic {
		t.Errorf("absolute workdir regression: old=%q new=%q", oldLogic, newLogic)
	}
}

func TestResolveWorkdir_RealisticAdguardHomeScenario(t *testing.T) {
	// 模拟 adguardhome 应用 workdir: . 的真实场景
	// service.yaml 在 /etc/supd/services/adguardhome/service.yaml
	// workdir: . → ResolveWorkdir 应返回 /etc/supd/services/adguardhome
	svcEntry := buildSvcEntry("/etc/supd/services/adguardhome/service.yaml")
	svcConfig := &config.ServiceConfig{Workdir: "."}

	got := ResolveWorkdir(svcConfig, svcEntry)
	want := "/etc/supd/services/adguardhome"
	if got != want {
		t.Errorf("adguardhome scenario: got %q, want %q", got, want)
	}

	// 验证返回值是绝对路径（exec.Cmd.Dir 需要绝对路径，相对路径会以 supd CWD 解析导致不确定行为）
	if !filepath.IsAbs(got) {
		t.Errorf("ResolveWorkdir must return absolute path, got relative %q", got)
	}

	// 验证不含 .. 残留（已 Clean）
	if strings.Contains(got, "..") {
		t.Errorf("ResolveWorkdir result should be cleaned, got %q containing ..", got)
	}
}
