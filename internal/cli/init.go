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
	Long:  "创建 supd 工作目录的完整结构，包括配置文件、环境变量文件、随机生成的 auth_token，以及覆盖核心特性的示例服务与扩展。",
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

	// 创建示例服务（4 个，覆盖 4 种 readiness + 3 种 restart policy + depends_on + signals）
	if err := createExampleServices(dir); err != nil {
		return err
	}

	// 创建示例扩展（3 个全局，覆盖 3 种全局触发器；服务级扩展随 web-demo 创建）
	if err := createExampleExtensions(dir); err != nil {
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

	content := fmt.Sprintf(`# =============================================================================
# supd 全局主配置
# =============================================================================
# 本文件是 supd 进程监督器的全局配置，定义 HTTP API、认证、日志、扩展、
# 文件历史等行为。所有字段均由 `+"`supd init`"+` 生成默认值，可按需修改。
#
# 热重载（无需重启 supd）：
#   - 大部分字段支持 SIGHUP 信号或文件 watcher 自动热重载
#   - 标注【需重启】的字段必须重启 supd 才能生效
#   - 服务级配置（services/*/service.yaml）单独热重载
#
# 规格参考：docs/需求规格说明_v1.5.md §1.4（REQ-D-006）
# =============================================================================

# settings — supd 自身运行参数
settings:
  # HTTP API 监听地址
  # 格式：":<port>" 监听所有网卡；"127.0.0.1:<port>" 仅监听本地
  # 默认：":7979"
  # 【需重启】
  http_listen: ":7979"

  # API 认证模式
  # 可选值：
  #   none         不启用认证（仅内网完全可信场景）
  #   local_skip   本地/局域网免认证，外部需 token（推荐家庭 NAS）
  #   always_token 所有请求必须携带 token
  # 默认："local_skip"
  auth_mode: "local_skip"

  # API 认证 token（由 `+"`supd init`"+` 使用 crypto/rand 生成 32 字节 hex）
  # 在 always_token 模式下必填；local_skip 模式下仅外部请求校验
  # 命令：`+"`supd token verify <token>`"+` 验证，`+"`supd token generate`"+` 重新生成
  auth_token: "%s"

  # local_skip 模式下免认证的 CIDR 网段列表
  # 默认包含常见内网网段；可添加 IPv6（如 fc00::/7）
  local_networks:
    - "192.168.0.0/16"
    - "10.0.0.0/8"
    - "127.0.0.0/8"
    - "172.16.0.0/12"

  # 日志级别：debug / info / warn / error
  # 默认："info"
  log_level: "info"

  # 单个日志文件最大体积（MB），超过后轮转
  # 默认：10
  log_max_size_mb: 10

  # 日志文件保留数量（轮转后旧文件数）
  # 默认：5
  log_max_files: 5

  # 优雅退出总预算（秒）
  # 包含：服务停止 + 扩展任务等待（最多 30s）+ HTTP 关闭
  # 超时后强制终止剩余进程
  # 默认：30
  shutdown_grace_seconds: 30

  # 扩展执行默认超时（秒）
  # meta.yaml 未指定 timeout_seconds 时使用此值
  # 默认：600（10 分钟）
  extension_default_timeout_seconds: 600

  # 扩展执行硬上限（秒）
  # 即使 meta.yaml 中 timeout_seconds 超过此值也会被截断
  # 规格 §2.2.8 数值锁定：1800
  extension_hard_limit_seconds: 1800

  # 扩展运行历史保留时长（秒），超过后自动清理（仅内存，重启清空）
  # 默认：604800（7 天）
  run_history_retention_seconds: 604800

  # 文件编辑历史保留版本数（PUT /api/files 修改时自动保留）
  # 规格 §2.3.1 数值锁定：50
  file_history_versions: 50

  # 文件上传大小上限（MB），适用于 /api/files/upload 和 /api/runtimes/upload
  # 规格 §2.12.6 数值锁定：100
  max_upload_size_mb: 100

# env_files — 全局环境变量文件列表
# 按文件名字母序加载，后加载的覆盖先加载的同名变量
# 路径相对于工作目录
env_files:
  - env/00-base.yaml

# extension_dirs — 全局扩展目录列表
# 每个子目录视为一个扩展（需含 meta.yaml + 入口脚本）
# 路径相对于工作目录
extension_dirs:
  - extensions/

# runtimes — 运行时映射
# key:   runtime 名称（meta.yaml 中 runtime 字段引用）
# value: 可执行文件绝对路径
# 内置：bash（默认）/ python / python3 / node / sh
# 也可通过 `+"`supd runtimes install`"+` 安装自定义 runtime（如 tjs）
runtimes: {}

# defaults — 全局默认值（服务可在 service.yaml 中覆盖）
defaults:
  # restart — 服务默认重启策略
  # policy 可选值：
  #   always      总是重启（无论退出码）
  #   on-failure  仅退出码非 0 时重启
  #   never       从不重启
  restart:
    policy: "always"            # 重启策略
    backoff_ms: 1000            # 初始退避时长（毫秒）
    max_backoff_ms: 30000       # 最大退避时长（毫秒）
    multiplier: 2               # 退避乘数（每次失败 backoff *= multiplier）
    max_retries: 0              # 最大重试次数（0 = 不限制）
    reset_after_seconds: 300    # 成功运行此时长后重置退避
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

// exampleEntry 描述一个示例（服务或扩展）的名称与待写入文件映射
// key: 相对于示例根目录的子路径（如 "service.yaml" 或 "extensions/demo-lifecycle/run.sh"）
// value: 文件内容（来自 init_examples.go 中的字面量）
type exampleEntry struct {
	name  string
	files map[string]string
}

// writeExampleFile 写入示例文件（已存在则跳过，除非 --force）
// 行为与 createDefaultConfig/createDefaultEnv 一致：
//   - 文件已存在且未 --force：跳过并打印提示
//   - --dry-run：仅打印，不实际写入
//   - 自动创建父目录
//
// mode 通常为 0644（YAML/文本）或 0755（可执行脚本 .sh/.py）
func writeExampleFile(path, content string, mode os.FileMode) error {
	if _, err := os.Stat(path); err == nil {
		if !initForce {
			infof("  跳过已存在的文件: %s (使用 --force 覆盖)", path)
			return nil
		}
	}
	if initDryRun {
		infof("  会创建文件: %s", path)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", path, err)
	}
	verbosef("已创建: %s", path)
	return nil
}

// createExampleServices 创建 4 个示例服务
// 覆盖 4 种 readiness（http_check/tcp_check/fd_notify/script）
// + 3 种 restart policy（always/on-failure/no）+ depends_on + signals
// 其中 web-demo 携带 1 个服务级扩展 demo-lifecycle（service_lifecycle + debounce:5s）
func createExampleServices(dir string) error {
	services := []exampleEntry{
		{
			name: "web-demo",
			files: map[string]string{
				"service.yaml":                          webDemoServiceYAML,
				"run.py":                                webDemoRunPY,
				"extensions/demo-lifecycle/meta.yaml":   demoLifecycleMetaYAML,
				"extensions/demo-lifecycle/run.sh":      demoLifecycleRunSH,
			},
		},
		{
			name: "tcp-echo",
			files: map[string]string{
				"service.yaml": tcpEchoServiceYAML,
				"run.sh":       tcpEchoRunSH,
			},
		},
		{
			name: "signals-demo",
			files: map[string]string{
				"service.yaml": signalsDemoServiceYAML,
				"run.sh":       signalsDemoRunSH,
			},
		},
		{
			name: "script-ready-demo",
			files: map[string]string{
				"service.yaml":   scriptReadyDemoServiceYAML,
				"run.sh":         scriptReadyDemoRunSH,
				"check_ready.sh": scriptReadyDemoCheckSH,
			},
		},
	}
	for _, svc := range services {
		svcDir := filepath.Join(dir, "services", svc.name)
		for relPath, content := range svc.files {
			mode := os.FileMode(0644)
			ext := filepath.Ext(relPath)
			if ext == ".sh" || ext == ".py" {
				mode = 0755
			}
			if err := writeExampleFile(filepath.Join(svcDir, relPath), content, mode); err != nil {
				return err
			}
		}
	}
	return nil
}

// createExampleExtensions 创建 3 个全局示例扩展
// 覆盖 3 种全局触发器（on_demand/on_schedule/supd_lifecycle）
// + 3 种并发策略（replace/serialize/parallel）
// 第 4 种触发器（service_lifecycle）与并发策略（debounce:Ns）由 web-demo 的服务级扩展 demo-lifecycle 演示
func createExampleExtensions(dir string) error {
	exts := []exampleEntry{
		{
			name: "on-demand-tool",
			files: map[string]string{
				"meta.yaml": onDemandToolMetaYAML,
				"run.sh":    onDemandToolRunSH,
			},
		},
		{
			name: "scheduled-task",
			files: map[string]string{
				"meta.yaml": scheduledTaskMetaYAML,
				"run.sh":    scheduledTaskRunSH,
			},
		},
		{
			name: "supd-startup-hook",
			files: map[string]string{
				"meta.yaml": supdStartupHookMetaYAML,
				"run.sh":    supdStartupHookRunSH,
				"env.yaml":  supdStartupHookEnvYAML,
			},
		},
	}
	for _, ext := range exts {
		extDir := filepath.Join(dir, "extensions", ext.name)
		for relPath, content := range ext.files {
			mode := os.FileMode(0644)
			if filepath.Ext(relPath) == ".sh" {
				mode = 0755
			}
			if err := writeExampleFile(filepath.Join(extDir, relPath), content, mode); err != nil {
				return err
			}
		}
	}
	return nil
}
