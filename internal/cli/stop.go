package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// REQ-F-039: stop 命令 — 停止服务
var (
	stopAll bool
)

var stopCmd = &cobra.Command{
	Use:   "stop <name>",
	Short: "停止服务",
	Long:  "停止指定服务或所有服务。通过 HTTP API 与运行中的 supd 通信。",
	Example: `  # 停止单个服务
  supd stop web-demo

  # 停止所有服务
  supd stop --all`,
	RunE: runStop,
}

func init() {
	stopCmd.Flags().BoolVar(&stopAll, "all", false, "停止所有服务")
}

// runStop 执行 stop 命令
// REQ-F-039: supd stop <name> / supd stop --all
func runStop(cmd *cobra.Command, args []string) error {
	client := getAPIClient()
	if err := client.CheckSupdRunning(); err != nil {
		return err
	}

	if stopAll {
		return stopAllServices(client)
	}

	if len(args) == 0 {
		return fmt.Errorf("请指定服务名称或使用 --all")
	}

	return stopService(client, args[0])
}

func stopService(client *APIClient, name string) error {
	var result map[string]any
	if err := client.PostJSON("/api/services/"+name+"/stop", nil, &result); err != nil {
		return fmt.Errorf("停止服务 %s 失败: %w", name, err)
	}
	infof("服务 %s 已停止", name)
	return nil
}

func stopAllServices(client *APIClient) error {
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
		if err := stopService(client, name); err != nil {
			infof("  停止 %s 失败: %v", name, err)
		}
	}
	return nil
}
