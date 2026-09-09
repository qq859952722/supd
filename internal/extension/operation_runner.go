package extension

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/supdorg/supd/internal/store"
)

// 操作注入的 5 个 SUPD_* 环境变量名，见 §五 运行时环境变量。
var (
	// ErrOperationNotFound 操作 ID 在注册表中不存在（API 映射 404）。
	ErrOperationNotFound = errors.New("operation not found")
	// ErrOperationParamInvalid 触发参数非法（非对象/超 8KB），API 映射 400。
	ErrOperationParamInvalid = errors.New("operation params invalid")
)

// servicePhaseConcurrency 服务阶段跨服务并发上限（硬编码，§四.3，不新增配置字段）。
const servicePhaseConcurrency = 4

// operationParamsLimit 操作参数上限 8KB（§五 参数规则）。
const operationParamsLimit = 8192

// TriggerOutcome 触发结果：已创建 Execution 的引用（ExecutionID + TopicID）。
type TriggerOutcome struct {
	ExecutionID string
	TopicID     string
}

// opSubmit 单个计划 Run：runID + 目标扩展/action/服务。
type opSubmit struct {
	runID       string
	extName     string
	actionID    string
	serviceName string // 全局阶段为空串
	phase       string // global / service
}

// opExec 一次执行的两阶段运行上下文（持久层对应一个 Execution）。
type opExec struct {
	executionID string
	operationID string
	topicID     string
	params      string // 紧凑 JSON 对象（缺省 "{}"）

	globalSubmits []opSubmit     // 全局阶段稳定序
	serviceGroups []serviceGroup // 服务阶段按服务分组（组内稳定序）
}

// serviceGroup 服务阶段某服务的响应者集合（组内 extension_name→action_id 稳定序串行）。
type serviceGroup struct {
	serviceName string
	submits     []opSubmit
}

// OperationRunner 两阶段操作执行器（§四 操作执行语义）。
//
// 职责：
//   - 通过 RunGateway 提交 Run 并等待终态（不依赖 TaskManager 重启后仍存在，重启恢复走 Store）；
//   - 持久化 Execution / planned Run / Topic 经 Store（第一事务：Execution+Topic+全局 planned Run；
//     第二事务：服务阶段 planned Run 行）；
//   - 触发时冻结 OperationRegistry 快照（热重载只影响新 Execution）。
type OperationRunner struct {
	gateway  RunGateway
	store    *store.Store
	registry *OperationRegistry
	logger   *slog.Logger
}

// NewOperationRunner 创建 OperationRunner。
func NewOperationRunner(gateway RunGateway, st *store.Store, registry *OperationRegistry) *OperationRunner {
	return &OperationRunner{
		gateway:  gateway,
		store:    st,
		registry: registry,
		logger:   slog.Default(),
	}
}

// normalizeParams 校验并规范化触发参数：
//   - 顶层必须是 JSON 对象（缺省 "{}"）；
//   - ≤ 8KB（按紧凑后字节数计）；
//   - 返回紧凑 JSON 对象字符串，供 SUPD_OPERATION_PARAMS 注入（不进入命令行）。
func normalizeParams(params []byte) (string, error) {
	trimmed := bytes.TrimSpace(params)
	if len(trimmed) == 0 {
		return "{}", nil
	}
	if trimmed[0] != '{' {
		return "", fmt.Errorf("params must be a top-level JSON object, got %q", firstNBytes(trimmed, 32))
	}
	if !json.Valid(trimmed) {
		return "", errors.New("params is not valid JSON")
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, trimmed); err != nil {
		return "", err
	}
	if buf.Len() > operationParamsLimit {
		return "", fmt.Errorf("params exceed %d bytes", operationParamsLimit)
	}
	return buf.String(), nil
}

func firstNBytes(b []byte, n int) string {
	if len(b) > n {
		b = b[:n]
	}
	return string(b)
}

// beginExecution 冻结快照、校验参数、创建 Execution（第一事务：execution+topic+全局 planned Run）。
// 返回执行上下文与 Outcome（含 topicID，供后续 Run 注入 SUPD_NOTIFICATION_TOPIC_ID）。
func (r *OperationRunner) beginExecution(opID string, params []byte) (*opExec, *TriggerOutcome, error) {
	snap, ok := r.registry.Snapshot(opID)
	if !ok {
		return nil, nil, ErrOperationNotFound
	}
	compact, err := normalizeParams(params)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrOperationParamInvalid, err)
	}

	executionID := uuid.Must(uuid.NewV7()).String() // execution UUIDv7（设计稿 §五/§七.2）
	data := &opExec{
		executionID: executionID,
		operationID: opID,
		params:      compact,
	}

	// 全局阶段 planned Run（第一事务）。
	var planned []store.PlannedRun
	for _, gr := range snap.GlobalRuns {
		runID := uuid.New().String()
		data.globalSubmits = append(data.globalSubmits, opSubmit{
			runID: runID, extName: gr.ExtensionName, actionID: gr.ActionID, phase: "global",
		})
		planned = append(planned, store.PlannedRun{
			RunID: runID, Phase: "global", ServiceName: nil,
			ExtensionName: gr.ExtensionName, ActionID: gr.ActionID,
		})
	}

	topicID, err := r.store.CreateExecution(context.Background(), store.CreateExecutionInput{
		ExecutionID:    executionID,
		OperationID:    opID,
		OperationLabel: snap.Label,
		CreatedAt:      time.Now().UnixMilli(),
		Runs:           planned,
	})
	if err != nil {
		return nil, nil, err
	}
	data.topicID = topicID

	// 服务阶段 planned Run：按服务分组（组内稳定序），runID 预生成。
	byService := make(map[string]*serviceGroup)
	for _, rr := range snap.ServiceRuns {
		runID := uuid.New().String()
		g, ok := byService[rr.ServiceName]
		if !ok {
			g = &serviceGroup{serviceName: rr.ServiceName}
			byService[rr.ServiceName] = g
		}
		g.submits = append(g.submits, opSubmit{
			runID: runID, extName: rr.ExtensionName, actionID: rr.ActionID,
			serviceName: rr.ServiceName, phase: "service",
		})
	}
	for _, g := range byService {
		data.serviceGroups = append(data.serviceGroups, *g)
	}
	// 服务组稳定序（service_name 升序；组内 responder 已在 Registry 按
	// service_name→extension_name→action_id 稳定排序）。
	sortServiceGroups(data.serviceGroups)

	return data, &TriggerOutcome{ExecutionID: executionID, TopicID: topicID}, nil
}

func sortServiceGroups(groups []serviceGroup) {
	for i := 1; i < len(groups); i++ {
		for j := i; j > 0 && groups[j-1].serviceName > groups[j].serviceName; j-- {
			groups[j-1], groups[j] = groups[j], groups[j-1]
		}
	}
}

// StartExecution 校验参数、创建 Execution 并异步执行两阶段，返回 Execution 引用。
// 参数校验失败（非对象/超 8KB）同步返回 ErrOperationParamInvalid（不建 Execution）；
// 未知操作同步返回 ErrOperationNotFound；其余执行在后台 goroutine 继续。
func (r *OperationRunner) StartExecution(ctx context.Context, opID string, params []byte) (*TriggerOutcome, error) {
	data, outcome, err := r.beginExecution(opID, params)
	if err != nil {
		return nil, err
	}
	if !r.hasResponders(data) {
		r.finishNoResponders(data, outcome)
		return outcome, nil
	}
	go r.runTwoPhase(context.Background(), data)
	return outcome, nil
}

// RunSynchronous 同步执行一次完整两阶段（测试与内部集成路径）。
func (r *OperationRunner) RunSynchronous(ctx context.Context, opID string, params []byte) (*TriggerOutcome, error) {
	data, outcome, err := r.beginExecution(opID, params)
	if err != nil {
		return nil, err
	}
	if !r.hasResponders(data) {
		r.finishNoResponders(data, outcome)
		return outcome, nil
	}
	r.runTwoPhase(ctx, data)
	return outcome, nil
}

func (r *OperationRunner) hasResponders(data *opExec) bool {
	return len(data.globalSubmits) > 0 || len(data.serviceGroups) > 0
}

// finishNoResponders 无匹配响应者：写 system warning 通知 + 完成 Execution + 立即关闭 Topic（§四.4）。
func (r *OperationRunner) finishNoResponders(data *opExec, outcome *TriggerOutcome) {
	now := time.Now().UnixMilli()
	_ = r.store.AppendNotification(context.Background(), store.NotificationInput{
		TopicID:     data.topicID,
		Level:       "warning",
		Content:     "没有服务或全局扩展响应此操作",
		SourceType:  store.SrcTypeSystem,
		ExecutionID: &outcome.ExecutionID,
	})
	_ = r.store.FinishExecution(context.Background(), data.executionID, now)
	_ = r.store.CloseTopic(context.Background(), data.topicID, now)
}

// runTwoPhase 执行两阶段并完成（全局 → 服务 → Finish + CloseTopic）。
func (r *OperationRunner) runTwoPhase(ctx context.Context, data *opExec) {
	// 方案 A 失败隔离：全局失败不阻止后续全局与服务阶段（逐个处理，不提前中断）。
	r.runGlobalPhase(ctx, data)
	r.runServicePhase(ctx, data)

	now := time.Now().UnixMilli()
	if r.store != nil {
		if err := r.store.FinishExecution(ctx, data.executionID, now); err != nil {
			r.logger.Warn("finish operation execution failed", "execution_id", data.executionID, "error", err)
		}
		if err := r.store.CloseTopic(ctx, data.topicID, now); err != nil {
			r.logger.Warn("close operation topic failed", "execution_id", data.executionID, "topic_id", data.topicID, "error", err)
		}
	}
}

// runGlobalPhase 全局阶段：按 extension_name→action_id 稳定序逐个顺序执行（§四.2）。
func (r *OperationRunner) runGlobalPhase(ctx context.Context, data *opExec) {
	for _, s := range data.globalSubmits {
		r.submitOne(ctx, data, s)
	}
}

// runServicePhase 服务阶段：创建服务 planned Run（第二事务），跨服务有界并行（≤4），
// 同服务内稳定序串行（§四.3）。每个 Run 复用既有 action concurrency。
func (r *OperationRunner) runServicePhase(ctx context.Context, data *opExec) {
	if len(data.serviceGroups) == 0 {
		return
	}
	// 第二事务：创建服务阶段 planned run 行。
	var planned []store.PlannedRun
	for _, g := range data.serviceGroups {
		for _, s := range g.submits {
			svc := s.serviceName
			planned = append(planned, store.PlannedRun{
				RunID: s.runID, Phase: "service", ServiceName: &svc,
				ExtensionName: s.extName, ActionID: s.actionID,
			})
		}
	}
	if r.store != nil {
		if err := r.store.AddPlannedRuns(context.Background(), data.executionID, planned); err != nil {
			r.logger.Warn("create service planned runs failed", "execution_id", data.executionID, "error", err)
		}
	}

	sem := make(chan struct{}, servicePhaseConcurrency)
	var wg sync.WaitGroup
	for _, g := range data.serviceGroups {
		wg.Add(1)
		sem <- struct{}{}
		group := g
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			// 同服务内稳定序串行。
			for _, s := range group.submits {
				r.submitOne(ctx, data, s)
			}
		}()
	}
	wg.Wait()
}

// submitOne 提交单个 Run（复用 RunGateway）并等待其终态回调。
func (r *OperationRunner) submitOne(ctx context.Context, data *opExec, s opSubmit) {
	if r.gateway == nil {
		return
	}
	done := make(chan struct{})
	r.gateway.OnRunTerminal(s.runID, func(tr TerminalResult) {
		// §5.1 状态权威性：终态回调更新 operation_run 快照（幂等，level=store 保证只前进）。
		if r.store != nil {
			var sa, fa *int64
			if !tr.StartedAt.IsZero() {
				v := tr.StartedAt.UnixMilli()
				sa = &v
			}
			if !tr.FinishedAt.IsZero() {
				v := tr.FinishedAt.UnixMilli()
				fa = &v
			}
			_ = r.store.UpdateRunState(context.Background(), s.runID, string(tr.State), sa, fa)
		}
		close(done)
	})

	spec := RunSpec{
		ServiceName:   s.serviceName,
		ExtensionName: s.extName,
		ActionID:      s.actionID,
		Context: TriggerContext{
			EventType:            "on_demand",
			TriggerSource:        "webui",
			TriggerUser:          "webui", // v5 决策：操作路径统一注入 webui
			ServiceName:          s.serviceName,
			ActionID:             s.actionID,
			OperationID:          data.operationID,
			OperationParams:      data.params,
			NotificationTopicID:  data.topicID,
			OperationExecutionID: data.executionID,
			OperationPhase:       s.phase,
		},
	}
	r.gateway.SubmitRun(spec, s.runID)

	select {
	case <-done:
	case <-ctx.Done():
	}
}

// RecoverInterruptedExecutions 重启恢复（§四.6）：将未完成 Execution 标记为中断；
// 不恢复子进程；run 状态保留原值。
func (r *OperationRunner) RecoverInterruptedExecutions(ctx context.Context) error {
	if r.store == nil {
		return nil
	}
	unfinished, err := r.store.ListUnfinishedExecutions(ctx)
	if err != nil {
		return err
	}
	for _, e := range unfinished {
		if err := r.store.InterruptExecution(ctx, e.ID, time.Now().UnixMilli()); err != nil {
			r.logger.Warn("mark interrupted execution failed", "execution_id", e.ID, "error", err)
		}
	}
	return nil
}
