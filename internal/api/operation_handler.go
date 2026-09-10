package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/supdorg/supd/internal/errors"
)

// runOperationRequest POST /api/operations/{id}/run 请求体：可选 {"params": <object>}。
type runOperationRequest struct {
	Params json.RawMessage `json:"params"`
}

// maxRunBodyBytes run 端点请求体上限（参数本身≤8KB，body 留足余量）。
const maxRunBodyBytes = 64 * 1024

// handleListOperations GET /api/operations
// 列出操作卡片：OperationInfo + 上次执行摘要。
func (s *Server) handleListOperations(w http.ResponseWriter, r *http.Request) {
	if s.operationProvider == nil {
		respondError(w, errors.ErrInternal, "operation provider not configured")
		return
	}
	cards, err := s.operationProvider.ListOperations(r.Context())
	if err != nil {
		respondProviderError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, cards)
}

// handleGetOperation GET /api/operations/{id}
// 单操作详情（含注册者/响应者/warning）。
func (s *Server) handleGetOperation(w http.ResponseWriter, r *http.Request) {
	if s.operationProvider == nil {
		respondError(w, errors.ErrInternal, "operation provider not configured")
		return
	}
	id := chi.URLParam(r, "id")
	det, ok := s.operationProvider.GetOperation(r.Context(), id)
	if !ok {
		respondError(w, errors.ErrExtensionNotFound, "operation "+id+" not found")
		return
	}
	respondJSON(w, http.StatusOK, det)
}

// handleRunOperation POST /api/operations/{id}/run
// 触发操作：body {"params": <object>}，可选 Idempotency-Key header。
func (s *Server) handleRunOperation(w http.ResponseWriter, r *http.Request) {
	if s.operationProvider == nil {
		respondError(w, errors.ErrInternal, "operation provider not configured")
		return
	}
	id := chi.URLParam(r, "id")

	r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)
	var req runOperationRequest
	if err := decodeJSONBody(r, &req); err != nil {
		respondError(w, errors.ErrInvalidRequest, err.Error())
		return
	}

	idemKey := r.Header.Get("Idempotency-Key")
	result, err := s.operationProvider.RunOperation(r.Context(), id, req.Params, idemKey)
	if err != nil {
		respondProviderError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, result)
}

// handleListOperationExecutions GET /api/operation-executions
// 执行历史列表（分页：limit/offset，limit 上限 1000）。
func (s *Server) handleListOperationExecutions(w http.ResponseWriter, r *http.Request) {
	if s.operationProvider == nil {
		respondError(w, errors.ErrInternal, "operation provider not configured")
		return
	}
	limit := 50
	offset := 0
	if limStr := r.URL.Query().Get("limit"); limStr != "" {
		v, err := strconv.Atoi(limStr)
		if err != nil || v < 0 {
			respondError(w, errors.ErrInvalidRequest, "invalid limit parameter")
			return
		}
		if v > 1000 {
			v = 1000
		}
		limit = v
	}
	if offStr := r.URL.Query().Get("offset"); offStr != "" {
		v, err := strconv.Atoi(offStr)
		if err != nil || v < 0 {
			respondError(w, errors.ErrInvalidRequest, "invalid offset parameter")
			return
		}
		offset = v
	}
	execs, err := s.operationProvider.ListExecutions(r.Context(), limit, offset)
	if err != nil {
		respondProviderError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, execs)
}

// handleGetOperationExecution GET /api/operation-executions/{id}
// 执行详情（runs 快照 + topic 链接）。
func (s *Server) handleGetOperationExecution(w http.ResponseWriter, r *http.Request) {
	if s.operationProvider == nil {
		respondError(w, errors.ErrInternal, "operation provider not configured")
		return
	}
	id := chi.URLParam(r, "id")
	det, ok := s.operationProvider.GetExecution(r.Context(), id)
	if !ok {
		respondError(w, errors.ErrRunNotFound, "operation execution "+id+" not found")
		return
	}
	respondJSON(w, http.StatusOK, det)
}

// handleDeleteOperationExecution DELETE /api/operation-executions/{id}
// 删除单条执行记录（run 级联；关联通知 Topic 不受影响）。
func (s *Server) handleDeleteOperationExecution(w http.ResponseWriter, r *http.Request) {
	if s.operationProvider == nil {
		respondError(w, errors.ErrInternal, "operation provider not configured")
		return
	}
	id := chi.URLParam(r, "id")
	exists, err := s.operationProvider.DeleteExecution(r.Context(), id)
	if err != nil {
		respondProviderError(w, err)
		return
	}
	if !exists {
		respondError(w, errors.ErrRunNotFound, "operation execution "+id+" not found")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleDeleteAllOperationExecutions DELETE /api/operation-executions
// 清空全部执行记录。
func (s *Server) handleDeleteAllOperationExecutions(w http.ResponseWriter, r *http.Request) {
	if s.operationProvider == nil {
		respondError(w, errors.ErrInternal, "operation provider not configured")
		return
	}
	if err := s.operationProvider.DeleteAllExecutions(r.Context()); err != nil {
		respondProviderError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}
