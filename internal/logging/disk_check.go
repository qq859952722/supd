package logging

import (
	"os"
	"syscall"
)

// diskFullBufferCapacity 磁盘满时内存缓冲容量
// REQ-E-008: 磁盘满→内存缓冲1000行
const diskFullBufferCapacity = 1000

// DiskFullBuffer 磁盘满时的内存缓冲
// REQ-E-008: 磁盘满→内存缓冲1000行+跳过轮转
type DiskFullBuffer struct {
	lines   []string
	dropped int
}

// NewDiskFullBuffer 创建磁盘满内存缓冲
func NewDiskFullBuffer() *DiskFullBuffer {
	return &DiskFullBuffer{
		lines: make([]string, 0, diskFullBufferCapacity),
	}
}

// Add 添加日志行，超出容量丢弃最旧的
// REQ-E-008: 缓冲满时丢弃最旧行
func (b *DiskFullBuffer) Add(line string) {
	if len(b.lines) >= diskFullBufferCapacity {
		// 丢弃最旧的50%以减少频繁移动
		half := diskFullBufferCapacity / 2
		copy(b.lines, b.lines[half:])
		b.lines = b.lines[:diskFullBufferCapacity-half]
		b.dropped += half
	}
	b.lines = append(b.lines, line)
}

// Flush 磁盘恢复后写出缓冲内容
// REQ-E-008: 磁盘恢复后将缓冲内容写出
func (b *DiskFullBuffer) Flush(writer func(string)) int {
	flushed := len(b.lines)
	for _, line := range b.lines {
		writer(line)
	}
	b.lines = b.lines[:0]
	return flushed
}

// Dropped 返回因缓冲满而丢弃的行数
func (b *DiskFullBuffer) Dropped() int {
	return b.dropped
}

// Len 返回当前缓冲行数
func (b *DiskFullBuffer) Len() int {
	return len(b.lines)
}

// IsDiskFull 检查磁盘是否满（通过尝试获取磁盘使用情况）
// REQ-E-008: 检测磁盘满状态
func IsDiskFull(path string) bool {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		// 无法获取磁盘信息，假设磁盘满以触发缓冲
		return true
	}
	// 可用空间小于1MB视为磁盘满
	available := stat.Bavail * uint64(stat.Bsize)
	const oneMB = 1 * 1024 * 1024
	return available < oneMB
}

// TryWriteWithFallback 尝试写入日志，磁盘满时缓冲到内存
// REQ-E-008: 磁盘满→内存缓冲+跳过轮转
func TryWriteWithFallback(writer *LogWriter, rotator *RotatingLogWriter, buffer *DiskFullBuffer, line []byte) error {
	// 检查磁盘是否满
	dirPath := "/"
	if rotator != nil {
		dirPath = rotator.dirPath
	}

	if IsDiskFull(dirPath) {
		// 磁盘满：缓冲到内存，跳过轮转
		buffer.Add(string(line))
		return nil
	}

	// 磁盘正常：先flush缓冲
	if buffer.Len() > 0 {
		buffer.Flush(func(s string) {
			// C-01-001 修复：flush 回调中写入失败仅记录到 stderr fallback
			if _, err := writer.Write([]byte(s)); err != nil {
				stderrWarn("disk check buffer flush write failed (ignored)", err)
			}
		})
	}

	// 写入当前行
	_, err := writer.Write(line)
	if err != nil {
		// 写入失败（可能磁盘满），缓冲到内存
		buffer.Add(string(line))
		// 检查是否磁盘满
		if IsDiskFull(dirPath) {
			return nil // 静默缓冲
		}
		return err
	}

	// 写入成功后尝试轮转（跳过如果磁盘满）
	if rotator != nil && !IsDiskFull(dirPath) {
		// G-01-2: rotateIfNeeded 已改为接受字节数参数；此处为降级路径，
		// 传 0 触发 os.Stat 校准（与旧行为等价）
		rotator.rotateIfNeeded(0)
	}

	return nil
}

// CanWriteToDir 检查目录是否可写
// REQ-E-009: user.Lookup失败时的明确错误信息辅助
func CanWriteToDir(dir string) bool {
	// 尝试创建临时文件来检查写权限
	tmpFile, err := os.CreateTemp(dir, ".supd_check_*")
	if err != nil {
		return false
	}
	// C-01-001 修复：Close/Remove 错误不影响判断结果（文件已创建即说明可写）
	if cerr := tmpFile.Close(); cerr != nil {
		stderrWarn("CanWriteToDir tmpfile close failed (ignored)", cerr)
	}
	if rerr := os.Remove(tmpFile.Name()); rerr != nil {
		stderrWarn("CanWriteToDir tmpfile remove failed (ignored)", rerr)
	}
	return true
}
