package core

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/supdorg/supd/internal/config"
)

// scriptChecker script类型readiness检查
// REQ-F-009: 执行校验脚本，exit 0判定ready，非0判定未ready
type scriptChecker struct {
	check []string
	dir   string   // 工作目录（服务目录），使 check 中的相对路径可解析
	env   []string // 规格 §2.2.3: 继承服务进程的环境变量，使 check 脚本能访问服务 env
}

func newScriptChecker(cfg *config.ReadinessConfig, dir string, env []string) (*scriptChecker, error) {
	if len(cfg.Check) == 0 {
		return nil, fmt.Errorf("readiness script: check command is required")
	}
	return &scriptChecker{
		check: cfg.Check,
		dir:   dir,
		env:   env,
	}, nil
}

// Check 执行单次脚本探测；重试间隔和总超时由调用方负责。
func (s *scriptChecker) Check(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, s.check[0], s.check[1:]...)
	// 与主服务命令一致，在服务目录下执行 check 脚本，使相对路径（如 check_ready.sh）可解析
	if s.dir != "" {
		cmd.Dir = s.dir
	}
	// 规格 §2.2.3: type=script 时继承服务的环境变量
	if len(s.env) > 0 {
		cmd.Env = s.env
	}
	return cmd.Run()
}

// Close script无需清理
func (s *scriptChecker) Close() error {
	return nil
}
