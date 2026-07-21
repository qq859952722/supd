// Package cli 实现 supd 命令行接口。
// REQ-F-039: cobra CLI，约 25 个命令
// REQ-C-012: 使用 spf13/cobra + spf13/pflag
package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// REQ-F-039: 全局 flags
var (
	cfgPath string
	workDir string
	verbose bool
	quiet   bool
	noColor bool
)

// REQ-F-039: rootCmd 是 CLI 根命令
var rootCmd = &cobra.Command{
	Use:   "supd",
	Short: "supd — 家庭 NAS 进程监督器",
	Long:  "supd 是面向家庭 NAS 场景的进程监督器，学习 S6 思想但不兼容。",
	// P-02-001: 未知命令返回中文错误（替代 cobra 默认的 "unknown command"）
	// 仅当未匹配到任何子命令时才触发；匹配到子命令时由子命令的 Args 校验
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return fmt.Errorf("未知命令: %s。请使用 'supd --help' 查看可用命令", args[0])
		}
		return nil
	},
	// REQ-F-039: 默认行为等同 supd run
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCmd.RunE(cmd, args)
	},
	// P-02-001: 命令返回错误时不打印usage（仅打印错误消息）
	SilenceUsage: true,
	// P-02-01: 错误由 main.go 统一输出（"错误: ..."），避免 cobra 默认的 "Error: ..." 重复
	SilenceErrors: true,
}

func init() {
	// REQ-F-039: 全局 flags（2.13.3）
	rootCmd.PersistentFlags().StringVar(&cfgPath, "config", "", "配置文件路径")
	rootCmd.PersistentFlags().StringVar(&workDir, "workdir", "", "工作目录")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "详细输出")
	rootCmd.PersistentFlags().BoolVar(&quiet, "quiet", false, "静默")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "禁用彩色")

	// 注册所有子命令
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(restartCmd)
	rootCmd.AddCommand(signalCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(extCmd)
	rootCmd.AddCommand(exportCmd)
	rootCmd.AddCommand(importCmd)
	rootCmd.AddCommand(tokenCmd)
	rootCmd.AddCommand(reloadCmd)
	rootCmd.AddCommand(runtimesCmd)
	rootCmd.AddCommand(validateCmd)

	// P-01-05: 设置 flag 错误处理函数为中文，递归应用到所有子命令
	setChineseFlagErrorFunc(rootCmd)
}

// setChineseFlagErrorFunc 递归设置命令树的 flag 错误处理函数，输出中文错误消息。
func setChineseFlagErrorFunc(cmd *cobra.Command) {
	cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		// M-01-002 修复：使用 %w 包装保留错误链，便于上层 errors.Is/As 判断
		return fmt.Errorf("参数错误: %w", err)
	})
	for _, sub := range cmd.Commands() {
		setChineseFlagErrorFunc(sub)
	}
}

// Execute 执行根命令。
func Execute() error {
	return rootCmd.Execute()
}

// getWorkDir 获取工作目录，优先使用 --workdir，否则默认 /etc/supd/
// REQ-F-039: 默认工作目录 /etc/supd/
// 注意：必须返回绝对路径，否则 PathValidator 的前缀检查会失败
func getWorkDir() string {
	if workDir != "" {
		// 将相对路径转为绝对路径，避免 PathValidator 的前缀匹配问题
		if abs, err := filepath.Abs(workDir); err == nil {
			return abs
		}
		return workDir
	}
	return "/etc/supd"
}

// getConfigPath 获取配置文件路径
func getConfigPath() string {
	if cfgPath != "" {
		return cfgPath
	}
	return fmt.Sprintf("%s/config.yaml", getWorkDir())
}

// getAPIClient 创建 API 客户端，用于与运行中的 supd 通信
// REQ-F-039: CLI 命令通过 HTTP API 与 supd 通信
//
// L-04-003 修复：改为变量形式以便测试覆盖。
// 原函数形式使得 *WithoutSupd 类测试依赖 localhost:7979 不可连接，
// 当真实 supd 运行时测试会间歇性失败。改为变量后，测试可临时覆盖为不可连接地址。
var getAPIClient = func() *APIClient {
	baseURL := "http://localhost:7979"
	token := ""
	return NewAPIClient(baseURL, token)
}

// exactArgs 返回一个 cobra.PositionalArgs 验证函数，参数数量不匹配时返回中文错误。
// P-01-05/P-02-04: 替代 cobra.ExactArgs，避免英文 "accepts N arg(s), received M"
func exactArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != n {
			return fmt.Errorf("参数数量错误：期望 %d 个，实际 %d 个", n, len(args))
		}
		return nil
	}
}

// maximumNArgs 返回一个 cobra.PositionalArgs 验证函数，参数过多时返回中文错误。
// P-01-05/P-02-04: 替代 cobra.MaximumNArgs，避免英文错误消息
func maximumNArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) > n {
			return fmt.Errorf("参数数量错误：最多 %d 个，实际 %d 个", n, len(args))
		}
		return nil
	}
}

// infof 输出信息（quiet 时静默）
func infof(format string, args ...any) {
	if !quiet {
		fmt.Printf(format+"\n", args...)
	}
}

// verbosef 输出详细信息（仅 verbose 时）
func verbosef(format string, args ...any) {
	if verbose {
		fmt.Printf(format+"\n", args...)
	}
}

// readFile 读取文件内容为字符串
func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
