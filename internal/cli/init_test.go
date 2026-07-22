package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCreateDefaultConfig_NewFile 测试 createDefaultConfig 创建新文件
func TestCreateDefaultConfig_NewFile(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	origForce := initForce
	origDryRun := initDryRun
	defer func() {
		initForce = origForce
		initDryRun = origDryRun
	}()
	initForce = false
	initDryRun = false

	if err := createDefaultConfig(cfgPath); err != nil {
		t.Fatalf("createDefaultConfig 失败: %v", err)
	}

	// 验证文件存在
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("文件未创建: %v", err)
	}
	// 验证权限 0600
	if info.Mode().Perm() != 0600 {
		t.Errorf("文件权限 = %v, want 0600", info.Mode().Perm())
	}

	// 验证内容
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	content := string(data)

	// 验证关键字段都存在
	requiredFields := []string{
		"settings:",
		"http_listen:",
		"auth_mode:",
		"auth_token:",
		"log_level:",
		"log_max_size_mb:",
		"log_max_files:",
		"shutdown_grace_seconds:",
		"extension_default_timeout_seconds:",
		"extension_hard_limit_seconds:",
		"run_history_retention_seconds:",
		"file_history_versions:",
		"max_upload_size_mb:",
		"env_files:",
		"extension_dirs:",
		"runtimes:",
		"defaults:",
		"restart:",
		"backoff_ms:",
		"max_backoff_ms:",
		"multiplier:",
		"max_retries:",
		"reset_after_seconds:",
	}
	for _, field := range requiredFields {
		if !strings.Contains(content, field) {
			t.Errorf("config.yaml 缺少字段: %s", field)
		}
	}

	// 验证 auth_token 是 64 字符 hex
	token := extractAuthToken(content)
	if len(token) != 64 {
		t.Errorf("auth_token 长度 = %d, want 64", len(token))
	}
	for _, c := range token {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("auth_token 包含非 hex 字符: %c", c)
			break
		}
	}
}

// TestCreateDefaultConfig_FileExistsNoForce 测试文件已存在且无 --force 时跳过
func TestCreateDefaultConfig_FileExistsNoForce(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	// 预先创建文件
	originalContent := "original content"
	if err := os.WriteFile(cfgPath, []byte(originalContent), 0644); err != nil {
		t.Fatalf("write original: %v", err)
	}

	origForce := initForce
	origDryRun := initDryRun
	defer func() {
		initForce = origForce
		initDryRun = origDryRun
	}()
	initForce = false
	initDryRun = false

	if err := createDefaultConfig(cfgPath); err != nil {
		t.Fatalf("createDefaultConfig 失败: %v", err)
	}

	// 验证文件未被覆盖
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	if string(data) != originalContent {
		t.Errorf("文件被覆盖: got %q, want %q", string(data), originalContent)
	}
}

// TestCreateDefaultConfig_FileExistsWithForce 测试 --force 覆盖已存在文件
func TestCreateDefaultConfig_FileExistsWithForce(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	// 预先创建文件
	originalContent := "original content"
	if err := os.WriteFile(cfgPath, []byte(originalContent), 0644); err != nil {
		t.Fatalf("write original: %v", err)
	}

	origForce := initForce
	origDryRun := initDryRun
	defer func() {
		initForce = origForce
		initDryRun = origDryRun
	}()
	initForce = true
	initDryRun = false

	if err := createDefaultConfig(cfgPath); err != nil {
		t.Fatalf("createDefaultConfig 失败: %v", err)
	}

	// 验证文件已被覆盖
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	if string(data) == originalContent {
		t.Errorf("文件未被覆盖")
	}
	if !strings.Contains(string(data), "auth_token:") {
		t.Errorf("覆盖后文件应包含 auth_token 字段")
	}
}

// TestCreateDefaultConfig_DryRun 测试 dry-run 模式不创建文件
func TestCreateDefaultConfig_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	origForce := initForce
	origDryRun := initDryRun
	defer func() {
		initForce = origForce
		initDryRun = origDryRun
	}()
	initForce = false
	initDryRun = true

	if err := createDefaultConfig(cfgPath); err != nil {
		t.Fatalf("createDefaultConfig dry-run 失败: %v", err)
	}

	// 验证文件未创建
	if _, err := os.Stat(cfgPath); err == nil {
		t.Errorf("dry-run 模式不应创建文件")
	}
}

// TestCreateDefaultEnv_NewFile 测试 createDefaultEnv 创建新文件
func TestCreateDefaultEnv_NewFile(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, "env", "00-base.yaml")
	if err := os.MkdirAll(filepath.Dir(envPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	origForce := initForce
	origDryRun := initDryRun
	defer func() {
		initForce = origForce
		initDryRun = origDryRun
	}()
	initForce = false
	initDryRun = false

	if err := createDefaultEnv(envPath); err != nil {
		t.Fatalf("createDefaultEnv 失败: %v", err)
	}

	// 验证文件存在
	info, err := os.Stat(envPath)
	if err != nil {
		t.Fatalf("文件未创建: %v", err)
	}
	// 验证权限 0644
	if info.Mode().Perm() != 0644 {
		t.Errorf("文件权限 = %v, want 0644", info.Mode().Perm())
	}

	// 验证内容
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "supd") {
		t.Errorf("env 文件内容应包含 'supd'，got %s", content)
	}
}

// TestCreateDefaultEnv_FileExistsNoForce 测试 env 文件已存在且无 --force 时跳过
func TestCreateDefaultEnv_FileExistsNoForce(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, "00-base.yaml")

	// 预先创建文件
	originalContent := "original env content"
	if err := os.WriteFile(envPath, []byte(originalContent), 0644); err != nil {
		t.Fatalf("write original: %v", err)
	}

	origForce := initForce
	origDryRun := initDryRun
	defer func() {
		initForce = origForce
		initDryRun = origDryRun
	}()
	initForce = false
	initDryRun = false

	if err := createDefaultEnv(envPath); err != nil {
		t.Fatalf("createDefaultEnv 失败: %v", err)
	}

	// 验证文件未被覆盖
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	if string(data) != originalContent {
		t.Errorf("文件被覆盖: got %q, want %q", string(data), originalContent)
	}
}

// TestCreateDefaultEnv_DryRun 测试 dry-run 模式不创建 env 文件
func TestCreateDefaultEnv_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, "00-base.yaml")

	origForce := initForce
	origDryRun := initDryRun
	defer func() {
		initForce = origForce
		initDryRun = origDryRun
	}()
	initForce = false
	initDryRun = true

	if err := createDefaultEnv(envPath); err != nil {
		t.Fatalf("createDefaultEnv dry-run 失败: %v", err)
	}

	// 验证文件未创建
	if _, err := os.Stat(envPath); err == nil {
		t.Errorf("dry-run 模式不应创建文件")
	}
}

// TestRunInit_CreatesAllDirectories 测试 runInit 创建所有预期目录
func TestRunInit_CreatesAllDirectories(t *testing.T) {
	tmpDir := t.TempDir()

	origWorkDir := workDir
	origForce := initForce
	origDryRun := initDryRun
	defer func() {
		workDir = origWorkDir
		initForce = origForce
		initDryRun = origDryRun
	}()
	workDir = tmpDir
	initForce = false
	initDryRun = false

	if err := runInit(nil, []string{}); err != nil {
		t.Fatalf("runInit 失败: %v", err)
	}

	expectedDirs := []string{
		"env",
		"extensions",
		"runtimes",
		"assets/certs",
		"assets/templates",
		"script_tmp",
		"services",
	}
	for _, d := range expectedDirs {
		path := filepath.Join(tmpDir, d)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("目录未创建: %s: %v", d, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("路径不是目录: %s", d)
		}
		// 验证权限 0755
		if info.Mode().Perm() != 0755 {
			t.Errorf("目录 %s 权限 = %v, want 0755", d, info.Mode().Perm())
		}
	}

	// 验证文件
	expectedFiles := []string{
		"config.yaml",
		"env/00-base.yaml",
	}
	for _, f := range expectedFiles {
		path := filepath.Join(tmpDir, f)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("文件未创建: %s: %v", f, err)
		}
	}
}

// TestRunInit_DryRunNoSideEffects 测试 dry-run 模式无副作用
func TestRunInit_DryRunNoSideEffects(t *testing.T) {
	tmpDir := t.TempDir()

	origWorkDir := workDir
	origForce := initForce
	origDryRun := initDryRun
	defer func() {
		workDir = origWorkDir
		initForce = origForce
		initDryRun = origDryRun
	}()
	workDir = tmpDir
	initForce = false
	initDryRun = true

	if err := runInit(nil, []string{}); err != nil {
		t.Fatalf("runInit dry-run 失败: %v", err)
	}

	// 验证目录均未创建
	dirs := []string{"env", "extensions", "runtimes", "services", "assets"}
	for _, d := range dirs {
		path := filepath.Join(tmpDir, d)
		if _, err := os.Stat(path); err == nil {
			t.Errorf("dry-run 模式不应创建目录: %s", d)
		}
	}

	// 验证 config.yaml 未创建
	if _, err := os.Stat(filepath.Join(tmpDir, "config.yaml")); err == nil {
		t.Errorf("dry-run 模式不应创建 config.yaml")
	}
}

// TestRunInit_Idempotent 测试无 --force 时重复调用是幂等的（不覆盖现有文件）
func TestRunInit_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()

	origWorkDir := workDir
	origForce := initForce
	origDryRun := initDryRun
	defer func() {
		workDir = origWorkDir
		initForce = origForce
		initDryRun = origDryRun
	}()
	workDir = tmpDir
	initForce = false
	initDryRun = false

	// 第一次 init
	if err := runInit(nil, []string{}); err != nil {
		t.Fatalf("第一次 runInit 失败: %v", err)
	}

	cfgPath := filepath.Join(tmpDir, "config.yaml")
	data1, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("读取 config.yaml 失败: %v", err)
	}
	token1 := extractAuthToken(string(data1))

	// 第二次 init（无 --force）
	if err := runInit(nil, []string{}); err != nil {
		t.Fatalf("第二次 runInit 失败: %v", err)
	}

	data2, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("读取 config.yaml 失败: %v", err)
	}
	token2 := extractAuthToken(string(data2))

	if token1 != token2 {
		t.Errorf("无 --force 时，auth_token 不应被覆盖: %q vs %q", token1, token2)
	}
}

// TestGenerateAuthToken_LengthAndHex 测试 auth_token 长度和 hex 格式
func TestGenerateAuthToken_LengthAndHex(t *testing.T) {
	token, err := generateAuthToken()
	if err != nil {
		t.Fatalf("generateAuthToken 失败: %v", err)
	}
	if len(token) != 64 {
		t.Errorf("token 长度 = %d, want 64", len(token))
	}
	for _, c := range token {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("token 包含非 hex 字符: %c", c)
			break
		}
	}
}

// TestGenerateAuthToken_Uniqueness 测试多次生成的 token 互不相同
func TestGenerateAuthToken_Uniqueness(t *testing.T) {
	const count = 10
	tokens := make(map[string]bool, count)
	for i := 0; i < count; i++ {
		token, err := generateAuthToken()
		if err != nil {
			t.Fatalf("第 %d 次 generateAuthToken 失败: %v", i, err)
		}
		if tokens[token] {
			t.Errorf("第 %d 次生成的 token 与之前重复: %s", i, token)
		}
		tokens[token] = true
	}
	if len(tokens) != count {
		t.Errorf("生成 %d 个 token，去重后 %d 个", count, len(tokens))
	}
}

// TestRunInit_DropbearSshServiceFiles 验证 dropbear-ssh 服务生成 run.sh + env.yaml
// 且不再生成 setup-ssh-keys 扩展（公钥配置已移至服务 env.yaml + run.sh）
func TestRunInit_DropbearSshServiceFiles(t *testing.T) {
	tmpDir := t.TempDir()

	origWorkDir := workDir
	origForce := initForce
	origDryRun := initDryRun
	defer func() {
		workDir = origWorkDir
		initForce = origForce
		initDryRun = origDryRun
	}()
	workDir = tmpDir
	initForce = true // 强制覆盖，确保生成最新模板
	initDryRun = false

	if err := runInit(nil, []string{}); err != nil {
		t.Fatalf("runInit 失败: %v", err)
	}

	// dropbear-ssh 服务应生成 3 个文件：service.yaml + run.sh + env.yaml
	expectedFiles := []string{
		"services/dropbear-ssh/service.yaml",
		"services/dropbear-ssh/run.sh",
		"services/dropbear-ssh/env.yaml",
	}
	for _, f := range expectedFiles {
		path := filepath.Join(tmpDir, f)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("dropbear-ssh 文件未创建: %s: %v", f, err)
			continue
		}
		// run.sh 应有执行权限
		if f == "run.sh" && info.Mode().Perm()&0100 == 0 {
			t.Errorf("dropbear-ssh/run.sh 应有执行权限，got %v", info.Mode().Perm())
		}
	}

	// service.yaml 应包含 autostart: false（默认不自动启动）
	svcYaml, err := os.ReadFile(filepath.Join(tmpDir, "services/dropbear-ssh/service.yaml"))
	if err != nil {
		t.Fatalf("读取 service.yaml 失败: %v", err)
	}
	if !strings.Contains(string(svcYaml), "autostart: false") {
		t.Errorf("dropbear-ssh service.yaml 应包含 'autostart: false'")
	}

	// env.yaml 应包含 SSH_PUBLIC_KEY 变量
	envYaml, err := os.ReadFile(filepath.Join(tmpDir, "services/dropbear-ssh/env.yaml"))
	if err != nil {
		t.Fatalf("读取 env.yaml 失败: %v", err)
	}
	if !strings.Contains(string(envYaml), "SSH_PUBLIC_KEY") {
		t.Errorf("dropbear-ssh env.yaml 应包含 'SSH_PUBLIC_KEY' 变量")
	}

	// run.sh 应同时支持公钥认证和空白密码免认证两种模式
	runSH, err := os.ReadFile(filepath.Join(tmpDir, "services/dropbear-ssh/run.sh"))
	if err != nil {
		t.Fatalf("读取 run.sh 失败: %v", err)
	}
	runContent := string(runSH)
	if !strings.Contains(runContent, "dropbear -R -s -F") {
		t.Errorf("run.sh 应包含公钥认证模式（dropbear -s）")
	}
	if !strings.Contains(runContent, "dropbear -R -B -F") {
		t.Errorf("run.sh 应包含空白密码免认证模式（dropbear -B）")
	}

	// setup-ssh-keys 扩展不应再生成（已删除）
	setupExtPath := filepath.Join(tmpDir, "extensions", "setup-ssh-keys")
	if _, err := os.Stat(setupExtPath); err == nil {
		t.Errorf("setup-ssh-keys 扩展不应再生成（公钥配置已移至 dropbear-ssh 服务的 env.yaml + run.sh）")
	}
}
