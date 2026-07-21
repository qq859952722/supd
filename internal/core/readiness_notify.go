package core

import (
	"context"
	"fmt"
	"os"

	"github.com/supdorg/supd/internal/config"
)

// NotifyChecker fd_notify类型readiness检查
// REQ-F-009: 子进程通过文件描述符通知ready
// 使用os.Pipe()创建管道，写端通过cmd.ExtraFiles传递给子进程
// 子进程fd编号=3（ExtraFiles[0]），写入任意字节后关闭写端
// supd从读端读取即判定ready
type NotifyChecker struct {
	reader *os.File
	writer *os.File
}

// NewNotifyChecker 创建fd_notify checker
// 返回checker和写端fd（需由调用方设置到cmd.ExtraFiles）
// 导出以供 API StartService 路径使用（fd_notify 需在 StartProcess 前创建）
func NewNotifyChecker(cfg *config.ReadinessConfig) (*NotifyChecker, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("readiness fd_notify: create pipe: %w", err)
	}

	// 写端由调用方通过WriterFd()获取，设置到cmd.ExtraFiles传递给子进程
	// 调用方需在cmd.Start()之后调用CloseWriter()关闭supd侧写端
	return &NotifyChecker{
		reader: r,
		writer: w,
	}, nil
}

// WriterFd 返回写端的文件描述符，供设置到cmd.ExtraFiles
func (n *NotifyChecker) WriterFd() *os.File {
	return n.writer
}

// CloseWriter 在cmd.Start()之后调用，关闭supd侧的写端
// 这样子进程关闭写端后，supd读端能收到EOF
func (n *NotifyChecker) CloseWriter() error {
	if n.writer != nil {
		err := n.writer.Close()
		n.writer = nil
		return err
	}
	return nil
}

// Check 从读端读取数据，收到即ready
// REQ-F-009: 子进程写入任意字节后关闭写端，supd从读端读取即判定ready
func (n *NotifyChecker) Check(ctx context.Context) error {
	// 使用goroutine+select实现超时控制
	type result struct {
		n   int
		err error
	}
	ch := make(chan result, 1)
	go func() {
		buf := make([]byte, 1)
		nr, err := n.reader.Read(buf)
		ch <- result{nr, err}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case res := <-ch:
		if res.err != nil {
			return fmt.Errorf("readiness fd_notify: read: %w", res.err)
		}
		if res.n > 0 {
			return nil
		}
		return fmt.Errorf("readiness fd_notify: read 0 bytes")
	}
}

// Close 关闭读端
func (n *NotifyChecker) Close() error {
	if n.writer != nil {
		n.writer.Close()
		n.writer = nil
	}
	if n.reader != nil {
		err := n.reader.Close()
		n.reader = nil
		return err
	}
	return nil
}
