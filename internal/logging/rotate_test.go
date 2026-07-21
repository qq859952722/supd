package logging

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- RotatingLogWriter 测试 ---

// TestRotatingLogWriter_NoRotationBelowLimit 文件未超限不触发轮转
func TestRotatingLogWriter_NoRotationBelowLimit(t *testing.T) {
	dir := t.TempDir()
	currentPath := filepath.Join(dir, "current")

	// maxSizeMB=1 (1MB)，写入少量数据不应触发轮转
	rw, err := NewRotatingLogWriter(currentPath, 1, 5)
	if err != nil {
		t.Fatalf("NewRotatingLogWriter failed: %v", err)
	}

	data := []byte("small log line\n")
	rw.Write(data)
	rw.Close()

	// current 应该存在，不应有任何归档文件
	if _, err := os.Stat(currentPath); os.IsNotExist(err) {
		t.Error("current file should exist")
	}

	archives := listArchiveFiles(dir)
	if len(archives) != 0 {
		t.Errorf("expected 0 archive files, got %d: %v", len(archives), archives)
	}
}

// TestRotatingLogWriter_RotationOnLimit 文件超限触发轮转
func TestRotatingLogWriter_RotationOnLimit(t *testing.T) {
	dir := t.TempDir()
	currentPath := filepath.Join(dir, "current")

	// maxSizeMB=1 (1MB = 1048576 bytes)，用小阈值方便测试
	// 使用 1KB 的 maxSizeMB 不现实，改用字节数操控
	// 让 maxSizeMB=1，写入超过 1MB 的数据
	rw, err := NewRotatingLogWriter(currentPath, 1, 5)
	if err != nil {
		t.Fatalf("NewRotatingLogWriter failed: %v", err)
	}

	// 写入超过 1MB 的数据
	line := strings.Repeat("a", 1024) + "\n" // 1025 bytes per line
	for i := 0; i < 1100; i++ {
		rw.Write([]byte(line))
	}
	rw.Close()

	// current 应该存在
	if _, err := os.Stat(currentPath); os.IsNotExist(err) {
		t.Error("current file should exist after rotation")
	}

	// 应该有归档文件
	archives := listArchiveFiles(dir)
	if len(archives) == 0 {
		t.Error("expected at least 1 archive file after writing >1MB")
	}

	// 归档文件名格式应为 @<ISO8601>.s
	for _, name := range archives {
		if !strings.HasPrefix(name, "@") {
			t.Errorf("archive file name should start with @, got %q", name)
		}
		if !strings.HasSuffix(name, ".s") {
			t.Errorf("archive file name should end with .s, got %q", name)
		}
	}
}

// TestRotatingLogWriter_SameSecondConflict 同秒多次轮转序号正确
func TestRotatingLogWriter_SameSecondConflict(t *testing.T) {
	dir := t.TempDir()

	// 预先创建一个同名的归档文件，模拟同秒冲突
	ts := "2026-07-06T15-30-00"
	existingArchive := filepath.Join(dir, "@"+ts+".s")
	if err := os.WriteFile(existingArchive, []byte("old"), 0644); err != nil {
		t.Fatalf("failed to create existing archive: %v", err)
	}

	currentPath := filepath.Join(dir, "current")
	rw, err := NewRotatingLogWriter(currentPath, 1, 5)
	if err != nil {
		t.Fatalf("NewRotatingLogWriter failed: %v", err)
	}

	// 验证 resolveConflict 逻辑
	// 直接测试 resolveConflict 方法
	baseName := "@" + ts + ".s"
	result := rw.resolveConflict(dir, baseName)

	expectedName := "@" + ts + "-1.s"
	if !strings.HasSuffix(result, expectedName) {
		t.Errorf("resolveConflict returned %q, expected suffix %q", result, expectedName)
	}

	// 创建序号1的文件，再测试序号2
	conflictPath1 := filepath.Join(dir, "@"+ts+"-1.s")
	if err := os.WriteFile(conflictPath1, []byte("old1"), 0644); err != nil {
		t.Fatalf("failed to create conflict file: %v", err)
	}

	result2 := rw.resolveConflict(dir, baseName)
	expectedName2 := "@" + ts + "-2.s"
	if !strings.HasSuffix(result2, expectedName2) {
		t.Errorf("resolveConflict returned %q, expected suffix %q", result2, expectedName2)
	}

	rw.Close()
}

// TestRotatingLogWriter_MaxFilesCleanup 超max_files删除最旧
func TestRotatingLogWriter_MaxFilesCleanup(t *testing.T) {
	dir := t.TempDir()

	// 手动创建归档文件模拟超出 max_files
	archives := []string{
		"@2026-07-01T00-00-00.s",
		"@2026-07-02T00-00-00.s",
		"@2026-07-03T00-00-00.s",
	}
	for _, name := range archives {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("old"), 0644); err != nil {
			t.Fatalf("failed to create archive %s: %v", name, err)
		}
	}

	currentPath := filepath.Join(dir, "current")
	rw, err := NewRotatingLogWriter(currentPath, 1, 2) // maxFiles=2
	if err != nil {
		t.Fatalf("NewRotatingLogWriter failed: %v", err)
	}
	rw.Write([]byte("init\n"))

	// 调用 cleanOldArchives，maxFiles=2，当前3个归档，应删除1个最旧的
	rw.cleanOldArchives(dir, 2)

	remaining := listArchiveFiles(dir)
	if len(remaining) != 2 {
		t.Errorf("expected 2 archives after cleanup, got %d: %v", len(remaining), remaining)
	}

	// 最旧的 @2026-07-01T00-00-00.s 应该被删除
	for _, name := range remaining {
		if name == "@2026-07-01T00-00-00.s" {
			t.Error("oldest archive should have been deleted")
		}
	}

	rw.Close()
}

// TestRotatingLogWriter_ArchiveNameFormat 归档文件名格式
func TestRotatingLogWriter_ArchiveNameFormat(t *testing.T) {
	dir := t.TempDir()
	currentPath := filepath.Join(dir, "current")

	rw, err := NewRotatingLogWriter(currentPath, 1, 5)
	if err != nil {
		t.Fatalf("NewRotatingLogWriter failed: %v", err)
	}
	defer rw.Close()

	name := rw.archiveName()

	// 格式应为 @<ISO8601>.s，如 @2026-07-06T15-30-00.s
	if !strings.HasPrefix(name, "@") {
		t.Errorf("archive name should start with @, got %q", name)
	}
	if !strings.HasSuffix(name, ".s") {
		t.Errorf("archive name should end with .s, got %q", name)
	}
	// 中间部分应该是 YYYY-MM-DDTHH-MM-SS 格式（无冒号）
	middle := strings.TrimPrefix(name, "@")
	middle = strings.TrimSuffix(middle, ".s")
	// 验证长度：2006-01-02T15-04-05 = 19 字符
	if len(middle) != 19 {
		t.Errorf("archive timestamp part length = %d, want 19; got %q", len(middle), middle)
	}
	// 验证无冒号
	if strings.Contains(middle, ":") {
		t.Errorf("archive name should not contain colons, got %q", name)
	}
}

// --- CatchAllLogger 测试 ---

// TestCatchAllLogger_WritesToBothOutputs 同时写文件和stderr
func TestCatchAllLogger_WritesToBothOutputs(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "supd.log")

	// 创建一个 buffer 替代 stderr 来捕获输出
	var stderrBuf bytes.Buffer

	logger, err := NewCatchAllLogger(logPath)
	if err != nil {
		t.Fatalf("NewCatchAllLogger failed: %v", err)
	}

	// 替换 stderr 为 buffer（仅用于验证逻辑）
	// 由于 NewCatchAllLogger 内部已绑定 os.Stderr，我们验证文件写入
	msg := []byte("test catch-all message\n")
	n, err := logger.Write(msg)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != len(msg) {
		t.Errorf("Write returned %d, want %d", n, len(msg))
	}
	logger.Close()

	// 验证文件内容
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if !bytes.Contains(content, msg) {
		t.Errorf("file content should contain %q, got %q", string(msg), string(content))
	}

	// stderrBuf 未被使用（因为 logger 内部已绑定 os.Stderr），仅验证方法签名
	_ = stderrBuf
}

// TestCatchAllLogger_AutoCreateDir 自动创建目录
func TestCatchAllLogger_AutoCreateDir(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "deep", "nested", "supd.log")

	logger, err := NewCatchAllLogger(logPath)
	if err != nil {
		t.Fatalf("NewCatchAllLogger failed: %v", err)
	}
	defer logger.Close()

	logger.Write([]byte("test\n"))

	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Error("log file should be created in nested directory")
	}
}

// TestCatchAllLogger_Path 返回正确路径
func TestCatchAllLogger_Path(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "supd.log")

	logger, err := NewCatchAllLogger(logPath)
	if err != nil {
		t.Fatalf("NewCatchAllLogger failed: %v", err)
	}
	defer logger.Close()

	if logger.Path() != logPath {
		t.Errorf("Path() = %q, want %q", logger.Path(), logPath)
	}
}

// TestInitSupdLogger 初始化 supd 自身日志
func TestInitSupdLogger(t *testing.T) {
	dir := t.TempDir()

	err := InitSupdLogger(dir)
	if err != nil {
		t.Fatalf("InitSupdLogger failed: %v", err)
	}

	if supdLogger == nil {
		t.Fatal("supdLogger should not be nil after InitSupdLogger")
	}

	// 验证日志文件路径
	expectedPath := filepath.Join(dir, "supd.log")
	if supdLogger.Path() != expectedPath {
		t.Errorf("supdLogger.Path() = %q, want %q", supdLogger.Path(), expectedPath)
	}

	// 测试 SupdLog
	SupdLog("test supd message")

	// 关闭后验证内容
	supdLogger.Close()
	supdLogger = nil

	content, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if !strings.Contains(string(content), "test supd message") {
		t.Errorf("supd.log should contain 'test supd message', got %q", string(content))
	}
	if !strings.Contains(string(content), "[info]") {
		t.Errorf("supd.log should contain [info] level, got %q", string(content))
	}
}

// TestSupdLog_NilLogger supdLogger 未初始化时不 panic
func TestSupdLog_NilLogger(t *testing.T) {
	supdLogger = nil
	// 不应 panic
	SupdLog("should not panic")
}

// --- ExtensionLogger 测试 ---

// TestExtensionLogger_ServiceLevelPrefix 服务级扩展前缀 [ext:<ext-name>]
func TestExtensionLogger_ServiceLevelPrefix(t *testing.T) {
	dir := t.TempDir()

	cfg := ExtensionLogConfig{
		ExtName:       "pre-start",
		IsServiceLevel: true,
		ServiceName:   "myservice",
		LogRootDir:    dir,
	}

	logger, err := NewExtensionLogger(cfg)
	if err != nil {
		t.Fatalf("NewExtensionLogger failed: %v", err)
	}

	logger.Write([]byte("extension output"))
	logger.Close()

	// 服务级扩展写入服务日志目录的 current 文件
	logPath := filepath.Join(dir, "services", "myservice", "current")
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile failed for %s: %v", logPath, err)
	}

	// 应包含 [ext:pre-start] 前缀
	if !strings.Contains(string(content), "[ext:pre-start]") {
		t.Errorf("service-level extension log should contain [ext:pre-start], got %q", string(content))
	}
	// 应包含原始内容
	if !strings.Contains(string(content), "extension output") {
		t.Errorf("service-level extension log should contain original message, got %q", string(content))
	}
}

// TestExtensionLogger_GlobalLevelPath 全局级扩展写入正确路径
func TestExtensionLogger_GlobalLevelPath(t *testing.T) {
	dir := t.TempDir()

	cfg := ExtensionLogConfig{
		ExtName:       "backup",
		IsServiceLevel: false,
		RunID:         "abc123",
		LogRootDir:    dir,
	}

	logger, err := NewExtensionLogger(cfg)
	if err != nil {
		t.Fatalf("NewExtensionLogger failed: %v", err)
	}

	logger.Write([]byte("global extension output"))
	logger.Close()

	// 全局扩展写入 extensions/<ext-name>/<run_id>.log
	expectedPath := filepath.Join(dir, "extensions", "backup", "abc123.log")
	content, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("ReadFile failed for %s: %v", expectedPath, err)
	}

	// 全局扩展：标准格式（无 [ext:] 前缀）
	if !strings.Contains(string(content), "global extension output") {
		t.Errorf("global extension log should contain original message, got %q", string(content))
	}
	// 不应包含 [ext:backup] 前缀（全局扩展使用标准格式）
	if strings.Contains(string(content), "[ext:backup]") {
		t.Errorf("global extension log should NOT contain [ext:] prefix, got %q", string(content))
	}
}

// TestExtensionLogger_Path 返回正确路径
func TestExtensionLogger_Path(t *testing.T) {
	dir := t.TempDir()

	// 服务级（现在也写入独立扩展日志文件，Path() 返回独立日志路径）
	svcCfg := ExtensionLogConfig{
		ExtName:       "pre-start",
		IsServiceLevel: true,
		ServiceName:   "myservice",
		RunID:         "run001",
		LogRootDir:    dir,
	}
	svcLogger, err := NewExtensionLogger(svcCfg)
	if err != nil {
		t.Fatalf("NewExtensionLogger (service) failed: %v", err)
	}
	expectedSvcPath := filepath.Join(dir, "extensions", "pre-start", "run001.log")
	if svcLogger.Path() != expectedSvcPath {
		t.Errorf("service Path() = %q, want %q", svcLogger.Path(), expectedSvcPath)
	}
	svcLogger.Close()

	// 全局级
	globalCfg := ExtensionLogConfig{
		ExtName:       "backup",
		IsServiceLevel: false,
		RunID:         "run001",
		LogRootDir:    dir,
	}
	globalLogger, err := NewExtensionLogger(globalCfg)
	if err != nil {
		t.Fatalf("NewExtensionLogger (global) failed: %v", err)
	}
	expectedGlobalPath := filepath.Join(dir, "extensions", "backup", "run001.log")
	if globalLogger.Path() != expectedGlobalPath {
		t.Errorf("global Path() = %q, want %q", globalLogger.Path(), expectedGlobalPath)
	}
	globalLogger.Close()
}

// TestExtensionLogger_RotationOnLimit 验证 ExtensionLogger 主 writer 接入轮转（G-01 修复）
// 规格 §2.2.16: 扩展运行日志上限 10MB 硬编码
// 写入 >10MB 数据后应触发轮转，生成 @<ISO8601>.s 归档文件
func TestExtensionLogger_RotationOnLimit(t *testing.T) {
	dir := t.TempDir()

	cfg := ExtensionLogConfig{
		ExtName:    "backup",
		RunID:      "rot-test",
		LogRootDir: dir,
	}
	logger, err := NewExtensionLogger(cfg)
	if err != nil {
		t.Fatalf("NewExtensionLogger failed: %v", err)
	}

	// 写入 >10MB 数据触发轮转
	// ExtensionLogger.Write 经 formatLine 添加时间戳/级别前缀，每行约 1075 字节
	// 11000 行 ≈ 11.8MB，足以触发 10MB 上限轮转
	line := strings.Repeat("a", 1024)
	for i := 0; i < 11000; i++ {
		logger.Write([]byte(line))
	}
	logger.Close()

	// 验证 current 文件存在
	extDir := filepath.Join(dir, "extensions", "backup")
	currentPath := filepath.Join(extDir, "rot-test.log")
	if _, err := os.Stat(currentPath); os.IsNotExist(err) {
		t.Error("current log file should exist after rotation")
	}

	// 验证归档文件存在（轮转已触发）
	archives := listArchiveFiles(extDir)
	if len(archives) == 0 {
		t.Error("expected at least 1 archive file after writing >10MB, got 0 (rotation not triggered)")
	}

	// 归档文件名格式应为 @<ISO8601>.s
	for _, name := range archives {
		if !strings.HasPrefix(name, "@") || !strings.HasSuffix(name, ".s") {
			t.Errorf("archive file name should be @<ISO8601>.s, got %q", name)
		}
	}
}

// TestSupdLifecycleLog supd_lifecycle 触发的扩展写入 supd.log
func TestSupdLifecycleLog(t *testing.T) {
	dir := t.TempDir()

	// 先初始化 supdLogger
	err := InitSupdLogger(dir)
	if err != nil {
		t.Fatalf("InitSupdLogger failed: %v", err)
	}

	SupdLifecycleLog("startup-hook", "supd is starting", dir)

	// 关闭后验证
	supdLogger.Close()
	supdLogger = nil

	logPath := filepath.Join(dir, "supd.log")
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if !strings.Contains(string(content), "[ext:startup-hook]") {
		t.Errorf("supd_lifecycle log should contain [ext:startup-hook], got %q", string(content))
	}
	if !strings.Contains(string(content), "supd is starting") {
		t.Errorf("supd_lifecycle log should contain original message, got %q", string(content))
	}
}

// TestSupdLifecycleLog_WithoutInit supdLogger 未初始化时直接写文件
func TestSupdLifecycleLog_WithoutInit(t *testing.T) {
	dir := t.TempDir()
	supdLogger = nil

	// 需要确保目录存在
	logDir := filepath.Join(dir, "logs")
	os.MkdirAll(logDir, 0755)

	SupdLifecycleLog("shutdown-hook", "supd is stopping", logDir)

	logPath := filepath.Join(logDir, "supd.log")
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if !strings.Contains(string(content), "[ext:shutdown-hook]") {
		t.Errorf("supd_lifecycle log should contain [ext:shutdown-hook], got %q", string(content))
	}
	if !strings.Contains(string(content), "supd is stopping") {
		t.Errorf("supd_lifecycle log should contain original message, got %q", string(content))
	}
}

// --- RotatingLogWriter 集成测试 ---

// TestRotatingLogWriter_FullRotationCycle 完整轮转周期
func TestRotatingLogWriter_FullRotationCycle(t *testing.T) {
	dir := t.TempDir()
	currentPath := filepath.Join(dir, "current")

	// maxSizeMB=1, maxFiles=3
	rw, err := NewRotatingLogWriter(currentPath, 1, 3)
	if err != nil {
		t.Fatalf("NewRotatingLogWriter failed: %v", err)
	}

	// 写入大量数据触发多次轮转
	line := strings.Repeat("x", 1024) + "\n"
	for i := 0; i < 3500; i++ {
		rw.Write([]byte(line))
	}
	rw.Close()

	// current 应该存在
	if _, err := os.Stat(currentPath); os.IsNotExist(err) {
		t.Error("current file should exist")
	}

	// 归档文件不应超过 maxFiles=3
	archives := listArchiveFiles(dir)
	if len(archives) > 3 {
		t.Errorf("expected at most 3 archive files, got %d: %v", len(archives), archives)
	}
}

// TestCleanOldArchives_NoArchivesNeeded 归档数不超过 maxFiles 不删除
func TestCleanOldArchives_NoArchivesNeeded(t *testing.T) {
	dir := t.TempDir()
	currentPath := filepath.Join(dir, "current")

	// 创建2个归档文件
	for _, name := range []string{"@2026-07-01T00-00-00.s", "@2026-07-02T00-00-00.s"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	rw, err := NewRotatingLogWriter(currentPath, 1, 5)
	if err != nil {
		t.Fatalf("NewRotatingLogWriter failed: %v", err)
	}
	rw.Write([]byte("init\n"))

	// maxFiles=5，当前只有2个归档，不应删除
	rw.cleanOldArchives(dir, 5)

	remaining := listArchiveFiles(dir)
	if len(remaining) != 2 {
		t.Errorf("expected 2 archives (no cleanup needed), got %d: %v", len(remaining), remaining)
	}

	rw.Close()
}

// TestListArchiveFiles 辅助函数测试
func TestListArchiveFiles(t *testing.T) {
	dir := t.TempDir()

	// 创建混合文件
	os.WriteFile(filepath.Join(dir, "@2026-07-01T00-00-00.s"), []byte("archive"), 0644)
	os.WriteFile(filepath.Join(dir, "@2026-07-02T00-00-00.s"), []byte("archive"), 0644)
	os.WriteFile(filepath.Join(dir, "current"), []byte("current"), 0644)
	os.WriteFile(filepath.Join(dir, "other.txt"), []byte("other"), 0644)

	archives := listArchiveFiles(dir)

	if len(archives) != 2 {
		t.Errorf("expected 2 archive files, got %d: %v", len(archives), archives)
	}

	// 应按名称排序
	if archives[0] != "@2026-07-01T00-00-00.s" {
		t.Errorf("first archive = %q, want @2026-07-01T00-00-00.s", archives[0])
	}
	if archives[1] != "@2026-07-02T00-00-00.s" {
		t.Errorf("second archive = %q, want @2026-07-02T00-00-00.s", archives[1])
	}
}
