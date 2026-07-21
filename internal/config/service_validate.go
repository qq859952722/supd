package config

import (
	"fmt"
	"regexp"
	"strings"
)

// serviceNameRegex 服务名正则
// REQ-D-007: ^[a-z][a-z0-9-]*$
var serviceNameRegex = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// allowedSignals 允许的自定义信号
var allowedSignals = map[string]bool{
	"HUP": true, "INT": true, "QUIT": true, "USR1": true,
	"USR2": true, "PIPE": true, "ALRM": true, "CHLD": true,
}

// forbiddenSignals 禁止使用的信号
var forbiddenSignals = map[string]bool{
	"TERM": true, "KILL": true, "STOP": true, "CONT": true,
	"SEGV": true, "ABRT": true, "BUS": true, "FPE": true, "ILL": true,
}

// validReadinessTypes 有效的 readiness 类型
// REQ-F-009: 4种类型
var validReadinessTypes = map[string]bool{
	"fd_notify": true, "tcp_check": true, "http_check": true, "script": true,
}

// validRestartPolicies 有效的重启策略
var validRestartPolicies = map[string]bool{
	"always": true, "on-failure": true, "never": true,
}

// setServiceDefaults 为服务配置填充默认值
func setServiceDefaults(sc *ServiceConfig) {
	if sc.Autostart == nil {
		t := true
		sc.Autostart = &t
	}
	if sc.Icon == "" {
		sc.Icon = "box"
	}

	if sc.Readiness != nil {
		if sc.Readiness.ExpectedStatus == 0 {
			sc.Readiness.ExpectedStatus = 200
		}
		if sc.Readiness.IntervalSeconds == 0 {
			sc.Readiness.IntervalSeconds = 1
		}
		if sc.Readiness.TimeoutSeconds == 0 {
			sc.Readiness.TimeoutSeconds = 5
		}
	}

	if sc.Stop != nil {
		if sc.Stop.GraceSeconds == 0 {
			sc.Stop.GraceSeconds = DefaultStopGraceSeconds
		}
		if sc.Stop.TimeoutSeconds == 0 {
			sc.Stop.TimeoutSeconds = DefaultStopTimeoutSeconds
		}
	}

	if sc.Logging != nil {
		if sc.Logging.Enabled == nil {
			t := true
			sc.Logging.Enabled = &t
		}
		if sc.Logging.MaxSizeMB == 0 {
			sc.Logging.MaxSizeMB = 10
		}
		if sc.Logging.MaxFiles == 0 {
			sc.Logging.MaxFiles = 5
		}
	}
}

// validateService 校验服务配置的合法性
func validateService(sc *ServiceConfig) error {
	// name 必填
	if sc.Name == "" {
		return fmt.Errorf("name: is required")
	}
	if !serviceNameRegex.MatchString(sc.Name) {
		return fmt.Errorf("name: invalid value %q, must match ^[a-z][a-z0-9-]*$", sc.Name)
	}

	// version 必填
	if sc.Version == "" {
		return fmt.Errorf("version: is required")
	}

	// command 必填，且至少1个元素
	if len(sc.Command) == 0 {
		return fmt.Errorf("command: is required and must have at least one element")
	}

	// workdir 如果设置必须以 / 开头
	if sc.Workdir != "" && !strings.HasPrefix(sc.Workdir, "/") {
		return fmt.Errorf("workdir: must be an absolute path, got %q", sc.Workdir)
	}

	// K-01-001 修复：depends_on 自引用检测（规格 §2.1.2: 服务 A 的 depends_on 包含 A 自己，视为循环依赖）
	// 提前在配置解析阶段拦截，避免依赖图构建时才发现
	for _, dep := range sc.DependsOn {
		if dep == sc.Name {
			return fmt.Errorf("depends_on: self-reference detected (service %q depends on itself)", sc.Name)
		}
	}

	// readiness 校验
	if sc.Readiness != nil {
		if err := validateReadiness(sc.Readiness); err != nil {
			return err
		}
	}

	// restart 校验
	if sc.Restart != nil {
		if err := validateRestart(sc.Restart); err != nil {
			return err
		}
	}

	// stop 校验
	if sc.Stop != nil {
		if err := validateStop(sc.Stop); err != nil {
			return err
		}
	}

	// logging 校验
	if sc.Logging != nil {
		if err := validateLogging(sc.Logging); err != nil {
			return err
		}
	}

	// signals 校验
	if sc.Signals != nil {
		if err := validateSignals(sc.Signals); err != nil {
			return err
		}
	}

	// package 校验
	if sc.Package != nil {
		if err := validatePackage(sc.Package); err != nil {
			return err
		}
	}

	return nil
}

func validateReadiness(r *ReadinessConfig) error {
	if !validReadinessTypes[r.Type] {
		return fmt.Errorf("readiness.type: invalid value %q, must be one of: fd_notify, tcp_check, http_check, script", r.Type)
	}

	switch r.Type {
	case "fd_notify":
		if r.Fd <= 0 {
			return fmt.Errorf("readiness.fd: required and must be positive for fd_notify type, got %d", r.Fd)
		}
	case "tcp_check":
		if r.Port <= 0 {
			return fmt.Errorf("readiness.port: required and must be positive for tcp_check type, got %d", r.Port)
		}
	case "http_check":
		if r.URL == "" {
			return fmt.Errorf("readiness.url: required for http_check type")
		}
	case "script":
		if len(r.Check) == 0 {
			return fmt.Errorf("readiness.check: required for script type and must have at least one element")
		}
		// K-01-002 说明：readiness script 的 Check 是命令数组（如 ["bash", "script.sh"]），
		// 由 ProcessManager 通过 exec.Command 直接执行（不经 shell），
		// 因此 Check[0] 中的 ".." 不会被 shell 展开，无路径穿越风险。
		// 若 Check[0] 为相对路径（如 "./script.sh"），exec 会以服务工作目录为 CWD 解析，
		// 仍受 baseDir 边界约束（服务工作目录在 baseDir 内）。
	}

	if r.IntervalSeconds <= 0 {
		return fmt.Errorf("readiness.interval_seconds: must be positive, got %d", r.IntervalSeconds)
	}
	if r.TimeoutSeconds <= 0 {
		return fmt.Errorf("readiness.timeout_seconds: must be positive, got %d", r.TimeoutSeconds)
	}

	return nil
}

func validateRestart(r *RestartConfig) error {
	if !validRestartPolicies[r.Policy] {
		return fmt.Errorf("restart.policy: invalid value %q, must be one of: always, on-failure, never", r.Policy)
	}
	if r.BackoffMs < 0 {
		return fmt.Errorf("restart.backoff_ms: must be non-negative, got %d", r.BackoffMs)
	}
	if r.MaxBackoffMs < 0 {
		return fmt.Errorf("restart.max_backoff_ms: must be non-negative, got %d", r.MaxBackoffMs)
	}
	if r.BackoffMs > 0 && r.MaxBackoffMs > 0 && r.MaxBackoffMs < r.BackoffMs {
		return fmt.Errorf("restart.max_backoff_ms (%d) must be >= backoff_ms (%d)", r.MaxBackoffMs, r.BackoffMs)
	}
	if r.Multiplier < 0 {
		return fmt.Errorf("restart.multiplier: must be non-negative, got %d", r.Multiplier)
	}
	if r.MaxRetries < 0 {
		return fmt.Errorf("restart.max_retries: must be non-negative, got %d", r.MaxRetries)
	}
	if r.ResetAfterSeconds < 0 {
		return fmt.Errorf("restart.reset_after_seconds: must be non-negative, got %d", r.ResetAfterSeconds)
	}
	return nil
}

func validateStop(s *StopConfig) error {
	if s.GraceSeconds <= 0 {
		return fmt.Errorf("stop.grace_seconds: must be positive, got %d", s.GraceSeconds)
	}
	if s.TimeoutSeconds <= 0 {
		return fmt.Errorf("stop.timeout_seconds: must be positive, got %d", s.TimeoutSeconds)
	}
	return nil
}

func validateLogging(l *LoggingConfig) error {
	if l.MaxSizeMB <= 0 {
		return fmt.Errorf("logging.max_size_mb: must be positive, got %d", l.MaxSizeMB)
	}
	if l.MaxFiles <= 0 {
		return fmt.Errorf("logging.max_files: must be positive, got %d", l.MaxFiles)
	}
	return nil
}

func validateSignals(s *SignalsConfig) error {
	fields := []struct {
		name  string
		value string
	}{
		{"reload", s.Reload},
		{"rotate_logs", s.RotateLogs},
		{"graceful_quit", s.GracefulQuit},
	}
	for _, f := range fields {
		if f.value == "" {
			continue
		}
		sig := strings.ToUpper(f.value)
		if forbiddenSignals[sig] {
			return fmt.Errorf("signals.%s: forbidden signal %q", f.name, f.value)
		}
		if !allowedSignals[sig] {
			return fmt.Errorf("signals.%s: invalid signal %q, allowed: HUP, INT, QUIT, USR1, USR2, PIPE, ALRM, CHLD", f.name, f.value)
		}
	}
	return nil
}

func validatePackage(p *PackageConfig) error {
	if p.Default != "" && p.Default != "include" && p.Default != "exclude" {
		return fmt.Errorf("package.default: invalid value %q, must be include or exclude", p.Default)
	}
	return nil
}
