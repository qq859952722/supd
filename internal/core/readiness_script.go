package core

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/supdorg/supd/internal/config"
)

// scriptChecker script类型readiness检查
// REQ-F-009: 执行校验脚本，exit 0判定ready，非0判定未ready
type scriptChecker struct {
	check           []string
	intervalSeconds int
	dir             string // 工作目录（服务目录），使 check 中的相对路径可解析
}

func newScriptChecker(cfg *config.ReadinessConfig, dir string) (*scriptChecker, error) {
	if len(cfg.Check) == 0 {
		return nil, fmt.Errorf("readiness script: check command is required")
	}
	return &scriptChecker{
		check:           cfg.Check,
		intervalSeconds: cfg.IntervalSeconds,
		dir:             dir,
	}, nil
}

// Check 循环执行校验脚本，exit 0返回nil
// REQ-F-009: interval_seconds间隔循环执行，超时由ctx控制
func (s *scriptChecker) Check(ctx context.Context) error {
	interval := time.Duration(s.intervalSeconds) * time.Second

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		cmd := exec.CommandContext(ctx, s.check[0], s.check[1:]...)
		// 与主服务命令一致，在服务目录下执行 check 脚本，使相对路径（如 check_ready.sh）可解析
		if s.dir != "" {
			cmd.Dir = s.dir
		}
		err := cmd.Run()
		if err == nil {
			return nil
		}

		// 如果是ctx取消导致的，直接返回
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// 等待interval或ctx取消
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

// Close script无需清理
func (s *scriptChecker) Close() error {
	return nil
}
