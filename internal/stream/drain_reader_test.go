package stream

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestReadLines_Normal(t *testing.T) {
	var got []string
	err := ReadLines(strings.NewReader("a\nb\nc\n"), 8192, func(l string) { got = append(got, l) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("got %#v", got)
	}
}

func TestReadLines_TrailingWithoutNewline(t *testing.T) {
	// EOF 时无换行的非空尾部也应交 fn
	var got []string
	err := ReadLines(strings.NewReader("a\nb"), 8192, func(l string) { got = append(got, l) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[1] != "b" {
		t.Fatalf("got %#v", got)
	}
}

func TestReadLines_CRLF(t *testing.T) {
	var got []string
	err := ReadLines(strings.NewReader("a\r\nb\r\n"), 8192, func(l string) { got = append(got, l) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %#v, trailing CR not stripped", got)
	}
}

func TestReadLines_OverlongLineDrainsAndKeepsReading(t *testing.T) {
	// 64KB+1 无换行不中断，且后续行仍被读到
	const limit = 8192
	long := strings.Repeat("x", 64*1024+1) // 65537 字节无换行
	input := long + "\nafter\n"
	var got []string
	err := ReadLines(strings.NewReader(input), limit, func(l string) { got = append(got, l) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d lines, want 2 (overlong drained + normal line)", len(got))
	}

	// 超长行被截断：保留 limit-len(marker) 字节 + marker
	wantLen := limit - len(TruncationMarker)
	truncated := got[0]
	if len(truncated) != limit {
		t.Errorf("truncated line length = %d, want %d (limit)", len(truncated), limit)
	}
	if !strings.HasPrefix(truncated, strings.Repeat("x", wantLen)) {
		t.Errorf("truncated line does not keep the first %d bytes", wantLen)
	}
	if !strings.HasSuffix(truncated, TruncationMarker) {
		t.Errorf("truncated line missing marker %q", TruncationMarker)
	}
	if got[1] != "after" {
		t.Errorf("second line = %q, want %q (overlong did not break drain)", got[1], "after")
	}
}

func TestReadLines_ExactLimitLine(t *testing.T) {
	// 恰好 limit 字节 → 正常交 fn，不截断
	const limit = 8192
	line := strings.Repeat("y", limit)
	var got []string
	err := ReadLines(strings.NewReader(line+"\n"), limit, func(l string) { got = append(got, l) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != line {
		t.Fatalf("exact-limit line not preserved: got %#v", got)
	}
}

func TestReadLines_ConsecutiveLongLines(t *testing.T) {
	// 连续多个超长行也要全部排水
	const limit = 8192
	long1 := strings.Repeat("a", limit+1)
	long2 := strings.Repeat("b", limit+500)
	input := long1 + "\n" + long2 + "\nend\n"
	var got []string
	err := ReadLines(strings.NewReader(input), limit, func(l string) { got = append(got, l) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d lines, want 3", len(got))
	}
	if got[2] != "end" {
		t.Errorf("last line = %q, want %q", got[2], "end")
	}
}

type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

func TestReadLines_ReadErrorReturned(t *testing.T) {
	wantErr := errors.New("boom")
	var got []string
	err := ReadLines(io.MultiReader(strings.NewReader("a\n"), errReader{wantErr}), 8192, func(l string) { got = append(got, l) })
	if !errors.Is(err, wantErr) {
		t.Fatalf("got err %v, want %v", err, wantErr)
	}
	// 错误前已读到的行仍交 fn
	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("lines before error not delivered: %#v", got)
	}
}

func TestReadLines_EmptyInput(t *testing.T) {
	var got []string
	err := ReadLines(strings.NewReader(""), 8192, func(l string) { got = append(got, l) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d lines for empty input", len(got))
	}
}
