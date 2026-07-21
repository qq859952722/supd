package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// REQ-F-039: reload 命令 — 手动触发配置热重载
// 规格 §2.4.2: 热重载是核心特性（自动 fsnotify + 手动 POST /api/reload 兜底）
// P-02-001 修复：原实现错误调用 POST /api/settings（仅 GET/PUT）和 PUT /api/services/{name}（更新服务配置），
// 两个子命令均失败。改为统一调用 POST /api/reload（server.go 中注册的真正重载端点）。
// 后端不支持单服务级热重载（spec 无此要求），原 --service 标志从未工作过，已移除。
var reloadCmd = &cobra.Command{
	Use:   "reload",
	Short: "热重载配置",
	Long:  "手动触发配置热重载。通过 HTTP API 调用 POST /api/reload，重新扫描 services/ 与 extensions/ 目录并更新所有 providers。",
	Example: `  # 手动触发配置热重载
  supd reload`,
	RunE: runReload,
}

// runReload 执行 reload 命令
// REQ-F-039 / N-04-002: supd reload → POST /api/reload
func runReload(cmd *cobra.Command, args []string) error {
	client := getAPIClient()
	if err := client.CheckSupdRunning(); err != nil {
		return err
	}

	var result map[string]any
	if err := client.PostJSON("/api/reload", nil, &result); err != nil {
		return fmt.Errorf("重载配置失败: %w", err)
	}

	// 解析后端返回的状态字段
	status, _ := result["status"].(string)
	switch status {
	case "ok":
		infof("配置已重载（services=%v, global_extensions=%v）",
			result["services"], result["global_extensions"])
	case "partial":
		infof("配置部分重载（services=%v, global_extensions=%v, scan_errors=%v）",
			result["services"], result["global_extensions"], result["scan_errors"])
		if details, ok := result["error_details"].([]any); ok {
			for _, d := range details {
				if m, ok := d.(map[string]any); ok {
					verbosef("  扫描错误: %s — %s", m["path"], m["message"])
				}
			}
		}
	default:
		infof("配置已重载")
	}
	return nil
}
