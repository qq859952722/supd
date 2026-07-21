package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// REQ-F-039: status 命令 — 查询服务状态
var (
	statusOutputJSON bool
)

var statusCmd = &cobra.Command{
	Use:   "status [name]",
	Short: "查询服务状态",
	Long:  "查询所有服务或指定服务的状态，通过 HTTP API 与运行中的 supd 通信。",
	Example: `  # 查询所有服务状态
  supd status

  # 查询单个服务详情
  supd status web-demo

  # 以 JSON 格式输出（便于脚本处理）
  supd status --json`,
	Args: maximumNArgs(1),
	RunE: runStatus,
}

func init() {
	statusCmd.Flags().BoolVarP(&statusOutputJSON, "output", "o", false, "以 JSON 格式输出")
}

// runStatus 执行 status 命令
// REQ-F-039: 通过 HTTP API 查询服务状态
func runStatus(cmd *cobra.Command, args []string) error {
	client := getAPIClient()
	if err := client.CheckSupdRunning(); err != nil {
		return err
	}

	if len(args) > 0 {
		// 单服务状态
		return showServiceStatus(client, args[0])
	}

	// 全部服务状态
	return showAllServicesStatus(client)
}

func showServiceStatus(client *APIClient, name string) error {
	var result map[string]any
	if err := client.GetJSON("/api/services/"+name, &result); err != nil {
		return fmt.Errorf("查询服务 %s 状态失败: %w", name, err)
	}

	if statusOutputJSON {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	// 表格输出
	printServiceStatus(result)
	return nil
}

func showAllServicesStatus(client *APIClient) error {
	var response struct {
		Services []map[string]any `json:"services"`
	}
	if err := client.GetJSON("/api/services", &response); err != nil {
		return fmt.Errorf("查询服务状态失败: %w", err)
	}

	result := response.Services
	if statusOutputJSON {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	if len(result) == 0 {
		infof("没有已配置的服务")
		return nil
	}

	for _, svc := range result {
		printServiceStatus(svc)
	}
	return nil
}

func printServiceStatus(svc map[string]any) {
	name, _ := svc["name"].(string)
	state, _ := svc["state"].(string)
	fmt.Printf("%-30s %s\n", name, state)
}
