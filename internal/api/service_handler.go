package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"go.yaml.in/yaml/v4"

	"github.com/supdorg/supd/internal/errors"
)

// MaxConfigSize 配置文件请求体大小上限（1MB）
// N-01-001: 限制服务配置更新请求体，防止超大请求 DoS（配置文件通常远小于此值）
const MaxConfigSize = 1 << 20 // 1MB

// stripWorkdirPrefix 将路径转为相对于 workdir 的相对路径
// config_path 在 discovery 中基于 CWD 构造（如 test_workdir/services/...），
// 但 /api/files 白名单期望相对于 workdir 的路径（如 services/...）
func (s *Server) stripWorkdirPrefix(p string) string {
	if p == "" {
		return p
	}
	base := s.pathValidator.baseDir
	// 尝试去掉 baseDir 前缀
	rel, err := filepath.Rel(base, p)
	if err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	// baseDir 可能不带尾斜杠，尝试直接 TrimPrefix
	trimmed := strings.TrimPrefix(p, base)
	if trimmed != p {
		return trimmed
	}
	trimmed = strings.TrimPrefix(p, strings.TrimSuffix(base, "/"))
	if trimmed != p {
		return strings.TrimPrefix(trimmed, "/")
	}
	return p
}

// REQ-I-006: 服务管理 API 数据结构

// ServiceListResponse GET /api/services 响应
type ServiceListResponse struct {
	Services []ServiceSummary `json:"services"`
}

// ServiceSummary 服务列表项
// REQ-I-006: 返回name/status/pid/uptime/restart_count/icon/tags
type ServiceSummary struct {
	Name         string     `json:"name"`
	Status       string     `json:"status"`
	PID          int        `json:"pid,omitempty"`
	Uptime       int64      `json:"uptime,omitempty"` // 秒
	RestartCount int        `json:"restart_count"`
	Icon         string     `json:"icon,omitempty"`
	Tags         []string   `json:"tags,omitempty"`
	CPUPercent   float64    `json:"cpu_percent"`
	MemoryMB     float64    `json:"memory_mb"`
	Ports        []PortInfo `json:"ports,omitempty"` // 服务监听端口（含 HTTP 判定）
}

// ServiceDetail GET /api/services/{name} 响应
// REQ-I-006: 含完整service.yaml解析结果+运行时状态+待生效变更标记
type ServiceDetail struct {
	Name           string                `json:"name"`
	Status         string                `json:"status"`
	PID            int                   `json:"pid,omitempty"`
	Uptime         int64                 `json:"uptime,omitempty"` // 秒
	RestartCount   int                   `json:"restart_count"`
	Config         *ServiceConfigDetail  `json:"config"`
	ConfigPath     string                `json:"config_path,omitempty"`
	PendingChanges []string              `json:"pending_changes,omitempty"`
	CPUPercent     float64               `json:"cpu_percent"`
	MemoryMB       float64               `json:"memory_mb"`
	Ports          []PortInfo            `json:"ports,omitempty"` // 服务监听端口
}

// ServiceConfigDetail 服务配置详情（用于API响应）
type ServiceConfigDetail struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Icon        string   `json:"icon"`
	Autostart   *bool    `json:"autostart"`
	Command     []string `json:"command"`
	Runtime     string   `json:"runtime"`
	User        string   `json:"user"`
	Group       string   `json:"group"`
	Workdir     string   `json:"workdir"`
	DependsOn   []string `json:"depends_on"`
	Tags        []string `json:"tags"`
}

// CreateServiceRequest POST /api/services 请求体
type CreateServiceRequest struct {
	Name        string   `json:"name"`
	Version     string   `json:"version,omitempty"`
	Description string   `json:"description,omitempty"`
	Icon        string   `json:"icon,omitempty"`
	Autostart   *bool    `json:"autostart,omitempty"`
	Command     []string `json:"command"`
	Runtime     string   `json:"runtime,omitempty"`
	User        string   `json:"user,omitempty"`
	Group       string   `json:"group,omitempty"`
	Workdir     string   `json:"workdir,omitempty"`
	DependsOn   []string `json:"depends_on,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	// E-09-002/F-01-001 修复：补充完整配置块（与 service.yaml 字段一致）
	Readiness *ReadinessRequest `json:"readiness,omitempty"`
	Restart   *RestartRequest   `json:"restart,omitempty"`
	Stop      *StopRequest      `json:"stop,omitempty"`
	Logging   *LoggingRequest   `json:"logging,omitempty"`
	Signals   *SignalsRequest   `json:"signals,omitempty"`
}

// ReadinessRequest 对应 service.yaml 的 readiness 配置
type ReadinessRequest struct {
	Type            string   `json:"type"`
	Fd              int      `json:"fd,omitempty"`
	Port            int      `json:"port,omitempty"`
	URL             string   `json:"url,omitempty"`
	ExpectedStatus  int      `json:"expected_status,omitempty"`
	Check           []string `json:"check,omitempty"`
	IntervalSeconds int      `json:"interval_seconds,omitempty"`
	TimeoutSeconds  int      `json:"timeout_seconds,omitempty"`
}

// RestartRequest 对应 service.yaml 的 restart 配置
type RestartRequest struct {
	Policy            string `json:"policy"`
	BackoffMs         int    `json:"backoff_ms,omitempty"`
	MaxBackoffMs      int    `json:"max_backoff_ms,omitempty"`
	Multiplier        int    `json:"multiplier,omitempty"`
	MaxRetries        int    `json:"max_retries,omitempty"`
	ResetAfterSeconds int    `json:"reset_after_seconds,omitempty"`
}

// StopRequest 对应 service.yaml 的 stop 配置
type StopRequest struct {
	GraceSeconds   int `json:"grace_seconds,omitempty"`
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
}

// LoggingRequest 对应 service.yaml 的 logging 配置
type LoggingRequest struct {
	Enabled   *bool `json:"enabled,omitempty"`
	MaxSizeMB int   `json:"max_size_mb,omitempty"`
	MaxFiles  int   `json:"max_files,omitempty"`
}

// SignalsRequest 对应 service.yaml 的 signals 配置
type SignalsRequest struct {
	Reload       string `json:"reload,omitempty"`
	RotateLogs   string `json:"rotate_logs,omitempty"`
	GracefulQuit string `json:"graceful_quit,omitempty"`
}

// handleListServices GET /api/services
// REQ-I-006: 列出所有服务，含运行时状态
func (s *Server) handleListServices(w http.ResponseWriter, r *http.Request) {
	if s.stateProvider == nil {
		respondError(w, errors.ErrInternal, "state provider not configured")
		return
	}

	states := s.stateProvider.ListServiceStates()

	services := make([]ServiceSummary, 0, len(states))
	for name, info := range states {
		summary := ServiceSummary{
			Name:         name,
			Status:       string(info.State),
			PID:          info.PID,
			Uptime:       info.Uptime,
			RestartCount: info.RestartCount,
		}

		if info.Config != nil {
			summary.Icon = info.Config.Icon
			summary.Tags = info.Config.Tags
		}

		// 采集运行中服务的 CPU/内存/端口
		if info.PID > 0 {
			if res, err := collectProcessResources(info.PID); err == nil {
				summary.CPUPercent = res.CPUPercent
				summary.MemoryMB = res.MemoryMB
				summary.Ports = collectProcessPorts(info.PID)
			} else {
				// 降级方案：PID命名空间不一致时，通过命令行匹配 /proc
				cmdPattern := ""
				if info.Config != nil && len(info.Config.Command) > 0 {
					cmdPattern = info.Config.Command[0]
				}
				procs := collectProcessTreeByCommand(info.PID, cmdPattern)
				for _, p := range procs {
					summary.MemoryMB += p.MemoryMB
				}
				summary.Ports = collectProcessPortsByCommand(cmdPattern)
			}
		}

		services = append(services, summary)
	}

	respondJSON(w, http.StatusOK, ServiceListResponse{Services: services})
}

// handleGetService GET /api/services/{name}
// REQ-I-006: 获取服务详情，含完整配置+运行时状态+待生效变更
func (s *Server) handleGetService(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		respondError(w, errors.ErrInvalidRequest, "service name is required")
		return
	}
	// K-01-003: 校验 URL name 格式，防止路径穿越
	if !isValidServiceName(name) {
		respondError(w, errors.ErrInvalidRequest, fmt.Sprintf("invalid service name: %s", name))
		return
	}

	if s.stateProvider == nil {
		respondError(w, errors.ErrInternal, "state provider not configured")
		return
	}

	info, exists := s.stateProvider.GetServiceState(name)
	if !exists {
		respondError(w, errors.ErrServiceNotFound, fmt.Sprintf("service %s not found", name))
		return
	}

	detail := ServiceDetail{
		Name:         name,
		Status:       string(info.State),
		PID:          info.PID,
		Uptime:       info.Uptime,
		RestartCount: info.RestartCount,
		ConfigPath:   s.stripWorkdirPrefix(info.ConfigPath),
	}

	if info.Config != nil {
		detail.Config = &ServiceConfigDetail{
			Name:        info.Config.Name,
			Version:     info.Config.Version,
			Description: info.Config.Description,
			Icon:        info.Config.Icon,
			Autostart:   info.Config.Autostart,
			Command:     info.Config.Command,
			Runtime:     info.Config.Runtime,
			User:        info.Config.User,
			Group:       info.Config.Group,
			Workdir:     info.Config.Workdir,
			DependsOn:   info.Config.DependsOn,
			Tags:        info.Config.Tags,
		}
	}

	// 待生效变更（ReloadManager 的待生效变更展示功能暂未接入）
	// I-04-004 修复：移除无意义的占位代码

	// 采集运行中服务的 CPU/内存/端口
	if info.PID > 0 {
		if res, err := collectProcessResources(info.PID); err == nil {
			detail.CPUPercent = res.CPUPercent
			detail.MemoryMB = res.MemoryMB
			detail.Ports = collectProcessPorts(info.PID)
		} else {
			cmdPattern := ""
			if info.Config != nil && len(info.Config.Command) > 0 {
				cmdPattern = info.Config.Command[0]
			}
			detail.Ports = collectProcessPortsByCommand(cmdPattern)
		}
	}

	respondJSON(w, http.StatusOK, detail)
}

// handleCreateService POST /api/services
// REQ-I-006: 创建服务，body为JSON等价格式，后端生成service.yaml文件
func (s *Server) handleCreateService(w http.ResponseWriter, r *http.Request) {
	var req CreateServiceRequest
	// N-01-001 修复：空 body 时返回友好错误消息（避免向用户暴露 "EOF"）
	if err := decodeJSONBody(r, &req); err != nil {
		respondError(w, errors.ErrInvalidRequest, err.Error())
		return
	}

	if req.Name == "" {
		respondFieldErrors(w, errors.ErrInvalidRequest, "service config validation failed",
			errors.FieldError{Field: "name", Message: "name is required"})
		return
	}

	// N-01-01: 校验服务名格式，防止路径穿越（如 ../evil 逃逸 services/ 目录）
	// REQ-D-007: 服务名必须匹配 ^[a-z][a-z0-9-]*$
	if !isValidServiceName(req.Name) {
		respondFieldErrors(w, errors.ErrInvalidRequest, "service config validation failed",
			errors.FieldError{Field: "name", Message: "name must match ^[a-z][a-z0-9-]*$ (lowercase letters, digits, hyphens; max 64 chars)"})
		return
	}

	if len(req.Command) == 0 {
		respondFieldErrors(w, errors.ErrInvalidRequest, "service config validation failed",
			errors.FieldError{Field: "command", Message: "command is required"})
		return
	}

	// N-01-002: 校验 version 必填（与配置层 validateService 一致）
	if req.Version == "" {
		respondFieldErrors(w, errors.ErrInvalidRequest, "service config validation failed",
			errors.FieldError{Field: "version", Message: "version is required"})
		return
	}

	// 检查服务是否已存在
	if s.stateProvider != nil {
		if _, exists := s.stateProvider.GetServiceState(req.Name); exists {
			respondError(w, errors.ErrServiceExists, fmt.Sprintf("service %s already exists", req.Name))
			return
		}
	}

	// N-01-I2 修复：校验 depends_on 引用是否存在（仅校验当前已注册的服务）
	if s.stateProvider != nil && len(req.DependsOn) > 0 {
		var missingDeps []errors.FieldError
		for _, dep := range req.DependsOn {
			if _, exists := s.stateProvider.GetServiceState(dep); !exists {
				missingDeps = append(missingDeps, errors.FieldError{
					Field:   "depends_on",
					Message: fmt.Sprintf("dependency %q is not registered", dep),
				})
			}
		}
		if len(missingDeps) > 0 {
			respondFieldErrors(w, errors.ErrInvalidRequest, "service config validation failed", missingDeps...)
			return
		}
	}

	// 生成服务目录路径
	var svcDir string
	if s.pathValidator != nil {
		svcDir = filepath.Join(s.pathValidator.baseDir, "services", req.Name)
	} else {
		svcDir = filepath.Join("/etc/supd/services", req.Name)
	}

	// 创建服务目录
	if err := os.MkdirAll(svcDir, 0755); err != nil {
		respondError(w, errors.ErrInternal, fmt.Sprintf("failed to create service directory: %v", err))
		return
	}

	// 构建配置并写入 service.yaml
	svcConfig := serviceRequestToConfig(req)
	data, err := yaml.Marshal(svcConfig)
	if err != nil {
		respondError(w, errors.ErrInternal, fmt.Sprintf("failed to marshal service config: %v", err))
		return
	}

	configPath := filepath.Join(svcDir, "service.yaml")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		respondError(w, errors.ErrInternal, fmt.Sprintf("failed to write service config: %v", err))
		return
	}

	// N-02-1 修复：创建文件成功后同步刷新 discovery，使新服务立即可见
	// （否则需等待文件 watcher 500ms 防抖后才能查询，立即 GET 会返回 404）
	if s.watchProvider != nil {
		if err := s.watchProvider.ReloadConfig(); err != nil {
			// 刷新失败不影响创建结果，仅记录日志
			slog.Warn("failed to reload discovery after service creation", "name", req.Name, "error", err)
		} else if wp, ok := s.watchProvider.(*CoreWatchProvider); ok && wp.Discovery != nil {
			// 将新的 Discovery 传播到 stateProvider 等所有 providers，
			// 为新发现的服务创建状态机（SetDiscovery 内部处理）
			s.UpdateDiscovery(wp.Discovery)
		}
	}

	respondJSON(w, http.StatusCreated, map[string]string{
		"name":    req.Name,
		"message": "service created",
	})
}

// handleUpdateService PUT /api/services/{name}
// REQ-I-006: 更新服务配置
func (s *Server) handleUpdateService(w http.ResponseWriter, r *http.Request) {
	// N-01-001: 限制请求体大小，防止超大请求 DoS
	r.Body = http.MaxBytesReader(w, r.Body, MaxConfigSize)

	name := chi.URLParam(r, "name")
	if name == "" {
		respondError(w, errors.ErrInvalidRequest, "service name is required")
		return
	}
	// K-01-003: 校验 URL name 格式，防止路径穿越
	if !isValidServiceName(name) {
		respondError(w, errors.ErrInvalidRequest, fmt.Sprintf("invalid service name: %s", name))
		return
	}

	// 检查服务是否存在
	if s.stateProvider != nil {
		if _, exists := s.stateProvider.GetServiceState(name); !exists {
			respondError(w, errors.ErrServiceNotFound, fmt.Sprintf("service %s not found", name))
			return
		}
	}

	var req CreateServiceRequest
	if err := decodeJSONBody(r, &req); err != nil {
		respondError(w, errors.ErrInvalidRequest, err.Error())
		return
	}

	// 确保 name 一致
	req.Name = name

	// 生成 service.yaml 文件路径
	var svcDir string
	if s.pathValidator != nil {
		svcDir = filepath.Join(s.pathValidator.baseDir, "services", name)
	} else {
		svcDir = filepath.Join("/etc/supd/services", name)
	}

	configPath := filepath.Join(svcDir, "service.yaml")

	svcConfig := serviceRequestToConfig(req)
	data, err := yaml.Marshal(svcConfig)
	if err != nil {
		respondError(w, errors.ErrInternal, fmt.Sprintf("failed to marshal service config: %v", err))
		return
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		respondError(w, errors.ErrInternal, fmt.Sprintf("failed to write service config: %v", err))
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"name":    name,
		"message": "service updated",
	})
}

// handleDeleteService DELETE /api/services/{name}
// REQ-I-006: 删除服务，必须down或failed状态；运行中返回409 SERVICE_RUNNING
// REQ-I-006: 删除整个服务目录含extensions/，保留data/目录
func (s *Server) handleDeleteService(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		respondError(w, errors.ErrInvalidRequest, "service name is required")
		return
	}
	// K-01-003: 校验 URL name 格式，防止路径穿越
	if !isValidServiceName(name) {
		respondError(w, errors.ErrInvalidRequest, fmt.Sprintf("invalid service name: %s", name))
		return
	}

	if s.stateProvider == nil {
		respondError(w, errors.ErrInternal, "state provider not configured")
		return
	}

	info, exists := s.stateProvider.GetServiceState(name)
	if !exists {
		respondError(w, errors.ErrServiceNotFound, fmt.Sprintf("service %s not found", name))
		return
	}

	// N-02-2 修复：检查服务状态，运行中（starting/up/ready/stopping）不允许删除
	// pending 是初始状态（未启动），与 down/failed 一样允许删除
	status := string(info.State)
	isRunning := status == "starting" || status == "up" || status == "ready" || status == "stopping"
	if isRunning {
		respondError(w, errors.ErrServiceRunning, fmt.Sprintf("service %s is running (status: %s), stop it first", name, status))
		return
	}

	// 生成服务目录路径
	var svcDir string
	if s.pathValidator != nil {
		svcDir = filepath.Join(s.pathValidator.baseDir, "services", name)
	} else {
		svcDir = filepath.Join("/etc/supd/services", name)
	}

	// REQ-I-006: 删除整个服务目录含extensions/，保留data/目录
	dataDir := filepath.Join(svcDir, "data")
	dataBackup := ""
	if info, err := os.Stat(dataDir); err == nil && info.IsDir() {
		dataBackup = dataDir + ".bak"
		if err := os.Rename(dataDir, dataBackup); err != nil {
			respondError(w, errors.ErrInternal, fmt.Sprintf("failed to backup data directory: %v", err))
			return
		}
	}

	// 删除整个服务目录
	if err := os.RemoveAll(svcDir); err != nil {
		if dataBackup != "" {
			if rerr := os.Rename(dataBackup, dataDir); rerr != nil {
				slog.Warn("rollback data directory after remove failure failed", "backup", dataBackup, "target", dataDir, "error", rerr)
			}
		}
		respondError(w, errors.ErrInternal, fmt.Sprintf("failed to remove service directory: %v", err))
		return
	}

	// 恢复 data/ 目录
	if dataBackup != "" {
		if err := os.MkdirAll(svcDir, 0755); err == nil {
			if rerr := os.Rename(dataBackup, dataDir); rerr != nil {
				slog.Warn("restore data directory after service deletion failed", "backup", dataBackup, "target", dataDir, "error", rerr)
			}
		}
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"name":    name,
		"message": "service deleted",
	})
}

// serviceRequestToConfig 将 API 请求转为 ServiceConfig
func serviceRequestToConfig(req CreateServiceRequest) map[string]any {
	cfg := map[string]any{
		"name":        req.Name,
		"version":     req.Version,
		"description": req.Description,
		"icon":        req.Icon,
		"autostart":   req.Autostart,
		"command":     req.Command,
		"runtime":     req.Runtime,
		"user":        req.User,
		"group":       req.Group,
		"workdir":     req.Workdir,
		"depends_on":  req.DependsOn,
		"tags":        req.Tags,
	}
	// E-09-002/F-01-001 修复：补充完整配置块
	if req.Readiness != nil {
		r := map[string]any{"type": req.Readiness.Type}
		if req.Readiness.Fd != 0 {
			r["fd"] = req.Readiness.Fd
		}
		if req.Readiness.Port != 0 {
			r["port"] = req.Readiness.Port
		}
		if req.Readiness.URL != "" {
			r["url"] = req.Readiness.URL
		}
		if req.Readiness.ExpectedStatus != 0 {
			r["expected_status"] = req.Readiness.ExpectedStatus
		}
		if len(req.Readiness.Check) > 0 {
			r["check"] = req.Readiness.Check
		}
		if req.Readiness.IntervalSeconds != 0 {
			r["interval_seconds"] = req.Readiness.IntervalSeconds
		}
		if req.Readiness.TimeoutSeconds != 0 {
			r["timeout_seconds"] = req.Readiness.TimeoutSeconds
		}
		cfg["readiness"] = r
	}
	if req.Restart != nil {
		r := map[string]any{"policy": req.Restart.Policy}
		if req.Restart.BackoffMs != 0 {
			r["backoff_ms"] = req.Restart.BackoffMs
		}
		if req.Restart.MaxBackoffMs != 0 {
			r["max_backoff_ms"] = req.Restart.MaxBackoffMs
		}
		if req.Restart.Multiplier != 0 {
			r["multiplier"] = req.Restart.Multiplier
		}
		if req.Restart.MaxRetries != 0 {
			r["max_retries"] = req.Restart.MaxRetries
		}
		if req.Restart.ResetAfterSeconds != 0 {
			r["reset_after_seconds"] = req.Restart.ResetAfterSeconds
		}
		cfg["restart"] = r
	}
	if req.Stop != nil {
		s := map[string]any{}
		if req.Stop.GraceSeconds != 0 {
			s["grace_seconds"] = req.Stop.GraceSeconds
		}
		if req.Stop.TimeoutSeconds != 0 {
			s["timeout_seconds"] = req.Stop.TimeoutSeconds
		}
		if len(s) > 0 {
			cfg["stop"] = s
		}
	}
	if req.Logging != nil {
		l := map[string]any{}
		if req.Logging.Enabled != nil {
			l["enabled"] = *req.Logging.Enabled
		}
		if req.Logging.MaxSizeMB != 0 {
			l["max_size_mb"] = req.Logging.MaxSizeMB
		}
		if req.Logging.MaxFiles != 0 {
			l["max_files"] = req.Logging.MaxFiles
		}
		if len(l) > 0 {
			cfg["logging"] = l
		}
	}
	if req.Signals != nil {
		s := map[string]any{}
		if req.Signals.Reload != "" {
			s["reload"] = req.Signals.Reload
		}
		if req.Signals.RotateLogs != "" {
			s["rotate_logs"] = req.Signals.RotateLogs
		}
		if req.Signals.GracefulQuit != "" {
			s["graceful_quit"] = req.Signals.GracefulQuit
		}
		if len(s) > 0 {
			cfg["signals"] = s
		}
	}
	return cfg
}
