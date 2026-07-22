package core

import (
	"os"
	"path/filepath"
	"testing"

	"go.yaml.in/yaml/v4"

	"github.com/supdorg/supd/internal/config"
)

// writeEnvFile 写入 env.yaml 文件（结构：env: {KEY: {value, enabled, hint}}）
func writeEnvFile(t *testing.T, path string, vars map[string]config.EnvVar) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	ef := &config.EnvFile{Env: vars}
	data, err := yaml.Marshal(ef)
	if err != nil {
		t.Fatalf("marshal env: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// envToMap 将 "K=V" 切片转为 map（后者覆盖前者），便于断言
func envToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if k, v, ok := cutEnv(kv); ok {
			m[k] = v
		}
	}
	return m
}

func cutEnv(kv string) (string, string, bool) {
	for i, c := range kv {
		if c == '=' {
			return kv[:i], kv[i+1:], true
		}
	}
	return "", "", false
}

// 验证：无 env.yaml 时返回 os.Environ()（仅 supd 自身环境变量）
func TestBuildServiceProcessEnv_NoEnvFiles(t *testing.T) {
	tmp := t.TempDir()
	// 不创建任何 env.yaml
	env := BuildServiceProcessEnv(tmp, "svc1", nil)

	// 应该等于 os.Environ()
	osEnv := os.Environ()
	if len(env) != len(osEnv) {
		t.Errorf("len(env) = %d, want %d (os.Environ)", len(env), len(osEnv))
	}
}

// 验证：服务 env.yaml 中的变量被注入到子进程环境
func TestBuildServiceProcessEnv_ServiceEnvInjected(t *testing.T) {
	tmp := t.TempDir()
	// 创建服务 env.yaml
	writeEnvFile(t, filepath.Join(tmp, "services", "myapp", "env.yaml"), map[string]config.EnvVar{
		"MYAPP_PORT": {Value: "8080"},
		"MYAPP_MODE": {Value: "production"},
	})

	env := BuildServiceProcessEnv(tmp, "myapp", nil)
	m := envToMap(env)

	if m["MYAPP_PORT"] != "8080" {
		t.Errorf("MYAPP_PORT = %q, want 8080", m["MYAPP_PORT"])
	}
	if m["MYAPP_MODE"] != "production" {
		t.Errorf("MYAPP_MODE = %q, want production", m["MYAPP_MODE"])
	}
}

// 验证：全局 env 文件 + 服务 env 合并，服务层覆盖全局层
func TestBuildServiceProcessEnv_GlobalAndServiceMerge(t *testing.T) {
	tmp := t.TempDir()
	// 全局 env.yaml
	writeEnvFile(t, filepath.Join(tmp, "env", "00-base.yaml"), map[string]config.EnvVar{
		"GLOBAL_VAR":   {Value: "from-global"},
		"OVERRIDE_VAR": {Value: "global-value"},
	})
	// 服务 env.yaml
	writeEnvFile(t, filepath.Join(tmp, "services", "svc", "env.yaml"), map[string]config.EnvVar{
		"SERVICE_VAR":  {Value: "from-service"},
		"OVERRIDE_VAR": {Value: "service-value"}, // 覆盖全局
	})

	env := BuildServiceProcessEnv(tmp, "svc", []string{"env/00-base.yaml"})
	m := envToMap(env)

	if m["GLOBAL_VAR"] != "from-global" {
		t.Errorf("GLOBAL_VAR = %q, want from-global", m["GLOBAL_VAR"])
	}
	if m["SERVICE_VAR"] != "from-service" {
		t.Errorf("SERVICE_VAR = %q, want from-service", m["SERVICE_VAR"])
	}
	// 服务层应覆盖全局层
	if m["OVERRIDE_VAR"] != "service-value" {
		t.Errorf("OVERRIDE_VAR = %q, want service-value (service should override global)", m["OVERRIDE_VAR"])
	}
}

// 验证：enabled:false 的变量不注入
func TestBuildServiceProcessEnv_DisabledNotInjected(t *testing.T) {
	tmp := t.TempDir()
	disabled := false
	writeEnvFile(t, filepath.Join(tmp, "services", "svc", "env.yaml"), map[string]config.EnvVar{
		"ENABLED_VAR":   {Value: "yes"},
		"DISABLED_VAR":  {Value: "no", Enabled: &disabled},
	})

	env := BuildServiceProcessEnv(tmp, "svc", nil)
	m := envToMap(env)

	if m["ENABLED_VAR"] != "yes" {
		t.Errorf("ENABLED_VAR = %q, want yes", m["ENABLED_VAR"])
	}
	if _, exists := m["DISABLED_VAR"]; exists {
		t.Errorf("DISABLED_VAR should not be injected (enabled:false), got %q", m["DISABLED_VAR"])
	}
}

// 验证：env.yaml 中的变量覆盖 os.Environ() 中的同名变量
func TestBuildServiceProcessEnv_OverridesOSEnviron(t *testing.T) {
	tmp := t.TempDir()
	// 在 os.Environ() 中设置一个变量
	t.Setenv("TEST_OVERRIDE_VAR", "from-os")

	writeEnvFile(t, filepath.Join(tmp, "services", "svc", "env.yaml"), map[string]config.EnvVar{
		"TEST_OVERRIDE_VAR": {Value: "from-env-yaml"},
	})

	env := BuildServiceProcessEnv(tmp, "svc", nil)
	m := envToMap(env)

	if m["TEST_OVERRIDE_VAR"] != "from-env-yaml" {
		t.Errorf("TEST_OVERRIDE_VAR = %q, want from-env-yaml (env.yaml should override os.Environ)", m["TEST_OVERRIDE_VAR"])
	}
}

// 验证：env.yaml 文件不存在时静默跳过（不报错）
func TestBuildServiceProcessEnv_MissingFilesSkipped(t *testing.T) {
	tmp := t.TempDir()
	// 引用不存在的全局 env 文件 + 不存在的服务 env
	env := BuildServiceProcessEnv(tmp, "nonexistent-svc", []string{"env/missing.yaml"})

	// 应该等于 os.Environ()，不报错
	osEnv := os.Environ()
	if len(env) != len(osEnv) {
		t.Errorf("len(env) = %d, want %d (missing files should be silently skipped)", len(env), len(osEnv))
	}
}

// 验证：多个全局 env 文件按顺序加载，后者覆盖前者
func TestBuildServiceProcessEnv_MultipleGlobalEnvFiles(t *testing.T) {
	tmp := t.TempDir()
	// 00-base.yaml
	writeEnvFile(t, filepath.Join(tmp, "env", "00-base.yaml"), map[string]config.EnvVar{
		"VAR": {Value: "from-00"},
	})
	// 99-override.yaml
	writeEnvFile(t, filepath.Join(tmp, "env", "99-override.yaml"), map[string]config.EnvVar{
		"VAR": {Value: "from-99"}, // 覆盖 00-base
	})

	env := BuildServiceProcessEnv(tmp, "svc", []string{"env/00-base.yaml", "env/99-override.yaml"})
	m := envToMap(env)

	if m["VAR"] != "from-99" {
		t.Errorf("VAR = %q, want from-99 (later file should override earlier)", m["VAR"])
	}
}
