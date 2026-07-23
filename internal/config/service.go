package config

import (
	"fmt"
	"os"

	"github.com/supdorg/supd/internal/identity"
)

// ServiceConfig 服务配置
// REQ-D-007: service.yaml
type ServiceConfig struct {
	Name        string   `yaml:"name"`
	Version     string   `yaml:"version"`
	Description string   `yaml:"description"`
	Icon        string   `yaml:"icon"`
	Autostart   *bool    `yaml:"autostart"` // pointer to distinguish unset from false
	Command     []string `yaml:"command"`
	Runtime     string   `yaml:"runtime"`
	User        string   `yaml:"user"`
	Group       string   `yaml:"group"`
	UID         int      `yaml:"uid"`          // UID 模式：直接指定 uid（与 user 互斥）
	GID         int      `yaml:"gid"`          // UID 模式：直接指定 gid（0 表示 = uid）
	Groups      []int    `yaml:"groups"`       // UID 模式：补充组 gid 列表
	Workdir     string   `yaml:"workdir"`
	DependsOn   []string `yaml:"depends_on"`
	Tags        []string `yaml:"tags"`

	Readiness *ReadinessConfig `yaml:"readiness"`
	Restart   *RestartConfig   `yaml:"restart"`
	Stop      *StopConfig      `yaml:"stop"`
	Logging   *LoggingConfig   `yaml:"logging"`
	Signals   *SignalsConfig   `yaml:"signals"`
	Package   *PackageConfig   `yaml:"package"`
}

// ReadinessConfig readiness 检查配置
// REQ-F-009: 4种类型 fd_notify/tcp_check/http_check/script
type ReadinessConfig struct {
	Type            string   `yaml:"type"`             // fd_notify | tcp_check | http_check | script
	Fd              int      `yaml:"fd"`               // type=fd_notify
	Port            int      `yaml:"port"`             // type=tcp_check
	URL             string   `yaml:"url"`              // type=http_check
	ExpectedStatus  int      `yaml:"expected_status"`  // type=http_check, 默认 200
	Check           []string `yaml:"check"`            // type=script
	IntervalSeconds int      `yaml:"interval_seconds"` // 默认 1
	TimeoutSeconds  int      `yaml:"timeout_seconds"`  // 默认 5
}

// StopConfig 停止配置
// REQ-F-007: stop.timeout_seconds 覆盖整个停止流程
type StopConfig struct {
	GraceSeconds   int `yaml:"grace_seconds"`   // 默认 10
	TimeoutSeconds int `yaml:"timeout_seconds"` // 默认 60
}

// LoggingConfig 日志配置
type LoggingConfig struct {
	Enabled   *bool `yaml:"enabled"`    // 默认 true
	MaxSizeMB int   `yaml:"max_size_mb"` // 默认 10
	MaxFiles  int   `yaml:"max_files"`   // 默认 5
}

// SignalsConfig 自定义信号配置
type SignalsConfig struct {
	Reload       string `yaml:"reload"`
	RotateLogs   string `yaml:"rotate_logs"`
	GracefulQuit string `yaml:"graceful_quit"`
}

// PackageConfig 打包配置
type PackageConfig struct {
	Include []string `yaml:"include"`
	Exclude []string `yaml:"exclude"`
	Default string   `yaml:"default"` // include | exclude
}

// LoadService 从文件加载服务配置，填充默认值并校验
func LoadService(path string) (*ServiceConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read service config: %w", err)
	}

	sc := &ServiceConfig{}
	if err := SafeUnmarshal(data, sc, DefaultSafeYAMLOptions); err != nil {
		return nil, fmt.Errorf("parse service config %s: %w", path, err)
	}

	setServiceDefaults(sc)
	if err := validateService(sc); err != nil {
		return nil, err
	}

	return sc, nil
}

// ToCredentialSpec 从服务配置构造身份 spec。
// §2.2.13: User 模式（user/group）与 UID 模式（uid/gid/groups）互斥，
// 互斥由 validateService 保证，此处直接映射。
func (sc *ServiceConfig) ToCredentialSpec() identity.CredentialSpec {
	return identity.CredentialSpec{
		User:   sc.User,
		Group:  sc.Group,
		UID:    sc.UID,
		GID:    sc.GID,
		Groups: sc.Groups,
	}
}
