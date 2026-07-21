package api

import (
	"net/http"

	"github.com/supdorg/supd/internal/errors"
	"github.com/supdorg/supd/internal/extension"
)

// REQ-D-004: 定时任务端点

// handleListCron GET /api/cron
// 列出所有 on_schedule 扩展及下次执行时间
func (s *Server) handleListCron(w http.ResponseWriter, r *http.Request) {
	if s.cronProvider == nil {
		respondError(w, errors.ErrInternal, "cron provider not configured")
		return
	}

	entries := s.cronProvider.ListCronEntries()
	respondJSON(w, http.StatusOK, entries)
}

// handleCronHistory GET /api/cron/history
func (s *Server) handleCronHistory(w http.ResponseWriter, r *http.Request) {
	if s.cronProvider == nil {
		respondError(w, errors.ErrInternal, "cron provider not configured")
		return
	}

	filter := extension.RunFilter{
		// 仅查询 on_schedule 触发的任务
	}
	// 按触发类型过滤在 provider 实现中处理

	runs := s.cronProvider.ListCronHistory(filter)
	if runs == nil {
		runs = []*extension.RunResult{}
	}
	respondJSON(w, http.StatusOK, runs)
}
