// Package stream 提供共享的排水安全行读取器。
//
// 只依赖标准库；logging 与 extension 均依赖本包。
package stream

import (
	"bufio"
	"io"
	"strings"
)

// TruncationMarker 超长行追加的固定尾部标记。
const TruncationMarker = "...[truncated]"

// ReadLines 持续读取行并回调 fn，永不因超长行退出。
//
// limit 为单行字节上限；超过 limit 的行保留前 limit-len(TruncationMarker)
// 字节并追加 TruncationMarker 后交 fn，剩余部分继续读取并丢弃（保持排水）。
// UTF-8 按原始字节计数，不按 rune 计数。
// EOF 时无换行的非空尾部也交 fn。超限部分继续读取丢弃。
// 返回首个非 EOF 读取错误（EOF 不算错误）。fn 回调不得 panic。
func ReadLines(r io.Reader, limit int, fn func(line string)) error {
	if limit <= len(TruncationMarker) {
		limit = len(TruncationMarker) + 1
	}

	br := bufio.NewReaderSize(r, limit)
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			emitLine(line, limit, fn)
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// emitLine 去掉行尾换行后交给 fn，必要时按 limit 截断并追加 TruncationMarker。
func emitLine(raw string, limit int, fn func(string)) {
	line := stripDelim(raw)
	if len(line) <= limit {
		fn(line)
		return
	}
	keep := limit - len(TruncationMarker)
	if keep < 0 {
		keep = 0
	}
	fn(line[:keep] + TruncationMarker)
}

// stripDelim 去除行尾 \n，并兼容 \r\n / 仅有 \r 的旧式换行。
func stripDelim(line string) string {
	line = strings.TrimSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\r")
	return line
}
