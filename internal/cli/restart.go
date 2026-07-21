package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// REQ-F-039: restart 命令 — 重启服务
var restartCmd = &cobra.Command{
	Use:   "restart <name>",
	Short: "重启服务",
	Long:  "重启指定服务。通过 HTTP API 与运行中的 supd 通信。",
	Example: `  # 重启单个服务
  supd restart web-demo`,
	Args: exactArgs(1),
	RunE:  runRestart,
}

// runRestart 执行 restart 命令
// REQ-F-039: supd restart <name>
func runRestart(cmd *cobra.Command, args []string) error {
	client := getAPIClient()
	if err := client.CheckSupdRunning(); err != nil {
		return err
	}

	name := args[0]
	var result map[string]any
	if err := client.PostJSON("/api/services/"+name+"/restart", nil, &result); err != nil {
		return fmt.Errorf("重启服务 %s 失败: %w", name, err)
	}
	infof("服务 %s 已重启", name)
	return nil
}
