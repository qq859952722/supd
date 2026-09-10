package extension

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/supdorg/supd/internal/watch"
)

// RunSpec 一次 Run 提交所需的完整描述；不新增第二套参数模型，复用既有 TriggerContext。
type RunSpec struct {
	// ServiceName 服务名；全局扩展为空字符串。
	ServiceName string
	// ExtensionName 扩展名（在 ServiceName 作用域内查找；全局扩展时按全局扩展名查找）。
	ExtensionName string
	// ActionID action id；为空时按既有规则取第一个 action。
	ActionID string
	// Context 触发上下文；调用方可注入操作参数（TempEnv 等）与 RunID。
	// Context.RunID 必须等于 SubmitRun 的 runID；操作参数通过 Context 的操作字段注入。
	Context TriggerContext
}

// TerminalResult 统一终态结果。复用既有任务终态结构（state+时间+摘要），不新增状态枚举。
type TerminalResult struct {
	RunID         string
	State         TaskState
	ServiceName   string
	ExtensionName string
	ActionID      string
	StartedAt     time.Time
	FinishedAt    time.Time
	ResultMsg     string
	ResultLevel   string
	ExitCode      int
}

// newTerminalResult 从既有 RunResult 构造统一终态结果。
func newTerminalResult(r *RunResult) TerminalResult {
	return TerminalResult{
		RunID:         r.RunID,
		State:         r.State,
		ServiceName:   r.ServiceName,
		ExtensionName: r.ExtensionName,
		ActionID:      r.ActionID,
		StartedAt:     r.StartedAt,
		FinishedAt:    r.FinishedAt,
		ResultMsg:     r.ResultMsg,
		ResultLevel:   r.ResultLevel,
		ExitCode:      r.ExitCode,
	}
}

// RunGateway 统一 Run 提交与终态回调（设计稿 §四.5）。
// 实现类包装现有 Dispatcher 路径（内部复用既有 executeWithConcurrency 触发函数），
// 不新增第二套执行模型。既有 on_demand/cron/lifecycle 触发路径保持原样，本接口作为
// 新增并行入口，与其汇聚到同一底层执行与收口函数。
type RunGateway interface {
	// SubmitRun 提交一个 Run；runID 由调用方预生成并全链路复用，Gateway 不覆盖。
	// 返回 accepted=false/error 表示提交失败；若该 Run 已有 planned 预登记，
	// 则更新该记录为 failed 并收口（幂等，不新增第二条）；未预登记的普通 Run 不创建记录。
	SubmitRun(spec RunSpec, runID string) (accepted bool, err error)

	// OnRunTerminal 注册终态回调；同一 runID 至多触发一次（幂等）。
	// result 含终态 state（七种既有任务状态之一）与摘要信息。
	// 若回调注册晚于终态发生（先终态后注册），注册时立即补调一次。
	OnRunTerminal(runID string, fn func(result TerminalResult))
}

// runGateway 实现 RunGateway。
//
// 生命周期边界：回调注册表为进程内存态，supd 重启后随进程重建，本实现不承诺
// 跨重启投递；重启后由 Operation 持久层状态（Store）接管恢复，Runner 等待逻辑
// 只存在于进程内。
type runGateway struct {
	dispatcher *Dispatcher
	taskMgr    *TaskManager
	discovery  *watch.DiscoveryResult // 供解析扩展 meta；宿主在热重载时更新引用

	mu        sync.Mutex
	callbacks map[string]func(TerminalResult) // runID -> 尚未触发的回调
	cached    map[string]TerminalResult       // runID -> 终态先于注册时缓存的结果
	fired     map[string]bool                 // runID -> 终态回调已投递
}

// NewRunGateway 创建 RunGateway。
// dispatcher 提供既有触发/收口路径；taskMgr 记录终态（可为 nil，此时仅回调不落历史）；
// discovery 提供扩展 meta 解析（热重载时宿主应更新引用）。
func NewRunGateway(dispatcher *Dispatcher, taskMgr *TaskManager, discovery *watch.DiscoveryResult) RunGateway {
	return &runGateway{
		dispatcher: dispatcher,
		taskMgr:    taskMgr,
		discovery:  discovery,
		callbacks:  make(map[string]func(TerminalResult)),
		cached:     make(map[string]TerminalResult),
		fired:      make(map[string]bool),
	}
}

// SetDiscovery 热重载后更新 Discovery 引用（按服务/扩展名解析 Run 目标扩展）。
// 仅存在于具体实现，RunGateway 接口不含此方法；宿主在热重载时类型断言调用。
func (g *runGateway) SetDiscovery(d *watch.DiscoveryResult) {
	if d == nil {
		return
	}
	g.discovery = d
}

// SubmitRun 提交一个 Run。
// 任务 04-2：pre-generated RunID 全链路复用（内部不覆盖生成）；缺失 RunID 直接拒绝。
// 任务 04-3：统一终态收口入口——所有路径的终态记录与回调均在此汇聚。
func (g *runGateway) SubmitRun(spec RunSpec, runID string) (bool, error) {
	// 04-2：RunID 由调用方单点生成；缺失直接返回 accepted=false/error。
	if runID == "" {
		return false, errors.New("run gateway: runID is required")
	}

	if g.dispatcher == nil {
		return g.failSubmit(spec, runID, errors.New("run gateway: dispatcher not available"))
	}
	if g.discovery == nil {
		return g.failSubmit(spec, runID, errors.New("run gateway: discovery not available"))
	}

	// 按 (service, extension) 精确解析扩展（服务作用域优先，同 trigger_on_demand 语义）。
	// 服务阶段 Run（spec.ServiceName 非空）必须在该服务内解析：不同服务可部署同名扩展，
	// 避免解析到错误服务的同名扩展（导致 SUPD_SERVICE/SUPD_SERVICE_DIR 归属错误）。
	var extEntry *watch.ExtensionEntry
	var svcName string
	var err error
	if spec.ServiceName != "" {
		if svc, ok := g.discovery.Services[spec.ServiceName]; ok {
			if ext, ok := svc.Extensions[spec.ExtensionName]; ok {
				extEntry, svcName = ext, spec.ServiceName
			}
		}
		if extEntry == nil {
			return g.failSubmit(spec, runID, fmt.Errorf("service %s: extension %s not found", spec.ServiceName, spec.ExtensionName))
		}
	} else {
		extEntry, svcName, err = findExtensionByName(g.discovery, spec.ExtensionName)
		if err != nil {
			return g.failSubmit(spec, runID, err)
		}
	}
	if extEntry.Meta == nil {
		return g.failSubmit(spec, runID, fmt.Errorf("extension %s: meta.yaml parse failed, cannot trigger", spec.ExtensionName))
	}
	meta := extEntry.Meta

	// 校验/解析 actionID。
	actionID := spec.ActionID
	if actionID == "" {
		actionID = FindActionByID(meta, "")
	} else if FindActionByID(meta, actionID) != actionID {
		return g.failSubmit(spec, runID, fmt.Errorf("extension %s: action %s not found", spec.ExtensionName, actionID))
	}

	// 构建触发上下文：以调用方 spec.Context 为基础，补充服务作用域与预生成 RunID。
	tc := spec.Context
	if tc.EventType == "" {
		tc.EventType = "on_demand"
	}
	if tc.TriggerSource == "" {
		tc.TriggerSource = "webui"
	}
	tc.ActionID = actionID
	tc.RunID = runID // 04-2：内部不覆盖调用方预生成的 runID
	tc.ServiceName = svcName
	tc.ServiceLevel = extEntry.ServiceName != ""
	if tc.WorkDir == "" {
		tc.WorkDir = filepath.Dir(extEntry.ConfigPath)
	}
	if tc.ExtensionEnvPath == "" {
		tc.ExtensionEnvPath = extEntry.EnvPath
	}
	if svcName != "" {
		if tc.ServiceDir == "" {
			tc.ServiceDir = filepath.Join(g.dispatcher.baseDir, "services", svcName)
		}
		// 服务级扩展默认 run_as 继承服务身份（与 on_demand/schedule 路径一致）；
		// 调用方已显式配置身份时保留调用方值。
		if spec.Context.ServiceSpec.IsEmpty() {
			if svcEntry, ok := g.discovery.Services[svcName]; ok && svcEntry.Config != nil {
				tc.ServiceSpec = svcEntry.Config.ToCredentialSpec()
			}
		}
	}

	// 复用既有执行/收口路径（同一底层函数，汇聚同一收口点）。
	result, runErr := g.dispatcher.executeWithConcurrency(context.Background(), meta, tc, actionID, nil)

	// 统一终态收口：result 非空（含被拒/被替换/killed的既有终态）→ 记录并回拨；
	// result 为空且错误 → 若已有 planned 预登记则更新为 failed 收口，否则不创建记录。
	g.recordAndNotify(spec, runID, result, runErr)

	if result != nil {
		// 该 Run 已获取终态（success/failed/timeout/canceled/killed），进入收口。
		// 提交层面的错误（如 serialize 队列满）同时透出，但 Run 已取得终态。
		if runErr != nil {
			return true, runErr
		}
		return true, nil
	}
	if runErr != nil {
		return false, runErr
	}
	return false, nil
}

// failSubmit 提交前置失败（meta 缺失/discovery 缺失等）：不启动执行，直接按 04-3-4 收口。
func (g *runGateway) failSubmit(spec RunSpec, runID string, cause error) (bool, error) {
	g.recordPlannedFailed(spec, runID, cause)
	return false, cause
}

// recordAndNotify 统一终态收口：记录到 TaskManager 并触发终态回调。
func (g *runGateway) recordAndNotify(spec RunSpec, runID string, result *RunResult, runErr error) {
	if result != nil {
		// 与 dispatcher 的 F2-001 收口一致，result 已由 executeWithConcurrency 补全元数据；
		// 此处统一记录终态并触发回调。
		if g.taskMgr != nil {
			g.taskMgr.RecordRun(result)
		}
		g.emitTerminal(runID, newTerminalResult(result))
		return
	}
	// result == nil：提交失败，未产生新运行的终态。
	if runErr != nil {
		g.recordPlannedFailed(spec, runID, runErr)
	}
}

// recordPlannedFailed 提交失败收口：若 runID 已存在 planned/running 预登记则更新为 failed
// 并触发一次终态收口（RecordRun 以 runID 为键覆盖，幂等，不新增第二条记录）；无预登记时
// 不创建记录（未预登记的普通 Run 不伪造历史）。
func (g *runGateway) recordPlannedFailed(spec RunSpec, runID string, cause error) {
	if g.taskMgr == nil {
		return
	}
	planned := g.taskMgr.GetRun(runID)
	if planned == nil {
		return
	}
	if planned.IsTerminal() {
		// 已是终态（如二次提交失败），不重复改写，仅确保回调投递一次。
		g.emitTerminal(runID, newTerminalResult(planned))
		return
	}
	startedAt := planned.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	failed := &RunResult{
		RunID:         runID,
		ExtensionName: spec.ExtensionName,
		ActionID:      spec.ActionID,
		State:         TaskFailed,
		StartedAt:     startedAt,
		FinishedAt:    time.Now(),
		TriggerType:   spec.Context.EventType,
		ServiceName:   spec.ServiceName,
		ResultLevel:   "error",
		ResultMsg:     cause.Error(),
	}
	if failed.TriggerType == "" {
		failed.TriggerType = "on_demand"
	}
	g.taskMgr.RecordRun(failed)
	g.emitTerminal(runID, newTerminalResult(failed))
}

// emitTerminal 投递终态回调：幂等（同一 runID 至多一次）；未注册时缓存结果待注册补调。
func (g *runGateway) emitTerminal(runID string, tr TerminalResult) {
	g.mu.Lock()
	if g.fired[runID] {
		g.mu.Unlock()
		return
	}
	if fn, ok := g.callbacks[runID]; ok {
		delete(g.callbacks, runID)
		g.fired[runID] = true
		g.mu.Unlock()
		fn(tr)
		return
	}
	// 未注册回调：缓存结果，待 OnRunTerminal 注册时补调（先终态后注册）。
	g.cached[runID] = tr
	g.mu.Unlock()
}

// OnRunTerminal 注册终态回调：已终态且未投递 → 立即补调；未终态 → 暂存等待终态；已投递 → 忽略。
func (g *runGateway) OnRunTerminal(runID string, fn func(result TerminalResult)) {
	if fn == nil {
		return
	}
	g.mu.Lock()
	if g.fired[runID] {
		g.mu.Unlock()
		return
	}
	if cached, ok := g.cached[runID]; ok {
		delete(g.cached, runID)
		g.fired[runID] = true
		g.mu.Unlock()
		fn(cached)
		return
	}
	g.callbacks[runID] = fn
	g.mu.Unlock()
}
