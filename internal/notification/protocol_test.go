package notification

import (
	"strings"
	"testing"
)

func TestParseNotifyLine(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		want   *ParsedNotify
		wantOK bool // true = 协议行（无论合法与否）
	}{
		{name: "info 正例", line: `::notify:: info "开始检查"`, want: &ParsedNotify{Level: NotifyInfo, Content: "开始检查"}, wantOK: true},
		{name: "success 正例", line: `::notify:: success "all good"`, want: &ParsedNotify{Level: NotifySuccess, Content: "all good"}, wantOK: true},
		{name: "warning 正例", line: `::notify:: warning "发现新版本 1.2.3"`, want: &ParsedNotify{Level: NotifyWarning, Content: "发现新版本 1.2.3"}, wantOK: true},
		{name: "error 正例", line: `::notify:: error "failed"`, want: &ParsedNotify{Level: NotifyError, Content: "failed"}, wantOK: true},
		{name: "非协议行", line: "hello world", want: nil, wantOK: false},
		{name: "未知前缀非协议", line: "::unknown:: info \"x\"", want: nil, wantOK: false},
		{name: "缺尾随空格非协议", line: "::notify::info \"x\"", want: nil, wantOK: false},
		{name: "缺引号格式错误", line: `::notify:: info noquote`, want: nil, wantOK: true},
		{name: "首字符非引号格式错误", line: `::notify:: info content"`, want: nil, wantOK: true},
		{name: "缺闭合引号格式错误", line: `::notify:: info "unclosed`, want: nil, wantOK: true},
		{name: "非法等级", line: `::notify:: hiss "bad level"`, want: nil, wantOK: true},
		{name: "空 content 正例", line: `::notify:: info ""`, want: &ParsedNotify{Level: NotifyInfo, Content: ""}, wantOK: true},
		{name: "行内双引号原样保留", line: `::notify:: info "a "b" c"`, want: &ParsedNotify{Level: NotifyInfo, Content: `a "b" c`}, wantOK: true},
		{name: "行内反斜杠原样保留", line: `::notify:: info "a\b\"c"`, want: &ParsedNotify{Level: NotifyInfo, Content: `a\b\"c`}, wantOK: true},
		{name: "尾随内容格式错误", line: `::notify:: info "x" trailing`, want: nil, wantOK: true},
		{name: "无等级格式错误", line: `::notify:: info`, want: nil, wantOK: true},
		{name: "协议行无内容格式错误", line: `::notify:: `, want: nil, wantOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseNotifyLine(tt.line)
			if ok != tt.wantOK {
				t.Fatalf("ParseNotifyLine(%q) ok = %v, want %v", tt.line, ok, tt.wantOK)
			}
			if tt.want == nil {
				if got != nil {
					t.Fatalf("ParseNotifyLine(%q) = %+v, want nil", tt.line, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("ParseNotifyLine(%q) = nil, want %+v", tt.line, tt.want)
			}
			if got.Level != tt.want.Level {
				t.Errorf("Level = %q, want %q", got.Level, tt.want.Level)
			}
			if got.Content != tt.want.Content {
				t.Errorf("Content = %q, want %q", got.Content, tt.want.Content)
			}
		})
	}
}

func TestParseNotifyLine_8KBBoundary(t *testing.T) {
	// 协议行总长 <= 8192 → 正常解析
	prefix := `::notify:: info "`
	msgLen := 8192 - len(prefix) - 1 // 减去一个闭合引号
	msg := strings.Repeat("a", msgLen)
	line := prefix + msg + `"`
	if len(line) != 8192 {
		t.Fatalf("test line length = %d, want 8192", len(line))
	}
	got, ok := ParseNotifyLine(line)
	if !ok || got == nil {
		t.Fatalf("ParseNotifyLine(exact 8192) = %+v, %v; want valid notify", got, ok)
	}
	if got.Level != NotifyInfo || got.Content != msg {
		t.Errorf("Content mismatch at boundary")
	}

	// 8193 → 超长按普通日志处理，不生成通知
	over := prefix + msg + `"x` // 使总长 8193 且缺闭合引号
	if len(over) != 8193 {
		t.Fatalf("over line length = %d, want 8193", len(over))
	}
	got2, ok2 := ParseNotifyLine(over)
	if ok2 || got2 != nil {
		t.Fatalf("ParseNotifyLine(8193) = %+v, %v; want (nil, false)", got2, ok2)
	}
}
