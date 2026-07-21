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
}

func newScriptChecker(cfg *config.ReadinessConfig) (*scriptChecker, error) {
	if len(cfg.Check) == 0 {
		return nil, fmt.Errorf("readiness script: check command is required")
	}
	return &scriptChecker{
		check:           cfg.Check,
		intervalSeconds: cfg.IntervalSeconds,
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
