package api

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/supdorg/supd/internal/errors"
)

// REQ-I-006: 服务历史 API
// HistoryEntry, HistoryResponse types are defined in interfaces.go

// HistoryResponse 历史记录响应
type HistoryResponse struct {
	Entries []HistoryEntry `json:"entries"`
}

// handleServiceHistory GET /api/services/{name}/history
// REQ-I-006: 获取服务启动/停止历史
func (s *Server) handleServiceHistory(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		respondError(w, errors.ErrInvalidRequest, "service name is required")
		return
	}

	if s.stateProvider != nil {
		if _, exists := s.stateProvider.GetServiceState(name); !exists {
			respondError(w, errors.ErrServiceNotFound, fmt.Sprintf("service %s not found", name))
			return
		}
	}

	if s.serviceHistoryGetter == nil {
		respondError(w, errors.ErrInternal, "service history getter not configured")
		return
	}

	entries := s.serviceHistoryGetter.GetServiceHistory(name)
	if entries == nil {
		entries = []HistoryEntry{}
	}

	respondJSON(w, http.StatusOK, HistoryResponse{Entries: entries})
}

// handleServiceDeaths GET /api/services/{name}/deaths
// REQ-I-006: 获取服务异常退出记录
func (s *Server) handleServiceDeaths(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		respondError(w, errors.ErrInvalidRequest, "service name is required")
		return
	}

	if s.stateProvider != nil {
		if _, exists := s.stateProvider.GetServiceState(name); !exists {
			respondError(w, errors.ErrServiceNotFound, fmt.Sprintf("service %s not found", name))
			return
		}
	}

	if s.serviceHistoryGetter == nil {
		respondError(w, errors.ErrInternal, "service history getter not configured")
		return
	}

	entries := s.serviceHistoryGetter.GetServiceDeaths(name)
	if entries == nil {
		entries = []HistoryEntry{}
	}

	respondJSON(w, http.StatusOK, HistoryResponse{Entries: entries})
}
