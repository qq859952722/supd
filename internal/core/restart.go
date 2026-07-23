package core

import (
	"math"
	"sync"
	"syscall"
	"time"
)

// RestartPolicy 重启策略枚举
// REQ-F-008: 3种策略，禁止新增（见AGENTS.md 2.3节枚举值锁定清单）
type RestartPolicy string

const (
	RestartAlways    RestartPolicy = "always"
	RestartOnFailure RestartPolicy = "on-failure"
	RestartNever     RestartPolicy = "never"
)

// RestartEngine 重启策略引擎
// REQ-F-008: 3种策略+指数退避+max_retries+reset_after_seconds
// B-04-001 修复：添加 mutex 保护 retries/lastStartTime，避免 supervisor goroutine 写 + API handler 读的潜在竞态
type RestartEngine struct {
	policy            RestartPolicy
	backoffMs         int
	maxBackoffMs      int
	multiplier        int
	maxRetries        int
	resetAfterSeconds int

	mu            sync.Mutex // B-04-001 修复：保护 retries/lastStartTime 的并发访问
	retries       int
	lastStartTime time.Time
}

// NewRestartEngine 创建重启策略引擎
func NewRestartEngine(policy RestartPolicy, backoffMs, maxBackoffMs, multiplier, maxRetries, resetAfterSeconds int) *RestartEngine {
	return &RestartEngine{
		policy:            policy,
		backoffMs:         backoffMs,
		maxBackoffMs:      maxBackoffMs,
		multiplier:        multiplier,
		maxRetries:        maxRetries,
		resetAfterSeconds: resetAfterSeconds,
	}
}

// ShouldRestart 判断是否应该重启
// REQ-F-008: on-failure触发条件：退出码非0，或被信号杀死（非SIGTERM/SIGINT的手动停止）
func (e *RestartEngine) ShouldRestart(exitCode int, signaled bool, sig syscall.Signal) bool {
	switch e.policy {
	case RestartAlways:
		return true
	case RestartOnFailure:
		if exitCode != 0 {
			return true
		}
		if signaled && sig != syscall.SIGTERM && sig != syscall.SIGINT {
			return true
		}
		return false
	case RestartNever:
		return false
	default:
		return false
	}
}

// BackoffDuration 返回退避等待时长
// REQ-F-008: 公式 min(backoff_ms * multiplier^(retries-1), max_backoff_ms)
// retries=1（第1次重启）: 延迟 = backoff_ms
// retries=2（第2次重启）: 延迟 = backoff_ms * multiplier
// B-04-001 修复：加锁保护 retries 读取
func (e *RestartEngine) BackoffDuration() time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.retries == 0 {
		return 0
	}
	calculated := float64(e.backoffMs) * math.Pow(float64(e.multiplier), float64(e.retries-1))
	ms := math.Min(calculated, float64(e.maxBackoffMs))
	return time.Duration(ms) * time.Millisecond
}

// IncrementRetries 增加重试计数
// B-04-001 修复：加锁保护 retries 写入
func (e *RestartEngine) IncrementRetries() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.retries++
}

// MaxRetriesReached 是否达到最大重试次数
// REQ-F-008: max_retries=0表示不限制
// B-04-001 修复：加锁保护 retries 读取
func (e *RestartEngine) MaxRetriesReached() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.maxRetries == 0 {
		return false
	}
	return e.retries >= e.maxRetries
}

// RecordStart 记录服务启动时间（用于reset_after_seconds计算）
// B-04-001 修复：加锁保护 lastStartTime 写入
func (e *RestartEngine) RecordStart() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lastStartTime = time.Now()
}

// ResetIfNeeded 如果服务连续运行超过reset_after_seconds，重置retries
// REQ-F-008: 重置retries计数器+退避时间
// B-04-001 修复：加锁保护 retries/lastStartTime 读写
//
// A-02-001 修复说明（语义澄清）：
// 规格字面表述为"立即重置"，但 retries 计数器仅在以下两个决策点被读取：
//   1. superviseService 中服务退出后调用 ShouldRestart/MaxRetriesReached 前（bootstrap.go:706）
//   2. service_operator.go:348 手动重启 API 调用前
// 因此"退出时检查并重置"与"运行期间立即重置"在功能上等价——
// 在退出决策点看到的就是已重置的计数器值。
// 不引入后台定时器主动轮询，避免增加 superviseService 复杂度（M-04-001 已记录）。
func (e *RestartEngine) ResetIfNeeded() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.resetAfterSeconds <= 0 {
		return
	}
	if e.lastStartTime.IsZero() {
		return
	}
	if time.Since(e.lastStartTime) >= time.Duration(e.resetAfterSeconds)*time.Second {
		e.retries = 0
	}
}

// Retries 返回当前重试计数
// B-04-001 修复：加锁保护 retries 读取
func (e *RestartEngine) Retries() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.retries
}

// Policy 返回当前重启策略
// A-02-001 修复：调用方需根据策略区分 failed vs down 状态
func (e *RestartEngine) Policy() RestartPolicy {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.policy
}

// SyncConfigFrom 用 other 的配置字段原地更新当前 engine，保留 retries/lastStartTime 运行时状态。
// 规格 §2.4.3: restart 配置变更"立即生效"，热重载时对已有 engine 原地更新，
// 使正在重试循环中的服务下次决策（ShouldRestart/MaxRetriesReached）使用最新配置。
// 例如 max_retries 从 0（无限）改为 5 后，已累积 100 次重试的服务下次 MaxRetriesReached 即为 true。
func (e *RestartEngine) SyncConfigFrom(other *RestartEngine) {
	if other == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	other.mu.Lock()
	defer other.mu.Unlock()
	e.policy = other.policy
	e.backoffMs = other.backoffMs
	e.maxBackoffMs = other.maxBackoffMs
	e.multiplier = other.multiplier
	e.maxRetries = other.maxRetries
	e.resetAfterSeconds = other.resetAfterSeconds
}
