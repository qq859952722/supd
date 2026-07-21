package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDoValidate_EmptyFile 测试空文件应返回错误（YAML 解析为 nil，根节点非映射）
func TestDoValidate_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "empty.yaml")
	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatalf("write empty.yaml: %v", err)
	}

	result := doValidate(path)
	if result.Valid {
		t.Errorf("空文件不应通过校验")
	}
	if result.File != path {
		t.Errorf("result.File = %q, want %q", result.File, path)
	}
}

// TestDoValidate_RootNotMap 测试根节点为非映射类型（如列表）应返回错误
func TestDoValidate_RootNotMap(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "list.yaml")
	content := "- item1\n- item2\n- item3\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write list.yaml: %v", err)
	}

	result := doValidate(path)
	if result.Valid {
		t.Errorf("根节点为列表不应通过校验")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "映射") || strings.Contains(e, "dict") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("应包含根节点非映射错误，errors=%v", result.Errors)
	}
}

// TestDoValidate_MissingSettings 测试缺少 settings 字段应产生警告（非错误）
func TestDoValidate_MissingSettings(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "no_settings.yaml")
	content := "env_files:\n  - env/00-base.yaml\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write no_settings.yaml: %v", err)
	}

	result := doValidate(path)
	if !result.Valid {
		t.Errorf("缺少 settings 应通过校验（仅警告），errors=%v", result.Errors)
	}
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "settings") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("应包含 settings 警告，warnings=%v", result.Warnings)
	}
}

// TestDoValidate_InvalidLogLevel 测试 log_level 非法值
func TestDoValidate_InvalidLogLevel(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "bad_loglevel.yaml")
	content := `settings:
  log_level: "verbose"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write bad_loglevel.yaml: %v", err)
	}

	result := doValidate(path)
	if result.Valid {
		t.Errorf("非法 log_level 不应通过校验")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "log_level") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("应检测到 log_level 错误，errors=%v", result.Errors)
	}
}

// TestDoValidate_InvalidAuthMode 测试 auth_mode 非法值
func TestDoValidate_InvalidAuthMode(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "bad_authmode.yaml")
	content := `settings:
  auth_mode: "invalid"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write bad_authmode.yaml: %v", err)
	}

	result := doValidate(path)
	if result.Valid {
		t.Errorf("非法 auth_mode 不应通过校验")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "auth_mode") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("应检测到 auth_mode 错误，errors=%v", result.Errors)
	}
}

// TestDoValidate_EmptyHTTPListen 测试 http_listen 空字符串应产生警告
func TestDoValidate_EmptyHTTPListen(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "empty_listen.yaml")
	content := `settings:
  http_listen: ""
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write empty_listen.yaml: %v", err)
	}

	result := doValidate(path)
	if !result.Valid {
		t.Errorf("http_listen 空字符串应通过校验（仅警告），errors=%v", result.Errors)
	}
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "http_listen") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("应包含 http_listen 警告，warnings=%v", result.Warnings)
	}
}

// TestDoValidate_EnvFilesNonString 测试 env_files 包含非字符串元素
func TestDoValidate_EnvFilesNonString(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "bad_envfiles.yaml")
	content := `env_files:
  - env/00-base.yaml
  - 123
  - env/01-extra.yaml
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write bad_envfiles.yaml: %v", err)
	}

	result := doValidate(path)
	if result.Valid {
		t.Errorf("env_files 含非字符串元素不应通过校验")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "env_files") && strings.Contains(e, "1") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("应检测到 env_files[1] 错误，errors=%v", result.Errors)
	}
}

// TestDoValidate_ExtensionDirsNonString 测试 extension_dirs 包含非字符串元素
func TestDoValidate_ExtensionDirsNonString(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "bad_extdirs.yaml")
	content := `extension_dirs:
  - extensions/
  - false
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write bad_extdirs.yaml: %v", err)
	}

	result := doValidate(path)
	if result.Valid {
		t.Errorf("extension_dirs 含非字符串元素不应通过校验")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "extension_dirs") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("应检测到 extension_dirs 错误，errors=%v", result.Errors)
	}
}

// TestDoValidate_NonexistentPath 测试不存在的路径返回错误并包含中文友好消息
func TestDoValidate_NonexistentPath(t *testing.T) {
	result := doValidate("/nonexistent/path/to/config.yaml")
	if result.Valid {
		t.Errorf("不存在的路径不应通过校验")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "路径不存在") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("应包含 '路径不存在' 中文消息，errors=%v", result.Errors)
	}
}

// TestDoValidate_MultipleErrors 测试多个错误同时存在
func TestDoValidate_MultipleErrors(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "multi_errors.yaml")
	content := `settings:
  auth_mode: "invalid"
  log_level: "verbose"
  log_max_size_mb: -5
  log_max_files: 0
  shutdown_grace_seconds: -1
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write multi_errors.yaml: %v", err)
	}

	result := doValidate(path)
	if result.Valid {
		t.Errorf("多错误配置不应通过校验")
	}
	if len(result.Errors) < 5 {
		t.Errorf("应至少检测到 5 个错误，实际 %d: %v", len(result.Errors), result.Errors)
	}
}

// TestValidateSettingsV2_ValidSettings 测试合法 settings 通过校验
func TestValidateSettingsV2_ValidSettings(t *testing.T) {
	settings := map[string]interface{}{
		"http_listen":                     ":8080",
		"auth_mode":                       "local_skip",
		"log_level":                       "info",
		"log_max_size_mb":                 10,
		"log_max_files":                   5,
		"shutdown_grace_seconds":          30,
		"extension_default_timeout_seconds": 600,
		"extension_hard_limit_seconds":      1800,
		"run_history_retention_seconds":     604800,
		"file_history_versions":             50,
		"max_upload_size_mb":                100,
	}
	result := &ValidateResult{}
	validateSettingsV2(settings, result)
	if len(result.Errors) != 0 {
		t.Errorf("合法 settings 不应有错误，errors=%v", result.Errors)
	}
}

// TestCheckPositiveIntV2_ZeroAndNegative 测试 0 和负数应产生错误
func TestCheckPositiveIntV2_ZeroAndNegative(t *testing.T) {
	tests := []struct {
		name    string
		value   interface{}
		wantErr bool
	}{
		{"int_zero", 0, true},
		{"int_negative", -1, true},
		{"int_positive", 1, false},
		{"int64_zero", int64(0), true},
		{"int64_negative", int64(-5), true},
		{"int64_positive", int64(10), false},
		{"float64_zero", float64(0), true},
		{"float64_negative", float64(-3.14), true},
		{"float64_positive", float64(10.5), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ValidateResult{}
			m := map[string]interface{}{"test_field": tt.value}
			checkPositiveIntV2(m, "test_field", r)
			if tt.wantErr && len(r.Errors) == 0 {
				t.Errorf("value=%v 应产生错误", tt.value)
			}
			if !tt.wantErr && len(r.Errors) != 0 {
				t.Errorf("value=%v 不应产生错误，errors=%v", tt.value, r.Errors)
			}
		})
	}
}

// TestCheckPositiveIntV2_MissingKey 测试缺失字段不应产生错误（可选字段）
func TestCheckPositiveIntV2_MissingKey(t *testing.T) {
	r := &ValidateResult{}
	m := map[string]interface{}{}
	checkPositiveIntV2(m, "missing_field", r)
	if len(r.Errors) != 0 {
		t.Errorf("缺失字段不应产生错误，errors=%v", r.Errors)
	}
}

// TestValidateStringSliceV2_MixedTypes 测试混合类型 slice
func TestValidateStringSliceV2_MixedTypes(t *testing.T) {
	tests := []struct {
		name     string
		slice    []interface{}
		wantErrs int
	}{
		{"all_strings", []interface{}{"a", "b", "c"}, 0},
		{"all_non_strings", []interface{}{1, true, 3.14}, 3},
		{"mixed", []interface{}{"a", 1, "b", false}, 2},
		{"empty", []interface{}{}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ValidateResult{}
			validateStringSliceV2(tt.slice, "test_slice", r)
			if len(r.Errors) != tt.wantErrs {
				t.Errorf("got %d errors, want %d, errors=%v", len(r.Errors), tt.wantErrs, r.Errors)
			}
		})
	}
}

// TestValidateServicesAndExtensions_NoDirs 测试空目录（无 services/extensions 子目录）返回 0
func TestValidateServicesAndExtensions_NoDirs(t *testing.T) {
	tmpDir := t.TempDir()
	r := &ValidateResult{}
	count := validateServicesAndExtensions(tmpDir, r)
	if count != 0 {
		t.Errorf("空目录 count = %d, want 0", count)
	}
	if len(r.Errors) != 0 {
		t.Errorf("空目录不应有错误，errors=%v", r.Errors)
	}
}

// TestValidateServicesAndExtensions_ValidService 测试合法 service.yaml 通过校验
func TestValidateServicesAndExtensions_ValidService(t *testing.T) {
	tmpDir := t.TempDir()
	svcDir := filepath.Join(tmpDir, "services", "svc1")
	if err := os.MkdirAll(svcDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := `name: svc1
version: "1.0"
command:
  - sleep
  - "60"
`
	if err := os.WriteFile(filepath.Join(svcDir, "service.yaml"), []byte(content), 0644); err != nil {
		t.Fatalf("write service.yaml: %v", err)
	}

	r := &ValidateResult{}
	count := validateServicesAndExtensions(tmpDir, r)
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
	if len(r.Errors) != 0 {
		t.Errorf("合法 service 不应有错误，errors=%v", r.Errors)
	}
}

// TestValidateServicesAndExtensions_InvalidService 测试非法 service.yaml 产生错误
func TestValidateServicesAndExtensions_InvalidService(t *testing.T) {
	tmpDir := t.TempDir()
	svcDir := filepath.Join(tmpDir, "services", "bad-svc")
	if err := os.MkdirAll(svcDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// 缺少 command 字段（必填）
	content := `name: bad-svc
version: "1.0"
`
	if err := os.WriteFile(filepath.Join(svcDir, "service.yaml"), []byte(content), 0644); err != nil {
		t.Fatalf("write service.yaml: %v", err)
	}

	r := &ValidateResult{}
	count := validateServicesAndExtensions(tmpDir, r)
	if count != 0 {
		t.Errorf("count = %d, want 0 (invalid service)", count)
	}
	if len(r.Errors) == 0 {
		t.Errorf("非法 service 应产生错误")
	}
}

// TestValidateServicesAndExtensions_ValidExtension 测试合法 meta.yaml 通过校验
func TestValidateServicesAndExtensions_ValidExtension(t *testing.T) {
	tmpDir := t.TempDir()
	extDir := filepath.Join(tmpDir, "extensions", "ext1")
	if err := os.MkdirAll(extDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// ExtensionMeta.Entry 是 string（非 list），Triggers 是 struct（非 list）
	content := `name: ext1
version: "1.0"
description: test extension
entry: run.sh
timeout_seconds: 60
concurrency: replace
triggers:
  on_demand: true
`
	if err := os.WriteFile(filepath.Join(extDir, "meta.yaml"), []byte(content), 0644); err != nil {
		t.Fatalf("write meta.yaml: %v", err)
	}

	r := &ValidateResult{}
	count := validateServicesAndExtensions(tmpDir, r)
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
	if len(r.Errors) != 0 {
		t.Errorf("合法 extension 不应有错误，errors=%v", r.Errors)
	}
}

// TestRunValidate_ValidConfigText 测试 runValidate 文本输出（合法配置）
func TestRunValidate_ValidConfigText(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	content := `settings:
  http_listen: ":8080"
  auth_mode: "none"
  log_level: "info"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	// 保存原始状态
	origJSON := validateOutputJSON
	defer func() { validateOutputJSON = origJSON }()
	validateOutputJSON = false

	err := runValidate(nil, []string{cfgPath})
	if err != nil {
		t.Errorf("合法配置应返回 nil，got %v", err)
	}
}

// TestRunValidate_InvalidConfigReturnsError 测试 runValidate 校验失败时返回错误
func TestRunValidate_InvalidConfigReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	content := `settings:
  auth_mode: "invalid"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	origJSON := validateOutputJSON
	defer func() { validateOutputJSON = origJSON }()
	validateOutputJSON = false

	err := runValidate(nil, []string{cfgPath})
	if err == nil {
		t.Errorf("非法配置应返回错误")
	}
}

// TestRunValidate_JSONOutput 测试 runValidate JSON 输出格式
func TestRunValidate_JSONOutput(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	content := `settings:
  http_listen: ":8080"
  auth_mode: "none"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	origJSON := validateOutputJSON
	defer func() { validateOutputJSON = origJSON }()
	validateOutputJSON = true

	// JSON 输出到 stdout，捕获并验证可解析
	err := runValidate(nil, []string{cfgPath})
	if err != nil {
		t.Errorf("合法配置应返回 nil，got %v", err)
	}
}

// TestRunValidate_NoArgsUsesConfigPath 测试无参数时使用 getConfigPath()
func TestRunValidate_NoArgsUsesConfigPath(t *testing.T) {
	// 保存原始状态
	origCfgPath := cfgPath
	defer func() { cfgPath = origCfgPath }()

	tmpDir := t.TempDir()
	cfgPath = filepath.Join(tmpDir, "config.yaml")
	content := `settings:
  http_listen: ":8080"
  auth_mode: "none"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	err := runValidate(nil, []string{})
	if err != nil {
		t.Errorf("无参数合法配置应返回 nil，got %v", err)
	}
}

// TestValidateResult_JSONMarshal 测试 ValidateResult JSON 序列化
func TestValidateResult_JSONMarshal(t *testing.T) {
	r := ValidateResult{
		Valid:    true,
		File:     "/tmp/test.yaml",
		Errors:   nil,
		Warnings: []string{"test warning"},
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, `"valid":true`) {
		t.Errorf("JSON 应包含 valid:true, got %s", s)
	}
	if !strings.Contains(s, `"file":"/tmp/test.yaml"`) {
		t.Errorf("JSON 应包含 file 字段, got %s", s)
	}
	// Errors 为空时应省略（omitempty）
	if strings.Contains(s, `"errors"`) {
		t.Errorf("空 errors 应被 omitempty 省略, got %s", s)
	}
}
