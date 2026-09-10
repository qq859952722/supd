package extension

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/core"
	"github.com/supdorg/supd/internal/logging"
	"github.com/supdorg/supd/internal/notification"
	"github.com/supdorg/supd/internal/stream"
)

// maxRunResults runResults 容量硬编码上限（设计稿 Phase 0.4；不新增配置字段）。
const maxRunResults = 500

// Executor 扩展执行器
// REQ-F-016: 11步执行流程编排
type Executor struct {
	logDir      string
	baseDir     string
	runResults  map[string]*RunResult // key=runID
	insertOrder []string              // runID 插入序（用于有界淘汰，最旧在前）
	mu          sync.RWMutex          // REQ-C-003: runResults 读写互斥

	// REQ-F-028, REQ-F-029: runtime 别名解析
	runtimes           map[string]string // config.yaml 声明的运行时
	discoveredRuntimes map[string]string // 扫描发现的运行时

	// notifySink 通知接收点（预留接线点，由节点 08 替换为真实路由；本节点为测试 Sink）。
	// notifyDropped 队列满时丢弃的 notify 计数。
	notifySink    notification.NotifySink
	notifyDropped atomic.Int64
}

// SetNotifySink 注入通知接收点。扩展 stdout 中合法的 ::notify:: 行写入日志外，
// 还会非阻塞 TryEnqueue 提交通知；nil 或队列满时被忽略（丢弃并计数），
// 不影响扩展执行与新任务状态。
func (e *Executor) SetNotifySink(sink notification.NotifySink) {
	e.notifySink = sink
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

	// Step 3: 构造命令：<runtime 路径> entry 或 entry
	// REQ-F-028, REQ-F-029: runtime 别名解析（三层来源：config > scan > builtin）
	runtimePath := ""
	if meta.Runtime != "" {
		registry := config.BuildRegistryAt(e.baseDir, e.runtimes, e.discoveredRuntimes)
		rt, err := config.Resolve(registry, meta.Runtime)
		if err != nil {
			return nil, fmt.Errorf("extension %s runtime %q: %w", meta.Name, meta.Runtime, err)
		}
		runtimePath = rt.AbsPath
	}
	entryRoot := tc.WorkDir
	if entryRoot == "" {
		entryRoot = e.baseDir
	}
	entryPath := config.ResolvePath(entryRoot, meta.Entry)
	command := BuildCommand(meta.Runtime, runtimePath, entryPath)
	if len(command) == 0 {
		return nil, fmt.Errorf("extension %s: command is empty", meta.Name)
	}

	// Step 4: 设置进程组（Setpgid: true），关闭 stdin — 由 StartProcess 处理
	// Step 5: 设置执行身份（REQ-F-023, 2.2.13: run_as 字段语义，User/UID 模式互斥）
	// REQ-P-005: 非 root 启动 supd 时，run_as 只能切换到当前用户（记录警告）
	isServiceLevel := tc.ServiceName != ""
	uid, gid, groups, warn, rerr := ResolveRunAs(meta.ToCredentialSpec(), tc.ServiceSpec, isServiceLevel)
	if rerr != nil {
		return nil, fmt.Errorf("extension %s: resolve run_as: %w", meta.Name, rerr)
	}
	if warn != "" {
		slog.Warn(warn, "extension", meta.Name, "run_id", runID)
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
	// 设计稿 §六.4/§六.5：stdout 唯一 reader；排水安全读取；notify 非阻塞。
	go func() {
		defer func() { stdoutDone <- struct{}{} }()
		parser := NewProtocolParser()
		rerr := stream.ReadLines(process.StdoutPipe(), executorLineLimit, func(line string) {
			// REQ-F-017: 解析 stdout 协议指令（progress/result 行为保持不变）
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

			// 新增 ::notify:: 协议：非阻塞提交，不阻塞 reader。
			// 协议行但格式/等级非法（isProto && pn==nil）时记一条 warning，
			// 与服务侧 ServiceLogger 行为一致（否则脚本引号写错无任何提示）。
			if pn, isProto := notification.ParseNotifyLine(line); isProto {
				if pn != nil {
					e.enqueueNotify(pn, meta, tc, runID)
				} else if extLogger != nil {
					// Write 内部含时间戳/级别格式化（"[warn]" 触发 detectLevel→warn）；
					// 写失败不告警：磁盘满场景 extLogger 主路径已 slog.Warn，避免重复告警。
					_, _ = extLogger.Write([]byte("[warn] invalid ::notify:: line, treated as log: " + line))
				}
			}

			// 所有行（含协议行）都写日志
			if extLogger != nil {
				// C-01-006: 记录写入错误，不阻塞主流程
				if _, werr := extLogger.Write([]byte(line)); werr != nil {
					slog.Warn("extension log write failed", "extension", meta.Name, "run_id", runID, "log_dir", e.logDir, "stream", "stdout", "error", werr)
				}
			}
		})
		if rerr != nil && !isBenignReadErr(rerr) {
			slog.Warn("extension stdout read error", "extension", meta.Name, "run_id", runID, "error", rerr)
		}
		close(protocolCh)
	}()

	// stderr 读取 goroutine（只写日志，不解析协议；排水安全读取）
	go func() {
		defer func() { stderrDone <- struct{}{} }()
		rerr := stream.ReadLines(process.StderrPipe(), executorLineLimit, func(line string) {
			if extLogger != nil {
				// C-01-006: 记录写入错误，不阻塞主流程
				if _, werr := extLogger.Write([]byte(line)); werr != nil {
					slog.Warn("extension log write failed", "extension", meta.Name, "run_id", runID, "log_dir", e.logDir, "stream", "stderr", "error", werr)
				}
			} else {
				// C-05-002 兜底：extLogger 创建失败时，stderr 透传到 slog 便于诊断
				slog.Info("extension stderr (logger unavailable)", "extension", meta.Name, "run_id", runID, "line", line)
			}
		})
		if rerr != nil && !isBenignReadErr(rerr) {
			slog.Warn("extension stderr read error", "extension", meta.Name, "run_id", runID, "error", rerr)
		}
	}()

	return stdoutDone, stderrDone, protocolCh
}

// isBenignReadErr 判断管道读取错误是否为进程正常退出时的良性终止
// （exec.Cmd 在 Wait/进程退出时会关闭 StdoutPipe/StderrPipe 读端，此时正在
// 排水的 Reader 可能读到 os.ErrClosed/io.ErrClosedPipe）。此类错误不代表
// 真实读取故障，按正常 EOF 处理，不记录告警。
func isBenignReadErr(err error) bool {
	return errors.Is(err, os.ErrClosed) || errors.Is(err, os.ErrInvalid)
}

// enqueueNotify 非阻塞提交通知。来源上下文由 supd 侧填充（脚本不可覆盖）。
// 操作 Run（tc.OperationExecutionID 非空）携带 execution 上下文，NotifyRouter 据此
// 路由到对应操作 Topic；普通 Run 该字段为空 → 路由到扩展/服务默认 Topic。
// Sink 为 nil 或队列满时忽略并计数，不影响扩展执行。
func (e *Executor) enqueueNotify(pn *notification.ParsedNotify, meta *config.ExtensionMeta, tc TriggerContext, runID string) {
	if e.notifySink == nil {
		return
	}
	if !e.notifySink.TryEnqueue(notification.PendingNotification{
		Level:         pn.Level,
		Content:       pn.Content,
		ServiceName:   tc.ServiceName,
		ExtensionName: meta.Name,
		ActionID:      tc.ActionID,
		RunID:         runID,
		ExecutionID:   tc.OperationExecutionID,
	}) {
		e.notifyDropped.Add(1)
	}
}

// executorLineLimit 扩展 stdout/stderr 单行读取字节上限（协议窗口 8KB）。
const executorLineLimit = 8192

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
			"run_as_uid", meta.RunAsUID,
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
		timedOut, _ := timeoutGuard.Check()
		if timedOut {
			// 终态按触发原因分类：超时即使升级为 SIGKILL 仍是 timeout。
			result.State = TaskTimeout
			wr = <-waitCh
			result.ExitCode = wr.exitCode
		} else {
			if err := process.SendSignal(syscall.SIGTERM); err != nil {
				slog.Warn("failed to send SIGTERM on context cancel", "extension", meta.Name, "run_id", ec.runID, "error", err)
			}
			select {
			case wr = <-waitCh:
				// 进程响应 SIGTERM 在 5s 内退出 → canceled
				result.State = TaskCanceled
				result.ExitCode = wr.exitCode
			case <-time.After(5 * time.Second):
				// 5s 后仍未退出 → SIGKILL 强杀 → killed
				slog.Warn("process did not exit 5s after SIGTERM, sending SIGKILL", "extension", meta.Name, "run_id", ec.runID)
				// C-05-001: 记录 KillProcessGroup 错误，与 timeout.go 保持一致
				if err := process.KillProcessGroup(); err != nil {
					slog.Warn("failed to send SIGKILL after context cancel", "extension", meta.Name, "run_id", ec.runID, "error", err)
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
		timedOut, _ := timeoutGuard.Check()
		if timedOut {
			// SIGKILL 仅是终止手段；超时触发的终态始终是 timeout。
			result.State = TaskTimeout
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
			slog.Warn("extension log close failed", "extension", meta.Name, "run_id", ec.runID, "log_dir", e.logDir, "error", cerr)
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
// 退化为当前 maxRunResults 内的记录，按插入序从最旧到最新返回。
func (e *Executor) ListResults() []*RunResult {
	e.mu.RLock()
	defer e.mu.RUnlock()

	results := make([]*RunResult, 0, len(e.insertOrder))
	for _, runID := range e.insertOrder {
		if r, ok := e.runResults[runID]; ok {
			results = append(results, r)
		}
	}
	return results
}

// storeResult 存储运行结果（内部方法，加写锁）
// 有界淘汰：容量达 maxRunResults 时按插入序淘汰最旧记录（设计稿 Phase 0.4）。
func (e *Executor) storeResult(result *RunResult) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.runResults[result.RunID]; !exists {
		// 新 runID：按插入序入队；容量已满时淘汰最旧记录。
		if len(e.insertOrder) >= maxRunResults && len(e.runResults) >= maxRunResults {
			oldest := e.insertOrder[0]
			e.insertOrder = e.insertOrder[1:]
			delete(e.runResults, oldest)
		}
		e.insertOrder = append(e.insertOrder, result.RunID)
	}
	e.runResults[result.RunID] = result
}
