package logging

import (
	"bufio"
	"os"
	"path/filepath"
)

// TailReader 实时日志流读取器
// REQ-F-010: 实时日志流：tail当前current文件，长轮询方式
type TailReader struct {
	filePath string // 监听的文件路径
	pos      int64  // 当前读取位置
}

// NewTailReader 创建实时日志流读取器，从指定位置开始tail
// REQ-F-010: tail当前current文件
func NewTailReader(filePath string, pos int64) *TailReader {
	return &TailReader{
		filePath: filePath,
		pos:      pos,
	}
}

// ReadNewLines 读取从pos位置开始的新行
// REQ-F-010: 长轮询方式读取新行
func (t *TailReader) ReadNewLines() ([]string, error) {
	f, err := os.Open(t.filePath)
	if err != nil {
		return nil, err
	}
	// C-01-001 修复：读取场景 Close 错误无影响（数据已读完），但显式接收并记录
	defer func() {
		if cerr := f.Close(); cerr != nil {
			stderrWarn("tail reader close failed (ignored)", cerr)
		}
	}()

	// 跳到上次读取的位置
	if t.pos > 0 {
		if _, err := f.Seek(t.pos, 0); err != nil {
			return nil, err
		}
	}

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return lines, err
	}

	// 更新位置到文件末尾
	newPos, err := f.Seek(0, 2)
	if err != nil {
		return lines, err
	}
	t.pos = newPos

	return lines, nil
}

// CurrentPos 返回当前读取位置
func (t *TailReader) CurrentPos() int64 {
	return t.pos
}

// TailServiceLogs tail服务日志的便捷函数
// REQ-F-010: tail当前current文件，长轮询方式
// sincePos=0 表示从头读，>0 表示从该位置开始读
// 返回新行列表和新位置
func TailServiceLogs(svcName, logDir string, sincePos int64) ([]string, int64, error) {
	// 构建日志文件路径：logDir/svcName/current
	filePath := filepath.Join(logDir, svcName, "current")

	reader := NewTailReader(filePath, sincePos)
	lines, err := reader.ReadNewLines()
	if err != nil {
		if os.IsNotExist(err) {
			// 文件不存在，返回空结果
			return nil, sincePos, nil
		}
		return nil, sincePos, err
	}

	return lines, reader.CurrentPos(), nil
}
