package logging

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// REQ-F-010: 日志搜索上限1000行
const DefaultMaxLines = 1000

// SearchParams 日志搜索参数
// REQ-F-010: 后端grep实现，返回匹配行+上下文，单次搜索最多返回1000行匹配
type SearchParams struct {
	Pattern         string // 搜索关键字（简单子串匹配）
	ServiceName     string // 服务名（空=搜索所有服务）
	LogDir          string // 日志根目录（如 /var/log/supd/services/）
	MaxLines        int    // 最大返回行数（默认1000，REQ-F-010锁定）
	ContextLines    int    // 上下文行数（默认0）
	IncludeArchives bool   // 是否搜索归档文件
}

// SearchResult 日志搜索结果
type SearchResult struct {
	Lines        []SearchLine // 匹配行列表
	TotalMatches int          // 总匹配数（可能超过MaxLines）
	Truncated    bool         // 是否被截断
}

// SearchLine 单条搜索匹配行
type SearchLine struct {
	FilePath   string   // 来源文件
	LineNumber int      // 行号
	Content    string   // 行内容
	Context    []string // 上下文行
}

// SearchLogs 在服务日志文件中搜索匹配行
// REQ-F-010: 后端grep实现，简单子串匹配（strings.Contains），不建索引
// REQ-F-010: 单次搜索最多返回1000行匹配（数值锁定）
func SearchLogs(params SearchParams) (*SearchResult, error) {
	if params.MaxLines <= 0 {
		params.MaxLines = DefaultMaxLines
	}

	result := &SearchResult{
		// G-04-001 修复：预分配容量，避免高频日志搜索时的 1014 allocs/op
		Lines: make([]SearchLine, 0, params.MaxLines),
	}

	// 确定搜索的目录列表
	dirs, err := searchDirs(params.LogDir, params.ServiceName)
	if err != nil {
		return nil, err
	}

	// 遍历每个服务目录，搜索文件
	for _, dir := range dirs {
		if err := searchDir(dir, &params, result); err != nil {
			return nil, err
		}
		if result.TotalMatches >= params.MaxLines {
			break
		}
	}

	return result, nil
}

// searchDirs 返回需要搜索的服务目录列表
func searchDirs(logDir, serviceName string) ([]string, error) {
	if serviceName != "" {
		// 指定服务名，只搜索该服务目录
		dir := filepath.Join(logDir, serviceName)
		info, err := os.Stat(dir)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, err
		}
		if !info.IsDir() {
			return nil, nil
		}
		return []string{dir}, nil
	}

	// 未指定服务名，搜索所有服务目录
	entries, err := os.ReadDir(logDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, filepath.Join(logDir, entry.Name()))
		}
	}
	return dirs, nil
}

// searchDir 在单个服务目录中搜索匹配行
func searchDir(dir string, params *SearchParams, result *SearchResult) error {
	// 始终搜索 current 文件
	currentPath := filepath.Join(dir, "current")
	if _, err := os.Stat(currentPath); err == nil {
		if err := searchFile(currentPath, params, result); err != nil {
			return err
		}
	}

	// 搜索归档文件
	if params.IncludeArchives {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		for _, entry := range entries {
			// 归档文件以 @ 开头，后缀为 .s
			name := entry.Name()
			if name == "current" {
				continue
			}
			if strings.HasPrefix(name, "@") && strings.HasSuffix(name, ".s") {
				archivePath := filepath.Join(dir, name)
				if err := searchFile(archivePath, params, result); err != nil {
					return err
				}
				if result.TotalMatches >= params.MaxLines {
					return nil
				}
			}
		}
	}

	return nil
}

// searchFile 在单个文件中流式搜索匹配行
// G-01-001 修复：改为 bufio.Scanner 逐行扫描，不再将整个文件读入内存，
// 避免大文件(60MB+) OOM。仅累积匹配行（上限 MaxLines）及其上下文。
func searchFile(filePath string, params *SearchParams, result *SearchResult) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	// C-01-001 修复：读取场景 Close 错误无影响（数据已读完），但显式接收并记录
	defer func() {
		if cerr := f.Close(); cerr != nil {
			stderrWarn("search file close failed (ignored)", cerr)
		}
	}()

	scanner := bufio.NewScanner(f)

	ctxLines := params.ContextLines
	// before-context 环形缓冲：保存最近 ctxLines 行，匹配时提供前文
	var beforeBuf []string
	var bufStart, bufFilled int
	if ctxLines > 0 {
		beforeBuf = make([]string, ctxLines)
	}

	lineNum := 0
	// pending 记录尚未收齐 after-context 的已存储匹配（索引指向 result.Lines）
	type pendingAfter struct {
		lineIdx   int
		remaining int
	}
	var pending []pendingAfter

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// 1. 为所有待收集 after-context 的匹配追加当前行（按命中顺序）
		if ctxLines > 0 && len(pending) > 0 {
			kept := pending[:0]
			for _, p := range pending {
				result.Lines[p.lineIdx].Context = append(result.Lines[p.lineIdx].Context, line)
				p.remaining--
				if p.remaining > 0 {
					kept = append(kept, p)
				}
			}
			pending = kept
		}

		// 2. 关键字子串匹配
		if !strings.Contains(line, params.Pattern) {
			// 3. 更新 before-context 环形缓冲后继续
			if ctxLines > 0 {
				beforeBuf[bufStart] = line
				bufStart = (bufStart + 1) % ctxLines
				if bufFilled < ctxLines {
					bufFilled++
				}
			}
			continue
		}

		result.TotalMatches++

		// 已收集满则不再存储新匹配，但继续扫描以准确统计 TotalMatches
		if len(result.Lines) >= params.MaxLines {
			result.Truncated = true
			if ctxLines > 0 {
				beforeBuf[bufStart] = line
				bufStart = (bufStart + 1) % ctxLines
				if bufFilled < ctxLines {
					bufFilled++
				}
			}
			continue
		}

		sl := SearchLine{
			FilePath:   filePath,
			LineNumber: lineNum,
			Content:    line,
		}
		if ctxLines > 0 {
			sl.Context = beforeContext(beforeBuf, bufStart, bufFilled, ctxLines)
			pending = append(pending, pendingAfter{
				lineIdx:   len(result.Lines),
				remaining: ctxLines,
			})
		}
		result.Lines = append(result.Lines, sl)

		// 3. 更新 before-context 环形缓冲
		if ctxLines > 0 {
			beforeBuf[bufStart] = line
			bufStart = (bufStart + 1) % ctxLines
			if bufFilled < ctxLines {
				bufFilled++
			}
		}
	}
	return scanner.Err()
}

// beforeContext 从环形缓冲中按时间顺序（旧→新）返回当前匹配行之前的内容
func beforeContext(buf []string, start, filled, size int) []string {
	if filled == 0 {
		return nil
	}
	out := make([]string, 0, filled)
	if filled < size {
		// 未满：有效条目位于 0..filled-1
		for i := 0; i < filled; i++ {
			out = append(out, buf[i])
		}
		return out
	}
	// 已满：最旧条目位于 start
	for i := 0; i < size; i++ {
		out = append(out, buf[(start+i)%size])
	}
	return out
}
