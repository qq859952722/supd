package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// REQ-F-039: start 命令 — 启动服务
var (
	startAll bool
)

var startCmd = &cobra.Command{
	Use:   "start <name>",
	Short: "启动服务",
	Long:  "启动指定服务或所有服务。通过 HTTP API 与运行中的 supd 通信。",
	Example: `  # 启动单个服务
  supd start web-demo

  # 启动所有服务
  supd start --all`,
	RunE: runStart,
}

func init() {
	startCmd.Flags().BoolVar(&startAll, "all", false, "启动所有服务")
}

// runStart 执行 start 命令
// REQ-F-039: supd start <name> / supd start --all
func runStart(cmd *cobra.Command, args []string) error {
	client := getAPIClient()
	if err := client.CheckSupdRunning(); err != nil {
		return err
	}

	if startAll {
		return startAllServices(client)
	}

	if len(args) == 0 {
		return fmt.Errorf("请指定服务名称或使用 --all")
	}

	return startService(client, args[0])
}

func startService(client *APIClient, name string) error {
	var result map[string]any
	if err := client.PostJSON("/api/services/"+name+"/start", nil, &result); err != nil {
		return fmt.Errorf("启动服务 %s 失败: %w", name, err)
	}
	infof("服务 %s 已启动", name)
	return nil
}

func startAllServices(client *APIClient) error {
	// 获取所有服务列表
	var response struct {
		Services []map[string]any `json:"services"`
	}
	if err := client.GetJSON("/api/services", &response); err != nil {
		return fmt.Errorf("获取服务列表失败: %w", err)
	}

	services := response.Services
	for _, svc := range services {
		name, _ := svc["name"].(string)
		if name == "" {
			continue
		}
		if err := startService(client, name); err != nil {
			infof("  启动 %s 失败: %v", name, err)
		}
	}
	return nil
}
