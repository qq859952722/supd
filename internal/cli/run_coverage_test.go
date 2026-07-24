package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/core"
	"github.com/supdorg/supd/internal/watch"
)

// TestValidateLogDir_Success 测试 validateLogDir 在可写目录时成功（K-04-001 修复路径）
func TestValidateLogDir_Success(t *testing.T) {
	dir := t.TempDir()
	if err := validateLogDir(dir); err != nil {
		t.Errorf("validateLogDir(%q) 应成功，实际: %v", dir, err)
	}
	// 验证测试写入文件已被清理
	if _, err := os.Stat(filepath.Join(dir, ".supd_writable_test")); !os.IsNotExist(err) {
		t.Error("可写性测试文件应已被删除")
	}
}

// TestValidateLogDir_MkdirError 测试父路径为文件时 MkdirAll 失败的错误路径
func TestValidateLogDir_MkdirError(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(parent, []byte("x"), 0644); err != nil {
		t.Fatalf("创建父文件失败: %v", err)
	}
	// 以文件作为父级，子目录无法创建
	if err := validateLogDir(filepath.Join(parent, "sub", "log")); err == nil {
		t.Error("父路径为文件时 validateLogDir 应返回错误")
	}
}

// TestBuildStopConfigs_Empty 测试空 Services 返回空映射
// 注：生产路径中 result.Discovery 总由 bootstrap.Run() 赋值，非 nil；
// 此处仅覆盖 Discovery 已初始化但无服务的分支。
func TestBuildStopConfigs_Empty(t *testing.T) {
	resNil := &core.BootstrapResult{Discovery: &watch.DiscoveryResult{}}
	cfg := buildStopConfigs(resNil)
	if len(cfg) != 0 {
		t.Errorf("空 Services 应返回空映射，实际长度 %d", len(cfg))
	}
}

// TestBuildStopConfigs_Defaults 测试无 Stop 配置时回退默认 10s/60s
func TestBuildStopConfigs_Defaults(t *testing.T) {
	res := &core.BootstrapResult{
		Discovery: &watch.DiscoveryResult{
			Services: map[string]*watch.ServiceEntry{
				"noConfig": {Name: "noConfig", Config: nil},
				"emptyStop": {Name: "emptyStop", Config: &config.ServiceConfig{Stop: nil}},
				"zeroStop":  {Name: "zeroStop", Config: &config.ServiceConfig{Stop: &config.StopConfig{}}},
			},
		},
	}
	cfg := buildStopConfigs(res)
	for _, name := range []string{"noConfig", "emptyStop", "zeroStop"} {
		assertStopConfig(t, name, cfg[name], 10, 60)
	}
}

// TestBuildStopConfigs_Custom 测试自定义 grace/timeout 被采用
func TestBuildStopConfigs_Custom(t *testing.T) {
	res := &core.BootstrapResult{
		Discovery: &watch.DiscoveryResult{
			Services: map[string]*watch.ServiceEntry{
				"custom": {
					Name: "custom",
					Config: &config.ServiceConfig{
						Stop: &config.StopConfig{GraceSeconds: 5, TimeoutSeconds: 30},
					},
				},
				"partialGrace": {
					Name: "partialGrace",
					Config: &config.ServiceConfig{
						Stop: &config.StopConfig{GraceSeconds: 0, TimeoutSeconds: 45},
					},
				},
			},
		},
	}
	cfg := buildStopConfigs(res)
	assertStopConfig(t, "custom", cfg["custom"], 5, 30)
	// grace=0 应回退默认 10，timeout=45 保留
	assertStopConfig(t, "partialGrace", cfg["partialGrace"], 10, 45)
}

func assertStopConfig(t *testing.T, name string, sc core.StopConfig, grace, timeout int) {
	t.Helper()
	if sc.GraceSeconds != grace {
		t.Errorf("%s: GraceSeconds = %d, want %d", name, sc.GraceSeconds, grace)
	}
	if sc.TimeoutSeconds != timeout {
		t.Errorf("%s: TimeoutSeconds = %d, want %d", name, sc.TimeoutSeconds, timeout)
	}
}
