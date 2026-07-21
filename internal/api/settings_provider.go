package api

import (
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v4"

	"github.com/supdorg/supd/internal/config"
)

// --- SettingsProvider 适配器 ---

type ConfigSettingsProvider struct {
	Config  *config.Config
	BaseDir string
}

func (p *ConfigSettingsProvider) GetSettings() *config.Settings {
	return &p.Config.Settings
}

func (p *ConfigSettingsProvider) UpdateSettings(settings *config.Settings) error {
	if settings == nil {
		return nil
	}
	// F-06-001 修复：保留原 auth_token，防止前端未传该字段时被清空
	if settings.AuthToken == "" {
		settings.AuthToken = p.Config.Settings.AuthToken
	}
	p.Config.Settings = *settings
	return p.writeConfigFile()
}

func (p *ConfigSettingsProvider) GetEnv() (*config.EnvFile, error) {
	envPath := filepath.Join(p.BaseDir, "env", "00-base.yaml")
	env, err := config.LoadEnv(envPath)
	if err != nil {
		// env 文件不存在时返回空对象而非错误
		return &config.EnvFile{Env: make(map[string]config.EnvVar)}, nil
	}
	return env, nil
}

func (p *ConfigSettingsProvider) UpdateEnv(envFile *config.EnvFile) error {
	if envFile == nil {
		return nil
	}
	envPath := filepath.Join(p.BaseDir, "env", "00-base.yaml")
	if err := os.MkdirAll(filepath.Dir(envPath), 0755); err != nil {
		return fmt.Errorf("create env dir: %w", err)
	}
	data, err := yaml.Marshal(envFile)
	if err != nil {
		return fmt.Errorf("marshal env file: %w", err)
	}
	return os.WriteFile(envPath, data, 0644)
}

func (p *ConfigSettingsProvider) GetRuntimesConfig() map[string]string {
	return p.Config.Runtimes
}

func (p *ConfigSettingsProvider) UpdateRuntimesConfig(runtimes map[string]string) error {
	p.Config.Runtimes = runtimes
	return p.writeConfigFile()
}

// GetEnvFiles 返回全局环境变量文件列表
func (p *ConfigSettingsProvider) GetEnvFiles() []string {
	return p.Config.EnvFiles
}

// UpdateEnvFiles 更新全局环境变量文件加载顺序
// REQ-D-006: env_files 不影响已运行服务；新启动的服务用新 env
func (p *ConfigSettingsProvider) UpdateEnvFiles(files []string) error {
	p.Config.EnvFiles = files
	return p.writeConfigFile()
}

// GetExtensionDirs 返回全局扩展目录列表
func (p *ConfigSettingsProvider) GetExtensionDirs() []string {
	return p.Config.ExtensionDirs
}

// UpdateExtensionDirs 更新全局扩展目录
// 注意：更改后需要重新扫描扩展，扩展列表会变化
func (p *ConfigSettingsProvider) UpdateExtensionDirs(dirs []string) error {
	p.Config.ExtensionDirs = dirs
	return p.writeConfigFile()
}

// GetDefaults 返回全局默认重启策略
func (p *ConfigSettingsProvider) GetDefaults() config.DefaultRestart {
	return p.Config.Defaults
}

// UpdateDefaults 更新全局默认重启策略
// REQ-2.4.6: defaults.restart 立即生效
func (p *ConfigSettingsProvider) UpdateDefaults(defaults config.DefaultRestart) error {
	p.Config.Defaults = defaults
	return p.writeConfigFile()
}

func (p *ConfigSettingsProvider) writeConfigFile() error {
	configPath := filepath.Join(p.BaseDir, "config.yaml")
	data, err := yaml.Marshal(p.Config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(configPath, data, 0644)
}
