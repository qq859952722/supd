package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// REQ-F-039: ext 子命令组 — 扩展操作
var (
	extAction string
	extEnv    []string
)

var extCmd = &cobra.Command{
	Use:   "ext",
	Short: "扩展操作",
	Long:  "管理全局扩展：列出、查看详情、运行、查看状态。",
}

var extListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有扩展",
	RunE:  runExtList,
}

var extShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "显示扩展详情",
	Args:  exactArgs(1),
	RunE:  runExtShow,
}

var extRunCmd = &cobra.Command{
	Use:   "run <name>",
	Short: "运行扩展",
	Long:  "运行指定扩展的某个 action。必须指定 --action 参数。",
	Args:  exactArgs(1),
	RunE:  runExtRun,
}

var extStatusCmd = &cobra.Command{
	Use:   "status <name>",
	Short: "查看扩展运行状态",
	Args:  exactArgs(1),
	RunE:  runExtStatus,
}

func init() {
	extRunCmd.Flags().StringVar(&extAction, "action", "", "要运行的 action ID (必填)")
	extRunCmd.Flags().StringArrayVar(&extEnv, "env", nil, "环境变量 KEY=value")

	extCmd.AddCommand(extListCmd)
	extCmd.AddCommand(extShowCmd)
	extCmd.AddCommand(extRunCmd)
	extCmd.AddCommand(extStatusCmd)
}

// REQ-F-039: supd ext list
func runExtList(cmd *cobra.Command, args []string) error {
	client := getAPIClient()
	if err := client.CheckSupdRunning(); err != nil {
		return err
	}

	var result []map[string]any
	if err := client.GetJSON("/api/extensions", &result); err != nil {
		return fmt.Errorf("列出扩展失败: %w", err)
	}

	if len(result) == 0 {
		infof("没有已配置的扩展")
		return nil
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(data))
	return nil
}

// REQ-F-039: supd ext show <name>
func runExtShow(cmd *cobra.Command, args []string) error {
	client := getAPIClient()
	if err := client.CheckSupdRunning(); err != nil {
		return err
	}

	name := args[0]
	var result map[string]any
	if err := client.GetJSON("/api/extensions/"+name, &result); err != nil {
		return fmt.Errorf("查看扩展 %s 失败: %w", name, err)
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(data))
	return nil
}

// REQ-F-039: supd ext run <name> --action <id> [--env KEY=value]
func runExtRun(cmd *cobra.Command, args []string) error {
	client := getAPIClient()
	if err := client.CheckSupdRunning(); err != nil {
		return err
	}

	if extAction == "" {
		return fmt.Errorf("必须指定 --action 参数")
	}

	name := args[0]
	body := map[string]any{
		"action_id": extAction,
	}
	if len(extEnv) > 0 {
		envMap := make(map[string]string)
		for _, e := range extEnv {
			parts := splitEnvVar(e)
			if len(parts) == 2 {
				envMap[parts[0]] = parts[1]
			}
		}
		body["env_overrides"] = envMap
	}

	var result map[string]any
	if err := client.PostJSON("/api/extensions/"+name+"/run", body, &result); err != nil {
		return fmt.Errorf("运行扩展 %s 失败: %w", name, err)
	}

	infof("扩展 %s 的 action %s 已触发", name, extAction)
	return nil
}

// REQ-F-039: supd ext status <name>
func runExtStatus(cmd *cobra.Command, args []string) error {
	client := getAPIClient()
	if err := client.CheckSupdRunning(); err != nil {
		return err
	}

	name := args[0]
	var result map[string]any
	if err := client.GetJSON("/api/extensions/"+name+"/status", &result); err != nil {
		return fmt.Errorf("查看扩展 %s 状态失败: %w", name, err)
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(data))
	return nil
}

// splitEnvVar 拆分 KEY=value 格式的环境变量
func splitEnvVar(s string) []string {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return []string{s[:i], s[i+1:]}
		}
	}
	return []string{s}
}
