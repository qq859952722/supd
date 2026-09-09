package api

import (
	"context"
	"time"

	"github.com/supdorg/supd/internal/store"
)

// nowAPI 当前 Unix 毫秒时间戳（软删/已读时间戳）。
func nowAPI() int64 { return time.Now().UnixMilli() }

// NotificationProvider 通知中心 API 的最小依赖面（store 查询/写 + changes epoch/seq）。
// handler 不直接依赖 core 实现（§十二.5.3）；实现由 Node 08-3 的 CoreNotificationProvider 提供。
type NotificationProvider interface {
	ListTopics(ctx context.Context, f store.TopicFilter) ([]store.TopicItem, error)
	GetTopic(ctx context.Context, id string) (*store.TopicDetail, error)
	ListNotifications(ctx context.Context, topicID string, sinceSeq int64, limit int) ([]store.Notification, error)
	// MarkRead 将 Topic 已读游标推进到当前 last_seq，返回新 read_seq。
	MarkRead(ctx context.Context, topicID string) (int64, error)
	MarkAllRead(ctx context.Context) error
	DeleteTopic(ctx context.Context, topicID string) error
	DeleteAllTopics(ctx context.Context) error

	// StoreError 磁盘满等可观测错误态（API 顶层 store_error 字段）。
	StoreError() *store.StoreError

	// changes 长轮询相关。
	Epoch() string
	GlobalSeq() int64
	// WaitForGlobalChange 等待 GlobalSeq 超过 since 的新写事件或 ctx 取消（不持有 DB 连接）。
	WaitForGlobalChange(ctx context.Context, since int64) <-chan struct{}
}

// CoreNotificationProvider 实现 NotificationProvider，直接包装单连接 Store。
type CoreNotificationProvider struct {
	store *store.Store
}

// NewCoreNotificationProvider 创建通知中心 Provider。
func NewCoreNotificationProvider(st *store.Store) *CoreNotificationProvider {
	return &CoreNotificationProvider{store: st}
}

func (p *CoreNotificationProvider) ListTopics(ctx context.Context, f store.TopicFilter) ([]store.TopicItem, error) {
	if p.store == nil {
		return []store.TopicItem{}, nil
	}
	return p.store.ListTopics(ctx, f)
}

func (p *CoreNotificationProvider) GetTopic(ctx context.Context, id string) (*store.TopicDetail, error) {
	if p.store == nil {
		return nil, nil
	}
	return p.store.GetTopic(ctx, id)
}

func (p *CoreNotificationProvider) ListNotifications(ctx context.Context, topicID string, sinceSeq int64, limit int) ([]store.Notification, error) {
	if p.store == nil {
		return []store.Notification{}, nil
	}
	return p.store.ListNotifications(ctx, topicID, sinceSeq, limit)
}

func (p *CoreNotificationProvider) MarkRead(ctx context.Context, topicID string) (int64, error) {
	if p.store == nil {
		return 0, nil
	}
	d, err := p.store.GetTopic(ctx, topicID)
	if err != nil {
		return 0, err
	}
	if d == nil {
		return 0, store.ErrTopicNotFound
	}
	if err := p.store.MarkRead(ctx, topicID, d.LastSeq); err != nil {
		return 0, err
	}
	return d.LastSeq, nil
}

func (p *CoreNotificationProvider) MarkAllRead(ctx context.Context) error {
	if p.store == nil {
		return nil
	}
	return p.store.MarkAllRead(ctx)
}

func (p *CoreNotificationProvider) DeleteTopic(ctx context.Context, topicID string) error {
	if p.store == nil {
		return nil
	}
	d, err := p.store.GetTopic(ctx, topicID)
	if err != nil {
		return err
	}
	if d == nil {
		return store.ErrTopicNotFound
	}
	return p.store.DeleteTopic(ctx, topicID, nowAPI())
}

func (p *CoreNotificationProvider) DeleteAllTopics(ctx context.Context) error {
	if p.store == nil {
		return nil
	}
	return p.store.DeleteAllTopics(ctx, nowAPI())
}

func (p *CoreNotificationProvider) StoreError() *store.StoreError {
	if p.store == nil {
		return nil
	}
	return p.store.StoreError()
}

func (p *CoreNotificationProvider) Epoch() string {
	if p.store == nil {
		return ""
	}
	return p.store.Epoch()
}

func (p *CoreNotificationProvider) GlobalSeq() int64 {
	if p.store == nil {
		return 0
	}
	return p.store.GlobalSeq()
}

func (p *CoreNotificationProvider) WaitForGlobalChange(ctx context.Context, since int64) <-chan struct{} {
	if p.store == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return p.store.WaitForGlobalChange(ctx, since)
}