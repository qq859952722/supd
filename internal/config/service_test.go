package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// helper: write a service.yaml and load it
func loadServiceFromYAML(t *testing.T, content string) (*ServiceConfig, error) {
	t.Helper()
	tmpDir := t.TempDir()
	svcPath := filepath.Join(tmpDir, "service.yaml")
	if err := os.WriteFile(svcPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return LoadService(svcPath)
}

func TestLoadServiceMinimal(t *testing.T) {
	minimal := `
name: my-app
version: "1.0.0"
command:
  - /usr/bin/my-app
`
	sc, err := loadServiceFromYAML(t, minimal)
	if err != nil {
		t.Fatalf("LoadService failed: %v", err)
	}

	if sc.Name != "my-app" {
		t.Errorf("Name = %q, want my-app", sc.Name)
	}
	if sc.Version != "1.0.0" {
		t.Errorf("Version = %q, want 1.0.0", sc.Version)
	}
	if len(sc.Command) != 1 || sc.Command[0] != "/usr/bin/my-app" {
		t.Errorf("Command = %v, want [/usr/bin/my-app]", sc.Command)
	}

	// 默认值
	if sc.Autostart == nil || *sc.Autostart != true {
		t.Error("Autostart should default to true")
	}
	if sc.Icon != "box" {
		t.Errorf("Icon = %q, want box", sc.Icon)
	}
}

func TestLoadServiceFull(t *testing.T) {
	full := `
name: nginx
version: "2.1.0"
description: "Nginx web server"
icon: globe
autostart: false
command:
  - /usr/sbin/nginx
  - -g
  - "daemon off;"
runtime: ""
user: www-data
group: www-data
workdir: /var/lib/nginx
depends_on:
  - redis
tags:
  - web
  - proxy

readiness:
  type: http_check
  url: "http://localhost:80/health"
  expected_status: 200
  interval_seconds: 2
  timeout_seconds: 10

restart:
  policy: on-failure
  backoff_ms: 2000
  max_backoff_ms: 60000
  multiplier: 3
  max_retries: 5
  reset_after_seconds: 600

stop:
  grace_seconds: 15
  timeout_seconds: 120

logging:
  enabled: false
  max_size_mb: 20
  max_files: 10

signals:
  reload: HUP
  rotate_logs: USR1
  graceful_quit: QUIT

package:
  include:
    - service.yaml
    - env.yaml
    - extensions/
  exclude:
    - data/
    - logs/
    - "*.log"
    - .cache/
  default: include
`
	sc, err := loadServiceFromYAML(t, full)
	if err != nil {
		t.Fatalf("LoadService failed: %v", err)
	}

	if sc.Name != "nginx" {
		t.Errorf("Name = %q, want nginx", sc.Name)
	}
	if sc.Version != "2.1.0" {
		t.Errorf("Version = %q, want 2.1.0", sc.Version)
	}
	if sc.Description != "Nginx web server" {
		t.Errorf("Description = %q, want 'Nginx web server'", sc.Description)
	}
	if sc.Icon != "globe" {
		t.Errorf("Icon = %q, want globe", sc.Icon)
	}
	if sc.Autostart == nil || *sc.Autostart != false {
		t.Error("Autostart should be false")
	}
	if sc.User != "www-data" {
		t.Errorf("User = %q, want www-data", sc.User)
	}
	if sc.Group != "www-data" {
		t.Errorf("Group = %q, want www-data", sc.Group)
	}
	if sc.Workdir != "/var/lib/nginx" {
		t.Errorf("Workdir = %q, want /var/lib/nginx", sc.Workdir)
	}
	if len(sc.DependsOn) != 1 || sc.DependsOn[0] != "redis" {
		t.Errorf("DependsOn = %v, want [redis]", sc.DependsOn)
	}
	if len(sc.Tags) != 2 {
		t.Errorf("Tags = %v, want 2 items", sc.Tags)
	}

	// readiness
	if sc.Readiness == nil {
		t.Fatal("Readiness should not be nil")
	}
	if sc.Readiness.Type != "http_check" {
		t.Errorf("Readiness.Type = %q, want http_check", sc.Readiness.Type)
	}
	if sc.Readiness.URL != "http://localhost:80/health" {
		t.Errorf("Readiness.URL = %q, want http://localhost:80/health", sc.Readiness.URL)
	}
	if sc.Readiness.ExpectedStatus != 200 {
		t.Errorf("Readiness.ExpectedStatus = %d, want 200", sc.Readiness.ExpectedStatus)
	}
	if sc.Readiness.IntervalSeconds != 2 {
		t.Errorf("Readiness.IntervalSeconds = %d, want 2", sc.Readiness.IntervalSeconds)
	}
	if sc.Readiness.TimeoutSeconds != 10 {
		t.Errorf("Readiness.TimeoutSeconds = %d, want 10", sc.Readiness.TimeoutSeconds)
	}

	// restart
	if sc.Restart == nil {
		t.Fatal("Restart should not be nil")
	}
	if sc.Restart.Policy != "on-failure" {
		t.Errorf("Restart.Policy = %q, want on-failure", sc.Restart.Policy)
	}
	if sc.Restart.BackoffMs != 2000 {
		t.Errorf("Restart.BackoffMs = %d, want 2000", sc.Restart.BackoffMs)
	}

	// stop
	if sc.Stop == nil {
		t.Fatal("Stop should not be nil")
	}
	if sc.Stop.GraceSeconds != 15 {
		t.Errorf("Stop.GraceSeconds = %d, want 15", sc.Stop.GraceSeconds)
	}
	if sc.Stop.TimeoutSeconds != 120 {
		t.Errorf("Stop.TimeoutSeconds = %d, want 120", sc.Stop.TimeoutSeconds)
	}

	// logging
	if sc.Logging == nil {
		t.Fatal("Logging should not be nil")
	}
	if sc.Logging.Enabled == nil || *sc.Logging.Enabled != false {
		t.Error("Logging.Enabled should be false")
	}
	if sc.Logging.MaxSizeMB != 20 {
		t.Errorf("Logging.MaxSizeMB = %d, want 20", sc.Logging.MaxSizeMB)
	}
	if sc.Logging.MaxFiles != 10 {
		t.Errorf("Logging.MaxFiles = %d, want 10", sc.Logging.MaxFiles)
	}

	// signals
	if sc.Signals == nil {
		t.Fatal("Signals should not be nil")
	}
	if sc.Signals.Reload != "HUP" {
		t.Errorf("Signals.Reload = %q, want HUP", sc.Signals.Reload)
	}
	if sc.Signals.RotateLogs != "USR1" {
		t.Errorf("Signals.RotateLogs = %q, want USR1", sc.Signals.RotateLogs)
	}
	if sc.Signals.GracefulQuit != "QUIT" {
		t.Errorf("Signals.GracefulQuit = %q, want QUIT", sc.Signals.GracefulQuit)
	}

	// package
	if sc.Package == nil {
		t.Fatal("Package should not be nil")
	}
	if len(sc.Package.Include) != 3 {
		t.Errorf("Package.Include = %v, want 3 items", sc.Package.Include)
	}
	if sc.Package.Default != "include" {
		t.Errorf("Package.Default = %q, want include", sc.Package.Default)
	}
}

// --- Name 校验 ---

func TestValidateServiceNameEmpty(t *testing.T) {
	yaml := `
name: ""
version: "1.0.0"
command:
  - /usr/bin/app
`
	_, err := loadServiceFromYAML(t, yaml)
	if err == nil {
		t.Error("expected error for empty name")
	}
}

func TestValidateServiceNameUpperCase(t *testing.T) {
	yaml := `
name: MyAPP
version: "1.0.0"
command:
  - /usr/bin/app
`
	_, err := loadServiceFromYAML(t, yaml)
	if err == nil {
		t.Error("expected error for uppercase name")
	}
}

func TestValidateServiceNameStartWithDigit(t *testing.T) {
	yaml := `
name: 1app
version: "1.0.0"
command:
  - /usr/bin/app
`
	_, err := loadServiceFromYAML(t, yaml)
	if err == nil {
		t.Error("expected error for name starting with digit")
	}
}

func TestValidateServiceNameUnderscore(t *testing.T) {
	yaml := `
name: my_app
version: "1.0.0"
command:
  - /usr/bin/app
`
	_, err := loadServiceFromYAML(t, yaml)
	if err == nil {
		t.Error("expected error for name with underscore")
	}
}

func TestValidateServiceNameValid(t *testing.T) {
	names := []string{"a", "app", "my-app", "app1", "a-b-c"}
	for _, n := range names {
		yaml := "name: " + n + "\nversion: \"1.0.0\"\ncommand:\n  - /usr/bin/app\n"
		_, err := loadServiceFromYAML(t, yaml)
		if err != nil {
			t.Errorf("name %q should be valid, got error: %v", n, err)
		}
	}
}

// TestValidateServiceDependsOnSelfReference K-01-001: 验证 depends_on 自引用检测。
// 规格 §2.1.2: 服务 A 的 depends_on 包含 A 自己，视为循环依赖，应在配置解析阶段拦截。
func TestValidateServiceDependsOnSelfReference(t *testing.T) {
	yaml := `
name: app
version: "1.0.0"
command:
  - /usr/bin/app
depends_on:
  - app
`
	_, err := loadServiceFromYAML(t, yaml)
	if err == nil {
		t.Fatal("expected error for self-referencing depends_on, got nil")
	}
	if !strings.Contains(err.Error(), "self-reference") {
		t.Errorf("expected error containing 'self-reference', got: %v", err)
	}
}

// TestValidateServiceDependsOnOthers K-01-001 补充: 验证非自引用的 depends_on 被允许。
func TestValidateServiceDependsOnOthers(t *testing.T) {
	yaml := `
name: app
version: "1.0.0"
command:
  - /usr/bin/app
depends_on:
  - redis
  - postgres
`
	sc, err := loadServiceFromYAML(t, yaml)
	if err != nil {
		t.Fatalf("expected valid config with non-self depends_on, got error: %v", err)
	}
	if len(sc.DependsOn) != 2 {
		t.Errorf("expected 2 depends_on entries, got %d", len(sc.DependsOn))
	}
}

// --- Version 校验 ---

func TestValidateServiceVersionEmpty(t *testing.T) {
	yaml := `
name: app
version: ""
command:
  - /usr/bin/app
`
	_, err := loadServiceFromYAML(t, yaml)
	if err == nil {
		t.Error("expected error for empty version")
	}
}

// --- Command 校验 ---

func TestValidateServiceCommandEmpty(t *testing.T) {
	yaml := `
name: app
version: "1.0.0"
command: []
`
	_, err := loadServiceFromYAML(t, yaml)
	if err == nil {
		t.Error("expected error for empty command")
	}
}

func TestValidateServiceCommandMissing(t *testing.T) {
	yaml := `
name: app
version: "1.0.0"
`
	_, err := loadServiceFromYAML(t, yaml)
	if err == nil {
		t.Error("expected error for missing command")
	}
}

// --- Readiness 校验 ---

func TestValidateReadinessFdNotify(t *testing.T) {
	yaml := `
name: app
version: "1.0.0"
command:
  - /usr/bin/app
readiness:
  type: fd_notify
  fd: 3
`
	sc, err := loadServiceFromYAML(t, yaml)
	if err != nil {
		t.Fatalf("LoadService failed: %v", err)
	}
	if sc.Readiness.Type != "fd_notify" {
		t.Errorf("Readiness.Type = %q, want fd_notify", sc.Readiness.Type)
	}
	if sc.Readiness.Fd != 3 {
		t.Errorf("Readiness.Fd = %d, want 3", sc.Readiness.Fd)
	}
}

func TestValidateReadinessFdNotifyMissingFd(t *testing.T) {
	yaml := `
name: app
version: "1.0.0"
command:
  - /usr/bin/app
readiness:
  type: fd_notify
`
	_, err := loadServiceFromYAML(t, yaml)
	if err == nil {
		t.Error("expected error for fd_notify without fd")
	}
}

func TestValidateReadinessTcpCheck(t *testing.T) {
	yaml := `
name: app
version: "1.0.0"
command:
  - /usr/bin/app
readiness:
  type: tcp_check
  port: 8080
`
	sc, err := loadServiceFromYAML(t, yaml)
	if err != nil {
		t.Fatalf("LoadService failed: %v", err)
	}
	if sc.Readiness.Type != "tcp_check" {
		t.Errorf("Readiness.Type = %q, want tcp_check", sc.Readiness.Type)
	}
	if sc.Readiness.Port != 8080 {
		t.Errorf("Readiness.Port = %d, want 8080", sc.Readiness.Port)
	}
}

func TestValidateReadinessTcpCheckMissingPort(t *testing.T) {
	yaml := `
name: app
version: "1.0.0"
command:
  - /usr/bin/app
readiness:
  type: tcp_check
`
	_, err := loadServiceFromYAML(t, yaml)
	if err == nil {
		t.Error("expected error for tcp_check without port")
	}
}

func TestValidateReadinessHttpCheck(t *testing.T) {
	yaml := `
name: app
version: "1.0.0"
command:
  - /usr/bin/app
readiness:
  type: http_check
  url: "http://localhost:80/health"
`
	sc, err := loadServiceFromYAML(t, yaml)
	if err != nil {
		t.Fatalf("LoadService failed: %v", err)
	}
	if sc.Readiness.ExpectedStatus != 200 {
		t.Errorf("Readiness.ExpectedStatus default = %d, want 200", sc.Readiness.ExpectedStatus)
	}
}

func TestValidateReadinessHttpCheckMissingUrl(t *testing.T) {
	yaml := `
name: app
version: "1.0.0"
command:
  - /usr/bin/app
readiness:
  type: http_check
`
	_, err := loadServiceFromYAML(t, yaml)
	if err == nil {
		t.Error("expected error for http_check without url")
	}
}

func TestValidateReadinessScript(t *testing.T) {
	yaml := `
name: app
version: "1.0.0"
command:
  - /usr/bin/app
readiness:
  type: script
  check:
    - /usr/bin/curl
    - -f
    - http://localhost/health
`
	sc, err := loadServiceFromYAML(t, yaml)
	if err != nil {
		t.Fatalf("LoadService failed: %v", err)
	}
	if len(sc.Readiness.Check) != 3 {
		t.Errorf("Readiness.Check = %v, want 3 items", sc.Readiness.Check)
	}
}

func TestValidateReadinessScriptMissingCheck(t *testing.T) {
	yaml := `
name: app
version: "1.0.0"
command:
  - /usr/bin/app
readiness:
  type: script
`
	_, err := loadServiceFromYAML(t, yaml)
	if err == nil {
		t.Error("expected error for script without check")
	}
}

func TestValidateReadinessInvalidType(t *testing.T) {
	yaml := `
name: app
version: "1.0.0"
command:
  - /usr/bin/app
readiness:
  type: grpc_check
`
	_, err := loadServiceFromYAML(t, yaml)
	if err == nil {
		t.Error("expected error for invalid readiness type")
	}
}

func TestValidateReadinessDefaults(t *testing.T) {
	yaml := `
name: app
version: "1.0.0"
command:
  - /usr/bin/app
readiness:
  type: tcp_check
  port: 80
`
	sc, err := loadServiceFromYAML(t, yaml)
	if err != nil {
		t.Fatalf("LoadService failed: %v", err)
	}
	if sc.Readiness.IntervalSeconds != 1 {
		t.Errorf("Readiness.IntervalSeconds default = %d, want 1", sc.Readiness.IntervalSeconds)
	}
	if sc.Readiness.TimeoutSeconds != 5 {
		t.Errorf("Readiness.TimeoutSeconds default = %d, want 5", sc.Readiness.TimeoutSeconds)
	}
}

// --- Workdir 校验 ---

func TestValidateWorkdirRelative(t *testing.T) {
	yaml := `
name: app
version: "1.0.0"
command:
  - /usr/bin/app
workdir: relative/path
`
	_, err := loadServiceFromYAML(t, yaml)
	if err == nil {
		t.Error("expected error for relative workdir")
	}
}

func TestValidateWorkdirAbsolute(t *testing.T) {
	yaml := `
name: app
version: "1.0.0"
command:
  - /usr/bin/app
workdir: /var/lib/app
`
	_, err := loadServiceFromYAML(t, yaml)
	if err != nil {
		t.Errorf("absolute workdir should be valid, got error: %v", err)
	}
}

// --- Restart 校验 ---

func TestValidateRestartInvalidPolicy(t *testing.T) {
	yaml := `
name: app
version: "1.0.0"
command:
  - /usr/bin/app
restart:
  policy: sometimes
`
	_, err := loadServiceFromYAML(t, yaml)
	if err == nil {
		t.Error("expected error for invalid restart policy")
	}
}

func TestValidateRestartValidPolicies(t *testing.T) {
	policies := []string{"always", "on-failure", "never"}
	for _, p := range policies {
		yaml := "name: app\nversion: \"1.0.0\"\ncommand:\n  - /usr/bin/app\nrestart:\n  policy: " + p + "\n"
		_, err := loadServiceFromYAML(t, yaml)
		if err != nil {
			t.Errorf("policy %q should be valid, got error: %v", p, err)
		}
	}
}

// --- Stop 校验 ---

func TestValidateStopDefaults(t *testing.T) {
	yaml := `
name: app
version: "1.0.0"
command:
  - /usr/bin/app
stop: {}
`
	sc, err := loadServiceFromYAML(t, yaml)
	if err != nil {
		t.Fatalf("LoadService failed: %v", err)
	}
	if sc.Stop.GraceSeconds != 10 {
		t.Errorf("Stop.GraceSeconds default = %d, want 10", sc.Stop.GraceSeconds)
	}
	if sc.Stop.TimeoutSeconds != 60 {
		t.Errorf("Stop.TimeoutSeconds default = %d, want 60", sc.Stop.TimeoutSeconds)
	}
}

// --- Logging 校验 ---

func TestValidateLoggingDefaults(t *testing.T) {
	yaml := `
name: app
version: "1.0.0"
command:
  - /usr/bin/app
logging: {}
`
	sc, err := loadServiceFromYAML(t, yaml)
	if err != nil {
		t.Fatalf("LoadService failed: %v", err)
	}
	if sc.Logging.Enabled == nil || *sc.Logging.Enabled != true {
		t.Error("Logging.Enabled should default to true")
	}
	if sc.Logging.MaxSizeMB != 10 {
		t.Errorf("Logging.MaxSizeMB default = %d, want 10", sc.Logging.MaxSizeMB)
	}
	if sc.Logging.MaxFiles != 5 {
		t.Errorf("Logging.MaxFiles default = %d, want 5", sc.Logging.MaxFiles)
	}
}

// --- Signals 校验 ---

func TestValidateSignalsForbidden(t *testing.T) {
	forbidden := []string{"TERM", "KILL", "STOP", "CONT", "SEGV", "ABRT", "BUS", "FPE", "ILL"}
	for _, sig := range forbidden {
		yaml := "name: app\nversion: \"1.0.0\"\ncommand:\n  - /usr/bin/app\nsignals:\n  reload: " + sig + "\n"
		_, err := loadServiceFromYAML(t, yaml)
		if err == nil {
			t.Errorf("expected error for forbidden signal %q", sig)
		}
	}
}

func TestValidateSignalsAllowed(t *testing.T) {
	allowed := []string{"HUP", "INT", "QUIT", "USR1", "USR2", "PIPE", "ALRM", "CHLD"}
	for _, sig := range allowed {
		yaml := "name: app\nversion: \"1.0.0\"\ncommand:\n  - /usr/bin/app\nsignals:\n  reload: " + sig + "\n"
		_, err := loadServiceFromYAML(t, yaml)
		if err != nil {
			t.Errorf("signal %q should be allowed, got error: %v", sig, err)
		}
	}
}

func TestValidateSignalsInvalid(t *testing.T) {
	yaml := `
name: app
version: "1.0.0"
command:
  - /usr/bin/app
signals:
  reload: SIGFOO
`
	_, err := loadServiceFromYAML(t, yaml)
	if err == nil {
		t.Error("expected error for invalid signal name")
	}
}

// --- Autostart 语义 ---

func TestAutostartDefaultTrue(t *testing.T) {
	yaml := `
name: app
version: "1.0.0"
command:
  - /usr/bin/app
`
	sc, err := loadServiceFromYAML(t, yaml)
	if err != nil {
		t.Fatalf("LoadService failed: %v", err)
	}
	if sc.Autostart == nil || *sc.Autostart != true {
		t.Error("Autostart should default to true when not set")
	}
}

func TestAutostartExplicitFalse(t *testing.T) {
	yaml := `
name: app
version: "1.0.0"
command:
  - /usr/bin/app
autostart: false
`
	sc, err := loadServiceFromYAML(t, yaml)
	if err != nil {
		t.Fatalf("LoadService failed: %v", err)
	}
	if sc.Autostart == nil || *sc.Autostart != false {
		t.Error("Autostart should be false when explicitly set")
	}
}

func TestAutostartExplicitTrue(t *testing.T) {
	yaml := `
name: app
version: "1.0.0"
command:
  - /usr/bin/app
autostart: true
`
	sc, err := loadServiceFromYAML(t, yaml)
	if err != nil {
		t.Fatalf("LoadService failed: %v", err)
	}
	if sc.Autostart == nil || *sc.Autostart != true {
		t.Error("Autostart should be true when explicitly set")
	}
}

// --- Package 校验 ---

func TestValidatePackageInvalidDefault(t *testing.T) {
	yaml := `
name: app
version: "1.0.0"
command:
  - /usr/bin/app
package:
  default: all
`
	_, err := loadServiceFromYAML(t, yaml)
	if err == nil {
		t.Error("expected error for invalid package.default")
	}
}

func TestValidatePackageValidDefault(t *testing.T) {
	for _, d := range []string{"include", "exclude"} {
		yaml := "name: app\nversion: \"1.0.0\"\ncommand:\n  - /usr/bin/app\npackage:\n  default: " + d + "\n"
		_, err := loadServiceFromYAML(t, yaml)
		if err != nil {
			t.Errorf("package.default %q should be valid, got error: %v", d, err)
		}
	}
}

// --- Load 错误 ---

func TestLoadServiceNonexistent(t *testing.T) {
	_, err := LoadService("/nonexistent/service.yaml")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadServiceInvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	svcPath := filepath.Join(tmpDir, "service.yaml")
	if err := os.WriteFile(svcPath, []byte("{{invalid yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadService(svcPath)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

// --- Restart backoff 校验 ---

func TestValidateRestartMaxBackoffLessThanBackoff(t *testing.T) {
	yaml := `
name: app
version: "1.0.0"
command:
  - /usr/bin/app
restart:
  policy: always
  backoff_ms: 5000
  max_backoff_ms: 2000
`
	_, err := loadServiceFromYAML(t, yaml)
	if err == nil {
		t.Error("expected error for max_backoff_ms < backoff_ms")
	}
}

// --- Logging 数值校验 ---

func TestValidateLoggingInvalidMaxSizeMB(t *testing.T) {
	yaml := `
name: app
version: "1.0.0"
command:
  - /usr/bin/app
logging:
  max_size_mb: -1
`
	_, err := loadServiceFromYAML(t, yaml)
	if err == nil {
		t.Error("expected error for negative max_size_mb")
	}
}

func TestValidateLoggingInvalidMaxFiles(t *testing.T) {
	yaml := `
name: app
version: "1.0.0"
command:
  - /usr/bin/app
logging:
  max_files: -1
`
	_, err := loadServiceFromYAML(t, yaml)
	if err == nil {
		t.Error("expected error for negative max_files")
	}
}
