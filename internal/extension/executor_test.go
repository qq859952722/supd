package extension

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/supdorg/supd/internal/config"
)

// helper: 创建临时测试脚本
func createTestScript(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+content), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

// helper: 创建基本 ExtensionMeta
func testMeta(name, entry string) *config.ExtensionMeta {
	enabled := true
	return &config.ExtensionMeta{
		Name:    name,
		Enabled: &enabled,
		Entry:   entry,
		Actions: []config.Action{
			{ID: "run", Label: "Run", ButtonStyle: "default"},
		},
	}
}

// TestSimpleScriptSuccess 测试简单脚本执行成功
// REQ-F-016: exit 0 → TaskSuccess
func TestSimpleScriptSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")
	scriptPath := createTestScript(t, tmpDir, "success.sh", "echo hello")

	executor := NewExecutor(logDir, tmpDir)
	meta := testMeta("test-ext", scriptPath)
	tc := TriggerContext{
		EventType:     "on_demand",
		TriggerSource: "cli",
		TriggerUser:   "test-user",
		ActionID:      "run",
	}

	result, err := executor.Execute(context.Background(), meta, tc, nil, 1800, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.State != TaskSuccess {
		t.Errorf("expected state %s, got %s", TaskSuccess, result.State)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if result.ExtensionName != "test-ext" {
		t.Errorf("expected extension name test-ext, got %s", result.ExtensionName)
	}
	if result.RunID == "" {
		t.Error("expected non-empty run ID")
	}
	if result.TriggerType != "on_demand" {
		t.Errorf("expected trigger type on_demand, got %s", result.TriggerType)
	}
	if !result.IsTerminal() {
		t.Error("expected result to be terminal")
	}
	if !result.IsSuccess() {
		t.Error("expected result to be success")
	}
}

// TestScriptNonZeroExit 测试脚本退出码非0
// REQ-F-016: exit 非 0 → TaskFailed
func TestScriptNonZeroExit(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")
	scriptPath := createTestScript(t, tmpDir, "fail.sh", "echo error >&2; exit 1")

	executor := NewExecutor(logDir, tmpDir)
	meta := testMeta("test-ext", scriptPath)
	tc := TriggerContext{
		EventType:     "on_demand",
		TriggerSource: "cli",
		TriggerUser:   "test-user",
		ActionID:      "run",
	}

	result, err := executor.Execute(context.Background(), meta, tc, nil, 1800, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.State != TaskFailed {
		t.Errorf("expected state %s, got %s", TaskFailed, result.State)
	}
	if result.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", result.ExitCode)
	}
	if result.IsSuccess() {
		t.Error("expected result to not be success")
	}
}

// TestSupdEnvInjection 测试 SUPD_* 环境变量注入
// REQ-F-016, 2.2.5: 14个SUPD_*变量按适用场景注入
func TestSupdEnvInjection(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	// 创建脚本输出关键 SUPD_* 变量
	scriptContent := `echo "RUN_ID=$SUPD_RUN_ID"
echo "EVENT=$SUPD_EVENT"
echo "TRIGGER_SOURCE=$SUPD_TRIGGER_SOURCE"
echo "TRIGGER_USER=$SUPD_TRIGGER_USER"
echo "EXT_NAME=$SUPD_EXTENSION_NAME"
echo "ACTION=$SUPD_ACTION"
echo "TRIGGER_TIME=$SUPD_TRIGGER_TIME"
`
	scriptPath := createTestScript(t, tmpDir, "env_test.sh", scriptContent)

	executor := NewExecutor(logDir, tmpDir)
	meta := testMeta("my-ext", scriptPath)
	tc := TriggerContext{
		EventType:     "on_demand",
		TriggerSource: "webui",
		TriggerUser:   "admin",
		ActionID:      "run",
	}

	result, err := executor.Execute(context.Background(), meta, tc, nil, 1800, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.State != TaskSuccess {
		t.Errorf("expected success, got %s", result.State)
	}

	// 读取日志文件验证
	logPath := filepath.Join(logDir, "extensions", "my-ext", result.RunID+".log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	logContent := string(data)

	// 验证基础变量
	if !strings.Contains(logContent, "EVENT=on_demand") {
		t.Errorf("expected SUPD_EVENT=on_demand in log, got: %s", logContent)
	}
	if !strings.Contains(logContent, "TRIGGER_SOURCE=webui") {
		t.Errorf("expected SUPD_TRIGGER_SOURCE=webui in log, got: %s", logContent)
	}
	if !strings.Contains(logContent, "TRIGGER_USER=admin") {
		t.Errorf("expected SUPD_TRIGGER_USER=admin in log, got: %s", logContent)
	}
	if !strings.Contains(logContent, "EXT_NAME=my-ext") {
		t.Errorf("expected SUPD_EXTENSION_NAME=my-ext in log, got: %s", logContent)
	}
	if !strings.Contains(logContent, "ACTION=run") {
		t.Errorf("expected SUPD_ACTION=run in log, got: %s", logContent)
	}
	if !strings.Contains(logContent, "RUN_ID=") {
		t.Errorf("expected SUPD_RUN_ID in log, got: %s", logContent)
	}
	if !strings.Contains(logContent, "TRIGGER_TIME=") {
		t.Errorf("expected SUPD_TRIGGER_TIME in log, got: %s", logContent)
	}
}

// TestSupdEnvLifecycle 测试 lifecycle 触发时 SUPD_PHASE 注入
// REQ-F-016: lifecycle 触发时额外注入 SUPD_PHASE
func TestSupdEnvLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	scriptContent := `echo "PHASE=$SUPD_PHASE"
echo "SERVICE=$SUPD_SERVICE"
echo "SERVICE_PID=$SUPD_SERVICE_PID"
`
	scriptPath := createTestScript(t, tmpDir, "lifecycle_test.sh", scriptContent)

	executor := NewExecutor(logDir, tmpDir)
	meta := testMeta("svc-ext", scriptPath)
	tc := TriggerContext{
		EventType:     "service_lifecycle",
		TriggerSource: "service_lifecycle",
		TriggerUser:   "system",
		Phase:         "pre_start",
		ServiceName:   "my-service",
		ServicePID:    12345,
		ActionID:      "run",
	}

	result, err := executor.Execute(context.Background(), meta, tc, nil, 1800, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.State != TaskSuccess {
		t.Errorf("expected success, got %s", result.State)
	}

	// 读取日志
	logPath := filepath.Join(logDir, "services", "my-service", "current")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	logContent := string(data)

	if !strings.Contains(logContent, "PHASE=pre_start") {
		t.Errorf("expected SUPD_PHASE=pre_start in log, got: %s", logContent)
	}
	if !strings.Contains(logContent, "SERVICE=my-service") {
		t.Errorf("expected SUPD_SERVICE=my-service in log, got: %s", logContent)
	}
	if !strings.Contains(logContent, "SERVICE_PID=12345") {
		t.Errorf("expected SUPD_SERVICE_PID=12345 in log, got: %s", logContent)
	}
}

// TestSupdEnvOnFailure 测试 on_failure 时的额外变量注入
// REQ-F-016: on_failure 时注入 SUPD_SERVICE_EXIT_CODE/SUPD_SERVICE_SIGNAL/SUPD_SERVICE_RESTART_COUNT
func TestSupdEnvOnFailure(t *testing.T) {
	tc := TriggerContext{
		EventType:       "service_lifecycle",
		Phase:           "on_failure",
		ServiceName:     "my-service",
		ServicePID:      12345,
		ServiceExitCode: 1,
		ServiceSignal:   15,
		RestartCount:    3,
		ActionID:        "run",
	}

	env := BuildSupdEnv("test-run-id", "test-ext", tc)

	envMap := make(map[string]string)
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		envMap[parts[0]] = parts[1]
	}

	// 验证基础变量
	if envMap["SUPD_EVENT"] != "service_lifecycle" {
		t.Errorf("expected SUPD_EVENT=service_lifecycle, got %s", envMap["SUPD_EVENT"])
	}
	// 验证 lifecycle 变量
	if envMap["SUPD_PHASE"] != "on_failure" {
		t.Errorf("expected SUPD_PHASE=on_failure, got %s", envMap["SUPD_PHASE"])
	}
	// 验证 service_lifecycle 变量
	if envMap["SUPD_SERVICE"] != "my-service" {
		t.Errorf("expected SUPD_SERVICE=my-service, got %s", envMap["SUPD_SERVICE"])
	}
	// 验证 on_failure 变量
	if envMap["SUPD_SERVICE_EXIT_CODE"] != "1" {
		t.Errorf("expected SUPD_SERVICE_EXIT_CODE=1, got %s", envMap["SUPD_SERVICE_EXIT_CODE"])
	}
	if envMap["SUPD_SERVICE_SIGNAL"] != "15" {
		t.Errorf("expected SUPD_SERVICE_SIGNAL=15, got %s", envMap["SUPD_SERVICE_SIGNAL"])
	}
	if envMap["SUPD_SERVICE_RESTART_COUNT"] != "3" {
		t.Errorf("expected SUPD_SERVICE_RESTART_COUNT=3, got %s", envMap["SUPD_SERVICE_RESTART_COUNT"])
	}
}

// TestBuildCommand 测试命令构造
// REQ-F-016, 2.2.5 第3步: <runtime 路径> entry args... 或 entry args...
func TestBuildCommand(t *testing.T) {
	tests := []struct {
		name        string
		runtime     string
		runtimePath string
		entry       string
		args        []string
		want        []string
	}{
		{
			name:        "no runtime no args",
			runtime:     "",
			runtimePath: "",
			entry:       "/bin/sh",
			args:        nil,
			want:        []string{"/bin/sh"},
		},
		{
			name:        "no runtime with args",
			runtime:     "",
			runtimePath: "",
			entry:       "/bin/sh",
			args:        []string{"-c", "echo hello"},
			want:        []string{"/bin/sh", "-c", "echo hello"},
		},
		{
			name:        "with runtime no args",
			runtime:     "python3",
			runtimePath: "/usr/bin/python3",
			entry:       "script.py",
			args:        nil,
			want:        []string{"/usr/bin/python3", "script.py"},
		},
		{
			name:        "with runtime with args",
			runtime:     "python3",
			runtimePath: "/usr/bin/python3",
			entry:       "script.py",
			args:        []string{"--verbose", "input.txt"},
			want:        []string{"/usr/bin/python3", "script.py", "--verbose", "input.txt"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildCommand(tt.runtime, tt.runtimePath, tt.entry, tt.args)
			if len(got) != len(tt.want) {
				t.Errorf("expected %v, got %v", tt.want, got)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("at index %d: expected %q, got %q", i, tt.want[i], got[i])
				}
			}
		})
	}
}

// TestContextCancel 测试 context 取消时任务状态 canceled
// REQ-F-016: context 取消 → TaskCanceled
func TestContextCancel(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	// 创建会 sleep 很久的脚本
	scriptPath := createTestScript(t, tmpDir, "long.sh", "sleep 60")

	executor := NewExecutor(logDir, tmpDir)
	meta := testMeta("test-ext", scriptPath)
	tc := TriggerContext{
		EventType:     "on_demand",
		TriggerSource: "cli",
		TriggerUser:   "test-user",
		ActionID:      "run",
	}

	ctx, cancel := context.WithCancel(context.Background())

	// 在另一个 goroutine 中执行
	resultCh := make(chan *RunResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := executor.Execute(ctx, meta, tc, nil, 1800, nil)
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- result
	}()

	// 等待一小段时间后取消 context
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case result := <-resultCh:
		if result.State != TaskCanceled {
			t.Errorf("expected state %s, got %s", TaskCanceled, result.State)
		}
	case err := <-errCh:
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("test timed out")
	}
}

// TestContextTimeout 测试 context 超时时任务状态 canceled
// REQ-F-019: context 超时属于外部取消 → TaskCanceled；扩展超时由 TimeoutGuard 管理 → TaskTimeout
func TestContextTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	// 创建会 sleep 很久的脚本
	scriptPath := createTestScript(t, tmpDir, "long.sh", "sleep 60")

	executor := NewExecutor(logDir, tmpDir)
	meta := testMeta("test-ext", scriptPath)
	tc := TriggerContext{
		EventType:     "on_demand",
		TriggerSource: "cli",
		TriggerUser:   "test-user",
		ActionID:      "run",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	result, err := executor.Execute(ctx, meta, tc, nil, 1800, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// context 超时属于外部取消，不是扩展超时
	if result.State != TaskCanceled {
		t.Errorf("expected state %s, got %s", TaskCanceled, result.State)
	}
}

// TestLogWrite 测试日志写入
// REQ-F-016: 扩展运行日志写入
func TestLogWrite(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	scriptContent := `echo "line1"
echo "line2" >&2
echo "line3"
`
	scriptPath := createTestScript(t, tmpDir, "log_test.sh", scriptContent)

	executor := NewExecutor(logDir, tmpDir)
	meta := testMeta("test-ext", scriptPath)
	tc := TriggerContext{
		EventType:     "on_demand",
		TriggerSource: "cli",
		TriggerUser:   "test-user",
		ActionID:      "run",
	}

	result, err := executor.Execute(context.Background(), meta, tc, nil, 1800, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.State != TaskSuccess {
		t.Errorf("expected success, got %s", result.State)
	}

	// 验证日志文件存在
	logPath := filepath.Join(logDir, "extensions", "test-ext", result.RunID+".log")
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Errorf("log file does not exist: %s", logPath)
	}
}

// TestGetResult 测试结果查询
// REQ-F-016: GetResult 查询任务状态
func TestGetResult(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")
	scriptPath := createTestScript(t, tmpDir, "simple.sh", "echo hello")

	executor := NewExecutor(logDir, tmpDir)
	meta := testMeta("test-ext", scriptPath)
	tc := TriggerContext{
		EventType:     "on_demand",
		TriggerSource: "cli",
		TriggerUser:   "test-user",
		ActionID:      "run",
	}

	result, err := executor.Execute(context.Background(), meta, tc, nil, 1800, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 通过 GetResult 查询
	stored := executor.GetResult(result.RunID)
	if stored == nil {
		t.Fatal("expected result to be stored")
	}
	if stored.RunID != result.RunID {
		t.Errorf("expected run ID %s, got %s", result.RunID, stored.RunID)
	}
	if stored.State != result.State {
		t.Errorf("expected state %s, got %s", result.State, stored.State)
	}

	// 查询不存在的 runID
	missing := executor.GetResult("nonexistent")
	if missing != nil {
		t.Error("expected nil for nonexistent run ID")
	}
}

// TestListResults 测试列出所有结果
// REQ-F-016: ListResults 列出所有任务状态
func TestListResults(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")
	scriptPath := createTestScript(t, tmpDir, "simple.sh", "echo hello")

	executor := NewExecutor(logDir, tmpDir)
	meta := testMeta("test-ext", scriptPath)
	tc := TriggerContext{
		EventType:     "on_demand",
		TriggerSource: "cli",
		TriggerUser:   "test-user",
		ActionID:      "run",
	}

	result1, _ := executor.Execute(context.Background(), meta, tc, nil, 1800, nil)
	result2, _ := executor.Execute(context.Background(), meta, tc, nil, 1800, nil)

	results := executor.ListResults()
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	ids := map[string]bool{result1.RunID: true, result2.RunID: true}
	for _, r := range results {
		if !ids[r.RunID] {
			t.Errorf("unexpected run ID %s", r.RunID)
		}
	}
}

// TestTaskStateConstants 测试7种任务状态常量
// REQ-F-016, 2.2.10: 7种任务状态锁定
func TestTaskStateConstants(t *testing.T) {
	states := map[TaskState]bool{
		TaskPending:  true,
		TaskRunning:  true,
		TaskSuccess:  true,
		TaskFailed:   true,
		TaskTimeout:  true,
		TaskCanceled: true,
		TaskKilled:   true,
	}
	if len(states) != 7 {
		t.Errorf("expected 7 task states, got %d", len(states))
	}
}

// TestRunResultIsTerminal 测试终态判断
// REQ-F-016: success/failed/timeout/canceled/killed 为终态
func TestRunResultIsTerminal(t *testing.T) {
	terminalStates := []TaskState{TaskSuccess, TaskFailed, TaskTimeout, TaskCanceled, TaskKilled}
	nonTerminalStates := []TaskState{TaskPending, TaskRunning}

	for _, s := range terminalStates {
		r := &RunResult{State: s}
		if !r.IsTerminal() {
			t.Errorf("expected %s to be terminal", s)
		}
	}

	for _, s := range nonTerminalStates {
		r := &RunResult{State: s}
		if r.IsTerminal() {
			t.Errorf("expected %s to not be terminal", s)
		}
	}
}

// TestRunResultIsSuccess 测试成功判断
// REQ-F-016: warning 视为成功
func TestRunResultIsSuccess(t *testing.T) {
	r := &RunResult{State: TaskSuccess}
	if !r.IsSuccess() {
		t.Error("expected TaskSuccess to be success")
	}

	r = &RunResult{State: TaskFailed}
	if r.IsSuccess() {
		t.Error("expected TaskFailed to not be success")
	}

	r = &RunResult{State: TaskRunning}
	if r.IsSuccess() {
		t.Error("expected TaskRunning to not be success")
	}
}

// TestBuildSupdEnvBasic 测试 BuildSupdEnv 基本功能
// REQ-F-016: 14个SUPD_*变量按适用场景注入
func TestBuildSupdEnvBasic(t *testing.T) {
	tc := TriggerContext{
		EventType:     "on_demand",
		TriggerSource: "cli",
		TriggerUser:   "test-user",
		ActionID:      "run",
	}

	env := BuildSupdEnv("test-run-id", "test-ext", tc)

	// on_demand 场景应有7个基础变量
	if len(env) != 7 {
		t.Errorf("expected 7 env vars for on_demand, got %d: %v", len(env), env)
	}

	envMap := make(map[string]string)
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		envMap[parts[0]] = parts[1]
	}

	// 验证基础变量
	if envMap["SUPD_EVENT"] != "on_demand" {
		t.Errorf("expected SUPD_EVENT=on_demand, got %s", envMap["SUPD_EVENT"])
	}
	if envMap["SUPD_RUN_ID"] != "test-run-id" {
		t.Errorf("expected SUPD_RUN_ID=test-run-id, got %s", envMap["SUPD_RUN_ID"])
	}
	if envMap["SUPD_EXTENSION_NAME"] != "test-ext" {
		t.Errorf("expected SUPD_EXTENSION_NAME=test-ext, got %s", envMap["SUPD_EXTENSION_NAME"])
	}
	if envMap["SUPD_ACTION"] != "run" {
		t.Errorf("expected SUPD_ACTION=run, got %s", envMap["SUPD_ACTION"])
	}

	// on_demand 不应有 SUPD_PHASE
	if _, ok := envMap["SUPD_PHASE"]; ok {
		t.Error("on_demand should not have SUPD_PHASE")
	}
}

// TestBuildSupdEnvSupdLifecycle 测试 supd_lifecycle 场景
// REQ-F-016: supd_lifecycle 触发时注入 SUPD_PHASE
func TestBuildSupdEnvSupdLifecycle(t *testing.T) {
	tc := TriggerContext{
		EventType:     "supd_lifecycle",
		TriggerSource: "supd_lifecycle",
		TriggerUser:   "system",
		Phase:         "post_ready",
		ActionID:      "init",
	}

	env := BuildSupdEnv("test-run-id", "test-ext", tc)

	envMap := make(map[string]string)
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		envMap[parts[0]] = parts[1]
	}

	if envMap["SUPD_PHASE"] != "post_ready" {
		t.Errorf("expected SUPD_PHASE=post_ready, got %s", envMap["SUPD_PHASE"])
	}
	// supd_lifecycle 不应有 SUPD_SERVICE
	if _, ok := envMap["SUPD_SERVICE"]; ok {
		t.Error("supd_lifecycle should not have SUPD_SERVICE")
	}
}

// TestCommandEmptyEntry 测试空 entry 情况
func TestCommandEmptyEntry(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	executor := NewExecutor(logDir, tmpDir)
	meta := testMeta("test-ext", "") // 空 entry
	tc := TriggerContext{
		EventType: "on_demand",
		ActionID:  "run",
	}

	result, err := executor.Execute(context.Background(), meta, tc, nil, 1800, nil)
	if err == nil {
		t.Error("expected error for empty entry")
	}
	if result.State != TaskFailed {
		t.Errorf("expected state %s, got %s", TaskFailed, result.State)
	}
}

// TestMergedEnv 测试合并环境变量传递
// REQ-F-016: mergedEnv + SUPD_* 合并传递给进程
func TestMergedEnv(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")

	scriptContent := `echo "MY_VAR=$MY_VAR"
echo "SUPD_RUN_ID=$SUPD_RUN_ID"
`
	scriptPath := createTestScript(t, tmpDir, "env_merge.sh", scriptContent)

	executor := NewExecutor(logDir, tmpDir)
	meta := testMeta("test-ext", scriptPath)
	tc := TriggerContext{
		EventType: "on_demand",
		ActionID:  "run",
	}

	// 模拟生产环境的 mergedEnv 构建方式：
	// os.Environ() + 扩展级别环境变量覆盖，确保子进程拥有 PATH 等必要变量
	mergedEnv := append(os.Environ(), "MY_VAR=hello_world")

	result, err := executor.Execute(context.Background(), meta, tc, mergedEnv, 1800, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.State != TaskSuccess {
		t.Errorf("expected success, got %s", result.State)
	}

	// 读取日志
	logPath := filepath.Join(logDir, "extensions", "test-ext", result.RunID+".log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	logContent := string(data)

	if !strings.Contains(logContent, "MY_VAR=hello_world") {
		t.Errorf("expected MY_VAR=hello_world in log, got: %s", logContent)
	}
}

// TestExecutor_NoGoroutineLeak 验证 Execute 完成后不泄漏 goroutine
// B-04-004: 审计要求 — 在集成测试结束后检查 goroutine 数量（runtime.NumGoroutine()）。
// Execute 内部启动 4 个 goroutine（stdout 解析/stderr 写日志/TimeoutGuard 超时监控/进程等待），
// 均应在 Execute 返回前 join 或在 Stop() 后短时间内退出：
//   - stdout/stderr/wait goroutine 通过 <-stdoutDone / <-stderrDone / <-waitCh 同步等待
//   - TimeoutGuard goroutine 在 Stop() 后通过 stopCh 异步退出（Execute 未调用 Wait()）
//
// 本测试连续执行 3 次短任务，每次结束后短暂睡眠让 TimeoutGuard 的 stop goroutine 退出，
// 最终 GC 并对比 goroutine 数量。若每次 Execute 泄漏 ≥1 个 goroutine，
// 3 次后将累计 ≥3 个泄漏，远超 ±2 容差，测试会失败。
//
// 容差 ±2 用于吸收 runtime 内部 goroutine（如 timer Goroutine、testing 框架自身）的抖动。
func TestExecutor_NoGoroutineLeak(t *testing.T) {
	// 基准 goroutine 数（两次 GC 稳定化）
	runtime.GC()
	runtime.GC()
	baseline := runtime.NumGoroutine()

	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")
	// 短任务脚本：echo 立即返回（~5ms），不会触发任何超时定时器
	scriptPath := createTestScript(t, tmpDir, "quick.sh", "echo hello")

	executor := NewExecutor(logDir, tmpDir)
	meta := testMeta("leak-ext", scriptPath)
	tc := TriggerContext{
		EventType: "on_demand",
		ActionID:  "run",
	}

	const iterations = 3
	for i := 0; i < iterations; i++ {
		result, err := executor.Execute(context.Background(), meta, tc, nil, 1800, nil)
		if err != nil {
			t.Fatalf("iteration %d: Execute failed: %v", i, err)
		}
		if result.State != TaskSuccess {
			t.Fatalf("iteration %d: state = %s, want %s", i, result.State, TaskSuccess)
		}
		// 给 TimeoutGuard 的 stop goroutine 留出退出时间
		// Execute 返回前已调用 Stop()（向 stopCh 发送），但未 Wait() join goroutine。
		// 200ms 远超 select 调度延迟（通常 <1ms），确保 goroutine 真正退出。
		time.Sleep(200 * time.Millisecond)
	}

	// 最终 GC 并检查 goroutine 数
	runtime.GC()
	after := runtime.NumGoroutine()

	if after > baseline+2 {
		t.Errorf("goroutine leak: baseline=%d, after=%d (iterations=%d, tolerance=+2)",
			baseline, after, iterations)
	}
}

// 确保测试脚本存在
func init() {
	// 确保 sh 可用
	if _, err := exec.LookPath("sh"); err != nil {
		panic("sh not found in PATH")
	}
}
