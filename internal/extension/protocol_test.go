package extension

import (
	"strings"
	"testing"
)

// REQ-F-017: stdout 协议解析单元测试

func TestParseLine_OrdinaryLog(t *testing.T) {
	// REQ-F-017: 无 :: 前缀 → 普通日志
	line := "hello world"
	got := ParseLine(line)
	if got.Type != LineTypeLog {
		t.Errorf("ParseLine(%q).Type = %v, want LineTypeLog", line, got.Type)
	}
	if got.Raw != line {
		t.Errorf("ParseLine(%q).Raw = %q, want %q", line, got.Raw, line)
	}
}

func TestParseLine_Progress(t *testing.T) {
	// REQ-F-017: ::progress:: 50 "half done"
	line := `::progress:: 50 "half done"`
	got := ParseLine(line)
	if got.Type != LineTypeProgress {
		t.Fatalf("ParseLine(%q).Type = %v, want LineTypeProgress", line, got.Type)
	}
	if got.Progress != 50 {
		t.Errorf("ParseLine(%q).Progress = %d, want 50", line, got.Progress)
	}
	if got.Message != "half done" {
		t.Errorf("ParseLine(%q).Message = %q, want %q", line, got.Message, "half done")
	}
	if got.Raw != line {
		t.Errorf("ParseLine(%q).Raw = %q, want %q", line, got.Raw, line)
	}
}

func TestParseLine_ResultSuccess(t *testing.T) {
	// REQ-F-017: ::result:: success "all good"
	line := `::result:: success "all good"`
	got := ParseLine(line)
	if got.Type != LineTypeResult {
		t.Fatalf("ParseLine(%q).Type = %v, want LineTypeResult", line, got.Type)
	}
	if got.ResultStatus != "success" {
		t.Errorf("ParseLine(%q).ResultStatus = %q, want %q", line, got.ResultStatus, "success")
	}
	if got.Message != "all good" {
		t.Errorf("ParseLine(%q).Message = %q, want %q", line, got.Message, "all good")
	}
}

func TestParseLine_ResultWarning(t *testing.T) {
	// REQ-F-017: ::result:: warning "disk low"
	line := `::result:: warning "disk low"`
	got := ParseLine(line)
	if got.Type != LineTypeResult {
		t.Fatalf("ParseLine(%q).Type = %v, want LineTypeResult", line, got.Type)
	}
	if got.ResultStatus != "warning" {
		t.Errorf("ParseLine(%q).ResultStatus = %q, want %q", line, got.ResultStatus, "warning")
	}
	if got.Message != "disk low" {
		t.Errorf("ParseLine(%q).Message = %q, want %q", line, got.Message, "disk low")
	}
}

func TestParseLine_ResultError(t *testing.T) {
	// REQ-F-017: ::result:: error "failed"
	line := `::result:: error "failed"`
	got := ParseLine(line)
	if got.Type != LineTypeResult {
		t.Fatalf("ParseLine(%q).Type = %v, want LineTypeResult", line, got.Type)
	}
	if got.ResultStatus != "error" {
		t.Errorf("ParseLine(%q).ResultStatus = %q, want %q", line, got.ResultStatus, "error")
	}
	if got.Message != "failed" {
		t.Errorf("ParseLine(%q).Message = %q, want %q", line, got.Message, "failed")
	}
}

func TestParseLine_ProgressNonNumeric(t *testing.T) {
	// REQ-F-017: 百分比非数字 → 按普通日志处理
	line := `::progress:: abc "not a number"`
	got := ParseLine(line)
	if got.Type != LineTypeLog {
		t.Errorf("ParseLine(%q).Type = %v, want LineTypeLog", line, got.Type)
	}
}

func TestParseLine_ProgressOutOfRange101(t *testing.T) {
	// REQ-F-017: 百分比超范围(101) → 普通日志
	line := `::progress:: 101 "over"`
	got := ParseLine(line)
	if got.Type != LineTypeLog {
		t.Errorf("ParseLine(%q).Type = %v, want LineTypeLog", line, got.Type)
	}
}

func TestParseLine_ProgressOutOfRangeNegative(t *testing.T) {
	// REQ-F-017: 百分比超范围(-1) → 普通日志
	line := `::progress:: -1 "under"`
	got := ParseLine(line)
	if got.Type != LineTypeLog {
		t.Errorf("ParseLine(%q).Type = %v, want LineTypeLog", line, got.Type)
	}
}

func TestParseLine_ResultInvalidStatus(t *testing.T) {
	// REQ-F-017: 状态非枚举值 → 按普通日志处理
	line := `::result:: unknown "bad status"`
	got := ParseLine(line)
	if got.Type != LineTypeLog {
		t.Errorf("ParseLine(%q).Type = %v, want LineTypeLog", line, got.Type)
	}
}

func TestParseLine_LineTooLong(t *testing.T) {
	// REQ-F-017: 行长超过 8KB → 截断，按普通日志处理
	longLine := strings.Repeat("x", 8193)
	got := ParseLine(longLine)
	if got.Type != LineTypeLog {
		t.Errorf("ParseLine(longLine).Type = %v, want LineTypeLog", got.Type)
	}
	if len(got.Raw) != 8192 {
		t.Errorf("ParseLine(longLine).Raw length = %d, want 8192", len(got.Raw))
	}
}

func TestParseLine_LineTooLongProgress(t *testing.T) {
	// 即使是合法 progress 指令，超过 8KB 也按普通日志处理
	longMsg := strings.Repeat("a", 8200)
	line := `::progress:: 50 "` + longMsg + `"`
	got := ParseLine(line)
	if got.Type != LineTypeLog {
		t.Errorf("ParseLine(longProgress).Type = %v, want LineTypeLog", got.Type)
	}
}

func TestParseLine_UnknownPrefix(t *testing.T) {
	// REQ-F-017: 未识别的 ::xxx:: 前缀 → 按普通日志处理
	line := `::unknown:: something`
	got := ParseLine(line)
	if got.Type != LineTypeLog {
		t.Errorf("ParseLine(%q).Type = %v, want LineTypeLog", line, got.Type)
	}
	if got.Raw != line {
		t.Errorf("ParseLine(%q).Raw = %q, want %q", line, got.Raw, line)
	}
}

func TestParseLine_ProgressBoundary0(t *testing.T) {
	// 边界值 0
	line := `::progress:: 0 "start"`
	got := ParseLine(line)
	if got.Type != LineTypeProgress {
		t.Fatalf("ParseLine(%q).Type = %v, want LineTypeProgress", line, got.Type)
	}
	if got.Progress != 0 {
		t.Errorf("ParseLine(%q).Progress = %d, want 0", line, got.Progress)
	}
}

func TestParseLine_ProgressBoundary100(t *testing.T) {
	// 边界值 100
	line := `::progress:: 100 "done"`
	got := ParseLine(line)
	if got.Type != LineTypeProgress {
		t.Fatalf("ParseLine(%q).Type = %v, want LineTypeProgress", line, got.Type)
	}
	if got.Progress != 100 {
		t.Errorf("ParseLine(%q).Progress = %d, want 100", line, got.Progress)
	}
}

func TestParseLine_EscapedQuotes(t *testing.T) {
	// REQ-F-017: 消息内双引号转义 \" → 反转义
	line := `::progress:: 50 "say \"hello\""`
	got := ParseLine(line)
	if got.Type != LineTypeProgress {
		t.Fatalf("ParseLine(%q).Type = %v, want LineTypeProgress", line, got.Type)
	}
	if got.Message != `say "hello"` {
		t.Errorf("ParseLine(%q).Message = %q, want %q", line, got.Message, `say "hello"`)
	}
}

func TestParseLine_ResultEscapedQuotes(t *testing.T) {
	// REQ-F-017: result 消息内双引号转义 \" → 反转义
	line := `::result:: error "failed with \"code 1\""`
	got := ParseLine(line)
	if got.Type != LineTypeResult {
		t.Fatalf("ParseLine(%q).Type = %v, want LineTypeResult", line, got.Type)
	}
	if got.Message != `failed with "code 1"` {
		t.Errorf("ParseLine(%q).Message = %q, want %q", line, got.Message, `failed with "code 1"`)
	}
}

func TestParseLine_ProgressEmptyMessage(t *testing.T) {
	// REQ-F-017: 消息为空 ::progress:: 50 ""
	line := `::progress:: 50 ""`
	got := ParseLine(line)
	if got.Type != LineTypeProgress {
		t.Fatalf("ParseLine(%q).Type = %v, want LineTypeProgress", line, got.Type)
	}
	if got.Progress != 50 {
		t.Errorf("ParseLine(%q).Progress = %d, want 50", line, got.Progress)
	}
	if got.Message != "" {
		t.Errorf("ParseLine(%q).Message = %q, want empty", line, got.Message)
	}
}

func TestParseLine_ResultEmptyMessage(t *testing.T) {
	// result 消息为空
	line := `::result:: success ""`
	got := ParseLine(line)
	if got.Type != LineTypeResult {
		t.Fatalf("ParseLine(%q).Type = %v, want LineTypeResult", line, got.Type)
	}
	if got.Message != "" {
		t.Errorf("ParseLine(%q).Message = %q, want empty", line, got.Message)
	}
}

func TestParseLine_ProgressNoQuote(t *testing.T) {
	// 没有引号 → 普通日志
	line := `::progress:: 50 no quote`
	got := ParseLine(line)
	if got.Type != LineTypeLog {
		t.Errorf("ParseLine(%q).Type = %v, want LineTypeLog", line, got.Type)
	}
}

func TestParseLine_ProgressUnclosedQuote(t *testing.T) {
	// 没有结束引号 → 普通日志
	line := `::progress:: 50 "unclosed`
	got := ParseLine(line)
	if got.Type != LineTypeLog {
		t.Errorf("ParseLine(%q).Type = %v, want LineTypeLog", line, got.Type)
	}
}

func TestParseLine_ResultNoQuote(t *testing.T) {
	// 没有引号 → 普通日志
	line := `::result:: success no quote`
	got := ParseLine(line)
	if got.Type != LineTypeLog {
		t.Errorf("ParseLine(%q).Type = %v, want LineTypeLog", line, got.Type)
	}
}

// --- ProtocolParser 测试 ---

func TestProtocolParser_FeedProgress(t *testing.T) {
	// REQ-F-017: ProtocolParser.Feed 累积 progress 状态
	p := NewProtocolParser()

	p.Feed(`::progress:: 30 "step 1"`)
	if p.Progress() != 30 {
		t.Errorf("Progress() = %d, want 30", p.Progress())
	}

	p.Feed(`::progress:: 60 "step 2"`)
	if p.Progress() != 60 {
		t.Errorf("Progress() = %d, want 60", p.Progress())
	}
}

func TestProtocolParser_FeedResult(t *testing.T) {
	// REQ-F-017: 多次 result 取最后一个
	p := NewProtocolParser()

	p.Feed(`::result:: success "first"`)
	r := p.Result()
	if r == nil || r.ResultStatus != "success" || r.Message != "first" {
		t.Fatalf("Result() = %v, want success/first", r)
	}

	p.Feed(`::result:: error "second"`)
	r = p.Result()
	if r == nil || r.ResultStatus != "error" || r.Message != "second" {
		t.Fatalf("Result() = %v, want error/second", r)
	}
}

func TestProtocolParser_MultipleProgressTakesLast(t *testing.T) {
	// REQ-F-017: 多次 progress 取最后一个
	p := NewProtocolParser()
	p.Feed(`::progress:: 10 "a"`)
	p.Feed(`::progress:: 50 "b"`)
	p.Feed(`::progress:: 90 "c"`)

	if p.Progress() != 90 {
		t.Errorf("Progress() = %d, want 90", p.Progress())
	}
}

func TestProtocolParser_MultipleResultTakesLast(t *testing.T) {
	// REQ-F-017: 一个 run 只能有一个 result，多次输出以最后一次为准
	p := NewProtocolParser()
	p.Feed(`::result:: success "ok"`)
	p.Feed(`::result:: warning "warn"`)
	p.Feed(`::result:: error "fail"`)

	r := p.Result()
	if r == nil {
		t.Fatal("Result() = nil, want non-nil")
	}
	if r.ResultStatus != "error" {
		t.Errorf("Result().ResultStatus = %q, want %q", r.ResultStatus, "error")
	}
	if r.Message != "fail" {
		t.Errorf("Result().Message = %q, want %q", r.Message, "fail")
	}
}

func TestProtocolParser_FeedLogDoesNotAffectState(t *testing.T) {
	// 普通日志行不影响 progress 和 result 状态
	p := NewProtocolParser()
	p.Feed(`::progress:: 50 "half"`)
	p.Feed("just a log line")
	p.Feed("another log")

	if p.Progress() != 50 {
		t.Errorf("Progress() = %d, want 50 (unchanged by log lines)", p.Progress())
	}
	if p.Result() != nil {
		t.Errorf("Result() = %v, want nil (unchanged by log lines)", p.Result())
	}
}

func TestProtocolParser_FeedInvalidProgressDoesNotAffectState(t *testing.T) {
	// 无效 progress 不更新状态
	p := NewProtocolParser()
	p.Feed(`::progress:: 50 "half"`)

	// 无效百分比
	p.Feed(`::progress:: abc "bad"`)
	if p.Progress() != 50 {
		t.Errorf("Progress() = %d, want 50 (unchanged by invalid progress)", p.Progress())
	}

	// 超范围百分比
	p.Feed(`::progress:: 200 "bad"`)
	if p.Progress() != 50 {
		t.Errorf("Progress() = %d, want 50 (unchanged by out-of-range progress)", p.Progress())
	}
}

func TestProtocolParser_FeedInvalidResultDoesNotAffectState(t *testing.T) {
	// 无效 result 不更新状态
	p := NewProtocolParser()
	p.Feed(`::result:: success "ok"`)

	p.Feed(`::result:: invalid "bad"`)
	r := p.Result()
	if r == nil || r.ResultStatus != "success" || r.Message != "ok" {
		t.Errorf("Result() = %v, want success/ok (unchanged by invalid result)", r)
	}
}

func TestProtocolParser_ResultNilInitially(t *testing.T) {
	// 初始状态 result 为 nil
	p := NewProtocolParser()
	if p.Result() != nil {
		t.Errorf("Result() = %v, want nil initially", p.Result())
	}
}

func TestProtocolParser_ProgressZeroInitially(t *testing.T) {
	// 初始状态 progress 为 0
	p := NewProtocolParser()
	if p.Progress() != 0 {
		t.Errorf("Progress() = %d, want 0 initially", p.Progress())
	}
}

func TestProtocolParser_FeedReturnsParsedLine(t *testing.T) {
	// Feed 返回的 ParsedLine 与 ParseLine 一致
	p := NewProtocolParser()

	got := p.Feed(`::progress:: 75 "three quarters"`)
	if got.Type != LineTypeProgress {
		t.Errorf("Feed().Type = %v, want LineTypeProgress", got.Type)
	}
	if got.Progress != 75 {
		t.Errorf("Feed().Progress = %d, want 75", got.Progress)
	}
	if got.Message != "three quarters" {
		t.Errorf("Feed().Message = %q, want %q", got.Message, "three quarters")
	}
}

// --- extractQuotedMessage 测试 ---

func TestExtractQuotedMessage(t *testing.T) {
	tests := []struct {
		input string
		msg   string
		ok    bool
	}{
		{`"hello"`, "hello", true},
		{`"hello world"`, "hello world", true},
		{`""`, "", true},
		{`"say \"hi\""`, `say "hi"`, true},
		{`"escaped \" quote"`, `escaped " quote`, true},
		{`"multi \"escape\" here"`, `multi "escape" here`, true},
		{`no quote`, "", false},
		{`"unclosed`, "", false},
		{`"unclosed \"`, "", false},
	}

	for _, tt := range tests {
		msg, ok := extractQuotedMessage(tt.input)
		if ok != tt.ok {
			t.Errorf("extractQuotedMessage(%q) ok = %v, want %v", tt.input, ok, tt.ok)
		}
		if msg != tt.msg {
			t.Errorf("extractQuotedMessage(%q) msg = %q, want %q", tt.input, msg, tt.msg)
		}
	}
}

// --- 边界与混合场景 ---

func TestParseLine_ExactLineLimit(t *testing.T) {
	// 刚好 8192 字节 → 正常解析
	prefix := `::progress:: 50 "`
	// 构造总长 8192 的行
	msgLen := 8192 - len(prefix) - 1 // -1 for closing quote
	msg := strings.Repeat("a", msgLen)
	line := prefix + msg + `"`

	if len(line) != 8192 {
		t.Fatalf("test line length = %d, want 8192", len(line))
	}

	got := ParseLine(line)
	if got.Type != LineTypeProgress {
		t.Fatalf("ParseLine(exact 8192).Type = %v, want LineTypeProgress", got.Type)
	}
	if got.Progress != 50 {
		t.Errorf("ParseLine(exact 8192).Progress = %d, want 50", got.Progress)
	}
}

func TestParseLine_OneOverLineLimit(t *testing.T) {
	// 8193 字节 → 截断为普通日志
	prefix := `::progress:: 50 "`
	msgLen := 8193 - len(prefix) - 1
	msg := strings.Repeat("a", msgLen)
	line := prefix + msg + `"`

	if len(line) != 8193 {
		t.Fatalf("test line length = %d, want 8193", len(line))
	}

	got := ParseLine(line)
	if got.Type != LineTypeLog {
		t.Errorf("ParseLine(8193).Type = %v, want LineTypeLog", got.Type)
	}
}

func TestParseLine_MixedSequence(t *testing.T) {
	// 混合序列：普通日志、progress、result
	lines := []string{
		"starting up",
		`::progress:: 10 "init"`,
		"some output",
		`::progress:: 50 "halfway"`,
		`::result:: success "done"`,
	}

	p := NewProtocolParser()
	for _, line := range lines {
		p.Feed(line)
	}

	if p.Progress() != 50 {
		t.Errorf("Progress() = %d, want 50", p.Progress())
	}
	r := p.Result()
	if r == nil || r.ResultStatus != "success" || r.Message != "done" {
		t.Errorf("Result() = %v, want success/done", r)
	}
}

func TestParseLine_ProgressWithExtraSpaces(t *testing.T) {
	// ::progress::  后面可能有多余空格
	line := `::progress::  50  "half done"`
	got := ParseLine(line)
	if got.Type != LineTypeProgress {
		t.Fatalf("ParseLine(%q).Type = %v, want LineTypeProgress", line, got.Type)
	}
	if got.Progress != 50 {
		t.Errorf("ParseLine(%q).Progress = %d, want 50", line, got.Progress)
	}
	if got.Message != "half done" {
		t.Errorf("ParseLine(%q).Message = %q, want %q", line, got.Message, "half done")
	}
}

func TestParseLine_ResultWithExtraSpaces(t *testing.T) {
	line := `::result::  success  "all good"`
	got := ParseLine(line)
	if got.Type != LineTypeResult {
		t.Fatalf("ParseLine(%q).Type = %v, want LineTypeResult", line, got.Type)
	}
	if got.ResultStatus != "success" {
		t.Errorf("ParseLine(%q).ResultStatus = %q, want %q", line, got.ResultStatus, "success")
	}
}
