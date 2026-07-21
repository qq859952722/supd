package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// REQ-F-039: signal 命令 — 发送信号
var signalCmd = &cobra.Command{
	Use:   "signal <name> <SIGNAL>",
	Short: "向服务进程发送信号",
	Long:  "向指定服务的进程发送 UNIX 信号。通过 HTTP API 与运行中的 supd 通信。",
	Args:  exactArgs(2),
	RunE:  runSignal,
}

// runSignal 执行 signal 命令
// REQ-F-039: supd signal <name> <SIGNAL>
func runSignal(cmd *cobra.Command, args []string) error {
	client := getAPIClient()
	if err := client.CheckSupdRunning(); err != nil {
		return err
	}

	name := args[0]
	sig := args[1]

	body := map[string]string{"signal": sig}
	var result map[string]any
	if err := client.PostJSON("/api/services/"+name+"/signal", body, &result); err != nil {
		return fmt.Errorf("发送信号 %s 到服务 %s 失败: %w", sig, name, err)
	}
	infof("已发送信号 %s 到服务 %s", sig, name)
	return nil
}
