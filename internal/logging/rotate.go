package logging

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// RotatingLogWriter 带轮转功能的日志写入器
// REQ-F-010: 单文件超过 max_size_mb 触发轮转，归档文件超过 max_files 删除最旧
// REQ-C-003: 文件写互斥为唯一允许的 mutex（继承 LogWriter 的 mu）
type RotatingLogWriter struct {
	mu        sync.Mutex
	writer    *LogWriter
	maxSizeMB int
	maxFiles  int
	dirPath   string
	// G-01-2 修复：内存计数器累计已写字节数，避免每条日志 os.Stat syscall
	// 每 1MB 校准一次（防止计数器漂移），轮转后归零
	currentSize int64
	// G-01-001: 磁盘满降级内存缓冲（REQ-E-008: 磁盘满→内存缓冲1000行）
	// 写入失败时降级到内存缓冲，写入成功后尝试 flush 缓冲内容
	diskBuffer *DiskFullBuffer
}

// NewRotatingLogWriter 创建带轮转功能的日志写入器
// REQ-F-010: max_size_mb 默认 10，max_files 默认 5（0 自动应用默认值）
func NewRotatingLogWriter(path string, maxSizeMB, maxFiles int) (*RotatingLogWriter, error) {
	// 应用默认值（与 config.validateLogging 一致）
	if maxSizeMB <= 0 {
		maxSizeMB = 10
	}
	if maxFiles <= 0 {
		maxFiles = 5
	}

	writer, err := NewLogWriter(path)
	if err != nil {
		return nil, err
	}

	dirPath := filepath.Dir(path)

	// G-01-2 修复：初始化时获取当前文件大小作为基线
	initialSize := int64(0)
	if info, err := os.Stat(path); err == nil {
		initialSize = info.Size()
	}

	return &RotatingLogWriter{
		writer:      writer,
		maxSizeMB:   maxSizeMB,
		maxFiles:    maxFiles,
		dirPath:     dirPath,
		currentSize: initialSize,
		diskBuffer:  NewDiskFullBuffer(), // G-01-001: 初始化磁盘满降级缓冲
	}, nil
}

// Write 写入日志行，写入后检查文件大小，超过 maxSizeMB 触发轮转
// REQ-F-010: 单文件超过 max_size_mb 触发轮转
// G-01-2 修复：用内存计数器替代每条日志 os.Stat，校准阈值 1MB
// G-01-001: 写入失败降级到内存缓冲（REQ-E-008），写入成功后 flush 缓冲
func (r *RotatingLogWriter) Write(line []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	n, err := r.writer.Write(line)
	if err != nil {
		// G-01-001: 写入失败（可能磁盘满），降级到内存缓冲
		// REQ-E-008: 磁盘满→内存缓冲1000行，缓冲满时丢弃最旧
		r.diskBuffer.Add(string(line))
		slog.Warn("log write failed, buffered to memory",
			"path", r.writer.Path(), "error", err, "buffered", r.diskBuffer.Len())
		return 0, nil
	}

	// G-01-001: 写入成功后，若缓冲中有待 flush 的行，尝试写出（磁盘恢复后自动恢复）
	// flush 期间的写入错误被忽略（最坏情况是部分行丢失，符合"允许低概率数据丢失"原则）
	// C-01-001 修复：显式接收错误并记录到 stderr fallback
	if r.diskBuffer.Len() > 0 {
		r.diskBuffer.Flush(func(s string) {
			if _, err := r.writer.Write([]byte(s)); err != nil {
				stderrWarn("disk buffer flush write failed (ignored)", err)
			}
		})
	}

	r.rotateIfNeeded(n)

	return n, nil
}

// Close 关闭日志写入器
func (r *RotatingLogWriter) Close() error {
	return r.writer.Close()
}

// Path 返回当前日志文件路径
func (r *RotatingLogWriter) Path() string {
	return r.writer.Path()
}

// rotateIfNeeded 检查文件大小，超过 maxSizeMB 触发轮转
// REQ-F-010: 单文件超过 max_size_mb 触发轮转
// G-01-2 修复：优先用内存计数器判断；每 1MB 校准一次防止漂移
// bytesWritten=0 时强制 os.Stat 校准（降级路径使用）
func (r *RotatingLogWriter) rotateIfNeeded(bytesWritten int) {
	maxBytes := int64(r.maxSizeMB) * 1024 * 1024

	// 降级路径（disk_check.go 调用，bytesWritten=0）：直接 os.Stat
	if bytesWritten == 0 {
		info, err := os.Stat(r.writer.Path())
		if err != nil {
			return
		}
		r.currentSize = info.Size()
		if info.Size() >= maxBytes {
			r.rotate()
		}
		return
	}

	// 主路径：内存计数器累加
	r.currentSize += int64(bytesWritten)

	// 快速路径：计数器未达阈值，无需 os.Stat
	if r.currentSize < maxBytes {
		// 每 1MB 校准一次计数器，防止 O_APPEND 模式下其他写入者导致漂移
		// 用当前写之前的值判断是否跨越 1MB 边界
		prevSize := r.currentSize - int64(bytesWritten)
		if prevSize/int64(1024*1024) != r.currentSize/int64(1024*1024) {
			if info, err := os.Stat(r.writer.Path()); err == nil {
				r.currentSize = info.Size()
			}
		}
		return
	}

	// 慢速路径：计数器达阈值，os.Stat 校准确认后触发轮转
	info, err := os.Stat(r.writer.Path())
	if err != nil {
		return
	}
	if info.Size() >= maxBytes {
		r.rotate()
	} else {
		// 计数器漂移，重置为真实值
		r.currentSize = info.Size()
	}
}

// rotate 执行轮转：重命名 current→@ISO8601.s，同秒冲突追加序号，超过 max_files 删除最旧
// REQ-F-010: current 重命名为 @<ISO8601>.s，归档文件超过 max_files 删除最旧
func (r *RotatingLogWriter) rotate() {
	currentPath := r.writer.Path()

	// 先关闭当前文件
	// C-01-001 修复：Close 失败记录到 stderr fallback（不阻塞轮转流程，重命名会处理 fd）
	if err := r.writer.Close(); err != nil {
		stderrWarn("rotate: close old writer failed", err)
	}

	// 生成归档文件名（解决同秒冲突）
	archiveName := r.archiveName()
	fullArchivePath := r.resolveConflict(r.dirPath, archiveName)

	// 重命名 current → 归档文件
	if err := os.Rename(currentPath, fullArchivePath); err != nil {
		// 重命名失败，重新打开 current 继续写
		writer, err2 := NewLogWriter(currentPath)
		if err2 == nil {
			r.writer = writer
		}
		return
	}

	// 创建新的 current 文件
	writer, err := NewLogWriter(currentPath)
	if err != nil {
		return
	}
	r.writer = writer
	// G-01-2: 轮转后重置计数器
	r.currentSize = 0

	// 清理超出 max_files 的归档文件
	r.cleanOldArchives(r.dirPath, r.maxFiles)
}

// archiveName 生成归档文件名：@<ISO8601>.s
// REQ-F-010: 归档文件名用 ISO8601 格式，冒号替换为连字符
func (r *RotatingLogWriter) archiveName() string {
	// REQ-F-010: ISO8601 格式，去除冒号用连字符，如 @2026-07-06T15-30-00.s
	ts := time.Now().Format("2006-01-02T15-04-05")
	return fmt.Sprintf("@%s.s", ts)
}

// resolveConflict 解决同秒冲突：同一秒内多次轮转时追加序号
// REQ-F-010: 同秒内多次轮转追加序号，如 @2026-07-06T15-30-00-1.s
func (r *RotatingLogWriter) resolveConflict(dir string, baseName string) string {
	// 基础路径（不带序号）
	basePath := filepath.Join(dir, baseName)

	// 如果基础路径不存在，直接使用
	if _, err := os.Stat(basePath); os.IsNotExist(err) {
		return basePath
	}

	// 同秒冲突：追加序号 -1, -2, ...
	// baseName 格式为 @2026-07-06T15-30-00.s
	// 去掉 .s 后缀，追加 -N，再加回 .s
	ext := filepath.Ext(baseName)  // .s
	nameWithoutExt := strings.TrimSuffix(baseName, ext) // @2026-07-06T15-30-00

	seq := 1
	for {
		conflictPath := filepath.Join(dir, fmt.Sprintf("%s-%d%s", nameWithoutExt, seq, ext))
		if _, err := os.Stat(conflictPath); os.IsNotExist(err) {
			return conflictPath
		}
		seq++
	}
}

// cleanOldArchives 删除超过 maxFiles 的最旧归档文件
// REQ-F-010: 归档文件数超过 max_files 时删除最旧的
func (r *RotatingLogWriter) cleanOldArchives(dir string, maxFiles int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	// 筛选归档文件（以 @ 开头，以 .s 结尾）
	var archives []os.DirEntry
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "@") && strings.HasSuffix(name, ".s") {
			archives = append(archives, entry)
		}
	}

	// 如果归档文件数不超过 maxFiles，无需删除
	if len(archives) <= maxFiles {
		return
	}

	// 按名称排序（ISO8601 时间戳字符串排序 = 时间排序，同秒序号也在名称中递增）
	sort.Slice(archives, func(i, j int) bool {
		return archives[i].Name() < archives[j].Name()
	})

	// 删除最旧的文件，直到剩余 maxFiles 个
	toDelete := len(archives) - maxFiles
	for i := 0; i < toDelete; i++ {
		path := filepath.Join(dir, archives[i].Name())
		// C-01-001 修复：删除失败不影响主流程，仅记录到 stderr fallback
		if err := os.Remove(path); err != nil {
			stderrWarn("clean old archive remove failed (ignored)", err)
		}
	}
}

// listArchiveFiles 列出目录中的归档文件（供测试使用）
func listArchiveFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var result []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "@") && strings.HasSuffix(name, ".s") {
			result = append(result, name)
		}
	}

	sort.Strings(result)
	return result
}
