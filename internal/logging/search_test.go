package logging

import (
	"os"
	"path/filepath"
	"testing"
)

// --- SearchLogs 测试 ---

// TestSearchLogs_Basic 基本子串搜索
func TestSearchLogs_Basic(t *testing.T) {
	dir := t.TempDir()
	svcDir := filepath.Join(dir, "myservice")
	os.MkdirAll(svcDir, 0755)

	// 写入日志文件
	currentPath := filepath.Join(svcDir, "current")
	content := "line one\nerror happened here\nline three\nall good\n"
	os.WriteFile(currentPath, []byte(content), 0644)

	params := SearchParams{
		Pattern:     "error",
		ServiceName: "myservice",
		LogDir:      dir,
		MaxLines:    DefaultMaxLines,
	}

	result, err := SearchLogs(params)
	if err != nil {
		t.Fatalf("SearchLogs failed: %v", err)
	}

	if result.TotalMatches != 1 {
		t.Errorf("TotalMatches = %d, want 1", result.TotalMatches)
	}
	if len(result.Lines) != 1 {
		t.Fatalf("len(Lines) = %d, want 1", len(result.Lines))
	}
	if result.Lines[0].Content != "error happened here" {
		t.Errorf("Content = %q, want %q", result.Lines[0].Content, "error happened here")
	}
	if result.Lines[0].LineNumber != 2 {
		t.Errorf("LineNumber = %d, want 2", result.Lines[0].LineNumber)
	}
	if result.Lines[0].FilePath != currentPath {
		t.Errorf("FilePath = %q, want %q", result.Lines[0].FilePath, currentPath)
	}
	if result.Truncated {
		t.Error("Truncated = true, want false")
	}
}

// TestSearchLogs_AllServices 不指定服务名时搜索所有服务
func TestSearchLogs_AllServices(t *testing.T) {
	dir := t.TempDir()

	// 创建两个服务目录
	for _, svc := range []string{"svc1", "svc2"} {
		svcDir := filepath.Join(dir, svc)
		os.MkdirAll(svcDir, 0755)
		currentPath := filepath.Join(svcDir, "current")
		content := "normal line\nerror in " + svc + "\n"
		os.WriteFile(currentPath, []byte(content), 0644)
	}

	params := SearchParams{
		Pattern:  "error",
		LogDir:   dir,
		MaxLines: DefaultMaxLines,
	}

	result, err := SearchLogs(params)
	if err != nil {
		t.Fatalf("SearchLogs failed: %v", err)
	}

	if result.TotalMatches != 2 {
		t.Errorf("TotalMatches = %d, want 2", result.TotalMatches)
	}
	if len(result.Lines) != 2 {
		t.Errorf("len(Lines) = %d, want 2", len(result.Lines))
	}
}

// TestSearchLogs_MaxLines 搜索结果行数上限（REQ-F-010: 1000行锁定）
func TestSearchLogs_MaxLines(t *testing.T) {
	dir := t.TempDir()
	svcDir := filepath.Join(dir, "myservice")
	os.MkdirAll(svcDir, 0755)

	// 写入包含大量匹配行的日志文件
	currentPath := filepath.Join(svcDir, "current")
	var content []byte
	for i := 0; i < 1500; i++ {
		content = append(content, []byte("error line here\n")...)
	}
	os.WriteFile(currentPath, content, 0644)

	// 使用默认 MaxLines=1000
	params := SearchParams{
		Pattern:     "error",
		ServiceName: "myservice",
		LogDir:      dir,
		MaxLines:    DefaultMaxLines,
	}

	result, err := SearchLogs(params)
	if err != nil {
		t.Fatalf("SearchLogs failed: %v", err)
	}

	if result.TotalMatches != 1500 {
		t.Errorf("TotalMatches = %d, want 1500", result.TotalMatches)
	}
	if len(result.Lines) != 1000 {
		t.Errorf("len(Lines) = %d, want 1000", len(result.Lines))
	}
	if !result.Truncated {
		t.Error("Truncated = false, want true")
	}
}

// TestSearchLogs_MaxLinesCustom 自定义最大行数
func TestSearchLogs_MaxLinesCustom(t *testing.T) {
	dir := t.TempDir()
	svcDir := filepath.Join(dir, "myservice")
	os.MkdirAll(svcDir, 0755)

	currentPath := filepath.Join(svcDir, "current")
	var content []byte
	for i := 0; i < 50; i++ {
		content = append(content, []byte("error line\n")...)
	}
	os.WriteFile(currentPath, content, 0644)

	params := SearchParams{
		Pattern:     "error",
		ServiceName: "myservice",
		LogDir:      dir,
		MaxLines:    10,
	}

	result, err := SearchLogs(params)
	if err != nil {
		t.Fatalf("SearchLogs failed: %v", err)
	}

	if result.TotalMatches != 50 {
		t.Errorf("TotalMatches = %d, want 50", result.TotalMatches)
	}
	if len(result.Lines) != 10 {
		t.Errorf("len(Lines) = %d, want 10", len(result.Lines))
	}
	if !result.Truncated {
		t.Error("Truncated = false, want true")
	}
}

// TestSearchLogs_DefaultMaxLines MaxLines=0时使用默认值1000
func TestSearchLogs_DefaultMaxLines(t *testing.T) {
	dir := t.TempDir()
	svcDir := filepath.Join(dir, "myservice")
	os.MkdirAll(svcDir, 0755)

	currentPath := filepath.Join(svcDir, "current")
	os.WriteFile(currentPath, []byte("error line\n"), 0644)

	params := SearchParams{
		Pattern:     "error",
		ServiceName: "myservice",
		LogDir:      dir,
		MaxLines:    0, // 应使用默认值1000
	}

	result, err := SearchLogs(params)
	if err != nil {
		t.Fatalf("SearchLogs failed: %v", err)
	}

	if len(result.Lines) != 1 {
		t.Errorf("len(Lines) = %d, want 1", len(result.Lines))
	}
}

// TestSearchLogs_ContextLines 上下文行
func TestSearchLogs_ContextLines(t *testing.T) {
	dir := t.TempDir()
	svcDir := filepath.Join(dir, "myservice")
	os.MkdirAll(svcDir, 0755)

	currentPath := filepath.Join(svcDir, "current")
	content := "line1\nline2\nerror here\nline4\nline5\n"
	os.WriteFile(currentPath, []byte(content), 0644)

	params := SearchParams{
		Pattern:      "error",
		ServiceName:  "myservice",
		LogDir:       dir,
		MaxLines:     DefaultMaxLines,
		ContextLines: 1,
	}

	result, err := SearchLogs(params)
	if err != nil {
		t.Fatalf("SearchLogs failed: %v", err)
	}

	if len(result.Lines) != 1 {
		t.Fatalf("len(Lines) = %d, want 1", len(result.Lines))
	}

	ctx := result.Lines[0].Context
	// 上下文1行：匹配行前1行 + 匹配行后1行
	if len(ctx) != 2 {
		t.Fatalf("len(Context) = %d, want 2", len(ctx))
	}
	if ctx[0] != "line2" {
		t.Errorf("Context[0] = %q, want %q", ctx[0], "line2")
	}
	if ctx[1] != "line4" {
		t.Errorf("Context[1] = %q, want %q", ctx[1], "line4")
	}
}

// TestSearchLogs_ContextLinesAtFileStart 文件开头匹配行的上下文
func TestSearchLogs_ContextLinesAtFileStart(t *testing.T) {
	dir := t.TempDir()
	svcDir := filepath.Join(dir, "myservice")
	os.MkdirAll(svcDir, 0755)

	currentPath := filepath.Join(svcDir, "current")
	content := "error at start\nline2\nline3\n"
	os.WriteFile(currentPath, []byte(content), 0644)

	params := SearchParams{
		Pattern:      "error",
		ServiceName:  "myservice",
		LogDir:       dir,
		MaxLines:     DefaultMaxLines,
		ContextLines: 2,
	}

	result, err := SearchLogs(params)
	if err != nil {
		t.Fatalf("SearchLogs failed: %v", err)
	}

	ctx := result.Lines[0].Context
	// 开头行：前0行 + 后2行
	if len(ctx) != 2 {
		t.Fatalf("len(Context) = %d, want 2", len(ctx))
	}
	if ctx[0] != "line2" {
		t.Errorf("Context[0] = %q, want %q", ctx[0], "line2")
	}
	if ctx[1] != "line3" {
		t.Errorf("Context[1] = %q, want %q", ctx[1], "line3")
	}
}

// TestSearchLogs_NoMatch 无匹配结果
func TestSearchLogs_NoMatch(t *testing.T) {
	dir := t.TempDir()
	svcDir := filepath.Join(dir, "myservice")
	os.MkdirAll(svcDir, 0755)

	currentPath := filepath.Join(svcDir, "current")
	os.WriteFile(currentPath, []byte("normal line\nanother line\n"), 0644)

	params := SearchParams{
		Pattern:     "nonexistent",
		ServiceName: "myservice",
		LogDir:      dir,
		MaxLines:    DefaultMaxLines,
	}

	result, err := SearchLogs(params)
	if err != nil {
		t.Fatalf("SearchLogs failed: %v", err)
	}

	if result.TotalMatches != 0 {
		t.Errorf("TotalMatches = %d, want 0", result.TotalMatches)
	}
	if len(result.Lines) != 0 {
		t.Errorf("len(Lines) = %d, want 0", len(result.Lines))
	}
}

// TestSearchLogs_NonexistentService 搜索不存在的服务
func TestSearchLogs_NonexistentService(t *testing.T) {
	dir := t.TempDir()

	params := SearchParams{
		Pattern:     "error",
		ServiceName: "nosuchservice",
		LogDir:      dir,
		MaxLines:    DefaultMaxLines,
	}

	result, err := SearchLogs(params)
	if err != nil {
		t.Fatalf("SearchLogs failed: %v", err)
	}

	if result.TotalMatches != 0 {
		t.Errorf("TotalMatches = %d, want 0", result.TotalMatches)
	}
}

// TestSearchLogs_NonexistentLogDir 日志目录不存在
func TestSearchLogs_NonexistentLogDir(t *testing.T) {
	params := SearchParams{
		Pattern:  "error",
		LogDir:   "/nonexistent/path",
		MaxLines: DefaultMaxLines,
	}

	result, err := SearchLogs(params)
	if err != nil {
		t.Fatalf("SearchLogs failed: %v", err)
	}

	if result.TotalMatches != 0 {
		t.Errorf("TotalMatches = %d, want 0", result.TotalMatches)
	}
}

// TestSearchLogs_IncludeArchives 搜索归档文件
func TestSearchLogs_IncludeArchives(t *testing.T) {
	dir := t.TempDir()
	svcDir := filepath.Join(dir, "myservice")
	os.MkdirAll(svcDir, 0755)

	// 当前文件
	currentPath := filepath.Join(svcDir, "current")
	os.WriteFile(currentPath, []byte("current error line\nnormal\n"), 0644)

	// 归档文件
	archivePath := filepath.Join(svcDir, "@2026-07-01T10-00-00.s")
	os.WriteFile(archivePath, []byte("archive error line\nnormal\n"), 0644)

	// 不搜索归档
	params := SearchParams{
		Pattern:         "error",
		ServiceName:     "myservice",
		LogDir:          dir,
		MaxLines:        DefaultMaxLines,
		IncludeArchives: false,
	}

	result, err := SearchLogs(params)
	if err != nil {
		t.Fatalf("SearchLogs failed: %v", err)
	}
	if result.TotalMatches != 1 {
		t.Errorf("Without archives: TotalMatches = %d, want 1", result.TotalMatches)
	}

	// 搜索归档
	params.IncludeArchives = true
	result, err = SearchLogs(params)
	if err != nil {
		t.Fatalf("SearchLogs failed: %v", err)
	}
	if result.TotalMatches != 2 {
		t.Errorf("With archives: TotalMatches = %d, want 2", result.TotalMatches)
	}
}

// TestSearchLogs_ArchiveOnlyOnly 无current文件，仅归档
func TestSearchLogs_ArchiveOnlyOnly(t *testing.T) {
	dir := t.TempDir()
	svcDir := filepath.Join(dir, "myservice")
	os.MkdirAll(svcDir, 0755)

	// 仅归档文件，无 current
	archivePath := filepath.Join(svcDir, "@2026-07-01T10-00-00.s")
	os.WriteFile(archivePath, []byte("archive error line\n"), 0644)

	params := SearchParams{
		Pattern:         "error",
		ServiceName:     "myservice",
		LogDir:          dir,
		MaxLines:        DefaultMaxLines,
		IncludeArchives: true,
	}

	result, err := SearchLogs(params)
	if err != nil {
		t.Fatalf("SearchLogs failed: %v", err)
	}
	if result.TotalMatches != 1 {
		t.Errorf("TotalMatches = %d, want 1", result.TotalMatches)
	}
}

// TestSearchLogs_MultipleMatchesInFile 单文件多匹配行
func TestSearchLogs_MultipleMatchesInFile(t *testing.T) {
	dir := t.TempDir()
	svcDir := filepath.Join(dir, "myservice")
	os.MkdirAll(svcDir, 0755)

	currentPath := filepath.Join(svcDir, "current")
	content := "error one\nnormal\nerror two\nnormal\nerror three\n"
	os.WriteFile(currentPath, []byte(content), 0644)

	params := SearchParams{
		Pattern:     "error",
		ServiceName: "myservice",
		LogDir:      dir,
		MaxLines:    DefaultMaxLines,
	}

	result, err := SearchLogs(params)
	if err != nil {
		t.Fatalf("SearchLogs failed: %v", err)
	}

	if result.TotalMatches != 3 {
		t.Errorf("TotalMatches = %d, want 3", result.TotalMatches)
	}
	if len(result.Lines) != 3 {
		t.Fatalf("len(Lines) = %d, want 3", len(result.Lines))
	}
	// 验证行号
	if result.Lines[0].LineNumber != 1 {
		t.Errorf("Lines[0].LineNumber = %d, want 1", result.Lines[0].LineNumber)
	}
	if result.Lines[1].LineNumber != 3 {
		t.Errorf("Lines[1].LineNumber = %d, want 3", result.Lines[1].LineNumber)
	}
	if result.Lines[2].LineNumber != 5 {
		t.Errorf("Lines[2].LineNumber = %d, want 5", result.Lines[2].LineNumber)
	}
}

// TestSearchLogs_SubstringMatch 简单子串匹配（非正则）
func TestSearchLogs_SubstringMatch(t *testing.T) {
	dir := t.TempDir()
	svcDir := filepath.Join(dir, "myservice")
	os.MkdirAll(svcDir, 0755)

	currentPath := filepath.Join(svcDir, "current")
	content := "special chars *.[]? here\nno match\n"
	os.WriteFile(currentPath, []byte(content), 0644)

	params := SearchParams{
		Pattern:     "*.[]?", // 纯子串匹配，不作为正则
		ServiceName: "myservice",
		LogDir:      dir,
		MaxLines:    DefaultMaxLines,
	}

	result, err := SearchLogs(params)
	if err != nil {
		t.Fatalf("SearchLogs failed: %v", err)
	}

	if result.TotalMatches != 1 {
		t.Errorf("TotalMatches = %d, want 1", result.TotalMatches)
	}
}
