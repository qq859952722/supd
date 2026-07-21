package extension

import (
	"strconv"
	"strings"
)

// REQ-F-017, 2.2.6: stdout 协议解析
// 扩展通过 stdout 输出约定格式的行，supd 实时解析
// stderr 全部按普通日志处理，不解析协议（由调用方保证）

const (
	// maxLineLen 行长超过 8KB 则截断按普通日志处理
	// REQ-F-017: 行长超过 8KB → 截断，按普通日志处理
	maxLineLen = 8192

	// progressPrefix progress 指令前缀
	progressPrefix = "::progress::"
	// resultPrefix result 指令前缀
	resultPrefix = "::result::"
)

// LineType 协议行类型
// REQ-F-017: stdout 输出行类型分为普通日志、progress 指令、result 指令
type LineType int

const (
	// LineTypeLog 普通日志行
	LineTypeLog LineType = iota
	// LineTypeProgress ::progress:: 指令
	LineTypeProgress
	// LineTypeResult ::result:: 指令
	LineTypeResult
)

// ParsedLine 解析后的行
// REQ-F-017: 协议解析结果
type ParsedLine struct {
	// Type 行类型
	Type LineType
	// Progress ::progress:: 的百分比 0-100
	Progress int
	// ResultStatus ::result:: 的状态 success/warning/error
	ResultStatus string
	// Message 消息内容
	Message string
	// Raw 原始行内容
	Raw string
}

// ParseLine 解析单行 stdout 输出
// REQ-F-017: 实时解析扩展 stdout 输出的约定格式行
func ParseLine(line string) ParsedLine {
	// REQ-F-017: 行长超过 8KB → 截断，按普通日志处理
	if len(line) > maxLineLen {
		return ParsedLine{
			Type: LineTypeLog,
			Raw:  line[:maxLineLen],
		}
	}

	// REQ-F-017: ::progress:: <0-100> "<message>"
	if strings.HasPrefix(line, progressPrefix) {
		return parseProgressLine(line)
	}

	// REQ-F-017: ::result:: <success|warning|error> "<message>"
	if strings.HasPrefix(line, resultPrefix) {
		return parseResultLine(line)
	}

	// REQ-F-017: 未识别的 ::xxx:: 前缀 → 按普通日志处理
	// 无 :: 前缀 → 按普通日志处理
	return ParsedLine{
		Type: LineTypeLog,
		Raw:  line,
	}
}

// parseProgressLine 解析 ::progress:: 指令行
// REQ-F-017: 格式 ::progress:: <0-100> "<message>"
func parseProgressLine(line string) ParsedLine {
	// 去掉前缀，剩余部分形如:  50 "half done"
	rest := strings.TrimPrefix(line, progressPrefix)

	// 找到第一个引号的位置，前面是百分比，后面是消息
	quoteIdx := strings.IndexByte(rest, '"')
	if quoteIdx < 0 {
		// REQ-F-017: 格式不合法 → 普通日志
		return ParsedLine{Type: LineTypeLog, Raw: line}
	}

	// 提取百分比部分（引号前的内容，去掉前后空白）
	numPart := strings.TrimSpace(rest[:quoteIdx])
	progress, err := strconv.Atoi(numPart)
	if err != nil {
		// REQ-F-017: 百分比非数字 → 按普通日志处理
		return ParsedLine{Type: LineTypeLog, Raw: line}
	}

	// REQ-F-017: 百分比不在 0-100 范围 → 普通日志
	if progress < 0 || progress > 100 {
		return ParsedLine{Type: LineTypeLog, Raw: line}
	}

	// 提取引号内消息
	msg, ok := extractQuotedMessage(rest[quoteIdx:])
	if !ok {
		return ParsedLine{Type: LineTypeLog, Raw: line}
	}

	return ParsedLine{
		Type:     LineTypeProgress,
		Progress: progress,
		Message:  msg,
		Raw:      line,
	}
}

// parseResultLine 解析 ::result:: 指令行
// REQ-F-017: 格式 ::result:: <success|warning|error> "<message>"
func parseResultLine(line string) ParsedLine {
	// 去掉前缀，剩余部分形如:  success "all good"
	rest := strings.TrimPrefix(line, resultPrefix)

	// 找到第一个引号的位置，前面是状态，后面是消息
	quoteIdx := strings.IndexByte(rest, '"')
	if quoteIdx < 0 {
		return ParsedLine{Type: LineTypeLog, Raw: line}
	}

	// 提取状态部分（引号前的内容，去掉前后空白）
	statusPart := strings.TrimSpace(rest[:quoteIdx])

	// REQ-F-017: 状态非 success/warning/error → 按普通日志处理
	if statusPart != "success" && statusPart != "warning" && statusPart != "error" {
		return ParsedLine{Type: LineTypeLog, Raw: line}
	}

	// 提取引号内消息
	msg, ok := extractQuotedMessage(rest[quoteIdx:])
	if !ok {
		return ParsedLine{Type: LineTypeLog, Raw: line}
	}

	return ParsedLine{
		Type:         LineTypeResult,
		ResultStatus: statusPart,
		Message:      msg,
		Raw:          line,
	}
}

// extractQuotedMessage 提取双引号包裹的消息，处理 \" 转义
// REQ-F-017: 消息内双引号需转义为 \"，解析时反转义
// 输入 s 应以 " 开头，如 "half done" 或 "say \"hello\""
func extractQuotedMessage(s string) (string, bool) {
	if len(s) == 0 || s[0] != '"' {
		return "", false
	}

	// 查找结束引号，同时处理 \" 转义
	var sb strings.Builder
	i := 1 // 跳过开头的 "
	for i < len(s) {
		if s[i] == '\\' {
			// 转义字符
			if i+1 < len(s) && s[i+1] == '"' {
				// \" → "
				sb.WriteByte('"')
				i += 2
				continue
			}
			// 其他反斜杠保留原样
			sb.WriteByte(s[i])
			i++
			continue
		}
		if s[i] == '"' {
			// 结束引号
			return sb.String(), true
		}
		sb.WriteByte(s[i])
		i++
	}

	// 没有找到结束引号
	return "", false
}

// ProtocolParser 流式协议解析器
// REQ-F-017: 一个 run 只能有一个 result，多次输出以最后一次为准
type ProtocolParser struct {
	progress int        // 最新 progress 值
	result   *ParsedLine // 最新 result（多次输出以最后一次为准）
}

// NewProtocolParser 创建流式协议解析器
// REQ-F-017: 初始化协议解析器
func NewProtocolParser() *ProtocolParser {
	return &ProtocolParser{}
}

// Feed 喂入一行 stdout 输出，返回解析结果，更新内部状态
// REQ-F-017: 实时解析扩展 stdout，维护最新 progress 和 result 状态
func (p *ProtocolParser) Feed(line string) ParsedLine {
	parsed := ParseLine(line)

	switch parsed.Type {
	case LineTypeProgress:
		p.progress = parsed.Progress
	case LineTypeResult:
		// REQ-F-017: 多次输出 result 以最后一次为准
		p.result = &parsed
	}

	return parsed
}

// Progress 返回最新 progress 值
// REQ-F-017: 获取当前进度
func (p *ProtocolParser) Progress() int {
	return p.progress
}

// Result 返回最新 result（可能 nil）
// REQ-F-017: 获取最新 result 指令，多次输出以最后一次为准
func (p *ProtocolParser) Result() *ParsedLine {
	return p.result
}
