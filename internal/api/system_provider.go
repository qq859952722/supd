package api

import (
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v4/process"

	"github.com/supdorg/supd/internal/config"
)

// --- SystemProvider 适配器 ---

type CoreSystemProvider struct {
	Config    *config.Config
	StartTime time.Time
	BaseDir   string
	Version   string // supd 版本号（由 cli.Version 注入，避免与 ldflags 注入的版本不一致）
	proc      *process.Process // 缓存 gopsutil Process 对象，CPUPercent 需要基线
	procOnce  sync.Once
}

// REQ-I-006: 采集 supd 自身进程的 CPU 与 RSS 内存（使用 gopsutil），
// 以及工作目录所在分区的磁盘占用（syscall.Statfs）。
func (p *CoreSystemProvider) GetSystemStatus() SystemStatusInfo {
	// 首次调用时创建 Process 对象并保存，后续复用以获得正确的 CPU 基线
	p.procOnce.Do(func() {
		if proc, err := process.NewProcess(int32(os.Getpid())); err == nil {
			p.proc = proc
			// 预热：第一次 CPUPercent 调用建立基线，返回值为 0
			_, _ = p.proc.CPUPercent()
		}
	})

	var cpuPercent float64
	var memMB float64
	if p.proc != nil {
		if v, err := p.proc.CPUPercent(); err == nil {
			cpuPercent = v
		}
		if mi, err := p.proc.MemoryInfo(); err == nil && mi != nil {
			memMB = float64(mi.RSS) / 1024 / 1024
		}
	}

	// 磁盘：BaseDir 所在分区的总/已用空间
	var diskTotalMB, diskUsedMB float64
	if p.BaseDir != "" {
		var stat syscall.Statfs_t
		if err := syscall.Statfs(p.BaseDir, &stat); err == nil {
			totalBytes := stat.Blocks * uint64(stat.Bsize)
			freeBytes := stat.Bavail * uint64(stat.Bsize)
			diskTotalMB = float64(totalBytes) / 1024 / 1024
			diskUsedMB = float64(totalBytes-freeBytes) / 1024 / 1024
		}
	}

	return SystemStatusInfo{
		StartTime:   p.StartTime,
		Version:     p.Version,
		Uptime:      int64(time.Since(p.StartTime).Seconds()),
		HTTPListen:  p.Config.Settings.HTTPListen,
		AuthMode:    p.Config.Settings.AuthMode,
		WorkDir:     p.BaseDir,
		CPU:         cpuPercent,
		MemoryMB:    memMB,
		DiskTotalMB: diskTotalMB,
		DiskUsedMB:  diskUsedMB,
	}
}
