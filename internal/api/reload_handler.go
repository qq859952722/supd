package api

import (
	"net/http"

	"github.com/supdorg/supd/internal/errors"
	"github.com/supdorg/supd/internal/watch"
)

// triggerReload 执行配置热重载核心逻辑：扫描 → 保留旧配置 → 更新 providers → 发布事件。
// R-001 修复：抽取为独立方法，供 handleReload 和导入确认 handler 复用。
// 返回 newDiscovery、扫描错误数、错误详情，供调用方构造响应。
func (s *Server) triggerReload() (newDiscovery *watch.DiscoveryResult, errCount int, errDetails []map[string]string) {
	wp, ok := s.watchProvider.(*CoreWatchProvider)
	if !ok || wp == nil {
		return nil, -1, nil // -1 表示 watch provider 未配置（调用方需特殊处理）
	}

	oldDiscovery := wp.Discovery
	disc := watch.NewDiscovery(wp.BaseDir, wp.LogDir)
	newDiscovery = disc.Scan()

	// N-04-I2 修复：配置错误时保留旧配置（不中断服务）
	watch.PreserveOldConfigOnError(oldDiscovery, newDiscovery)

	// 更新所有 providers 的 Discovery 引用
	s.UpdateDiscovery(newDiscovery)

	// 收集扫描错误摘要（C-03-001：不再静默吞错）
	errCount = len(newDiscovery.Errors)
	if errCount > 0 {
		errDetails = make([]map[string]string, 0, errCount)
		for _, e := range newDiscovery.Errors {
			errDetails = append(errDetails, map[string]string{
				"path":    e.Path,
				"message": e.Message,
			})
		}
	}

	// 发送 config_reloaded 事件（规格 §2.9.7）
	if s.eventRing != nil {
		s.eventRing.Publish("config_reloaded", map[string]any{
			"source":            "api",
			"immediate_changes": 0,
			"pending_changes":   0,
			"errors":            errCount,
		})
		// N-04-I1 修复：有扫描错误时发布 config_reload_failed 事件
		if errCount > 0 {
			s.eventRing.Publish("config_reload_failed", map[string]any{
				"source": "api",
				"errors": errDetails,
			})
		}
	}
	return newDiscovery, errCount, errDetails
}

// handleReload POST /api/reload
// N-04-002 修复：手动触发配置热重载端点
// 规格 §2.4.2 热重载是核心特性，自动重载失效时用户需可手动触发
// C-03-001 修复：扫描产生的 DiscoveryError 计入响应和事件，避免静默吞错
func (s *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	newDiscovery, errCount, errDetails := s.triggerReload()
	if newDiscovery == nil {
		respondError(w, errors.ErrInternal, "watch provider not configured")
		return
	}

	// N-04-I3 修复：有扫描错误时状态为 partial（部分成功）
	status := "ok"
	if errCount > 0 {
		status = "partial"
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"status":            status,
		"message":           "configuration reloaded",
		"services":          len(newDiscovery.Services),
		"global_extensions": len(newDiscovery.GlobalExts),
		"scan_errors":       errCount,
		"error_details":     errDetails,
	})
}
