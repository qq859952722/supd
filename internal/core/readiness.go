package core

import (
	"context"
	"fmt"

	"github.com/supdorg/supd/internal/config"
)

// ReadinessChecker readiness检查接口
// REQ-F-009: 4种readiness检查类型
type ReadinessChecker interface {
	// Check 执行readiness检查，返回是否ready
	// ctx用于超时控制
	Check(ctx context.Context) error
	// Close 清理资源（如fd_notify的pipe读端）
	Close() error
}

// NewReadinessChecker 工厂函数，根据type创建对应的checker
// REQ-F-009: fd_notify/tcp_check/http_check/script
// dir 为服务目录，仅 script 类型使用（使 check 中的相对路径可解析），其他类型忽略
func NewReadinessChecker(cfg *config.ReadinessConfig, dir string) (ReadinessChecker, error) {
	if cfg == nil {
		return nil, fmt.Errorf("readiness config is nil")
	}

	switch cfg.Type {
	case "fd_notify":
		return NewNotifyChecker(cfg)
	case "tcp_check":
		return newTCPChecker(cfg)
	case "http_check":
		return newHTTPChecker(cfg)
	case "script":
		return newScriptChecker(cfg, dir)
	default:
		return nil, fmt.Errorf("readiness: unsupported type %q", cfg.Type)
	}
}
