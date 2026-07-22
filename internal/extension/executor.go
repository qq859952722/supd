package extension

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/core"
	"github.com/supdorg/supd/internal/logging"
)

// Executor 扩展执行器
// REQ-F-016: 11步执行流程编排
type Executor struct {
	logDir     string
	baseDir    string
	runResults map[string]*RunResult // key=runID
	mu         sync.RWMutex          // REQ-C-003: runResults 读写互斥

	// REQ-F-028, REQ-F-029: runtime 别名解析
	runtimes           map[string]string // config.yaml 声明的运行时
	discoveredRuntimes map[string]string // 扫描发现的运行时
}

// NewExecutor 创建扩展执行器
// REQ-F-016: 初始化执行器，logDir 为日志根目录（如 /var/log/supd）
func NewExecutor(logDir, baseDir string) *Executor {
	return &Executor{
		logDir:     logDir,
		baseDir:    baseDir,
		runResults: make(map[string]*RunResult),
	}
}

// SetRuntimes 设置运行时配置，用于 runtime 别名解析
// REQ-F-028: 三层运行时来源（config > scan > builtin）
// REQ-F-029: 运行时可用性校验
func (e *Executor) SetRuntimes(configRuntimes map[string]string, discoveredRuntimes map[string]string) {
	e.runtimes = configRuntimes
	e.discoveredRuntimes = discoveredRuntimes
}

// execContext 构建后的执行上下文（Steps 1-5 输出）
type execContext struct {
	runID      string
	envSlice   []string
	command    []string
	credential *syscall.Credential
	workDir    string
}

// buildExecContext 构建执行上下文（Steps 2-5）
// REQ-F-016: 环境变量合并、命令构建、run_as 解析、工作目录确定
// runID 由调用方在 Step 1 生成后传入
func (e *Executor) buildExecContext(runID string, meta *config.ExtensionMeta, tc TriggerContext, mergedEnv []string) (*execContext, error) {
	// Step 2: 构造环境变量：os.Environ()（基础）+ 合并层级 env + 注入的 SUPD_* 上下文变量
	// 后者可覆盖同名系统变量（如 env.yaml 中定义 PATH 会覆盖系统 PATH）
	supdEnv := BuildSupdEnv(runID, meta.Name, tc)
	envSlice := make([]string, 0, len(os.Environ())+len(mergedEnv)+len(supdEnv))
	envSlice = append(envSlice, os.Environ()...)
	envSlice = append(envSlice, mergedEnv...)
	envSlice = append(envSlice, supdEnv...)

	// Step 3: 构造命令：<runtime 路径> entry args... 或 entry args...
	// REQ-F-028, REQ-F-029: runtime 别名解析（三层来源：config > scan > builtin）
	runtimePath := meta.Runtime
	if meta.Runtime != "" && (e.runtimes != nil || e.discoveredRuntimes != nil) {
		registry := config.BuildRegistry(e.runtimes, e.discoveredRuntimes)
		if rt, err := config.Resolve(registry, meta.Runtime); err == nil && rt.Available {
			runtimePath = rt.AbsPath
		}
	}
	command := BuildCommand(meta.Runtime, runtimePath, meta.Entry, tc.ActionArgs)
	if len(command) == 0 {
		return nil, fmt.Errorf("extension %s: command is empty", meta.Name)
	}

	// Step 4: 设置进程组（Setpgid: true），关闭 stdin — 由 StartProcess 处理
	// Step 5: 设置执行身份（REQ-F-023, 2.2.13: run_as 字段语义）
	// REQ-P-005: 非 root 启动 supd 时，run_as 只能切换到当前用户（记录警告）
	isServiceLevel := tc.ServiceName != ""
	uid, gid, groups, warn, rerr := ResolveRunAs(meta.RunAs, tc.ServiceUser, isServiceLevel)
	if rerr != nil {
		return nil, fmt.Errorf("extension %s: resolve run_as: %w", meta.Name, rerr)
	}
	if warn != "" {
		slog.Warn(warn, "extension", meta.Name)
	}
	var credential *syscall.Credential
	// 仅当目标用户不是当前用户时才设置 Credential（避免不必要的身份切换）
	if uid != uint32(os.Getuid()) || gid != uint32(os.Getgid()) {
		credential = BuildCredential(uid, gid, groups)
	}

	// 工作目录：优先使用 TriggerContext.WorkDir，为空时回退到 Executor.baseDir
	workDir := e.baseDir
	if tc.WorkDir != "" {
		workDir = tc.WorkDir
	}

	return &execContext{
		runID:      runID,
		envSlice:   envSlice,
		command:    command,
		credential: credential,
		workDir:    workDir,
	}, nil
}

// protocolUpdate stdout 协议解析结果（通过 channel 传递，避免并发写 result）
type protocolUpdate struct {
	progress    int
	resultLevel string
	resultMsg   string
	hasProgress bool
	hasResult   bool
}

// startOutputGoroutines 启动 stdout/stderr 读取 goroutine（Steps 7-8）
// REQ-F-017: stdout 解析 ::progress:: 和 ::result:: 协议指令
// 进度更新仅通过 onProgress 回调实时转发，不发 channel（避免缓冲区满阻塞）
// protocolCh 仅用于 result 指令（最多1条，不会阻塞）
func (e *Executor) startOutputGoroutines(meta *config.ExtensionMeta, tc TriggerContext, runID string,
	process *core.Process, extLogger *logging.ExtensionLogger, onProgress ProgressCallback) (
	stdoutDone, stderrDone chan struct{}, protocolCh chan protocolUpdate) {

	stdoutDone = make(chan struct{}, 1)
	stderrDone = make(chan struct{}, 1)
	protocolCh = make(chan protocolUpdate, 64)

	// stdout 读取 goroutine — REQ-F-017: 解析 ::progress:: 和 ::result:: 协议指令
	go func() {
		defer func() { stdoutDone <- struct{}{} }()
		parser := NewProtocolParser()
		scanner := bufio.NewScanner(process.StdoutPipe())
		for scanner.Scan() {
			line := scanner.Text()
			// REQ-F-017: 解析 stdout 协议指令
			parsed := parser.Feed(line)
			switch parsed.Type {
			case LineTypeProgress:
				p := parser.Progress()
				// 实时回调：更新 TaskManager 中的进度
				if onProgress != nil {
					onProgress(p, "")
				}
			case LineTypeResult:
				update := protocolUpdate{hasResult: true}
				if p := parser.Progress(); p > 0 {
					update.progress = p
					update.hasProgress = true
				}
				if r := parser.Result(); r != nil {
					update.resultLevel = r.ResultStatus
					update.resultMsg = r.Message
				}
				protocolCh <- update
				// 实时回调：更新 TaskManager 中的结果消息
				if onProgress != nil {
					onProgress(update.progress, update.resultMsg)
				}
			}
			// 所有行（含协议行）都写日志
			if extLogger != nil {
				// C-01-006: 记录写入错误，不阻塞主流程
				if _, werr := extLogger.Write([]byte(line)); werr != nil {
					slog.Warn("extension log write failed", "extension", meta.Name, "stream", "stdout", "error", werr)
				}
			}
		}
		close(protocolCh)
	}()

	// stderr 读取 goroutine
	go func() {
		defer func() { stderrDone <- struct{}{} }()
		scanner := bufio.NewScanner(process.StderrPipe())
		for scanner.Scan() {
			line := scanner.Bytes()
			if extLogger != nil {
				// C-01-006: 记录写入错误，不阻塞主流程
				if _, werr := extLogger.Write(line); werr != nil {
					slog.Warn("extension log write failed", "extension", meta.Name, "stream", "stderr", "error", werr)
				}
			} else {
				// C-05-002 兜底：extLogger 创建失败时，stderr 透传到 slog 便于诊断
				slog.Info("extension stderr (logger unavailable)", "extension", meta.Name, "run_id", runID, "line", string(line))
			}
		}
	}()

	return stdoutDone, stderrDone, protocolCh
}

// determineFinalState 判定最终任务状态（Step 11）
// A-03-001 修复：最终状态判定优先级 — timeout/killed/canceled > ::result:: 协议 > exit code
// 仅在非 timeout/killed/canceled 情况下，根据协议或 exit code 判定 success/failed
func determineFinalState(result *RunResult, wr waitResult,
	hasResultProtocol bool, protocolResultLevel, protocolResultMsg string,
	hasProtocolProgress bool, protocolProgress int) {

	if result.State != "" && result.State != TaskRunning {
		// 状态已由 timeout/killed/canceled 路径确定，不再覆盖
		return
	}

	if hasResultProtocol {
		// 有 ::result:: 协议输出，以协议为准
		result.ResultLevel = protocolResultLevel
		result.ResultMsg = protocolResultMsg
		if hasProtocolProgress {
			result.Progress = protocolProgress
		}
		// 根据协议级别判定最终状态
		switch protocolResultLevel {
		case "success":
			result.State = TaskSuccess
		case "warning":
			// warning 视为成功（扩展通过协议报告非致命警告）
			result.State = TaskSuccess
		case "error":
			result.State = TaskFailed
			result.ExitCode = wr.exitCode
		default:
			// 未知级别，回退到 exit code 判定
			if wr.exitCode == 0 {
				result.State = TaskSuccess
			} else {
				result.State = TaskFailed
				result.ExitCode = wr.exitCode
			}
		}
	} else {
		// 无 ::result:: 协议输出，用 exit code 判定
		if wr.exitCode == 0 {
			result.State = TaskSuccess
			result.ResultLevel = "success"
		} else {
			result.State = TaskFailed
			result.ExitCode = wr.exitCode
			result.ResultMsg = fmt.Sprintf("exit code %d", wr.exitCode)
			result.ResultLevel = "error"
		}
	}
}

// Execute 执行扩展任务，按11步流程编排
// REQ-F-016, 2.2.5: 扩展执行11步完整流程
// REQ-F-019: hardLimitSeconds 为 config.yaml 的 extension_hard_limit_seconds（默认1800）
// onProgress: 进度回调，可为 nil；stdout goroutine 解析 ::progress:: / ::result:: 时调用
func (e *Executor) Execute(ctx context.Context, meta *config.ExtensionMeta, tc TriggerContext, mergedEnv []string, hardLimitSeconds int, onProgress ProgressCallback) (*RunResult, error) {
	// Step 1: 生成 run_id（UUID）— 优先使用调用方预生成的 RunID
	runID := tc.RunID
	if runID == "" {
		runID = uuid.New().String()
	}
	startedAt := time.Now()

	// 构建结果对象
	result := &RunResult{
		RunID:         runID,
		ExtensionName: meta.Name,
		ActionID:      tc.ActionID,
		State:         TaskRunning,
		StartedAt:     startedAt,
		TriggerType:   tc.EventType,
		ServiceName:   tc.ServiceName,
	}

	// Steps 2-5: 构建执行上下文（环境变量、命令、run_as、工作目录）
	ec, err := e.buildExecContext(runID, meta, tc, mergedEnv)
	if err != nil {
		result.State = TaskFailed
		result.ExitCode = -1
		// N-04-USER-CRED 修复：run_as 解析失败（如用户不存在）时填充 ResultMsg，
		// 让前端能直接看到错误原因和解决方法（用户要求"详细的记录并提示错误原因和解决方法"）
		result.ResultMsg = err.Error()
		result.ResultLevel = "error"
		result.FinishedAt = time.Now()
		e.storeResult(result)
		slog.Error("extension build context failed",
			"extension", meta.Name,
			"run_as", meta.RunAs,
			"service_user", tc.ServiceUser,
			"service", tc.ServiceName,
			"error", err)
		return result, err
	}

	// Step 6: 启动进程
	process, err := core.StartProcess(
		fmt.Sprintf("ext:%s[%s]", meta.Name, ec.runID[:8]),
		ec.command,
		ec.envSlice,
		ec.workDir,
		ec.credential,
	)
	if err != nil {
		result.State = TaskFailed
		result.ExitCode = -1
		// 同上：填充 ResultMsg 让前端可见
		result.ResultMsg = fmt.Sprintf("start process failed: %v", err)
		result.ResultLevel = "error"
		result.FinishedAt = time.Now()
		e.storeResult(result)
		return result, fmt.Errorf("extension %s: start process failed: %w", meta.Name, err)
	}

	// Steps 7-8: 启动 stdout/stderr 读取 goroutine + 创建扩展日志器
	extLogger, err := logging.NewExtensionLogger(logging.ExtensionLogConfig{
		ExtName:        meta.Name,
		IsServiceLevel: tc.ServiceName != "",
		ServiceName:    tc.ServiceName,
		RunID:          ec.runID,
		LogRootDir:     e.logDir,
	})
	if err != nil {
		// 日志器创建失败不影响主流程
		extLogger = nil
	}

	stdoutDone, stderrDone, protocolCh := e.startOutputGoroutines(
		meta, tc, ec.runID, process, extLogger, onProgress)

	// Step 9: 启动超时定时器
	// REQ-F-019, 2.2.8: 三层防线 — extension timeout → SIGTERM(5s) → SIGKILL；硬上限直接SIGKILL
	timeoutGuard := NewTimeoutGuard(TimeoutConfig{
		ExtensionTimeout: meta.TimeoutSeconds,
		HardLimitSeconds: hardLimitSeconds,
	}, process)
	timeoutGuard.Start(ctx)

	// Step 10: 阻塞等待 cmd.Wait() 返回
	waitCh := make(chan waitResult, 1)
	go func() {
		exitCode, signaled, sig := process.Wait()
		waitCh <- waitResult{exitCode: exitCode, signaled: signaled, sig: sig}
	}()

	// 同时监听 context 取消和进程退出
	var wr waitResult
	select {
	case <-ctx.Done():
		// C-05-01/02 + A-04-001 修复：实现规格 §2.2.7 的 SIGTERM → 5s → SIGKILL 流程
		// ctx 被取消（用户取消或 replace 策略取消前任务）时：
		//   1. 先发 SIGTERM 优雅终止，等待进程退出（最多 5s）
		//   2. 5s 内退出 → canceled（规格 §2.2.10: 主动取消）
		//   3. 5s 后仍未退出 → SIGKILL 强杀 → killed（规格 §2.2.10: 被 SIGKILL）
		timeoutGuard.Stop()
		// N-C-01 修复：race 场景 — timeout 与 ctx.Done 近似同时触发时，
		// 优先判定为 timeout/killed（避免误标为 canceled）
		timedOut, hardKilled := timeoutGuard.Check()
		if timedOut {
			if hardKilled {
				result.State = TaskKilled
			} else {
				result.State = TaskTimeout
			}
			wr = <-waitCh
			result.ExitCode = wr.exitCode
		} else {
			if err := process.SendSignal(syscall.SIGTERM); err != nil {
				slog.Warn("failed to send SIGTERM on context cancel", "error", err)
			}
			select {
			case wr = <-waitCh:
				// 进程响应 SIGTERM 在 5s 内退出 → canceled
				result.State = TaskCanceled
				result.ExitCode = wr.exitCode
			case <-time.After(5 * time.Second):
				// 5s 后仍未退出 → SIGKILL 强杀 → killed
				slog.Warn("process did not exit 5s after SIGTERM, sending SIGKILL")
				// C-05-001: 记录 KillProcessGroup 错误，与 timeout.go 保持一致
				if err := process.KillProcessGroup(); err != nil {
					slog.Warn("failed to send SIGKILL after context cancel", "extension", meta.Name, "error", err)
				}
				wr = <-waitCh
				result.State = TaskKilled
				result.ExitCode = wr.exitCode
			}
		}
	case wr = <-waitCh:
		// 进程退出，停止超时监控
		timeoutGuard.Stop()
		// Step 11 (part 1): 判定 timeout/killed/canceled 状态
		timedOut, hardKilled := timeoutGuard.Check()
		if timedOut {
			if hardKilled {
				result.State = TaskKilled
			} else {
				result.State = TaskTimeout
			}
			result.ExitCode = wr.exitCode
		} else if wr.signaled {
			if wr.sig == syscall.SIGKILL {
				result.State = TaskKilled
			} else {
				result.State = TaskCanceled
			}
			result.ExitCode = wr.exitCode
		}
		// A-03-001 修复：不在此处用 exit code 判定 success/failed，
		// 先读取 ::result:: 协议输出，优先以协议为准（§2.2.10）
	}

	// 等待 stdout/stderr goroutine 完成
	<-stdoutDone
	<-stderrDone

	// 从 protocolCh 读取所有更新，在主 goroutine 中安全写入 result
	hasResultProtocol := false
	var protocolResultLevel string
	var protocolResultMsg string
	var protocolProgress int
	hasProtocolProgress := false
	for update := range protocolCh {
		if update.hasProgress {
			result.Progress = update.progress
			protocolProgress = update.progress
			hasProtocolProgress = true
		}
		if update.hasResult {
			hasResultProtocol = true
			protocolResultLevel = update.resultLevel
			protocolResultMsg = update.resultMsg
		}
	}

	// Step 11 (part 2): 判定最终状态（协议 > exit code）
	determineFinalState(result, wr, hasResultProtocol, protocolResultLevel, protocolResultMsg, hasProtocolProgress, protocolProgress)

	// 关闭日志器
	if extLogger != nil {
		// C-01-006: 记录关闭错误，不影响任务结果
		if cerr := extLogger.Close(); cerr != nil {
			slog.Warn("extension log close failed", "extension", meta.Name, "error", cerr)
		}
	}

	result.FinishedAt = time.Now()
	e.storeResult(result)

	return result, nil
}

// waitResult 进程等待结果
type waitResult struct {
	exitCode int
	signaled bool
	sig      syscall.Signal
}

// GetResult 根据 runID 获取运行结果
// REQ-F-016: 查询任务状态
func (e *Executor) GetResult(runID string) *RunResult {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.runResults[runID]
}

// ListResults 列出所有运行结果
// REQ-F-016: 列出所有任务状态
func (e *Executor) ListResults() []*RunResult {
	e.mu.RLock()
	defer e.mu.RUnlock()

	results := make([]*RunResult, 0, len(e.runResults))
	for _, r := range e.runResults {
		results = append(results, r)
	}
	return results
}

// storeResult 存储运行结果（内部方法，加写锁）
func (e *Executor) storeResult(result *RunResult) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.runResults[result.RunID] = result
}
