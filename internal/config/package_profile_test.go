package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPackageProfile(t *testing.T) {
	svcDir := t.TempDir()

	// 创建一个 profile 文件
	profileContent := []byte("include:\n  - bin/\n  - extensions/\nexclude: []\ndefault: exclude\n")
	if err := os.WriteFile(filepath.Join(svcDir, "package.share.yaml"), profileContent, 0644); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	pc, err := LoadPackageProfile(svcDir, "share")
	if err != nil {
		t.Fatalf("LoadPackageProfile: %v", err)
	}
	if pc.Default != "exclude" {
		t.Errorf("default = %q, want %q", pc.Default, "exclude")
	}
	if len(pc.Include) != 2 {
		t.Errorf("include length = %d, want 2", len(pc.Include))
	}
}

func TestLoadPackageProfileNotFound(t *testing.T) {
	svcDir := t.TempDir()
	_, err := LoadPackageProfile(svcDir, "nonexistent")
	if !os.IsNotExist(err) {
		t.Errorf("expected os.ErrNotExist, got %v", err)
	}
}

func TestLoadPackageProfileInvalidName(t *testing.T) {
	svcDir := t.TempDir()
	_, err := LoadPackageProfile(svcDir, "../etc/passwd")
	if err == nil {
		t.Error("expected error for path traversal attempt, got nil")
	}
}

func TestListPackageProfiles(t *testing.T) {
	svcDir := t.TempDir()

	// 创建几个 profile 文件
	for _, name := range []string{"package.default.yaml", "package.migrate.yaml", "package.share.yaml"} {
		if err := os.WriteFile(filepath.Join(svcDir, name), []byte("default: include\n"), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	// 创建一个不合法名称的文件（应被跳过）
	if err := os.WriteFile(filepath.Join(svcDir, "package.UPPER.yaml"), []byte("default: include\n"), 0644); err != nil {
		t.Fatalf("write UPPER: %v", err)
	}

	profiles, err := ListPackageProfiles(svcDir)
	if err != nil {
		t.Fatalf("ListPackageProfiles: %v", err)
	}

	// 应包含 default, migrate, share（不含 UPPER）
	want := []string{"default", "migrate", "share"}
	if len(profiles) != len(want) {
		t.Fatalf("profiles = %v, want %v", profiles, want)
	}
	for i, p := range profiles {
		if p != want[i] {
			t.Errorf("profiles[%d] = %q, want %q", i, p, want[i])
		}
	}
}

func TestListPackageProfilesEmpty(t *testing.T) {
	svcDir := t.TempDir()
	profiles, err := ListPackageProfiles(svcDir)
	if err != nil {
		t.Fatalf("ListPackageProfiles: %v", err)
	}
	if len(profiles) != 1 || profiles[0] != "default" {
		t.Errorf("profiles = %v, want [default]", profiles)
	}
}

func TestResolveExportProfileDefaultFallback(t *testing.T) {
	svcDir := t.TempDir()

	// 无文件、无 service.yaml package 配置：应返回 nil + "builtin"
	pc, source, err := ResolveExportProfile(svcDir, "default", nil)
	if err != nil {
		t.Fatalf("ResolveExportProfile: %v", err)
	}
	if pc != nil {
		t.Errorf("expected nil profile for builtin fallback, got %v", pc)
	}
	if source != "builtin" {
		t.Errorf("source = %q, want %q", source, "builtin")
	}
}

func TestResolveExportProfileNamedMustExist(t *testing.T) {
	svcDir := t.TempDir()
	_, _, err := ResolveExportProfile(svcDir, "nonexistent", nil)
	if err == nil {
		t.Error("expected error for nonexistent named profile, got nil")
	}
}
