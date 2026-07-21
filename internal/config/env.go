package config

import (
	"fmt"
	"os"
	"strings"
)

// EnvVar 单条环境变量
// REQ-D-008: env.yaml 格式
type EnvVar struct {
	Value   string `yaml:"value" json:"value"`
	Enabled *bool  `yaml:"enabled" json:"enabled,omitempty"` // pointer: nil = true (default)
	Hint    string `yaml:"hint" json:"hint,omitempty"`
}

// EnvFile env.yaml 文件结构
type EnvFile struct {
	Env map[string]EnvVar `yaml:"env" json:"env"`
}

// LoadEnv 从文件加载环境变量
func LoadEnv(path string) (*EnvFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read env file: %w", err)
	}

	ef := &EnvFile{}
	if err := SafeUnmarshal(data, ef, DefaultSafeYAMLOptions); err != nil {
		return nil, fmt.Errorf("parse env file %s: %w", path, err)
	}

	if ef.Env == nil {
		ef.Env = make(map[string]EnvVar)
	}

	return ef, nil
}

// IsEnabled 返回变量的 enabled 状态（默认 true）
func (e EnvVar) IsEnabled() bool {
	if e.Enabled == nil {
		return true
	}
	return *e.Enabled
}

// IsSensitive 判断变量是否为敏感字段（密码字段启发式识别）
// REQ-F-015: 变量名包含 PASSWORD/PWD/SECRET/TOKEN/KEY 时前端按 password 渲染
func IsSensitive(name string) bool {
	upper := strings.ToUpper(name)
	keywords := []string{"PASSWORD", "PWD", "SECRET", "TOKEN", "KEY"}
	for _, kw := range keywords {
		if strings.Contains(upper, kw) {
			return true
		}
	}
	return false
}
