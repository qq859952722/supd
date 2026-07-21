package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// REQ-F-039: import 命令 — 导入服务/扩展
var (
	importYes bool
)

var importCmd = &cobra.Command{
	Use:   "import <path>",
	Short: "导入服务或扩展",
	Long:  "从文件导入服务或扩展的配置。通过 HTTP API 与运行中的 supd 通信。",
	Args:  exactArgs(1),
	RunE:  runImport,
}

func init() {
	importCmd.Flags().BoolVar(&importYes, "yes", false, "自动确认导入，不提示")
}

// runImport 执行 import 命令
// REQ-F-039: supd import <path> [--yes]
func runImport(cmd *cobra.Command, args []string) error {
	client := getAPIClient()
	if err := client.CheckSupdRunning(); err != nil {
		return err
	}

	path := args[0]

	// 检查文件是否存在
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("文件 %s 不存在", path)
	}

	// 读取文件
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取文件失败: %w", err)
	}

	// 先调用 /api/services/import 预览
	var preview map[string]any
	importBody := map[string]any{
		"data": string(data),
	}
	if err := client.PostJSON("/api/services/import", importBody, &preview); err != nil {
		return fmt.Errorf("导入预览失败: %w", err)
	}

	if !importYes {
		// 显示预览信息并确认
		infof("即将导入:")
		// NOTE: 更详细的预览信息待后续版本实现
		infof("使用 --yes 自动确认，或按 Ctrl+C 取消")
		// 简化确认：在非交互模式下直接继续
	}

	// 确认导入
	var result map[string]any
	if err := client.PostJSON("/api/services/import/confirm", importBody, &result); err != nil {
		return fmt.Errorf("导入确认失败: %w", err)
	}

	infof("导入成功: %s", path)
	return nil
}
