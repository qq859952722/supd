package api

import (
	"net/http"
	"time"

	"github.com/supdorg/supd/internal/errors"
	"github.com/supdorg/supd/internal/extension"
)

// REQ-F-001~003: 系统状态端点

// SystemStatus 系统状态响应
type SystemStatus struct {
	StartTime   string        `json:"start_time"`
	Version     string        `json:"version"`
	Uptime      int64         `json:"uptime_seconds"`
	HTTPListen  string        `json:"http_listen"`
	AuthMode    string        `json:"auth_mode"`
	WorkDir     string        `json:"work_dir"`
	CPU         float64       `json:"cpu_percent"`
	MemoryMB    float64       `json:"memory_mb"`
	DiskTotalMB float64       `json:"disk_total_mb"`
	DiskUsedMB  float64       `json:"disk_used_mb"`
	Watcher     WatcherStatus `json:"watcher"`
}

type WatcherStatus struct {
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason,omitempty"`
}

type watcherStateProvider interface {
	Enabled() bool
	Reason() string
}

func watcherStatus(provider WatchProvider) WatcherStatus {
	state, ok := provider.(watcherStateProvider)
	if !ok {
		return WatcherStatus{Enabled: false, Reason: "watcher state unavailable"}
	}
	return WatcherStatus{Enabled: state.Enabled(), Reason: state.Reason()}
}

// DiagnosticInfo 诊断信息汇总
// 导出完整的系统诊断数据，帮助排查问题
type DiagnosticInfo struct {
	Timestamp    string                `json:"timestamp"`
	System       *SystemStatus         `json:"system"`
	Services     []diagnosticService   `json:"services"`
	Extensions   []diagnosticExtension `json:"extensions"`
	RecentRuns   []diagnosticRun       `json:"recent_runs"`
	RecentEvents []diagnosticEvent     `json:"recent_events"`
	CronJobs     []diagnosticCron      `json:"cron_jobs"`
	Settings     *diagnosticSettings   `json:"settings"`
	Runtimes     []diagnosticRuntime   `json:"runtimes"`
}

type diagnosticService struct {
	Name         string `json:"name"`
	State        string `json:"state"`
	PID          int    `json:"pid,omitempty"`
	Uptime       int64  `json:"uptime_seconds,omitempty"`
	RestartCount int    `json:"restart_count"`
	Autostart    bool   `json:"autostart"`
	Readiness    string `json:"readiness,omitempty"`
}

type diagnosticRun struct {
	RunID         string              `json:"run_id"`
	ExtensionName string              `json:"extension_name"`
	ActionID      string              `json:"action_id"`
	State         extension.TaskState `json:"state"`
	ExitCode      int                 `json:"exit_code"`
	Progress      int                 `json:"progress"`
	StartedAt     time.Time           `json:"started_at"`
	FinishedAt    time.Time           `json:"finished_at"`
	TriggerType   string              `json:"trigger_type"`
	ServiceName   string              `json:"service_name"`
}

type diagnosticExtension struct {
	Name         string `json:"name"`
	Version      string `json:"version"`
	TriggerType  string `json:"trigger_type"`
	Enabled      bool   `json:"enabled"`
	RunCount     int    `json:"run_count"`
	SuccessCount int    `json:"success_count"`
	FailCount    int    `json:"fail_count"`
}

type diagnosticEvent struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Message   string `json:"message,omitempty"`
}

type diagnosticCron struct {
	ExtensionName string `json:"extension_name"`
	ActionID      string `json:"action_id"`
	Schedule      string `json:"schedule"`
	NextRun       string `json:"next_run,omitempty"`
}

type diagnosticSettings struct {
	HTTPListen                     string   `json:"http_listen"`
	AuthMode                       string   `json:"auth_mode"`
	AuthTokenConfigured            bool     `json:"auth_token_configured"`
	LogLevel                       string   `json:"log_level"`
	LogMaxSizeMB                   int      `json:"log_max_size_mb"`
	ShutdownGraceSeconds           int      `json:"shutdown_grace_seconds"`
	ExtensionDefaultTimeoutSeconds int      `json:"extension_default_timeout_seconds"`
	ExtensionHardLimitSeconds      int      `json:"extension_hard_limit_seconds"`
	LocalNetworks                  []string `json:"local_networks"`
	EnvFiles                       []string `json:"env_files"`
	ExtensionDirs                  []string `json:"extension_dirs"`
}

type diagnosticRuntime struct {
	Alias     string `json:"alias"`
	Path      string `json:"path"`
	Available bool   `json:"available"`
}

// handleSystemStatus GET /api/system/status
func (s *Server) handleSystemStatus(w http.ResponseWriter, r *http.Request) {
	if s.systemProvider == nil {
		respondError(w, errors.ErrInternal, "system provider not configured")
		return
	}

	status := s.systemProvider.GetSystemStatus()

	resp := SystemStatus{
		StartTime:   status.StartTime.Format(time.RFC3339),
		Version:     status.Version,
		Uptime:      status.Uptime,
		HTTPListen:  status.HTTPListen,
		AuthMode:    status.AuthMode,
		WorkDir:     status.WorkDir,
		CPU:         status.CPU,
		MemoryMB:    status.MemoryMB,
		DiskTotalMB: status.DiskTotalMB,
		DiskUsedMB:  status.DiskUsedMB,
		// 填充 watcher 运行期状态（与 diagnostic 保持一致）。
		Watcher: watcherStatus(s.watchProvider),
	}

	respondJSON(w, http.StatusOK, resp)
}

// handleDiagnostic GET /api/system/diagnostic
// 导出完整诊断信息，帮助排查问题
func (s *Server) handleDiagnostic(w http.ResponseWriter, r *http.Request) {
	info := DiagnosticInfo{
		Timestamp: time.Now().Format(time.RFC3339),
	}

	// 1. 系统状态
	if s.systemProvider != nil {
		status := s.systemProvider.GetSystemStatus()
		info.System = &SystemStatus{
			StartTime:   status.StartTime.Format(time.RFC3339),
			Version:     status.Version,
			Uptime:      status.Uptime,
			HTTPListen:  status.HTTPListen,
			AuthMode:    status.AuthMode,
			WorkDir:     status.WorkDir,
			CPU:         status.CPU,
			MemoryMB:    status.MemoryMB,
			DiskTotalMB: status.DiskTotalMB,
			DiskUsedMB:  status.DiskUsedMB,
			// 填充 watcher 运行期状态（此前遗漏导致恒为 {enabled:false}）。
			Watcher: watcherStatus(s.watchProvider),
		}
	}

	// 2. 服务状态
	if s.stateProvider != nil {
		for name, svc := range s.stateProvider.ListServiceStates() {
			ds := diagnosticService{
				Name:         name,
				State:        string(svc.State),
				PID:          svc.PID,
				Uptime:       svc.Uptime,
				RestartCount: svc.RestartCount,
			}
			if svc.Config != nil {
				if svc.Config.Autostart != nil {
					ds.Autostart = *svc.Config.Autostart
				}
				if svc.Config.Readiness != nil {
					ds.Readiness = svc.Config.Readiness.Type
				}
			}
			info.Services = append(info.Services, ds)
		}
	}

	// 3. 扩展状态
	if s.extProvider != nil {
		for _, ext := range s.extProvider.ListExtensions() {
			info.Extensions = append(info.Extensions, diagnosticExtension{
				Name:         ext.Name,
				Version:      ext.Version,
				TriggerType:  ext.TriggerType,
				Enabled:      ext.Enabled,
				RunCount:     ext.RunCount,
				SuccessCount: ext.SuccessCount,
				FailCount:    ext.FailCount,
			})
		}
	}

	// 4. 最近运行记录（仅返回结构化元数据，不返回可能含敏感信息的结果消息）
	if s.taskProvider != nil {
		for _, run := range s.taskProvider.ListRuns(extension.RunFilter{Limit: 30}) {
			if run == nil {
				continue
			}
			info.RecentRuns = append(info.RecentRuns, diagnosticRun{
				RunID:         run.RunID,
				ExtensionName: run.ExtensionName,
				ActionID:      run.ActionID,
				State:         run.State,
				ExitCode:      run.ExitCode,
				Progress:      run.Progress,
				StartedAt:     run.StartedAt,
				FinishedAt:    run.FinishedAt,
				TriggerType:   run.TriggerType,
				ServiceName:   run.ServiceName,
			})
		}
	}

	// 5. 最近事件
	if s.eventRing != nil {
		events, _ := s.eventRing.Since(0, 50)
		for _, e := range events {
			info.RecentEvents = append(info.RecentEvents, diagnosticEvent{
				Type:      e.Type,
				Timestamp: e.Time.Format(time.RFC3339),
			})
		}
	}

	// 6. Cron 任务
	if s.cronProvider != nil {
		for _, entry := range s.cronProvider.ListCronEntries() {
			info.CronJobs = append(info.CronJobs, diagnosticCron{
				ExtensionName: entry.ExtensionName,
				ActionID:      entry.ActionID,
				Schedule:      entry.Schedule,
				NextRun:       entry.NextRun,
			})
		}
	}

	// 7. 设置（不导出敏感信息）
	if s.settingsProvider != nil {
		settings := s.settingsProvider.GetSettings()
		if settings != nil {
			info.Settings = &diagnosticSettings{
				HTTPListen:                     settings.HTTPListen,
				AuthMode:                       settings.AuthMode,
				AuthTokenConfigured:            settings.AuthToken != "",
				LogLevel:                       settings.LogLevel,
				LogMaxSizeMB:                   settings.LogMaxSizeMB,
				ShutdownGraceSeconds:           settings.ShutdownGraceSeconds,
				ExtensionDefaultTimeoutSeconds: settings.ExtensionDefaultTimeoutSeconds,
				ExtensionHardLimitSeconds:      settings.ExtensionHardLimitSeconds,
				LocalNetworks:                  settings.LocalNetworks,
			}
		}
		info.Settings.EnvFiles = s.settingsProvider.GetEnvFiles()
		info.Settings.ExtensionDirs = s.settingsProvider.GetExtensionDirs()
	}

	// 8. 运行时
	if s.runtimeProvider != nil {
		for _, rt := range s.runtimeProvider.ListRuntimes() {
			info.Runtimes = append(info.Runtimes, diagnosticRuntime{
				Alias:     rt.Alias,
				Path:      rt.Path,
				Available: rt.Available,
			})
		}
	}

	respondJSON(w, http.StatusOK, info)
}
