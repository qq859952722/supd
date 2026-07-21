package api

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/supdorg/supd/internal/extension"
)

// --- TaskProvider 适配器 ---

type CoreTaskProvider struct {
	TaskMgr *extension.TaskManager
	LogDir  string
}

func (p *CoreTaskProvider) ListRuns(filter extension.RunFilter) []*extension.RunResult {
	if p.TaskMgr == nil {
		return nil
	}
	return p.TaskMgr.ListRuns(filter)
}

func (p *CoreTaskProvider) GetRun(runID string) *extension.RunResult {
	if p.TaskMgr == nil {
		return nil
	}
	return p.TaskMgr.GetRun(runID)
}

func (p *CoreTaskProvider) CancelRun(runID string) error {
	if p.TaskMgr == nil {
		return fmt.Errorf("task manager not configured")
	}
	run := p.TaskMgr.GetRun(runID)
	if run == nil {
		return fmt.Errorf("run %s not found", runID)
	}
	if run.IsTerminal() {
		return fmt.Errorf("run %s already in terminal state: %s", runID, run.State)
	}
	// N-03-001 修复：实际调用 ConcurrencyManager.CancelRun 终止进程
	if !p.TaskMgr.CancelRun(runID) {
		return fmt.Errorf("run %s not found in concurrency manager (may have already completed)", runID)
	}
	return nil
}

func (p *CoreTaskProvider) GetRunLogs(runID string, sincePos int64) ([]string, int64, error) {
	run := p.GetRun(runID)
	if run == nil {
		return nil, 0, fmt.Errorf("run %s not found", runID)
	}

	// 全局扩展和服务级扩展现在都写入独立日志文件：
	// /var/log/supd/extensions/<ext-name>/<run_id>.log
	logPath := filepath.Join(p.LogDir, "extensions", run.ExtensionName, run.RunID+".log")
	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, 0, nil
		}
		return nil, 0, err
	}
	defer f.Close()

	if sincePos > 0 {
		if _, err := f.Seek(sincePos, io.SeekStart); err != nil {
			return nil, sincePos, err
		}
	}

	lines := []string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	pos, _ := f.Seek(0, io.SeekCurrent)
	return lines, pos, scanner.Err()
}

// DeleteRunLogs 清空指定运行的日志文件
func (p *CoreTaskProvider) DeleteRunLogs(runID string) error {
	run := p.GetRun(runID)
	if run == nil {
		return fmt.Errorf("run %s not found", runID)
	}

	logPath := filepath.Join(p.LogDir, "extensions", run.ExtensionName, run.RunID+".log")
	if err := os.Truncate(logPath, 0); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ClearRuns 清除匹配过滤条件的终态任务记录
func (p *CoreTaskProvider) ClearRuns(filter extension.RunFilter) int {
	return p.TaskMgr.ClearRuns(filter)
}
