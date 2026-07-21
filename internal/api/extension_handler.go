package api

import (
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/supdorg/supd/internal/archive"
	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/errors"
)

// REQ-F-014~024: 扩展 CRUD + 详情 + 运行端点

// ExtensionSummary 扩展列表项
// REQ-2.2.14: 扩展列表展示含状态、运行历史、触发时机
type ExtensionSummary struct {
	Name         string             `json:"name"`
	Version      string             `json:"version"`
	Description  string             `json:"description,omitempty"`
	Enabled      bool               `json:"enabled"`
	DisplayState string             `json:"display_state"`
	TriggerType  string             `json:"trigger_type"`
	Service      string             `json:"service,omitempty"`
	RunCount     int                `json:"run_count"`
	SuccessCount int                `json:"success_count"`
	FailCount    int                `json:"fail_count"`
	LastRunAt    string             `json:"last_run_at,omitempty"`
	LastStatus   string             `json:"last_status,omitempty"`
	// 完整触发器配置，便于前端展示所有触发条件（on_demand/on_schedule/service_lifecycle/supd_lifecycle）
	Triggers *config.Triggers `json:"triggers,omitempty"`
	// 动作列表，便于前端展示可执行的操作按钮
	Actions []config.Action `json:"actions,omitempty"`
	// 并发策略
	Concurrency string `json:"concurrency,omitempty"`
	// 运行时和入口（便于前端快速识别扩展用途）
	Runtime string `json:"runtime,omitempty"`
	Entry   string `json:"entry,omitempty"`
}

// ExtensionDetail 扩展详情
type ExtensionDetail struct {
	Name             string                `json:"name"`
	Version          string                `json:"version"`
	Description      string                `json:"description,omitempty"`
	Enabled          bool                  `json:"enabled"`
	Config           *config.ExtensionMeta `json:"config,omitempty"`
	DisplayState     string                `json:"display_state"`
	TriggerType      string                `json:"trigger_type,omitempty"`
	Concurrency      string                `json:"concurrency,omitempty"`
	RunCount         int                   `json:"run_count"`
	SuccessCount     int                   `json:"success_count"`
	FailCount        int                   `json:"fail_count"`
	Actions          []config.Action       `json:"actions,omitempty"`
	ConfigErrors     []string              `json:"config_errors,omitempty"`
	ConfigPath       string                `json:"config_path,omitempty"`
	EnvPath          string                `json:"env_path,omitempty"`
}

// RunExtensionRequest POST /api/extensions/{name}/run body
// N-03-01 修复：支持 dry_run 字段（REQ-F-037 A.2）
type RunExtensionRequest struct {
	Action string `json:"action"`
	DryRun bool   `json:"dry_run,omitempty"`
}

// ExtensionImportPreviewResponse 扩展导入预览响应（§2.12.5 两阶段导入）
type ExtensionImportPreviewResponse struct {
	Name        string `json:"name"`                    // 包内扩展名
	ArchiveVer  string `json:"archive_version"`         // 压缩包内版本
	LocalVer    string `json:"local_version,omitempty"` // 本地已有版本
	ExistsLocal bool   `json:"exists_local"`            // 本地是否已存在
	Service     string `json:"service,omitempty"`       // 关联的服务名（服务级扩展）
}

// handleListExtensions GET /api/extensions
func (s *Server) handleListExtensions(w http.ResponseWriter, r *http.Request) {
	if s.extProvider == nil {
		respondError(w, errors.ErrInternal, "extension provider not configured")
		return
	}

	exts := s.extProvider.ListExtensions()
	summaries := make([]ExtensionSummary, 0, len(exts))
	for _, ext := range exts {
		summary := ExtensionSummary{
			Name:         ext.Name,
			Version:      ext.Version,
			Description:  ext.Description,
			Enabled:      ext.Enabled,
			DisplayState: ext.DisplayState,
			TriggerType:  ext.TriggerType,
			Service:      ext.Service,
			RunCount:     ext.RunCount,
			SuccessCount: ext.SuccessCount,
			FailCount:    ext.FailCount,
			LastRunAt:    ext.LastRunAt,
			LastStatus:   ext.LastStatus,
		}
		// D-05-001 设计说明：当 ext.Meta 为 nil（meta.yaml 解析失败）时，
		// Triggers/Actions/Concurrency/Runtime/Entry 等可选字段保持零值并按 omitted 省略。
		// 前端可通过 ExtensionDetail 端点（GET /api/extensions/{name}）获取 ConfigErrors
		// 字段以诊断配置错误。ExtensionSummary 仅提供列表展示，不混入错误诊断信息。
		if ext.Meta != nil {
			summary.Triggers = &ext.Meta.Triggers
			summary.Actions = ext.Meta.Actions
			summary.Concurrency = ext.Meta.Concurrency
			summary.Runtime = ext.Meta.Runtime
			summary.Entry = ext.Meta.Entry
		}
		summaries = append(summaries, summary)
	}

	respondJSON(w, http.StatusOK, summaries)
}

// handleGetExtension GET /api/extensions/{name}
func (s *Server) handleGetExtension(w http.ResponseWriter, r *http.Request) {
	if s.extProvider == nil {
		respondError(w, errors.ErrInternal, "extension provider not configured")
		return
	}

	name := chi.URLParam(r, "name")
	ext, ok := s.extProvider.GetExtension(name)
	if !ok {
		respondError(w, errors.ErrExtensionNotFound, "extension not found")
		return
	}

	detail := ExtensionDetail{
		Name:         ext.Name,
		Version:      ext.Version,
		Description:  ext.Description,
		Enabled:      ext.Enabled,
		Config:       ext.Meta,
		DisplayState: ext.DisplayState,
		TriggerType:  ext.TriggerType,
		RunCount:     ext.RunCount,
		SuccessCount: ext.SuccessCount,
		FailCount:    ext.FailCount,
		ConfigPath:   s.stripWorkdirPrefix(ext.ConfigPath),
		EnvPath:      s.stripWorkdirPrefix(ext.EnvPath),
	}
	// D-05-004 修复：ext.Meta 可能为 nil，访问 Concurrency/Actions 前需检查
	if ext.Meta != nil {
		detail.Concurrency = ext.Meta.Concurrency
		detail.Actions = ext.Meta.Actions
	}

	respondJSON(w, http.StatusOK, detail)
}

// handleCreateExtension POST /api/extensions
func (s *Server) handleCreateExtension(w http.ResponseWriter, r *http.Request) {
	if s.extProvider == nil {
		respondError(w, errors.ErrInternal, "extension provider not configured")
		return
	}

	var meta config.ExtensionMeta
	if err := decodeJSONBody(r, &meta); err != nil {
		respondError(w, errors.ErrInvalidRequest, err.Error())
		return
	}

	config.SetExtensionDefaults(&meta)
	if err := config.ValidateExtension(&meta); err != nil {
		respondError(w, errors.ErrServiceConfigInvalid, err.Error())
		return
	}

	if err := s.extProvider.CreateExtension(&meta, ""); err != nil {
		respondError(w, errors.ErrInternal, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, ExtensionDetail{
		Name:         meta.Name,
		Version:      meta.Version,
		Description:  meta.Description,
		Enabled:      meta.Enabled != nil && *meta.Enabled,
		Config:       &meta,
		DisplayState: string(getDisplayStateFromMeta(&meta)),
	})
}

// handleUpdateExtension PUT /api/extensions/{name}
// F-01-001 修复：支持部分更新（如仅切换 enabled 状态）
// 当请求体中 name 为空时，视为部分更新：加载现有配置，仅合并请求中提供的字段
func (s *Server) handleUpdateExtension(w http.ResponseWriter, r *http.Request) {
	if s.extProvider == nil {
		respondError(w, errors.ErrInternal, "extension provider not configured")
		return
	}

	name := chi.URLParam(r, "name")

	var meta config.ExtensionMeta
	if err := decodeJSONBody(r, &meta); err != nil {
		respondError(w, errors.ErrInvalidRequest, err.Error())
		return
	}

	// F-01-001: 部分更新检测 — 请求体未提供 name 时，视为部分更新
	if meta.Name == "" {
		existing, ok := s.extProvider.GetExtension(name)
		if !ok {
			respondError(w, errors.ErrExtensionNotFound, "extension not found")
			return
		}
		if existing.Meta != nil {
			// 保存请求中提供的字段
			enabledFromRequest := meta.Enabled
			// 以现有配置为基础
			meta = *existing.Meta
			// 仅应用请求中提供的字段更新
			if enabledFromRequest != nil {
				meta.Enabled = enabledFromRequest
			}
		}
		// 部分更新无需重新校验（现有配置已校验过）
	} else {
		// 完整更新，走正常校验流程
		config.SetExtensionDefaults(&meta)
		if err := config.ValidateExtension(&meta); err != nil {
			respondError(w, errors.ErrServiceConfigInvalid, err.Error())
			return
		}
	}

	if err := s.extProvider.UpdateExtension(name, &meta, ""); err != nil {
		respondError(w, errors.ErrInternal, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, ExtensionDetail{
		Name:         meta.Name,
		Version:      meta.Version,
		Description:  meta.Description,
		Enabled:      meta.Enabled != nil && *meta.Enabled,
		Config:       &meta,
		DisplayState: string(getDisplayStateFromMeta(&meta)),
	})
}

// handleDeleteExtension DELETE /api/extensions/{name}
// REQ-F-014: 扩展不能运行中，否则409
func (s *Server) handleDeleteExtension(w http.ResponseWriter, r *http.Request) {
	if s.extProvider == nil {
		respondError(w, errors.ErrInternal, "extension provider not configured")
		return
	}

	name := chi.URLParam(r, "name")

	if err := s.extProvider.DeleteExtension(name, ""); err != nil {
		// N-03-DELETE-404 修复：provider 返回 ErrExtensionNotFound 时映射为 404
		respondProviderError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleSaveExtensionEnv PUT /api/extensions/{name}/env
func (s *Server) handleSaveExtensionEnv(w http.ResponseWriter, r *http.Request) {
	if s.extProvider == nil {
		respondError(w, errors.ErrInternal, "extension provider not configured")
		return
	}

	name := chi.URLParam(r, "name")

	var envFile config.EnvFile
	if err := decodeJSONBody(r, &envFile); err != nil {
		respondError(w, errors.ErrInvalidRequest, err.Error())
		return
	}
	if envFile.Env == nil {
		envFile.Env = make(map[string]config.EnvVar)
	}

	if err := s.extProvider.SaveExtensionEnv(name, &envFile, ""); err != nil {
		respondError(w, errors.ErrInternal, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, envFile)
}

// handleRunExtension POST /api/extensions/{name}/run
func (s *Server) handleRunExtension(w http.ResponseWriter, r *http.Request) {
	if s.extProvider == nil {
		respondError(w, errors.ErrInternal, "extension provider not configured")
		return
	}

	name := chi.URLParam(r, "name")

	// N-03-001 修复：原实现直接调用 RunExtension，若扩展不存在会返回 fmt.Errorf("extension %s not found")，
	// handler 统一包装为 ErrExtensionFailed（HTTP 200），导致客户端看到 200 OK 但实际是 404 场景。
	// 修复：先调用 GetExtension 校验存在性，不存在则返回 404 EXTENSION_NOT_FOUND。
	info, ok := s.extProvider.GetExtension(name)
	if !ok {
		respondError(w, errors.ErrExtensionNotFound, fmt.Sprintf("extension %s not found", name))
		return
	}

	var req RunExtensionRequest
	// N-01-001 修复：RunExtension 允许空 body（用户可使用默认 action 运行）
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !stderrors.Is(err, io.EOF) {
		respondError(w, errors.ErrInvalidRequest, "invalid request body")
		return
	}

	// dry_run 支持通过 query string 传递（规格 §2.2.16）：?dry_run=true
	if q := r.URL.Query().Get("dry_run"); q == "true" || q == "1" {
		req.DryRun = true
	}

	// N-03-002 修复：原 FindActionByID 在 actionID 不存在时静默回退到第一个 action，
	// 用户传入错误的 actionID 也会执行默认 action，违反"明确错误"原则。
	// 修复：若用户显式提供 actionID 且扩展中不存在该 action，返回 400 INVALID_REQUEST。
	if req.Action != "" && info != nil && info.Meta != nil {
		found := false
		for _, a := range info.Meta.Actions {
			if a.ID == req.Action {
				found = true
				break
			}
		}
		if !found {
			respondFieldErrors(w, errors.ErrInvalidRequest,
				fmt.Sprintf("action %q not found in extension %s", req.Action, name),
				errors.FieldError{Field: "action", Message: fmt.Sprintf("unknown action: %s", req.Action)})
			return
		}
	}

	result, err := s.extProvider.RunExtension(r.Context(), name, req.Action, "", req.DryRun)
	if err != nil {
		respondError(w, errors.ErrExtensionFailed, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, result)
}

// handleGetExtensionStatus GET /api/extensions/{name}/status
func (s *Server) handleGetExtensionStatus(w http.ResponseWriter, r *http.Request) {
	if s.extProvider == nil {
		respondError(w, errors.ErrInternal, "extension provider not configured")
		return
	}

	name := chi.URLParam(r, "name")

	status, err := s.extProvider.GetExtensionStatus(name, "")
	if err != nil {
		respondError(w, errors.ErrExtensionNotFound, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, status)
}

// handleExportExtension GET /api/extensions/{name}/export
// 导出整个扩展目录为 tar.gz
func (s *Server) handleExportExtension(w http.ResponseWriter, r *http.Request) {
	if s.extProvider == nil {
		respondError(w, errors.ErrInternal, "extension provider not configured")
		return
	}

	name := chi.URLParam(r, "name")

	info, ok := s.extProvider.GetExtension(name)
	if !ok {
		respondError(w, errors.ErrExtensionNotFound, fmt.Sprintf("extension %s not found", name))
		return
	}

	// 从 ConfigPath 推导扩展目录
	extDir := filepath.Dir(info.ConfigPath)
	if _, err := os.Stat(extDir); os.IsNotExist(err) {
		respondError(w, errors.ErrExtensionNotFound, fmt.Sprintf("extension directory %s not found", extDir))
		return
	}

	// 设置响应头 — tar.gz
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.tar.gz", name))

	// 打包整个扩展目录
	if err := archive.PackDir(extDir, w); err != nil {
		slog.Error("导出扩展失败", "name", name, "error", err)
		return
	}
}

// handleImportExtension POST /api/extensions/import
// §2.12.5 两阶段导入-预览：接收 .tar.gz，提取 meta.yaml，返回版本对比信息
func (s *Server) handleImportExtension(w http.ResponseWriter, r *http.Request) {
	if s.extProvider == nil {
		respondError(w, errors.ErrInternal, "extension provider not configured")
		return
	}

	maxSize := int64(MaxUploadSize)
	r.Body = http.MaxBytesReader(w, r.Body, maxSize)
	if err := r.ParseMultipartForm(maxSize); err != nil {
		respondError(w, errors.ErrFileTooLarge, fmt.Sprintf("upload too large or invalid: %v", err))
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		respondError(w, errors.ErrInvalidRequest, "missing file in request")
		return
	}
	defer file.Close()

	// 从压缩包提取 meta.yaml（basename 匹配，可能含子目录下的 meta.yaml）
	fileContents, err := archive.FileContentFromArchive(file, []string{"meta.yaml"})
	if err != nil {
		respondError(w, errors.ErrInvalidRequest, fmt.Sprintf("invalid archive: %v", err))
		return
	}

	// 取顶层 meta.yaml（路径最短者，避免子目录 lib/meta.yaml 干扰）
	var metaData []byte
	minDepth := 9999
	for path, data := range fileContents {
		depth := strings.Count(filepath.Clean(path), string(os.PathSeparator))
		if depth < minDepth {
			minDepth = depth
			metaData = data
		}
	}
	if metaData == nil {
		respondError(w, errors.ErrInvalidRequest, "meta.yaml not found in archive")
		return
	}

	var meta config.ExtensionMeta
	if err := config.SafeUnmarshal(metaData, &meta, config.DefaultSafeYAMLOptions); err != nil {
		respondError(w, errors.ErrInvalidRequest, fmt.Sprintf("invalid meta.yaml: %v", err))
		return
	}
	config.SetExtensionDefaults(&meta)
	if err := config.ValidateExtension(&meta); err != nil {
		respondError(w, errors.ErrServiceConfigInvalid, fmt.Sprintf("meta.yaml validation failed: %v", err))
		return
	}

	resp := ExtensionImportPreviewResponse{
		Name:       meta.Name,
		ArchiveVer: meta.Version,
	}
	if existing, ok := s.extProvider.GetExtension(meta.Name); ok {
		resp.ExistsLocal = true
		resp.LocalVer = existing.Version
	}

	respondJSON(w, http.StatusOK, resp)
}

// handleImportExtensionConfirm POST /api/extensions/import/confirm
// §2.12.5 两阶段导入-确认：接收 .tar.gz 二次上传，备份现有目录，原子解压覆盖，触发热重载
func (s *Server) handleImportExtensionConfirm(w http.ResponseWriter, r *http.Request) {
	if s.extProvider == nil {
		respondError(w, errors.ErrInternal, "extension provider not configured")
		return
	}

	maxSize := int64(MaxUploadSize)
	r.Body = http.MaxBytesReader(w, r.Body, maxSize)
	if err := r.ParseMultipartForm(maxSize); err != nil {
		respondError(w, errors.ErrFileTooLarge, fmt.Sprintf("upload too large or invalid: %v", err))
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		respondError(w, errors.ErrInvalidRequest, "missing file in request")
		return
	}
	defer file.Close()

	name := r.FormValue("name")
	if name == "" {
		respondFieldErrors(w, errors.ErrInvalidRequest, "name is required",
			errors.FieldError{Field: "name", Message: "extension name is required"})
		return
	}
	service := r.FormValue("service")

	// 安全校验扩展名（防路径穿越）— 扩展名格式与服务名一致 ^[a-z][a-z0-9-]*$
	safeName := SanitizeFilename(name)
	if safeName != name || !isValidServiceName(name) {
		respondFieldErrors(w, errors.ErrInvalidRequest, "invalid extension name",
			errors.FieldError{Field: "name", Message: "name must match ^[a-z][a-z0-9-]*$"})
		return
	}

	// 确定目标目录
	var baseDir string
	if s.pathValidator != nil {
		baseDir = s.pathValidator.baseDir
	} else {
		baseDir = "/etc/supd/"
	}

	var targetDir string
	if service != "" {
		if !isValidServiceName(service) {
			respondError(w, errors.ErrInvalidRequest, "invalid service name")
			return
		}
		targetDir = filepath.Join(baseDir, "services", service, "extensions", name)
	} else {
		targetDir = filepath.Join(baseDir, "extensions", name)
	}

	// 防御性前缀检查（name/service 已通过 isValidServiceName 校验，此为纵深防御）
	if !strings.HasPrefix(filepath.Clean(targetDir), filepath.Clean(baseDir)) {
		respondError(w, errors.ErrFileAccessDenied, "invalid target directory")
		return
	}

	// D-06-03: 按目标目录加锁，防止并发导入同名扩展导致目录损坏/数据丢失
	// 锁覆盖备份-创建-解包-回滚全过程；不同扩展互不阻塞
	unlockImport := acquireImportLock(targetDir)
	defer unlockImport()

	// 备份现有目录（若存在）— os.Rename 是同文件系统上的原子操作
	var backupDir string
	if _, err := os.Stat(targetDir); err == nil {
		timestamp := time.Now().Format("20060102150405")
		backupDir = fmt.Sprintf("%s.bak.%s", targetDir, timestamp)
		if err := os.Rename(targetDir, backupDir); err != nil {
			slog.Error("backup extension dir failed", "dir", targetDir, "error", err)
			respondError(w, errors.ErrInternal, "failed to backup existing extension directory")
			return
		}
		slog.Info("extension directory backed up", "original", targetDir, "backup", backupDir)
	}

	// 创建新目录并解压
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		// 目录创建失败，尝试回滚
		if backupDir != "" {
			if rbErr := os.Rename(backupDir, targetDir); rbErr != nil {
				slog.Error("rollback failed after mkdir failure", "error", rbErr)
			}
		}
		respondError(w, errors.ErrInternal, "failed to create extension directory")
		return
	}

	// 解压 — multipart.File 实现 io.Reader，可直接传入
	if err := archive.UnpackDir(file, targetDir); err != nil {
		slog.Error("unpack extension archive failed", "dir", targetDir, "error", err)
		// 解压失败，执行完整回滚
		if removeErr := os.RemoveAll(targetDir); removeErr != nil {
			slog.Error("cleanup partial extract failed", "error", removeErr)
		}
		if backupDir != "" {
			if rbErr := os.Rename(backupDir, targetDir); rbErr != nil {
				slog.Error("rollback backup failed", "backup", backupDir, "target", targetDir, "error", rbErr)
			}
		}
		// H-03-002 修复：zip slip（路径穿越）返回 403 而非 500
		if stderrors.Is(err, archive.ErrPathTraversal) {
			respondError(w, errors.ErrFileAccessDenied, fmt.Sprintf("archive contains path traversal: %v", err))
		} else {
			respondError(w, errors.ErrInternal, fmt.Sprintf("failed to unpack extension archive: %v", err))
		}
		return
	}

	slog.Info("extension imported successfully", "name", name, "service", service, "backup", backupDir)

	// R-001 修复：导入成功后显式触发热重载，避免依赖 fsnotify 异步检测的延迟和漏事件风险
	// 失败时不影响导入本身（导入已成功），仅记录 warn 日志
	resp := map[string]any{
		"name":    name,
		"message": "extension imported, hot-reload triggered",
	}
	newDiscovery, errCount, errDetails := s.triggerReload()
	if newDiscovery != nil {
		resp["services"] = len(newDiscovery.Services)
		resp["global_extensions"] = len(newDiscovery.GlobalExts)
		resp["scan_errors"] = errCount
		if errCount > 0 {
			resp["error_details"] = errDetails
			slog.Warn("extension import triggered reload with scan errors", "name", name, "errors", errCount)
		}
	} else {
		slog.Warn("extension import reload skipped: watch provider not configured", "name", name)
	}

	respondJSON(w, http.StatusCreated, resp)
}

// --- 服务级扩展端点 ---

// handleListServiceExtensions GET /api/services/{name}/extensions
func (s *Server) handleListServiceExtensions(w http.ResponseWriter, r *http.Request) {
	if s.extProvider == nil {
		respondError(w, errors.ErrInternal, "extension provider not configured")
		return
	}

	svcName := chi.URLParam(r, "name")
	exts := s.extProvider.ListExtensions()
	summaries := make([]ExtensionSummary, 0)
	for _, ext := range exts {
		if ext.Service == svcName {
			summary := ExtensionSummary{
				Name:         ext.Name,
				Version:      ext.Version,
				Description:  ext.Description,
				Enabled:      ext.Enabled,
				DisplayState: ext.DisplayState,
				TriggerType:  ext.TriggerType,
				Service:      ext.Service,
				RunCount:     ext.RunCount,
				SuccessCount: ext.SuccessCount,
				FailCount:    ext.FailCount,
				LastRunAt:    ext.LastRunAt,
				LastStatus:   ext.LastStatus,
			}
			if ext.Meta != nil {
				summary.Triggers = &ext.Meta.Triggers
				summary.Actions = ext.Meta.Actions
				summary.Concurrency = ext.Meta.Concurrency
			}
			summaries = append(summaries, summary)
		}
	}

	respondJSON(w, http.StatusOK, summaries)
}

// handleGetServiceExtension GET /api/services/{name}/extensions/{ext}
func (s *Server) handleGetServiceExtension(w http.ResponseWriter, r *http.Request) {
	if s.extProvider == nil {
		respondError(w, errors.ErrInternal, "extension provider not configured")
		return
	}

	svcName := chi.URLParam(r, "name")
	extName := chi.URLParam(r, "ext")

	ext, ok := s.extProvider.GetExtension(extName)
	if !ok || ext.Service != svcName {
		respondError(w, errors.ErrExtensionNotFound, "extension not found")
		return
	}

	detail := ExtensionDetail{
		Name:         ext.Name,
		Version:      ext.Version,
		Description:  ext.Description,
		Enabled:      ext.Enabled,
		Config:       ext.Meta,
		DisplayState: ext.DisplayState,
		TriggerType:  ext.TriggerType,
		RunCount:     ext.RunCount,
		SuccessCount: ext.SuccessCount,
		FailCount:    ext.FailCount,
		ConfigPath:   s.stripWorkdirPrefix(ext.ConfigPath),
		EnvPath:      s.stripWorkdirPrefix(ext.EnvPath),
	}
	// D-05-004 修复：ext.Meta 可能为 nil，访问 Concurrency/Actions 前需检查
	if ext.Meta != nil {
		detail.Concurrency = ext.Meta.Concurrency
		detail.Actions = ext.Meta.Actions
	}

	respondJSON(w, http.StatusOK, detail)
}

// handleCreateServiceExtension POST /api/services/{name}/extensions
func (s *Server) handleCreateServiceExtension(w http.ResponseWriter, r *http.Request) {
	if s.extProvider == nil {
		respondError(w, errors.ErrInternal, "extension provider not configured")
		return
	}

	svcName := chi.URLParam(r, "name")

	var meta config.ExtensionMeta
	if err := decodeJSONBody(r, &meta); err != nil {
		respondError(w, errors.ErrInvalidRequest, err.Error())
		return
	}

	config.SetExtensionDefaults(&meta)
	if err := config.ValidateExtension(&meta); err != nil {
		respondError(w, errors.ErrServiceConfigInvalid, err.Error())
		return
	}

	if err := s.extProvider.CreateExtension(&meta, svcName); err != nil {
		respondError(w, errors.ErrInternal, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, ExtensionDetail{
		Name:         meta.Name,
		Version:      meta.Version,
		Description:  meta.Description,
		Enabled:      meta.Enabled != nil && *meta.Enabled,
		Config:       &meta,
		DisplayState: string(getDisplayStateFromMeta(&meta)),
	})
}

// handleUpdateServiceExtension PUT /api/services/{name}/extensions/{ext}
func (s *Server) handleUpdateServiceExtension(w http.ResponseWriter, r *http.Request) {
	if s.extProvider == nil {
		respondError(w, errors.ErrInternal, "extension provider not configured")
		return
	}

	svcName := chi.URLParam(r, "name")
	extName := chi.URLParam(r, "ext")

	var meta config.ExtensionMeta
	if err := decodeJSONBody(r, &meta); err != nil {
		respondError(w, errors.ErrInvalidRequest, err.Error())
		return
	}

	config.SetExtensionDefaults(&meta)
	if err := config.ValidateExtension(&meta); err != nil {
		respondError(w, errors.ErrServiceConfigInvalid, err.Error())
		return
	}

	if err := s.extProvider.UpdateExtension(extName, &meta, svcName); err != nil {
		respondError(w, errors.ErrInternal, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, ExtensionDetail{
		Name:         meta.Name,
		Version:      meta.Version,
		Description:  meta.Description,
		Enabled:      meta.Enabled != nil && *meta.Enabled,
		Config:       &meta,
		DisplayState: string(getDisplayStateFromMeta(&meta)),
	})
}

// handleDeleteServiceExtension DELETE /api/services/{name}/extensions/{ext}
func (s *Server) handleDeleteServiceExtension(w http.ResponseWriter, r *http.Request) {
	if s.extProvider == nil {
		respondError(w, errors.ErrInternal, "extension provider not configured")
		return
	}

	svcName := chi.URLParam(r, "name")
	extName := chi.URLParam(r, "ext")

	if err := s.extProvider.DeleteExtension(extName, svcName); err != nil {
		// N-03-DELETE-404 修复：provider 返回 ErrExtensionNotFound 时映射为 404
		respondProviderError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleSaveServiceExtensionEnv PUT /api/services/{name}/extensions/{ext}/env
func (s *Server) handleSaveServiceExtensionEnv(w http.ResponseWriter, r *http.Request) {
	if s.extProvider == nil {
		respondError(w, errors.ErrInternal, "extension provider not configured")
		return
	}

	svcName := chi.URLParam(r, "name")
	extName := chi.URLParam(r, "ext")

	var envFile config.EnvFile
	if err := decodeJSONBody(r, &envFile); err != nil {
		respondError(w, errors.ErrInvalidRequest, err.Error())
		return
	}
	if envFile.Env == nil {
		envFile.Env = make(map[string]config.EnvVar)
	}

	if err := s.extProvider.SaveExtensionEnv(extName, &envFile, svcName); err != nil {
		respondError(w, errors.ErrInternal, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, envFile)
}

// handleRunServiceExtension POST /api/services/{name}/extensions/{ext}/run
func (s *Server) handleRunServiceExtension(w http.ResponseWriter, r *http.Request) {
	if s.extProvider == nil {
		respondError(w, errors.ErrInternal, "extension provider not configured")
		return
	}

	svcName := chi.URLParam(r, "name")
	extName := chi.URLParam(r, "ext")

	var req RunExtensionRequest
	// N-01-001 修复：RunExtension 允许空 body（用户可使用默认 action 运行）
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !stderrors.Is(err, io.EOF) {
		respondError(w, errors.ErrInvalidRequest, "invalid request body")
		return
	}

	// dry_run 支持通过 query string 传递（规格 §2.2.16）：?dry_run=true
	if q := r.URL.Query().Get("dry_run"); q == "true" || q == "1" {
		req.DryRun = true
	}

	result, err := s.extProvider.RunExtension(r.Context(), extName, req.Action, svcName, req.DryRun)
	if err != nil {
		respondError(w, errors.ErrExtensionFailed, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, result)
}

// getDisplayStateFromMeta 从 ExtensionMeta 获取展示状态
func getDisplayStateFromMeta(meta *config.ExtensionMeta) string {
	if meta.Enabled != nil && !*meta.Enabled {
		return "disabled"
	}
	if len(meta.Actions) == 0 {
		return "automated"
	}
	return "active"
}
