package api

import (
	"fmt"
	"net/http"
	"path/filepath"
	"syscall"

	"github.com/go-chi/chi/v5"

	"github.com/supdorg/supd/internal/errors"
)

// REQ-I-006: 资源查询 API

// ResourceResponse 资源使用响应
type ResourceResponse struct {
	CPUPercent    float64   `json:"cpu_percent"`
	MemoryMB      float64   `json:"memory_mb"`
	MemoryPercent float64   `json:"memory_percent"`
	ProcessCount  int       `json:"process_count"`
	FDCount       int       `json:"fd_count"`
	DiskTotalMB   float64   `json:"disk_total_mb,omitempty"`
	DiskUsedMB    float64   `json:"disk_used_mb,omitempty"`
	Ports         []PortInfo `json:"ports,omitempty"`
}

// ProcessInfo 进程信息
type ProcessInfo struct {
	PID         int     `json:"pid"`
	PPID        int     `json:"ppid"`
	User        string  `json:"user"`
	UID         int     `json:"uid"`
	Group       string  `json:"group"`
	GID         int     `json:"gid"`
	CPUPercent  float64 `json:"cpu_percent"`
	MemoryMB    float64 `json:"memory_mb"`
	Status      string  `json:"status"`
	ThreadCount int     `json:"thread_count"`
	Command     string  `json:"command"`
	// StartedAt 进程启动时间（Unix 毫秒时间戳，与 gopsutil CreateTime 一致）
	StartedAt int64 `json:"started_at"`
}

// ProcessListResponse 进程列表响应
type ProcessListResponse struct {
	Processes []ProcessInfo `json:"processes"`
}

// handleServiceResources GET /api/services/{name}/resources
// REQ-I-006: 按需采集服务资源使用情况
func (s *Server) handleServiceResources(w http.ResponseWriter, r *http.Request) {
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

	info, exists := s.stateProvider.GetServiceState(name)
	if !exists || info.PID == 0 {
		respondJSON(w, http.StatusOK, ResourceResponse{})
		return
	}

	resources, err := collectProcessResources(info.PID)
	if err != nil {
		// 降级方案：PID命名空间不一致时，通过命令行匹配 /proc 获取资源
		cmdPattern := ""
		if info.Config != nil && len(info.Config.Command) > 0 {
			cmdPattern = info.Config.Command[0]
		}
		procs := collectProcessTreeByCommand(info.PID, cmdPattern)
		resources = &ResourceResponse{}
		for _, p := range procs {
			resources.MemoryMB += p.MemoryMB
			resources.ProcessCount++
		}
		// 端口采集也走降级方案：通过命令行匹配 host PID
		resources.Ports = collectProcessPortsByCommand(cmdPattern)
	} else {
		// 正常路径：直接用 namespace PID 采集端口
		resources.Ports = collectProcessPorts(info.PID)
	}

	// 计算服务所在文件夹的磁盘分区占用
	workdir := ""
	if info.Config != nil && info.Config.Workdir != "" {
		workdir = info.Config.Workdir
	} else if info.ConfigPath != "" {
		workdir = filepath.Dir(info.ConfigPath)
	}
	if workdir != "" {
		var stat syscall.Statfs_t
		if err := syscall.Statfs(workdir, &stat); err == nil {
			totalBytes := stat.Blocks * uint64(stat.Bsize)
			freeBytes := stat.Bavail * uint64(stat.Bsize)
			resources.DiskTotalMB = float64(totalBytes) / 1024 / 1024
			resources.DiskUsedMB = float64(totalBytes-freeBytes) / 1024 / 1024
		}
	}

	respondJSON(w, http.StatusOK, resources)
}

// handleServiceProcesses GET /api/services/{name}/processes
// REQ-I-006: 获取服务进程树
func (s *Server) handleServiceProcesses(w http.ResponseWriter, r *http.Request) {
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

	info, exists := s.stateProvider.GetServiceState(name)
	if !exists || info.PID == 0 {
		respondJSON(w, http.StatusOK, ProcessListResponse{Processes: []ProcessInfo{}})
		return
	}

	processes, err := collectProcessTree(info.PID)
	if err != nil || len(processes) == 0 {
		// 降级方案：PID命名空间不一致时（如sandbox环境），通过命令行匹配 /proc
		cmdPattern := ""
		if info.Config != nil && len(info.Config.Command) > 0 {
			cmdPattern = info.Config.Command[0]
		}
		processes = collectProcessTreeByCommand(info.PID, cmdPattern)
	}

	respondJSON(w, http.StatusOK, ProcessListResponse{Processes: processes})
}
