package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvSingleFile(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, "env.yaml")

	content := `
env:
  DATABASE_URL:
    value: "postgres://localhost:5432/mydb"
    hint: "数据库连接地址"
  APP_PORT:
    value: "8080"
    enabled: true
  DEBUG_MODE:
    value: "true"
    enabled: false
    hint: "调试模式"
`
	if err := os.WriteFile(envPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	ef, err := LoadEnv(envPath)
	if err != nil {
		t.Fatalf("LoadEnv failed: %v", err)
	}

	if len(ef.Env) != 3 {
		t.Fatalf("len(Env) = %d, want 3", len(ef.Env))
	}

	dbURL, ok := ef.Env["DATABASE_URL"]
	if !ok {
		t.Fatal("DATABASE_URL not found")
	}
	if dbURL.Value != "postgres://localhost:5432/mydb" {
		t.Errorf("DATABASE_URL.Value = %q, want postgres://localhost:5432/mydb", dbURL.Value)
	}
	if dbURL.Hint != "数据库连接地址" {
		t.Errorf("DATABASE_URL.Hint = %q, want 数据库连接地址", dbURL.Hint)
	}

	appPort, ok := ef.Env["APP_PORT"]
	if !ok {
		t.Fatal("APP_PORT not found")
	}
	if appPort.Value != "8080" {
		t.Errorf("APP_PORT.Value = %q, want 8080", appPort.Value)
	}

	debugMode, ok := ef.Env["DEBUG_MODE"]
	if !ok {
		t.Fatal("DEBUG_MODE not found")
	}
	if debugMode.Value != "true" {
		t.Errorf("DEBUG_MODE.Value = %q, want true", debugMode.Value)
	}
}

func TestLoadEnvDefaultEnabledTrue(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, "env.yaml")

	// enabled 字段省略，应默认为 true
	content := `
env:
  MY_VAR:
    value: "hello"
`
	if err := os.WriteFile(envPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	ef, err := LoadEnv(envPath)
	if err != nil {
		t.Fatalf("LoadEnv failed: %v", err)
	}

	v := ef.Env["MY_VAR"]
	if v.Enabled != nil {
		t.Errorf("Enabled = %v, want nil (default true)", *v.Enabled)
	}
	if !v.IsEnabled() {
		t.Error("IsEnabled() = false, want true (default)")
	}
}

func TestLoadEnvEnabledFalse(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, "env.yaml")

	content := `
env:
  DISABLED_VAR:
    value: "secret"
    enabled: false
`
	if err := os.WriteFile(envPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	ef, err := LoadEnv(envPath)
	if err != nil {
		t.Fatalf("LoadEnv failed: %v", err)
	}

	v := ef.Env["DISABLED_VAR"]
	if v.Enabled == nil || *v.Enabled != false {
		t.Error("Enabled should be false")
	}
	if v.IsEnabled() {
		t.Error("IsEnabled() = true, want false")
	}
	// value 保留在 env.yaml 中
	if v.Value != "secret" {
		t.Errorf("Value = %q, want secret (should be preserved)", v.Value)
	}
}

func TestLoadEnvEmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, "env.yaml")

	// 空文件
	if err := os.WriteFile(envPath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	ef, err := LoadEnv(envPath)
	if err != nil {
		t.Fatalf("LoadEnv failed: %v", err)
	}

	if ef.Env == nil {
		t.Error("Env should not be nil after LoadEnv")
	}
	if len(ef.Env) != 0 {
		t.Errorf("len(Env) = %d, want 0", len(ef.Env))
	}
}

func TestLoadEnvNonexistent(t *testing.T) {
	_, err := LoadEnv("/nonexistent/env.yaml")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestMergeEnvFourLayers(t *testing.T) {
	// 层1: 全局 env
	layer1 := &EnvFile{
		Env: map[string]EnvVar{
			"DB_HOST":   {Value: "global-host"},
			"DB_PORT":   {Value: "5432"},
			"APP_NAME":  {Value: "global-app"},
			"SHARED_VAR": {Value: "from-layer1"},
		},
	}

	// 层2: 全局扩展私有 env
	layer2 := &EnvFile{
		Env: map[string]EnvVar{
			"DB_HOST":  {Value: "ext-global-host"},
			"EXT_OPT":  {Value: "enabled-by-ext"},
		},
	}

	// 层3: 服务 env
	layer3 := &EnvFile{
		Env: map[string]EnvVar{
			"DB_HOST":   {Value: "service-host"},
			"SHARED_VAR": {Value: "from-layer3"},
			"LOG_LEVEL": {Value: "debug"},
		},
	}

	// 层4: 服务级扩展私有 env
	disabled := false
	layer4 := &EnvFile{
		Env: map[string]EnvVar{
			"DB_HOST": {Value: "svc-ext-host"},
			"LOG_LEVEL": {Value: "info", Enabled: &disabled},
		},
	}

	merged := MergeEnv(layer1, layer2, layer3, layer4)

	// 层4 覆盖层3 覆盖层2 覆盖层1
	if merged["DB_HOST"].Value != "svc-ext-host" {
		t.Errorf("DB_HOST = %q, want svc-ext-host", merged["DB_HOST"].Value)
	}
	if merged["DB_PORT"].Value != "5432" {
		t.Errorf("DB_PORT = %q, want 5432", merged["DB_PORT"].Value)
	}
	if merged["APP_NAME"].Value != "global-app" {
		t.Errorf("APP_NAME = %q, want global-app", merged["APP_NAME"].Value)
	}
	if merged["SHARED_VAR"].Value != "from-layer3" {
		t.Errorf("SHARED_VAR = %q, want from-layer3", merged["SHARED_VAR"].Value)
	}
	if merged["EXT_OPT"].Value != "enabled-by-ext" {
		t.Errorf("EXT_OPT = %q, want enabled-by-ext", merged["EXT_OPT"].Value)
	}
	// LOG_LEVEL 被层4覆盖为 enabled=false
	if merged["LOG_LEVEL"].Value != "info" {
		t.Errorf("LOG_LEVEL.Value = %q, want info", merged["LOG_LEVEL"].Value)
	}
	if merged["LOG_LEVEL"].IsEnabled() {
		t.Error("LOG_LEVEL should be disabled after layer4 override")
	}
}

func TestMergeEnvNilLayers(t *testing.T) {
	layer := &EnvFile{
		Env: map[string]EnvVar{
			"VAR": {Value: "val"},
		},
	}

	merged := MergeEnv(nil, layer, nil)
	if merged["VAR"].Value != "val" {
		t.Errorf("VAR = %q, want val", merged["VAR"].Value)
	}
}

func TestMergeEnvEmpty(t *testing.T) {
	merged := MergeEnv()
	if len(merged) != 0 {
		t.Errorf("len(merged) = %d, want 0", len(merged))
	}
}

func TestToInjectEnvFiltersDisabled(t *testing.T) {
	enabled := true
	disabled := false

	merged := map[string]EnvVar{
		"VAR_ENABLED":   {Value: "yes", Enabled: &enabled},
		"VAR_DISABLED":  {Value: "no", Enabled: &disabled},
		"VAR_DEFAULT":   {Value: "default", Enabled: nil}, // nil = true
	}

	injected := ToInjectEnv(merged)

	if _, ok := injected["VAR_ENABLED"]; !ok {
		t.Error("VAR_ENABLED should be injected")
	}
	if injected["VAR_ENABLED"] != "yes" {
		t.Errorf("VAR_ENABLED = %q, want yes", injected["VAR_ENABLED"])
	}
	if _, ok := injected["VAR_DISABLED"]; ok {
		t.Error("VAR_DISABLED should NOT be injected (enabled=false)")
	}
	if _, ok := injected["VAR_DEFAULT"]; !ok {
		t.Error("VAR_DEFAULT should be injected (default enabled=true)")
	}
}

func TestIsSensitive(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"DB_PASSWORD", true},
		{"MYSQL_PWD", true},
		{"API_SECRET", true},
		{"AUTH_TOKEN", true},
		{"PRIVATE_KEY", true},
		{"APP_PASSWORD_EXTRA", true},
		{"DATABASE_URL", false},
		{"APP_NAME", false},
		{"HOST", false},
		{"PORT", false},
		{"password", true},   // case insensitive
		{"MySecret", true},   // case insensitive
		{"accessToken", true}, // contains TOKEN -> true
	}

	for _, tc := range tests {
		result := IsSensitive(tc.name)
		if result != tc.expected {
			t.Errorf("IsSensitive(%q) = %v, want %v", tc.name, result, tc.expected)
		}
	}
}

func TestMergeEnvEnabledOverride(t *testing.T) {
	// 层1: VAR 被 enabled=true
	enabled := true
	layer1 := &EnvFile{
		Env: map[string]EnvVar{
			"VAR": {Value: "original", Enabled: &enabled},
		},
	}

	// 层2: 同名 VAR 被 enabled=false（覆盖 enabled 状态）
	disabled := false
	layer2 := &EnvFile{
		Env: map[string]EnvVar{
			"VAR": {Value: "overridden", Enabled: &disabled},
		},
	}

	merged := MergeEnv(layer1, layer2)

	if merged["VAR"].Value != "overridden" {
		t.Errorf("VAR.Value = %q, want overridden", merged["VAR"].Value)
	}
	if merged["VAR"].IsEnabled() {
		t.Error("VAR should be disabled after layer2 override")
	}

	// ToInjectEnv 应该不注入
	injected := ToInjectEnv(merged)
	if _, ok := injected["VAR"]; ok {
		t.Error("VAR should NOT be injected after being disabled by later layer")
	}
}
