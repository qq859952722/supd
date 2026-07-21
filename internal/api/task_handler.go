package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/supdorg/supd/internal/errors"
	"github.com/supdorg/supd/internal/extension"
)

// REQ-F-020: 任务查询+取消+日志端点

// handleListRuns GET /api/extensions/runs
// 查询参数：status, include_recent, limit
func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	if s.taskProvider == nil {
		respondError(w, errors.ErrInternal, "task provider not configured")
		return
	}

	filter := extension.RunFilter{}

	// status 过滤
	if statusStr := r.URL.Query().Get("status"); statusStr != "" {
		filter.State = extension.TaskState(statusStr)
	}

	// extension 过滤（按扩展名查询历史）
	// 同时支持 extension 和 extension_name 两种参数名
	if extName := r.URL.Query().Get("extension_name"); extName != "" {
		filter.ExtensionName = extName
	} else if extName := r.URL.Query().Get("extension"); extName != "" {
		filter.ExtensionName = extName
	}

	// service 过滤（按服务名查询关联扩展的运行历史）
	// 同时支持 service 和 service_name 两种参数名
	if svcName := r.URL.Query().Get("service_name"); svcName != "" {
		filter.ServiceName = svcName
	} else if svcName := r.URL.Query().Get("service"); svcName != "" {
		filter.ServiceName = svcName
	}

	// trigger_type 过滤（按触发类型过滤）
	if tt := r.URL.Query().Get("trigger_type"); tt != "" {
		filter.TriggerType = tt
	}

	// limit 限制
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil {
			respondError(w, errors.ErrInvalidRequest, "invalid limit parameter")
			return
		}
		// D-01-001 修复：limit 上限校验（防止客户端请求过大数据集导致内存压力）
		// 与 handleSearchLogs 的 1000 行上限保持一致（log_handler.go:159-169）
		if limit < 0 {
			respondError(w, errors.ErrInvalidRequest, "limit must be non-negative")
			return
		}
		if limit > 1000 {
			limit = 1000
		}
		filter.Limit = limit
	}

	runs := s.taskProvider.ListRuns(filter)
	respondJSON(w, http.StatusOK, runs)
}

// handleGetRun GET /api/extensions/runs/{runID}
func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	if s.taskProvider == nil {
		respondError(w, errors.ErrInternal, "task provider not configured")
		return
	}

	runID := chi.URLParam(r, "runID")
	run := s.taskProvider.GetRun(runID)
	if run == nil {
		respondError(w, errors.ErrRunNotFound, "run not found")
		return
	}

	respondJSON(w, http.StatusOK, run)
}

// handleGetRunLogs GET /api/extensions/runs/{runID}/logs
// 长轮询：since_pos 参数
func (s *Server) handleGetRunLogs(w http.ResponseWriter, r *http.Request) {
	if s.taskProvider == nil {
		respondError(w, errors.ErrInternal, "task provider not configured")
		return
	}

	runID := chi.URLParam(r, "runID")

	// 确认 run 存在
	run := s.taskProvider.GetRun(runID)
	if run == nil {
		respondError(w, errors.ErrRunNotFound, "run not found")
		return
	}

	// 解析 since_pos 参数
	var sincePos int64
	if posStr := r.URL.Query().Get("since_pos"); posStr != "" {
		pos, err := strconv.ParseInt(posStr, 10, 64)
		if err != nil {
			respondError(w, errors.ErrInvalidRequest, "invalid since_pos parameter")
			return
		}
		sincePos = pos
	}

	lines, newPos, err := s.taskProvider.GetRunLogs(runID, sincePos)
	if err != nil {
		respondError(w, errors.ErrInternal, err.Error())
		return
	}

	// spec L1298: 长轮询 — wait=true 且无新数据时挂起最多 30 秒
	// spec L1690: 长轮询 30s 硬编码；仅在任务仍处于活跃态（running/pending）时挂起
	// spec L1146: 不引入 ETag/If-Modified-Since 等机制
	wait := r.URL.Query().Get("wait") == "true"
	isActive := run != nil && (run.State == extension.TaskRunning || run.State == extension.TaskPending)
	if wait && isActive && len(lines) == 0 && newPos == sincePos {
		// D-03-001 修复：任务日志长轮询端点必须受 LongPollLimiter 限制（规格 §1.2 全局50/单客户端5）
		// 原 LongPollLimiter.Middleware 仅应用于 /api/events，未覆盖任务日志长轮询端点，
		// 存在资源耗尽风险（恶意客户端可通过任务日志长轮询绕过限制耗尽连接池）。
		clientKey := extractClientIP(r).String()
		if !s.longPollLimiter.Acquire(clientKey) {
			respondError(w, errors.ErrServiceBusy, "too many concurrent long-poll requests")
			return
		}
		defer s.longPollLimiter.Release(clientKey)

		const longPollTimeout = 30 * time.Second // spec L1690
		const pollInterval = 200 * time.Millisecond
		deadline := time.Now().Add(longPollTimeout)
		for time.Now().Before(deadline) {
			// 客户端断开时立即退出，避免 goroutine 泄漏
			select {
			case <-r.Context().Done():
				return
			default:
			}
			time.Sleep(pollInterval)
			lines, newPos, err = s.taskProvider.GetRunLogs(runID, sincePos)
			if err != nil {
				respondError(w, errors.ErrInternal, err.Error())
				return
			}
			if len(lines) > 0 || newPos > sincePos {
				break
			}
			// 任务进入终态时退出（不再会有新日志）
			if run = s.taskProvider.GetRun(runID); run == nil ||
				(run.State != extension.TaskRunning && run.State != extension.TaskPending) {
				isActive = false
				break
			}
		}
	}

	// F-02-001: 根据 run.State 计算 has_more，任务仍在运行时可能产生新日志
	// 前端依赖 has_more 决定是否继续轮询（TanStack Query refetchInterval）
	// F-02-005: pending 也属于活跃态，前端应继续轮询直至进入终态
	hasMore := isActive
	respondJSON(w, http.StatusOK, map[string]any{
		"lines":     lines,
		"next_pos":  newPos,
		"has_more":  hasMore,
	})
}

// handleCancelRun POST /api/extensions/runs/{runID}/cancel
// REQ-D-009: 取消已完成的任务→409 RUN_ALREADY_DONE
func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	if s.taskProvider == nil {
		respondError(w, errors.ErrInternal, "task provider not configured")
		return
	}

	runID := chi.URLParam(r, "runID")

	if err := s.taskProvider.CancelRun(runID); err != nil {
		respondError(w, errors.ErrRunAlreadyDone, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteRunLogs DELETE /api/extensions/runs/{runID}/logs
// 清空指定运行的日志文件
func (s *Server) handleDeleteRunLogs(w http.ResponseWriter, r *http.Request) {
	if s.taskProvider == nil {
		respondError(w, errors.ErrInternal, "task provider not configured")
		return
	}

	runID := chi.URLParam(r, "runID")

	if err := s.taskProvider.DeleteRunLogs(runID); err != nil {
		respondError(w, errors.ErrInternal, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleClearRuns DELETE /api/extensions/runs
// 清空匹配条件的终态运行记录（支持 extension_name / service_name 查询参数）
func (s *Server) handleClearRuns(w http.ResponseWriter, r *http.Request) {
	if s.taskProvider == nil {
		respondError(w, errors.ErrInternal, "task provider not configured")
		return
	}

	filter := extension.RunFilter{}
	if extName := r.URL.Query().Get("extension_name"); extName != "" {
		filter.ExtensionName = extName
	} else if extName := r.URL.Query().Get("extension"); extName != "" {
		filter.ExtensionName = extName
	}
	if svcName := r.URL.Query().Get("service_name"); svcName != "" {
		filter.ServiceName = svcName
	} else if svcName := r.URL.Query().Get("service"); svcName != "" {
		filter.ServiceName = svcName
	}

	deleted := s.taskProvider.ClearRuns(filter)
	respondJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}
