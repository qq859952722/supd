package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/supdorg/supd/internal/errors"
	"github.com/supdorg/supd/internal/logging"
)

// REQ-I-006: 日志 API

// LogSearchResponse 日志搜索响应
type LogSearchResponse struct {
	Lines []LogLine `json:"lines"`
	Total int       `json:"total"`
}

// LogLine 日志行
type LogLine struct {
	Timestamp         string `json:"timestamp"`
	Level             string `json:"level"`
	Content           string `json:"content"`
	IsExtensionOutput bool   `json:"is_extension_output"`
}

// handleServiceLogs GET /api/services/{name}/logs
// REQ-I-006: 服务日志长轮询
func (s *Server) handleServiceLogs(w http.ResponseWriter, r *http.Request) {
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

	if s.logProvider == nil {
		respondError(w, errors.ErrInternal, "log provider not configured")
		return
	}

	// 解析 since 参数（字节位置）
	var sincePos int64
	sinceStr := r.URL.Query().Get("since")
	if sinceStr != "" {
		var err error
		sincePos, err = strconv.ParseInt(sinceStr, 10, 64)
		if err != nil {
			respondError(w, errors.ErrInvalidRequest, "invalid since parameter")
			return
		}
	}

	// 获取日志
	lines, newPos, err := s.logProvider.GetServiceLogs(name, sincePos)
	if err != nil {
		respondError(w, errors.ErrInternal, fmt.Sprintf("failed to get logs: %v", err))
		return
	}

	// REQ-I-006 / spec L1251: 长轮询 — wait=true 且无新数据时挂起最多 30 秒
	// spec L1690: 长轮询 30s 硬编码，不可配置
	// spec L1146: 不引入 ETag/If-Modified-Since 等机制
	wait := r.URL.Query().Get("wait") == "true"
	if wait && len(lines) == 0 && newPos == sincePos {
		// D-03-001 修复：日志长轮询端点必须受 LongPollLimiter 限制（规格 §1.2 全局50/单客户端5）
		// 原 LongPollLimiter.Middleware 仅应用于 /api/events，未覆盖日志长轮询端点，
		// 存在资源耗尽风险（恶意客户端可通过日志长轮询绕过限制耗尽连接池）。
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
			lines, newPos, err = s.logProvider.GetServiceLogs(name, sincePos)
			if err != nil {
				respondError(w, errors.ErrInternal, fmt.Sprintf("failed to get logs: %v", err))
				return
			}
			if len(lines) > 0 || newPos > sincePos {
				break
			}
		}
	}

	// 构造响应
	var data []any
	for _, line := range lines {
		data = append(data, map[string]string{
			"content": line,
		})
	}

	// 如果有数据直接返回，否则返回空结果
	response := LongPollResponse{
		Data:      data,
		NextSince: fmt.Sprintf("%d", newPos),
		HasMore:   false,
	}

	if data == nil {
		response.Data = []any{}
	}

	respondJSON(w, http.StatusOK, response)
}

// handleSearchLogs GET /api/services/{name}/logs/search
// REQ-I-006: 日志搜索，上限1000行（REQ-F-010数值锁定）
func (s *Server) handleSearchLogs(w http.ResponseWriter, r *http.Request) {
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

	if s.logProvider == nil {
		respondError(w, errors.ErrInternal, "log provider not configured")
		return
	}

	// 解析搜索参数
	q := r.URL.Query().Get("q")
	if q == "" {
		respondError(w, errors.ErrInvalidRequest, "search query 'q' is required")
		return
	}

	limit := logging.DefaultMaxLines // REQ-F-010: 默认1000
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		n, err := strconv.Atoi(limitStr)
		if err != nil || n < 1 {
			respondError(w, errors.ErrInvalidRequest, "invalid limit parameter")
			return
		}
		if n > logging.DefaultMaxLines {
			n = logging.DefaultMaxLines // REQ-F-010: 不超过1000
		}
		limit = n
	}

	// 解析时间范围参数（仅校验格式，实际过滤由底层实现）
	if sinceStr := r.URL.Query().Get("since"); sinceStr != "" {
		if _, err := time.Parse(time.RFC3339, sinceStr); err != nil {
			respondError(w, errors.ErrInvalidRequest, "invalid since parameter, expected ISO8601 format")
			return
		}
	}

	if untilStr := r.URL.Query().Get("until"); untilStr != "" {
		if _, err := time.Parse(time.RFC3339, untilStr); err != nil {
			respondError(w, errors.ErrInvalidRequest, "invalid until parameter, expected ISO8601 format")
			return
		}
	}

	// 调用日志搜索
	results, err := s.logProvider.SearchServiceLogs(name, q, limit)
	if err != nil {
		respondError(w, errors.ErrInternal, fmt.Sprintf("search failed: %v", err))
		return
	}

	// 转换为 API 响应格式
	response := LogSearchResponse{
		Lines: make([]LogLine, 0, len(results)),
		Total: len(results),
	}

	for _, line := range results {
		response.Lines = append(response.Lines, LogLine{
			Content: line,
		})
	}

	respondJSON(w, http.StatusOK, response)
}
