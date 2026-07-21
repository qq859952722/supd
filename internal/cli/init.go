package cli

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// REQ-F-039: init 命令 — 初始化目录结构
var (
	initForce  bool
	initDryRun bool
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "初始化 supd 目录结构",
	Long:  "创建 supd 工作目录的完整结构，包括配置文件、环境变量文件和随机生成的 auth_token。",
	Example: `  # 初始化默认工作目录（当前目录下的 supd_workdir/）
  supd init

  # 初始化指定工作目录
  supd --workdir /path/to/workdir init

  # 仅预览将要创建的内容，不实际创建
  supd init --dry-run

  # 强制覆盖已存在的配置文件
  supd init --force`,
	RunE: runInit,
}

func init() {
	initCmd.Flags().BoolVar(&initForce, "force", false, "强制覆盖已存在的文件")
	initCmd.Flags().BoolVar(&initDryRun, "dry-run", false, "仅打印将要创建的内容，不实际创建")
}

// runInit 执行 init 命令
// REQ-F-039: 创建完整目录结构 + 默认 config.yaml + 默认 env/00-base.yaml + 随机 auth_token
func runInit(cmd *cobra.Command, args []string) error {
	dir := getWorkDir()

	// 目录列表，按需求规格说明 1.6 节
	dirs := []string{
		filepath.Join(dir, "env"),
		filepath.Join(dir, "extensions"),
		filepath.Join(dir, "runtimes"),
		filepath.Join(dir, "assets", "certs"),
		filepath.Join(dir, "assets", "templates"),
		filepath.Join(dir, "script_tmp"),
		filepath.Join(dir, "services"),
	}

	// 创建目录
	for _, d := range dirs {
		if initDryRun {
			infof("  会创建目录: %s", d)
			continue
		}
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("创建目录 %s 失败: %w", d, err)
		}
		verbosef("已创建目录: %s", d)
	}

	// 创建默认 config.yaml
	configPath := filepath.Join(dir, "config.yaml")
	if err := createDefaultConfig(configPath); err != nil {
		return err
	}

	// 创建默认 env/00-base.yaml
	envPath := filepath.Join(dir, "env", "00-base.yaml")
	if err := createDefaultEnv(envPath); err != nil {
		return err
	}

	if !initDryRun {
		infof("supd 工作目录已初始化: %s", dir)
	} else {
		infof("(dry-run) supd 工作目录将初始化于: %s", dir)
	}

	return nil
}

// createDefaultConfig 创建默认 config.yaml，含随机生成的 auth_token
// REQ-F-039: 随机生成 auth_token 并打印
func createDefaultConfig(path string) error {
	// 检查文件是否已存在
	if _, err := os.Stat(path); err == nil {
		if !initForce {
			infof("  跳过已存在的文件: %s (使用 --force 覆盖)", path)
			return nil
		}
	}

	// 生成随机 auth_token: 32 字节 hex
	// REQ-F-039: auth_token 使用 crypto/rand 生成
	token, err := generateAuthToken()
	if err != nil {
		return fmt.Errorf("生成 auth_token 失败: %w", err)
	}

	content := fmt.Sprintf(`# supd 全局主配置
# REQ-D-006: config.yaml

settings:
  http_listen: ":8080"
  auth_mode: "local_skip"
  auth_token: "%s"
  log_level: "info"
  log_max_size_mb: 10
  log_max_files: 5
  shutdown_grace_seconds: 30
  extension_default_timeout_seconds: 600
  extension_hard_limit_seconds: 1800
  run_history_retention_seconds: 604800
  file_history_versions: 50
  max_upload_size_mb: 100

env_files:
  - env/00-base.yaml

extension_dirs:
  - extensions/

runtimes: {}

defaults:
  restart:
    policy: "always"
    backoff_ms: 1000
    max_backoff_ms: 30000
    multiplier: 2
    max_retries: 0
    reset_after_seconds: 300
`, token)

	if initDryRun {
		infof("  会创建文件: %s", path)
		infof("  auth_token: %s", token)
		return nil
	}

	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", path, err)
	}

	verbosef("已创建: %s", path)
	infof("  生成的 auth_token: %s", token)
	infof("  请妥善保存此 token，它不会再次显示")
	return nil
}

// createDefaultEnv 创建默认 env/00-base.yaml
func createDefaultEnv(path string) error {
	if _, err := os.Stat(path); err == nil {
		if !initForce {
			infof("  跳过已存在的文件: %s (使用 --force 覆盖)", path)
			return nil
		}
	}

	content := `# supd 默认环境变量
# 全局环境变量文件，按文件名字母序加载
`

	if initDryRun {
		infof("  会创建文件: %s", path)
		return nil
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", path, err)
	}
	verbosef("已创建: %s", path)
	return nil
}

// generateAuthToken 使用 crypto/rand 生成 32 字节 hex 字符串
func generateAuthToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
