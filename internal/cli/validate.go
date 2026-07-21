package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/supdorg/supd/internal/config"
)

// REQ-F-039: validate 命令 — 校验配置
var (
	validateOutputJSON bool
)

var validateCmd = &cobra.Command{
	Use:   "validate [path]",
	Short: "校验配置文件",
	Long:  "校验 supd 配置文件的语法和语义正确性。不连接运行中的 supd 实例。",
	Example: `  # 校验默认工作目录下的 config.yaml
  supd validate

  # 校验指定路径的配置文件
  supd validate /path/to/config.yaml

  # 以 JSON 格式输出（便于脚本处理）
  supd validate -o json`,
	Args: maximumNArgs(1),
	RunE:  runValidate,
}

func init() {
	validateCmd.Flags().BoolVarP(&validateOutputJSON, "output", "o", false, "以 JSON 格式输出结果")
}

// ValidateResult 校验结果结构
type ValidateResult struct {
	Valid    bool     `json:"valid"`
	File     string   `json:"file"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// runValidate 执行 validate 命令
// REQ-F-039: supd validate / supd validate <path> / supd validate -o json
func runValidate(cmd *cobra.Command, args []string) error {
	var cfgPath string

	if len(args) > 0 {
		cfgPath = args[0]
	} else {
		cfgPath = getConfigPath()
	}

	result := doValidate(cfgPath)

	// P-02-02: 校验 services 和 extensions 配置文件
	workDir := filepath.Dir(cfgPath)
	extraCount := validateServicesAndExtensions(workDir, &result)
	result.Valid = len(result.Errors) == 0

	if validateOutputJSON {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
	} else {
		if result.Valid {
			infof("%s: 校验通过", result.File)
			if extraCount > 0 {
				infof("  (另校验 %d 个服务/扩展配置文件均通过)", extraCount)
			}
		} else {
			for _, e := range result.Errors {
				fmt.Fprintf(os.Stderr, "  错误: %s\n", e)
			}
			fmt.Fprintf(os.Stderr, "%s: 校验失败\n", result.File)
		}
		for _, w := range result.Warnings {
			infof("  警告: %s", w)
		}
	}

	// P-02-001 修复：校验失败时返回非零退出码
	if !result.Valid {
		return fmt.Errorf("校验失败")
	}
	return nil
}

// doValidate 执行实际的配置校验
// REQ-D-006: 配置校验规则
func doValidate(path string) ValidateResult {
	result := ValidateResult{File: path}

	data, err := readFile(path)
	if err != nil {
		// P-02-002: 路径不存在返回中文友好错误，避免暴露 Go 内部错误
		if os.IsNotExist(err) {
			result.Errors = append(result.Errors, fmt.Sprintf("路径不存在: %s", path))
		} else {
			result.Errors = append(result.Errors, fmt.Sprintf("读取文件: %v", err))
		}
		return result
	}

	// YAML 解析
	var raw any
	if err := config.SafeUnmarshal([]byte(data), &raw, config.DefaultSafeYAMLOptions); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("YAML 解析: %v", err))
		return result
	}

	// yaml.v4 解析为 map[string]interface{}
	cfgMap, ok := raw.(map[string]interface{})
	if !ok {
		result.Errors = append(result.Errors, "配置根节点必须是映射 (dict)")
		return result
	}

	// 校验 settings 字段
	settings, ok := cfgMap["settings"].(map[string]interface{})
	if ok {
		validateSettingsV2(settings, &result)
	} else {
		result.Warnings = append(result.Warnings, "缺少 settings 字段")
	}

	// 校验 env_files
	if envFiles, ok := cfgMap["env_files"].([]interface{}); ok {
		validateStringSliceV2(envFiles, "env_files", &result)
	}

	// 校验 extension_dirs
	if extDirs, ok := cfgMap["extension_dirs"].([]interface{}); ok {
		validateStringSliceV2(extDirs, "extension_dirs", &result)
	}

	result.Valid = len(result.Errors) == 0
	return result
}

// validateServicesAndExtensions 遍历 workdir 下的服务与扩展配置文件并校验
// P-02-02: 补充校验 services/*/service.yaml、extensions/*/meta.yaml
// 以及服务级扩展 services/*/extensions/*/meta.yaml
// 返回成功校验的文件数（不含 config.yaml）
func validateServicesAndExtensions(workDir string, result *ValidateResult) int {
	count := 0

	// services/*/service.yaml
	serviceConfigs, _ := filepath.Glob(filepath.Join(workDir, "services", "*", "service.yaml"))
	for _, svcPath := range serviceConfigs {
		if _, err := config.LoadService(svcPath); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", svcPath, err))
		} else {
			count++
		}
	}

	// extensions/*/meta.yaml（全局扩展）
	extConfigs, _ := filepath.Glob(filepath.Join(workDir, "extensions", "*", "meta.yaml"))
	for _, metaPath := range extConfigs {
		if _, err := config.LoadExtension(metaPath); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", metaPath, err))
		} else {
			count++
		}
	}

	// services/*/extensions/*/meta.yaml（服务级扩展）
	svcExtConfigs, _ := filepath.Glob(filepath.Join(workDir, "services", "*", "extensions", "*", "meta.yaml"))
	for _, metaPath := range svcExtConfigs {
		if _, err := config.LoadExtension(metaPath); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", metaPath, err))
		} else {
			count++
		}
	}

	return count
}

// validateSettingsV2 校验 settings 块
// REQ-D-006: 校验规则
func validateSettingsV2(s map[string]interface{}, r *ValidateResult) {
	// http_listen
	if hl, ok := s["http_listen"].(string); ok && hl == "" {
		r.Warnings = append(r.Warnings, "settings.http_listen 为空字符串")
	}

	// auth_mode — REQ-2.7.1: none | local_skip | always_token
	authModes := map[string]bool{"none": true, "local_skip": true, "always_token": true}
	if am, ok := s["auth_mode"].(string); ok && !authModes[am] {
		r.Errors = append(r.Errors, fmt.Sprintf("settings.auth_mode=%q 无效，必须是 none/local_skip/always_token", am))
	}

	// log_level
	logLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if ll, ok := s["log_level"].(string); ok && !logLevels[ll] {
		r.Errors = append(r.Errors, fmt.Sprintf("settings.log_level=%q 无效，必须是 debug/info/warn/error", ll))
	}

	// 数值字段
	checkPositiveIntV2(s, "log_max_size_mb", r)
	checkPositiveIntV2(s, "log_max_files", r)
	checkPositiveIntV2(s, "shutdown_grace_seconds", r)
	checkPositiveIntV2(s, "extension_default_timeout_seconds", r)
	checkPositiveIntV2(s, "extension_hard_limit_seconds", r)
	checkPositiveIntV2(s, "run_history_retention_seconds", r)
	checkPositiveIntV2(s, "file_history_versions", r)
	checkPositiveIntV2(s, "max_upload_size_mb", r)
}

func checkPositiveIntV2(m map[string]interface{}, key string, r *ValidateResult) {
	v, ok := m[key]
	if !ok {
		return
	}
	switch n := v.(type) {
	case int:
		if n <= 0 {
			r.Errors = append(r.Errors, fmt.Sprintf("settings.%s 必须 > 0，得到 %d", key, n))
		}
	case int64:
		if n <= 0 {
			r.Errors = append(r.Errors, fmt.Sprintf("settings.%s 必须 > 0，得到 %d", key, n))
		}
	case float64:
		if n <= 0 {
			r.Errors = append(r.Errors, fmt.Sprintf("settings.%s 必须 > 0，得到 %.0f", key, n))
		}
	}
}

func validateStringSliceV2(slice []interface{}, fieldName string, r *ValidateResult) {
	for i, item := range slice {
		if _, ok := item.(string); !ok {
			r.Errors = append(r.Errors, fmt.Sprintf("%s[%d]: 不是字符串", fieldName, i))
		}
	}
}
