package api

import (
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/supdorg/supd/internal/errors"
)

// REQ-F-028~030: 运行时管理端点

// RuntimeListResponse 运行时列表响应
type RuntimeListResponse struct {
	Runtimes []RuntimeEntry `json:"runtimes"`
	Default  string         `json:"default"` // bash
}

// RuntimeEntry 运行时条目
type RuntimeEntry struct {
	Alias     string `json:"alias"`
	Path      string `json:"path"`
	Source    string `json:"source"`    // builtin/config/scan
	Available bool   `json:"available"`
}

// handleListRuntimes GET /api/runtimes
// 列出所有运行时含 source 和 available
func (s *Server) handleListRuntimes(w http.ResponseWriter, r *http.Request) {
	if s.runtimeProvider == nil {
		respondError(w, errors.ErrInternal, "runtime provider not configured")
		return
	}

	runtimes := s.runtimeProvider.ListRuntimes()
	entries := make([]RuntimeEntry, 0, len(runtimes))
	for _, rt := range runtimes {
		entries = append(entries, RuntimeEntry(rt))
	}

	respondJSON(w, http.StatusOK, RuntimeListResponse{
		Runtimes: entries,
		Default:  "bash",
	})
}

// handleUploadRuntime POST /api/runtimes/upload
func (s *Server) handleUploadRuntime(w http.ResponseWriter, r *http.Request) {
	if s.runtimeProvider == nil {
		respondError(w, errors.ErrInternal, "runtime provider not configured")
		return
	}

	// 限制上传大小
	if s.config != nil && s.config.Settings.MaxUploadSizeMB > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, int64(s.config.Settings.MaxUploadSizeMB)*1024*1024)
	}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, errors.ErrFileTooLarge, "request body too large")
		return
	}

	// 从查询参数获取名称
	name := r.URL.Query().Get("name")
	if name == "" {
		respondError(w, errors.ErrInvalidRequest, "name parameter required")
		return
	}
	// S-03: 校验 name 防止路径穿越（绝对路径/含 / 或 ..）
	safeName := SanitizeFilename(name)
	if safeName == "" || safeName == "." || safeName == ".." || safeName != name {
		respondError(w, errors.ErrInvalidRequest, "invalid runtime name: must be a simple filename without path separators")
		return
	}

	if err := s.runtimeProvider.UploadRuntime(safeName, data); err != nil {
		respondError(w, errors.ErrInternal, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

// handleDeleteRuntime DELETE /api/runtimes/{name}
func (s *Server) handleDeleteRuntime(w http.ResponseWriter, r *http.Request) {
	if s.runtimeProvider == nil {
		respondError(w, errors.ErrInternal, "runtime provider not configured")
		return
	}

	name := chi.URLParam(r, "name")
	// S-03: 校验 name 防止路径穿越
	safeName := SanitizeFilename(name)
	if safeName == "" || safeName == "." || safeName == ".." || safeName != name {
		respondError(w, errors.ErrInvalidRequest, "invalid runtime name: must be a simple filename without path separators")
		return
	}

	if err := s.runtimeProvider.DeleteRuntime(safeName); err != nil {
		respondError(w, errors.ErrRuntimeNotFound, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
