package api

import (
	"encoding/json"
	stderrors "errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/go-chi/chi/v5"
	"go.yaml.in/yaml/v4"

	"github.com/supdorg/supd/internal/config"
	"github.com/supdorg/supd/internal/core"
	"github.com/supdorg/supd/internal/errors"
)

// REQ-I-006: 服务操作 API

// SignalRequest POST /api/services/{name}/signal body
type SignalRequest struct {
	Signal string `json:"signal"` // 如 "HUP", "USR1"
}

// handleStartService POST /api/services/{name}/start
// REQ-I-006: 启动服务，返回202 Accepted
func (s *Server) handleStartService(w http.ResponseWriter, r *http.Request) {
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

	if s.serviceOperator == nil {
		respondError(w, errors.ErrInternal, "service operator not configured")
		return
	}

	if s.stateProvider != nil {
		if _, exists := s.stateProvider.GetServiceState(name); !exists {
			respondError(w, errors.ErrServiceNotFound, fmt.Sprintf("service %s not found", name))
			return
		}
	}

	if err := s.serviceOperator.StartService(name); err != nil {
		// N-04-USER-CRED 修复：优先识别 *ServiceError（如 RUNTIME_USER_NOT_FOUND），
		// 按 Code 映射 HTTP 状态码（422 Unprocessable Entity），避免被降级为 500
		var se *errors.ServiceError
		if stderrors.As(err, &se) {
			respondProviderError(w, err)
			return
		}
		// 区分"服务已运行"（409 Conflict）、"命令不存在"（400）和其他启动失败（500）
		errMsg := err.Error()
		if strings.Contains(errMsg, "already running") {
			respondError(w, errors.ErrServiceRunning, errMsg)
		} else if strings.Contains(errMsg, "no such file") || strings.Contains(errMsg, "executable file not found") {
			// N-01-I3 修复：命令不存在属于配置错误，返回 400 而非 500
			respondError(w, errors.ErrInvalidRequest, fmt.Sprintf("failed to start service %s: %v", name, err))
		} else {
			respondError(w, errors.ErrInternal, fmt.Sprintf("failed to start service %s: %v", name, err))
		}
		return
	}

	respondJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

// handleStopService POST /api/services/{name}/stop
// REQ-I-006: 停止服务，返回202 Accepted
func (s *Server) handleStopService(w http.ResponseWriter, r *http.Request) {
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

	if s.serviceOperator == nil {
		respondError(w, errors.ErrInternal, "service operator not configured")
		return
	}

	if s.stateProvider != nil {
		info, exists := s.stateProvider.GetServiceState(name)
		if !exists {
			respondError(w, errors.ErrServiceNotFound, fmt.Sprintf("service %s not found", name))
			return
		}
		// N-02-002 修复：停止未运行的服务时返回400而非500
		if info.State != core.StateUp && info.State != core.StateReady && info.State != core.StateStarting {
			respondError(w, errors.ErrInvalidRequest, fmt.Sprintf("服务 %s 当前状态为 %s，无法停止（服务未运行）", name, info.State))
			return
		}
	}

	if err := s.serviceOperator.StopService(name); err != nil {
		respondError(w, errors.ErrInternal, fmt.Sprintf("failed to stop service %s: %v", name, err))
		return
	}

	respondJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

// handleRestartService POST /api/services/{name}/restart
// REQ-I-006: 重启服务，返回202 Accepted
func (s *Server) handleRestartService(w http.ResponseWriter, r *http.Request) {
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

	if s.serviceOperator == nil {
		respondError(w, errors.ErrInternal, "service operator not configured")
		return
	}

	if s.stateProvider != nil {
		if _, exists := s.stateProvider.GetServiceState(name); !exists {
			respondError(w, errors.ErrServiceNotFound, fmt.Sprintf("service %s not found", name))
			return
		}
	}

	if err := s.serviceOperator.RestartService(name); err != nil {
		// N-04-USER-CRED 修复：优先识别 *ServiceError（如 RUNTIME_USER_NOT_FOUND），
		// 按 Code 映射 HTTP 状态码（422 Unprocessable Entity），避免被降级为 500
		var se *errors.ServiceError
		if stderrors.As(err, &se) {
			respondProviderError(w, err)
			return
		}
		respondError(w, errors.ErrInternal, fmt.Sprintf("failed to restart service %s: %v", name, err))
		return
	}

	respondJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

// handleSignalService POST /api/services/{name}/signal
// REQ-I-006: 向服务进程组发送自定义信号，返回202 Accepted
func (s *Server) handleSignalService(w http.ResponseWriter, r *http.Request) {
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

	if s.serviceOperator == nil {
		respondError(w, errors.ErrInternal, "service operator not configured")
		return
	}

	if s.stateProvider != nil {
		info, exists := s.stateProvider.GetServiceState(name)
		if !exists {
			respondError(w, errors.ErrServiceNotFound, fmt.Sprintf("service %s not found", name))
			return
		}
		// N-02-001 修复：仅运行中状态（starting/up/ready）允许发送信号，
		// 其他状态（pending/stopping/down/failed）返回 400 INVALID_REQUEST
		// 否则 SendSignal 会因进程不存在返回 500 INTERNAL_ERROR，让用户误以为是系统 bug
		switch info.State {
		case core.StateStarting, core.StateUp, core.StateReady:
			// 允许发送信号
		default:
			respondError(w, errors.ErrInvalidRequest,
				fmt.Sprintf("service %s is in state %s, cannot send signal (only starting/up/ready allowed)", name, info.State))
			return
		}
	}

	var req SignalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, errors.ErrInvalidRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	if req.Signal == "" {
		respondFieldErrors(w, errors.ErrInvalidRequest, "signal is required",
			errors.FieldError{Field: "signal", Message: "signal name is required"})
		return
	}

	sig, err := parseSignal(req.Signal)
	if err != nil {
		respondError(w, errors.ErrInvalidRequest, err.Error())
		return
	}

	if err := s.serviceOperator.SendSignal(name, sig); err != nil {
		respondError(w, errors.ErrInternal, fmt.Sprintf("failed to send signal to service %s: %v", name, err))
		return
	}

	respondJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

// parseSignal 将信号名字符串转为 syscall.Signal
func parseSignal(name string) (syscall.Signal, error) {
	// 去除可能的 SIG 前缀
	if len(name) > 3 && name[:3] == "SIG" {
		name = name[3:]
	}

	signalMap := map[string]syscall.Signal{
		"HUP":    syscall.SIGHUP,
		"INT":    syscall.SIGINT,
		"QUIT":   syscall.SIGQUIT,
		"ILL":    syscall.SIGILL,
		"TRAP":   syscall.SIGTRAP,
		"ABRT":   syscall.SIGABRT,
		"BUS":    syscall.SIGBUS,
		"FPE":    syscall.SIGFPE,
		"KILL":   syscall.SIGKILL,
		"USR1":   syscall.SIGUSR1,
		"SEGV":   syscall.SIGSEGV,
		"USR2":   syscall.SIGUSR2,
		"PIPE":   syscall.SIGPIPE,
		"ALRM":   syscall.SIGALRM,
		"TERM":   syscall.SIGTERM,
		"CHLD":   syscall.SIGCHLD,
		"CONT":   syscall.SIGCONT,
		"STOP":   syscall.SIGSTOP,
		"TSTP":   syscall.SIGTSTP,
		"TTIN":   syscall.SIGTTIN,
		"TTOU":   syscall.SIGTTOU,
		"URG":    syscall.SIGURG,
		"XCPU":   syscall.SIGXCPU,
		"XFSZ":   syscall.SIGXFSZ,
		"VTALRM": syscall.SIGVTALRM,
		"PROF":   syscall.SIGPROF,
		"WINCH":  syscall.SIGWINCH,
		"IO":     syscall.SIGIO,
		"SYS":    syscall.SIGSYS,
	}

	sig, ok := signalMap[name]
	if !ok {
		if n, err := strconv.Atoi(name); err == nil {
			return syscall.Signal(n), nil
		}
		return 0, fmt.Errorf("unknown signal: %s", name)
	}
	return sig, nil
}

// handleStartAllServices POST /api/services/start
// 批量启动所有服务
func (s *Server) handleStartAllServices(w http.ResponseWriter, r *http.Request) {
	if s.serviceOperator == nil {
		respondError(w, errors.ErrInternal, "service operator not configured")
		return
	}
	if s.stateProvider == nil {
		respondError(w, errors.ErrInternal, "state provider not configured")
		return
	}

	states := s.stateProvider.ListServiceStates()
	started, failed := 0, 0
	for name := range states {
		if err := s.serviceOperator.StartService(name); err != nil {
			failed++
		} else {
			started++
		}
	}

	respondJSON(w, http.StatusAccepted, map[string]int{
		"started": started,
		"failed":  failed,
	})
}

// handleStopAllServices POST /api/services/stop
// 批量停止所有服务
func (s *Server) handleStopAllServices(w http.ResponseWriter, r *http.Request) {
	if s.serviceOperator == nil {
		respondError(w, errors.ErrInternal, "service operator not configured")
		return
	}
	if s.stateProvider == nil {
		respondError(w, errors.ErrInternal, "state provider not configured")
		return
	}

	states := s.stateProvider.ListServiceStates()
	stopped, failed := 0, 0
	for name := range states {
		if err := s.serviceOperator.StopService(name); err != nil {
			failed++
		} else {
			stopped++
		}
	}

	respondJSON(w, http.StatusAccepted, map[string]int{
		"stopped": stopped,
		"failed":  failed,
	})
}

// handleForceStopService POST /api/services/{name}/force-stop
// 强制停止服务（SIGKILL）
func (s *Server) handleForceStopService(w http.ResponseWriter, r *http.Request) {
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

	if s.serviceOperator == nil {
		respondError(w, errors.ErrInternal, "service operator not configured")
		return
	}

	if s.stateProvider != nil {
		if _, exists := s.stateProvider.GetServiceState(name); !exists {
			respondError(w, errors.ErrServiceNotFound, fmt.Sprintf("service %s not found", name))
			return
		}
	}

	if err := s.serviceOperator.ForceStopService(name); err != nil {
		respondError(w, errors.ErrInternal, fmt.Sprintf("failed to force stop service %s: %v", name, err))
		return
	}

	respondJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

// handleClearFailedService POST /api/services/{name}/clear-failed
// 清除服务的 failed 状态，重置为 pending
func (s *Server) handleClearFailedService(w http.ResponseWriter, r *http.Request) {
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

	if s.serviceOperator == nil {
		respondError(w, errors.ErrInternal, "service operator not configured")
		return
	}

	if s.stateProvider != nil {
		if _, exists := s.stateProvider.GetServiceState(name); !exists {
			respondError(w, errors.ErrServiceNotFound, fmt.Sprintf("service %s not found", name))
			return
		}
	}

	if err := s.serviceOperator.ClearFailedState(name); err != nil {
		respondError(w, errors.ErrInternal, fmt.Sprintf("failed to clear failed state for %s: %v", name, err))
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

// handleUpdateServiceConfig PUT /api/services/{name}/config
// 更新服务配置文件内容（原始 YAML 文本）
func (s *Server) handleUpdateServiceConfig(w http.ResponseWriter, r *http.Request) {
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

	if s.stateProvider != nil {
		if _, exists := s.stateProvider.GetServiceState(name); !exists {
			respondError(w, errors.ErrServiceNotFound, fmt.Sprintf("service %s not found", name))
			return
		}
	}

	// N-01-001: 限制请求体大小，防止超大请求 DoS
	r.Body = http.MaxBytesReader(w, r.Body, MaxConfigSize)

	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, errors.ErrInvalidRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	// N-02-01: 拒绝空内容，防止静默销毁配置文件
	if strings.TrimSpace(req.Content) == "" {
		respondFieldErrors(w, errors.ErrInvalidRequest, "config content must not be empty",
			errors.FieldError{Field: "content", Message: "config content is required and must not be empty/whitespace-only"})
		return
	}

	// 获取配置文件路径
	var configPath string
	if s.stateProvider != nil {
		if info, exists := s.stateProvider.GetServiceState(name); exists && info.ConfigPath != "" {
			configPath = info.ConfigPath
		}
	}
	if configPath == "" {
		if s.pathValidator != nil {
			configPath = filepath.Join(s.pathValidator.baseDir, "services", name, "service.yaml")
		} else {
			configPath = filepath.Join("/etc/supd/services", name, "service.yaml")
		}
	}

	if err := os.WriteFile(configPath, []byte(req.Content), 0644); err != nil {
		respondError(w, errors.ErrInternal, fmt.Sprintf("failed to write config: %v", err))
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"name":    name,
		"message": "config updated",
	})
}

// handleSaveServiceEnv PUT /api/services/{name}/env
// 保存服务环境变量
func (s *Server) handleSaveServiceEnv(w http.ResponseWriter, r *http.Request) {
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

	if s.stateProvider != nil {
		if _, exists := s.stateProvider.GetServiceState(name); !exists {
			respondError(w, errors.ErrServiceNotFound, fmt.Sprintf("service %s not found", name))
			return
		}
	}

	// 前端发送 config.EnvFile JSON 格式（{env:{KEY:{value,enabled?,hint?}}}）
	// 与 handleSaveExtensionEnv / handleUpdateEnv 保持一致
	var envFile config.EnvFile
	// N-01-001: 限制请求体大小，防止超大请求 DoS
	r.Body = http.MaxBytesReader(w, r.Body, MaxConfigSize)
	if err := json.NewDecoder(r.Body).Decode(&envFile); err != nil {
		respondError(w, errors.ErrInvalidRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}
	if envFile.Env == nil {
		envFile.Env = make(map[string]config.EnvVar)
	}

	// 确定环境变量文件路径
	var envPath string
	if s.pathValidator != nil {
		envPath = filepath.Join(s.pathValidator.baseDir, "services", name, "env.yaml")
	} else {
		envPath = filepath.Join("/etc/supd/services", name, "env.yaml")
	}

	// 序列化为 YAML 并写入（含 env: 包装层，格式与 config.LoadEnv 兼容）
	data, err := yaml.Marshal(&envFile)
	if err != nil {
		respondError(w, errors.ErrInternal, fmt.Sprintf("failed to marshal env: %v", err))
		return
	}

	if err := os.WriteFile(envPath, data, 0644); err != nil {
		respondError(w, errors.ErrInternal, fmt.Sprintf("failed to write env file: %v", err))
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"name":    name,
		"message": "env saved",
	})
}
