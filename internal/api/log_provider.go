package api

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/supdorg/supd/internal/logging"
)

// --- LogProvider 适配器 ---

type FileLogProvider struct {
	LogDir string
}

func (p *FileLogProvider) GetServiceLogs(serviceName string, sincePos int64) ([]string, int64, error) {
	logPath := filepath.Join(p.LogDir, "services", serviceName, "current")
	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	defer f.Close()

	if sincePos > 0 {
		if _, err := f.Seek(sincePos, io.SeekStart); err != nil {
			return nil, sincePos, err
		}
	}

	// REQ-F-010: 服务级扩展日志也写入此文件（前缀 [ext:<name>]）
	// 进程日志接口应仅返回服务进程输出，过滤掉扩展日志行
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "[ext:") {
			continue
		}
		lines = append(lines, line)
	}

	pos, _ := f.Seek(0, io.SeekCurrent)
	return lines, pos, scanner.Err()
}

func (p *FileLogProvider) SearchServiceLogs(serviceName string, pattern string, maxLines int) ([]string, error) {
	logDir := filepath.Join(p.LogDir, "services")
	result, err := logging.SearchLogs(logging.SearchParams{
		Pattern:     pattern,
		ServiceName: serviceName,
		LogDir:      logDir,
		MaxLines:    maxLines,
	})
	if err != nil {
		return nil, err
	}

	var lines []string
	for _, sl := range result.Lines {
		lines = append(lines, sl.Content)
	}
	return lines, nil
}
