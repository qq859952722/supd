package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	svcerr "github.com/supdorg/supd/internal/errors"
	"github.com/supdorg/supd/internal/store"
)

// 通知中心 7 端点（设计稿 §八）：GET topics / GET topics{id} / POST read / POST read-all /
// DELETE topics{id} / DELETE topics / GET changes。明确不存在通知写 API（§八）。

// changes 相关常量。
const (
	// changesWaitMax wait 参数上限（秒）。
	changesWaitMax = 30
	// changesDefaultWait wait 默认值（秒）。
	changesDefaultWait = 30
	// topicsDetailDefaultLimit 详情分页默认条数。
	topicsDetailDefaultLimit = 200
	// topicsDetailMaxLimit 详情分页最大条数。
	topicsDetailMaxLimit = 500
)

// topicsResponse GET /api/notifications/topics 响应。
type topicsResponse struct {
	Topics     []store.TopicItem `json:"topics"`
	StoreError *store.StoreError `json:"store_error"`
}

// topicDetailResponse GET /api/notifications/topics/{id} 响应。
type topicDetailResponse struct {
	Topic         *store.TopicDetail   `json:"topic"`
	Notifications []store.Notification `json:"notifications"`
	StoreError    *store.StoreError    `json:"store_error"`
	NextSeq       int64                `json:"next_seq"`
	HasMore       bool                 `json:"has_more"`
}

// mutResult 变更类端点成功响应。
type mutResult struct {
	OK bool `json:"ok"`
}

// changesResponse GET /api/notifications/changes 响应。
type changesResponse struct {
	Reload bool   `json:"reload"`
	Epoch  string `json:"epoch"`
	Seq    int64  `json:"seq"`
}

// notificationProvider 快捷访问（nil 安全）。
func (s *Server) notifProvider() NotificationProvider { return s.notificationProvider }

// handleListTopics GET /api/notifications/topics
// 筛选：kind / unread / level / source_type / service_name；按最近 notification.created_at 倒序。
func (s *Server) handleListTopics(w http.ResponseWriter, r *http.Request) {
	prov := s.notifProvider()
	if prov == nil {
		respondError(w, svcerr.ErrInternal, "notification provider not configured")
		return
	}
	f := store.TopicFilter{
		Kind:        r.URL.Query().Get("kind"),
		Level:       r.URL.Query().Get("level"),
		SourceType:  r.URL.Query().Get("source_type"),
		ServiceName: r.URL.Query().Get("service_name"),
	}
	if u := r.URL.Query().Get("unread"); u == "true" || u == "1" {
		f.Unread = true
	}
	topics, err := prov.ListTopics(r.Context(), f)
	if err != nil {
		respondProviderError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, topicsResponse{Topics: topics, StoreError: prov.StoreError()})
}

// handleGetTopic GET /api/notifications/topics/{id}
// 详情 + 分页通知（seq 正序）。查询参数 ?seq=<sinceSeq>&limit=<n>。
func (s *Server) handleGetTopic(w http.ResponseWriter, r *http.Request) {
	prov := s.notifProvider()
	if prov == nil {
		respondError(w, svcerr.ErrInternal, "notification provider not configured")
		return
	}
	id := chi.URLParam(r, "id")
	since := int64(0)
	if sv := r.URL.Query().Get("seq"); sv != "" {
		v, err := strconv.ParseInt(sv, 10, 64)
		if err != nil || v < 0 {
			respondError(w, svcerr.ErrInvalidRequest, "invalid seq parameter")
			return
		}
		since = v
	}
	limit := topicsDetailDefaultLimit
	if lv := r.URL.Query().Get("limit"); lv != "" {
		v, err := strconv.Atoi(lv)
		if err != nil || v < 0 {
			respondError(w, svcerr.ErrInvalidRequest, "invalid limit parameter")
			return
		}
		if v > topicsDetailMaxLimit {
			v = topicsDetailMaxLimit
		}
		limit = v
	}

	detail, err := prov.GetTopic(r.Context(), id)
	if err != nil {
		respondProviderError(w, err)
		return
	}
	if detail == nil {
		respondError(w, svcerr.ErrFileNotFound, "notification topic "+id+" not found")
		return
	}
	notifs, err := prov.ListNotifications(r.Context(), id, since, limit)
	if err != nil {
		respondProviderError(w, err)
		return
	}
	nextSeq := detail.LastSeq
	if len(notifs) > 0 {
		nextSeq = notifs[len(notifs)-1].Seq
	}
	hasMore := len(notifs) == limit
	respondJSON(w, http.StatusOK, topicDetailResponse{
		Topic: detail, Notifications: notifs, StoreError: prov.StoreError(),
		NextSeq: nextSeq, HasMore: hasMore,
	})
}

// handleReadTopic POST /api/notifications/topics/{id}/read
// read_seq=last_seq（游标只前进）。
func (s *Server) handleReadTopic(w http.ResponseWriter, r *http.Request) {
	prov := s.notifProvider()
	if prov == nil {
		respondError(w, svcerr.ErrInternal, "notification provider not configured")
		return
	}
	id := chi.URLParam(r, "id")
	seq, err := prov.MarkRead(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrTopicNotFound) {
			respondError(w, svcerr.ErrFileNotFound, "notification topic "+id+" not found")
			return
		}
		respondProviderError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"read_seq": seq})
}

// handleReadAll POST /api/notifications/read-all
func (s *Server) handleReadAll(w http.ResponseWriter, r *http.Request) {
	prov := s.notifProvider()
	if prov == nil {
		respondError(w, svcerr.ErrInternal, "notification provider not configured")
		return
	}
	if err := prov.MarkAllRead(r.Context()); err != nil {
		respondProviderError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, mutResult{OK: true})
}

// handleDeleteTopic DELETE /api/notifications/topics/{id}（软删）
func (s *Server) handleDeleteTopic(w http.ResponseWriter, r *http.Request) {
	prov := s.notifProvider()
	if prov == nil {
		respondError(w, svcerr.ErrInternal, "notification provider not configured")
		return
	}
	id := chi.URLParam(r, "id")
	if err := prov.DeleteTopic(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrTopicNotFound) {
			respondError(w, svcerr.ErrFileNotFound, "notification topic "+id+" not found")
			return
		}
		respondProviderError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, mutResult{OK: true})
}

// handleDeleteAllTopics DELETE /api/notifications/topics（清空，全部软删）
func (s *Server) handleDeleteAllTopics(w http.ResponseWriter, r *http.Request) {
	prov := s.notifProvider()
	if prov == nil {
		respondError(w, svcerr.ErrInternal, "notification provider not configured")
		return
	}
	if err := prov.DeleteAllTopics(r.Context()); err != nil {
		respondProviderError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, mutResult{OK: true})
}

// handleChanges GET /api/notifications/changes?epoch=&since=&wait=
//
// 语义（设计稿 §八 + v5）：
//   - epoch 不符 → reload=true + 当前 epoch/seq（前端全量重载）；
//   - since < GlobalSeq → 立即返回当前 GlobalSeq（有新写，无历史窗口）；
//   - since >= GlobalSeq → 阻塞等待新写事件（内存广播，不持有 DB 连接）或超时后返回。
//
// 复用 s.longPollLimiter（与 /api/events 共用全局 50 / 单客户端 5 额度），
// 超限返回 503/SERVICE_BUSY（规格 §2.6.5，2026-09-10 审计决策）。
func (s *Server) handleChanges(w http.ResponseWriter, r *http.Request) {
	prov := s.notifProvider()
	if prov == nil {
		respondError(w, svcerr.ErrInternal, "notification provider not configured")
		return
	}
	clientIP := extractClientIP(r).String()
	if !s.longPollLimiter.Acquire(clientIP) {
		respondError(w, svcerr.ErrServiceBusy, "too many concurrent long-poll requests")
		return
	}
	defer s.longPollLimiter.Release(clientIP)

	epoch := r.URL.Query().Get("epoch")
	since := int64(0)
	if sv := r.URL.Query().Get("since"); sv != "" {
		v, err := strconv.ParseInt(sv, 10, 64)
		if err != nil || v < 0 {
			respondError(w, svcerr.ErrInvalidRequest, "invalid since parameter")
			return
		}
		since = v
	}
	wait := changesDefaultWait
	if wv := r.URL.Query().Get("wait"); wv != "" {
		v, err := strconv.Atoi(wv)
		if err != nil {
			respondError(w, svcerr.ErrInvalidRequest, "invalid wait parameter")
			return
		}
		if v < 1 {
			v = 1
		}
		if v > changesWaitMax {
			v = changesWaitMax
		}
		wait = v
	}

	cur := prov.Epoch()
	if epoch != "" && epoch != cur {
		// epoch 不符 → 要求全量重载。
		respondJSON(w, http.StatusOK, changesResponse{Reload: true, Epoch: cur, Seq: prov.GlobalSeq()})
		return
	}

	if since < prov.GlobalSeq() {
		// 有新写（无历史窗口）→ 立即返回当前 seq。
		respondJSON(w, http.StatusOK, changesResponse{Reload: false, Epoch: cur, Seq: prov.GlobalSeq()})
		return
	}

	// since >= GlobalSeq：阻塞等待新写事件或超时。
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(wait)*time.Second)
	defer cancel()
	select {
	case <-prov.WaitForGlobalChange(ctx, since):
	case <-ctx.Done():
	}
	respondJSON(w, http.StatusOK, changesResponse{Reload: false, Epoch: cur, Seq: prov.GlobalSeq()})
}
