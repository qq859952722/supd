package api

import (
	"encoding/json"
	"net/http"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/errors"
)

// REQ-D-006: 设置端点

// settingsMaxBodyBytes 设置类端点请求体大小上限（1MB）。
// F-01-006: 设置/env/runtimes 数据通常很小，1MB 足以覆盖合理场景；
// 超出则视为异常请求并拒绝，避免异常大请求消耗资源。
const settingsMaxBodyBytes = 1 << 20 // 1MB

// SettingsResponse GET /api/settings 的响应
// 包含 Settings 结构体字段 + Config 级别的字段（env_files/extension_dirs/defaults）
//
// F-06-002 修复：不返回 auth_token 明文，使用 auth_token_configured 标记是否已配置
//
// F-06-001: Defaults 字段 nil 语义
//   - 当前实现：handleGetSettings 第 76-77 行始终将 Defaults 设为非 nil（GetDefaults 返回值零值）
//     因此 `omitempty` 在响应序列化时实际不会触发；响应中 defaults 字段始终存在
//   - 类型仍为指针是为与 SettingsUpdateRequest.Defaults 保持对称，便于前后端共享同一类型语义
type SettingsResponse struct {
	HTTPListen                     string   `json:"http_listen"`
	AuthMode                       string   `json:"auth_mode"`
	AuthTokenConfigured            bool     `json:"auth_token_configured"`
	LocalNetworks                  []string `json:"local_networks"`
	LogMaxSizeMB                   int      `json:"log_max_size_mb"`
	LogMaxFiles                    int      `json:"log_max_files"`
	LogLevel                       string   `json:"log_level"`
	ShutdownGraceSeconds           int      `json:"shutdown_grace_seconds"`
	ExtensionDefaultTimeoutSeconds int      `json:"extension_default_timeout_seconds"`
	ExtensionHardLimitSeconds      int      `json:"extension_hard_limit_seconds"`
	RunHistoryRetentionSeconds     int      `json:"run_history_retention_seconds"`
	FileHistoryVersions            int      `json:"file_history_versions"`
	MaxUploadSizeMB                int      `json:"max_upload_size_mb"`
	EnvFiles                       []string               `json:"env_files,omitempty"`
	ExtensionDirs                  []string               `json:"extension_dirs,omitempty"`
	Defaults                       *config.DefaultRestart `json:"defaults,omitempty"`
}

// SettingsUpdateRequest PUT /api/settings 的请求体
// 支持更新 Settings 字段 + Config 级别的字段
//
// F-06-001: 指针类型字段的 nil 语义（部分更新协议）
//   - Defaults 为 *config.DefaultRestart 指针类型，配合 `omitempty` 实现"部分更新"语义：
//     * JSON 请求中省略 defaults 字段（反序列化为 nil） → 不更新 defaults，保留原值
//     * JSON 请求中包含 defaults 字段（反序列化为非 nil） → 用请求值覆盖原值（含零值字段）
//   - EnvFiles/ExtensionDirs 为切片类型，nil 表示"不更新"，空切片 [] 表示"清空"
//   - Settings 嵌套结构体为值类型，始终整体更新；前端不希望修改的字段需先 GET 再原样回传
//   - 前端实现：表单中所有字段都会随提交一同发送，相当于全量替换 defaults（详见 web/src/pages/Settings.tsx）
type SettingsUpdateRequest struct {
	config.Settings
	EnvFiles      []string               `json:"env_files,omitempty"`
	ExtensionDirs []string               `json:"extension_dirs,omitempty"`
	Defaults      *config.DefaultRestart `json:"defaults,omitempty"`
}

// handleGetSettings GET /api/settings
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	if s.settingsProvider == nil {
		respondError(w, errors.ErrInternal, "settings provider not configured")
		return
	}

	settings := s.settingsProvider.GetSettings()
	resp := SettingsResponse{
		HTTPListen:                     settings.HTTPListen,
		AuthMode:                       settings.AuthMode,
		AuthTokenConfigured:            settings.AuthToken != "",
		LocalNetworks:                  settings.LocalNetworks,
		LogMaxSizeMB:                   settings.LogMaxSizeMB,
		LogMaxFiles:                    settings.LogMaxFiles,
		LogLevel:                       settings.LogLevel,
		ShutdownGraceSeconds:           settings.ShutdownGraceSeconds,
		ExtensionDefaultTimeoutSeconds: settings.ExtensionDefaultTimeoutSeconds,
		ExtensionHardLimitSeconds:      settings.ExtensionHardLimitSeconds,
		RunHistoryRetentionSeconds:     settings.RunHistoryRetentionSeconds,
		FileHistoryVersions:            settings.FileHistoryVersions,
		MaxUploadSizeMB:                settings.MaxUploadSizeMB,
		EnvFiles:      s.settingsProvider.GetEnvFiles(),
		ExtensionDirs: s.settingsProvider.GetExtensionDirs(),
		Defaults:      nil,
	}
	// defaults 始终返回（即使为零值）
	d := s.settingsProvider.GetDefaults()
	resp.Defaults = &d

	respondJSON(w, http.StatusOK, resp)
}

// handleUpdateSettings PUT /api/settings
// REQ-D-006: 更新全局设置，写入 config.yaml
// 支持 Settings 字段 + env_files + extension_dirs + defaults
func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	if s.settingsProvider == nil {
		respondError(w, errors.ErrInternal, "settings provider not configured")
		return
	}

	// F-01-006 修复：限制请求体大小，避免异常大请求消耗资源
	r.Body = http.MaxBytesReader(w, r.Body, settingsMaxBodyBytes)

	var req SettingsUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, errors.ErrInvalidRequest, "invalid request body: "+err.Error())
		return
	}

	// 1. 更新 Settings 字段
	if err := s.settingsProvider.UpdateSettings(&req.Settings); err != nil {
		respondError(w, errors.ErrInternal, err.Error())
		return
	}


	// 2. 更新 env_files（如果请求中包含）
	if req.EnvFiles != nil {
		if err := s.settingsProvider.UpdateEnvFiles(req.EnvFiles); err != nil {
			respondError(w, errors.ErrInternal, err.Error())
			return
		}
	}

	// 3. 更新 extension_dirs（如果请求中包含）
	if req.ExtensionDirs != nil {
		if err := s.settingsProvider.UpdateExtensionDirs(req.ExtensionDirs); err != nil {
			respondError(w, errors.ErrInternal, err.Error())
			return
		}
	}

	// 4. 更新 defaults（如果请求中包含）
	if req.Defaults != nil {
		if err := s.settingsProvider.UpdateDefaults(*req.Defaults); err != nil {
			respondError(w, errors.ErrInternal, err.Error())
			return
		}
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleGetEnv GET /api/settings/env
func (s *Server) handleGetEnv(w http.ResponseWriter, r *http.Request) {
	if s.settingsProvider == nil {
		respondError(w, errors.ErrInternal, "settings provider not configured")
		return
	}

	env, err := s.settingsProvider.GetEnv()
	if err != nil {
		respondError(w, errors.ErrInternal, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, env)
}

// handleUpdateEnv PUT /api/settings/env
// REQ-D-008: 更新全局环境变量，写入 env/00-base.yaml
func (s *Server) handleUpdateEnv(w http.ResponseWriter, r *http.Request) {
	if s.settingsProvider == nil {
		respondError(w, errors.ErrInternal, "settings provider not configured")
		return
	}

	// F-01-006 修复：限制请求体大小
	r.Body = http.MaxBytesReader(w, r.Body, settingsMaxBodyBytes)

	// 直接解码到 config.EnvFile 结构体
	var envFile config.EnvFile
	if err := json.NewDecoder(r.Body).Decode(&envFile); err != nil {
		respondError(w, errors.ErrInvalidRequest, "invalid request body: "+err.Error())
		return
	}

	if err := s.settingsProvider.UpdateEnv(&envFile); err != nil {
		respondError(w, errors.ErrInternal, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleGetRuntimesConfig GET /api/settings/runtimes
func (s *Server) handleGetRuntimesConfig(w http.ResponseWriter, r *http.Request) {
	if s.settingsProvider == nil {
		respondError(w, errors.ErrInternal, "settings provider not configured")
		return
	}

	runtimes := s.settingsProvider.GetRuntimesConfig()
	respondJSON(w, http.StatusOK, runtimes)
}

// handleUpdateRuntimesConfig PUT /api/settings/runtimes
func (s *Server) handleUpdateRuntimesConfig(w http.ResponseWriter, r *http.Request) {
	if s.settingsProvider == nil {
		respondError(w, errors.ErrInternal, "settings provider not configured")
		return
	}

	// F-01-006 修复：限制请求体大小
	r.Body = http.MaxBytesReader(w, r.Body, settingsMaxBodyBytes)

	var runtimes map[string]string
	if err := json.NewDecoder(r.Body).Decode(&runtimes); err != nil {
		respondError(w, errors.ErrInvalidRequest, "invalid request body")
		return
	}

	if err := s.settingsProvider.UpdateRuntimesConfig(runtimes); err != nil {
		respondError(w, errors.ErrInternal, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
