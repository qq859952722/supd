package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// REQ-F-039: runtimes 子命令组 — 运行时管理
var runtimesCmd = &cobra.Command{
	Use:   "runtimes",
	Short: "运行时管理",
	Long:  "管理运行时（如 bun、deno）：列出、安装、移除。",
}

var runtimesListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有运行时",
	RunE:  runRuntimesList,
}

var runtimesInstallCmd = &cobra.Command{
	Use:   "install <name> <path>",
	Short: "安装运行时",
	Long:  "将可执行文件注册为运行时别名。path 必须是绝对路径。",
	Args:  exactArgs(2),
	RunE:  runRuntimesInstall,
}

var runtimesRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "移除运行时",
	Args:  exactArgs(1),
	RunE:  runRuntimesRemove,
}

func init() {
	runtimesCmd.AddCommand(runtimesListCmd)
	runtimesCmd.AddCommand(runtimesInstallCmd)
	runtimesCmd.AddCommand(runtimesRemoveCmd)
}

// REQ-F-039: supd runtimes list
// BUG-05 修复：原代码调用 /api/settings/runtimes（返回 map）却按 {"runtimes":{...}} 解析，
// 导致永远显示"没有已配置的运行时"。改为调用 /api/runtimes 获取完整运行时列表（含 source/available）。
func runRuntimesList(cmd *cobra.Command, args []string) error {
	client := getAPIClient()
	if err := client.CheckSupdRunning(); err != nil {
		return err
	}

	var resp struct {
		Runtimes []struct {
			Alias     string `json:"alias"`
			Path      string `json:"path"`
			Source    string `json:"source"`
			Available bool   `json:"available"`
		} `json:"runtimes"`
		Default string `json:"default"`
	}
	if err := client.GetJSON("/api/runtimes", &resp); err != nil {
		return fmt.Errorf("获取运行时列表失败: %w", err)
	}

	if len(resp.Runtimes) == 0 {
		infof("没有已配置的运行时")
		return nil
	}

	for _, rt := range resp.Runtimes {
		marker := ""
		if rt.Alias == resp.Default {
			marker = " (default)"
		}
		status := ""
		if !rt.Available {
			status = " [unavailable]"
		}
		fmt.Printf("%-20s %-40s %s%s%s\n", rt.Alias, rt.Path, rt.Source, marker, status)
	}
	return nil
}

// REQ-F-039: supd runtimes install <name> <path>
func runRuntimesInstall(cmd *cobra.Command, args []string) error {
	name := args[0]
	execPath := args[1]

	// 验证路径是绝对路径
	if len(execPath) == 0 || execPath[0] != '/' {
		return fmt.Errorf("运行时路径必须是绝对路径，得到: %s", execPath)
	}

	client := getAPIClient()
	if err := client.CheckSupdRunning(); err != nil {
		return err
	}

	// 获取当前 runtimes 配置
	var currentSettings map[string]any
	if err := client.GetJSON("/api/settings", &currentSettings); err != nil {
		// 可能是新配置，使用空 map
		currentSettings = make(map[string]any)
	}

	// 更新 runtimes 配置
	runtimesMap := make(map[string]interface{})
	if existingRuntimes, ok := currentSettings["runtimes"].(map[string]interface{}); ok {
		for k, v := range existingRuntimes {
			runtimesMap[k] = v
		}
	}
	runtimesMap[name] = execPath

	updateBody := map[string]any{"runtimes": runtimesMap}
	var result map[string]any
	if err := client.PutJSON("/api/settings/runtimes", updateBody, &result); err != nil {
		return fmt.Errorf("安装运行时 %s 失败: %w", name, err)
	}

	infof("运行时 %s 已安装: %s", name, execPath)
	return nil
}

// REQ-F-039: supd runtimes remove <name>
func runRuntimesRemove(cmd *cobra.Command, args []string) error {
	name := args[0]

	client := getAPIClient()
	if err := client.CheckSupdRunning(); err != nil {
		return err
	}

	// 删除操作：通过 PUT 更新 runtimes 配置移除该键
	var currentSettings map[string]any
	if err := client.GetJSON("/api/settings", &currentSettings); err != nil {
		return fmt.Errorf("获取当前设置失败: %w", err)
	}

	runtimesMap := make(map[string]interface{})
	if existingRuntimes, ok := currentSettings["runtimes"].(map[string]interface{}); ok {
		for k, v := range existingRuntimes {
			if k != name {
				runtimesMap[k] = v
			}
		}
	}

	updateBody := map[string]any{"runtimes": runtimesMap}
	var result map[string]any
	if err := client.PutJSON("/api/settings/runtimes", updateBody, &result); err != nil {
		return fmt.Errorf("移除运行时 %s 失败: %w", name, err)
	}

	infof("运行时 %s 已移除", name)
	return nil
}
