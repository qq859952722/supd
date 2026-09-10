// Package notification 定义通知协议解析与通知接收接口（NotifyRouter 落库路由）。
//
// 路由决策（设计稿 §六.2）：本层实现 NotificationSink，对来自服务/扩展 stdout 的
// ::notify:: 行补充来源上下文并决定目标 Topic：
//
//	a. 操作扩展 Run（ExecutionID 非空且其 Topic 未关闭）→ 操作 Topic；
//	b1. 非操作服务级扩展 Run → 所属服务默认 Topic（kind=service, source=service_name）；
//	b2. 非操作全局扩展 Run → 该扩展默认 Topic（kind=extension, source=extension_name）；
//	c. 普通服务进程 stdout → 该服务默认 Topic（kind=service）；
//	d. 默认 Topic 不存在 → 首条通知在同一事务内自动创建（writer 内保证）。
//
// 来源字段只来自 supd 侧闭包（脚本不可伪造）；全部经 Store 有界 TryEnqueue（满则丢弃并计数）。
// 本包依赖 store；store 不反向依赖本包（唯一方向）。本层只能写 store，不得反向依赖
// logging/extension/core。
package notification

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/supdorg/supd/internal/store"
)

// lateWarnInterval 已关闭 Topic 迟到通知 warning 日志限频：每 Topic 每分钟 1 条。
const lateWarnInterval = time.Minute

// NotifyRouter 实现 NotificationSink：路由 + 落库（§六.2）。
type NotifyRouter struct {
	store *store.Store

	mu       sync.Mutex
	lastWarn map[string]time.Time // key=executionID → 上次 warning 时间（限频）
}

// NewRouter 创建通知路由器。
func NewRouter(st *store.Store) *NotifyRouter {
	return &NotifyRouter{store: st, lastWarn: make(map[string]time.Time)}
}

// TryEnqueue 非阻塞提交一条通知。
// 返回值语义（与节点 03 接口一致）：true=已受理（含迟交拒绝、内容超限丢弃等"自主处理"）；
// false=底层有界队列满，通知被丢弃（调用方可计数）。
// 所有路径均不阻塞 stdout reader，且不影响 Run/服务状态。
func (r *NotifyRouter) TryEnqueue(pn PendingNotification) bool {
	// 内容二次防御：重建协议行总长 > 8KB 视为不合法，整体不生成通知（协议行长上限 §六.5）。
	if !withinContentLimit(pn) {
		slog.Warn("notification content exceeds 8KB protocol limit, dropped", "content_len", len(pn.Content))
		return true
	}

	// 情形 a：操作扩展 Run。
	if pn.ExecutionID != "" {
		return r.routeOperation(pn)
	}

	// 情形 b2：非操作全局扩展 Run → 扩展默认 Topic。
	if pn.ExtensionName != "" && pn.ServiceName == "" {
		return r.appendDefault(TopicKindExtension, pn.ExtensionName, SrcTypeExtension, pn)
	}
	// 情形 b1：非操作服务级扩展 Run → 所属服务默认 Topic。
	if pn.ExtensionName != "" {
		return r.appendDefault(TopicKindService, pn.ServiceName, SrcTypeExtension, pn)
	}
	// 情形 c：普通服务进程 stdout → 该服务默认 Topic。
	return r.appendDefault(TopicKindService, pn.ServiceName, SrcTypeService, pn)
}

// routeOperation 情形 a：操作 Run 的通知写入其 operation Topic（未关闭时）。
// 已关闭/已删除/不存在 → 不落库，warning 限频日志。
func (r *NotifyRouter) routeOperation(pn PendingNotification) bool {
	topic, err := r.store.GetOperationTopic(context.Background(), pn.ExecutionID)
	if err != nil {
		// 查询失败：丢弃并记录，不影响 Run 状态。
		slog.Warn("get operation topic for notify routing failed",
			"execution_id", pn.ExecutionID, "error", err)
		return true
	}
	if topic == nil || topic.ClosedAt != nil {
		r.warnLate(pn.ExecutionID)
		return true
	}
	return r.store.TryAppendNotification(toInput(topic.ID, SrcTypeExtension, pn))
}

// appendDefault 情形 d：写入默认 Topic（不存在则在 writer 事务内创建）。非阻塞。
func (r *NotifyRouter) appendDefault(kind, source, srcType string, pn PendingNotification) bool {
	return r.store.TryAppendToDefaultTopic(kind, source, toInput("", srcType, pn))
}

// warnLate 已关闭 Topic 迟到通知：限频 warning（每 Topic 每分钟 1 条，v5 确认）。
func (r *NotifyRouter) warnLate(executionID string) {
	r.mu.Lock()
	now := time.Now()
	if last, ok := r.lastWarn[executionID]; ok && now.Sub(last) < lateWarnInterval {
		r.mu.Unlock()
		return
	}
	r.lastWarn[executionID] = now
	r.mu.Unlock()
	slog.Warn("notification to closed operation topic dropped",
		"execution_id", executionID)
}

// withinContentLimit 重建协议行（前缀+level+引号+content）并校验 ≤ 8192 字节。
func withinContentLimit(pn PendingNotification) bool {
	if len(pn.Content) == 0 {
		return true
	}
	line := notifyPrefix + string(pn.Level) + " \"" + pn.Content + "\""
	return len(line) <= notifyMaxLen
}

// toInput 将 PendingNotification 映射为 store.NotificationInput。
// 空串字符串字段转 nil 指针；TopicID 由调用方覆盖。
func toInput(topicID, srcType string, pn PendingNotification) store.NotificationInput {
	return store.NotificationInput{
		TopicID:       topicID,
		Level:         string(pn.Level),
		Content:       pn.Content,
		SourceType:    srcType,
		ServiceName:   strPtr(pn.ServiceName),
		ExtensionName: strPtr(pn.ExtensionName),
		ActionID:      strPtr(pn.ActionID),
		RunID:         strPtr(pn.RunID),
		ExecutionID:   strPtr(pn.ExecutionID),
	}
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// 常量别名，避免与 store 命名空间直接耦合（值等于 store 常量）。
const (
	TopicKindService   = store.TopicKindService
	TopicKindExtension = store.TopicKindExtension
	SrcTypeExtension   = store.SrcTypeExtension
	SrcTypeService     = store.SrcTypeService
)

// 断言编译期实现接口。
var _ NotifySink = (*NotifyRouter)(nil)
