package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/supdorg/supd/internal/errors"
)

// REQ-2.9.7: 事件流端点
// 14种事件类型（锁定清单2.3节）：
// service_state/service_died/service_ready/service_failed/service_exited/
// extension_started/extension_completed/extension_failed/extension_canceled/
// extension_timeout/cron_triggered/config_reloaded/config_reload_failed/system_resource_warning

// DefaultRecentEventLimit 最近事件默认返回上限
// REQ-2.9.7: 事件环形缓冲200条（数值锁定）
const DefaultRecentEventLimit = 200

// Event 事件结构
type Event struct {
	Time    string `json:"time"`    // ISO8601带时区
	Type    string `json:"type"`    // 事件类型
	Payload any    `json:"payload"` // 事件数据
}

// handleEvents GET /api/events
// 长轮询端点，参数：since/wait/limit
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if s.eventRing == nil {
		respondError(w, errors.ErrInternal, "event ring buffer not configured")
		return
	}

	// 使用通用长轮询处理
	HandleLongPoll(w, r, s.eventRing.RefWall(), s.eventFetcher, s.eventRing.WaitForEvent)
}

// eventFetcher 事件数据获取函数
func (s *Server) eventFetcher(sinceNano int64, limit int) ([]any, int64, bool, error) {
	events, hasMore := s.eventRing.Since(sinceNano, limit)

	var result []any
	var newestNano int64
	for _, e := range events {
		event := Event{
			Time:    e.Time.Format(time.RFC3339Nano),
			Type:    e.Type,
			Payload: e.Payload,
		}
		result = append(result, event)
		newestNano = e.Nano
	}

	return result, newestNano, hasMore, nil
}

// handleRecentEvents GET /api/system/events/recent
// 参数：limit
func (s *Server) handleRecentEvents(w http.ResponseWriter, r *http.Request) {
	if s.eventRing == nil {
		respondError(w, errors.ErrInternal, "event ring buffer not configured")
		return
	}

	// limit 参数，默认200（REQ-2.9.7: 事件环形缓冲200条）
	limit := DefaultRecentEventLimit
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		l, err := strconv.Atoi(limitStr)
		if err != nil || l < 1 {
			respondError(w, errors.ErrInvalidRequest, "invalid limit parameter")
			return
		}
		limit = l
	}

	// N-05-I2 修复：使用 Recent 获取最新 limit 条事件（而非 Since 返回的最旧事件）
	events := s.eventRing.Recent(limit)

	var result []Event
	for _, e := range events {
		result = append(result, Event{
			Time:    e.Time.Format(time.RFC3339Nano),
			Type:    e.Type,
			Payload: e.Payload,
		})
	}

	respondJSON(w, http.StatusOK, result)
}
