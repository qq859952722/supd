package extension

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ConcurrencyPolicy 并发策略类型
// REQ-F-018, 2.2.7: 4种并发策略，锁定不可新增
type ConcurrencyPolicy string

const (
	// PolicyReplace 取消前任务，启动新任务（默认）
	PolicyReplace ConcurrencyPolicy = "replace"
	// PolicySerialize 排队，前任务完成后才跑新的
	PolicySerialize ConcurrencyPolicy = "serialize"
	// PolicyParallel 并行跑，不限数量
	PolicyParallel ConcurrencyPolicy = "parallel"
	// PolicyDebounce trailing debounce
	PolicyDebounce ConcurrencyPolicy = "debounce"
)

// ConcurrencyConfig 并发配置
// REQ-F-018, 2.2.7: concurrency 字段的解析结果
type ConcurrencyConfig struct {
	Policy     ConcurrencyPolicy
	DebounceMs int // 仅 debounce 时有效，如 5000 表示 5 秒
}

// ParseConcurrency 解析 concurrency 字符串为 ConcurrencyConfig
// REQ-F-018, 2.2.7: replace | serialize | parallel | debounce:Ns
// N 为 1-3600 的整数，单位秒；其他格式返回错误（由调用方处理）
func ParseConcurrency(s string) (ConcurrencyConfig, error) {
	switch s {
	case "", "replace":
		return ConcurrencyConfig{Policy: PolicyReplace}, nil
	case "serialize":
		return ConcurrencyConfig{Policy: PolicySerialize}, nil
	case "parallel":
		return ConcurrencyConfig{Policy: PolicyParallel}, nil
	}

	if strings.HasPrefix(s, "debounce:") {
		suffix := strings.TrimPrefix(s, "debounce:")
		// 仅支持 "Ns" 格式（规格 §2.2.7: N 为 1-3600 的整数，单位秒）
		if !strings.HasSuffix(suffix, "s") {
			return ConcurrencyConfig{Policy: PolicyReplace}, fmt.Errorf("concurrency: invalid debounce format %q, expected debounce:Ns", s)
		}
		numStr := strings.TrimSuffix(suffix, "s")
		n, err := strconv.Atoi(numStr)
		if err != nil {
			return ConcurrencyConfig{Policy: PolicyReplace}, fmt.Errorf("concurrency: invalid debounce format %q, N must be an integer", s)
		}
		// A-04-002: REQ-2.2.7 规定 N 为 1-3600 的整数
		if n < 1 || n > 3600 {
			return ConcurrencyConfig{Policy: PolicyReplace}, fmt.Errorf("concurrency: invalid debounce format %q, N must be 1-3600", s)
		}
		return ConcurrencyConfig{Policy: PolicyDebounce, DebounceMs: n * 1000}, nil
	}

	// 未知格式默认 replace（向后兼容）
	return ConcurrencyConfig{Policy: PolicyReplace}, nil
}

// runningTaskInfo 运行中任务的追踪信息
type runningTaskInfo struct {
	cancel    context.CancelFunc
	done      chan struct{}
	closeOnce sync.Once
}

// markDone 安全地关闭 done 通道（仅关闭一次）
func (i *runningTaskInfo) markDone() {
	i.closeOnce.Do(func() {
		close(i.done)
	})
}

// pendingRunInfo serialize 排队任务信息
type pendingRunInfo struct {
	runID     string
	parentCtx context.Context // 存储 Apply 调用时的父 context，确保取消传播
	execute   func(ctx context.Context) (*RunResult, error)
	resultCh  chan applyResult
}

// debouncePendingInfo debounce 待执行任务信息
type debouncePendingInfo struct {
	runID    string
	execute  func(ctx context.Context) (*RunResult, error)
	resultCh chan applyResult
}

// applyResult 执行结果
type applyResult struct {
	result *RunResult
	err    error
}

// ActionTracker 跟踪单个 action 的并发状态
// REQ-F-018, 2.2.7: 每个 action 独立跟踪并发状态
type ActionTracker struct {
	policy          ConcurrencyPolicy
	debounceMs      int
	runningRuns     map[string]*runningTaskInfo // 正在运行的 run_id → 任务信息
	pendingRun      *pendingRunInfo             // serialize 时的排队任务
	debouncePending *debouncePendingInfo        // debounce 时的待执行任务
	debouncer       *Debouncer                  // debounce 时的定时器
	mu              sync.Mutex
}

// NewActionTracker 创建 ActionTracker
// REQ-F-018, 2.2.7: 根据 concurrency 策略创建追踪器
func NewActionTracker(policy ConcurrencyPolicy, debounceMs int) *ActionTracker {
	t := &ActionTracker{
		policy:      policy,
		debounceMs:  debounceMs,
		runningRuns: make(map[string]*runningTaskInfo),
	}
	if policy == PolicyDebounce {
		t.debouncer = NewDebouncer(time.Duration(debounceMs) * time.Millisecond)
	}
	return t
}

// Apply 根据 concurrency 策略执行任务
// REQ-F-018, 2.2.7: 同一 action 多次触发的行为由 concurrency 字段控制
// parentCtx: 父 context，取消时会传播到任务 context
func (t *ActionTracker) Apply(parentCtx context.Context, runID string, execute func(ctx context.Context) (*RunResult, error)) (*RunResult, error) {
	switch t.policy {
	case PolicyReplace:
		return t.applyReplace(parentCtx, runID, execute)
	case PolicySerialize:
		return t.applySerialize(parentCtx, runID, execute)
	case PolicyParallel:
		return t.applyParallel(parentCtx, runID, execute)
	case PolicyDebounce:
		return t.applyDebounce(parentCtx, runID, execute)
	default:
		return t.applyReplace(parentCtx, runID, execute)
	}
}

// applyReplace 取消前任务，启动新任务
// REQ-F-018, 2.2.7: replace — 取消前任务（SIGTERM → 5 秒 → SIGKILL），启动新任务
// A-04-001/C-05-01 修复：executor.go 的 ctx.Done 路径已实现 SIGTERM→5s→SIGKILL 流程
// - Step 1: cancel context（executor 监听 ctx.Done() 后先发 SIGTERM，5s 不退再 SIGKILL）
// - Step 2: 等待旧任务退出，最多 5 秒（使用单一 deadline，总等待不超过 5s）
// - Step 3: 5 秒后仍未退出则不再等待，旧 goroutine 在进程退出后自行结束
func (t *ActionTracker) applyReplace(parentCtx context.Context, runID string, execute func(ctx context.Context) (*RunResult, error)) (*RunResult, error) {
	t.mu.Lock()

	// Step 1: 取消所有运行中任务的 context
	// executor.go 监听 ctx.Done() 后先发 SIGTERM，5s 不退再 SIGKILL（C-05-01 修复）
	var dones []chan struct{}
	for rid, info := range t.runningRuns {
		info.cancel()
		dones = append(dones, info.done)
		delete(t.runningRuns, rid)
	}

	// 创建新任务的 context（派生自 parentCtx，确保父 context 取消时传播）
	ctx, cancel := context.WithCancel(parentCtx)
	info := &runningTaskInfo{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	t.runningRuns[runID] = info

	t.mu.Unlock()

	// Step 2: 等待被取消的任务退出，最多 5 秒（规格 §2.2.7: SIGTERM → 5s → SIGKILL）
	// A-04-001: 使用单一 deadline 确保总等待时间不超过 5 秒（替代原先每任务各自 5s 的串行等待）
	if len(dones) > 0 {
		waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
		for _, done := range dones {
			select {
			case <-done:
				// 任务已正常退出
			case <-waitCtx.Done():
				// Step 3: 5 秒超时，不再等待剩余任务，继续执行新任务
				// 旧任务的 goroutine 会在进程退出后自行结束
			}
		}
		waitCancel()
	}

	// 执行新任务
	result, err := execute(ctx)

	// 清理
	info.markDone()
	t.mu.Lock()
	delete(t.runningRuns, runID)
	t.mu.Unlock()

	return result, err
}

// applySerialize 排队执行
// REQ-F-018, 2.2.7: serialize — 排队，前任务完成后才跑新的
func (t *ActionTracker) applySerialize(parentCtx context.Context, runID string, execute func(ctx context.Context) (*RunResult, error)) (*RunResult, error) {
	resultCh := make(chan applyResult, 1)

	t.mu.Lock()

	if len(t.runningRuns) == 0 {
		// 无运行中任务，直接执行
		ctx, cancel := context.WithCancel(parentCtx)
		info := &runningTaskInfo{
			cancel: cancel,
			done:   make(chan struct{}),
		}
		t.runningRuns[runID] = info
		t.mu.Unlock()

		result, err := execute(ctx)

		info.markDone()
		t.mu.Lock()
		delete(t.runningRuns, runID)
		// 检查是否有排队任务
		t.startPendingRunLocked()
		t.mu.Unlock()

		return result, err
	}

	// 有运行中的任务，将新任务放入 pendingRun
	// 如果已有排队任务，取消前一个（只保留最后一个）
	if t.pendingRun != nil {
		t.pendingRun.resultCh <- applyResult{
			result: &RunResult{
				RunID: t.pendingRun.runID,
				State: TaskCanceled,
			},
		}
	}
	t.pendingRun = &pendingRunInfo{
		runID:     runID,
		parentCtx: parentCtx,
		execute:   execute,
		resultCh:  resultCh,
	}
	t.mu.Unlock()

	// 等待执行结果
	ar := <-resultCh
	return ar.result, ar.err
}

// applyParallel 并行执行
// REQ-F-018, 2.2.7: parallel — 并行跑，不限数量
func (t *ActionTracker) applyParallel(parentCtx context.Context, runID string, execute func(ctx context.Context) (*RunResult, error)) (*RunResult, error) {
	t.mu.Lock()

	ctx, cancel := context.WithCancel(parentCtx)
	info := &runningTaskInfo{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	t.runningRuns[runID] = info

	t.mu.Unlock()

	result, err := execute(ctx)

	info.markDone()
	t.mu.Lock()
	delete(t.runningRuns, runID)
	t.mu.Unlock()

	return result, err
}

// applyDebounce trailing debounce 执行
// REQ-F-018, 2.2.7: debounce — trailing debounce，每次触发重设计时器，
// N秒内无新触发后执行最后一次触发的 action
func (t *ActionTracker) applyDebounce(parentCtx context.Context, runID string, execute func(ctx context.Context) (*RunResult, error)) (*RunResult, error) {
	resultCh := make(chan applyResult, 1)

	t.mu.Lock()

	// 如果有之前的 debounce 待执行任务，取消它
	if t.debouncePending != nil {
		t.debouncePending.resultCh <- applyResult{
			result: &RunResult{
				RunID: t.debouncePending.runID,
				State: TaskCanceled,
			},
		}
	}

	// 设置新的 debounce 待执行任务
	t.debouncePending = &debouncePendingInfo{
		runID:    runID,
		execute:  execute,
		resultCh: resultCh,
	}

	// 重置 debounce 定时器
	t.debouncer.Reset(func() {
		t.executeDebouncePending(parentCtx)
	})

	t.mu.Unlock()

	// 等待结果
	ar := <-resultCh
	return ar.result, ar.err
}

// executeDebouncePending 执行 debounce 待执行任务
// REQ-F-018, 2.2.7: debounce 计时到期触发执行时，
// 如有运行中的同 action 任务，按该扩展的 concurrency 策略处理（默认 replace）
func (t *ActionTracker) executeDebouncePending(parentCtx context.Context) {
	t.mu.Lock()

	pending := t.debouncePending
	t.debouncePending = nil

	if pending == nil {
		t.mu.Unlock()
		return
	}

	// 如有运行中的任务，按 replace 策略取消
	// REQ-F-018, 2.2.7: debounce 到期时如有运行中任务按 concurrency 策略处理（默认 replace）
	var dones []chan struct{}
	for rid, info := range t.runningRuns {
		info.cancel()
		dones = append(dones, info.done)
		delete(t.runningRuns, rid)
	}

	// 创建新任务的 context（派生自 parentCtx）
	ctx, cancel := context.WithCancel(parentCtx)
	info := &runningTaskInfo{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	t.runningRuns[pending.runID] = info

	t.mu.Unlock()

	// 等待被取消的任务退出，最多 5 秒
	// A-04-001 修复：使用单一 deadline 确保总等待时间不超过 5 秒（规格 §2.2.7: SIGTERM → 5s → SIGKILL）
	if len(dones) > 0 {
		waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
		for _, done := range dones {
			select {
			case <-done:
			case <-waitCtx.Done():
				// 5 秒超时，不再等待剩余任务
			}
		}
		waitCancel()
	}

	// 执行任务
	result, err := pending.execute(ctx)

	// 清理
	info.markDone()
	t.mu.Lock()
	delete(t.runningRuns, pending.runID)
	t.mu.Unlock()

	// 发送结果
	pending.resultCh <- applyResult{result: result, err: err}
}

// startPendingRunLocked 启动排队的 serialize 任务
// 必须在持有 t.mu 的情况下调用
// 使用排队任务自身的 parentCtx 派生 context，确保父 context 取消时传播到排队任务
//
// A-04-001 修复说明（语义澄清）：
// serialize 策略 pendingRun 为单指针，新触发会覆盖旧 pending（"last pending wins" 语义）。
// 规格 §2.2.7 仅定义"排队，前任务完成后才跑新的"，未规定 pending 队列上限。
// 当前实现的语义：
//   - 同一 action 在运行中有 N 次新触发，仅最后一次会排队执行，前 N-1 次返回 TaskCanceled
//   - 这与 debounce 的"最后一次生效"语义一致，避免无限堆积 pending 任务导致内存泄漏
// 替代方案（pending 队列）的代价：需要限制队列长度（否则 OOM），且每次触发都要等待
// N 次任务完成才能执行——违反"排队"的直觉（用户期望快速响应最新触发）。
// 如规格 v1.6 需要队列语义，应显式定义 concurrency:serialize:N（N=队列上限）。
func (t *ActionTracker) startPendingRunLocked() {
	if t.pendingRun == nil || len(t.runningRuns) > 0 {
		return
	}

	pending := t.pendingRun
	t.pendingRun = nil

	// 从排队任务存储的 parentCtx 派生 context（而非 context.Background()）
	ctx, cancel := context.WithCancel(pending.parentCtx)
	info := &runningTaskInfo{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	t.runningRuns[pending.runID] = info

	// 在新 goroutine 中执行排队任务
	go func() {
		result, err := pending.execute(ctx)

		info.markDone()
		t.mu.Lock()
		delete(t.runningRuns, pending.runID)
		// 递归检查是否有更多排队任务
		t.startPendingRunLocked()
		t.mu.Unlock()

		pending.resultCh <- applyResult{result: result, err: err}
	}()
}

// RunCompleted 标记任务完成
// REQ-F-018: 任务完成后从 runningRuns 中移除；
// serialize 时检查是否有 pendingRun，有则执行
func (t *ActionTracker) RunCompleted(runID string, result *RunResult) {
	t.mu.Lock()
	defer t.mu.Unlock()

	info, ok := t.runningRuns[runID]
	if !ok {
		return
	}

	info.markDone()
	delete(t.runningRuns, runID)

	// serialize 时检查是否有排队任务
	if t.policy == PolicySerialize {
		t.startPendingRunLocked()
	}
}

// CancelRunning 取消指定运行
// REQ-F-018: 取消运行中的任务
// 返回 true 表示找到并取消了对应任务，false 表示未找到
func (t *ActionTracker) CancelRunning(runID string) bool {
	t.mu.Lock()
	info, ok := t.runningRuns[runID]
	if !ok {
		t.mu.Unlock()
		return false
	}
	info.cancel()
	t.mu.Unlock()

	// 等待任务完成
	<-info.done
	return true
}

// Stop 停止 tracker，取消 debounce timer 和 pending 任务
// B-05-002: 热重载删除扩展时调用，避免 tracker 残留
// 注意：不取消运行中任务（让它们自然完成），符合"热重载不影响运行中服务"原则
func (t *ActionTracker) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()

	// 停止 debounce timer
	if t.debouncer != nil {
		t.debouncer.Stop()
	}

	// 取消 serialize 的 pending 任务
	if t.pendingRun != nil {
		t.pendingRun.resultCh <- applyResult{
			result: &RunResult{
				RunID: t.pendingRun.runID,
				State: TaskCanceled,
			},
		}
		t.pendingRun = nil
	}

	// 取消 debounce 的 pending 任务
	if t.debouncePending != nil {
		t.debouncePending.resultCh <- applyResult{
			result: &RunResult{
				RunID: t.debouncePending.runID,
				State: TaskCanceled,
			},
		}
		t.debouncePending = nil
	}

	// 不取消 runningRuns（让运行中任务自然完成）
}

// HasRunning 是否有运行中的任务
// REQ-F-018: 检查是否有运行中的任务
func (t *ActionTracker) HasRunning() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.runningRuns) > 0
}

// ConcurrencyManager 管理所有扩展的 action 追踪器
// REQ-F-018, 2.2.7: 多 action 之间互不阻塞，每个 action 独立跟踪并发状态
type ConcurrencyManager struct {
	trackers map[string]*ActionTracker // key = "extName:actionID"
	mu       sync.RWMutex
}

// NewConcurrencyManager 创建 ConcurrencyManager
func NewConcurrencyManager() *ConcurrencyManager {
	return &ConcurrencyManager{
		trackers: make(map[string]*ActionTracker),
	}
}

// GetTracker 获取或创建指定扩展 action 的追踪器
// REQ-F-018, 2.2.7: 多 action 之间互不阻塞
func (m *ConcurrencyManager) GetTracker(extName, actionID string, policy ConcurrencyPolicy, debounceMs int) *ActionTracker {
	key := extName + ":" + actionID

	m.mu.RLock()
	tracker, ok := m.trackers[key]
	m.mu.RUnlock()

	if ok {
		return tracker
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	tracker, ok = m.trackers[key]
	if ok {
		return tracker
	}

	tracker = NewActionTracker(policy, debounceMs)
	m.trackers[key] = tracker
	return tracker
}

// CancelRun 取消指定 runID 的运行任务
// N-03-001 修复：遍历所有 tracker 调用 CancelRunning，runID 全局唯一（UUID）
// B-05-001 修复：先在锁内获取 tracker 引用快照，释放锁后再等待 <-info.done，
// 避免持 RLock 阻塞 GetTracker 的写路径（创建新 tracker）
func (m *ConcurrencyManager) CancelRun(runID string) bool {
	m.mu.RLock()
	trackers := make([]*ActionTracker, 0, len(m.trackers))
	for _, tracker := range m.trackers {
		trackers = append(trackers, tracker)
	}
	m.mu.RUnlock()

	for _, tracker := range trackers {
		if tracker.CancelRunning(runID) {
			return true
		}
	}
	return false
}

// HasAnyRunning 检查是否存在运行中的任务
// B-04-002 修复：关机流程需判断是否有扩展任务在跑
func (m *ConcurrencyManager) HasAnyRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, tracker := range m.trackers {
		if tracker.HasRunning() {
			return true
		}
	}
	return false
}

// RemoveExtension 移除指定扩展的所有 tracker（热重载删除扩展时调用）
// B-05-002: 清理 trackers map 中指定扩展的条目，避免内存泄漏
// 注意：运行中任务不取消（让它们自然完成），仅停止 debounce timer 和清理 pending
func (m *ConcurrencyManager) RemoveExtension(extName string) {
	prefix := extName + ":"
	m.mu.Lock()
	for key, tracker := range m.trackers {
		if strings.HasPrefix(key, prefix) {
			tracker.Stop()
			delete(m.trackers, key)
		}
	}
	m.mu.Unlock()
}

// WaitForAllRunning 等待所有运行中任务结束，最多等待 timeout
// B-04-002 修复：关机流程需要等待扩展任务结束，避免孤儿进程
// 返回仍在运行的任务数量
func (m *ConcurrencyManager) WaitForAllRunning(timeout time.Duration) int {
	m.mu.RLock()
	var dones []chan struct{}
	for _, tracker := range m.trackers {
		dones = append(dones, tracker.collectDones()...)
	}
	m.mu.RUnlock()

	if len(dones) == 0 {
		return 0
	}

	deadline := time.After(timeout)
	remaining := len(dones)
	for _, done := range dones {
		select {
		case <-done:
			remaining--
		case <-deadline:
			return remaining
		}
	}
	return 0
}

// collectDones 收集本 tracker 所有运行中任务的 done 通道（用于关机等待）
// B-04-002: 供 ConcurrencyManager.WaitForAllRunning 使用
func (t *ActionTracker) collectDones() []chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	dones := make([]chan struct{}, 0, len(t.runningRuns))
	for _, info := range t.runningRuns {
		dones = append(dones, info.done)
	}
	return dones
}
