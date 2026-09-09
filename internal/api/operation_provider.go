package api

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	svcerr "github.com/supdorg/supd/internal/errors"
	"github.com/supdorg/supd/internal/extension"
	"github.com/supdorg/supd/internal/store"
)

// OperationProvider 操作中心 API 的最小依赖面（registry 查询 / runner 触发 / store 查询）。
// handler 不直接依赖 core 实现（§十二.5.3）。
type OperationProvider interface {
	ListOperations(ctx context.Context) ([]OperationCard, error)
	GetOperation(ctx context.Context, id string) (*OperationDetail, bool)
	RunOperation(ctx context.Context, id string, params []byte, idemKey string) (*RunOperationResult, error)
	ListExecutions(ctx context.Context, limit, offset int) ([]store.ExecutionDetail, error)
	GetExecution(ctx context.Context, id string) (*store.ExecutionDetail, bool)
}

// LastExecution 操作卡片上次执行摘要（§九.1）。
type LastExecution struct {
	ExecutionID string `json:"execution_id"`
	CreatedAt   int64  `json:"created_at"`
	// State state 取值 running/interrupted/finished（v5 确认语义）。
	State string `json:"state"`
	// Result 仅 finished 时提供：success（无失败 Run）/failed（存在失败 Run）。
	Result string `json:"result,omitempty"`
}

// OperationCard 操作卡片（列表项，§九.1）：OperationInfo + 上次执行摘要。
// 字段契约与本项目前端固定一致（snake_case）。
type OperationCard struct {
	ID             string          `json:"id"`
	Label          string          `json:"label"`
	ButtonStyle    string          `json:"button_style"`
	Description    string          `json:"description"`
	Registrants    []string        `json:"registrants"`
	ResponderCount int             `json:"responder_count"`
	Warnings       []string        `json:"warnings"`
	LastExecution  *LastExecution  `json:"last_execution,omitempty"`
}

// OperationDetail 单操作详情（含注册者/响应者/配置 warning）。
type OperationDetail struct {
	OperationCard
	Responders []extension.ResponderRef `json:"responders"`
}

func cardFromInfo(info extension.OperationInfo, warnings []string) OperationCard {
	return OperationCard{
		ID:             info.ID,
		Label:          info.Label,
		ButtonStyle:    info.ButtonStyle,
		Description:    info.Description,
		Registrants:    info.Registrants,
		ResponderCount: len(info.Responders),
		Warnings:       warnings,
	}
}

// RunOperationResult POST /api/operations/{id}/run 的返回：已创建 Execution 引用。
type RunOperationResult struct {
	ExecutionID string `json:"execution_id"`
	TopicID     string `json:"topic_id"`
}

// idemTTL Idempotency-Key 内存 TTL（硬编码 10 分钟，§十二.5.3）。
const idemTTL = 10 * time.Minute

type idemEntry struct {
	executionID string
	topicID     string
	createdAt   time.Time
}

// operationTriggerer Provider 对 Runner 的最小依赖面（触发操作）。
type operationTriggerer interface {
	StartExecution(ctx context.Context, operationID string, params []byte) (*extension.TriggerOutcome, error)
}

// CoreOperationProvider 实现 OperationProvider。
type CoreOperationProvider struct {
	registry *extension.OperationRegistry
	runner   operationTriggerer
	store    *store.Store

	idemMu sync.Mutex
	idem   map[string]idemEntry
}

// NewCoreOperationProvider 创建 OperationProvider。
func NewCoreOperationProvider(registry *extension.OperationRegistry, runner operationTriggerer, st *store.Store) *CoreOperationProvider {
	return &CoreOperationProvider{
		registry: registry,
		runner:   runner,
		store:    st,
		idem:     make(map[string]idemEntry),
	}
}

// ListOperations 列出操作卡片（含上次执行摘要）。
func (p *CoreOperationProvider) ListOperations(ctx context.Context) ([]OperationCard, error) {
	var cards []OperationCard
	for _, info := range p.registry.List() {
		card := cardFromInfo(info, p.registry.Warnings(info.ID))
		if p.store != nil && info.ID != "" {
			last, err := p.store.GetLastExecutionForOperation(ctx, info.ID)
			if err != nil {
				return nil, err
			}
			if last != nil {
				card.LastExecution = p.buildLastExecution(ctx, last)
			}
		}
		cards = append(cards, card)
	}
	return cards, nil
}

// buildLastExecution 依据 §5 state 语义构造上次执行摘要：
//   - 无 finished_at 且无 interrupted_at → running
//   - interrupted_at 非空 → interrupted
//   - finished_at 非空 → finished，附整体结果：存在失败 Run → failed，否则 success。
func (p *CoreOperationProvider) buildLastExecution(ctx context.Context, e *store.Execution) *LastExecution {
	le := &LastExecution{ExecutionID: e.ID, CreatedAt: e.CreatedAt}
	switch {
	case e.InterruptedAt != nil:
		le.State = "interrupted"
	case e.FinishedAt != nil:
		le.State = "finished"
		le.Result = "success"
		// 查 runs 判定是否存在失败 Run（仅 finished 时提供结果）。
		if det, err := p.store.GetExecution(ctx, e.ID); err == nil {
			for _, r := range det.Runs {
				if isFailureRunState(r.State) {
					le.Result = "failed"
					break
				}
			}
		} else {
			le.Result = "failed"
		}
	default:
		le.State = "running"
	}
	return le
}

// isFailureRunState 判定 operation_run 状态是否视为执行失败（非 success 的终态）。
func isFailureRunState(state string) bool {
	switch state {
	case store.StateFailed, store.StateTimeout, store.StateCanceled, store.StateKilled:
		return true
	}
	return false
}

// GetOperation 返回单操作详情。
func (p *CoreOperationProvider) GetOperation(ctx context.Context, id string) (*OperationDetail, bool) {
	info, ok := p.registry.Get(id)
	if !ok {
		return nil, false
	}
	return &OperationDetail{
		OperationCard: cardFromInfo(*info, p.registry.Warnings(id)),
		Responders:    info.Responders,
	}, true
}

// RunOperation 触发操作（含 Idempotency-Key 幂等）：
//   - 幂等 key 命中 → 返回已创建 execution（内存 TTL 10 分钟）；
//   - 未知操作 → svcerr.ErrServiceNotFound（404）；
//   - 参数非法 → svcerr.ErrInvalidRequest（400）；
//   - 其余执行错误 → svcerr.ErrInternal。
func (p *CoreOperationProvider) RunOperation(_ context.Context, id string, params []byte, idemKey string) (*RunOperationResult, error) {
	if idemKey != "" {
		if entry, ok := p.idempotentGet(idemKey); ok {
			return &RunOperationResult{ExecutionID: entry.executionID, TopicID: entry.topicID}, nil
		}
	}

	outcome, err := p.runner.StartExecution(context.Background(), id, params)
	if err != nil {
		switch {
		case errors.Is(err, extension.ErrOperationNotFound):
			return nil, svcerr.NewServiceError(svcerr.ErrExtensionNotFound, "operation "+id+" not found")
		case errors.Is(err, extension.ErrOperationParamInvalid):
			return nil, svcerr.NewServiceError(svcerr.ErrInvalidRequest, err.Error())
		default:
			return nil, svcerr.NewServiceError(svcerr.ErrInternal, "start operation execution failed")
		}
	}

	if idemKey != "" {
		p.idempotentPut(idemKey, idemEntry{
			executionID: outcome.ExecutionID,
			topicID:     outcome.TopicID,
			createdAt:   time.Now(),
		})
	}
	return &RunOperationResult{ExecutionID: outcome.ExecutionID, TopicID: outcome.TopicID}, nil
}

// ListExecutions 返回执行历史（分页）。
func (p *CoreOperationProvider) ListExecutions(ctx context.Context, limit, offset int) ([]store.ExecutionDetail, error) {
	if p.store == nil {
		return []store.ExecutionDetail{}, nil
	}
	return p.store.ListExecutions(ctx, limit, offset)
}

// GetExecution 返回执行详情（runs 快照 + topic 链接）；不存在返回 (nil, false)。
func (p *CoreOperationProvider) GetExecution(ctx context.Context, id string) (*store.ExecutionDetail, bool) {
	if p.store == nil {
		return nil, false
	}
	det, err := p.store.GetExecution(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false
		}
		return nil, false
	}
	return det, true
}

// idempotentGet 查询幂等 key；同时清理过期条目。
func (p *CoreOperationProvider) idempotentGet(key string) (idemEntry, bool) {
	p.idemMu.Lock()
	defer p.idemMu.Unlock()
	p.purgeExpiredLocked()
	entry, ok := p.idem[key]
	return entry, ok
}

// idempotentPut 写入幂等 key。
func (p *CoreOperationProvider) idempotentPut(key string, entry idemEntry) {
	p.idemMu.Lock()
	defer p.idemMu.Unlock()
	p.purgeExpiredLocked()
	p.idem[key] = entry
}

// purgeExpiredLocked 清理超时幂等条目（调用方持锁）。
func (p *CoreOperationProvider) purgeExpiredLocked() {
	now := time.Now()
	for k, e := range p.idem {
		if now.Sub(e.createdAt) > idemTTL {
			delete(p.idem, k)
		}
	}
}