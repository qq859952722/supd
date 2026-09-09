// Package notification 定义通知协议解析与通知接收接口。
//
// 本节点配套设计稿 §六.1 通知协议：唯一协议 `::notify:: <level> "<content>"`。
// logging 与 extension 均依赖本包（本包只依赖标准库，避免循环依赖）。
package notification

import "strings"

// NotifyLevel 通知等级（设计稿 §六.1：info/success/warning/error 四种）。
type NotifyLevel string

const (
	NotifyInfo    NotifyLevel = "info"
	NotifySuccess NotifyLevel = "success"
	NotifyWarning NotifyLevel = "warning"
	NotifyError   NotifyLevel = "error"
)

// ParsedNotify 一行 stdout 解析结果。
type ParsedNotify struct {
	Level   NotifyLevel
	Content string
}

const (
	// notifyPrefix 协议行前缀（含尾随空格）。
	notifyPrefix = "::notify:: "
	// notifyMaxLen 协议行总长上限（字节）；超过则整体按截断普通日志处理，不生成通知。
	notifyMaxLen = 8192
)

// ParseNotifyLine 解析 `::notify:: <level> "<content>"`。
//
// 返回语义：
//   - (nil, false)：非协议行（按普通日志处理，不记 warning）；
//   - (nil, true)：协议行但格式/等级非法（按普通日志处理并记 warning）；
//   - (*ParsedNotify, true)：合法通知。
//
// 解析规则（用户已确认 2026-09-09）：
//   - 行必须以 `::notify:: ` 开头（含尾随空格）；
//   - 之后是 level 词（到下一个空格），level 必须 ∈ 四值之一；
//   - content 为剩余部分的一对双引号包裹 `"<content>"`；
//   - content 所见即所得：首尾双引号之间的文本原样保留，不做任何转义还原；
//   - 首字符非双引号、或缺闭合双引号 → 格式错误；
//   - level 与 content 之外的尾随内容 → 格式错误。
//
// 协议行总长 > 8192 字节不解析，整体按截断普通日志处理，不生成通知。
func ParseNotifyLine(line string) (*ParsedNotify, bool) {
	if len(line) > notifyMaxLen {
		return nil, false
	}
	if !strings.HasPrefix(line, notifyPrefix) {
		return nil, false
	}

	rest := line[len(notifyPrefix):]

	// level 到下一个空格
	spaceIdx := strings.IndexByte(rest, ' ')
	if spaceIdx <= 0 {
		return nil, true // 无等级或等级为空 → 格式错误
	}
	level := rest[:spaceIdx]
	switch NotifyLevel(level) {
	case NotifyInfo, NotifySuccess, NotifyWarning, NotifyError:
		// 合法等级
	default:
		return nil, true // 非法等级 → 格式错误
	}

	// content 为剩余部分的一对双引号包裹；首字符必须是 "，末字符必须是闭合的 "
	contentPart := rest[spaceIdx+1:]
	if len(contentPart) < 2 || contentPart[0] != '"' || contentPart[len(contentPart)-1] != '"' {
		return nil, true // 缺引号/首字符非引号/无闭合引号/尾随内容 → 格式错误
	}

	return &ParsedNotify{
		Level:   NotifyLevel(level),
		Content: contentPart[1 : len(contentPart)-1], // 所见即所得，不经转义还原
	}, true
}

// PendingNotification 一条待处理通知，含 level/content 与来源上下文。
// 来源字段由 supd 侧填充，脚本不可覆盖。
type PendingNotification struct {
	Level        NotifyLevel
	Content      string
	ServiceName  string
	ExtensionName string
	ActionID     string
	RunID        string
	ExecutionID  string
}

// NotifySink 通知接收点。实现方以有界队列/持久化写入为底层。
// TryEnqueue 必须非阻塞：队列满时返回 false（丢弃并计数），不阻塞调用方。
type NotifySink interface {
	TryEnqueue(n PendingNotification) bool
}